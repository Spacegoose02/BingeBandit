import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { roomPosition, togetherLink, useTogether, type TogetherRoom } from '../components/Together'
import { hueArea, hueSyncOn } from '../devicePrefs'
import { desktop, desktopCan, mpvTrackId, type DesktopEvent, type DesktopMenuSection } from '../desktop'
import { episodeLabel, formatClock } from '../format'
import { pickTracks } from '../tracks'
import type { Item, ItemDetail, Prefs } from '../types'

/**
 * The player in the Windows app: the file plays in mpv, full screen over the
 * app, under BingeBandit's own controls (apps/desktop/mpv-scripts). This page
 * keeps the server in step (progress, watched, Hue Sync), tells the controls
 * what to show, and does what the web player does around the video: intro and
 * credits skipping, the next episode, and Watch Together. mpv stays open
 * between episodes.
 */
const RECONNECT_MS = 120000 // how long to wait for a restarting server before giving up

export default function DesktopPlayer({
  detail,
  prefs,
  startAt,
  fileId,
  playlistId,
  together,
}: {
  detail: ItemDetail
  prefs: Prefs
  startAt: number
  fileId: number
  playlistId: number
  together: { roomId: string | null; announce: boolean; error?: string }
}) {
  const bridge = desktop!
  const { item, files = [] } = detail
  const file = files.find((f) => f.id === fileId) ?? files[0]
  const nav = useNavigate()
  const duration = file?.duration || (item.runtime ?? 0) * 60
  const [error, setError] = useState(file ? '' : 'This title has no file to play.')
  const [upNext, setUpNext] = useState<Item | null>(null)
  const [promptKind, setPromptKind] = useState<'' | 'intro' | 'credits'>('')

  const pos = useRef(startAt)
  const paused = useRef(false)
  const played = useRef(0) // seconds actually watched, for history
  const started = useRef(false)
  const finished = useRef(false) // reported as watched to the end
  const closed = useRef(false)
  const failed = useRef(false)
  const upNextRef = useRef<Item | null>(null)
  const prompt = useRef<'' | 'intro' | 'credits'>('')
  const introDone = useRef(false)
  const creditsDone = useRef(false)
  const playSession = useRef(Math.random().toString(36).slice(2) + Date.now().toString(36))

  const title = item.kind === 'episode' ? `${item.showTitle} ${episodeLabel(item)} · ${item.title}` : item.title
  const intro = detail.markers?.find((m) => m.kind === 'intro')
  const credits = detail.markers?.find((m) => m.kind === 'credits')
  const hasNext = item.kind === 'episode' || !!playlistId
  // A movie with a scene after the credits jumps to that scene instead of ending.
  const afterCredits = !hasNext && !!credits && credits.end < duration - 5
  const done = playlistId ? `/playlist/${playlistId}` : item.kind === 'episode' ? `/item/${item.showId}` : `/item/${item.id}`

  const setPrompt = (p: '' | 'intro' | 'credits') => {
    prompt.current = p
    setPromptKind(p)
    // An older Windows app has mpv's own controls: say it in text instead.
    if (p && !desktopCan('ui')) {
      bridge.command('osd', p === 'intro' ? 'Press OK to skip the intro' : hasNext ? 'Press OK for the next episode' : 'Press OK to skip the credits')
    }
  }

  // Watch Together: everyone else's play, pause, seek and next item land
  // here and go to mpv; this viewer's own go out through share().
  const [, setParams] = useSearchParams()
  const [roomId, setRoomId] = useState(desktopCan('together') ? together.roomId : null)
  const [people, setPeople] = useState<{ id: number; name: string }[]>([])
  const roomJoined = useRef(false)
  // Until when (ms) changes in mpv come from the room, not from this viewer.
  const following = useRef(0)
  const tg = useTogether(roomId, (s, skew) => {
    if (s.itemId !== item.id) {
      nav(`/play/${s.itemId}?together=${roomId}`, { replace: true })
      return
    }
    const target = roomPosition(s, skew)
    const jump = Math.abs(pos.current - target) > 2
    const pauseChanged = s.paused !== paused.current
    following.current = Date.now() + 2500
    if (jump) bridge.command('seek', target)
    if (pauseChanged) bridge.command('pause', s.paused)
    const what = pauseChanged ? (s.paused ? 'paused' : 'pressed play') : jump ? `skipped to ${formatClock(target)}` : ''
    if (what && s.byName) bridge.command('osd', `${s.byName} ${what}`)
  })
  const share = (p: boolean, at: number) => {
    if (roomId) tg.send({ paused: p, position: at })
  }
  const nextUrl = (next: Item) => {
    const q = new URLSearchParams()
    if (playlistId) q.set('playlist', String(playlistId))
    if (roomId) q.set('together', roomId)
    return `/play/${next.id}${q.toString() ? `?${q}` : ''}`
  }
  // Moving on in a watch party takes everyone along.
  const goNext = (next: Item) => nav(nextUrl(next), { replace: true, state: { togetherMove: !!roomId } })

  const report = (stopped = false, toEnd = false) => {
    if (!file || !started.current) return
    if (toEnd) finished.current = true
    api
      .post(
        '/api/progress',
        {
          playSessionId: playSession.current,
          itemId: item.id,
          fileId: file.id,
          position: toEnd ? duration : pos.current,
          duration,
          paused: paused.current,
          stopped,
          mode: 'direct',
          transcodeId: '',
          client: 'Windows',
          lights: hueSyncOn(),
          lightsArea: hueArea(),
          played: Math.round(played.current),
        },
        { keepalive: true },
      )
      .catch(() => undefined)
  }

  // Play the file from `at` (mpv reuses its window when it's still open).
  const playFrom = (at: number) => {
    if (!file) return
    const { audio, sub } = pickTracks(file, prefs)
    const audioStream = audio >= 0 ? file.streams.find((s) => s.index === audio) : undefined
    bridge
      .play({
        url: `/api/files/${file.id}/direct`,
        title,
        start: at,
        audio: audioStream ? mpvTrackId(file, audioStream) : 0,
        sub: sub && !sub.external ? mpvTrackId(file, sub) : 0,
        subUrl: sub?.external
          ? { url: `/api/files/${file.id}/subtitles/${sub.index}.vtt`, title: sub.title ?? '', lang: sub.language ?? '' }
          : undefined,
      })
      .catch((e: Error) => setError(e.message))
  }

  // mpv rides out a short cut itself (it reconnects for about two minutes),
  // but a server restart can still end the file early. Then wait for the
  // server, checking with growing gaps for up to two minutes, and carry on
  // from the same spot.
  const reconnectUntil = useRef(0) // 0: not reconnecting
  const reconnects = useRef(0) // times started over since playback last ran on
  const reconnectTimer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(reconnectTimer.current), [])
  const lostServer = (at: number, tries = 0): void => {
    if (!reconnectUntil.current) reconnectUntil.current = Date.now() + RECONNECT_MS
    if (Date.now() > reconnectUntil.current || reconnects.current >= 3) {
      reconnectUntil.current = 0
      failed.current = true
      setError("Lost the server. Try again once it's back on.")
      bridge.command('stop')
      return
    }
    bridge.command('osd', 'Lost the server. Reconnecting…')
    reconnectTimer.current = window.setTimeout(
      () =>
        fetch('/api/health', { cache: 'no-store' })
          .then((r) => r.ok, () => false)
          .then((up) => {
            if (closed.current) return
            if (!up) return lostServer(at, tries + 1)
            reconnects.current++
            playFrom(at)
          }),
      Math.min(2000 * 2 ** tries, 15000),
    )
  }
  // Ended or failed this far from the end, after playing: the connection
  // was cut rather than the file being over or unplayable.
  const cutShort = () => started.current && duration > 0 && duration - pos.current > 120

  // Finished (the end, or skipping the credits): on to the next episode, or out.
  const onEnded = async () => {
    report(false, true)
    setPrompt('')
    if (!hasNext) {
      bridge.command('stop')
      return nav(done, { replace: true })
    }
    try {
      const next = await api.get<Item | undefined>(
        playlistId ? `/api/playlists/${playlistId}/next?after=${item.id}` : `/api/items/${item.id}/next`,
      )
      if (!next) {
        bridge.command('stop')
        return nav(done, { replace: true })
      }
      if (prefs.autoPlayNext || roomId) return goNext(next)
      upNextRef.current = next
      setUpNext(next)
      if (!desktopCan('ui')) bridge.command('osd', `Up next: ${next.title}. Press N or OK to play it, Back to stop.`)
    } catch {
      bridge.command('stop')
      nav(-1)
    }
  }

  const skipNow = () => {
    const which = prompt.current
    setPrompt('')
    if (which === 'intro' && intro) {
      introDone.current = true
      bridge.command('seek', intro.end)
      share(paused.current, intro.end)
    } else if (which === 'credits' && credits) {
      creditsDone.current = true
      if (afterCredits) {
        bridge.command('seek', credits.end)
        share(paused.current, credits.end)
      } else onEnded()
    }
  }

  // Offers (or does) the intro and credits skip as playback reaches them.
  const markers = (t: number) => {
    const inIntro = !!intro && t >= intro.start && t < intro.end - 1
    const inCredits = !!credits && t >= credits.start && t < Math.min(credits.end, duration) - 1 && !upNextRef.current
    if (inIntro && !introDone.current && prefs.skipIntro !== 'off') {
      if (prefs.skipIntro === 'auto') {
        prompt.current = 'intro'
        bridge.command('osd', 'Skipped the intro')
        skipNow()
      } else if (prompt.current !== 'intro') setPrompt('intro')
    } else if (inCredits && !creditsDone.current && prefs.skipCredits !== 'off') {
      if (prefs.skipCredits === 'auto' && (hasNext || afterCredits)) {
        prompt.current = 'credits'
        if (afterCredits) bridge.command('osd', 'Skipped to the scene after the credits')
        skipNow()
      } else if (prompt.current !== 'credits') setPrompt('credits')
    } else if (!inIntro && !inCredits && prompt.current) {
      setPrompt('')
    }
  }

  const startRoom = async () => {
    try {
      const r = await api.post<TogetherRoom>('/api/together', { itemId: item.id, position: pos.current, paused: paused.current })
      roomJoined.current = true
      setRoomId(r.id)
      setParams(
        (p) => {
          p.set('together', r.id)
          p.delete('t')
          return p
        },
        { replace: true },
      )
      bridge.command('osd', 'Watch party started. Invite people from the menu.')
    } catch (e) {
      bridge.command('osd', (e as Error).message)
    }
  }
  const leaveRoom = () => {
    setRoomId(null)
    setParams(
      (p) => {
        p.delete('together')
        return p
      },
      { replace: true },
    )
    bridge.command('osd', 'You left the watch party')
  }
  const invite = async (userId: number) => {
    try {
      await api.post(`/api/together/${roomId}/invite`, { userIds: [userId] })
      bridge.command('osd', `Invited ${people.find((p) => p.id === userId)?.name ?? ''}`)
    } catch (e) {
      bridge.command('osd', (e as Error).message)
    }
  }

  // A menu entry picked in the controls.
  const onPick = (id: string) => {
    if (id === 'next') {
      creditsDone.current = true
      onEnded()
    } else if (id === 'tg:start') startRoom()
    else if (id === 'tg:leave') leaveRoom()
    else if (id === 'tg:copy' && tg.room) {
      bridge.command('copy', togetherLink(tg.room))
      bridge.command('osd', 'Invite link copied')
    } else if (id.startsWith('tg:invite:')) invite(Number(id.slice('tg:invite:'.length)))
  }

  // Keep the latest closures for mpv's events.
  const handler = useRef<(e: DesktopEvent) => void>(() => undefined)
  handler.current = (e) => {
    switch (e.type) {
      case 'time': {
        const step = e.position - pos.current
        if (!paused.current && !e.paused && step > 0 && step < 3) {
          played.current += step
          reconnects.current = 0
          reconnectUntil.current = 0
        }
        const pauseChanged = started.current && e.paused !== paused.current
        const seeked = started.current && Math.abs(step) > 3
        pos.current = e.position
        paused.current = e.paused
        if (!started.current) {
          started.current = true
          report()
        } else if (pauseChanged || seeked) {
          report()
          // This viewer paused, played or skipped: tell the room.
          if (Date.now() > following.current) share(e.paused, e.position)
        }
        markers(e.position)
        break
      }
      case 'end':
        if (finished.current) break
        if (cutShort()) lostServer(pos.current)
        else onEnded()
        break
      case 'key':
        if (e.name === 'bb-pick') onPick(e.arg ?? '')
        else if (upNextRef.current) {
          goNext(upNextRef.current)
        } else if (e.name === 'bb-enter') {
          if (prompt.current) skipNow()
          else bridge.command('pause', !paused.current)
        } else if (e.name === 'bb-next' && hasNext) {
          creditsDone.current = true
          onEnded()
        }
        break
      case 'closed':
        closed.current = true
        if (!finished.current) report(true)
        if (failed.current) break // stay to show what went wrong
        if (upNextRef.current || finished.current) nav(done, { replace: true })
        else nav(-1)
        break
      case 'error':
        if (cutShort() && !finished.current) {
          lostServer(pos.current)
          break
        }
        // mpv sits on a black screen after a failure; close it to show the message.
        failed.current = true
        setError(e.message)
        bridge.command('stop')
        break
    }
  }

  useEffect(() => {
    if (!file) return
    const off = bridge.onEvent((e) => handler.current(e))
    playFrom(startAt)
    if (together.error) bridge.command('osd', `Couldn't join the watch party: ${together.error}`)
    const t = window.setInterval(() => !paused.current && report(), 10000)
    return () => {
      off()
      window.clearInterval(t)
      if (!closed.current && !finished.current) report(true)
      // mpv quits unless the next page starts another file straight away.
      if (!closed.current) bridge.release()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // On joining a room: match its play/pause, or tell it this viewer moved it on.
  useEffect(() => {
    if (!tg.room || roomJoined.current) return
    roomJoined.current = true
    if (together.announce && roomId === together.roomId) tg.send({ itemId: item.id, paused: false, position: 0 })
    else if (tg.room.state.paused) {
      following.current = Date.now() + 2500
      bridge.command('pause', true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tg.room])
  // Stay within a few seconds of the room while it plays.
  useEffect(() => {
    if (!roomId) return
    const t = window.setInterval(() => {
      const r = tg.room
      if (!r || r.state.paused || paused.current || !started.current || Date.now() < following.current) return
      const target = tg.position()
      if (Math.abs(pos.current - target) > 3) {
        following.current = Date.now() + 2500
        bridge.command('seek', target)
      }
    }, 5000)
    return () => window.clearInterval(t)
  })
  useEffect(() => {
    if (!tg.ended) return
    bridge.command('osd', 'The watch party has ended.')
    setRoomId(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tg.ended])
  // Who can be invited.
  useEffect(() => {
    setPeople([])
    if (!roomId) return
    api.get<{ id: number; name: string }[]>(`/api/together/${roomId}/people`).then(setPeople, () => undefined)
  }, [roomId])

  // What the controls show.
  const members = tg.room?.members ?? []
  const subtitle =
    item.kind === 'episode'
      ? `${episodeLabel(item)} · ${item.title}`
      : [item.year, item.runtime ? `${item.runtime} min` : ''].filter(Boolean).join(' · ')
  const promptLabel =
    promptKind === 'intro' ? 'Skip intro' : promptKind === 'credits' ? (hasNext ? 'Next episode' : 'Skip credits') : ''
  const uiState = JSON.stringify({
    mode: 'vod',
    title: item.kind === 'episode' ? (item.showTitle ?? item.title) : item.title,
    subtitle,
    markers: [intro, credits].filter(Boolean).map((m) => ({ start: m!.start, end: m!.end })),
    prompt: promptLabel,
    upNext: upNext ? (upNext.kind === 'episode' ? `${episodeLabel(upNext)} · ${upNext.title}` : upNext.title) : '',
    badge: roomId ? `Watching together${members.length ? ` · ${members.length}` : ''}` : '',
  })
  useEffect(() => {
    bridge.command('state', JSON.parse(uiState))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uiState])

  const menu: DesktopMenuSection[] = []
  if (hasNext) menu.push({ title: 'Playing', items: [{ id: 'next', label: playlistId ? 'Play the next item' : 'Play the next episode' }] })
  if (desktopCan('together')) {
    const others = people.filter((p) => !members.some((m) => m.userId === p.id))
    menu.push({
      title: 'Watch together',
      items: roomId
        ? [
            { label: members.length ? members.map((m) => m.name).join(', ') : 'Joining…', disabled: true },
            ...others.map((p) => ({ id: `tg:invite:${p.id}`, label: `Invite ${p.name}` })),
            { id: 'tg:copy', label: 'Copy invite link' },
            { id: 'tg:leave', label: 'Leave the watch party' },
          ]
        : [{ id: 'tg:start', label: 'Start a watch party' }],
    })
  }
  const menuJson = JSON.stringify(menu)
  useEffect(() => {
    bridge.command('menu', JSON.parse(menuJson))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [menuJson])

  const backdrop = img(item.backdrop, 'w1280')
  return (
    <div className="desktop-player" style={backdrop ? { backgroundImage: `url(${backdrop})` } : undefined}>
      <div className="desktop-player-card">
        <div className="desktop-player-title">{title}</div>
        {error ? (
          <>
            <p className="desktop-player-error">{error}</p>
            <button className="btn" onClick={() => nav(-1)} autoFocus>
              Back
            </button>
          </>
        ) : upNext ? (
          <>
            <p>Up next: {upNext.title}</p>
            <div className="desktop-player-actions">
              <button className="btn btn-primary" onClick={() => goNext(upNext)} autoFocus>
                Play next
              </button>
              <button className="btn" onClick={() => bridge.command('stop')}>
                Stop
              </button>
            </div>
          </>
        ) : (
          <>
            <p>Playing in the BingeBandit player{roomId ? ', watching together' : ''}.</p>
            <button className="btn" onClick={() => bridge.command('stop')} autoFocus>
              Stop
            </button>
          </>
        )}
      </div>
    </div>
  )
}
