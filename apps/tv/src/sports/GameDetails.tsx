import React, { useEffect, useState } from 'react'
import { ActivityIndicator, Modal, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api } from '../api'
import { Button, Focusable } from '../components'
import { scaled, colors, font } from '../theme'
import type { SportsGame, SportsSide, SportsSummary, SportsTeam } from '../types'
import { revealScore, scoreShown, useSpoilers } from './hub'
import { Logo, gameStatus, leaderText } from './parts'

type Tab = 'box' | 'stats' | 'leaders' | 'standings'
const tabNames: Record<Tab, string> = { box: 'Box score', stats: 'Stats', leaders: 'Leaders', standings: 'Standings' }

/**
 * Holding OK on a game: its box score, team and player stats, leaders and
 * standings, with Follow buttons for both teams, Watch and Multi-View when
 * it's on a channel, and Game Lights when one of your teams plays. While
 * spoiler-free, everything but the standings waits for "Show the score".
 */
export default function GameDetails({
  game,
  league,
  sport,
  followed,
  onFollow,
  onWatch,
  onMultiView,
  lights,
  onClose,
}: {
  game: SportsGame
  league: string
  sport?: string
  followed: SportsTeam[]
  onFollow: (team: SportsSide, follow: boolean) => void
  onWatch?: () => void
  onMultiView?: () => void
  /** Game Lights for this profile: running or stopped, and the switch; absent when it's off. */
  lights?: { running: boolean; toggle: () => void }
  onClose: () => void
}) {
  useSpoilers()
  const [tab, setTab] = useState<Tab>('box')
  const [sum, setSum] = useState<SportsSummary | null>(null)
  const [error, setError] = useState('')
  const fights = sport === 'mma'
  useEffect(() => {
    if (fights) return
    let on = true
    const load = () =>
      api.get<SportsSummary>(`/api/sports/${game.league}/games/${encodeURIComponent(game.id)}`).then(
        (s) => on && setSum(s),
        (e) => on && setError(e.message),
      )
    load()
    const t = game.state === 'in' ? setInterval(load, 20000) : undefined
    return () => {
      on = false
      if (t) clearInterval(t)
    }
  }, [game.league, game.id, game.state, fights])
  const g = sum?.game?.id ? { ...game, ...sum.game, channels: game.channels, mine: game.mine } : game
  const shown = scoreShown(g)
  const isFollowed = (s: SportsSide) => followed.some((t) => t.league === g.league && t.id === s.id)
  const title = `${g.away.name} at ${g.home.name}`
  const sub = [gameStatus(g), shown ? `${g.away.abbr || g.away.shortName} ${g.away.score ?? ''} · ${g.home.abbr || g.home.shortName} ${g.home.score ?? ''}` : '', league]
    .filter(Boolean)
    .join(' · ')
  const tabs: Tab[] = fights ? [] : ['box', 'stats', 'leaders', 'standings']
  const hidden = !shown && g.state !== 'pre' && tab !== 'standings'
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <View style={st.box}>
          <View style={st.head}>
            <View style={st.flex}>
              <Text style={st.title} numberOfLines={1}>
                {title}
              </Text>
              <Text style={st.sub}>{sub}</Text>
            </View>
            <View style={st.tabs}>
              {tabs.map((t, i) => (
                <Focusable key={t} hasTVPreferredFocus={i === 0} onFocus={() => setTab(t)} onPress={() => setTab(t)} style={StyleSheet.flatten([st.tab, tab === t && st.tabOn])}>
                  <Text style={[st.tabText, tab === t && st.tabTextOn]}>{tabNames[t].toUpperCase()}</Text>
                </Focusable>
              ))}
            </View>
          </View>
          <View style={st.buttons}>
            {!fights &&
              [g.away, g.home].map((s) => (
                <Button key={s.id} small label={`${isFollowed(s) ? '★ Following' : '☆ Follow'} ${s.shortName || s.name}`} onPress={() => onFollow(s, !isFollowed(s))} />
              ))}
            {onWatch && <Button small primary label="Watch" onPress={onWatch} preferFocus={fights} />}
            {onMultiView && <Button small label="Add to Multi-View" onPress={onMultiView} />}
            {lights && <Button small label={lights.running ? 'Game Lights: on' : 'Game Lights: stopped'} onPress={lights.toggle} />}
            {!shown && g.state !== 'pre' && <Button small label="Show the score" onPress={() => revealScore(g.id)} />}
            <Button small label="Close" onPress={onClose} />
          </View>
          <ScrollView style={st.body} contentContainerStyle={st.bodyIn}>
            {fights ? (
              <FightCard game={g} shown={shown} />
            ) : hidden ? (
              <Text style={st.muted}>Spoiler-free: the score, stats and leaders stay hidden until you choose Show the score.</Text>
            ) : !sum ? (
              error ? <Text style={st.muted}>{error}</Text> : <ActivityIndicator color={colors.accent} />
            ) : tab === 'box' ? (
              <BoxScore sum={sum} game={g} sport={sport} />
            ) : tab === 'stats' ? (
              <TeamStats sum={sum} game={g} />
            ) : tab === 'leaders' ? (
              <Leaders sum={sum} game={g} />
            ) : (
              <StandingsTables sum={sum} game={g} />
            )}
          </ScrollView>
        </View>
      </View>
    </Modal>
  )
}

function FightCard({ game: g, shown }: { game: SportsGame; shown: boolean }) {
  return (
    <View>
      {[g.away, g.home].map((s) => (
        <View key={s.id} style={st.lineRow}>
          <Logo team={s} size={40} />
          <Text style={[st.lineName, shown && s.winner && st.strong]}>
            {s.name}
            {!!s.record && <Text style={st.muted}>  {s.record}</Text>}
            {shown && s.winner ? '  ✓ Won' : ''}
          </Text>
        </View>
      ))}
      <Text style={st.muted}>{[g.detail, g.venue, g.broadcasts?.join(', ')].filter(Boolean).join(' · ')}</Text>
    </View>
  )
}

function BoxScore({ sum, game: g, sport }: { sum: SportsSummary; game: SportsGame; sport?: string }) {
  // Baseball always shows nine innings, filling in as they're played.
  const periods = Math.max(g.away.lines?.length ?? 0, g.home.lines?.length ?? 0, sport === 'baseball' ? 9 : 0)
  const cols = Array.from({ length: periods }, (_, i) => String(i + 1))
  const total = sport === 'baseball' ? 'R' : 'T'
  return (
    <View>
      <View style={st.lineRow}>
        <View style={st.lineTeam} />
        {cols.map((c) => (
          <Text key={c} style={[st.cell, st.cellHead]}>
            {c}
          </Text>
        ))}
        <Text style={[st.cell, st.cellHead, st.total]}>{total}</Text>
      </View>
      {[g.away, g.home].map((s) => (
        <View key={s.id} style={st.lineRow}>
          <View style={[st.lineTeam, st.lineRowIn]}>
            <Logo team={s} size={36} />
            <Text style={st.abbr}>{s.abbr}</Text>
            <Text style={st.lineName} numberOfLines={1}>
              {s.name}
            </Text>
          </View>
          {cols.map((c, i) => (
            <Text key={c} style={st.cell}>
              {s.lines?.[i] ?? ''}
            </Text>
          ))}
          <Text style={[st.cell, st.total, st.strong]}>{s.score ?? ''}</Text>
        </View>
      ))}
      {sum.players.map((p, i) => (
        <Table
          key={i}
          title={`${(p.team === g.home.id ? g.home : g.away).shortName || ''} ${p.title}`}
          labels={p.labels}
          rows={p.rows.map((r) => ({ key: r.name, name: r.position ? `${r.name}  ${r.position}` : r.name, stats: r.stats }))}
        />
      ))}
    </View>
  )
}

function TeamStats({ sum, game: g }: { sum: SportsSummary; game: SportsGame }) {
  if (!sum.stats.length) return <Text style={st.muted}>No team stats yet.</Text>
  return (
    <Table
      title=""
      labels={[g.away.abbr || g.away.shortName, g.home.abbr || g.home.shortName]}
      rows={sum.stats.map((s) => ({ key: s.label, name: s.label, stats: [s.away, s.home] }))}
      wide
    />
  )
}

function Leaders({ sum, game: g }: { sum: SportsSummary; game: SportsGame }) {
  if (!sum.leaders.length) return <Text style={st.muted}>No leaders yet.</Text>
  return (
    <View>
      {sum.leaders.map((l) => (
        <View key={l.title} style={st.leader}>
          <Text style={st.leaderTitle}>{l.title}</Text>
          <Text style={st.leaderLine}>
            <Text style={st.abbr}>{g.away.abbr} </Text>
            {leaderText(l.away, true) || '—'}
          </Text>
          <Text style={st.leaderLine}>
            <Text style={st.abbr}>{g.home.abbr} </Text>
            {leaderText(l.home, true) || '—'}
          </Text>
        </View>
      ))}
    </View>
  )
}

function StandingsTables({ sum, game: g }: { sum: SportsSummary; game: SportsGame }) {
  if (!sum.standings.length) return <Text style={st.muted}>No standings for this league.</Text>
  return (
    <View>
      {sum.standings.map((t) => (
        <Table
          key={t.title}
          title={t.title}
          labels={t.labels}
          rows={t.rows.map((r) => ({ key: r.id || r.name, name: r.name, stats: r.stats, strong: r.id === g.home.id || r.id === g.away.id }))}
        />
      ))}
    </View>
  )
}

/** A stats table; each row is focusable so the remote can scroll a long one. */
function Table({ title, labels, rows, wide }: { title: string; labels: string[]; rows: { key: string; name: string; stats: string[]; strong?: boolean }[]; wide?: boolean }) {
  return (
    <View style={st.table}>
      <View style={st.tableRow}>
        <Text style={[st.tableName, st.tableTitle]}>{title}</Text>
        {labels.map((l, i) => (
          <Text key={i} style={[st.cell, st.cellHead, wide && st.cellWide]}>
            {l}
          </Text>
        ))}
      </View>
      {rows.map((r) => (
        <Focusable key={r.key} style={st.tableRow}>
          <Text style={[st.tableName, r.strong && st.strong]} numberOfLines={1}>
            {r.name}
          </Text>
          {r.stats.map((v, i) => (
            <Text key={i} style={[st.cell, wide && st.cellWide, r.strong && st.strong]}>
              {v}
            </Text>
          ))}
        </Focusable>
      ))}
    </View>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  box: { width: 1300, height: '92%', backgroundColor: colors.surface, borderRadius: 24, overflow: 'hidden' },
  head: { flexDirection: 'row', alignItems: 'flex-start', paddingHorizontal: 40, paddingTop: 40 },
  title: { color: colors.text, fontSize: font.heading, fontWeight: '700' },
  sub: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  tabs: { flexDirection: 'row', marginLeft: 30 },
  tab: { paddingVertical: 12, paddingHorizontal: 22, borderBottomWidth: 4, borderBottomColor: 'transparent' },
  tabOn: { borderBottomColor: colors.accent },
  tabText: { color: colors.muted, fontSize: 20, fontWeight: '800', letterSpacing: 2 },
  tabTextOn: { color: colors.text },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', paddingHorizontal: 32, paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: colors.border },
  body: { flex: 1 },
  bodyIn: { padding: 40 },
  muted: { color: colors.muted, fontSize: font.small },
  strong: { color: colors.text, fontWeight: '800' },
  lineRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 16 },
  lineRowIn: { flexDirection: 'row', alignItems: 'center' },
  lineTeam: { width: 460 },
  abbr: { color: colors.muted, fontSize: font.small, fontWeight: '700', marginHorizontal: 14 },
  lineName: { flex: 1, color: colors.text, fontSize: font.small, fontWeight: '600', marginLeft: 8 },
  cell: { width: 64, color: colors.text, fontSize: font.small, textAlign: 'center' },
  cellWide: { width: 180 },
  cellHead: { color: colors.muted, fontSize: 18 },
  total: { marginLeft: 30 },
  table: { marginTop: 26 },
  tableTitle: { color: colors.muted, fontWeight: '800' },
  tableRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6, borderRadius: 8 },
  tableName: { width: 380, color: colors.text, fontSize: 20 },
  leader: { backgroundColor: colors.surface2, borderRadius: 14, padding: 20, marginBottom: 14 },
  leaderTitle: { color: colors.muted, fontSize: 18, fontWeight: '800', marginBottom: 8 },
  leaderLine: { color: colors.text, fontSize: font.small, marginTop: 4 },
}))
