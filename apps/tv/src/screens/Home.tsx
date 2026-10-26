import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Image, Modal, Pressable, ScrollView, StyleSheet, TVFocusGuideView, Text, TextInput, View } from 'react-native'
import { api, cached, current, image, saveSession, writesDone } from '../api'
import { Button, Center, Focusable, ItemRow, PosterCard, WideCard } from '../components'
import { menuPosition, menuPositionLoaded, setMenuPosition, type MenuPosition } from '../devicePrefs'
import { Icon, type IconName } from '../icons'
import { homeBack, showLiveGuide, useNav, useScreen } from '../nav'
import { scaled, colors, font, gutter, isPhone, px } from '../theme'
import type { HomeData, Item, Library, User } from '../types'
import { checkForUpdate, type AppRelease } from '../updates'
import LibraryTab, { forgetSection } from './Library'
import LiveGuide from './LiveGuide'
import { fetchHousehold } from './Profiles'
import Settings, { type HideableTab } from './Settings'
import Sports from './Sports'

// Pinned libraries sit between Search and Live TV, as `lib:<id>`. More lists
// every library to open or pin; an unpinned one opened from there joins the
// menu while it's open.
type Tab = 'home' | 'search' | 'sports' | 'live' | 'settings' | 'more' | `lib:${number}`
type MenuPrefs = { hiddenTabs?: HideableTab[] | null }
const fixedNames: Record<string, string> = { home: 'Home', search: 'Search', sports: 'Sports', live: 'Live TV', settings: 'Settings', more: 'More' }

// Survives switching profiles, which starts the stack again on Home.
let lastTab: Tab = 'home'
// Asked once per launch, not on every return to Home.
let updateCheck: Promise<AppRelease | null> | null = null


const fixedIcons: Record<string, IconName> = { home: 'home', search: 'search', sports: 'sports', live: 'live', settings: 'settings', more: 'more' }

/** What holding OK on a menu entry pins or unpins: a library, or the Sports or Live TV tab. */
interface Pinnable {
  name: string
  pinned: boolean
  toggle: () => void
}

/**
 * The main screen: a menu down the left side (collapsed to icons until focus
 * moves into it) or across the top, and the chosen section beside or below it.
 */
export default function Home({ onSignedOut, onSwitchProfile }: { onSignedOut: () => void; onSwitchProfile: () => void }) {
  const [tab, setTabState] = useState<Tab>(lastTab)
  const setTab = (t: Tab) => {
    // Coming to a library from the menu opens its Recommended section; Back
    // from details stays on the same tab and section.
    if (t !== lastTab && t.startsWith('lib:')) forgetSection(Number(t.slice(4)))
    lastTab = t
    setTabState(t)
  }
  // Only the tab Home opened on asks for focus, so Back from details lands on
  // it. Following the current tab instead made focus fight the menu: a render
  // that came late gave focus back to the tab just left, which switched to it
  // again, back and forth.
  const [initialTab] = useState(lastTab)
  useEffect(() => {
    showLiveGuide.current = () => setTab('live')
    return () => {
      showLiveGuide.current = null
    }
  })
  // The menu switches as focus moves across it, but only once focus rests on
  // a tab, so passing over one doesn't build its whole screen.
  const focusTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const settle = () => {
    if (focusTimer.current) clearTimeout(focusTimer.current)
  }
  // Whether focus is in the menu. Focus coming up into it from the screen
  // below never switches the tab: that threw away what you were doing when
  // Up went one press too far. Only moving along the menu switches.
  const inMenu = useRef(false)
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  // The side menu shows its names only while focus is in it.
  const [position, setPosition] = useState<MenuPosition>(menuPosition)
  useEffect(() => {
    menuPositionLoaded().then(() => setPosition(menuPosition()))
  }, [])
  const side = !isPhone && position === 'side'
  const [expanded, setExpanded] = useState(false)
  const enterMenu = () => {
    settle()
    if (leaveTimer.current) clearTimeout(leaveTimer.current)
    const entering = !inMenu.current
    inMenu.current = true
    setExpanded(true)
    return entering
  }
  const focusTab = (t: Tab) => {
    if (!enterMenu()) focusTimer.current = setTimeout(() => setTab(t), 250)
  }
  const blurTab = () => {
    settle()
    // Focus moving to the next tab blurs this one first.
    leaveTimer.current = setTimeout(() => {
      inMenu.current = false
      setExpanded(false)
    }, 50)
  }
  useEffect(
    () => () => {
      settle()
      if (leaveTimer.current) clearTimeout(leaveTimer.current)
    },
    [],
  )
  // The menu's tabs, so Up lands on the current one and Back can go to Home.
  const tabViews = useRef(new Map<Tab, View>())
  const [, setDrawn] = useState(false)
  useEffect(() => setDrawn(true), []) // draw again once the tabs exist, for the focus guide
  const tabRef = useRef(tab)
  tabRef.current = tab
  // Back on Home: from the screen below it goes up to the tab you're on,
  // from another tab to Home, and only from Home may it leave the app.
  useEffect(() => {
    const back = () => {
      const focus = (t: Tab) => setTimeout(() => tabViews.current.get(t)?.requestTVFocus?.(), 50)
      if (!isPhone && !inMenu.current) {
        focus(tabRef.current)
        return true
      }
      if (tabRef.current !== 'home') {
        setTab('home')
        if (!isPhone) focus('home')
        return true
      }
      return false
    }
    homeBack.current = back
    return () => {
      if (homeBack.current === back) homeBack.current = null
    }
  }, [])
  const [update, setUpdate] = useState<AppRelease | null>(null)
  // The menu draws from the last list straight away, then refreshes.
  const [libraries, setLibraries] = useState<Library[]>(() => cached<Library[]>('/api/libraries') ?? [])
  // Look for a newer app once per launch; the Settings tab then shows a dot.
  useEffect(() => {
    updateCheck ??= checkForUpdate().catch(() => null)
    updateCheck.then(setUpdate)
    api.get<Library[]>('/api/libraries').then((libs) => {
      setLibraries(libs)
      // The library this tab showed last time may be gone, or no longer shared.
      if (lastTab.startsWith('lib:') && !libs.some((l) => `lib:${l.id}` === lastTab)) setTab('home')
      if (lastTab === 'more' && libs.length === 0) setTab('home')
    }, () => undefined)
  }, [])
  // Pinning from the app saves the whole order, as the web's pin editor does,
  // and shows the change at once.
  const togglePin = (l: Library) => {
    const before = libraries
    const next = libraries.map((x) => (x.id === l.id ? { ...x, pinned: !x.pinned } : x))
    setLibraries(next)
    api
      .put('/api/me/pins', { libraries: next.map((x) => ({ id: x.id, pinned: x.pinned })) })
      .then(() => api.get<Library[]>('/api/libraries'))
      .then(setLibraries, () => setLibraries(before))
  }
  // Tabs this person turned off, kept with their other preferences so they
  // follow the profile to every TV and phone.
  const [hidden, setHidden] = useState<HideableTab[]>(() => cached<MenuPrefs>('/api/me/prefs')?.hiddenTabs ?? [])
  useEffect(() => {
    api.get<MenuPrefs>('/api/me/prefs').then((p) => {
      const h = p.hiddenTabs ?? []
      setHidden(h)
      if ((h as Tab[]).includes(lastTab)) setTab('home')
    }, () => undefined)
  }, [])
  const toggleTab = (t: HideableTab) => {
    const before = hidden
    const next = hidden.includes(t) ? hidden.filter((x) => x !== t) : [...hidden, t]
    setHidden(next)
    // The rest of the preferences go back as they are.
    api
      .get<MenuPrefs>('/api/me/prefs')
      .then((p) => api.put('/api/me/prefs', { ...p, hiddenTabs: next }))
      .then(() => api.get('/api/me/prefs'))
      .catch(() => setHidden(before))
  }
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
  const icons: Record<string, IconName> = { ...fixedIcons }
  for (const l of libraries) {
    names[`lib:${l.id}`] = l.name
    icons[`lib:${l.id}`] = l.kind === 'movies' ? 'movies' : 'shows'
  }
  const library = libraries.find((l) => `lib:${l.id}` === tab)
  // Holding OK (or a long tap) on a library, Sports or Live TV drops down Pin/Unpin, like Plex.
  const [pinMenu, setPinMenu] = useState<Pinnable | null>(null)
  const pinLibrary = (l: Library): Pinnable => ({ name: l.name, pinned: l.pinned, toggle: () => togglePin(l) })
  const pinTab = (t: HideableTab): Pinnable => ({ name: fixedNames[t], pinned: !hidden.includes(t), toggle: () => toggleTab(t) })
  const shows = (t: HideableTab) => !hidden.includes(t) || tab === t // one opened from More stays while it's open
  const order: Tab[] = [
    'home',
    'search',
    ...libraries.filter((l) => l.pinned || l === library).map((l) => `lib:${l.id}` as Tab),
    ...(shows('sports') ? (['sports'] as Tab[]) : []),
    ...(liveTV && shows('live') ? (['live'] as Tab[]) : []),
    ...(libraries.length && shows('more') ? (['more'] as Tab[]) : []),
    'settings',
  ]
  const activeTab = tabViews.current.get(tab)
  // The logo drops down Settings, switching profile and signing out.
  const [accountMenu, setAccountMenu] = useState(false)
  const signOut = async () => {
    await api.post('/api/auth/logout').catch(() => undefined)
    await saveSession(null)
    lastTab = 'home'
    onSignedOut()
  }
  const tabs = order.map((t) => {
    const label = (
      <Text style={[isPhone ? st.tabText : st.menuText, tab === t && st.menuTextActive]} numberOfLines={1}>
        {names[t]}
        {t === 'settings' && update && <Text style={st.dot}> ●</Text>}
      </Text>
    )
    // Phones: a bottom tab bar you tap. TVs: a side or top menu that switches as focus moves along it.
    const lib = libraries.find((l) => `lib:${l.id}` === t)
    const hold = lib ? () => setPinMenu(pinLibrary(lib)) : t === 'sports' || t === 'live' ? () => setPinMenu(pinTab(t)) : undefined
    if (side)
      return (
        <Focusable
          key={t}
          viewRef={(v) => (v ? tabViews.current.set(t, v) : tabViews.current.delete(t))}
          hasTVPreferredFocus={t === initialTab && t !== 'home'}
          onFocus={() => focusTab(t)}
          onBlur={blurTab}
          onPress={() => {
            settle()
            setTab(t)
          }}
          onLongPress={hold}
          style={StyleSheet.flatten([st.railItem, tab === t && st.railItemActive])}
        >
          <Icon name={icons[t] ?? 'movies'} size={px(44)} color={tab === t ? colors.accent : colors.muted} />
          {expanded && <View style={st.railLabel}>{label}</View>}
          {!expanded && t === 'settings' && update && <Text style={[st.dot, st.railDot]}>●</Text>}
        </Focusable>
      )
    return isPhone ? (
      <Pressable key={t} onPress={() => setTab(t)} onLongPress={hold} style={[st.tab, order.length > 5 && st.tabScroll, tab === t && st.tabActive]}>
        {label}
      </Pressable>
    ) : (
      <Focusable
        key={t}
        viewRef={(v) => (v ? tabViews.current.set(t, v) : tabViews.current.delete(t))}
        hasTVPreferredFocus={t === initialTab && t !== 'home'}
        onFocus={() => focusTab(t)}
        // Moving down into the screen before it switched keeps the screen shown.
        onBlur={blurTab}
        onPress={() => {
          settle()
          setTab(t)
        }}
        onLongPress={hold}
        style={st.menuItem}
      >
        {label}
      </Focusable>
    )
  })
  const logo = (
    <Focusable onFocus={enterMenu} onBlur={blurTab} onPress={() => setAccountMenu(true)} style={side ? st.railLogo : st.logoTop}>
      <Image source={require('../assets/mascot.png')} style={st.mascot} />
    </Focusable>
  )
  return (
    <View style={[st.screen, side && st.screenSide]}>
      {side && (
        <View style={[st.rail, expanded && st.railOpen]}>
          {logo}
          <TVFocusGuideView style={st.flex} destinations={activeTab ? [activeTab] : undefined}>
            <ScrollView showsVerticalScrollIndicator={false} contentContainerStyle={st.railItems}>
              {tabs}
            </ScrollView>
          </TVFocusGuideView>
        </View>
      )}
      <View style={[st.flex, side && st.besideRail]}>
      {!isPhone && !side && (
        <View style={st.menu}>
          {logo}
          {/* Up from anywhere below lands on the tab you're on, not the one above the focused card. */}
          <TVFocusGuideView style={st.flex} destinations={activeTab ? [activeTab] : undefined}>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.menuItems}>
              {tabs}
            </ScrollView>
          </TVFocusGuideView>
        </View>
      )}
      {tab === 'home' && <HomeRows />}
      {library && <LibraryTab key={library.id} library={library} onTogglePin={() => togglePin(library)} />}
      {tab === 'search' && <Search />}
      {tab === 'more' && (
        <More
          libraries={libraries}
          tabs={(liveTV ? (['sports', 'live'] as HideableTab[]) : (['sports'] as HideableTab[])).map((t) => ({ tab: t, ...pinTab(t) }))}
          onOpen={(t) => setTab(t)}
          onHold={setPinMenu}
          pinLibrary={pinLibrary}
        />
      )}
      {tab === 'sports' && <Sports />}
      {tab === 'live' && liveTV && <LiveGuide />}
      {tab === 'settings' && (
        <Settings
          update={update}
          menu={{
            libraries,
            hidden,
            tabs: liveTV ? ['sports', 'live', 'more'] : ['sports', 'more'],
            togglePin,
            toggleTab,
            position,
            setPosition: (p) => {
              setMenuPosition(p)
              setPosition(p)
            },
          }}
          onSwitchProfile={onSwitchProfile}
          onSignOut={signOut}
        />
      )}
      </View>
      {pinMenu && (
        <PinMenu
          target={pinMenu}
          onToggle={() => {
            pinMenu.toggle()
            setPinMenu(null)
          }}
          onClose={() => setPinMenu(null)}
        />
      )}
      {accountMenu && (
        <AccountMenu
          onClose={() => setAccountMenu(false)}
          onSettings={() => {
            setAccountMenu(false)
            setTab('settings')
            if (!isPhone) setTimeout(() => tabViews.current.get('settings')?.requestTVFocus?.(), 50)
          }}
          onSwitchProfile={() => {
            setAccountMenu(false)
            onSwitchProfile()
          }}
          onSignOut={() => {
            setAccountMenu(false)
            signOut()
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

/**
 * Plex's More: every library (and the Sports and Live TV tabs) with a pin to
 * put it in the menu or take it out, beside Continue Watching and On Deck.
 */
function More({
  libraries,
  tabs,
  onOpen,
  onHold,
  pinLibrary,
}: {
  libraries: Library[]
  tabs: ({ tab: HideableTab } & Pinnable)[]
  onOpen: (t: Tab) => void
  onHold: (p: Pinnable) => void
  pinLibrary: (l: Library) => Pinnable
}) {
  const nav = useNav()
  const [home, setHome] = useState<HomeData | null>(() => cached<HomeData>('/api/home') ?? null)
  const { returns } = useScreen()
  useEffect(() => {
    writesDone()
      .then(() => api.get<HomeData>('/api/home'))
      .then(setHome, () => undefined)
  }, [returns])
  // Started titles are Continue Watching; the next episodes of shows are On Deck.
  const cw = home?.continueWatching ?? []
  const started = cw.filter((it) => it.watch?.position > 15)
  const onDeck = cw.filter((it) => !(it.watch?.position > 15))
  const play = (it: Item) => nav.push({ name: 'player', id: it.id, start: it.watch?.position > 15 ? it.watch.position : 0 })
  const entries: { key: string; label: string; hint: string; open: () => void; pin: Pinnable }[] = [
    ...libraries.map((l) => ({
      key: `lib:${l.id}`,
      label: l.name,
      hint: l.kind === 'movies' ? 'Movies' : 'Shows',
      open: () => onOpen(`lib:${l.id}`),
      pin: pinLibrary(l),
    })),
    ...tabs.map((t) => ({ key: t.tab, label: t.name, hint: 'Tab', open: () => onOpen(t.tab), pin: t })),
  ]
  return (
    <View style={[st.flex, !isPhone && st.moreSplit]}>
      <ScrollView style={!isPhone && st.moreList} contentContainerStyle={st.more}>
        <Text style={st.hint}>The pin puts a library or tab in your menu (and libraries on your Home). {isPhone ? 'Hold one' : 'Hold OK on one, here or in the menu,'} to do the same.</Text>
        {entries.map((e) => (
          <View key={e.key} style={st.dropdownRow}>
            <Focusable onPress={e.open} onLongPress={() => onHold(e.pin)} style={st.dropdownName}>
              <Text style={st.dropdownText} numberOfLines={1}>{e.label}</Text>
              <Text style={st.hint}>{e.hint}</Text>
            </Focusable>
            <Focusable onPress={e.pin.toggle} style={st.pinButton} accessibilityLabel={e.pin.pinned ? `Unpin ${e.label}` : `Pin ${e.label}`}>
              <Icon name={e.pin.pinned ? 'pinned' : 'pin'} size={px(40)} color={e.pin.pinned ? colors.accent : colors.muted} />
            </Focusable>
          </View>
        ))}
      </ScrollView>
      {!isPhone && (
        <ScrollView style={st.flex} contentContainerStyle={st.rows}>
          {started.length > 0 && (
            <ItemRow title="Continue Watching" items={started} renderItem={(it) => <WideCard item={it} onPress={() => play(it)} />} />
          )}
          {onDeck.length > 0 && (
            <ItemRow title="On Deck" items={onDeck} renderItem={(it) => <PosterCard item={it} onPress={() => nav.push({ name: 'details', id: it.id })} />} />
          )}
          {home && cw.length === 0 && <Text style={[st.mutedLeft, st.moreEmpty]}>Nothing started yet.</Text>}
        </ScrollView>
      )}
    </View>
  )
}

/**
 * The menu that holding OK on a library or tab brings up. A Modal keeps focus
 * inside it on a TV, and Back closes it.
 */
function PinMenu({ target, onToggle, onClose }: { target: Pinnable; onToggle: () => void; onClose: () => void }) {
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <Pressable style={st.scrim} onPress={onClose}>
        <View style={st.pinMenu}>
          <Text style={st.dropdownText} numberOfLines={1}>{target.name}</Text>
          <Text style={st.hint}>{target.pinned ? 'Pinned to the menu.' : 'Not pinned, so only under More.'}</Text>
          <View style={st.pinMenuButtons}>
            <Button primary preferFocus label={target.pinned ? 'Unpin' : 'Pin to menu'} onPress={onToggle} style={st.gapRight} />
            <Button label="Cancel" onPress={onClose} />
          </View>
        </View>
      </Pressable>
    </Modal>
  )
}

/** The logo's drop-down: who's signed in, Settings, switching profile, signing out. */
function AccountMenu({ onClose, onSettings, onSwitchProfile, onSignOut }: { onClose: () => void; onSettings: () => void; onSwitchProfile: () => void; onSignOut: () => void }) {
  const s = current()
  const [profiles, setProfiles] = useState(false)
  useEffect(() => {
    fetchHousehold().then((m) => setProfiles(m.length > 1), () => undefined)
  }, [])
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <Pressable style={[st.scrim, st.scrimTopLeft]} onPress={onClose}>
        <View style={st.accountMenu}>
          <Text style={st.dropdownText} numberOfLines={1}>{s?.user.profileName ?? s?.user.username}</Text>
          <Text style={st.hint} numberOfLines={1}>{s?.server.replace(/^https?:\/\//, '')}</Text>
          <Button preferFocus label="Settings" onPress={onSettings} style={st.accountButton} />
          {profiles && <Button label="Switch profile" onPress={onSwitchProfile} style={st.accountButton} />}
          <Button label="Switch user (sign out)" onPress={onSignOut} style={st.accountButton} />
        </View>
      </Pressable>
    </Modal>
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
  // Again each time Back comes here, for the new Continue Watching.
  const { returns } = useScreen()
  useEffect(load, [load, returns])

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
  screenSide: { flexDirection: 'row' },
  // The side menu lies over the screen, so opening it doesn't move what's beside it.
  rail: { position: 'absolute', left: 0, top: 0, bottom: 0, width: 132, zIndex: 5, paddingTop: 36, backgroundColor: 'rgba(11,13,16,0.82)' },
  railOpen: { width: 470, backgroundColor: 'rgba(20,24,29,0.97)', borderRightWidth: 1, borderRightColor: colors.border },
  besideRail: { marginLeft: 132 },
  railLogo: { marginLeft: 20, marginBottom: 20, alignSelf: 'flex-start', borderRadius: 50 },
  railItems: { paddingBottom: 30 },
  railItem: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 14, marginVertical: 4, paddingHorizontal: 22, paddingVertical: 16 },
  railItemActive: { backgroundColor: 'rgba(45,212,191,0.12)' },
  railLabel: { marginLeft: 26, flex: 1 },
  railDot: { position: 'absolute', top: 8, right: 10 },
  logoTop: { marginRight: 36, borderRadius: 50 },
  flex: { flex: 1 },
  menu: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: gutter, paddingTop: 44, paddingBottom: 8, zIndex: 2 },
  mascot: { width: 76, height: 76 },
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
  more: { paddingHorizontal: gutter, paddingTop: 20, paddingBottom: 60 },
  moreSplit: { flexDirection: 'row' },
  moreList: { flexGrow: 0, width: 760 },
  moreEmpty: { marginLeft: gutter, marginTop: 40 },
  pinButton: { width: 84, height: 84, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.surface2 },
  scrimTopLeft: { alignItems: 'flex-start', justifyContent: 'flex-start', paddingTop: 60, paddingLeft: 60 },
  accountMenu: { backgroundColor: colors.surface, borderRadius: 22, padding: 36, width: 640, borderWidth: 1, borderColor: colors.border },
  accountButton: { marginRight: 0, marginTop: 18, alignItems: 'flex-start' },
  dropdown: { backgroundColor: colors.surface, borderRadius: 22, padding: 30, maxWidth: 1100, borderWidth: 1, borderColor: colors.border },
  dropdownRow: { flexDirection: 'row', alignItems: 'center', marginTop: 16 },
  dropdownName: { flex: 1, paddingHorizontal: 24, paddingVertical: 14, borderRadius: 16, backgroundColor: colors.surface2, marginRight: 20 },
  dropdownText: { color: colors.text, fontSize: font.heading, fontWeight: '700' },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 6 },
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.6)', alignItems: 'center', justifyContent: 'center' },
  pinMenu: { backgroundColor: colors.surface, borderRadius: 22, padding: 40, minWidth: 700, maxWidth: 1000, borderWidth: 1, borderColor: colors.border },
  pinMenuButtons: { flexDirection: 'row', marginTop: 30 },
  gapRight: { marginRight: 24 },
  searchInput: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.heading, borderRadius: 16, paddingHorizontal: 28, paddingVertical: 18, borderWidth: 2, borderColor: colors.border, maxWidth: 1100, marginBottom: 30 },
  mutedLeft: { color: colors.muted, fontSize: font.body },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  gridCell: { marginBottom: 30 },
  gridTitle: { color: colors.text, fontSize: font.small, marginTop: 8, width: 230 },
}))
