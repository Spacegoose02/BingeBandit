import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { Icon } from './Icon'

export interface TogetherState {
  itemId: number
  paused: boolean
  position: number
  at: number // server time, ms
  rev: number
  by: number
  byName?: string
}

export interface TogetherRoom {
  id: string
  host: number
  state: TogetherState
  members: { userId: number; name: string }[]
  now: number // server time when this was sent, ms
}

/** Where the room's playback is right now, in seconds (skew: server clock minus ours, ms). */
export function roomPosition(s: TogetherState, skew: number): number {
  return s.paused ? s.position : s.position + Math.max(0, Date.now() + skew - s.at) / 1000
}

export const togetherLink = (room: TogetherRoom) => `${window.location.origin}/play/${room.state.itemId}?together=${room.id}`

/**
 * Keeps a player in a Watch Together room: applies everyone else's changes
 * as they arrive and returns send() for this viewer's own play, pause, seek
 * and next-item changes.
 */
export function useTogether(roomId: string | null, onRemote: (s: TogetherState, skew: number, room: TogetherRoom) => void) {
  const [room, setRoom] = useState<TogetherRoom | null>(null)
  const [ended, setEnded] = useState(false)
  const lastRev = useRef(0)
  const skew = useRef(0)
  const remote = useRef(onRemote)
  remote.current = onRemote

  useEffect(() => {
    setRoom(null)
    setEnded(false)
    if (!roomId) return
    lastRev.current = 0
    const es = new EventSource(`/api/together/${roomId}/events`)
    es.onmessage = (e) => {
      const r = JSON.parse(e.data) as TogetherRoom
      skew.current = r.now - Date.now()
      setRoom(r)
      if (r.state.rev > lastRev.current) {
        const first = lastRev.current === 0
        lastRev.current = r.state.rev
        if (!first) remote.current(r.state, skew.current, r)
      }
    }
    es.onerror = () => {
      // EventSource reconnects by itself; a room that is gone answers 404 and stops it.
      if (es.readyState === EventSource.CLOSED) setEnded(true)
    }
    return () => es.close()
  }, [roomId])

  const send = useCallback(
    (s: { paused: boolean; position: number; itemId?: number }) => {
      if (!roomId) return
      api
        .post<TogetherRoom>(`/api/together/${roomId}/state`, s)
        .then((r) => {
          lastRev.current = Math.max(lastRev.current, r.state.rev)
          setRoom(r)
        })
        .catch(() => undefined)
    },
    [roomId],
  )

  /** The room's position now, by the server's clock. */
  const position = useCallback(() => (room ? roomPosition(room.state, skew.current) : 0), [room])

  return { room, ended, send, position }
}

/** The Watch Together panel in the player: start a room, share it, invite, leave. */
export function TogetherPanel({
  itemId,
  room,
  roomId,
  onStart,
  onLeave,
}: {
  itemId: number
  room: TogetherRoom | null
  roomId: string | null
  onStart: () => Promise<void>
  onLeave: () => void
}) {
  const { user } = useAuth()
  const [people, setPeople] = useState<{ id: number; name: string }[] | null>(null)
  const [picked, setPicked] = useState<number[]>([])
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setPeople(null)
    if (!roomId) return
    api.get<{ id: number; name: string }[]>(`/api/together/${roomId}/people`).then(setPeople, () => setPeople([]))
  }, [roomId, itemId])

  if (!roomId) {
    return (
      <div className="menu player-menu together-panel">
        <div className="menu-section">Watch together</div>
        <p className="muted small">
          Watch with someone else on this server, each on your own screen. Play, pause and skipping stay in step for everyone.
        </p>
        <button
          className="btn btn-primary btn-sm"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            await onStart().catch((e) => setMsg(e.message))
            setBusy(false)
          }}
        >
          <Icon name="users" size={16} /> Start a watch party
        </button>
        {msg && <div className="form-error small">{msg}</div>}
      </div>
    )
  }

  const others = (people ?? []).filter((p) => !room?.members.some((m) => m.userId === p.id))
  const invite = async () => {
    setMsg('')
    try {
      await api.post(`/api/together/${roomId}/invite`, { userIds: picked })
      setMsg(picked.length === 1 ? 'Invitation sent.' : 'Invitations sent.')
      setPicked([])
    } catch (e) {
      setMsg((e as Error).message)
    }
  }

  return (
    <div className="menu player-menu together-panel">
      <div className="menu-section">Watching together</div>
      <div className="together-members">
        {(room?.members ?? []).map((m) => (
          <span key={m.userId} className="pill">
            {m.name}
            {m.userId === user?.id && ' (you)'}
          </span>
        ))}
      </div>
      {room && (
        <button
          className="btn btn-glass btn-sm"
          onClick={() => {
            navigator.clipboard?.writeText(togetherLink(room))
            setMsg('Link copied. Anyone signed in to this server who can watch this can join with it.')
          }}
        >
          <Icon name="link" size={16} /> Copy invite link
        </button>
      )}
      {others.length > 0 && (
        <>
          <div className="menu-section">Invite</div>
          {others.map((p) => (
            <label key={p.id} className="check small">
              <input
                type="checkbox"
                checked={picked.includes(p.id)}
                onChange={(e) => setPicked(e.target.checked ? [...picked, p.id] : picked.filter((x) => x !== p.id))}
              />
              {p.name}
            </label>
          ))}
          <button className="btn btn-primary btn-sm" disabled={!picked.length} onClick={invite}>
            Send invitation
          </button>
        </>
      )}
      {people && others.length === 0 && (
        <p className="muted small">Only people who accept recommendations from others can be invited by name. Share the link with anyone else.</p>
      )}
      {msg && <div className="muted small">{msg}</div>}
      <button className="btn btn-glass btn-sm danger" onClick={onLeave}>
        Leave the watch party
      </button>
    </div>
  )
}

interface Invite {
  room: string
  itemId: number
  title: string
  from: string
}

/** Shows invitations to watch parties, checked every 20 seconds. */
export function TogetherInvites() {
  const [invites, setInvites] = useState<Invite[]>([])
  const nav = useNavigate()
  useEffect(() => {
    let stopped = false
    const check = () =>
      api.get<Invite[]>('/api/together/invites').then((l) => !stopped && setInvites(l), () => undefined)
    check()
    const t = window.setInterval(() => document.visibilityState === 'visible' && check(), 20000)
    return () => {
      stopped = true
      window.clearInterval(t)
    }
  }, [])
  if (!invites.length) return null
  const decline = (i: Invite) => {
    setInvites((l) => l.filter((x) => x.room !== i.room))
    api.del(`/api/together/${i.room}/invite`).catch(() => undefined)
  }
  return (
    <div className="together-invites">
      {invites.map((i) => (
        <div key={i.room} className="notice together-invite">
          <Icon name="users" size={18} />
          <span className="grow">
            <strong>{i.from}</strong> invited you to watch <strong>{i.title}</strong> together.
          </span>
          <button className="btn btn-primary btn-sm" onClick={() => nav(`/play/${i.itemId}?together=${i.room}`)}>
            Join
          </button>
          <button className="btn btn-glass btn-sm" onClick={() => decline(i)}>
            Not now
          </button>
        </div>
      ))}
    </div>
  )
}
