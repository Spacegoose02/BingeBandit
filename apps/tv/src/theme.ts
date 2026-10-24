import { Dimensions, Platform } from 'react-native'

/** The same APK runs on phones; layouts check this to switch to touch. */
export const isPhone = !Platform.isTV

// Layouts are designed on a 1920×1080 canvas. Android TV reports a 1080p
// screen as ~960×540 density pixels (and 720p boxes differently), so every
// size goes through px() to land the same on any TV. Phones size from their
// short side (a phone is ~750 canvas units across in portrait), so text keeps
// its size when the phone turns.
const win = Dimensions.get('window')
const scale = isPhone ? Math.min(win.width, win.height) / 750 : win.width / 1920

/** Converts density pixels (e.g. from useWindowDimensions) back to canvas units. */
export function units(dp: number): number {
  return dp / scale
}

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
// (Sizes are canvas units; style sheets run them through scaled().)
export const font = isPhone
  ? { hero: 50, title: 42, heading: 32, body: 28, small: 24 }
  : { hero: 64, title: 48, heading: 34, body: 28, small: 22 }

export const gutter = isPhone ? 32 : 80
