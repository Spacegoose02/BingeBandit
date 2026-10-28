import Hls from 'hls.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type ApiError } from '../api'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { guideEpisode } from '../format'
import type { LiveChannel } from '../types'
import { ChannelLogo, channelNo, clock } from './LiveGuide'
import { ScoreBug } from '../components/ScoreBug'
import { useGameLights } from '../lights'
import type { Prefs } from '../types'

/** Full-screen live channel player with channel up/down and a mini guide. */
export default function LivePlayer() {
  const { id } = useParams()
  const nav = useNavigate()
  const { data: channels, reload } = useApi<LiveChannel[]>('/api/live/channels', 60000)
  const channel = channels?.find((c) => String(c.id) === id)
  useDocumentTitle(channel ? `${channelNo(channel.number)} ${channel.name}` : 'Live TV')

  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const hideTimer = useRef<number>()
  const failures = useRef(0)
  const viewerRef = useRef('')
  const stallSecs = useRef(12)
  const [attempt, setAttempt] = useState(0) // bumped to rejoin after a failure
  const [error, setError] = useState('')
  const [reconnecting, setReconnecting] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [paused, setPaused] = useState(false)
  const [behind, setBehind] = useState(0) // seconds behind live
  const [guide, setGuide] = useState(() => localStorage.getItem('bb.miniGuide') === '1')
  const [controls, setControls] = useState(true)
  const [muted, setMuted] = useState(false)
  const [menu, setMenu] = useState(false)
  const [toast, setToast] = useState('')
  const [, tick] = useState(0)
  const lights = useGameLights()
  const { data: prefs, reload: reloadPrefs } = useApi<Prefs>('/api/me/prefs')
  const [bugKey, setBugKey] = useState(0) // remounts the score bug after its switch changes

  // A new channel starts with a clean slate.
  useEffect(() => {
    failures.current = 0
    setError('')
  }, [id])

  // Start (or switch) the stream whenever the channel changes, and rejoin
  // after a failure.
  useEffect(() => {
    if (!id) return
    const video = videoRef.current
    if (!video) return
    let hls: Hls | null = null
    let viewer = ''
    let cancelled = false
    let retryTimer = 0
    let ping = 0
    setBuffering(true)
    setPaused(false)
    setBehind(0)
    // Rejoin on its own instead of leaving an error up: streams drop, providers
    // hiccup and the server restarts the conversion. Quick retries first, then
    // every 15 s while the error shows.
    const fail = (msg: string) => {
      if (cancelled) return
      cancelled = true
      hls?.destroy()
      hls = null
      hlsRef.current = null
      failures.current++
      if (failures.current > 3) setError(msg)
      setReconnecting(true)
      retryTimer = window.setTimeout(() => setAttempt((n) => n + 1), failures.current > 3 ? 15000 : 1500 * failures.current)
    }
    api
      .post<{ viewerId: string; playlist: string; bufferSecs?: number; stallSecs?: number }>(`/api/live/channels/${id}/watch`)
      .then((res) => {
        if (cancelled) {
          api.del(`/api/live/viewers/${res.viewerId}`).catch(() => undefined)
          return
        }
        viewer = res.viewerId
        viewerRef.current = viewer
        stallSecs.current = res.stallSecs ?? 12
        // Paused players stop fetching the playlist; this keeps the seat.
        ping = window.setInterval(() => {
          api.post(`/api/live/viewers/${viewer}/ping`).catch((e: ApiError) => e.status === 410 && fail('This channel stopped.'))
        }, 10000)
        if (Hls.isSupported()) {
          // Start behind live (the admin's buffer setting, in 2 s segments) so short
          // upstream hiccups don't stall playback; the server keeps a longer
          // window for pausing and rewinding.
          const segs = Math.max(2, Math.ceil((res.bufferSecs ?? 8) / 2))
          hls = new Hls({ liveSyncDurationCount: segs, manifestLoadingTimeOut: 30000, manifestLoadingMaxRetry: 6, levelLoadingMaxRetry: 6, fragLoadingMaxRetry: 6 })
          hlsRef.current = hls
          hls.on(Hls.Events.ERROR, (_e, d) => {
            if (!d.fatal) return
            if (d.type === Hls.ErrorTypes.NETWORK_ERROR) {
              const status = (d.response as { code?: number } | undefined)?.code
              if (status === 503 || status === 410 || status === 404) fail('This channel is unavailable right now.')
              else hls?.startLoad()
            } else if (d.type === Hls.ErrorTypes.MEDIA_ERROR) hls?.recoverMediaError()
            else fail('Playback failed.')
          })
          hls.loadSource(res.playlist)
          hls.attachMedia(video)
        } else {
          video.src = res.playlist
        }
        video.play().catch(() => undefined)
      })
      .catch((e) => fail((e as Error).message || 'This channel is unavailable right now.'))
    const leave = () => viewer && api.del(`/api/live/viewers/${viewer}`, { keepalive: true }).catch(() => undefined)
    window.addEventListener('pagehide', leave)
    return () => {
      cancelled = true
      window.clearTimeout(retryTimer)
      window.clearInterval(ping)
      window.removeEventListener('pagehide', leave)
      hls?.destroy()
      hlsRef.current = null
      viewerRef.current = ''
      leave()
    }
  }, [id, attempt])

  // Buffering for longer than the server's stall time: tell it, so it can
  // move the channel to its next stream (it checks the stream really is the
  // problem first). If it didn't switch, the stream is fine and the player is
  // what's stuck, so start the player over; if it did, give the new stream
  // one more stall period before doing the same.
  useEffect(() => {
    if (!buffering || paused || reconnecting) return
    const started = Date.now()
    let next = stallSecs.current
    let switched = false
    let done = false
    const reload = () => {
      if (done) return
      done = true
      setAttempt((n) => n + 1)
    }
    const t = window.setInterval(() => {
      const secs = (Date.now() - started) / 1000
      if (secs < next || !viewerRef.current) return
      next += stallSecs.current
      if (switched) return reload()
      api
        .post<{ switched?: boolean }>(`/api/live/viewers/${viewerRef.current}/buffering`, { seconds: Math.round(secs) })
        .then((r) => (r?.switched ? (switched = true) : reload()), () => undefined)
    }, 1000)
    return () => {
      done = true
      window.clearInterval(t)
    }
  }, [buffering, paused, reconnecting, attempt])

  const onPlaying = () => {
    setBuffering(false)
    setReconnecting(false)
    setError('')
    failures.current = 0
  }

  // How far behind live we are, for the badge and the "Go live" button.
  const liveEdge = (): number | null => {
    const video = videoRef.current
    if (!video) return null
    const sync = hlsRef.current?.liveSyncPosition
    if (sync) return sync
    return video.seekable.length ? video.seekable.end(video.seekable.length - 1) : null
  }
  const onTimeUpdate = () => {
    const video = videoRef.current
    const edge = liveEdge()
    if (!video || edge == null) return
    const b = Math.max(0, edge - video.currentTime)
    setBehind(b < 4 ? 0 : Math.round(b))
  }

  const togglePause = useCallback(() => {
    const video = videoRef.current
    if (!video) return
    if (video.paused) video.play().catch(() => undefined)
    else video.pause()
  }, [])

  const seekBy = useCallback((secs: number) => {
    const video = videoRef.current
    if (!video || !video.seekable.length) return
    const start = video.seekable.start(0)
    const end = liveEdge() ?? video.seekable.end(video.seekable.length - 1)
    video.currentTime = Math.min(end, Math.max(start + 1, video.currentTime + secs))
  }, [])

  const goLive = useCallback(() => {
    const video = videoRef.current
    const edge = liveEdge()
    if (video && edge != null) video.currentTime = edge
    video?.play().catch(() => undefined)
  }, [])

  useEffect(() => {
    const t = window.setInterval(() => tick((n) => n + 1), 15000) // progress bar
    return () => window.clearInterval(t)
  }, [])
  useEffect(() => localStorage.setItem('bb.miniGuide', guide ? '1' : '0'), [guide])
  useEffect(() => {
    if (videoRef.current) videoRef.current.muted = muted
  }, [muted])

  const step = useCallback(
    (dir: 1 | -1) => {
      if (!channels?.length) return
      const i = channels.findIndex((c) => String(c.id) === id)
      const next = channels[(i + dir + channels.length) % channels.length]
      nav(`/live/${next.id}`, { replace: true })
    },
    [channels, id, nav],
  )

  const poke = useCallback(() => {
    setControls(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => setControls(false), 4000)
  }, [])
  // Show the controls when a channel starts, then let them hide like any other time.
  useEffect(() => {
    poke()
    return () => window.clearTimeout(hideTimer.current)
  }, [id, poke])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const map: Record<string, () => void> = {
        ArrowUp: () => step(-1),
        PageUp: () => step(-1),
        ArrowDown: () => step(1),
        PageDown: () => step(1),
        g: () => setGuide((g) => !g),
        m: () => setMuted((m) => !m),
        ' ': togglePause,
        k: togglePause,
        ArrowLeft: () => seekBy(-10),
        ArrowRight: () => seekBy(30),
        l: goLive,
        f: () => (document.fullscreenElement ? document.exitFullscreen() : containerRef.current?.requestFullscreen()),
        o: () => setMenu((m) => !m),
        Escape: () => (menu ? setMenu(false) : !document.fullscreenElement && nav('/live')),
      }
      if (map[e.key]) {
        e.preventDefault()
        map[e.key]()
        poke()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [step, poke, nav, togglePause, seekBy, goLive, menu])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(''), 3000)
    return () => window.clearTimeout(t)
  }, [toast])

  // A new channel brings its own sound and caption tracks.
  useEffect(() => setMenu(false), [id])
  const audioTracks = menu ? (hlsRef.current?.audioTracks ?? []) : []
  const textTracks = menu && videoRef.current ? Array.from(videoRef.current.textTracks).filter((t) => t.kind === 'captions' || t.kind === 'subtitles') : []
  const pickAudio = (i: number) => {
    if (hlsRef.current) hlsRef.current.audioTrack = i
    tick((n) => n + 1)
  }
  const pickText = (t: TextTrack | null) => {
    for (const x of textTracks) x.mode = x === t ? 'showing' : 'disabled'
    tick((n) => n + 1)
  }
  const setScoreBug = async (on: boolean) => {
    if (!prefs) return
    try {
      await api.put('/api/me/prefs', { ...prefs, scoreBug: on })
      reloadPrefs()
      setBugKey((k) => k + 1)
    } catch (e) {
      setToast((e as Error).message)
    }
  }

  const now = Date.now() / 1000
  const prog = channel?.now
  const pct = prog ? Math.min(100, ((now - prog.start) / (prog.stop - prog.start)) * 100) : 0

  const toggleFav = async () => {
    if (!channel) return
    await api[channel.favorite ? 'del' : 'post'](`/api/live/channels/${channel.id}/favorite`)
    reload()
  }

  return (
    <div ref={containerRef} className={`live-player ${guide ? 'with-guide' : ''} ${controls || menu ? 'show-controls' : ''}`} onMouseMove={poke}>
      <div className="live-stage">
        <ScoreBug key={bugKey} channelId={Number(id)} />
        <video
          ref={videoRef}
          className="player-video"
          playsInline
          autoPlay
          onPlaying={onPlaying}
          onWaiting={() => setBuffering(true)}
          onPause={() => setPaused(true)}
          onPlay={() => setPaused(false)}
          onTimeUpdate={onTimeUpdate}
          onClick={() => setGuide((g) => !g)}
        />
        {(buffering || reconnecting) && !error && !paused && <div className="spinner" />}
        {error && (
          <div className="player-error">
            <div>{error}</div>
            <div className="muted small">Retrying on its own. You can also pick another channel.</div>
            <button className="btn btn-glass btn-sm" onClick={() => setError('')}>Dismiss</button>
          </div>
        )}

        <div className="player-top">
          <button className="icon-btn" onClick={() => nav('/live')} aria-label="Back to the guide">
            <Icon name="back" size={28} />
          </button>
          {channel && (
            <div className="live-info">
              <ChannelLogo channel={channel} size={36} />
              <div>
                <div className="player-title">
                  <span className="ch-num">{channelNo(channel.number)}</span> {channel.name}
                </div>
                <div className="player-sub">
                  {prog ? `${prog.title}${prog.year ? ` (${prog.year})` : ''}${prog.subtitle ? ` — ${prog.subtitle}` : ''} · ${clock(prog.start)}–${clock(prog.stop)}` : 'Live'}
                  {prog && [prog.rating, prog.genres || prog.category, prog.episode && guideEpisode(prog.episode), prog.stars && `★ ${prog.stars}`].filter(Boolean).map((f) => ` · ${f}`).join('')}
                  {channel.next && ` · Next: ${channel.next.title} at ${clock(channel.next.start)}`}
                </div>
                {prog?.description && <div className="live-desc" title={prog.description}>{prog.description}</div>}
                {prog && (
                  <div className="progress inline live-progress">
                    <div style={{ width: `${pct}%` }} />
                  </div>
                )}
              </div>
            </div>
          )}
          <div className="spacer" />
          <button className={`live-badge ${behind ? 'behind' : ''}`} onClick={goLive} title={behind ? 'Jump to live (L)' : 'Watching live'}>
            {behind ? `−${fmtBehind(behind)} · GO LIVE` : 'LIVE'}
          </button>
        </div>

        <div className="live-controls">
          <button className="icon-btn" onClick={() => step(-1)} aria-label="Previous channel"><Icon name="arrowUp" size={22} /></button>
          <button className="icon-btn" onClick={() => step(1)} aria-label="Next channel"><Icon name="arrowDown" size={22} /></button>
          <button className="icon-btn" onClick={() => seekBy(-10)} aria-label="Back 10 seconds"><Icon name="replay10" size={22} /></button>
          <button className="icon-btn" onClick={togglePause} aria-label={paused ? 'Play' : 'Pause'}><Icon name={paused ? 'play' : 'pause'} size={22} /></button>
          <button className="icon-btn" onClick={() => seekBy(30)} aria-label="Forward 30 seconds"><Icon name="forward30" size={22} /></button>
          <button className="icon-btn" onClick={() => setMuted((m) => !m)} aria-label={muted ? 'Unmute' : 'Mute'}>
            <Icon name={muted ? 'mute' : 'volume'} size={22} />
          </button>
          {channel && (
            <button className="icon-btn" onClick={toggleFav} aria-label={channel.favorite ? 'Remove favorite' : 'Add favorite'}>
              <Icon name="star" fill={channel.favorite} size={20} className={channel.favorite ? 'accent' : ''} />
            </button>
          )}
          <div className="spacer" />
          {toast && <span className="muted small">{toast}</span>}
          <div className="menu-anchor">
            <button className={`icon-btn ${menu ? 'active' : ''}`} onClick={() => setMenu((m) => !m)} aria-label="Options" title="Options (O)">
              <Icon name="sliders" size={22} />
            </button>
            {menu && (
              <div className="menu player-menu">
                {lights.person && (
                  <>
                    <div className="menu-section">Game Lights</div>
                    {([true, false] as const).map((run) => (
                      <button
                        key={String(run)}
                        className={!lights.person?.off === run ? 'active' : ''}
                        onClick={() => lights.setRunning(run).then(() => setToast(run ? 'Game Lights on' : 'Game Lights stopped'), (e) => setToast(e.message))}
                      >
                        {run ? 'On' : 'Stopped'}
                      </button>
                    ))}
                  </>
                )}
                {prefs && (
                  <>
                    <div className="menu-section">Score bug</div>
                    <button className={prefs.scoreBug ? 'active' : ''} onClick={() => setScoreBug(true)}>
                      On
                    </button>
                    <button className={!prefs.scoreBug ? 'active' : ''} onClick={() => setScoreBug(false)}>
                      Off
                    </button>
                  </>
                )}
                {audioTracks.length > 1 && <div className="menu-section">Audio</div>}
                {audioTracks.length > 1 &&
                  audioTracks.map((t, i) => (
                    <button key={i} className={hlsRef.current?.audioTrack === i ? 'active' : ''} onClick={() => pickAudio(i)}>
                      {t.name || t.lang || `Track ${i + 1}`}
                    </button>
                  ))}
                {textTracks.length > 0 && (
                  <>
                    <div className="menu-section">Captions</div>
                    <button className={textTracks.every((t) => t.mode !== 'showing') ? 'active' : ''} onClick={() => pickText(null)}>
                      Off
                    </button>
                    {textTracks.map((t, i) => (
                      <button key={i} className={t.mode === 'showing' ? 'active' : ''} onClick={() => pickText(t)}>
                        {t.label || t.language || `Captions ${i + 1}`}
                      </button>
                    ))}
                  </>
                )}
                <div className="menu-section">Guide</div>
                <button className={guide ? 'active' : ''} onClick={() => setGuide((g) => !g)}>
                  Mini guide {guide ? 'on' : 'off'}
                </button>
              </div>
            )}
          </div>
          <button className={`btn btn-glass btn-sm ${guide ? 'active' : ''}`} onClick={() => setGuide((g) => !g)}>
            <Icon name="list" size={16} /> Guide
          </button>
          <button
            className="icon-btn"
            onClick={() => (document.fullscreenElement ? document.exitFullscreen() : containerRef.current?.requestFullscreen())}
            aria-label="Fullscreen"
          >
            <Icon name="fullscreen" size={22} />
          </button>
        </div>
      </div>

      {guide && (
        <div className="mini-guide">
          {channels?.map((c) => {
            const p = c.now
            const cpct = p ? Math.min(100, ((now - p.start) / (p.stop - p.start)) * 100) : 0
            return (
              <button key={c.id} className={`mini-ch ${String(c.id) === id ? 'current' : ''}`} onClick={() => nav(`/live/${c.id}`, { replace: true })}>
                <div className="mini-ch-head">
                  <span className="ch-num">{channelNo(c.number)}</span>
                  <ChannelLogo channel={c} size={24} />
                  <span className="ch-name">{c.name}</span>
                </div>
                <div className="mini-ch-now">{p?.title ?? 'No guide information'}</div>
                {p && (
                  <div className="progress inline">
                    <div style={{ width: `${cpct}%` }} />
                  </div>
                )}
                {c.next && <div className="muted small">Next: {c.next.title}</div>}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

function fmtBehind(secs: number): string {
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
