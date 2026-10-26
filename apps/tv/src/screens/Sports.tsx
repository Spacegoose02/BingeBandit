import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Image, Modal, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { api } from '../api'
import { Button, Center, Focusable } from '../components'
import { useNav, useScreen } from '../nav'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { SportsGame, SportsHome, SportsLeague, SportsSide, SportsTeam } from '../types'

function startLabel(iso: string) {
  const d = new Date(iso)
  const time = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
  if (d.toDateString() === new Date().toDateString()) return `Today ${time}`
  if (d.toDateString() === new Date(Date.now() + 86400000).toDateString()) return `Tomorrow ${time}`
  return `${d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })} ${time}`
}

/** The Sports tab: games of the teams this person follows, and picking them. */
export default function Sports() {
  const { active } = useScreen()
  const [data, setData] = useState<SportsHome | null>(null)
  const [error, setError] = useState('')
  const [picking, setPicking] = useState(false)
  // The Game Lights button only shows when the server runs the plugin.
  const [lightsPlugin, setLightsPlugin] = useState(false)
  const [lights, setLights] = useState(false)
  useEffect(() => {
    api.get<{ id: string }[]>('/api/plugins').then((ps) => setLightsPlugin(ps.some((p) => p.id === 'game-lights')), () => undefined)
  }, [])
  const load = useCallback(() => {
    api.get<SportsHome>('/api/sports').then(
      (d) => {
        setData(d)
        setError(d.error ?? '')
      },
      (e) => setError(e.message),
    )
  }, [])
  // Scores refresh every half minute while the tab is on screen.
  useEffect(() => {
    if (!active || picking) return
    load()
    const t = setInterval(load, 30000)
    return () => clearInterval(t)
  }, [active, picking, load])

  if (!data) return <Center>{error ? <Text style={st.muted}>{error}</Text> : <Text style={st.muted}>Loading scores…</Text>}</Center>
  if (picking) {
    return (
      <TeamPicker
        leagues={data.leagues}
        chosen={data.teams}
        onDone={(teams) => {
          setPicking(false)
          if (teams) setData({ ...data, teams })
        }}
      />
    )
  }
  const leagueName = (id: string) => data.leagues.find((l) => l.id === id)?.name ?? id
  const sections: [string, SportsGame[]][] = [
    ['Live now', data.games.filter((g) => g.state === 'in')],
    ['Coming up', data.games.filter((g) => g.state === 'pre')],
    ['Results', data.games.filter((g) => g.state === 'post')],
  ]
  return (
    <ScrollView contentContainerStyle={st.screen}>
      <View style={st.head}>
        <Button label={data.teams.length ? 'Change teams' : 'Pick your teams'} primary={!data.teams.length} preferFocus onPress={() => setPicking(true)} />
        {lightsPlugin && data.teams.length > 0 && <Button label="Game Lights" onPress={() => setLights(true)} />}
        <Text style={st.teams} numberOfLines={2}>
          {data.teams.map((t) => t.shortName || t.name).join(' · ')}
        </Text>
      </View>
      {lights && <GameLights teams={data.teams} onClose={() => setLights(false)} />}
      {!!error && <Text style={st.error}>{error}</Text>}
      {data.teams.length === 0 && (
        <Text style={st.muted}>
          Pick the teams you follow, from the NFL and college football to the NBA, MLB, NHL and MLS. Their games and scores show up here, and you get a
          heads-up before each one starts.
        </Text>
      )}
      {data.teams.length > 0 && data.games.length === 0 && <Text style={st.muted}>No games for your teams from two days ago to a week from now.</Text>}
      {sections.map(
        ([title, games]) =>
          games.length > 0 && (
            <View key={title} style={st.section}>
              <Text style={st.sectionTitle}>{title}</Text>
              <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.row}>
                {games.map((g) => (
                  <GameCard key={g.id} game={g} league={leagueName(g.league)} />
                ))}
              </ScrollView>
            </View>
          ),
      )}
    </ScrollView>
  )
}

interface LightsPerson {
  off?: boolean
  area?: string
  source: string
  delays: Record<string, number>
  opponent?: string
  colours: Record<string, [string, string]>
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
 * This person's Game Lights: on or off, the room, how they're watching and
 * its delay (measured by pressing OK as a play shows on the TV), and what
 * the other team's scores do. Team colours are on My Game Lights on the web.
 */
function GameLights({ teams, onClose }: { teams: SportsTeam[]; onClose: () => void }) {
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
  const team = teams[0]
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <ScrollView style={st.lightsBox} contentContainerStyle={st.lightsBody}>
          <Text style={st.sectionTitle}>Game Lights</Text>
          {!!msg && <Text style={st.small}>{msg}</Text>}
          <Focusable hasTVPreferredFocus onPress={() => save({ ...p, off: !p.off })} style={st.lightsOption}>
            <Text style={st.name}>Lights for my teams: {p.off ? 'Off' : 'On'}</Text>
          </Focusable>
          <Focusable onPress={() => save({ ...p, area: cycle(areas.map((a) => a.id), p.area ?? '') })} style={st.lightsOption}>
            <Text style={st.name}>Room: {areaName}</Text>
            <Text style={st.small}>The room you watch in. OK for the next one.</Text>
          </Focusable>
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
            {team && (
              <Button
                small
                label={`Test a ${team.shortName || team.name} touchdown`}
                onPress={() => api.post(`${base}/test`, { effect: 'touchdown', team }).then(() => setMsg('Watch the lights…'), (e) => setMsg(e.message))}
              />
            )}
            <Button small label="Close" onPress={onClose} />
          </View>
          <Text style={st.small}>Your own team colours are on My Game Lights, under Account on the web.</Text>
        </ScrollView>
      </View>
    </Modal>
  )
}

function Logo({ team, size }: { team: SportsTeam; size: number }) {
  const s = { width: size, height: size }
  return team.logo ? (
    <Image source={{ uri: team.logo }} style={[s, st.logo]} resizeMode="contain" />
  ) : (
    <View style={[s, st.logoBlank, team.color ? { backgroundColor: `#${team.color}` } : null]} />
  )
}

/** One game; OK on a game with a Live TV channel tunes to it. */
function GameCard({ game: g, league }: { game: SportsGame; league: string }) {
  const nav = useNav()
  const channel = g.channels?.[0]
  const side = (s: SportsSide) => {
    const mine = g.mine?.includes(s.id)
    return (
      <View style={st.side}>
        <Logo team={s} size={56} />
        <Text style={[st.name, mine && st.nameMine]} numberOfLines={1}>
          {s.shortName || s.name}
          {!!s.record && <Text style={st.record}> {s.record}</Text>}
        </Text>
        {g.state !== 'pre' && <Text style={[st.score, (g.state === 'in' || s.winner) && st.scoreLead]}>{s.score}</Text>}
      </View>
    )
  }
  const status = g.state === 'pre' ? startLabel(g.start) : g.detail
  return (
    <Focusable onPress={() => channel && nav.push({ name: 'live', channelId: channel.id })} style={StyleSheet.flatten([st.card, g.state === 'in' && st.cardLive])}>
      <View style={st.cardHead}>
        <Text style={st.small}>{league}</Text>
        <Text style={[st.small, g.state === 'in' && st.live]}>{g.state === 'in' ? `● ${status}` : status}</Text>
      </View>
      {side(g.away)}
      {side(g.home)}
      <Text style={st.small} numberOfLines={1}>
        {channel ? `${isPhone ? 'Tap' : 'OK'} to watch on ${channel.name}` : g.broadcasts?.join(', ') ?? ''}
      </Text>
    </Focusable>
  )
}

/** League buttons, a search box and every team of the league; OK picks or drops one. */
function TeamPicker({ leagues, chosen, onDone }: { leagues: SportsLeague[]; chosen: SportsTeam[]; onDone: (teams: SportsTeam[] | null) => void }) {
  const [league, setLeague] = useState(chosen[0]?.league ?? leagues[0].id)
  const [teams, setTeams] = useState<SportsTeam[] | null>(null)
  const [picked, setPicked] = useState(chosen)
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    setTeams(null)
    api.get<SportsTeam[]>(`/api/sports/${league}/teams`).then(setTeams, (e) => setError(e.message))
  }, [league])
  const key = (t: SportsTeam) => `${t.league}/${t.id}`
  const has = useMemo(() => new Set(picked.map(key)), [picked])
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (teams ?? []).filter((t) => !q || [t.name, t.shortName, t.abbr].some((n) => n?.toLowerCase().includes(q)))
  }, [teams, query])
  const toggle = (t: SportsTeam) => setPicked(has.has(key(t)) ? picked.filter((p) => key(p) !== key(t)) : [...picked, t])
  const save = async () => {
    setError('')
    try {
      onDone(await api.put<SportsTeam[]>('/api/sports/teams', picked.map((t) => ({ league: t.league, id: t.id }))))
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <ScrollView contentContainerStyle={st.screen}>
      <View style={st.head}>
        <Button label={`Save ${picked.length} ${picked.length === 1 ? 'team' : 'teams'}`} primary preferFocus onPress={save} />
        <Button label="Cancel" onPress={() => onDone(null)} />
      </View>
      {!!error && <Text style={st.error}>{error}</Text>}
      {picked.length > 0 && (
        <Text style={st.teams} numberOfLines={2}>
          {picked.map((t) => t.name).join(' · ')}
        </Text>
      )}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.leagues}>
        {leagues.map((l) => (
          <Focusable key={l.id} onPress={() => setLeague(l.id)} style={StyleSheet.flatten([st.league, l.id === league && st.leagueOn])}>
            <Text style={[st.leagueText, l.id === league && st.leagueTextOn]}>{l.name}</Text>
          </Focusable>
        ))}
      </ScrollView>
      <TextInput style={st.search} value={query} onChangeText={setQuery} placeholder="Search teams" placeholderTextColor={colors.muted} />
      {!teams && !error && <Text style={st.muted}>Loading teams…</Text>}
      <View style={st.grid}>
        {shown.map((t) => (
          <Focusable key={t.id} onPress={() => toggle(t)} style={StyleSheet.flatten([st.pick, has.has(key(t)) && st.pickOn])}>
            <Logo team={t} size={44} />
            <Text style={st.pickName} numberOfLines={1}>
              {t.name}
            </Text>
            {has.has(key(t)) && <Text style={st.check}>✓</Text>}
          </Focusable>
        ))}
      </View>
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  lightsBox: { flexGrow: 0, width: 1100, maxHeight: '92%', backgroundColor: colors.surface, borderRadius: 22, borderWidth: 1, borderColor: colors.border },
  lightsBody: { padding: 40 },
  lightsOption: { backgroundColor: colors.surface2, borderRadius: 18, padding: 24, marginTop: 18 },
  lightsButtons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginVertical: 14 },
  screen: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: isPhone ? 160 : 60 },
  head: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', marginBottom: 20 },
  teams: { color: colors.muted, fontSize: font.small, flex: 1, marginVertical: 10 },
  muted: { color: colors.muted, fontSize: font.body, maxWidth: 1300, marginTop: 10 },
  error: { color: colors.danger, fontSize: font.small, marginBottom: 14 },
  section: { marginTop: 24 },
  sectionTitle: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginBottom: 14 },
  row: { paddingVertical: 10, paddingRight: 40 },
  card: { width: isPhone ? 560 : 480, backgroundColor: colors.surface, borderRadius: 20, padding: 24, marginRight: 26 },
  cardLive: { backgroundColor: colors.surface2 },
  cardHead: { flexDirection: 'row', justifyContent: 'space-between', marginBottom: 12 },
  small: { color: colors.muted, fontSize: font.small },
  live: { color: colors.danger, fontWeight: '700' },
  side: { flexDirection: 'row', alignItems: 'center', marginBottom: 12 },
  logo: { marginRight: 16 },
  logoBlank: { borderRadius: 999, backgroundColor: colors.surface3, marginRight: 16 },
  name: { flex: 1, color: colors.muted, fontSize: font.body },
  nameMine: { color: colors.text, fontWeight: '700' },
  record: { color: colors.muted, fontSize: font.small, fontWeight: '400' },
  score: { color: colors.muted, fontSize: font.heading, fontWeight: '800', marginLeft: 12 },
  scoreLead: { color: colors.text },
  leagues: { paddingVertical: 10 },
  league: { backgroundColor: colors.surface2, borderRadius: 999, paddingVertical: 12, paddingHorizontal: 26, marginRight: 14 },
  leagueOn: { backgroundColor: colors.accent },
  leagueText: { color: colors.text, fontSize: font.small, fontWeight: '700' },
  leagueTextOn: { color: colors.accentText },
  search: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.body, borderRadius: 14, paddingHorizontal: 24, paddingVertical: 14, marginVertical: 16, maxWidth: 900 },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  pick: { width: isPhone ? 330 : 420, flexDirection: 'row', alignItems: 'center', backgroundColor: colors.surface, borderRadius: 16, padding: 14, marginRight: 18, marginBottom: 18 },
  pickOn: { backgroundColor: colors.surface3 },
  pickName: { flex: 1, color: colors.text, fontSize: font.small, marginLeft: 14 },
  check: { color: colors.accent, fontSize: font.body, fontWeight: '800', marginLeft: 8 },
}))
