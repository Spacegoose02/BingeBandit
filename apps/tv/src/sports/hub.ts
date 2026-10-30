import { useEffect, useState } from 'react'
import { api } from '../api'
import type { LiveChannel, SportsGame } from '../types'

/** Each sport's stripe colour on cards and filter chips. */
export const sportColors: Record<string, string> = {
  football: '#f5b942',
  basketball: '#f59e42',
  baseball: '#f87171',
  hockey: '#60a5fa',
  soccer: '#4ade80',
  rugby: '#a78bfa',
  cricket: '#facc15',
  'australian-football': '#fb7185',
  volleyball: '#f472b6',
  lacrosse: '#2dd4bf',
  mma: '#e5e7eb',
}

export const sportNames: Record<string, string> = {
  football: 'Football',
  basketball: 'Basketball',
  baseball: 'Baseball',
  hockey: 'Hockey',
  soccer: 'Soccer',
  rugby: 'Rugby',
  cricket: 'Cricket',
  'australian-football': 'Aussie rules',
  volleyball: 'Volleyball',
  lacrosse: 'Lacrosse',
  mma: 'UFC',
}

/** A day as the server takes it (YYYYMMDD), `offset` days from today. */
export function dayKey(offset: number): string {
  const d = new Date(Date.now() + offset * 86400000)
  return `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
}

export function dayLabel(offset: number): string {
  if (offset === 0) return 'Today'
  if (offset === -1) return 'Yesterday'
  if (offset === 1) return 'Tomorrow'
  return new Date(Date.now() + offset * 86400000).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
}

export function startLabel(iso: string) {
  const d = new Date(iso)
  const time = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
  if (d.toDateString() === new Date().toDateString()) return `Today ${time}`
  if (d.toDateString() === new Date(Date.now() + 86400000).toDateString()) return `Tomorrow ${time}`
  return `${d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })} ${time}`
}

/** Starting within the next hour. */
export function startingSoon(g: SportsGame): boolean {
  const t = new Date(g.start).getTime() - Date.now()
  return g.state === 'pre' && t > -15 * 60000 && t < 60 * 60000
}

/** Live first, then soonest to start, then results newest first. */
export function byState(a: SportsGame, b: SportsGame): number {
  const rank = { in: 0, pre: 1, post: 2 }
  if (rank[a.state] !== rank[b.state]) return rank[a.state] - rank[b.state]
  const d = new Date(a.start).getTime() - new Date(b.start).getTime()
  return a.state === 'post' ? -d : d
}

// Spoiler-free mode (the spoilerFree preference) hides scores until someone
// asks to see a game's. What they've revealed lasts until the app closes.
const revealed = new Set<string>()
const listeners = new Set<() => void>()
let spoilerFree = false

export function setSpoilerFree(on: boolean) {
  spoilerFree = on
  listeners.forEach((l) => l())
}

export function revealScore(gameId: string) {
  revealed.add(gameId)
  listeners.forEach((l) => l())
}

/** Whether a game's score shows: not before it starts, not while spoiler-free and unrevealed. */
export function scoreShown(g: SportsGame): boolean {
  return g.state !== 'pre' && (!spoilerFree || revealed.has(g.id))
}

export function isSpoilerFree(): boolean {
  return spoilerFree
}

/** Redraws the caller when spoiler-free mode or a reveal changes. */
export function useSpoilers(): number {
  const [n, setN] = useState(0)
  useEffect(() => {
    const l = () => setN((x) => x + 1)
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  }, [])
  return n
}

/** Reads the profile's preferences, applies spoiler-free mode, and returns them. */
export async function loadSportsPrefs(): Promise<SportsPrefs> {
  const p = await api.get<SportsPrefs>('/api/me/prefs')
  setSpoilerFree(!!p.spoilerFree)
  return p
}

export interface MultiViewLayout {
  name: string
  channels: number[]
}

export interface SportsPrefs {
  spoilerFree?: boolean
  scoreBug?: boolean
  multiViewLayouts?: MultiViewLayout[] | null
  [key: string]: unknown
}

/** Changes some preferences, keeping the rest as the server has them. */
export async function patchPrefs(patch: Partial<SportsPrefs>): Promise<SportsPrefs> {
  const cur = await api.get<SportsPrefs>('/api/me/prefs')
  const next = await api.put<SportsPrefs>('/api/me/prefs', { ...cur, ...patch })
  if ('spoilerFree' in patch) setSpoilerFree(!!next.spoilerFree)
  return next
}

// The channels picked for Multi-View from the Sports Hub, in screen order.
export const MAX_SCREENS = 4
export const multiViewPicks: number[] = []

/** Adds a channel to the next Multi-View; false when it's full or already there. */
export function pickForMultiView(channelId: number): boolean {
  if (multiViewPicks.includes(channelId) || multiViewPicks.length >= MAX_SCREENS) return false
  multiViewPicks.push(channelId)
  return true
}

// Channel names that are sports networks, for the Channels tab and the
// Multi-View channel picker, beside channels showing a game right now.
const SPORTS_NAME =
  /\b(espn\w*|fs[12]|fox sports|nfl|redzone|nba|mlb|nhl|mls|golf|tennis|bein|tsn\d?|sportsnet|sky sports|tnt sports|bt sport|dazn|cbs sports|big ten|btn|sec network|acc network|pac-?12|willow|super ?sport|eurosport|premier sports|marquee|bally|msg|nesn|masn|sny|yes network|altitude|root sports|spectrum sportsnet|stadium|fight|ufc|wwe|nbc sports|peacock|racing|f1|nascar|motor)\b/i

/** Whether a Live TV channel looks like a sports channel. */
export function isSportsChannel(c: LiveChannel): boolean {
  if (SPORTS_NAME.test(c.name) || /sport/i.test(c.group ?? '')) return true
  const cat = `${c.now?.category ?? ''},${c.now?.genres ?? ''}`
  return /sport/i.test(cat)
}
