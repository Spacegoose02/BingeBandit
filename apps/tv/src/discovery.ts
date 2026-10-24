import NetInfo from '@react-native-community/netinfo'
import type { ServerInfo } from './types'

const PORT = 8484

/** Asks one address whether it's a BingeBandit server. */
export async function probe(url: string, timeoutMs = 1500): Promise<ServerInfo | null> {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), timeoutMs)
  try {
    const res = await fetch(url + '/api/health', { signal: ctl.signal })
    const data = await res.json()
    if (data?.product !== 'BingeBandit' && data?.status !== 'ok') return null
    return { url, name: data.name || 'BingeBandit', version: data.version || '' }
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

/**
 * Finds servers on the home network by asking every address in this device's
 * /24 (e.g. 192.168.1.1–254) on port 8484. Takes a couple of seconds.
 */
export async function scan(onFound: (s: ServerInfo) => void): Promise<number> {
  const state = await NetInfo.fetch()
  const ip = (state.details as { ipAddress?: string } | null)?.ipAddress
  if (!ip || !/^\d+\.\d+\.\d+\.\d+$/.test(ip)) return 0
  const prefix = ip.split('.').slice(0, 3).join('.')
  const hosts = Array.from({ length: 254 }, (_, i) => `http://${prefix}.${i + 1}:${PORT}`)
  let found = 0
  // Fifty at a time keeps cheap TV sticks from choking.
  for (let i = 0; i < hosts.length; i += 50) {
    const batch = await Promise.all(hosts.slice(i, i + 50).map((h) => probe(h, 900)))
    for (const s of batch) {
      if (s) {
        found++
        onFound(s)
      }
    }
  }
  return found
}
