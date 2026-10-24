import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, Image, ImageBackground, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { api, image, saveSession } from '../api'
import { Button, Center, Focusable, PosterCard, Row, WideCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter } from '../theme'
import type { HomeData, Item } from '../types'
import { checkForUpdate, type AppRelease } from '../updates'
import LiveGuide from './LiveGuide'
import Settings from './Settings'

type Tab = 'home' | 'search' | 'live' | 'settings'
const tabNames: Record<Tab, string> = { home: 'Home', search: 'Search', live: 'Live TV', settings: 'Settings' }

// Survives the remount when Back returns here from the player or details.
let lastTab: Tab = 'home'

/** The main screen: a menu across the top and the chosen section below. */
export default function Home({ onSignedOut }: { onSignedOut: () => void }) {
  const [tab, setTabState] = useState<Tab>(lastTab)
  const setTab = (t: Tab) => {
    lastTab = t
    setTabState(t)
  }
  const [update, setUpdate] = useState<AppRelease | null>(null)
  // Look for a newer app once per launch; the Settings tab then shows a dot.
  useEffect(() => {
    checkForUpdate().then(setUpdate, () => undefined)
  }, [])
  return (
    <View style={st.screen}>
      <View style={st.menu}>
        <Image source={require('../assets/mascot.png')} style={st.mascot} />
        {(Object.keys(tabNames) as Tab[]).map((t) => (
          <Focusable key={t} hasTVPreferredFocus={t === tab && t !== 'home'} onFocus={() => setTab(t)} onPress={() => setTab(t)} style={st.menuItem}>
            <Text style={[st.menuText, tab === t && st.menuTextActive]}>
              {tabNames[t]}
              {t === 'settings' && update && <Text style={st.dot}> ●</Text>}
            </Text>
          </Focusable>
        ))}
      </View>
      {tab === 'home' && <HomeRows />}
      {tab === 'search' && <Search />}
      {tab === 'live' && <LiveGuide />}
      {tab === 'settings' && (
        <Settings
          update={update}
          onSignOut={async () => {
            await api.post('/api/auth/logout').catch(() => undefined)
            await saveSession(null)
            lastTab = 'home'
            onSignedOut()
          }}
        />
      )}
    </View>
  )
}

/** Type to search titles and people; results update as you type. */
function Search() {
  const nav = useNav()
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Item[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (q.trim().length < 2) {
      setResults(null)
      return
    }
    const t = setTimeout(() => {
      api.get<Item[]>(`/api/search?q=${encodeURIComponent(q.trim())}`).then(
        (r) => {
          setResults(r)
          setError('')
        },
        (e) => setError(e.message),
      )
    }, 350)
    return () => clearTimeout(t)
  }, [q])
  return (
    <ScrollView contentContainerStyle={st.search}>
      <TextInput
        style={st.searchInput}
        value={q}
        onChangeText={setQ}
        placeholder="Search movies and shows"
        placeholderTextColor={colors.muted}
        autoCorrect={false}
        returnKeyType="search"
      />
      {!!error && <Text style={st.error}>{error}</Text>}
      {results && results.length === 0 && <Text style={st.mutedLeft}>Nothing matches “{q.trim()}”.</Text>}
      {results && results.length > 0 && (
        <View style={st.grid}>
          {results.map((it) => (
            <View key={it.id} style={st.gridCell}>
              <PosterCard item={it} onPress={() => nav.push({ name: 'details', id: it.id })} />
              <Text style={st.gridTitle} numberOfLines={1}>{it.title}</Text>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
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
  dot: { color: colors.accent, fontSize: font.small },
  search: { paddingHorizontal: gutter, paddingTop: 20, paddingBottom: 60 },
  searchInput: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.heading, borderRadius: 16, paddingHorizontal: 28, paddingVertical: 18, borderWidth: 2, borderColor: colors.border, maxWidth: 1100, marginBottom: 30 },
  mutedLeft: { color: colors.muted, fontSize: font.body },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  gridCell: { marginBottom: 30 },
  gridTitle: { color: colors.text, fontSize: font.small, marginTop: 8, width: 230 },
}))
