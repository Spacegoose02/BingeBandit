import { desktop } from './desktop'

/** Chrome's install event; not in TypeScript's DOM types yet. */
interface InstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

let deferred: InstallPromptEvent | null = null
const listeners = new Set<() => void>()
const changed = () => listeners.forEach((f) => f())

/** Keeps the browser's install prompt for the Install button. Call once at startup: it fires early. */
export function listenForInstallPrompt() {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferred = e as InstallPromptEvent
    changed()
  })
  window.addEventListener('appinstalled', () => {
    deferred = null
    changed()
  })
}

export function onInstallChange(f: () => void) {
  listeners.add(f)
  return () => void listeners.delete(f)
}

export const isIOS = () =>
  /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)

/** Opened from the Home Screen or as an installed app. */
export const isInstalled = () =>
  !!desktop ||
  !!window.matchMedia?.('(display-mode: standalone)').matches ||
  !!(navigator as { standalone?: boolean }).standalone

export const canPromptInstall = () => !!deferred

/** Shows the browser's install dialog. True if the person installed it. */
export async function promptInstall(): Promise<boolean> {
  const e = deferred
  if (!e) return false
  deferred = null
  changed()
  await e.prompt()
  return (await e.userChoice).outcome === 'accepted'
}
