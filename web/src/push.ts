import { api } from './api'
import { desktop } from './desktop'

/** A browser that gets push notifications, as GET /api/push lists it. */
export interface PushDevice {
  id: number
  endpoint: string
  device: string
  kindsOff: string[]
  createdAt: number
  lastUsed?: number
}

export interface PushStatus {
  publicKey: string
  devices: PushDevice[]
  kinds: string[]
}

/** Why push can't be turned on here, or '' when it can. */
export function pushUnavailable(): string {
  if (desktop) return 'The Windows app can’t get push notifications. Turn them on in a browser on your phone or computer.'
  if (!window.isSecureContext)
    return 'Browsers only allow push notifications on a secure (https://) address. Open BingeBandit through its public https address to turn them on.'
  const ios = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  const standalone = window.matchMedia?.('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone
  if (ios && !standalone) return 'On an iPhone or iPad, first add BingeBandit to your Home Screen (Share → Add to Home Screen), then open it from there.'
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window))
    return 'This browser doesn’t support push notifications.'
  return ''
}

/** Registers the service worker that shows push notifications. */
export function registerServiceWorker() {
  if (!('serviceWorker' in navigator) || !window.isSecureContext || desktop) return
  navigator.serviceWorker.register('/sw.js').catch(() => undefined)
  // A notification clicked while a tab the worker can't steer is open.
  navigator.serviceWorker.addEventListener('message', (e) => {
    if (e.data?.type === 'bb:open' && typeof e.data.link === 'string' && e.data.link.startsWith('/')) {
      window.history.pushState({}, '', e.data.link)
      window.dispatchEvent(new PopStateEvent('popstate'))
    }
  })
}

async function registration() {
  const reg = (await navigator.serviceWorker.getRegistration('/')) ?? (await navigator.serviceWorker.register('/sw.js'))
  return navigator.serviceWorker.ready.then(() => reg)
}

/** This browser's push subscription, if it has one. */
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (pushUnavailable()) return null
  const reg = await navigator.serviceWorker.getRegistration('/')
  return (await reg?.pushManager.getSubscription()) ?? null
}

function keyBytes(b64: string): Uint8Array<ArrayBuffer> {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (b64.length % 4)) % 4))
  const out = new Uint8Array(new ArrayBuffer(s.length))
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}

function deviceName(): string {
  const ua = navigator.userAgent
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /SamsungBrowser/.test(ua)
        ? 'Samsung Internet'
        : /Firefox\//.test(ua)
          ? 'Firefox'
          : /Chrome\//.test(ua)
            ? 'Chrome'
            : /Safari\//.test(ua)
              ? 'Safari'
              : 'Browser'
  const os = /Android/.test(ua)
    ? 'Android'
    : /iPhone|iPod/.test(ua)
      ? 'iPhone'
      : /iPad/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
        ? 'iPad'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Mac OS X/.test(ua)
            ? 'Mac'
            : /CrOS/.test(ua)
              ? 'ChromeOS'
              : /Linux/.test(ua)
                ? 'Linux'
                : ''
  return os ? `${browser} on ${os}` : browser
}

/** Asks for permission, subscribes this browser and tells the server. */
export async function turnOnPush(publicKey: string): Promise<PushDevice> {
  const why = pushUnavailable()
  if (why) throw new Error(why)
  const permission = await Notification.requestPermission()
  if (permission !== 'granted')
    throw new Error(
      permission === 'denied'
        ? 'Notifications are blocked for this site. Allow them in the browser’s site settings, then try again.'
        : 'Notifications weren’t allowed.',
    )
  const reg = await registration()
  let sub = await reg.pushManager.getSubscription()
  // A subscription made with another server's key can't be reused.
  const old = sub?.options.applicationServerKey
  if (sub && old && btoa(String.fromCharCode(...new Uint8Array(old))) !== btoa(String.fromCharCode(...keyBytes(publicKey)))) {
    await sub.unsubscribe()
    sub = null
  }
  try {
    sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) })
  } catch (e) {
    throw new Error(
      `This browser couldn’t sign up with its push service (${(e as Error).message}). Private windows and some privacy settings block it; try a normal window.`,
    )
  }
  const json = sub.toJSON()
  return api.post<PushDevice>('/api/push/devices', { endpoint: json.endpoint, keys: json.keys, device: deviceName() })
}

/** Stops push in this browser and forgets it on the server. */
export async function turnOffPush(devices?: PushDevice[]) {
  const sub = await currentSubscription()
  if (!sub) return
  const list = devices ?? (await api.get<PushStatus>('/api/push')).devices
  const mine = list.find((d) => d.endpoint === sub.endpoint)
  if (mine) await api.del(`/api/push/devices/${mine.id}`).catch(() => undefined)
  await sub.unsubscribe().catch(() => undefined)
}
