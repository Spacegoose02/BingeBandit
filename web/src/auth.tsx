import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from './api'
import type { User } from './types'
import { getPasskey, passkeyErrorMessage } from './passkeys'
import { askForProfile } from './profilePick'

interface AuthState {
  loading: boolean
  user: User | null
  needsSetup: boolean
  serverName: string
  refresh: () => Promise<void>
  login: (username: string, password: string, code?: string) => Promise<void>
  loginWithPasskey: () => Promise<boolean>
  /** Signs in with a session the server already set as a cookie (linking with a code). */
  signedIn: (user: User) => void
  setup: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [user, setUser] = useState<User | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [serverName, setServerName] = useState('BingeBandit')

  const refresh = useCallback(async () => {
    try {
      const s = await api.get<{ needsSetup: boolean; serverName: string }>('/api/setup')
      setNeedsSetup(s.needsSetup)
      setServerName(s.serverName || 'BingeBandit')
      setUser(s.needsSetup ? null : await api.get<User>('/api/me').catch(() => null))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
    const onUnauthorized = () => setUser(null)
    window.addEventListener('bb:unauthorized', onUnauthorized)
    return () => window.removeEventListener('bb:unauthorized', onUnauthorized)
  }, [refresh])

  const login = async (username: string, password: string, code?: string) => {
    const res = await api.post<{ user: User }>('/api/auth/login', { username, password, code, device: 'Web' })
    askForProfile()
    setUser(res.user)
  }

  /** Signs in with a passkey; false when the person cancelled the browser's prompt. */
  const loginWithPasskey = async () => {
    const { flow, options } = await api.post<{ flow: string; options: unknown }>('/api/auth/passkey/begin')
    let credential
    try {
      credential = await getPasskey(options)
    } catch (err) {
      const msg = passkeyErrorMessage(err)
      if (!msg) return false
      throw new Error(msg)
    }
    const res = await api.post<{ user: User }>('/api/auth/passkey/finish', { flow, credential, device: 'Web' })
    askForProfile()
    setUser(res.user)
    return true
  }

  const setup = async (username: string, password: string) => {
    const res = await api.post<{ user: User }>('/api/setup', { username, password, device: 'Web' })
    setNeedsSetup(false)
    setUser(res.user)
  }

  const logout = async () => {
    await api.post('/api/auth/logout').catch(() => undefined)
    setUser(null)
  }

  return (
    <AuthContext.Provider value={{ loading, user, needsSetup, serverName, refresh, login, loginWithPasskey, signedIn: setUser, setup, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth outside AuthProvider')
  return ctx
}
