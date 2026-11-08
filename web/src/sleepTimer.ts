// The sleep timer: pauses playback after a while, or at the end of the
// episode instead of starting the next one. It belongs to the app, not one
// player, so it carries on from one episode to the next; pausing lets the
// screen and the computer sleep.

import { useEffect, useState } from 'react'

export type SleepSetting = { at: number } | { episode: true } | null

let setting: SleepSetting = null
const listeners = new Set<() => void>()

export const SLEEP_MINUTES = [15, 30, 45, 60, 90]

/** Sets the timer: minutes from now, 'episode' for the end of this one, or 0 to turn it off. */
export function setSleep(v: number | 'episode') {
  setting = v === 'episode' ? { episode: true } : v > 0 ? { at: Date.now() + v * 60_000 } : null
  for (const l of listeners) l()
}

export function sleepSetting(): SleepSetting {
  return setting
}

/** Whether the timer stops playback at the end of this episode. */
export function sleepAtEpisodeEnd(): boolean {
  return !!setting && 'episode' in setting
}

/** The timer has run out: it's cleared and the caller pauses. */
export function sleepDue(): boolean {
  if (setting && 'at' in setting && Date.now() >= setting.at) {
    setSleep(0)
    return true
  }
  return false
}

export function sleepLabel(): string {
  if (!setting) return 'Off'
  if ('episode' in setting) return 'End of episode'
  const min = Math.max(1, Math.ceil((setting.at - Date.now()) / 60_000))
  return `${min} min left`
}

/** Redraws once a minute while a timer runs, and when it changes. */
export function useSleepTimer(): SleepSetting {
  const [, redraw] = useState(0)
  useEffect(() => {
    const l = () => redraw((n) => n + 1)
    listeners.add(l)
    const t = setInterval(l, 15_000)
    return () => {
      listeners.delete(l)
      clearInterval(t)
    }
  }, [])
  return setting
}
