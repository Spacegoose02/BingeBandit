import React from 'react'
import { Image, StyleSheet, Text, View } from 'react-native'
import { Focusable } from '../components'
import { scaled, colors, font, isPhone } from '../theme'
import type { LiveChannel, SportsGame, SportsLeader, SportsSide, SportsTeam } from '../types'
import { scoreShown, sportColors, startLabel } from './hub'

export function Logo({ team, size }: { team: Pick<SportsTeam, 'logo' | 'color'>; size: number }) {
  const s = { width: size, height: size }
  return team.logo ? (
    <Image source={{ uri: team.logo }} style={s} resizeMode="contain" />
  ) : (
    <View style={[s, st.logoBlank, team.color ? { backgroundColor: `#${team.color}` } : null]} />
  )
}

export function leaderText(l?: SportsLeader, long?: boolean): string {
  if (!l) return ''
  const who = long ? [l.name, l.position].filter(Boolean).join(' · ') : l.shortName
  return [who, l.value].filter(Boolean).join(long ? '  ·  ' : ' ')
}

/** "LIVE", the start time, or the final word, as the card's corner says it. */
export function gameStatus(g: SportsGame): string {
  if (g.state === 'pre') return startLabel(g.start)
  return g.detail
}

/**
 * One game in a Sports Hub row: the league, a star when one of your teams
 * plays, both teams with records and scores (hidden while spoiler-free), the
 * standout players and where it's on.
 */
export function GameCard({
  game: g,
  league,
  sport,
  previewing,
  preferFocus,
  onPress,
  onLongPress,
  onFocus,
}: {
  game: SportsGame
  league: string
  sport?: string
  previewing?: boolean
  preferFocus?: boolean
  onPress: () => void
  onLongPress: () => void
  onFocus?: () => void
}) {
  const shown = scoreShown(g)
  const live = g.state === 'in'
  const side = (s: SportsSide) => (
    <View style={st.side}>
      <Logo team={s} size={34} />
      <Text style={[st.team, (g.mine?.includes(s.id) || (shown && s.winner)) && st.teamStrong]} numberOfLines={1}>
        {s.shortName || s.name}
        {!!s.record && <Text style={st.record}> {s.record}</Text>}
      </Text>
      {g.state !== 'pre' && <Text style={[st.score, shown && (live || s.winner) && st.scoreStrong]}>{shown ? s.score : '–'}</Text>}
    </View>
  )
  const leaders = shown || g.state === 'pre' ? [leaderText(g.away.leader), leaderText(g.home.leader)].filter(Boolean).join('  ·  ') : ''
  const onChannels = !!g.channels?.length
  return (
    <Focusable
      onPress={onPress}
      onLongPress={onLongPress}
      onFocus={onFocus}
      hasTVPreferredFocus={preferFocus}
      style={StyleSheet.flatten([st.card, { borderLeftColor: sportColors[sport ?? ''] ?? colors.accent }, previewing && st.cardPreviewing])}
    >
      <View style={st.cardHead}>
        <Text style={st.tag}>{league}</Text>
        {!!g.mine?.length && <Text style={st.star}>★</Text>}
        <View style={st.flex} />
        {live ? <Text style={st.live}>● LIVE</Text> : <Text style={st.status} numberOfLines={1}>{gameStatus(g)}</Text>}
      </View>
      {side(g.away)}
      {side(g.home)}
      <Text style={st.leaders} numberOfLines={1}>
        {live ? `${g.detail}${leaders ? '  ·  ' : ''}${leaders}` : leaders || g.venue || ' '}
      </Text>
      <View style={st.cardFoot}>
        <Text style={st.network} numberOfLines={1}>
          {g.broadcasts?.join(', ') || ' '}
        </Text>
        {onChannels && <Text style={st.onChannels}>{previewing ? (isPhone ? 'Tap again for full screen' : 'OK again: full screen') : 'On your channels'}</Text>}
      </View>
    </Focusable>
  )
}

/** A Live TV channel in the Channels tab or the Multi-View picker, with what's on. */
export function ChannelCard({
  channel: c,
  game,
  preferFocus,
  onPress,
  onLongPress,
  onFocus,
}: {
  channel: LiveChannel
  game?: SportsGame
  preferFocus?: boolean
  onPress: () => void
  onLongPress?: () => void
  onFocus?: () => void
}) {
  return (
    <Focusable onPress={onPress} onLongPress={onLongPress} onFocus={onFocus} hasTVPreferredFocus={preferFocus} style={st.channel}>
      <View style={st.side}>
        {c.logo ? <Image source={{ uri: c.logo }} style={st.channelLogo} resizeMode="contain" /> : <View style={st.channelLogo} />}
        <Text style={st.channelName} numberOfLines={1}>
          <Text style={st.channelNumber}>{c.number} </Text>
          {c.name}
        </Text>
      </View>
      <Text style={st.leaders} numberOfLines={2}>
        {game ? `${game.away.shortName || game.away.name} at ${game.home.shortName || game.home.name}${game.state === 'in' ? ' · LIVE' : ` · ${gameStatus(game)}`}` : c.now?.title ?? ''}
      </Text>
    </Focusable>
  )
}

export const cardWidth = isPhone ? 560 : 440

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  logoBlank: { borderRadius: 999, backgroundColor: colors.surface3 },
  card: { width: cardWidth, backgroundColor: colors.surface, borderRadius: 16, borderLeftWidth: 5, paddingVertical: 16, paddingHorizontal: 18, marginRight: 22 },
  cardPreviewing: { backgroundColor: colors.surface2 },
  cardHead: { flexDirection: 'row', alignItems: 'center', marginBottom: 10 },
  tag: { color: '#f5b942', fontSize: 16, fontWeight: '800', borderWidth: 1, borderColor: 'rgba(245,185,66,0.4)', borderRadius: 6, paddingHorizontal: 8, paddingVertical: 2, overflow: 'hidden' },
  star: { color: colors.accent, fontSize: 20, marginLeft: 10 },
  live: { color: colors.danger, fontSize: 16, fontWeight: '800', backgroundColor: 'rgba(248,113,113,0.15)', borderRadius: 6, paddingHorizontal: 10, paddingVertical: 3, overflow: 'hidden' },
  status: { color: colors.muted, fontSize: 18, maxWidth: 260 },
  side: { flexDirection: 'row', alignItems: 'center', marginBottom: 6 },
  team: { flex: 1, color: colors.muted, fontSize: 22, marginLeft: 14 },
  teamStrong: { color: colors.text, fontWeight: '700' },
  record: { color: colors.muted, fontSize: 18, fontWeight: '400' },
  score: { color: colors.muted, fontSize: 26, fontWeight: '800', marginLeft: 10, minWidth: 40, textAlign: 'right' },
  scoreStrong: { color: colors.text },
  leaders: { color: colors.muted, fontSize: 16, marginTop: 8 },
  cardFoot: { flexDirection: 'row', alignItems: 'center', marginTop: 6 },
  network: { flex: 1, color: colors.muted, fontSize: 18 },
  onChannels: { color: colors.accent, fontSize: 16, fontWeight: '700' },
  channel: { width: cardWidth, backgroundColor: colors.surface, borderRadius: 16, padding: 18, marginRight: 22, marginBottom: 22 },
  channelLogo: { width: 64, height: 40, borderRadius: 6, backgroundColor: colors.surface2 },
  channelNumber: { color: colors.muted, fontWeight: '400' },
  channelName: { flex: 1, color: colors.text, fontSize: font.small, fontWeight: '700', marginLeft: 14 },
}))
