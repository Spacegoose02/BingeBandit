import { useCallback, useEffect, useState } from 'react'
import { api } from './api'

/** How a profile wants Hue Sync: off (the default), following the picture, or dimmed. */
export type HueMode = 'off' | 'sync' | 'theater'

export const hueModeNames: Record<HueMode, string> = {
  off: 'Off',
  sync: 'Sync with the picture',
  theater: 'Theater (dim the lights)',
}

/**
 * This profile's Hue Sync mode, saved by the plugin. `mode` stays null while
 * loading and when the plugin isn't running, so callers can hide their switch.
 */
export function useHueMode() {
  const [mode, setModeState] = useState<HueMode | null>(null)
  useEffect(() => {
    api.get<{ mode: HueMode }>('/api/plugins/hue-sync/me').then(
      (r) => setModeState(r.mode),
      () => setModeState(null),
    )
  }, [])
  const setMode = useCallback(async (m: HueMode) => {
    const prev = mode
    setModeState(m)
    try {
      await api.put('/api/plugins/hue-sync/me', { mode: m })
    } catch (e) {
      setModeState(prev)
      throw e
    }
  }, [mode])
  return { mode, setMode }
}

/** This profile's Game Lights settings, as the plugin keeps them. */
export interface GameLightsPerson {
  enabled?: boolean
  off?: boolean
  [key: string]: unknown
}

/**
 * Game Lights' start / stop for this profile. `person` stays null while
 * loading, when the plugin isn't running and when the profile hasn't turned
 * Game Lights on (Account), so callers can hide their switch.
 */
export function useGameLights() {
  const [person, setPerson] = useState<GameLightsPerson | null>(null)
  useEffect(() => {
    api.get<{ person?: GameLightsPerson }>('/api/plugins/game-lights/me/state').then(
      (s) => setPerson(s.person?.enabled ? s.person : null),
      () => setPerson(null),
    )
  }, [])
  const setRunning = useCallback(async (running: boolean) => {
    if (!person) return
    const prev = person
    const next = { ...person, off: !running }
    setPerson(next)
    try {
      await api.put('/api/plugins/game-lights/me/settings', next)
    } catch (e) {
      setPerson(prev)
      throw e
    }
  }, [person])
  return { person, setRunning }
}
