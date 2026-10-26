import { createContext, useContext } from 'react'

/** A screen on the navigation stack. The TV Back button pops it. */
export type Route =
  | { name: 'home' }
  | { name: 'details'; id: number }
  | { name: 'collection'; id: number }
  | { name: 'player'; id: number; start?: number }
  | { name: 'live'; channelId: number }
  | { name: 'trailer'; url: string; title: string }

export interface Nav {
  push: (r: Route) => void
  replace: (r: Route) => void
  back: () => void
}

export const NavContext = createContext<Nav>({ push: () => {}, replace: () => {}, back: () => {} })

export function useNav() {
  return useContext(NavContext)
}

/**
 * Lets the current screen take the Back button first (e.g. to close a menu).
 * The function returns true when it handled Back.
 */
export const backInterceptor: { current: (() => boolean) | null } = { current: null }

/**
 * Screens under the top one stay mounted (hidden), so Back returns to the
 * same scroll position. Each screen learns whether it is on top, and how many
 * times it has come back to the top, to refresh what may have changed.
 */
export interface ScreenState {
  active: boolean
  returns: number
}

export const ScreenContext = createContext<ScreenState>({ active: true, returns: 0 })

export function useScreen() {
  return useContext(ScreenContext)
}

export interface FocusTarget {
  requestTVFocus?: () => void
}

/**
 * Focusable controls report here as they take focus, so Back can put focus
 * back on the last one. Kept apart from ScreenContext so it never changes and
 * never redraws every card.
 */
export const FocusMemoryContext = createContext<(view: FocusTarget | null) => void>(() => {})
