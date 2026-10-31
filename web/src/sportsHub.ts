import type { LiveChannel, SportsGame } from './types'

// Shared by the Sports page and Multi-View (the TV app has the same in
// apps/tv/src/sports/hub.ts).

/** Live first, then soonest to start, then results newest first. */
export function byState(a: SportsGame, b: SportsGame): number {
  const rank = { in: 0, pre: 1, post: 2 }
  if (rank[a.state] !== rank[b.state]) return rank[a.state] - rank[b.state]
  const d = new Date(a.start).getTime() - new Date(b.start).getTime()
  return a.state === 'post' ? -d : d
}

/** "Bills at Jets", or a race's name. */
export function matchup(g: SportsGame): string {
  if (!g.away.id && !g.home.id) return g.name ?? ''
  return `${g.away.shortName || g.away.name} at ${g.home.shortName || g.home.name}`
}

/** The score as one line ("BUF 21 · NYJ 17 · 3rd 4:12"), or a race's leader. */
export function scoreLine(g: SportsGame): string {
  if (!g.away.id && !g.home.id) return [g.race?.[0] && `P1 ${g.race[0].shortName}`, g.detail].filter(Boolean).join(' · ')
  return `${g.away.abbr} ${g.away.score} · ${g.home.abbr} ${g.home.score} · ${g.detail}`
}

// Channel names that are sports networks, beside channels showing a game right now.
const SPORTS_NAME =
  /\b(espn\w*|fs[12]|fox sports|nfl|redzone|nba|mlb|nhl|mls|golf|tennis|bein|tsn\d?|sportsnet|sky sports|tnt sports|bt sport|dazn|cbs sports|big ten|btn|sec network|acc network|pac-?12|willow|super ?sport|eurosport|premier sports|marquee|bally|msg|nesn|masn|sny|yes network|altitude|root sports|spectrum sportsnet|stadium|fight|ufc|wwe|nbc sports|peacock|racing|f1|nascar|motor)\b/i

/** Whether a Live TV channel looks like a sports channel. */
export function isSportsChannel(c: LiveChannel): boolean {
  if (SPORTS_NAME.test(c.name) || /sport/i.test(c.group ?? '')) return true
  return /sport/i.test(`${c.now?.category ?? ''},${c.now?.genres ?? ''}`)
}

/** The game each channel is showing (live ones win), from any lists of games. */
export function gamesByChannel(...lists: SportsGame[][]): Map<number, SportsGame> {
  const m = new Map<number, SportsGame>()
  for (const g of lists.flat().sort(byState)) for (const c of g.channels ?? []) if (!m.has(c.id)) m.set(c.id, g)
  return m
}

// The channels picked for Multi-View from the Sports page, kept for this tab.
export const MAX_SCREENS = 4
const PICKS = 'bb.multiview'

export function multiViewPicks(): number[] {
  try {
    const v = JSON.parse(sessionStorage.getItem(PICKS) ?? '[]')
    return Array.isArray(v) ? v.filter((x) => typeof x === 'number').slice(0, MAX_SCREENS) : []
  } catch {
    return []
  }
}

export function setMultiViewPicks(ids: number[]) {
  try {
    sessionStorage.setItem(PICKS, JSON.stringify(ids.slice(0, MAX_SCREENS)))
  } catch {
    // No storage: picks last until the page changes.
  }
}

/** The Multi-View address for some channels. */
export const multiViewUrl = (ids: number[]) => `/multiview${ids.length ? `?ch=${ids.join(',')}` : ''}`
