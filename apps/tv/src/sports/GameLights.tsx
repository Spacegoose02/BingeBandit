import React, { useCallback, useEffect, useState } from 'react'
import { Modal, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api } from '../api'
import { Button, Focusable } from '../components'
import { scaled, colors, font } from '../theme'
import type { SportsTeam } from '../types'

interface LightsPerson {
  enabled?: boolean
  off?: boolean
  skip?: string[]
  area?: string
  source: string
  delays: Record<string, number>
  opponent?: string
  colours: Record<string, [string, string]>
  devices?: Record<string, unknown> // each TV's own switch and room, set in its Settings
}

interface LightsState {
  paired: boolean
  person: LightsPerson
  delay: number
  defaultDelay: number
  sources: string[]
  areas: { id: string; name: string; type: string }[]
  lastPlay?: { text: string; secondsAgo: number }
  error?: string
}

const opponentNames: Record<string, string> = { red: 'a dim red pulse', colour: 'a dim pulse in their colours', off: 'nothing' }

/**
 * This person's Game Lights: start / stop, which teams, the room, how they're watching and
 * its delay (measured by pressing OK as a play shows on the TV), and what
 * the other team's scores do. Team colours are on My Game Lights on the web.
 */
export default function GameLights({ teams, onClose }: { teams: SportsTeam[]; onClose: () => void }) {
  const base = '/api/plugins/game-lights/me'
  const [state, setState] = useState<LightsState | null>(null)
  const [msg, setMsg] = useState('')
  const load = useCallback(() => {
    api.get<LightsState>(`${base}/state`).then(
      (s) => {
        // Older plugins leave out an empty delays or colours.
        setState({ ...s, person: { ...s.person, delays: s.person.delays ?? {}, colours: s.person.colours ?? {} }, areas: s.areas ?? [], sources: s.sources ?? [] })
        if (s.error) setMsg(s.error)
        else if (!s.paired) setMsg('An admin still needs to pair the Hue bridge on the Game Lights setup page.')
      },
      (e) => setMsg(e.message),
    )
  }, [])
  useEffect(() => {
    load()
    const t = setInterval(load, 5000) // the newest play, for measuring the delay
    return () => clearInterval(t)
  }, [load])
  const save = (p: LightsPerson) => {
    setState((s) => (s ? { ...s, person: p, delay: p.delays[p.source] ?? s.defaultDelay } : s))
    api.put(`${base}/settings`, p).then(() => setMsg('Saved.'), (e) => setMsg(e.message))
  }
  const cycle = <T,>(list: T[], now: T) => list[(list.indexOf(now) + 1) % list.length]
  if (!state)
    return (
      <Modal transparent onRequestClose={onClose}>
        <View style={st.scrim}>
          <Text style={st.muted}>{msg || 'Loading…'}</Text>
          <Button label="Close" preferFocus onPress={onClose} />
        </View>
      </Modal>
    )
  const p = state.person
  const areas = [{ id: '', name: "The house's game lights" }, ...state.areas.map((a) => ({ id: a.id, name: `${a.name} (${a.type === 'Zone' ? 'zone' : 'room'})` }))]
  const areaName = areas.find((a) => a.id === (p.area ?? ''))?.name ?? areas[0].name
  const setDelay = (d: number) => save({ ...p, delays: { ...p.delays, [p.source]: Math.max(0, Math.min(300, d)) } })
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <ScrollView style={st.lightsBox} contentContainerStyle={st.lightsBody}>
          <Text style={st.sectionTitle}>Game Lights</Text>
          {!!msg && <Text style={st.small}>{msg}</Text>}
          <Focusable hasTVPreferredFocus onPress={() => save({ ...p, off: !p.off })} style={st.lightsOption}>
            <Text style={st.name}>{p.off ? 'Stopped: OK to start' : 'Running: OK to stop'}</Text>
            <Text style={st.small}>Stop the lights for a while without turning Game Lights off.</Text>
          </Focusable>
          {teams.map((t) => {
            const key = `${t.league}/${t.id}`
            const on = !(p.skip ?? []).includes(key)
            return (
              <View key={key} style={st.lightsOption}>
                <Text style={st.name}>{t.name}</Text>
                <View style={st.lightsButtons}>
                  <Button
                    small
                    label={on ? 'Lights on: OK to leave out' : 'Left out: OK to add'}
                    onPress={() => save({ ...p, skip: on ? [...(p.skip ?? []), key] : (p.skip ?? []).filter((k) => k !== key) })}
                  />
                  <Button
                    small
                    label="Test a touchdown"
                    onPress={() => api.post(`${base}/test`, { effect: 'touchdown', team: t }).then(() => setMsg('Watch the lights…'), (e) => setMsg(e.message))}
                  />
                </View>
              </View>
            )
          })}
          {p.devices ? (
            <View style={st.lightsOption}>
              <Text style={st.name}>Room</Text>
              <Text style={st.small}>Each TV turns Game Lights on and picks its room in Settings, This TV.</Text>
            </View>
          ) : (
            <Focusable onPress={() => save({ ...p, area: cycle(areas.map((a) => a.id), p.area ?? '') })} style={st.lightsOption}>
              <Text style={st.name}>Room: {areaName}</Text>
              <Text style={st.small}>The room you watch in. OK for the next one.</Text>
            </Focusable>
          )}
          <Focusable onPress={() => save({ ...p, source: cycle(state.sources, p.source) })} style={st.lightsOption}>
            <Text style={st.name}>Watching on: {p.source}</Text>
            <Text style={st.small}>Each way of watching keeps its own delay. OK for the next one.</Text>
          </Focusable>
          <View style={st.lightsOption}>
            <Text style={st.name}>Wait {state.delay} seconds after ESPN</Text>
            <View style={st.lightsButtons}>
              <Button small label="− 5" onPress={() => setDelay(state.delay - 5)} />
              <Button small label="− 1" onPress={() => setDelay(state.delay - 1)} />
              <Button small label="+ 1" onPress={() => setDelay(state.delay + 1)} />
              <Button small label="+ 5" onPress={() => setDelay(state.delay + 5)} />
            </View>
            <Text style={st.small}>
              {state.lastPlay ? `Newest play, ${state.lastPlay.secondsAgo} s ago: ${state.lastPlay.text}` : 'During a game, the newest play shows here.'}
            </Text>
            <Button
              small
              primary
              label="That play just showed on my TV"
              onPress={() =>
                api.post<{ delay: number; source: string }>(`${base}/calibrate`).then(
                  (r) => {
                    setMsg(`Delay for ${r.source} set to ${r.delay} s.`)
                    load()
                  },
                  (e) => setMsg(e.message),
                )
              }
            />
          </View>
          <Focusable onPress={() => save({ ...p, opponent: cycle(['red', 'colour', 'off'], p.opponent || 'red') })} style={st.lightsOption}>
            <Text style={st.name}>When the other team scores: {opponentNames[p.opponent || 'red']}</Text>
          </Focusable>
          <View style={st.lightsButtons}>
            <Button small label="Close" onPress={onClose} />
          </View>
          <Text style={st.small}>Your own team colours are on My Game Lights, under Account on the web.</Text>
        </ScrollView>
      </View>
    </Modal>
  )
}

const st = StyleSheet.create(scaled({
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  lightsBox: { flexGrow: 0, width: 1100, maxHeight: '92%', backgroundColor: colors.surface, borderRadius: 22, borderWidth: 1, borderColor: colors.border },
  lightsBody: { padding: 40 },
  lightsOption: { backgroundColor: colors.surface2, borderRadius: 18, padding: 24, marginTop: 18 },
  lightsButtons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginVertical: 14 },
  sectionTitle: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginBottom: 14 },
  muted: { color: colors.muted, fontSize: font.body, maxWidth: 1300, marginTop: 10 },
  small: { color: colors.muted, fontSize: font.small },
  name: { flex: 1, color: colors.muted, fontSize: font.body },
}))
