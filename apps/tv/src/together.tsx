import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Modal, Pressable, StyleSheet, Text, View } from 'react-native'
import { ApiError, api } from './api'
import { Button } from './components'
import { useNav } from './nav'
import { quietNow } from './quiet'
import { colors, font, scaled, quietPress } from './theme'

// Watch Together on the TV app: the same rooms as the web player. React
// Native has no EventSource, so the room is read every second and a half,
// which keeps everyone within about two seconds.

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

const POLL_MS = 1500

/** Where the room's playback is right now, in seconds (skew: server clock minus ours, ms). */
export function roomPosition(s: TogetherState, skew: number): number {
  return s.paused ? s.position : s.position + Math.max(0, Date.now() + skew - s.at) / 1000
}

/**
 * Keeps a player in a room: calls onRemote with each change someone else
 * made (and once with where the room is on joining), and returns send() for
 * this viewer's own play, pause, seek and next-episode changes.
 */
export function useTogether(roomId: string | null, onRemote: (s: TogetherState, skew: number) => void) {
  const [room, setRoom] = useState<TogetherRoom | null>(null)
  const [ended, setEnded] = useState(false)
  const lastRev = useRef(0)
  const skew = useRef(0)
  const remote = useRef(onRemote)
  remote.current = onRemote

  useEffect(() => {
    setRoom(null)
    setEnded(false)
    lastRev.current = 0
    if (!roomId) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const look = async () => {
      try {
        const sent = Date.now()
        const r = await api.get<TogetherRoom>(`/api/together/${roomId}?watching=1`)
        if (stopped) return
        // Half the round trip is how old the server's clock reading is.
        skew.current = r.now - (sent + Date.now()) / 2
        setRoom(r)
        if (r.state.rev > lastRev.current) {
          lastRev.current = r.state.rev
          remote.current(r.state, skew.current)
        }
      } catch (e) {
        if (stopped) return
        if (e instanceof ApiError && (e.status === 404 || e.status === 403)) {
          setEnded(true)
          return
        }
      }
      timer = setTimeout(look, POLL_MS)
    }
    look()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [roomId])

  const send = useCallback(
    (s: { paused: boolean; position: number; itemId?: number }) => {
      if (!roomId) return
      api.post<TogetherRoom>(`/api/together/${roomId}/state`, s).then(
        (r) => {
          lastRev.current = Math.max(lastRev.current, r.state.rev)
          setRoom(r)
        },
        () => undefined,
      )
    },
    [roomId],
  )
  return { room, ended, send }
}

/** The player's Watch Together panel: start a room, invite people, see who's in, leave. */
export function TogetherPanel({
  roomId,
  room,
  onStart,
  onLeave,
  onClose,
}: {
  roomId: string | null
  room: TogetherRoom | null
  onStart: () => Promise<void>
  onLeave: () => void
  onClose: () => void
}) {
  const [people, setPeople] = useState<{ id: number; name: string }[] | null>(null)
  const [invited, setInvited] = useState<number[]>([])
  const [msg, setMsg] = useState('')
  useEffect(() => {
    if (!roomId) return
    api.get<{ id: number; name: string }[]>(`/api/together/${roomId}/people`).then(setPeople, () => setPeople([]))
  }, [roomId])
  const inRoom = new Set(room?.members.map((m) => m.userId))
  return (
    <View style={st.panel}>
      <Text style={st.title}>Watch together</Text>
      {!roomId ? (
        <>
          <Text style={st.hint}>Watch with someone else on this server, each on your own screen. Play, pause and skipping stay in step for everyone.</Text>
          <View style={st.row}>
            <Button small primary preferFocus label="Start watching together" onPress={() => onStart().catch((e) => setMsg((e as Error).message))} />
            <Button small label="Close" onPress={onClose} />
          </View>
        </>
      ) : (
        <>
          <Text style={st.hint}>Watching with: {room?.members.map((m) => m.name).join(', ') || 'just you so far'}</Text>
          <Text style={st.hint}>Invite someone and it pops up on their TV, phone or browser.</Text>
          <View style={st.row}>
            {people?.length === 0 && <Text style={st.hint}>Nobody else can play this title here.</Text>}
            {people
              ?.filter((p) => !inRoom.has(p.id))
              .map((p, i) => (
                <Button
                  key={p.id}
                  small
                  preferFocus={i === 0}
                  primary={invited.includes(p.id)}
                  label={invited.includes(p.id) ? `${p.name} ✓ invited` : `Invite ${p.name}`}
                  onPress={() =>
                    api.post(`/api/together/${roomId}/invite`, { userIds: [p.id] }).then(
                      () => setInvited((v) => [...v, p.id]),
                      (e) => setMsg(e.message),
                    )
                  }
                />
              ))}
          </View>
          <View style={st.row}>
            <Button small label="Leave the room" onPress={onLeave} />
            <Button small label="Close" preferFocus={!people?.length} onPress={onClose} />
          </View>
        </>
      )}
      {!!msg && <Text style={st.error}>{msg}</Text>}
    </View>
  )
}

interface Invite {
  room: string
  itemId: number
  title: string
  from: string
}

/**
 * Invitations to watch together pop up over anything, every few seconds'
 * look, so pairing a TV is one press: Join opens the player in the room.
 */
export function TogetherInvites({ playing }: { playing: boolean }) {
  const playingRef = useRef(playing)
  playingRef.current = playing
  const nav = useNav()
  const [invite, setInvite] = useState<Invite | null>(null)
  const dismissed = useRef(new Set<string>())
  useEffect(() => {
    const look = () =>
      api.get<Invite[]>('/api/together/invites').then((list) => {
        if (quietNow(playingRef.current)) return
        setInvite((cur) => cur ?? list.find((i) => !dismissed.current.has(i.room)) ?? null)
      }, () => undefined)
    look()
    const t = setInterval(look, 10000)
    return () => clearInterval(t)
  }, [])
  if (!invite) return null
  const close = (decline: boolean) => {
    dismissed.current.add(invite.room)
    if (decline) api.del(`/api/together/${invite.room}/invite`).catch(() => undefined)
    setInvite(null)
  }
  return (
    <Modal transparent animationType="fade" onRequestClose={() => close(false)}>
      <Pressable android_disableSound={quietPress} style={st.scrim} onPress={() => close(false)}>
        <View style={st.invite}>
          <Text style={st.title}>{invite.from} wants to watch together</Text>
          <Text style={st.hint}>{invite.title}</Text>
          <View style={st.row}>
            <Button
              primary
              preferFocus
              label="Join"
              onPress={() => {
                close(false)
                nav.push({ name: 'player', id: invite.itemId, start: 0, together: invite.room })
              }}
            />
            <Button label="Not now" onPress={() => close(true)} />
          </View>
        </View>
      </Pressable>
    </Modal>
  )
}

const st = StyleSheet.create(scaled({
  panel: { position: 'absolute', left: 0, right: 0, bottom: 0, paddingHorizontal: 80, paddingTop: 30, paddingBottom: 50, backgroundColor: 'rgba(11,13,16,0.94)' },
  title: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 10 },
  error: { color: colors.danger, fontSize: font.small, marginTop: 10 },
  row: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginTop: 20 },
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.6)', alignItems: 'center', justifyContent: 'center' },
  invite: { backgroundColor: colors.surface, borderRadius: 22, padding: 40, minWidth: 800, maxWidth: 1100, borderWidth: 1, borderColor: colors.border },
}))
