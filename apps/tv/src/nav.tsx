import { createContext, useContext } from 'react'

/** A screen on the navigation stack. The TV Back button pops it. */
export type Route =
  | { name: 'home' }
  | { name: 'details'; id: number }
  | { name: 'collection'; id: number }
  | { name: 'player'; id: number; start?: number }
  | { name: 'live'; channelId: number }

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
