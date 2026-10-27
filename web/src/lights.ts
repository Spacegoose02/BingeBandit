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
