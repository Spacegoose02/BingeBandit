// "Are you still watching?": after STILL_WATCHING_AFTER episodes in a row
// that started by themselves with no key, click or tap in between, the
// next one waits to be asked for, so a screen left on doesn't stream (and
// the server convert) all night. The Android TV and Roku apps do the same.
export const STILL_WATCHING_AFTER = 3

let autoPlayed = 0

/** Someone pressed, clicked or tapped something. */
export function userActive() {
  autoPlayed = 0
}

/** Whether the next episode may start by itself; counts it when it may. */
export function mayAutoPlay(): boolean {
  if (autoPlayed + 1 >= STILL_WATCHING_AFTER) return false
  autoPlayed++
  return true
}

if (typeof window !== 'undefined') {
  for (const ev of ['keydown', 'pointerdown'] as const) window.addEventListener(ev, userActive, { capture: true, passive: true })
}
