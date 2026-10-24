import Hls from 'hls.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { LiveChannel } from '../types'
import { ChannelLogo, channelNo, clock } from './LiveGuide'

/** Full-screen live channel player with channel up/down and a mini guide. */
export default function LivePlayer() {
  const { id } = useParams()
  const nav = useNavigate()
  const { data: channels, reload } = useApi<LiveChannel[]>('/api/live/channels', 60000)
  const channel = channels?.find((c) => String(c.id) === id)
  useDocumentTitle(channel ? `${channelNo(channel.number)} ${channel.name}` : 'Live TV')

  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const hideTimer = useRef<number>()
  const [error, setError] = useState('')
  const [buffering, setBuffering] = useState(true)
  const [guide, setGuide] = useState(() => localStorage.getItem('bb.miniGuide') === '1')
  const [controls, setControls] = useState(true)
  const [muted, setMuted] = useState(false)
  const [, tick] = useState(0)

  // Start (or switch) the stream whenever the channel changes.
  useEffect(() => {
    if (!id) return
    const video = videoRef.current
    if (!video) return
    let hls: Hls | null = null
    let viewer = ''
    let cancelled = false
    setError('')
    setBuffering(true)
    api
      .post<{ viewerId: string; playlist: string }>(`/api/live/channels/${id}/watch`)
      .then((res) => {
        if (cancelled) {
          api.del(`/api/live/viewers/${res.viewerId}`).catch(() => undefined)
          return
        }
        viewer = res.viewerId
        if (Hls.isSupported()) {
          hls = new Hls({ liveSyncDurationCount: 3, maxLiveSyncPlaybackRate: 1.2, manifestLoadingMaxRetry: 6, levelLoadingMaxRetry: 6 })
          hls.on(Hls.Events.ERROR, (_e, d) => {
            if (!d.fatal) return
            if (d.type === Hls.ErrorTypes.NETWORK_ERROR) {
              const status = (d.response as { code?: number } | undefined)?.code
              if (status === 503 || status === 410) setError('This channel is unavailable right now. Try again in a moment.')
              else hls?.startLoad()
            } else if (d.type === Hls.ErrorTypes.MEDIA_ERROR) hls?.recoverMediaError()
            else setError('Playback failed.')
          })
          hls.loadSource(res.playlist)
          hls.attachMedia(video)
        } else {
          video.src = res.playlist
        }
        video.play().catch(() => undefined)
      })
      .catch((e) => !cancelled && setError((e as Error).message))
    const leave = () => viewer && api.del(`/api/live/viewers/${viewer}`, { keepalive: true }).catch(() => undefined)
    window.addEventListener('pagehide', leave)
    return () => {
      cancelled = true
      window.removeEventListener('pagehide', leave)
      hls?.destroy()
      leave()
    }
  }, [id])

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

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const map: Record<string, () => void> = {
        ArrowUp: () => step(-1),
        PageUp: () => step(-1),
        ArrowDown: () => step(1),
        PageDown: () => step(1),
        g: () => setGuide((g) => !g),
        m: () => setMuted((m) => !m),
        f: () => (document.fullscreenElement ? document.exitFullscreen() : containerRef.current?.requestFullscreen()),
        Escape: () => !document.fullscreenElement && nav('/live'),
      }
      if (map[e.key]) {
        e.preventDefault()
        map[e.key]()
        poke()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [step, poke, nav])

  const now = Date.now() / 1000
  const prog = channel?.now
  const pct = prog ? Math.min(100, ((now - prog.start) / (prog.stop - prog.start)) * 100) : 0

  const toggleFav = async () => {
    if (!channel) return
    await api[channel.favorite ? 'del' : 'post'](`/api/live/channels/${channel.id}/favorite`)
    reload()
  }

  return (
    <div ref={containerRef} className={`live-player ${guide ? 'with-guide' : ''} ${controls ? 'show-controls' : ''}`} onMouseMove={poke}>
      <div className="live-stage">
        <video
          ref={videoRef}
          className="player-video"
          playsInline
          autoPlay
          onPlaying={() => setBuffering(false)}
          onWaiting={() => setBuffering(true)}
          onClick={() => setGuide((g) => !g)}
        />
        {buffering && !error && <div className="spinner" />}
        {error && <div className="player-error">{error}</div>}

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
                  {prog ? `${prog.title} · ${clock(prog.start)}–${clock(prog.stop)}` : 'Live'}
                  {channel.next && ` · Next: ${channel.next.title} at ${clock(channel.next.start)}`}
                </div>
                {prog && (
                  <div className="progress inline live-progress">
                    <div style={{ width: `${pct}%` }} />
                  </div>
                )}
              </div>
            </div>
          )}
          <div className="spacer" />
          <span className="live-badge">LIVE</span>
        </div>

        <div className="live-controls">
          <button className="icon-btn" onClick={() => step(-1)} aria-label="Previous channel"><Icon name="arrowUp" size={22} /></button>
          <button className="icon-btn" onClick={() => step(1)} aria-label="Next channel"><Icon name="arrowDown" size={22} /></button>
          <button className="icon-btn" onClick={() => setMuted((m) => !m)} aria-label={muted ? 'Unmute' : 'Mute'}>
            <Icon name={muted ? 'mute' : 'volume'} size={22} />
          </button>
          {channel && (
            <button className="icon-btn" onClick={toggleFav} aria-label={channel.favorite ? 'Remove favorite' : 'Add favorite'}>
              <Icon name="star" fill={channel.favorite} size={20} className={channel.favorite ? 'accent' : ''} />
            </button>
          )}
          <div className="spacer" />
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
