import AsyncStorage from '@react-native-async-storage/async-storage'
import type { User } from './types'

/** A signed-in server: where it is and the session token for it. */
export interface Session {
  server: string // e.g. http://192.168.1.10:8484
  token: string
  user: User
}

let session: Session | null = null
const KEY = 'bb.session'

export async function loadSession(): Promise<Session | null> {
  try {
    const raw = await AsyncStorage.getItem(KEY)
    session = raw ? (JSON.parse(raw) as Session) : null
  } catch {
    session = null
  }
  return session
}

export async function saveSession(s: Session | null) {
  session = s
  cache.clear() // another server or profile sees different things
  if (s) await AsyncStorage.setItem(KEY, JSON.stringify(s))
  else await AsyncStorage.removeItem(KEY)
}

export function current(): Session | null {
  return session
}

export class ApiError extends Error {
  constructor(public status: number, message: string, public data?: Record<string, unknown>) {
    super(message)
  }
}

/** Normalises what someone typed into a server URL. */
export function normalizeServer(input: string): string {
  let s = input.trim().replace(/\/+$/, '')
  if (!/^https?:\/\//i.test(s)) s = 'http://' + s
  if (!/:\d+$/.test(s.replace(/^https?:\/\//, '')) && s.startsWith('http://')) s += ':8484'
  return s
}

export async function request<T>(method: string, path: string, body?: unknown, server = session?.server, timeoutMs = 15000): Promise<T> {
  if (!server) throw new ApiError(0, 'No server chosen')
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (session?.token && server === session.server) headers.Authorization = `Bearer ${session.token}`
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), timeoutMs)
  let res: Response
  try {
    res = await fetch(server + path, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined, signal: ctl.signal })
  } catch {
    throw new ApiError(0, `Can't reach ${server.replace(/^https?:\/\//, '')}. Is the server on?`)
  } finally {
    clearTimeout(timer)
  }
  if (res.status === 204 || res.status === 202) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, data?.error ?? `Server error ${res.status}`, data ?? undefined)
  return data as T
}

// The last answer to each GET, so a screen that comes back (Back from the
// player or details remounts it) can draw at once while it refreshes.
const cache = new Map<string, unknown>()

/** What the server last said for this path, if anything. */
export function cached<T>(path: string): T | undefined {
  return cache.get(path) as T | undefined
}

// The player's last progress report, sent as it closes. Screens that show
// progress wait for it so Continue Watching has the new position.
let lastWrite: Promise<unknown> = Promise.resolve()

/** Lets later loads wait for this write to land (see writesDone). */
export function trackWrite<T>(p: Promise<T>): Promise<T> {
  lastWrite = p.catch(() => undefined)
  return p
}

export function writesDone(): Promise<unknown> {
  return lastWrite
}

export const api = {
  get: <T>(path: string) =>
    request<T>('GET', path).then((data) => {
      cache.set(path, data)
      return data
    }),
  post: <T>(path: string, body: unknown = {}) => request<T>('POST', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

/** Headers for media and images, which load outside fetch. */
export function authHeaders(): Record<string, string> {
  return session ? { Authorization: `Bearer ${session.token}` } : {}
}

/** Image source for artwork; TMDB widths: w185 w342 w500 w780 w1280. */
export function image(url: string | undefined, width: string) {
  if (!url || !session) return undefined
  const full = url.startsWith('/') ? session.server + url : url
  return { uri: full.includes('/api/images/') ? `${full}?w=${width}` : full, headers: authHeaders() }
}

export function absolute(path: string): string {
  return (session?.server ?? '') + path
}
