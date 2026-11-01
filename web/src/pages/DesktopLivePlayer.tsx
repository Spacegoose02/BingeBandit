import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type ApiError } from '../api'
import { desktop, type DesktopEvent, type DesktopMenuSection } from '../desktop'
import { useApi, useDocumentTitle } from '../hooks'
import type { LiveChannel } from '../types'
import { ChannelLogo, channelNo, clock } from './LiveGuide'

/**
 * Live TV in the Windows app: the channel plays in mpv under BingeBandit's
 * controls. Up and down change channel, the menu lists every channel with
 * what's on, and left and right rewind and catch up within the stream. Like
 * the web player, this page holds the Live TV seat (pings it, gives it back),
 * rejoins on its own when the stream drops, and tells the server when the
 * stream stalls so it can move to the channel's next source.
 */
export default function DesktopLivePlayer() {
  const bridge = desktop!
  const { id } = useParams()
  const nav = useNavigate()
  const { data: channels, reload } = useApi<LiveChannel[]>('/api/live/channels', 60000)
  const channel = channels?.find((c) => String(c.id) === id)
  useDocumentTitle(channel ? `${channelNo(channel.number)} ${channel.name}` : 'Live TV')
  const [attempt, setAttempt] = useState(0) // bumped to rejoin after a failure
  const [error, setError] = useState('')
  const [buffering, setBuffering] = useState(false)
  const failures = useRef(0)
  const viewerRef = useRef('')
  const stallSecs = useRef(12)
  const failRef = useRef<(msg: string) => void>(() => undefined)
  const closed = useRef(false)
  const channelsRef = useRef<LiveChannel[] | null>(null)
  channelsRef.current = channels

  // A new channel starts with a clean slate.
  useEffect(() => {
    failures.current = 0
    setError('')
  }, [id])

  // Join the channel and play it, again after a failure.
  useEffect(() => {
    if (!id) return
    let viewer = ''
    let cancelled = false
    let retryTimer = 0
    let ping = 0
    setBuffering(false)
    // Streams drop, providers hiccup and the server restarts the conversion:
    // rejoin on its own, quickly at first, then every 15 s.
    const fail = (msg: string) => {
      if (cancelled) return
      cancelled = true
      failures.current++
      if (failures.current > 3) setError(msg)
      bridge.command('osd', failures.current > 3 ? `${msg} Trying again…` : 'Reconnecting…')
      retryTimer = window.setTimeout(() => setAttempt((n) => n + 1), failures.current > 3 ? 15000 : 1500 * failures.current)
    }
    failRef.current = fail
    api
      .post<{ viewerId: string; playlist: string; stallSecs?: number }>(`/api/live/channels/${id}/watch`)
      .then((res) => {
        if (cancelled) {
          api.del(`/api/live/viewers/${res.viewerId}`).catch(() => undefined)
          return
        }
        viewer = res.viewerId
        viewerRef.current = viewer
        stallSecs.current = res.stallSecs ?? 12
        // A paused player stops fetching the playlist; this keeps the seat.
        ping = window.setInterval(() => {
          api.post(`/api/live/viewers/${viewer}/ping`).catch((e: ApiError) => e.status === 410 && fail('This channel stopped.'))
        }, 10000)
        const ch = channelsRef.current?.find((c) => String(c.id) === id)
        return bridge.play({
          url: res.playlist,
          title: ch ? `${channelNo(ch.number)} ${ch.name}` : 'Live TV',
          start: 0,
          audio: 0,
          sub: 0,
          live: true,
        })
      })
      .catch((e) => fail((e as Error).message || 'This channel is unavailable right now.'))
    const leave = () => viewer && api.del(`/api/live/viewers/${viewer}`, { keepalive: true }).catch(() => undefined)
    window.addEventListener('pagehide', leave)
    return () => {
      cancelled = true
      window.clearTimeout(retryTimer)
      window.clearInterval(ping)
      window.removeEventListener('pagehide', leave)
      viewerRef.current = ''
      leave()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, attempt])

  // mpv quits when this page goes, unless another channel starts straight away.
  useEffect(
    () => () => {
      if (!closed.current) bridge.release()
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  // Buffering for longer than the server's stall time: tell it, so it can
  // move the channel to its next stream. If it didn't, the player is what's
  // stuck, so join again; if it did, give the new stream one more stall period.
  useEffect(() => {
    if (!buffering) return
    const started = Date.now()
    let next = stallSecs.current
    let switched = false
    let done = false
    const rejoin = () => {
      if (done) return
      done = true
      setAttempt((n) => n + 1)
    }
    const t = window.setInterval(() => {
      const secs = (Date.now() - started) / 1000
      if (secs < next || !viewerRef.current) return
      next += stallSecs.current
      if (switched) return rejoin()
      api
        .post<{ switched?: boolean }>(`/api/live/viewers/${viewerRef.current}/buffering`, { seconds: Math.round(secs) })
        .then((r) => (r?.switched ? (switched = true) : rejoin()), () => undefined)
    }, 1000)
    return () => {
      done = true
      window.clearInterval(t)
    }
  }, [buffering, attempt])

  const step = (dir: 1 | -1) => {
    if (!channels?.length) return
    const i = channels.findIndex((c) => String(c.id) === id)
    const next = channels[(i + dir + channels.length) % channels.length]
    nav(`/live/${next.id}`, { replace: true })
  }

  const toggleFav = async () => {
    if (!channel) return
    try {
      await api[channel.favorite ? 'del' : 'post'](`/api/live/channels/${channel.id}/favorite`)
      bridge.command('osd', channel.favorite ? 'Removed from favorites' : 'Added to favorites')
      reload()
    } catch (e) {
      bridge.command('osd', (e as Error).message)
    }
  }

  const handler = useRef<(e: DesktopEvent) => void>(() => undefined)
  handler.current = (e) => {
    switch (e.type) {
      case 'time':
        if (!e.paused && failures.current) {
          failures.current = 0
          setError('')
        }
        break
      case 'buffering':
        setBuffering(e.on)
        break
      case 'end':
        // A live stream that ends has dropped.
        failRef.current('The channel stopped.')
        break
      case 'error':
        failRef.current('This channel is unavailable right now.')
        break
      case 'key':
        if (e.name === 'bb-channel') step(e.arg === 'down' ? 1 : -1)
        else if (e.name === 'bb-pick' && e.arg?.startsWith('ch:')) nav(`/live/${e.arg.slice(3)}`, { replace: true })
        else if (e.name === 'bb-pick' && e.arg === 'fav') toggleFav()
        break
      case 'closed':
        closed.current = true
        nav('/live')
        break
    }
  }
  useEffect(() => bridge.onEvent((e) => handler.current(e)), [bridge])

  // What the controls show: the channel, what's on, and every channel in the menu.
  const now = Date.now() / 1000
  const prog = channel?.now
  const left = prog ? Math.max(0, Math.round((prog.stop - now) / 60)) : 0
  const uiState = JSON.stringify({
    mode: 'live',
    title: channel ? `${channelNo(channel.number)}  ${channel.name}` : 'Live TV',
    subtitle: [
      prog ? `${prog.title}${prog.subtitle ? ` — ${prog.subtitle}` : ''}` : '',
      prog ? `${clock(prog.start)}–${clock(prog.stop)}${left ? ` (${left} min left)` : ''}` : '',
      channel?.next ? `Next: ${channel.next.title} at ${clock(channel.next.start)}` : '',
    ]
      .filter(Boolean)
      .join('  ·  '),
  })
  useEffect(() => {
    bridge.command('state', JSON.parse(uiState))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uiState])
  // The program time left moves on; refresh it every half minute.
  const [, tick] = useState(0)
  useEffect(() => {
    const t = window.setInterval(() => tick((n) => n + 1), 30000)
    return () => window.clearInterval(t)
  }, [])

  const menu: DesktopMenuSection[] = []
  if (channel) menu.push({ title: 'This channel', items: [{ id: 'fav', label: channel.favorite ? 'Remove from favorites' : 'Add to favorites' }] })
  if (channels?.length) {
    menu.push({
      title: 'Channels',
      items: channels.map((c) => ({
        id: `ch:${c.id}`,
        label: `${channelNo(c.number)}  ${c.name}${c.now ? ` · ${c.now.title}` : ''}`,
        active: String(c.id) === id,
      })),
    })
  }
  const menuJson = JSON.stringify(menu)
  useEffect(() => {
    bridge.command('menu', JSON.parse(menuJson))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [menuJson])

  return (
    <div className="desktop-player">
      <div className="desktop-player-card">
        {channel && <ChannelLogo channel={channel} size={64} />}
        <div className="desktop-player-title">{channel ? `${channelNo(channel.number)} ${channel.name}` : 'Live TV'}</div>
        {error ? <p className="desktop-player-error">{error} Trying again…</p> : <p>{prog ? prog.title : 'Playing in the BingeBandit player.'}</p>}
        <div className="desktop-player-actions">
          <button className="btn" onClick={() => bridge.command('stop')} autoFocus>
            Stop
          </button>
          <button className="btn" onClick={() => nav('/live')}>
            Guide
          </button>
        </div>
      </div>
    </div>
  )
}
