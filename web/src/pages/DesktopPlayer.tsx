import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, img } from '../api'
import { hueArea, hueSyncOn } from '../devicePrefs'
import { desktop, mpvTrackId, type DesktopEvent } from '../desktop'
import { episodeLabel } from '../format'
import { pickTracks } from '../tracks'
import type { Item, ItemDetail, Prefs } from '../types'

/**
 * The player in the Windows app: the file plays in mpv, full screen over the
 * app, and this page keeps the server in step (progress, watched, Hue Sync)
 * and does what the web player does around the video: intro and credits
 * skipping and the next episode. mpv stays open between episodes.
 */
export default function DesktopPlayer({
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
  playlistId: number
}) {
  const bridge = desktop!
  const { item, files = [] } = detail
  const file = files.find((f) => f.id === fileId) ?? files[0]
  const nav = useNavigate()
  const duration = file?.duration || (item.runtime ?? 0) * 60
  const [error, setError] = useState(file ? '' : 'This title has no file to play.')
  const [upNext, setUpNext] = useState<Item | null>(null)

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
  const nextUrl = (next: Item) => `/play/${next.id}${playlistId ? `?playlist=${playlistId}` : ''}`

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

  // Finished (the end, or skipping the credits): on to the next episode, or out.
  const onEnded = async () => {
    report(false, true)
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
      if (prefs.autoPlayNext) return nav(nextUrl(next), { replace: true })
      upNextRef.current = next
      setUpNext(next)
      bridge.command('osd', `Up next: ${next.title}. Press N or OK to play it, Back to stop.`)
    } catch {
      bridge.command('stop')
      nav(-1)
    }
  }

  const skipNow = () => {
    const which = prompt.current
    prompt.current = ''
    if (which === 'intro' && intro) {
      introDone.current = true
      bridge.command('seek', intro.end)
    } else if (which === 'credits' && credits) {
      creditsDone.current = true
      if (afterCredits) bridge.command('seek', credits.end)
      else onEnded()
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
      } else if (prompt.current !== 'intro') {
        prompt.current = 'intro'
        bridge.command('osd', 'Press OK to skip the intro')
      }
    } else if (inCredits && !creditsDone.current && prefs.skipCredits !== 'off') {
      if (prefs.skipCredits === 'auto' && (hasNext || afterCredits)) {
        prompt.current = 'credits'
        if (afterCredits) bridge.command('osd', 'Skipped to the scene after the credits')
        skipNow()
      } else if (prompt.current !== 'credits') {
        prompt.current = 'credits'
        bridge.command('osd', hasNext ? 'Press OK for the next episode' : 'Press OK to skip the credits')
      }
    } else if (!inIntro && !inCredits) {
      prompt.current = ''
    }
  }

  // Keep the latest closures for mpv's events.
  const handler = useRef<(e: DesktopEvent) => void>(() => undefined)
  handler.current = (e) => {
    switch (e.type) {
      case 'time': {
        const step = e.position - pos.current
        if (!paused.current && !e.paused && step > 0 && step < 3) played.current += step
        const pauseChanged = started.current && e.paused !== paused.current
        pos.current = e.position
        paused.current = e.paused
        if (!started.current) {
          started.current = true
          report()
        } else if (pauseChanged || Math.abs(step) > 3) report() // paused, resumed or seeked
        markers(e.position)
        break
      }
      case 'end':
        if (!finished.current) onEnded()
        break
      case 'key':
        if (upNextRef.current) {
          nav(nextUrl(upNextRef.current), { replace: true })
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
    const { audio, sub } = pickTracks(file, prefs)
    const audioStream = audio >= 0 ? file.streams.find((s) => s.index === audio) : undefined
    bridge
      .play({
        url: `/api/files/${file.id}/direct`,
        title,
        start: startAt,
        audio: audioStream ? mpvTrackId(file, audioStream) : 0,
        sub: sub && !sub.external ? mpvTrackId(file, sub) : 0,
        subUrl: sub?.external
          ? { url: `/api/files/${file.id}/subtitles/${sub.index}.vtt`, title: sub.title ?? '', lang: sub.language ?? '' }
          : undefined,
      })
      .catch((e: Error) => setError(e.message))
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
              <button className="btn btn-primary" onClick={() => nav(nextUrl(upNext), { replace: true })} autoFocus>
                Play next
              </button>
              <button className="btn" onClick={() => bridge.command('stop')}>
                Stop
              </button>
            </div>
          </>
        ) : (
          <>
            <p>Playing in the BingeBandit player.</p>
            <button className="btn" onClick={() => bridge.command('stop')} autoFocus>
              Stop
            </button>
          </>
        )}
      </div>
    </div>
  )
}
