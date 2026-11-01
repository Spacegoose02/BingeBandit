import { useCallback, useEffect, useRef, useState } from 'react'
import { api, img } from '../api'
import { formatClock } from '../format'
import type { CastDevice, CastSession } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

// Casting: the server plays on a Chromecast, Google TV or DLNA TV on its
// network, and the browser is the remote. (Chrome's own Cast button only
// works on HTTPS pages, and the server is next to the TV anyway.)

const castChanged = 'bb:cast'

/** Tells the cast bar to look again (a cast started or stopped). */
export function castsChanged() {
  window.dispatchEvent(new Event(castChanged))
}

export const deviceKind = (d: CastDevice) => (d.kind === 'chromecast' ? 'Chromecast' : 'TV (DLNA)')

/** Lists the TVs the server finds and starts playing on the one picked. */
export function CastPicker({
  itemId,
  fileId,
  start,
  audioStream,
  onClose,
  onStarted,
}: {
  itemId: number
  fileId?: number
  start: number
  audioStream?: number
  onClose: () => void
  onStarted?: (s: CastSession) => void
}) {
  const [devices, setDevices] = useState<CastDevice[] | null>(null)
  const [looking, setLooking] = useState(false)
  const [starting, setStarting] = useState('')
  const [error, setError] = useState('')

  const look = useCallback((refresh: boolean) => {
    setLooking(true)
    setError('')
    api
      .get<{ devices: CastDevice[] }>(`/api/cast/devices${refresh ? '?refresh=1' : ''}`)
      .then((r) => setDevices(r.devices))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLooking(false))
  }, [])
  useEffect(() => look(false), [look])

  const cast = async (d: CastDevice) => {
    setStarting(d.id)
    setError('')
    try {
      const s = await api.post<CastSession>('/api/cast/start', {
        deviceId: d.id,
        itemId,
        fileId: fileId ?? 0,
        start: Math.floor(start),
        ...(audioStream !== undefined && audioStream >= 0 ? { audioStream } : {}),
      })
      castsChanged()
      onStarted?.(s)
      onClose()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setStarting('')
    }
  }

  return (
    <Modal title="Play on a TV" onClose={onClose}>
      {devices === null && !error && <p className="muted">Looking for TVs…</p>}
      {devices && devices.length === 0 && (
        <p className="muted small">
          No TVs found. The server looks for Chromecasts, Google TVs and DLNA smart TVs on its own network, so the TV needs to be on and on the same
          network as the server. If the server runs in Docker, an admin can add TVs by address in Settings → Casting.
        </p>
      )}
      {devices && devices.length > 0 && (
        <div className="cast-devices">
          {devices.map((d) => (
            <button key={d.id} className="cast-device" onClick={() => cast(d)} disabled={!!starting}>
              <Icon name={d.kind === 'chromecast' ? 'cast' : 'tv'} size={22} />
              <span>
                <strong>{d.name}</strong>
                <span className="muted small">
                  {[deviceKind(d), d.model, d.host].filter(Boolean).join(' · ')}
                </span>
              </span>
              {starting === d.id && <span className="muted small">Starting…</span>}
            </button>
          ))}
        </div>
      )}
      {error && <div className="form-error">{error}</div>}
      <div className="actions">
        <button className="btn btn-glass" onClick={() => look(true)} disabled={looking || !!starting}>
          <Icon name="refresh" size={16} /> {looking ? 'Looking…' : 'Look again'}
        </button>
      </div>
    </Modal>
  )
}

/** A button that opens the TV picker for a title. */
export function CastButton({ itemId, fileId, start, disabled }: { itemId: number; fileId?: number; start: number; disabled?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className="btn btn-glass icon-only" onClick={() => setOpen(true)} disabled={disabled} title="Play on a TV">
        <Icon name="cast" size={18} />
      </button>
      {open && <CastPicker itemId={itemId} fileId={fileId} start={start} onClose={() => setOpen(false)} />}
    </>
  )
}

/** The remote for what's playing on TVs, along the bottom of every page. */
export function CastBar() {
  const [sessions, setSessions] = useState<CastSession[]>([])
  const [error, setError] = useState('')
  const [dragging, setDragging] = useState<{ id: string; t: number } | null>(null)
  const busy = useRef(false)

  const load = useCallback(() => {
    if (document.hidden) return
    api
      .get<{ sessions: CastSession[] }>('/api/cast/sessions')
      .then((r) => setSessions(r.sessions))
      .catch(() => undefined)
  }, [])

  // Every 2 s while something is casting, else now and then (a cast started
  // from another device shows up too).
  const active = sessions.length > 0
  useEffect(() => {
    load()
    const t = window.setInterval(load, active ? 2000 : 30000)
    window.addEventListener(castChanged, load)
    document.addEventListener('visibilitychange', load)
    return () => {
      window.clearInterval(t)
      window.removeEventListener(castChanged, load)
      document.removeEventListener('visibilitychange', load)
    }
  }, [load, active])

  const control = async (s: CastSession, body: Record<string, unknown>) => {
    if (busy.current) return
    busy.current = true
    setError('')
    try {
      const next = await api.post<CastSession>(`/api/cast/sessions/${s.id}`, body)
      setSessions((list) => list.map((x) => (x.id === next.id ? next : x)))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      busy.current = false
    }
  }
  const stop = async (s: CastSession) => {
    setSessions((list) => list.filter((x) => x.id !== s.id))
    await api.del(`/api/cast/sessions/${s.id}`).catch(() => undefined)
    castsChanged()
  }

  if (!sessions.length) return null
  return (
    <div className="cast-bar" role="region" aria-label="Playing on TVs">
      {sessions.map((s) => {
        const pos = dragging?.id === s.id ? dragging.t : s.position
        const playing = s.state === 'playing' || s.state === 'loading'
        return (
          <div key={s.id} className="cast-row">
            {s.poster && <img src={img(s.poster, 'w92')} alt="" />}
            <div className="cast-info">
              <strong>{s.title}</strong>
              <span className="muted small">
                {s.subtitle ? `${s.subtitle} · ` : ''}
                {s.state === 'loading' ? 'Starting on' : 'On'} {s.device.name}
              </span>
              {s.error && <span className="form-error small">{s.error}</span>}
            </div>
            <div className="cast-controls">
              <button className="icon-btn" onClick={() => control(s, { action: 'seek', position: s.position - 10 })} aria-label="Back 10 seconds">
                <Icon name="replay10" size={20} />
              </button>
              <button className="icon-btn" onClick={() => control(s, { action: playing ? 'pause' : 'play' })} aria-label={playing ? 'Pause' : 'Play'}>
                <Icon name={playing ? 'pause' : 'play'} fill size={20} />
              </button>
              <button className="icon-btn" onClick={() => control(s, { action: 'seek', position: s.position + 30 })} aria-label="Forward 30 seconds">
                <Icon name="forward30" size={20} />
              </button>
            </div>
            <div className="cast-seek">
              <span className="small">{formatClock(pos)}</span>
              <input
                type="range"
                min={0}
                max={Math.max(1, Math.floor(s.duration))}
                value={Math.floor(pos)}
                onChange={(e) => setDragging({ id: s.id, t: Number(e.target.value) })}
                onPointerUp={() => {
                  if (dragging) control(s, { action: 'seek', position: dragging.t }).finally(() => setDragging(null))
                }}
                onKeyUp={() => {
                  if (dragging) control(s, { action: 'seek', position: dragging.t }).finally(() => setDragging(null))
                }}
                aria-label="Position"
              />
              <span className="small muted">{s.duration ? formatClock(s.duration) : ''}</span>
            </div>
            {s.device.kind === 'chromecast' && s.volume >= 0 && (
              <input
                className="cast-volume"
                type="range"
                min={0}
                max={100}
                defaultValue={Math.round(s.volume * 100)}
                onPointerUp={(e) => control(s, { action: 'volume', level: Number((e.target as HTMLInputElement).value) / 100 })}
                onKeyUp={(e) => control(s, { action: 'volume', level: Number((e.target as HTMLInputElement).value) / 100 })}
                aria-label="TV volume"
                title="TV volume"
              />
            )}
            <button className="btn btn-glass" onClick={() => stop(s)}>
              Stop
            </button>
          </div>
        )
      })}
      {error && <div className="form-error small">{error}</div>}
    </div>
  )
}
