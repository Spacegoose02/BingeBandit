import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ActivityIndicator, BackHandler, StatusBar, StyleSheet, Text, View } from 'react-native'
import { SafeAreaProvider, useSafeAreaInsets } from 'react-native-safe-area-context'
import { ApiError, api, loadSession, saveSession, type Session } from './src/api'
import { backInterceptor, FocusMemoryContext, homeBack, NavContext, ScreenContext, type FocusTarget, type Nav, type Route } from './src/nav'
import Connect from './src/screens/Connect'
import Details from './src/screens/Details'
import { CollectionScreen } from './src/screens/Library'
import Home from './src/screens/Home'
import LivePlayer from './src/screens/LivePlayer'
import Player, { TrailerScreen } from './src/screens/Player'
import Profiles, { needsPicker } from './src/screens/Profiles'
import SignIn from './src/screens/SignIn'
import { colors, font, isPhone, scaled } from './src/theme'
import type { ServerInfo, User } from './src/types'
import { ScoreBug } from './src/scorebug'
import { Toasts } from './src/toasts'
import { ItemMenuHost } from './src/itemMenu'
import { TogetherInvites } from './src/together'

type Phase =
  | { name: 'loading' }
  | { name: 'connect' }
  | { name: 'signin'; server: ServerInfo }
  | { name: 'profiles'; cancelable: boolean } // "Who's watching?", after sign-in or from Settings
  | { name: 'app' }

// A screen on the stack, with a key that stays its own while it is there.
interface Entry {
  route: Route
  key: number
}
let nextKey = 0
const entry = (route: Route): Entry => ({ route, key: nextKey++ })
const homeStack = () => [entry({ name: 'home' })]
// A second Back within this long leaves the app.
const EXIT_WINDOW = 2500

export default function App() {
  const [phase, setPhase] = useState<Phase>({ name: 'loading' })
  const [stack, setStack] = useState<Entry[]>(homeStack)

  // Reopen the last server; if the session was revoked, sign in again. Like
  // after signing in, ask who's watching unless the account says who (or
  // the TV was linked straight to a profile).
  useEffect(() => {
    loadSession().then(async (s) => {
      if (!s) return setPhase({ name: 'connect' })
      try {
        const user = await api.get<User>('/api/me')
        await saveSession({ ...s, user })
        setPhase(!s.profileChosen && (await needsPicker()) ? { name: 'profiles', cancelable: false } : { name: 'app' })
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          await saveSession(null)
          setPhase({ name: 'signin', server: { url: s.server, name: 'BingeBandit', version: '' } })
        } else {
          setPhase({ name: 'app' }) // offline for now: Home shows the error with Try again
        }
      }
    })
  }, [])

  const nav: Nav = useMemo(
    () => ({
      push: (r) => setStack((st) => [...st, entry(r)]),
      replace: (r) => setStack((st) => [...st.slice(0, -1), entry(r)]),
      back: () => setStack((st) => (st.length > 1 ? st.slice(0, -1) : st)),
    }),
    [],
  )

  // Leaving takes a second Back while the hint shows, so a stray press doesn't close the app.
  const lastBack = useRef(0)
  const [exitHint, setExitHint] = useState(false)
  useEffect(() => {
    if (!exitHint) return
    const t = setTimeout(() => setExitHint(false), EXIT_WINDOW)
    return () => clearTimeout(t)
  }, [exitHint])

  // The remote's Back button pops a screen; on Home it goes to the Home tab
  // and the menu first, and only then, pressed twice, leaves the app.
  useEffect(() => {
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      if (backInterceptor.current?.()) return true
      if (phase.name === 'signin') {
        setPhase({ name: 'connect' })
        return true
      }
      if (phase.name === 'profiles' && phase.cancelable) {
        setPhase({ name: 'app' })
        return true
      }
      if (phase.name === 'app' && stack.length > 1) {
        nav.back()
        return true
      }
      if (phase.name === 'app' && homeBack.current?.()) return true
      const now = Date.now()
      if (now - lastBack.current < EXIT_WINDOW) return false
      lastBack.current = now
      setExitHint(true)
      return true
    })
    return () => sub.remove()
  }, [phase, stack.length, nav])

  const signedIn = useCallback(async (s: Session) => {
    await saveSession(s)
    setStack(homeStack())
    // An account with profiles asks who's watching first, unless whoever
    // linked this TV picked the profile it opens as.
    setPhase(!s.profileChosen && (await needsPicker()) ? { name: 'profiles', cancelable: false } : { name: 'app' })
  }, [])

  const profileChosen = useCallback(() => {
    setStack(homeStack())
    setPhase({ name: 'app' })
  }, [])

  const top = stack[stack.length - 1].route
  let screen: React.ReactNode
  switch (phase.name) {
    case 'loading':
      screen = (
        <View style={styles.center}>
          <ActivityIndicator color={colors.accent} size="large" />
        </View>
      )
      break
    case 'connect':
      screen = <Connect onChosen={(server) => setPhase({ name: 'signin', server })} />
      break
    case 'signin':
      screen = <SignIn server={phase.server} onSignedIn={signedIn} onBack={() => setPhase({ name: 'connect' })} />
      break
    case 'profiles':
      screen = <Profiles onDone={profileChosen} onCancel={phase.cancelable ? () => setPhase({ name: 'app' }) : undefined} />
      break
    case 'app':
      // Every screen on the stack stays mounted, hidden under the top one,
      // so Back lands where you were instead of at the top of the list.
      screen = stack.map((e, i) => (
        <StackScreen key={e.key} active={i === stack.length - 1}>
          {routeScreen(e.route, {
            onSignedOut: () => setPhase({ name: 'connect' }),
            onSwitchProfile: () => setPhase({ name: 'profiles', cancelable: true }),
          })}
        </StackScreen>
      ))
  }

  const playing = top.name === 'player' || top.name === 'live' || top.name === 'trailer'
  const fullScreen = !isPhone || top.name === 'player' || top.name === 'live' || top.name === 'trailer'
  return (
    <SafeAreaProvider>
      <NavContext.Provider value={nav}>
        <StatusBar hidden={fullScreen} translucent backgroundColor="transparent" barStyle="light-content" />
        <ItemMenuHost>
          <Insets enabled={!fullScreen}>{screen}</Insets>
        </ItemMenuHost>
        {phase.name === 'app' && <Toasts playing={playing} />}
        {phase.name === 'app' && <TogetherInvites playing={playing} />}
        {phase.name === 'app' && <ScoreBug playing={playing && top.name !== 'trailer'} channelId={top.name === 'live' ? top.channelId : undefined} />}
        {exitHint && (
          <View style={styles.exitHint} pointerEvents="none">
            <Text style={styles.exitHintText}>Press Back again to exit</Text>
          </View>
        )}
      </NavContext.Provider>
    </SafeAreaProvider>
  )
}

function routeScreen(r: Route, home: { onSignedOut: () => void; onSwitchProfile: () => void }): React.ReactNode {
  switch (r.name) {
    case 'home':
      return <Home {...home} />
    case 'details':
      return <Details id={r.id} />
    case 'collection':
      return <CollectionScreen id={r.id} />
    case 'player':
      return <Player id={r.id} start={r.start} together={r.together} />
    case 'live':
      return <LivePlayer channelId={r.channelId} />
    case 'trailer':
      return <TrailerScreen url={r.url} title={r.title} />
  }
}

/**
 * One screen on the stack. Hidden screens are display: none, which Android
 * makes invisible, so they keep their scroll position but can't take focus.
 * Coming back to the top puts focus back on the control that had it.
 */
function StackScreen({ active, children }: { active: boolean; children: React.ReactNode }) {
  const last = useRef<FocusTarget | null>(null)
  const remember = useCallback((v: FocusTarget | null) => {
    last.current = v
  }, [])
  const [returns, setReturns] = useState(0)
  const wasActive = useRef(active)
  useEffect(() => {
    const back = active && !wasActive.current
    wasActive.current = active
    if (!back) return
    setReturns((n) => n + 1)
    if (isPhone) return
    // After Android has made the screen visible again.
    const t = setTimeout(() => last.current?.requestTVFocus?.(), 50)
    return () => clearTimeout(t)
  }, [active])
  const state = useMemo(() => ({ active, returns }), [active, returns])
  return (
    <View style={[styles.root, !active && styles.hidden]} pointerEvents={active ? 'auto' : 'none'}>
      <FocusMemoryContext.Provider value={remember}>
        <ScreenContext.Provider value={state}>{children}</ScreenContext.Provider>
      </FocusMemoryContext.Provider>
    </View>
  )
}

/** Phones draw behind the status and navigation bars (edge-to-edge); keep screens clear of them. */
function Insets({ enabled, children }: { enabled: boolean; children: React.ReactNode }) {
  const i = useSafeAreaInsets()
  const pad = enabled ? { paddingTop: i.top, paddingBottom: i.bottom, paddingLeft: i.left, paddingRight: i.right } : null
  return <View style={[styles.root, pad]}>{children}</View>
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.bg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
  hidden: { display: 'none' },
  ...scaled({
    exitHint: { position: 'absolute', bottom: 80, alignSelf: 'center', backgroundColor: 'rgba(20,24,29,0.94)', borderRadius: 20, paddingVertical: 20, paddingHorizontal: 36, borderWidth: 2, borderColor: colors.border },
    exitHintText: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  }),
})
