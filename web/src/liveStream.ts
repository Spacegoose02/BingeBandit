import Hls from 'hls.js'
import { useEffect, useState, type RefObject } from 'react'
import { api, type ApiError } from './api'

/**
 * Plays a Live TV channel in `video` (for Multi-View's screens; the full
 * Live TV player has its own, with rewinding and stream switching). It takes
 * a seat on the channel, keeps it with a ping, rejoins when the stream drops
 * and gives the seat back when the channel changes or the page closes.
 * Returns an error to show once retrying hasn't helped.
 */
export function useLiveStream(channelId: number | null, video: RefObject<HTMLVideoElement>): string {
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => setError(''), [channelId])
  useEffect(() => {
    const v = video.current
    if (!channelId || !v) return
    let hls: Hls | null = null
    let viewer = ''
    let cancelled = false
    let ping = 0
    let retry = 0
    const fail = (msg: string) => {
      if (cancelled) return
      cancelled = true
      hls?.destroy()
      hls = null
      if (attempt >= 2) setError(msg)
      retry = window.setTimeout(() => setAttempt((n) => n + 1), attempt >= 2 ? 15000 : 2000)
    }
    api
      .post<{ viewerId: string; playlist: string; bufferSecs?: number }>(`/api/live/channels/${channelId}/watch`)
      .then((res) => {
        if (cancelled) {
          api.del(`/api/live/viewers/${res.viewerId}`).catch(() => undefined)
          return
        }
        viewer = res.viewerId
        ping = window.setInterval(() => {
          api.post(`/api/live/viewers/${viewer}/ping`).catch((e: ApiError) => e.status === 410 && fail('This channel stopped.'))
        }, 10000)
        if (Hls.isSupported()) {
          const segs = Math.max(2, Math.ceil((res.bufferSecs ?? 8) / 2))
          hls = new Hls({ liveSyncDurationCount: segs, manifestLoadingTimeOut: 30000, manifestLoadingMaxRetry: 6, levelLoadingMaxRetry: 6, fragLoadingMaxRetry: 6 })
          hls.on(Hls.Events.ERROR, (_e, d) => {
            if (!d.fatal) return
            if (d.type === Hls.ErrorTypes.NETWORK_ERROR) {
              const status = (d.response as { code?: number } | undefined)?.code
              if (status === 503 || status === 410 || status === 404) fail('This channel is unavailable right now.')
              else hls?.startLoad()
            } else if (d.type === Hls.ErrorTypes.MEDIA_ERROR) hls?.recoverMediaError()
            else fail('Playback failed.')
          })
          hls.on(Hls.Events.FRAG_LOADED, () => setError(''))
          hls.loadSource(res.playlist)
          hls.attachMedia(v)
        } else {
          v.src = res.playlist
        }
        v.play().catch(() => undefined)
      })
      .catch((e) => fail((e as Error).message || 'This channel is unavailable right now.'))
    const leave = () => viewer && api.del(`/api/live/viewers/${viewer}`, { keepalive: true }).catch(() => undefined)
    window.addEventListener('pagehide', leave)
    return () => {
      cancelled = true
      window.clearTimeout(retry)
      window.clearInterval(ping)
      window.removeEventListener('pagehide', leave)
      hls?.destroy()
      v.removeAttribute('src')
      leave()
    }
  }, [channelId, attempt, video])
  return error
}
