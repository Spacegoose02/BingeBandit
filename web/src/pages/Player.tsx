import Hls from 'hls.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { channelLabel, episodeLabel, formatClock, languageName, normLang, resolutionLabel } from '../format'
import type { Item, ItemDetail, MediaFile, MediaStream, Prefs } from '../types'

const TEXT_SUBS = new Set(['subrip', 'srt', 'ass', 'ssa', 'webvtt', 'mov_text', 'text'])
const IMAGE_SUBS = new Set(['hdmv_pgs_subtitle', 'dvd_subtitle', 'dvb_subtitle', 'xsub'])
const LADDER = [2160, 1080, 720, 480]

export default function Player() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const { data, error } = useApi<ItemDetail>(`/api/items/${id}`)
  const { data: prefs } = useApi<Prefs>('/api/me/prefs')
  useDocumentTitle(data ? (data.item.kind === 'episode' ? `${data.item.showTitle} ${episodeLabel(data.item)}` : data.item.title) : 'Player')
  if (error) return <div className="player"><div className="player-error">{error}</div></div>
  if (!data || !prefs) return <div className="player"><div className="spinner" /></div>
  return (
    <PlayerView
      key={id}
      detail={data}
      prefs={prefs}
      startAt={Number(params.get('t')) || 0}
      fileId={Number(params.get('file')) || 0}
      playlistId={Number(params.get('playlist')) || 0}
    />
  )
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

interface Choice {
  quality: number // max height; 0 = original
  audio: number // stream index; -1 = file default
  burn: number // image subtitle stream burned in by the server; -1 = none
}

/**
 * Picks audio and subtitles from the user's preferences. Returns the audio
 * stream (-1 when it's the file default, so direct play stays possible) and
 * the subtitle stream, which is either rendered by us or burned in.
 */
function pickTracks(f: MediaFile, p: Prefs): { audio: number; sub?: MediaStream } {
  const audios = f.streams.filter((s) => s.type === 'audio')
  const fileDefault = audios.find((s) => s.default) ?? audios[0]
  let chosen = fileDefault
  if (p.audioLanguage) chosen = audios.find((s) => normLang(s.language) === normLang(p.audioLanguage)) ?? fileDefault
  const audio = chosen && chosen !== fileDefault ? chosen.index : -1

  const subs = f.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec)))
  const want = normLang(p.subtitleLanguage)
  const inLang = subs.filter((s) => normLang(s.language) === want)
  // Prefer text over image subtitles and plain over SDH.
  const rank = (s: MediaStream) => (IMAGE_SUBS.has(s.codec) ? 2 : 0) + (s.hearingImpaired ? 1 : 0)
  const best = (list: MediaStream[]) => [...list].sort((a, b) => rank(a) - rank(b))[0]
  const full = best(inLang.filter((s) => !s.forced)) ?? best(subs.filter((s) => !s.language && !s.forced))
  const forced = best(inLang.filter((s) => s.forced))

  const foreignAudio = !!chosen?.language && normLang(chosen.language) !== want
  let sub: MediaStream | undefined
  switch (p.subtitleMode) {
    case 'always':
      sub = full ?? forced
      break
    case 'auto':
      sub = foreignAudio ? full ?? forced : forced
      break
    case 'forced':
      sub = forced
      break
  }
  return { audio, sub }
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
  if (s.hearingImpaired) bits.push('SDH')
  if (s.external) bits.push('External')
  if (IMAGE_SUBS.has(s.codec)) bits.push('Burned in')
  return bits.join(' · ')
}

function PlayerView({
  detail,
  prefs,
  startAt,
  fileId,
  playlistId,
}: {
  detail: ItemDetail
  prefs: Prefs
  startAt: number
  fileId: number
  playlistId: number // continue through this playlist instead of the show
}) {
  const { item, files = [] } = detail
  const file = files.find((f) => f.id === fileId) ?? files[0]
  const nav = useNavigate()
  const initial = useRef(file ? pickTracks(file, prefs) : { audio: -1 })

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const offsetRef = useRef(0) // file time at video time 0 (transcodes start mid-file)
  const transcodeRef = useRef<string | null>(null)
  const startedRef = useRef(false)
  const forceTranscode = useRef(false) // direct play failed once; don't try again
  const settlingRef = useRef(true) // a load or seek is in progress, so waiting isn't a stall
  const stalls = useRef<number[]>([])
  const retries = useRef(0)
  // Seconds actually played (for watch history): small forward steps of
  // currentTime while playing; seeks and pauses add nothing.
  const playedRef = useRef(0)
  const lastTick = useRef<number | null>(null)
  const playSession = useRef(Math.random().toString(36).slice(2) + Date.now().toString(36))
  const hideTimer = useRef<number>()

  const initialSub = initial.current.sub
  const [choice, setChoice] = useState<Choice>({
    quality: 0,
    audio: initial.current.audio,
    burn: initialSub && IMAGE_SUBS.has(initialSub.codec) ? initialSub.index : -1,
  })
  const choiceRef = useRef(choice)
  choiceRef.current = choice
  const [mode, setMode] = useState<'direct' | 'transcode'>('direct')
  const [subIndex, setSubIndex] = useState(initialSub && TEXT_SUBS.has(initialSub.codec) ? initialSub.index : -1)
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
  const [toast, setToast] = useState('')
  const [upNext, setUpNext] = useState<Item | null>(null)
  const [countdown, setCountdown] = useState(10)

  const duration = file?.duration || (item.runtime ?? 0) * 60
  const position = () => offsetRef.current + (videoRef.current?.currentTime ?? 0)
  const audioStreams = file?.streams.filter((s) => s.type === 'audio') ?? []
  const subStreams = file?.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec))) ?? []

  const stopTranscode = useCallback(() => {
    const id = transcodeRef.current
    transcodeRef.current = null
    if (id) api.del(`/api/transcode/${id}`, { keepalive: true }).catch(() => undefined)
  }, [])

  const report = useCallback(
    (stopped = false, finished = false) => {
      const video = videoRef.current
      if (!video || !file || !startedRef.current) return
      api
        .post(
          '/api/progress',
          {
            playSessionId: playSession.current,
            itemId: item.id,
            fileId: file.id,
            // Skipping the credits counts as finishing, wherever they start.
            position: video.ended || finished ? duration : position(),
            duration,
            paused: video.paused,
            stopped,
            mode,
            transcodeId: transcodeRef.current ?? '',
            client: 'Web',
            played: Math.round(playedRef.current),
          },
          { keepalive: true },
        )
        .catch(() => undefined)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [file, item.id, duration, mode],
  )

  const load = useCallback(
    async (at: number, c: Choice) => {
      const video = videoRef.current
      if (!video || !file) return
      setError('')
      setBuffering(true)
      settlingRef.current = true
      hlsRef.current?.destroy()
      hlsRef.current = null
      const retry = (why: string) => {
        if (retries.current < 2) {
          retries.current++
          // Resume from where the viewer is, not where this load began.
          const resumeAt = startedRef.current ? offsetRef.current + (video.currentTime || 0) : at
          window.setTimeout(() => load(resumeAt, choiceRef.current), 1500)
        } else setError(why)
      }
      if (c.quality === 0 && c.audio < 0 && c.burn < 0 && !forceTranscode.current && canDirectPlay(file, video)) {
        stopTranscode()
        offsetRef.current = 0
        setMode('direct')
        video.src = `/api/files/${file.id}/direct`
        video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
      } else {
        try {
          const res = await api.post<{ session: { id: string }; playlist: string }>(`/api/files/${file.id}/transcode`, {
            start: at,
            maxHeight: c.quality,
            audioStream: c.audio,
            burnSubtitle: c.burn,
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
              else retry('Playback failed. The server could not convert this file.')
            })
            hls.loadSource(res.playlist)
            hls.attachMedia(video)
            hlsRef.current = hls
          } else {
            video.src = res.playlist // Safari plays HLS natively
          }
        } catch (e) {
          retry((e as Error).message)
          return
        }
      }
      video.play().catch(() => setPaused(true))
    },
    [file, stopTranscode],
  )

  const apply = (c: Choice) => {
    setChoice(c)
    setMenu(null)
    load(position(), c)
  }

  // First load, and cleanup when leaving the player.
  useEffect(() => {
    if (!file) return
    load(startAt, choiceRef.current)
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
    if (!toast) return
    const t = window.setTimeout(() => setToast(''), 6000)
    return () => window.clearTimeout(t)
  }, [toast])

  useEffect(() => {
    if (subIndex < 0 || !file) return setCues([])
    let cancelled = false
    fetch(`/api/files/${file.id}/subtitles/${subIndex}.vtt`, { credentials: 'same-origin' })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.statusText))))
      .then((t) => !cancelled && setCues(parseVtt(t)))
      .catch(() => !cancelled && setToast('Could not load those subtitles'))
    return () => {
      cancelled = true
    }
  }, [subIndex, file])

  const seek = (t: number) => {
    const video = videoRef.current
    if (!video) return
    t = Math.max(0, Math.min(t, Math.max(duration - 1, 0)))
    setTime(t)
    settlingRef.current = true
    if (mode === 'direct') {
      video.currentTime = t
      return
    }
    const local = t - offsetRef.current
    const s = video.seekable
    if (local >= 0 && s.length && local <= s.end(s.length - 1)) video.currentTime = local
    else load(t, choiceRef.current) // outside what has been transcoded: restart there
  }

  // Three stalls within a minute (not caused by seeking) means the
  // connection can't keep up: drop one quality step.
  const onStall = () => {
    setBuffering(true)
    if (!prefs.autoQuality || settlingRef.current || !startedRef.current || !file) return
    const now = Date.now()
    stalls.current = [...stalls.current.filter((t) => now - t < 60000), now]
    if (stalls.current.length < 3) return
    stalls.current = []
    const current = choiceRef.current.quality || file.height
    const lower = LADDER.find((q) => q < current && q < file.height)
    if (!lower) return
    setToast(`Playback kept pausing to buffer, so quality was lowered to ${resolutionLabel(lower)}.`)
    apply({ ...choiceRef.current, quality: lower })
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
    report(false, true)
    const done = playlistId ? `/playlist/${playlistId}` : item.kind === 'episode' ? `/item/${item.showId}` : `/item/${item.id}`
    if (!playlistId && item.kind !== 'episode') return nav(done, { replace: true })
    try {
      const next = await api.get<Item | undefined>(
        playlistId ? `/api/playlists/${playlistId}/next?after=${item.id}` : `/api/items/${item.id}/next`,
      )
      if (next) {
        setCountdown(10)
        setUpNext(next)
      } else nav(done, { replace: true })
    } catch {
      nav(-1)
    }
  }
  const nextUrl = (next: Item) => `/play/${next.id}${playlistId ? `?playlist=${playlistId}` : ''}`

  // Intro and credits markers: offer a button or act automatically, once per
  // visit, so seeking back into the intro doesn't skip it again.
  const intro = detail.markers?.find((m) => m.kind === 'intro')
  const credits = detail.markers?.find((m) => m.kind === 'credits')
  const introDone = useRef(false)
  const creditsDone = useRef(false)
  const inIntro = !!intro && time >= intro.start && time < intro.end - 1
  const inCredits = !!credits && time >= credits.start && time < Math.min(credits.end, duration) - 1 && !upNext
  const hasNext = item.kind === 'episode' || !!playlistId
  const skipIntro = () => {
    introDone.current = true
    if (intro) seek(intro.end)
  }
  const skipCredits = () => {
    creditsDone.current = true
    onEnded()
  }
  useEffect(() => {
    if (inIntro && prefs.skipIntro === 'auto' && !introDone.current) {
      introDone.current = true
      setToast('Skipped the intro')
      skipIntro()
    }
    if (inCredits && prefs.skipCredits === 'auto' && !creditsDone.current && hasNext) skipCredits()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inIntro, inCredits])

  useEffect(() => {
    if (!upNext || !prefs.autoPlayNext) return
    if (countdown <= 0) {
      nav(nextUrl(upNext), { replace: true })
      return
    }
    const t = window.setTimeout(() => setCountdown((c) => c - 1), 1000)
    return () => window.clearTimeout(t)
  }, [upNext, countdown, nav, prefs.autoPlayNext])

  const changeAudio = (a: number) => {
    const def = audioStreams.find((s) => s.default) ?? audioStreams[0]
    apply({ ...choice, audio: def && def.index === a ? -1 : a })
  }

  const changeSubtitle = (s: MediaStream | null) => {
    setMenu(null)
    if (s && IMAGE_SUBS.has(s.codec)) {
      setSubIndex(-1)
      if (choice.burn !== s.index) apply({ ...choice, burn: s.index })
      return
    }
    setSubIndex(s ? s.index : -1)
    if (choice.burn >= 0) apply({ ...choice, burn: -1 })
  }

  if (!file) return <div className="player"><div className="player-error">This item has no playable file.</div></div>

  const qualities = [0, ...LADDER.filter((q) => q < file.height)]
  const activeCue = cues.find((c) => time >= c.start && time <= c.end)
  const pct = duration ? (time / duration) * 100 : 0
  const title = item.kind === 'episode' ? item.showTitle : item.title
  const subtitle = item.kind === 'episode' ? `${episodeLabel(item)} · ${item.title}` : item.year ? String(item.year) : ''
  const effectiveAudio = choice.audio >= 0 ? choice.audio : (audioStreams.find((x) => x.default) ?? audioStreams[0])?.index

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
          settlingRef.current = false
          retries.current = 0
          setBuffering(false)
        }}
        onWaiting={onStall}
        onTimeUpdate={() => {
          const v = videoRef.current
          if (v && !v.paused) {
            const now = v.currentTime
            const step = lastTick.current === null ? 0 : now - lastTick.current
            if (step > 0 && step < 2) playedRef.current += step
            lastTick.current = now
          }
          setTime(position())
        }}
        onSeeking={() => (lastTick.current = null)}
        onEmptied={() => (lastTick.current = null)}
        onEnded={onEnded}
        onError={() => {
          // The browser couldn't decode the file after all: let the server convert it.
          if (mode !== 'direct') return
          forceTranscode.current = true
          load(position(), choiceRef.current)
        }}
      />

      {activeCue && (
        <div className={`subtitle-cue size-${prefs.subtitleSize} ${prefs.subtitleBackground ? 'boxed' : ''}`}>
          <span>{activeCue.text}</span>
        </div>
      )}
      {buffering && !error && <div className="spinner" />}
      {error && <div className="player-error">{error}</div>}
      {toast && <div className="player-toast">{toast}</div>}
      {inIntro && prefs.skipIntro === 'button' && (
        <button className="skip-btn" onClick={skipIntro}>
          Skip Intro <Icon name="next" size={18} />
        </button>
      )}
      {inCredits && prefs.skipCredits === 'button' && (
        <button className="skip-btn" onClick={skipCredits}>
          {hasNext ? 'Next' : 'Skip Credits'} <Icon name="next" size={18} />
        </button>
      )}

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
          <span className={`mode-pill ${mode}`}>
            {mode === 'direct' ? 'Direct Play' : choice.quality ? `${resolutionLabel(choice.quality)} Transcode` : 'Transcode'}
          </span>
          <div className="menu-anchor">
            <button className="icon-btn" onClick={() => setMenu(menu ? null : 'settings')} aria-label="Playback settings">
              <Icon name="sliders" size={22} />
            </button>
            {menu === 'settings' && (
              <div className="menu player-menu">
                <div className="menu-section">Quality</div>
                {qualities.map((q) => (
                  <button key={q} className={q === choice.quality ? 'active' : ''} onClick={() => apply({ ...choice, quality: q })}>
                    {q === 0 ? `Original (${resolutionLabel(file.height)})` : resolutionLabel(q)}
                  </button>
                ))}
                {audioStreams.length > 1 && <div className="menu-section">Audio</div>}
                {audioStreams.length > 1 &&
                  audioStreams.map((s) => (
                    <button key={s.index} className={s.index === effectiveAudio ? 'active' : ''} onClick={() => changeAudio(s.index)}>
                      {streamLabel(s)}
                    </button>
                  ))}
                {subStreams.length > 0 && <div className="menu-section">Subtitles</div>}
                {subStreams.length > 0 && (
                  <button className={subIndex === -1 && choice.burn === -1 ? 'active' : ''} onClick={() => changeSubtitle(null)}>
                    Off
                  </button>
                )}
                {subStreams.map((s) => (
                  <button
                    key={s.index}
                    className={subIndex === s.index || choice.burn === s.index ? 'active' : ''}
                    onClick={() => changeSubtitle(s)}
                  >
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
          {img(upNext.backdrop || upNext.showBackdrop, 'w500') && <img src={img(upNext.backdrop || upNext.showBackdrop, 'w500')} alt="" />}
          <div>
            <div className="muted small">{prefs.autoPlayNext ? `Up next in ${countdown}s` : 'Up next'}</div>
            <div className="up-next-title">
              {upNext.kind === 'episode' ? `${upNext.showTitle} · ${episodeLabel(upNext)} · ${upNext.title}` : upNext.title}
            </div>
            <div className="actions">
              <button className="btn btn-primary" onClick={() => nav(nextUrl(upNext), { replace: true })}>
                <Icon name="play" fill size={16} /> Play now
              </button>
              <button
                className="btn btn-glass"
                onClick={() => nav(playlistId ? `/playlist/${playlistId}` : `/item/${item.showId ?? item.id}`, { replace: true })}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
