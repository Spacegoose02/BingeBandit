import AsyncStorage from '@react-native-async-storage/async-storage'
import NetInfo from '@react-native-community/netinfo'
import { current, onUnreachable, patchSession, request } from './api'
import { probe, scan } from './discovery'

/*
 * One server, more than one address. A TV signed in with the public URL
 * (https://tv.example.com) can't reach it when the internet is down, even
 * sitting next to it; a phone signed in at home with 192.168.1.10 can't reach
 * it on the road. The app learns the other addresses while things work and
 * switches when the one it uses stops answering. The server's id from
 * /api/health makes sure an address belongs to the same server.
 */

const LAN_KEY = 'bb.lanScanAt.' // + the server's id
// How often to look for the server on the home network when no LAN address is known yet.
const LAN_SCAN_EVERY = 24 * 60 * 60 * 1000
// After a round that found nothing, failures within this long don't start another.
const QUIET_AFTER_MISS = 15 * 1000

function hostOf(url: string): string {
  return url.replace(/^https?:\/\//i, '').replace(/[:/].*$/, '').toLowerCase()
}

/** True for an address on a home network: a private IP or a .local name. */
export function isLocal(url: string): boolean {
  const h = hostOf(url)
  if (h.endsWith('.local') || h === 'localhost') return true
  const m = h.match(/^(\d+)\.(\d+)\.\d+\.\d+$/)
  if (!m) return false
  const [a, b] = [Number(m[1]), Number(m[2])]
  return a === 10 || a === 127 || (a === 192 && b === 168) || (a === 172 && b >= 16 && b <= 31)
}

function known(): string[] {
  const s = current()
  return s ? [s.server, ...(s.alternates ?? [])] : []
}

/** Uses `url` from now on, keeping every other address as an alternate. */
async function switchTo(url: string) {
  const all = known()
  await patchSession({ server: url, alternates: all.filter((u) => u !== url) })
}

async function remember(url: string) {
  const s = current()
  if (!s || known().includes(url)) return
  await patchSession({ alternates: [...(s.alternates ?? []), url] })
}

async function answers(url: string, id: string | undefined, timeoutMs: number): Promise<boolean> {
  const info = await probe(url, timeoutMs)
  return !!info && (!id || !info.id || info.id === id)
}

/** The server on this device's home network, by its id. Only on Wi-Fi or Ethernet. */
async function findOnLan(id: string): Promise<string | null> {
  const net = await NetInfo.fetch()
  if (net.type !== 'wifi' && net.type !== 'ethernet') return null
  await AsyncStorage.setItem(LAN_KEY + id, String(Date.now())).catch(() => undefined)
  let found: string | null = null
  await scan((s) => {
    if (!found && s.id === id) found = s.url
  })
  return found
}

let round: Promise<string | null> | null = null
let missedAt = 0

/**
 * Finds another address for the signed-in server after `failed` didn't
 * answer: the ones it knows first, then (with the server's id) a look
 * around the home network. Requests failing together share one round.
 */
function reconnect(failed: string): Promise<string | null> {
  const s = current()
  if (!s) return Promise.resolve(null)
  // Another request already moved on to an address that works.
  if (s.server !== failed) return Promise.resolve(s.server)
  if (round) return round
  if (Date.now() - missedAt < QUIET_AFTER_MISS) return Promise.resolve(null)
  round = (async () => {
    const others = known().filter((u) => u !== failed)
    const ok = await Promise.all(others.map((u) => answers(u, s.serverId, 3000)))
    let next = others.find((_, i) => ok[i]) ?? null
    if (!next && s.serverId) next = await findOnLan(s.serverId)
    if (next) await switchTo(next)
    else missedAt = Date.now()
    return next
  })().finally(() => {
    round = null
  })
  return round
}

onUnreachable(reconnect)

/**
 * Learns the signed-in server's other addresses while it answers: its id,
 * its public URL (for away from home), and its LAN address (for when the
 * internet is down) when this device is at home and only knows a public one.
 * Runs in the background after the app opens.
 */
export async function learnAddresses() {
  const s = current()
  if (!s) return
  const info = await probe(s.server, 4000)
  if (!info) return
  if (info.id && info.id !== s.serverId) await patchSession({ serverId: info.id })
  const link = await request<{ serverUrl?: string }>('GET', '/api/link/info').catch(() => null)
  const pub = link?.serverUrl?.replace(/\/+$/, '')
  if (pub && /^https?:\/\//i.test(pub)) await remember(pub)

  const id = current()?.serverId
  if (!id || known().some(isLocal)) return
  const last = Number(await AsyncStorage.getItem(LAN_KEY + id).catch(() => null)) || 0
  if (Date.now() - last < LAN_SCAN_EVERY) return
  const lan = await findOnLan(id)
  if (lan) await remember(lan)
}
