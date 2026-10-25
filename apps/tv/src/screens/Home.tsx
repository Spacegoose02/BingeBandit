import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { api, cached, current, image, saveSession, writesDone } from '../api'
import { Button, Center, Focusable, ItemRow, PosterCard, WideCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { HomeData, Item, Library, User } from '../types'
import { checkForUpdate, type AppRelease } from '../updates'
import LibraryTab from './Library'
import LiveGuide from './LiveGuide'
import Settings from './Settings'

// Pinned libraries sit between Search and Live TV, as `lib:<id>`.
type Tab = 'home' | 'search' | 'live' | 'settings' | `lib:${number}`
const fixedNames: Record<string, string> = { home: 'Home', search: 'Search', live: 'Live TV', settings: 'Settings' }

// Survives the remount when Back returns here from the player or details.
let lastTab: Tab = 'home'
// Asked once per launch, not on every return to Home.
let updateCheck: Promise<AppRelease | null> | null = null

const pinnedOf = (libs: Library[] | undefined) => (libs ?? []).filter((l) => l.pinned)

/** The main screen: a menu across the top and the chosen section below. */
export default function Home({ onSignedOut, onSwitchProfile }: { onSignedOut: () => void; onSwitchProfile: () => void }) {
  const [tab, setTabState] = useState<Tab>(lastTab)
  const setTab = (t: Tab) => {
    lastTab = t
    setTabState(t)
  }
  const [update, setUpdate] = useState<AppRelease | null>(null)
  // The menu draws from the last list straight away, then refreshes.
  const [libraries, setLibraries] = useState<Library[]>(() => pinnedOf(cached<Library[]>('/api/libraries')))
  // Look for a newer app once per launch; the Settings tab then shows a dot.
  useEffect(() => {
    updateCheck ??= checkForUpdate().catch(() => null)
    updateCheck.then(setUpdate)
    api.get<Library[]>('/api/libraries').then((libs) => {
      const pinned = pinnedOf(libs)
      setLibraries(pinned)
      // The library this tab showed last time may have been unpinned since.
      if (lastTab.startsWith('lib:') && !pinned.some((l) => `lib:${l.id}` === lastTab)) setTab('home')
    }, () => undefined)
  }, [])
  // An admin can turn Live TV off for someone; check on each launch, since
  // the saved sign-in may predate the change.
  const [liveTV, setLiveTV] = useState((cached<User>('/api/me') ?? current()?.user)?.liveTV !== false)
  useEffect(() => {
    api
      .get<User>('/api/me')
      .then((me) => {
        setLiveTV(me.liveTV !== false)
        if (me.liveTV === false && lastTab === 'live') setTab('home')
      })
      .catch(() => undefined)
  }, [])
  const names: Record<string, string> = { ...fixedNames }
  for (const l of libraries) names[`lib:${l.id}`] = l.name
  const order: Tab[] = ['home', 'search', ...libraries.map((l) => `lib:${l.id}` as Tab), ...(liveTV ? (['live'] as Tab[]) : []), 'settings']
  const library = libraries.find((l) => `lib:${l.id}` === tab)
  const tabs = order.map((t) => {
    const label = (
      <Text style={[isPhone ? st.tabText : st.menuText, tab === t && st.menuTextActive]} numberOfLines={1}>
        {names[t]}
        {t === 'settings' && update && <Text style={st.dot}> ●</Text>}
      </Text>
    )
    // Phones: a bottom tab bar you tap. TVs: a top menu that switches as focus moves across it.
    return isPhone ? (
      <Pressable key={t} onPress={() => setTab(t)} style={[st.tab, order.length > 5 && st.tabScroll, tab === t && st.tabActive]}>
        {label}
      </Pressable>
    ) : (
      <Focusable key={t} hasTVPreferredFocus={t === tab && t !== 'home'} onFocus={() => setTab(t)} onPress={() => setTab(t)} style={st.menuItem}>
        {label}
      </Focusable>
    )
  })
  return (
    <View style={st.screen}>
      {!isPhone && (
        <View style={st.menu}>
          <Image source={require('../assets/mascot.png')} style={st.mascot} />
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.menuItems}>
            {tabs}
          </ScrollView>
        </View>
      )}
      {tab === 'home' && <HomeRows />}
      {library && <LibraryTab key={library.id} library={library} />}
      {tab === 'search' && <Search />}
      {tab === 'live' && liveTV && <LiveGuide />}
      {tab === 'settings' && (
        <Settings
          update={update}
          onSwitchProfile={onSwitchProfile}
          onSignOut={async () => {
            await api.post('/api/auth/logout').catch(() => undefined)
            await saveSession(null)
            lastTab = 'home'
            onSignedOut()
          }}
        />
      )}
      {isPhone &&
        (order.length > 5 ? (
          <View style={st.tabBar}>
            <ScrollView horizontal showsHorizontalScrollIndicator={false}>{tabs}</ScrollView>
          </View>
        ) : (
          <View style={st.tabBar}>{tabs}</View>
        ))}
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
        disableFullscreenUI // a sideways phone would otherwise cover the results with a full-screen editor
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
  // Back from the player or details draws the last rows at once and refreshes behind them.
  const [data, setData] = useState<HomeData | null>(() => cached<HomeData>('/api/home') ?? null)
  const [error, setError] = useState('')
  const hero = useRef<((it: Item) => void) | null>(null)

  const load = useCallback(() => {
    setError('')
    // Wait for the player's last report so Continue Watching has the new position.
    writesDone()
      .then(() => api.get<HomeData>('/api/home'))
      .then(setData, (e) => setError(e.message))
  }, [])
  useEffect(load, [load])

  if (error && !data)
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
  const show = (it: Item) => hero.current?.(it)

  return (
    <View style={st.flex}>
      <Hero first={data.hero[0] ?? data.continueWatching[0] ?? data.rows[0]?.items[0] ?? null} feed={hero} />
      <ScrollView style={st.flex} contentContainerStyle={st.rows}>
        {data.continueWatching.length > 0 && (
          <ItemRow
            title="Continue Watching"
            items={data.continueWatching}
            renderItem={(it, i) => <WideCard item={it} preferFocus={i === 0} onFocus={() => show(it)} onPress={() => play(it)} />}
          />
        )}
        {data.rows.map((row, r) => (
          <ItemRow
            key={row.id}
            title={row.title}
            items={row.items}
            renderItem={(it, i) => (
              <PosterCard
                item={it}
                preferFocus={data.continueWatching.length === 0 && r === 0 && i === 0}
                onFocus={() => show(it)}
                onPress={() => open(it)}
              />
            )}
          />
        ))}
      </ScrollView>
    </View>
  )
}

/**
 * The focused title's backdrop and blurb. It redraws on its own, once focus
 * rests on a card, rather than redrawing every row on every remote press.
 */
function Hero({ first, feed }: { first: Item | null; feed: React.MutableRefObject<((it: Item) => void) | null> }) {
  const [hero, setHero] = useState(first)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    feed.current = (it) => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => setHero(it), 200)
    }
    return () => {
      if (timer) clearTimeout(timer)
      feed.current = null
    }
  }, [feed])
  const bg = hero ? image(hero.backdrop || hero.showBackdrop, 'w1280') : undefined
  return (
    <>
      {bg && <Image source={bg} style={st.heroImg} resizeMethod="resize" fadeDuration={200} />}
      {hero && (
        <View style={st.hero}>
          <Text style={st.heroTitle} numberOfLines={1}>{hero.kind === 'episode' ? hero.showTitle : hero.title}</Text>
          <Text style={st.heroMeta} numberOfLines={1}>
            {[hero.year, hero.contentRating, hero.genres?.slice(0, 3).join(', ')].filter(Boolean).join('  ·  ')}
          </Text>
          <Text style={st.heroOverview} numberOfLines={3}>{hero.overview}</Text>
        </View>
      )}
    </>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  flex: { flex: 1 },
  menu: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: gutter, paddingTop: 44, paddingBottom: 8, zIndex: 2 },
  mascot: { width: 76, height: 76, marginRight: 36 },
  menuItems: { alignItems: 'center' },
  menuItem: { paddingHorizontal: 26, paddingVertical: 10, marginRight: 12 },
  menuText: { color: colors.muted, fontSize: font.heading, fontWeight: '700' },
  menuTextActive: { color: colors.text },
  tabBar: { flexDirection: 'row', backgroundColor: colors.surface, borderTopWidth: 1, borderTopColor: colors.border },
  tab: { flex: 1, alignItems: 'center', paddingVertical: 26, borderTopWidth: 4, borderTopColor: 'transparent' },
  tabActive: { borderTopColor: colors.accent },
  tabScroll: { flex: 0, paddingHorizontal: 34 },
  tabText: { color: colors.muted, fontSize: font.small, fontWeight: '700' },
  heroImg: { position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, width: '100%', height: '100%', opacity: 0.35 },
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
