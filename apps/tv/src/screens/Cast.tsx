import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, image } from '../api'
import { Button, Center } from '../components'
import { useNav } from '../nav'
import { colors, font, gutter, quietPress, scaled } from '../theme'
import type { CastDevice, CastSession } from '../types'
import { SeekBar } from './Player'

/** A position as a clock: 4:05, or 1:35:12 past an hour. */
function clock(t: number) {
  t = Math.max(0, Math.floor(t))
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = String(t % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

const kindLabel = (d: CastDevice) => (d.kind === 'chromecast' ? 'Chromecast' : 'TV (DLNA)')

/**
 * Plays a title on a TV in the house: a Chromecast, Google TV or DLNA smart
 * TV that the server finds on its network. The server sends the TV the
 * video, so this works on any phone; the phone is the remote after that.
 * With id, picks a TV for that title (from start); with session, opens the
 * remote for a cast already going.
 */
export default function CastScreen({ id, start = 0, session: sessionId }: { id?: number; start?: number; session?: string }) {
  const nav = useNav()
  const [session, setSession] = useState<CastSession | null>(null)
  const [checking, setChecking] = useState(true)

  // A cast of this title already going opens straight to its remote.
  useEffect(() => {
    api
      .get<{ sessions: CastSession[] }>('/api/cast/sessions')
      .then((r) => setSession(r.sessions.find((s) => (sessionId ? s.id === sessionId : s.itemId === id)) ?? null))
      .catch(() => undefined)
      .finally(() => setChecking(false))
  }, [id, sessionId])

  return (
    <View style={st.screen}>
      <Pressable android_disableSound={quietPress} onPress={nav.back} hitSlop={20} style={st.back}>
        <Text style={st.backText}>‹ Back</Text>
      </Pressable>
      {checking ? (
        <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
      ) : session ? (
        <Remote session={session} onChange={setSession} />
      ) : id ? (
        <Picker id={id} start={start} onStarted={setSession} />
      ) : (
        <Center><Text style={st.muted}>That cast has ended.</Text></Center>
      )}
    </View>
  )
}

function Picker({ id, start, onStarted }: { id: number; start: number; onStarted: (s: CastSession) => void }) {
  const [devices, setDevices] = useState<CastDevice[] | null>(null)
  const [looking, setLooking] = useState(false)
  const [starting, setStarting] = useState('')
  const [error, setError] = useState('')

  const look = useCallback((refresh: boolean) => {
    setLooking(true)
    setError('')
    // A fresh look takes the server a few seconds.
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
      onStarted(await api.post<CastSession>('/api/cast/start', { deviceId: d.id, itemId: id, start: Math.floor(start) }))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setStarting('')
    }
  }

  return (
    <ScrollView contentContainerStyle={st.body}>
      <Text style={st.title}>Play on a TV</Text>
      {start > 15 && <Text style={st.muted}>From {clock(start)}</Text>}
      {devices === null && !error && <ActivityIndicator color={colors.accent} style={st.spinner} />}
      {devices?.length === 0 && (
        <Text style={st.muted}>
          No TVs found. The server looks for Chromecasts, Google TVs and DLNA smart TVs on its own network, so the TV needs to be on and on the same
          network as the server.
        </Text>
      )}
      {devices?.map((d) => (
        <Pressable
          key={d.id}
          android_disableSound={quietPress}
          disabled={!!starting}
          onPress={() => cast(d)}
          style={({ pressed }) => [st.device, pressed && st.pressed]}
        >
          <Text style={st.deviceName}>{d.name}</Text>
          <Text style={st.deviceKind}>{starting === d.id ? 'Starting…' : [kindLabel(d), d.model].filter(Boolean).join(' · ')}</Text>
        </Pressable>
      ))}
      {!!error && <Text style={st.error}>{error}</Text>}
      <View style={st.row}>
        <Button small label={looking ? 'Looking…' : 'Look again'} onPress={() => !looking && look(true)} />
      </View>
    </ScrollView>
  )
}

function Remote({ session, onChange }: { session: CastSession; onChange: (s: CastSession | null) => void }) {
  const nav = useNav()
  const [error, setError] = useState('')
  const [ended, setEnded] = useState(false)
  const [dragT, setDragT] = useState<number | null>(null)
  const busy = useRef(false)
  const s = session

  // Follow the TV every two seconds.
  useEffect(() => {
    if (ended) return
    const t = setInterval(() => {
      api.get<CastSession>(`/api/cast/sessions/${s.id}`).then(onChange, (e: { status?: number }) => {
        if (e.status === 404) setEnded(true)
      })
    }, 2000)
    return () => clearInterval(t)
  }, [s.id, ended, onChange])

  const control = async (body: Record<string, unknown>) => {
    if (busy.current) return
    busy.current = true
    setError('')
    try {
      onChange(await api.post<CastSession>(`/api/cast/sessions/${s.id}`, body))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      busy.current = false
    }
  }
  const stop = async () => {
    await api.del(`/api/cast/sessions/${s.id}`).catch(() => undefined)
    nav.back()
  }

  const playing = s.state === 'playing' || s.state === 'loading'
  const poster = image(s.poster, 'w342')
  const pos = dragT ?? s.position
  return (
    <ScrollView contentContainerStyle={st.body}>
      <View style={st.nowPlaying}>
        {poster && <Image source={poster} style={st.poster} />}
        <View style={st.flex}>
          <Text style={st.muted}>{ended ? 'Finished on' : s.state === 'loading' ? 'Starting on' : 'Playing on'} {s.device.name}</Text>
          <Text style={st.title}>{s.title}</Text>
          {!!s.subtitle && <Text style={st.sub}>{s.subtitle}</Text>}
        </View>
      </View>
      {ended ? (
        <>
          {!!s.error && <Text style={st.error}>{s.error}</Text>}
          <View style={st.row}><Button label="Done" primary onPress={nav.back} /></View>
        </>
      ) : (
        <>
          <SeekBar
            pct={s.duration ? Math.min(100, (s.position / s.duration) * 100) : 0}
            duration={s.duration}
            onDrag={setDragT}
            onSeek={(t) => control({ action: 'seek', position: t })}
          />
          <View style={st.times}>
            <Text style={st.time}>{clock(pos)}</Text>
            <Text style={st.time}>{s.duration ? `-${clock(s.duration - pos)}` : ''}</Text>
          </View>
          <View style={st.row}>
            <Button label="⟲ 10s" onPress={() => control({ action: 'seek', position: s.position - 10 })} />
            <Button primary label={playing ? '❚❚  Pause' : '▶  Play'} onPress={() => control({ action: playing ? 'pause' : 'play' })} />
            <Button label="30s ⟳" onPress={() => control({ action: 'seek', position: s.position + 30 })} />
          </View>
          {s.device.kind === 'chromecast' && s.volume >= 0 && (
            <View style={st.row}>
              <Button small label="Volume −" onPress={() => control({ action: 'volume', level: Math.max(0, s.volume - 0.05) })} />
              <Text style={st.volume}>{Math.round(s.volume * 100)}%</Text>
              <Button small label="Volume +" onPress={() => control({ action: 'volume', level: Math.min(1, s.volume + 0.05) })} />
            </View>
          )}
          {!!(error || s.error) && <Text style={st.error}>{error || s.error}</Text>}
          <View style={st.row}>
            <Button label="Stop playing on the TV" onPress={stop} />
          </View>
          <Text style={st.hint}>You can leave this screen; the TV keeps playing. Open the title again to get back here.</Text>
        </>
      )}
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  back: { alignSelf: 'flex-start', marginTop: 30, marginLeft: gutter },
  backText: { color: colors.accent, fontSize: font.body, fontWeight: '700' },
  body: { paddingHorizontal: gutter, paddingTop: 24, paddingBottom: 80 },
  flex: { flex: 1 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '900', marginBottom: 8 },
  sub: { color: colors.text, fontSize: font.body, opacity: 0.85 },
  muted: { color: colors.muted, fontSize: font.body, marginBottom: 12, lineHeight: 38 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 24, lineHeight: 34 },
  error: { color: colors.danger, fontSize: font.body, marginTop: 12 },
  spinner: { marginVertical: 30 },
  device: { backgroundColor: colors.surface2, borderColor: colors.border, borderWidth: 2, borderRadius: 18, padding: 26, marginBottom: 16 },
  pressed: { borderColor: colors.accent },
  deviceName: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  deviceKind: { color: colors.muted, fontSize: font.small, marginTop: 4 },
  row: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginTop: 24 },
  nowPlaying: { flexDirection: 'row', alignItems: 'center', marginBottom: 30 },
  poster: { width: 160, height: 240, borderRadius: 12, marginRight: 28 },
  times: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 8 },
  time: { color: colors.muted, fontSize: font.small },
  volume: { color: colors.text, fontSize: font.body, marginHorizontal: 16 },
}))
