import { api } from './api'

/** How a profile wants Hue Sync: off (the default), following the picture, or dimmed. */
export type HueMode = 'off' | 'sync' | 'theater'

export const hueModeNames: Record<HueMode, string> = { off: 'Off', sync: 'Sync', theater: 'Theater' }

export const nextHueMode = (m: HueMode): HueMode => (m === 'sync' ? 'theater' : m === 'theater' ? 'off' : 'sync')

/** This profile's Hue Sync mode; null when the plugin isn't running. */
export function fetchHueMode(): Promise<HueMode | null> {
  return api.get<{ mode: HueMode }>('/api/plugins/hue-sync/me').then(
    (r) => r.mode ?? 'off',
    () => null,
  )
}

export function saveHueMode(mode: HueMode): Promise<unknown> {
  return api.put('/api/plugins/hue-sync/me', { mode })
}

/** Whether this profile turned Game Lights on (on the web); false when the plugin isn't running. */
export function fetchGameLightsOn(): Promise<boolean> {
  return api.get<{ person?: { enabled?: boolean } }>('/api/plugins/game-lights/me/state').then(
    (s) => !!s.person?.enabled,
    () => false,
  )
}

/** This profile's Game Lights settings, as the plugin keeps them. */
export interface GameLightsPerson {
  enabled?: boolean
  off?: boolean
  [key: string]: unknown
}

/** This profile's Game Lights settings; null when the plugin isn't running or the profile hasn't turned it on. */
export function fetchGameLights(): Promise<GameLightsPerson | null> {
  return api.get<{ person?: GameLightsPerson }>('/api/plugins/game-lights/me/state').then(
    (s) => (s.person?.enabled ? s.person : null),
    () => null,
  )
}

export function saveGameLights(person: GameLightsPerson): Promise<unknown> {
  return api.put('/api/plugins/game-lights/me/settings', person)
}

/** What Settings needs to show Game Lights; null when the plugin isn't running. */
export interface GameLightsSetup {
  paired: boolean
  person: GameLightsPerson & { area?: string }
  areas: { id: string; name: string; type: string }[]
}

export function fetchGameLightsSetup(): Promise<GameLightsSetup | null> {
  return api.get<GameLightsSetup>('/api/plugins/game-lights/me/state').then(
    (s) => ({ ...s, person: s.person ?? {}, areas: s.areas ?? [] }),
    () => null,
  )
}
