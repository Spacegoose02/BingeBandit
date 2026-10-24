import { Dimensions } from 'react-native'

// Layouts are designed on a 1920×1080 canvas. Android TV reports a 1080p
// screen as ~960×540 density pixels (and 720p boxes differently), so every
// size goes through px() to land the same on any TV.
const scale = Dimensions.get('window').width / 1920

export function px(n: number): number {
  return Math.round(n * scale * 2) / 2
}

const SIZE_KEYS = /^(width|height|min(Width|Height)|max(Width|Height)|margin\w*|padding\w*|top|bottom|left|right|gap|rowGap|columnGap|fontSize|lineHeight|letterSpacing|border\w*Radius|borderWidth)$/

/** Scales the size properties of a style sheet definition (numbers only; '100%' etc. pass through). */
export function scaled<T extends Record<string, object>>(styles: T): T {
  const out: Record<string, object> = {}
  for (const [name, style] of Object.entries(styles)) {
    const s: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(style)) {
      s[k] = typeof v === 'number' && SIZE_KEYS.test(k) ? (k === 'borderWidth' ? Math.max(1, px(v)) : px(v)) : v
    }
    out[name] = s
  }
  return out as T
}

// Same palette as the web UI: near-black with the teal accent.
export const colors = {
  bg: '#0b0d10',
  surface: '#14181d',
  surface2: '#1c2128',
  surface3: '#262c35',
  border: 'rgba(255,255,255,0.08)',
  text: '#f2f4f7',
  muted: '#98a1ad',
  accent: '#2dd4bf',
  accentText: '#04211d',
  danger: '#f87171',
  ok: '#4ade80',
}

// TVs are viewed from across the room: everything is bigger than on the web.
// (Sizes are on the 1920×1080 canvas; style sheets run them through scaled().)
export const font = {
  hero: 64,
  title: 48,
  heading: 34,
  body: 28,
  small: 22,
}

export const gutter = 80
