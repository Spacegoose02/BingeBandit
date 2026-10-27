import React, { useEffect, useState } from 'react'
import { Image, Linking, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, cached } from '../api'
import { Center, Focusable } from '../components'
import { scaled, colors, font, gutter, isPhone } from '../theme'

interface DiscoverVideo {
  id: string
  title: string
  publishedAt: number
}

interface DiscoverRow {
  id: number
  name: string
  videos: DiscoverVideo[]
}

const ago = (t: number) => {
  const days = Math.floor((Date.now() / 1000 - t) / 86400)
  if (days < 1) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 30) return `${days} days ago`
  return new Date(t * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

/**
 * Discover: the newest videos from the YouTube playlists and channels an
 * admin picked on the web. OK opens one in the YouTube app.
 */
export default function Discover() {
  const [rows, setRows] = useState<DiscoverRow[] | null>(() => cached<DiscoverRow[]>('/api/discover') ?? null)
  const [error, setError] = useState('')
  useEffect(() => {
    api.get<DiscoverRow[]>('/api/discover').then(setRows, (e) => setError(e.message))
  }, [])
  const open = (v: DiscoverVideo) =>
    Linking.openURL(`https://www.youtube.com/watch?v=${v.id}`).catch(() => setError("Couldn't open YouTube on this device."))

  if (!rows) return <Center><Text style={st.muted}>{error || 'Loading…'}</Text></Center>
  return (
    <ScrollView contentContainerStyle={st.screen}>
      {!!error && <Text style={st.error}>{error}</Text>}
      {rows.length === 0 && <Text style={st.muted}>Nothing here yet. An admin adds YouTube playlists and channels under Discover on the web.</Text>}
      {rows.map((r, ri) => (
        <View key={r.id} style={st.section}>
          <Text style={st.sectionTitle}>{r.name}</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.row}>
            {r.videos.map((v, i) => (
              <Focusable key={v.id} onPress={() => open(v)} hasTVPreferredFocus={ri === 0 && i === 0} style={st.card}>
                <Image source={{ uri: `https://i.ytimg.com/vi/${v.id}/mqdefault.jpg` }} style={st.thumb} resizeMethod="resize" />
                <Text style={st.title} numberOfLines={2}>{v.title}</Text>
                <Text style={st.small}>{ago(v.publishedAt)}</Text>
              </Focusable>
            ))}
          </ScrollView>
        </View>
      ))}
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  screen: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: isPhone ? 160 : 60 },
  muted: { color: colors.muted, fontSize: font.body, maxWidth: 1300, marginTop: 10 },
  error: { color: colors.danger, fontSize: font.small, marginBottom: 14 },
  section: { marginTop: 24 },
  sectionTitle: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginBottom: 14 },
  row: { paddingVertical: 10, paddingRight: 40 },
  card: { width: 420, marginRight: 26, borderRadius: 16 },
  thumb: { width: 420, height: 236, borderRadius: 16, backgroundColor: colors.surface2 },
  title: { color: colors.text, fontSize: font.small, fontWeight: '600', marginTop: 10 },
  small: { color: colors.muted, fontSize: font.small, marginTop: 4 },
}))
