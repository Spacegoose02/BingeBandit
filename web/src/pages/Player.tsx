import Hls from 'hls.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { channelLabel, episodeLabel, formatClock, languageName, resolutionLabel } from '../format'
import type { Item, ItemDetail, MediaFile, MediaStream } from '../types'

const TEXT_SUBS = new Set(['subrip', 'srt', 'ass', 'ssa', 'webvtt', 'mov_text', 'text'])

export default function Player() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const { data, error } = useApi<ItemDetail>(`/api/items/${id}`)
  useDocumentTitle(data ? (data.item.kind === 'episode' ? `${data.item.showTitle} ${episodeLabel(data.item)}` : data.item.title) : 'Player')
  if (error) return <div className="player"><div className="player-error">{error}</div></div>
  if (!data) return <div className="player"><div className="spinner" /></div>
  return <PlayerView key={id} detail={data} startAt={Number(params.get('t')) || 0} fileId={Number(params.get('file')) || 0} />
}

/** Whether this browser can play the file as-is (no server work at all). */
function canDirectPlay(f: MediaFile, video: HTMLVideoElement): boolean {
  if (f.hdr) return false
  const mime = ({ mp4: 'video/mp4', m4v: 'video/mp4', mov: 'video/mp4', webm: 'video/webm' } as Record<string, string>)[f.container]
  const v = ({ h264: 'avc1.640028', hevc: 'hvc1.1.6.L120.90', vp9: 'vp09.00.10.08', av1: 'av01.0.08M.08' } as Record<string, string>)[f.videoCodec]
  const a = ({ aac: 'mp4a.40.2', mp3: 'mp3', opus: 'opus', flac: 'flac', vorbis: 'vorbis' } as Record<string, string>)[f.audioCodec]
  if (!mime || !v || (f.audioCodec && !a)) return false
  return video.canPlayType(`${mime}; codecs="${v}${a ? `,${a}` : ''}"`) !== ''
}

interface Cue {
  start: number
  end: number
  text: string
}

function parseVtt(text: string): Cue[] {
  const toSec = (t: string) => {
    const p = t.trim().split(':').map(Number)
    return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : p[0] * 60 + p[1]
  }
  const cues: Cue[] = []
  for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
    const lines = block.split('\n')
    const i = lines.findIndex((l) => l.includes('-->'))
    if (i < 0) continue
    const [a, b] = lines[i].split('-->')
    const body = lines.slice(i + 1).join('\n').replace(/<[^>]+>/g, '').trim()
    if (body) cues.push({ start: toSec(a), end: toSec(b.trim().split(' ')[0]), text: body })
  }
  return cues
}

function streamLabel(s: MediaStream): string {
  const bits = [languageName(s.language)]
  if (s.type === 'audio') bits.push(`${s.codec.toUpperCase()} ${channelLabel(s.channels ?? 2)}`)
  if (s.title) bits.push(s.title)
  if (s.forced) bits.push('Forced')
  return bits.join(' · ')
}

function PlayerView({ detail, startAt, fileId }: { detail: ItemDetail; startAt: number; fileId: number }) {
  const { item, files = [] } = detail
  const file = files.find((f) => f.id === fileId) ?? files[0]
  const nav = useNavigate()

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const offsetRef = useRef(0) // file time at video time 0 (transcodes start mid-file)
  const transcodeRef = useRef<string | null>(null)
  const startedRef = useRef(false)
  const playSession = useRef(Math.random().toString(36).slice(2) + Date.now().toString(36))
  const hideTimer = useRef<number>()

  const [mode, setMode] = useState<'direct' | 'transcode'>('direct')
  const [quality, setQuality] = useState(0) // max height; 0 = original
  const [audio, setAudio] = useState(-1) // stream index; -1 = file default
  const [subIndex, setSubIndex] = useState(-1)
  const [cues, setCues] = useState<Cue[]>([])
  const [time, setTime] = useState(startAt)
  const [paused, setPaused] = useState(true)
  const [buffering, setBuffering] = useState(true)
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('bb.volume') ?? 1))
  const [muted, setMuted] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [controls, setControls] = useState(true)
  const [menu, setMenu] = useState<null | 'settings'>(null)
  const [error, setError] = useState('')
  const [upNext, setUpNext] = useState<Item | null>(null)
  const [countdown, setCountdown] = useState(10)

  const duration = file?.duration || (item.runtime ?? 0) * 60
  const position = () => offsetRef.current + (videoRef.current?.currentTime ?? 0)
  const audioStreams = file?.streams.filter((s) => s.type === 'audio') ?? []
  const subStreams = file?.streams.filter((s) => s.type === 'subtitle' && TEXT_SUBS.has(s.codec)) ?? []

  const stopTranscode = useCallback(() => {
    const id = transcodeRef.current
    transcodeRef.current = null
    if (id) api.del(`/api/transcode/${id}`, { keepalive: true }).catch(() => undefined)
  }, [])

  const report = useCallback(
    (stopped = false) => {
      const video = videoRef.current
      if (!video || !file || !startedRef.current) return
      api
        .post(
          '/api/progress',
          {
            playSessionId: playSession.current,
            itemId: item.id,
            fileId: file.id,
            position: video.ended ? duration : position(),
            duration,
            paused: video.paused,
            stopped,
            mode,
            transcodeId: transcodeRef.current ?? '',
            client: 'Web',
          },
          { keepalive: true },
        )
        .catch(() => undefined)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [file, item.id, duration, mode],
  )

  const load = useCallback(
    async (at: number, q: number, a: number) => {
      const video = videoRef.current
      if (!video || !file) return
      setError('')
      setBuffering(true)
      hlsRef.current?.destroy()
      hlsRef.current = null
      if (q === 0 && a < 0 && canDirectPlay(file, video)) {
        stopTranscode()
        offsetRef.current = 0
        setMode('direct')
        video.src = `/api/files/${file.id}/direct`
        video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
      } else {
        try {
          const res = await api.post<{ session: { id: string }; playlist: string }>(`/api/files/${file.id}/transcode`, {
            start: at,
            maxHeight: q,
            audioStream: a,
            replace: transcodeRef.current ?? '',
          })
          transcodeRef.current = res.session.id
          offsetRef.current = at
          setMode('transcode')
          if (Hls.isSupported()) {
            const hls = new Hls({ startPosition: 0, maxBufferLength: 60, backBufferLength: 120 })
            hls.on(Hls.Events.ERROR, (_e, d) => {
              if (!d.fatal) return
              if (d.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad()
              else if (d.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError()
              else setError('Playback failed. The server could not convert this file.')
            })
            hls.loadSource(res.playlist)
            hls.attachMedia(video)
            hlsRef.current = hls
          } else {
            video.src = res.playlist // Safari plays HLS natively
          }
        } catch (e) {
          setError((e as Error).message)
          return
        }
      }
      video.play().catch(() => setPaused(true))
    },
    [file, stopTranscode],
  )

  // First load, and cleanup when leaving the player.
  useEffect(() => {
    if (!file) return
    load(startAt, 0, -1)
    // Closing the tab or reloading skips React cleanup; pagehide still fires.
    const onHide = () => {
      report(true)
      stopTranscode()
    }
    window.addEventListener('pagehide', onHide)
    return () => {
      window.removeEventListener('pagehide', onHide)
      report(true)
      hlsRef.current?.destroy()
      stopTranscode()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const t = window.setInterval(() => report(), 10000)
    return () => window.clearInterval(t)
  }, [report])

  useEffect(() => {
    if (videoRef.current) {
      videoRef.current.volume = volume
      videoRef.current.muted = muted
    }
    localStorage.setItem('bb.volume', String(volume))
  }, [volume, muted])

  useEffect(() => {
    if (subIndex < 0 || !file) return setCues([])
    let cancelled = false
    fetch(`/api/files/${file.id}/subtitles/${subIndex}.vtt`, { credentials: 'same-origin' })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.statusText))))
      .then((t) => !cancelled && setCues(parseVtt(t)))
      .catch(() => !cancelled && setError('Could not load subtitles'))
    return () => {
      cancelled = true
    }
  }, [subIndex, file])

  const seek = (t: number) => {
    const video = videoRef.current
    if (!video) return
    t = Math.max(0, Math.min(t, Math.max(duration - 1, 0)))
    setTime(t)
    if (mode === 'direct') {
      video.currentTime = t
      return
    }
    const local = t - offsetRef.current
    const s = video.seekable
    if (local >= 0 && s.length && local <= s.end(s.length - 1)) video.currentTime = local
    else load(t, quality, audio) // outside what has been transcoded: restart there
  }

  const togglePlay = () => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) v.play().catch(() => undefined)
    else v.pause()
  }

  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen()
    else containerRef.current?.requestFullscreen().catch(() => undefined)
  }

  const poke = useCallback(() => {
    setControls(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => {
      if (!videoRef.current?.paused) {
        setControls(false)
        setMenu(null)
      }
    }, 3000)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).tagName === 'SELECT') return
      const actions: Record<string, () => void> = {
        ' ': togglePlay,
        k: togglePlay,
        ArrowLeft: () => seek(position() - 10),
        ArrowRight: () => seek(position() + 10),
        j: () => seek(position() - 10),
        l: () => seek(position() + 30),
        f: toggleFullscreen,
        m: () => setMuted((m) => !m),
        ArrowUp: () => setVolume((v) => Math.min(1, v + 0.1)),
        ArrowDown: () => setVolume((v) => Math.max(0, v - 0.1)),
      }
      const fn = actions[e.key]
      if (fn) {
        e.preventDefault()
        fn()
        poke()
      }
    }
    const onFs = () => setFullscreen(!!document.fullscreenElement)
    window.addEventListener('keydown', onKey)
    document.addEventListener('fullscreenchange', onFs)
    return () => {
      window.removeEventListener('keydown', onKey)
      document.removeEventListener('fullscreenchange', onFs)
    }
  })

  const onEnded = async () => {
    report()
    if (item.kind !== 'episode') return nav(`/item/${item.id}`, { replace: true })
    try {
      const next = await api.get<Item | undefined>(`/api/items/${item.id}/next`)
      if (next) {
        setCountdown(10)
        setUpNext(next)
      } else nav(`/item/${item.showId}`, { replace: true })
    } catch {
      nav(-1)
    }
  }

  useEffect(() => {
    if (!upNext) return
    if (countdown <= 0) {
      nav(`/play/${upNext.id}`, { replace: true })
      return
    }
    const t = window.setTimeout(() => setCountdown((c) => c - 1), 1000)
    return () => window.clearTimeout(t)
  }, [upNext, countdown, nav])

  const changeQuality = (q: number) => {
    setQuality(q)
    setMenu(null)
    load(position(), q, audio)
  }
  const changeAudio = (a: number) => {
    const def = audioStreams.find((s) => s.default) ?? audioStreams[0]
    const next = def && def.index === a ? -1 : a
    setAudio(next)
    setMenu(null)
    load(position(), quality, next)
  }

  if (!file) return <div className="player"><div className="player-error">This item has no playable file.</div></div>

  const qualities = [0, 2160, 1080, 720, 480].filter((q) => q === 0 || q < file.height)
  const activeCue = cues.find((c) => time >= c.start && time <= c.end)
  const pct = duration ? (time / duration) * 100 : 0
  const title = item.kind === 'episode' ? item.showTitle : item.title
  const subtitle = item.kind === 'episode' ? `${episodeLabel(item)} · ${item.title}` : item.year ? String(item.year) : ''

  return (
    <div
      ref={containerRef}
      className={`player ${controls || paused ? 'show-controls' : ''}`}
      onMouseMove={poke}
      onTouchStart={poke}
    >
      <video
        ref={videoRef}
        className="player-video"
        playsInline
        onClick={togglePlay}
        onDoubleClick={toggleFullscreen}
        onPlay={() => setPaused(false)}
        onPause={() => {
          setPaused(true)
          report()
        }}
        onPlaying={() => {
          startedRef.current = true
          setBuffering(false)
        }}
        onWaiting={() => setBuffering(true)}
        onTimeUpdate={() => setTime(position())}
        onEnded={onEnded}
        onError={() => mode === 'direct' && load(position(), quality, audio === -1 ? (audioStreams[0]?.index ?? -1) : audio)}
      />

      {activeCue && <div className="subtitle-cue">{activeCue.text}</div>}
      {buffering && !error && <div className="spinner" />}
      {error && <div className="player-error">{error}</div>}

      <div className="player-top">
        <button className="icon-btn" onClick={() => nav(-1)} aria-label="Back">
          <Icon name="back" size={28} />
        </button>
        <div>
          <div className="player-title">{title}</div>
          <div className="player-sub">{subtitle}</div>
        </div>
      </div>

      <div className="player-bottom" onClick={(e) => e.stopPropagation()}>
        <div className="seek">
          <input
            type="range"
            min={0}
            max={duration || 1}
            step={1}
            value={Math.min(time, duration)}
            onChange={(e) => seek(Number(e.target.value))}
            style={{ '--pct': `${pct}%` } as React.CSSProperties}
            aria-label="Seek"
          />
        </div>
        <div className="player-controls">
          <button className="icon-btn" onClick={togglePlay} aria-label={paused ? 'Play' : 'Pause'}>
            <Icon name={paused ? 'play' : 'pause'} fill size={28} />
          </button>
          <button className="icon-btn" onClick={() => seek(position() - 10)} aria-label="Back 10 seconds">
            <Icon name="replay10" size={24} />
          </button>
          <button className="icon-btn" onClick={() => seek(position() + 30)} aria-label="Forward 30 seconds">
            <Icon name="forward30" size={24} />
          </button>
          <div className="volume">
            <button className="icon-btn" onClick={() => setMuted((m) => !m)} aria-label={muted ? 'Unmute' : 'Mute'}>
              <Icon name={muted || volume === 0 ? 'mute' : 'volume'} size={22} />
            </button>
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              onChange={(e) => {
                setMuted(false)
                setVolume(Number(e.target.value))
              }}
              style={{ '--pct': `${(muted ? 0 : volume) * 100}%` } as React.CSSProperties}
              aria-label="Volume"
            />
          </div>
          <div className="time">
            {formatClock(time)} / {formatClock(duration)}
          </div>
          <div className="spacer" />
          <span className={`mode-pill ${mode}`}>{mode === 'direct' ? 'Direct Play' : quality ? `${resolutionLabel(quality)} Transcode` : 'Transcode'}</span>
          <div className="menu-anchor">
            <button className="icon-btn" onClick={() => setMenu(menu ? null : 'settings')} aria-label="Playback settings">
              <Icon name="sliders" size={22} />
            </button>
            {menu === 'settings' && (
              <div className="menu player-menu">
                <div className="menu-section">Quality</div>
                {qualities.map((q) => (
                  <button key={q} className={q === quality ? 'active' : ''} onClick={() => changeQuality(q)}>
                    {q === 0 ? `Original (${resolutionLabel(file.height)})` : resolutionLabel(q)}
                  </button>
                ))}
                {audioStreams.length > 1 && <div className="menu-section">Audio</div>}
                {audioStreams.length > 1 &&
                  audioStreams.map((s) => {
                    const active = audio === s.index || (audio === -1 && s === (audioStreams.find((x) => x.default) ?? audioStreams[0]))
                    return (
                      <button key={s.index} className={active ? 'active' : ''} onClick={() => changeAudio(s.index)}>
                        {streamLabel(s)}
                      </button>
                    )
                  })}
                {subStreams.length > 0 && <div className="menu-section">Subtitles</div>}
                {subStreams.length > 0 && (
                  <button className={subIndex === -1 ? 'active' : ''} onClick={() => { setSubIndex(-1); setMenu(null) }}>
                    Off
                  </button>
                )}
                {subStreams.map((s) => (
                  <button key={s.index} className={subIndex === s.index ? 'active' : ''} onClick={() => { setSubIndex(s.index); setMenu(null) }}>
                    {streamLabel(s)}
                  </button>
                ))}
              </div>
            )}
          </div>
          <button className="icon-btn" onClick={toggleFullscreen} aria-label="Fullscreen">
            <Icon name={fullscreen ? 'exitFullscreen' : 'fullscreen'} size={22} />
          </button>
        </div>
      </div>

      {upNext && (
        <div className="up-next">
          {img(upNext.backdrop, 'w500') && <img src={img(upNext.backdrop, 'w500')} alt="" />}
          <div>
            <div className="muted small">Up next in {countdown}s</div>
            <div className="up-next-title">{episodeLabel(upNext)} · {upNext.title}</div>
            <div className="actions">
              <button className="btn btn-primary" onClick={() => nav(`/play/${upNext.id}`, { replace: true })}>
                <Icon name="play" fill size={16} /> Play now
              </button>
              <button className="btn btn-glass" onClick={() => nav(`/item/${item.showId}`, { replace: true })}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
