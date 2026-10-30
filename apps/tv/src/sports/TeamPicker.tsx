import React, { useEffect, useMemo, useState } from 'react'
import { ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { api } from '../api'
import { Button, Focusable } from '../components'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { SportsLeague, SportsTeam } from '../types'
import { Logo } from './parts'

/**
 * Following teams: what's followed up top (OK drops one), then a league's
 * teams with their abbreviations; OK stars or unstars one. Followed teams get
 * a star across the Sports Hub and their own rows under My Teams.
 */
export default function TeamPicker({ leagues, chosen, onDone }: { leagues: SportsLeague[]; chosen: SportsTeam[]; onDone: (teams: SportsTeam[] | null) => void }) {
  const pickable = useMemo(() => leagues.filter((l) => l.sport !== 'mma'), [leagues])
  const [league, setLeague] = useState(chosen[0]?.league ?? pickable[0].id)
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
  const leagueName = (id: string) => leagues.find((l) => l.id === id)?.name ?? id
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
        <Text style={st.title}>Favorite teams</Text>
        <Button label={`Save ${picked.length} ${picked.length === 1 ? 'team' : 'teams'}`} primary preferFocus onPress={save} />
        <Button label="Cancel" onPress={() => onDone(null)} />
      </View>
      <Text style={st.hint}>Followed teams get a star in the Sports Hub and their own rows under My Teams. Pick a league, then a team to follow it. Again to unfollow.</Text>
      {!!error && <Text style={st.error}>{error}</Text>}
      {picked.length > 0 && (
        <View style={st.grid}>
          {picked.map((t) => (
            <Focusable key={key(t)} onPress={() => toggle(t)} style={StyleSheet.flatten([st.chip, st.chipOn])}>
              <Text style={st.chipOnText}>
                ★ <Text style={st.chipLeague}>{leagueName(t.league)}</Text> {t.name} ✕
              </Text>
            </Focusable>
          ))}
        </View>
      )}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.leagues}>
        {pickable.map((l) => (
          <Focusable key={l.id} onPress={() => setLeague(l.id)} style={StyleSheet.flatten([st.chip, l.id === league && st.chipOn])}>
            <Text style={[st.chipText, l.id === league && st.chipOnText]}>{l.name}</Text>
          </Focusable>
        ))}
      </ScrollView>
      <TextInput style={st.search} value={query} onChangeText={setQuery} placeholder="Search teams" placeholderTextColor={colors.muted} />
      {!teams && !error && <Text style={st.hint}>Loading teams…</Text>}
      <View style={st.grid}>
        {shown.map((t) => {
          const on = has.has(key(t))
          return (
            <Focusable key={t.id} onPress={() => toggle(t)} style={StyleSheet.flatten([st.pick, on && st.pickOn])}>
              <Text style={[st.star, !on && st.starOff]}>★</Text>
              <Text style={st.abbr}>{t.abbr}</Text>
              <Logo team={t} size={32} />
              <Text style={[st.pickName, on && st.pickNameOn]} numberOfLines={1}>
                {t.name}
              </Text>
            </Focusable>
          )
        })}
      </View>
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  screen: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: isPhone ? 160 : 60 },
  head: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', marginBottom: 10 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800', marginRight: 30 },
  hint: { color: colors.muted, fontSize: font.small, marginVertical: 10, maxWidth: 1400 },
  error: { color: colors.danger, fontSize: font.small, marginBottom: 14 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 8 },
  leagues: { paddingVertical: 14 },
  chip: { borderRadius: 10, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.surface, paddingVertical: 10, paddingHorizontal: 20, marginRight: 14, marginBottom: 12 },
  chipOn: { borderColor: colors.accent, backgroundColor: 'rgba(45,212,191,0.12)' },
  chipText: { color: colors.text, fontSize: font.small },
  chipOnText: { color: colors.accent, fontSize: font.small },
  chipLeague: { fontWeight: '800' },
  search: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.body, borderRadius: 14, paddingHorizontal: 24, paddingVertical: 14, marginVertical: 12, maxWidth: 900 },
  pick: { width: isPhone ? 330 : 400, flexDirection: 'row', alignItems: 'center', backgroundColor: colors.surface, borderRadius: 12, borderWidth: 1, borderColor: colors.border, paddingVertical: 10, paddingHorizontal: 14, marginRight: 16, marginBottom: 14 },
  pickOn: { borderColor: colors.accent, backgroundColor: 'rgba(45,212,191,0.10)' },
  star: { color: colors.accent, fontSize: 20, width: 26 },
  starOff: { color: 'transparent' },
  abbr: { color: colors.muted, fontSize: 18, fontWeight: '800', width: 64 },
  pickName: { flex: 1, color: colors.text, fontSize: font.small, marginLeft: 12 },
  pickNameOn: { color: colors.accent },
}))
