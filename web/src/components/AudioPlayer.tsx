import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, img } from '../api'
import { formatTime } from '../libraryKinds'
import type { Track } from '../types'
import { Icon } from './Icon'

/**
 * The music and audiobook player: a bar along the bottom of every page that
 * keeps playing while you browse. Files play as they are when the browser
 * can; anything else (ALAC in Chrome, WMA, APE...) is converted to MP3 by
 * the server from the current position, and seeking asks again from there.
 * It reports progress like the video player, so audiobooks resume where you
 * left off and plays show on the admin dashboard.
 */

interface AudioState {
  queue: Track[]
  index: number
  playing: boolean
  /** Starts a queue at index. resume starts each audiobook chapter where it was left. */
  play: (queue: Track[], index?: number, opts?: { shuffle?: boolean; resume?: boolean }) => void
  /** Adds tracks after the current one. */
  playNext: (tracks: Track[]) => void
  toggle: () => void
  current?: Track
}

const AudioContext = createContext<AudioState | null>(null)

export function useAudio(): AudioState {
  const ctx = useContext(AudioContext)
  if (!ctx) throw new Error('useAudio outside AudioProvider')
  return ctx
}

/** Containers and codecs browsers don't play, so they're converted from the start. */
const convertAlways = new Set(['wma', 'ape', 'wv', 'dsf', 'aif', 'aiff', 'mka', 'mp2'])

function needsConversion(t: Track, probe: HTMLAudioElement): boolean {
  const c = t.file.container.toLowerCase()
  if (convertAlways.has(c)) return true
  if (t.file.audioCodec === 'alac') return probe.canPlayType('audio/mp4; codecs="alac"') === ''
  if (c === 'flac') return probe.canPlayType('audio/flac') === ''
  if (c === 'ogg' || c === 'oga' || c === 'opus') return probe.canPlayType('audio/ogg; codecs="opus"') === '' && probe.canPlayType('audio/ogg') === ''
  return false
}

function shuffled<T>(list: T[]): T[] {
  const out = [...list]
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return out
}

const SPEEDS = [1, 1.25, 1.5, 1.75, 2, 0.75]

export function AudioProvider({ children }: { children: ReactNode }) {
  const audio = useRef<HTMLAudioElement>(null)
  const [queue, setQueue] = useState<Track[]>([])
  const [index, setIndex] = useState(0)
  // Bumped by play(), so playing the same track again restarts it, while
  // adding to the queue (playNext) leaves the current one alone.
  const [token, setToken] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [time, setTime] = useState(0)
  const [showQueue, setShowQueue] = useState(false)
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('bb.audio.volume') ?? '1'))
  const [speed, setSpeed] = useState(1)
  // When converting, the stream starts at offset seconds into the file.
  const offset = useRef(0)
  const converted = useRef(false)
  const session = useRef('')
  const played = useRef(0)
  const lastTick = useRef(0)
  const resumeChapters = useRef(false)
  const current = queue[index] as Track | undefined
  const duration = current?.file.duration ?? 0

  const position = useCallback(() => offset.current + (audio.current?.currentTime ?? 0), [])

  const report = useCallback(
    (stopped = false, at?: number) => {
      const t = current
      if (!t || !session.current) return
      api
        .post('/api/progress', {
          playSessionId: session.current,
          itemId: t.item.id,
          fileId: t.file.fileId,
          position: at ?? position(),
          duration: t.file.duration,
          paused: audio.current?.paused ?? true,
          stopped,
          mode: converted.current ? 'transcode' : 'direct',
          client: 'Web',
          lights: false, // no Hue Sync for music
          played: Math.round(played.current),
        })
        .catch(() => undefined)
    },
    [current, position],
  )

  // Loads the current track, from start seconds in.
  const load = useCallback((t: Track, start: number, convert: boolean) => {
    const el = audio.current
    if (!el) return
    converted.current = convert
    if (convert) {
      offset.current = start
      el.src = `/api/files/${t.file.fileId}/audio?start=${start.toFixed(2)}`
    } else {
      offset.current = 0
      el.src = `/api/files/${t.file.fileId}/direct`
      if (start > 0) el.currentTime = start
    }
    el.playbackRate = speed
    el.play().catch(() => setPlaying(false))
  }, [speed])

  // A new track starts a new play session.
  useEffect(() => {
    const el = audio.current
    if (!current || !el) return
    session.current = `web-audio-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    played.current = 0
    const w = current.item.watch
    const start = resumeChapters.current && current.item.kind === 'chapter' && w.position > 15 && !w.watched ? w.position : 0
    load(current, start, needsConversion(current, el))
    return () => report(true)
  }, [index, token]) // eslint-disable-line react-hooks/exhaustive-deps

  // Progress every 10 s while playing.
  useEffect(() => {
    if (!playing) return
    const t = window.setInterval(() => report(), 10000)
    return () => window.clearInterval(t)
  }, [playing, report])

  const next = useCallback(() => {
    if (index + 1 < queue.length) setIndex(index + 1)
    else {
      audio.current?.pause()
      report(true, duration)
    }
  }, [index, queue.length, report, duration])
  const prev = useCallback(() => {
    if (position() > 4 || index === 0) seek(0)
    else setIndex(index - 1)
  }, [index, position]) // eslint-disable-line react-hooks/exhaustive-deps

  const seek = useCallback(
    (to: number) => {
      const el = audio.current
      if (!el || !current) return
      to = Math.max(0, Math.min(to, duration || to))
      if (converted.current) load(current, to, true)
      else el.currentTime = to
      setTime(to)
    },
    [current, duration, load],
  )

  const toggle = useCallback(() => {
    const el = audio.current
    if (!el || !current) return
    if (el.paused) el.play().catch(() => undefined)
    else el.pause()
  }, [current])

  const play = useCallback((tracks: Track[], at = 0, opts: { shuffle?: boolean; resume?: boolean } = {}) => {
    if (!tracks.length) return
    resumeChapters.current = !!opts.resume
    setToken((t) => t + 1)
    if (opts.shuffle) {
      setQueue(shuffled(tracks))
      setIndex(0)
    } else {
      setQueue([...tracks])
      setIndex(Math.max(0, Math.min(at, tracks.length - 1)))
    }
  }, [])

  const playNext = useCallback(
    (tracks: Track[]) => {
      if (!queue.length) return play(tracks)
      setQueue([...queue.slice(0, index + 1), ...tracks, ...queue.slice(index + 1)])
    },
    [queue, index, play],
  )

  const stop = () => {
    report(true)
    audio.current?.pause()
    audio.current?.removeAttribute('src')
    session.current = ''
    setQueue([])
    setIndex(0)
    setShowQueue(false)
  }

  useEffect(() => {
    if (audio.current) audio.current.volume = volume
    localStorage.setItem('bb.audio.volume', String(volume))
  }, [volume])
  useEffect(() => {
    if (audio.current) audio.current.playbackRate = speed
  }, [speed])

  // The OS media controls (keyboard media keys, lock screen, notification).
  useEffect(() => {
    if (!('mediaSession' in navigator) || !current) return
    const art = img(current.item.poster, 'w342')
    navigator.mediaSession.metadata = new MediaMetadata({
      title: current.item.title,
      artist: current.item.studio ?? '',
      album: current.album ?? '',
      artwork: art ? [{ src: art, sizes: '300x300', type: 'image/jpeg' }] : [],
    })
    navigator.mediaSession.setActionHandler('play', toggle)
    navigator.mediaSession.setActionHandler('pause', toggle)
    navigator.mediaSession.setActionHandler('nexttrack', next)
    navigator.mediaSession.setActionHandler('previoustrack', prev)
    navigator.mediaSession.setActionHandler('seekto', (d) => d.seekTime !== undefined && seek(d.seekTime))
  }, [current, toggle, next, prev, seek])

  const value = useMemo<AudioState>(() => ({ queue, index, playing, play, playNext, toggle, current }), [queue, index, playing, play, playNext, toggle, current])
  const isBook = current?.item.kind === 'chapter'

  return (
    <AudioContext.Provider value={value}>
      {children}
      <audio
        ref={audio}
        preload="auto"
        onPlay={() => {
          setPlaying(true)
          lastTick.current = audio.current?.currentTime ?? 0
        }}
        onPause={() => {
          setPlaying(false)
          report()
        }}
        onTimeUpdate={() => {
          const el = audio.current
          if (!el) return
          const d = el.currentTime - lastTick.current
          if (d > 0 && d < 2) played.current += d
          lastTick.current = el.currentTime
          setTime(position())
        }}
        onEnded={() => {
          report(true, duration)
          next()
        }}
        onError={() => {
          // Direct play failed: convert from where it got to.
          const el = audio.current
          if (current && el && !converted.current) load(current, position(), true)
          else setPlaying(false)
        }}
      />
      {current && (
        <div className="audio-bar" role="region" aria-label="Now playing">
          <Link to={`/item/${current.item.parentId ?? current.item.id}`} className="audio-now">
            <div className="audio-art">
              {current.item.poster && <img key={current.item.id} src={img(current.item.poster, 'w342')} alt="" onError={(e) => (e.currentTarget.style.visibility = 'hidden')} />}
            </div>
            <div className="audio-titles">
              <div className="audio-title" title={current.item.title}>{current.item.title}</div>
              <div className="muted small audio-sub">{[current.item.studio, current.album].filter((s, i, a) => s && a.indexOf(s) === i).join(' · ')}</div>
            </div>
          </Link>
          <div className="audio-center">
            <div className="audio-buttons">
              <button className="icon-btn" onClick={prev} aria-label="Previous"><Icon name="prev" size={20} /></button>
              {isBook && <button className="icon-btn audio-jump" onClick={() => seek(position() - 30)} aria-label="Back 30 seconds">−30</button>}
              <button className="audio-play" onClick={toggle} aria-label={playing ? 'Pause' : 'Play'}>
                <Icon name={playing ? 'pause' : 'play'} fill size={22} />
              </button>
              {isBook && <button className="icon-btn audio-jump" onClick={() => seek(position() + 30)} aria-label="Forward 30 seconds">+30</button>}
              <button className="icon-btn" onClick={next} disabled={index + 1 >= queue.length} aria-label="Next"><Icon name="skip" size={20} /></button>
            </div>
            <div className="audio-seek">
              <span className="small muted">{formatTime(time)}</span>
              <input
                type="range"
                min={0}
                max={duration || 1}
                step={1}
                value={Math.min(time, duration || 1)}
                onChange={(e) => seek(Number(e.target.value))}
                aria-label="Position"
              />
              <span className="small muted">{formatTime(duration)}</span>
            </div>
          </div>
          <div className="audio-right">
            {isBook && (
              <button className="btn btn-glass btn-sm" onClick={() => setSpeed(SPEEDS[(SPEEDS.indexOf(speed) + 1) % SPEEDS.length])} title="Playback speed">
                {speed}×
              </button>
            )}
            <button className={`icon-btn${showQueue ? ' active' : ''}`} onClick={() => setShowQueue((q) => !q)} aria-label="Queue" aria-expanded={showQueue}>
              <Icon name="list" size={20} />
            </button>
            <Icon name={volume === 0 ? 'mute' : 'volume'} size={18} />
            <input type="range" className="audio-volume" min={0} max={1} step={0.05} value={volume} onChange={(e) => setVolume(Number(e.target.value))} aria-label="Volume" />
            <button className="icon-btn" onClick={stop} aria-label="Stop and close the player"><Icon name="x" size={18} /></button>
          </div>
          {showQueue && (
            <div className="audio-queue panel">
              <div className="audio-queue-head">
                <strong>Queue</strong>
                <span className="muted small">{queue.length} {queue.length === 1 ? 'track' : 'tracks'}</span>
              </div>
              <ol>
                {queue.map((t, i) => (
                  <li key={`${t.item.id}-${i}`} className={i === index ? 'current' : ''}>
                    <button onClick={() => setIndex(i)}>
                      <span className="grow">{t.item.title}</span>
                      <span className="muted small">{t.item.studio}</span>
                      <span className="muted small">{formatTime(t.file.duration)}</span>
                    </button>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </div>
      )}
    </AudioContext.Provider>
  )
}
