import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { ActivityIndicator, BackHandler, StatusBar, StyleSheet, View } from 'react-native'
import { ApiError, api, loadSession, saveSession, type Session } from './src/api'
import { backInterceptor, NavContext, type Nav, type Route } from './src/nav'
import Connect from './src/screens/Connect'
import Details from './src/screens/Details'
import Home from './src/screens/Home'
import LivePlayer from './src/screens/LivePlayer'
import Player from './src/screens/Player'
import SignIn from './src/screens/SignIn'
import { colors } from './src/theme'
import type { ServerInfo, User } from './src/types'

type Phase = { name: 'loading' } | { name: 'connect' } | { name: 'signin'; server: ServerInfo } | { name: 'app' }

export default function App() {
  const [phase, setPhase] = useState<Phase>({ name: 'loading' })
  const [stack, setStack] = useState<Route[]>([{ name: 'home' }])

  // Reopen the last server; if the session was revoked, sign in again.
  useEffect(() => {
    loadSession().then(async (s) => {
      if (!s) return setPhase({ name: 'connect' })
      try {
        const user = await api.get<User>('/api/me')
        await saveSession({ ...s, user })
        setPhase({ name: 'app' })
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
      push: (r) => setStack((st) => [...st, r]),
      replace: (r) => setStack((st) => [...st.slice(0, -1), r]),
      back: () => setStack((st) => (st.length > 1 ? st.slice(0, -1) : st)),
    }),
    [],
  )

  // The remote's Back button pops a screen; on Home it leaves the app.
  useEffect(() => {
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      if (backInterceptor.current?.()) return true
      if (phase.name === 'signin') {
        setPhase({ name: 'connect' })
        return true
      }
      if (phase.name === 'app' && stack.length > 1) {
        nav.back()
        return true
      }
      return false
    })
    return () => sub.remove()
  }, [phase, stack.length, nav])

  const signedIn = useCallback(async (s: Session) => {
    await saveSession(s)
    setStack([{ name: 'home' }])
    setPhase({ name: 'app' })
  }, [])

  const top = stack[stack.length - 1]
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
    case 'app':
      // Keyed so pushing the same screen type with new params starts fresh.
      switch (top.name) {
        case 'home':
          screen = <Home onSignedOut={() => setPhase({ name: 'connect' })} />
          break
        case 'details':
          screen = <Details key={`d${top.id}`} id={top.id} />
          break
        case 'player':
          screen = <Player key={`p${top.id}-${stack.length}`} id={top.id} start={top.start} />
          break
        case 'live':
          screen = <LivePlayer key={`l${top.channelId}`} channelId={top.channelId} />
          break
      }
  }

  return (
    <NavContext.Provider value={nav}>
      <StatusBar hidden />
      <View style={styles.root}>{screen}</View>
    </NavContext.Provider>
  )
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.bg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
})
