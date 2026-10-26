import NetInfo from '@react-native-community/netinfo'
import { normalizeServer } from './api'
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
 * The addresses to try for what someone typed. A bare name like
 * tv.example.com is usually a server reached from outside through a reverse
 * proxy (https, no port), while 192.168.1.10 is the server itself on :8484,
 * so a name tries https first and an IP tries :8484 first. Anything after
 * the host is dropped, so pasting a /link address works too.
 */
export function addressCandidates(input: string): string[] {
  const s = input.trim().replace(/^(https?:\/\/)?([^/?#]*).*$/i, (_, scheme = '', host) => scheme + host)
  if (!s) return []
  if (/^https?:\/\//i.test(s)) return [normalizeServer(s)]
  if (/:\d+$/.test(s)) return [`http://${s}`, `https://${s}`]
  if (/^\d+\.\d+\.\d+\.\d+$/.test(s)) return [`http://${s}:${PORT}`, `http://${s}`]
  return [`https://${s}`, `http://${s}:${PORT}`, `http://${s}`]
}

/** Tries each address for what someone typed; the first server to answer wins. */
export async function probeTyped(input: string): Promise<ServerInfo | null> {
  for (const url of addressCandidates(input)) {
    const s = await probe(url, 4000)
    if (s) return s
  }
  return null
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
