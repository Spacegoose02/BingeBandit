import React, { useEffect, useState } from 'react'
import { Image, StyleSheet, Text, View } from 'react-native'
import { api } from './api'
import { scaled, colors, font } from './theme'
import type { SportsGame, SportsSide } from './types'

/**
 * While something plays, the score of each of your teams' games that's on
 * sits in the top left corner, like a broadcast's score bug. Not on the
 * channel showing that game, which has its own, and not when turned off
 * under Settings → Alerts, or while the Sports Hub is spoiler-free.
 */
/** Has the score bug read its switch again at once (Live TV's Options changes it). */
export const refreshScoreBug = { current: () => undefined as void }

/** The Live TV player says which channel it has tuned, since flipping channels doesn't change the screen. */
export const scoreBugChannel = { set: (_id: number | undefined) => undefined as void }

export function ScoreBug({ playing, channelId: screenChannel }: { playing: boolean; channelId?: number }) {
  const [games, setGames] = useState<SportsGame[]>([])
  const [tuned, setTuned] = useState<number | undefined>(undefined)
  useEffect(() => {
    scoreBugChannel.set = setTuned
    return () => {
      scoreBugChannel.set = () => undefined
    }
  }, [])
  const channelId = screenChannel === undefined ? undefined : (tuned ?? screenChannel)
  useEffect(() => {
    if (!playing) {
      setGames([])
      return
    }
    let on = true
    const look = async () => {
      try {
        const p = await api.get<{ scoreBug?: boolean; spoilerFree?: boolean }>('/api/me/prefs')
        if (p.scoreBug === false || p.spoilerFree) { // spoiler-free keeps scores off the screen
          if (on) setGames([])
          return
        }
        const { games } = await api.get<{ games: SportsGame[] }>(`/api/sports/live${channelId ? `?channel=${channelId}` : ''}`)
        if (on) setGames(games)
      } catch {
        // No sports, or offline: just no bug.
      }
    }
    look()
    refreshScoreBug.current = () => void look()
    const t = setInterval(look, 30000)
    return () => {
      on = false
      refreshScoreBug.current = () => undefined
      clearInterval(t)
    }
  }, [playing, channelId])
  const shown = games.filter((g) => !g.channels?.some((c) => c.id === channelId)).slice(0, 2)
  if (!playing || shown.length === 0) return null
  return (
    <View style={st.wrap} pointerEvents="none">
      {shown.map((g) => (
        <View key={g.id} style={st.bug}>
          <Side side={g.away} />
          <Side side={g.home} />
          <Text style={st.detail} numberOfLines={1}>{g.detail}</Text>
        </View>
      ))}
    </View>
  )
}

function Side({ side }: { side: SportsSide }) {
  return (
    <View style={[st.side, !!side.color && { borderLeftColor: `#${side.color}` }]}>
      {!!side.logo && <Image source={{ uri: side.logo }} style={st.logo} resizeMode="contain" />}
      <Text style={st.abbr}>{side.abbr}</Text>
      <Text style={st.score}>{side.score ?? '0'}</Text>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  wrap: { position: 'absolute', top: 40, left: 40 },
  bug: {
    flexDirection: 'row', alignItems: 'center', marginBottom: 10, borderRadius: 12, overflow: 'hidden',
    backgroundColor: 'rgba(12,14,18,0.86)',
  },
  side: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 14, paddingVertical: 8, borderLeftWidth: 6, borderLeftColor: colors.muted },
  logo: { width: 34, height: 34, marginRight: 8 },
  abbr: { color: colors.text, fontSize: font.small, fontWeight: '700', marginRight: 12 },
  score: { color: colors.text, fontSize: font.body, fontWeight: '800', minWidth: 30, textAlign: 'right' },
  detail: { color: colors.muted, fontSize: font.small, paddingHorizontal: 14 },
}))
