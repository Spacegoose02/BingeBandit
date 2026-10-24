import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, Image, ImageBackground, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, current, image, saveSession } from '../api'
import { Button, Center, Focusable, PosterCard, Row, WideCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter } from '../theme'
import type { HomeData, Item, LiveChannel } from '../types'
import Settings from './Settings'

type Tab = 'home' | 'live' | 'settings'

/** The main screen: a menu across the top and the chosen section below. */
export default function Home({ onSignedOut }: { onSignedOut: () => void }) {
  const [tab, setTab] = useState<Tab>('home')
  return (
    <View style={st.screen}>
      <View style={st.menu}>
        <Image source={require('../assets/mascot.png')} style={st.mascot} />
        {(['home', 'live', 'settings'] as Tab[]).map((t) => (
          <Focusable key={t} onFocus={() => setTab(t)} onPress={() => setTab(t)} style={st.menuItem}>
            <Text style={[st.menuText, tab === t && st.menuTextActive]}>{t === 'home' ? 'Home' : t === 'live' ? 'Live TV' : 'Settings'}</Text>
          </Focusable>
        ))}
      </View>
      {tab === 'home' && <HomeRows />}
      {tab === 'live' && <LiveList />}
      {tab === 'settings' && (
        <Settings
          onSignOut={async () => {
            await api.post('/api/auth/logout').catch(() => undefined)
            await saveSession(null)
            onSignedOut()
          }}
        />
      )}
    </View>
  )
}

function HomeRows() {
  const nav = useNav()
  const [data, setData] = useState<HomeData | null>(null)
  const [error, setError] = useState('')
  const [hero, setHero] = useState<Item | null>(null)

  const load = useCallback(() => {
    setError('')
    api.get<HomeData>('/api/home').then(
      (d) => {
        setData(d)
        setHero(d.hero[0] ?? d.continueWatching[0] ?? d.rows[0]?.items[0] ?? null)
      },
      (e) => setError(e.message),
    )
  }, [])
  useEffect(load, [load])

  if (error)
    return (
      <Center>
        <Text style={st.error}>{error}</Text>
        <Button label="Try again" primary preferFocus onPress={load} />
      </Center>
    )
  if (!data) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (data.rows.length === 0 && data.continueWatching.length === 0)
    return <Center><Text style={st.muted}>Nothing here yet. Add a library on the server first.</Text></Center>

  const open = (it: Item) => nav.push({ name: 'details', id: it.id })
  const play = (it: Item) => nav.push({ name: 'player', id: it.id, start: it.watch?.position > 15 ? it.watch.position : 0 })
  const bg = hero ? image(hero.backdrop || hero.showBackdrop, 'w1280') : undefined

  return (
    <ImageBackground source={bg ?? {}} style={st.flex} imageStyle={st.heroImg}>
      <View style={st.shade}>
        {hero && (
          <View style={st.hero}>
            <Text style={st.heroTitle} numberOfLines={1}>{hero.kind === 'episode' ? hero.showTitle : hero.title}</Text>
            <Text style={st.heroMeta} numberOfLines={1}>
              {[hero.year, hero.contentRating, hero.genres?.slice(0, 3).join(', ')].filter(Boolean).join('  ·  ')}
            </Text>
            <Text style={st.heroOverview} numberOfLines={3}>{hero.overview}</Text>
          </View>
        )}
        <ScrollView style={st.flex} contentContainerStyle={st.rows}>
          {data.continueWatching.length > 0 && (
            <Row title="Continue Watching">
              {data.continueWatching.map((it, i) => (
                <WideCard key={it.id} item={it} preferFocus={i === 0} onFocus={() => setHero(it)} onPress={() => play(it)} />
              ))}
            </Row>
          )}
          {data.rows.map((row, r) => (
            <Row key={row.id} title={row.title}>
              {row.items.map((it, i) => (
                <PosterCard
                  key={it.id}
                  item={it}
                  preferFocus={data.continueWatching.length === 0 && r === 0 && i === 0}
                  onFocus={() => setHero(it)}
                  onPress={() => open(it)}
                />
              ))}
            </Row>
          ))}
        </ScrollView>
      </View>
    </ImageBackground>
  )
}

function LiveList() {
  const nav = useNav()
  const [channels, setChannels] = useState<LiveChannel[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    api.get<LiveChannel[]>('/api/live/channels').then(setChannels, (e) => setError(e.message))
  }, [])
  if (error) return <Center><Text style={st.error}>{error}</Text></Center>
  if (!channels) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (channels.length === 0) return <Center><Text style={st.muted}>No live channels on this server yet.</Text></Center>
  const server = current()?.server ?? ''
  return (
    <ScrollView contentContainerStyle={st.liveList}>
      {channels.map((c) => (
        <Focusable key={c.id} onPress={() => nav.push({ name: 'live', channelId: c.id })} style={st.channel}>
          <Text style={st.chNumber}>{c.number}</Text>
          {c.logo ? (
            <Image source={{ uri: `${server}/api/live/channels/${c.id}/logo`, headers: { Authorization: `Bearer ${current()?.token}` } }} style={st.chLogo} resizeMode="contain" />
          ) : (
            <View style={st.chLogo} />
          )}
          <View style={st.flex}>
            <Text style={st.chName} numberOfLines={1}>{c.name}</Text>
            <Text style={st.chNow} numberOfLines={1}>{c.now ? `Now: ${c.now.title}` : 'No guide information'}</Text>
          </View>
          {c.next && <Text style={st.chNext} numberOfLines={1}>Next: {c.next.title}</Text>}
        </Focusable>
      ))}
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  flex: { flex: 1 },
  menu: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: gutter, paddingTop: 44, paddingBottom: 8, zIndex: 2 },
  mascot: { width: 76, height: 76, marginRight: 36 },
  menuItem: { paddingHorizontal: 26, paddingVertical: 10, marginRight: 12 },
  menuText: { color: colors.muted, fontSize: font.heading, fontWeight: '700' },
  menuTextActive: { color: colors.text },
  heroImg: { opacity: 0.35 },
  shade: { flex: 1 },
  hero: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: 10, maxWidth: 1500 },
  heroTitle: { color: colors.text, fontSize: font.hero, fontWeight: '900' },
  heroMeta: { color: colors.muted, fontSize: font.body, marginTop: 6 },
  heroOverview: { color: colors.text, fontSize: font.body, marginTop: 14, lineHeight: 40, opacity: 0.9 },
  rows: { paddingBottom: 60 },
  error: { color: colors.danger, fontSize: font.body, marginBottom: 20, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
  liveList: { paddingHorizontal: gutter, paddingVertical: 20 },
  channel: { flexDirection: 'row', alignItems: 'center', backgroundColor: colors.surface, padding: 24, marginBottom: 16 },
  chNumber: { color: colors.muted, fontSize: font.heading, fontWeight: '700', width: 110 },
  chLogo: { width: 150, height: 84, marginRight: 30 },
  chName: { color: colors.text, fontSize: font.heading, fontWeight: '700' },
  chNow: { color: colors.muted, fontSize: font.body, marginTop: 4 },
  chNext: { color: colors.muted, fontSize: font.small, maxWidth: 560, marginLeft: 30 },
}))
