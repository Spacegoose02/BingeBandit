// @ts-check
const os = require('node:os')

// Finding the server, the same way the TV app does (apps/tv/src/discovery.ts).

const PORT = 8484

/**
 * Asks one address whether it's a BingeBandit server.
 * @returns {Promise<{ url: string, name: string, version: string, id: string } | null>}
 */
async function probe(url, timeoutMs = 1500) {
  try {
    const res = await fetch(url + '/api/health', { signal: AbortSignal.timeout(timeoutMs) })
    const data = await res.json()
    if (data?.product !== 'BingeBandit' && data?.status !== 'ok') return null
    return { url, name: data.name || 'BingeBandit', version: data.version || '', id: data.id || url }
  } catch {
    return null
  }
}

/**
 * The addresses to try for what someone typed. A bare name is usually a
 * server behind a reverse proxy (https, no port); an IP is the server itself
 * on :8484. Anything after the host is dropped.
 */
function addressCandidates(input) {
  const s = String(input)
    .trim()
    .replace(/^(https?:\/\/)?([^/?#]*).*$/i, (_, scheme = '', host) => scheme + host)
    .replace(/\/+$/, '')
  if (!s) return []
  if (/^https?:\/\//i.test(s)) return [s]
  if (/:\d+$/.test(s)) return [`http://${s}`, `https://${s}`]
  if (/^\d+\.\d+\.\d+\.\d+$/.test(s)) return [`http://${s}:${PORT}`, `http://${s}`]
  return [`https://${s}`, `http://${s}:${PORT}`, `http://${s}`]
}

/** Tries each address for what someone typed; the first server to answer wins. */
async function probeTyped(input) {
  for (const url of addressCandidates(input)) {
    const s = await probe(url, 4000)
    if (s) return s
  }
  return null
}

/** This PC's home-network /24 prefixes, like 192.168.1. */
function lanPrefixes() {
  const out = new Set()
  for (const list of Object.values(os.networkInterfaces())) {
    for (const a of list ?? []) {
      if (a.family === 'IPv4' && !a.internal && /^(10|172|192)\./.test(a.address)) {
        out.add(a.address.split('.').slice(0, 3).join('.'))
      }
    }
  }
  return [...out]
}

/** Servers on this PC and the home network (every address in each /24, on :8484). */
async function scan() {
  const hosts = [`http://localhost:${PORT}`]
  for (const p of lanPrefixes()) for (let i = 1; i <= 254; i++) hosts.push(`http://${p}.${i}:${PORT}`)
  // The same server can answer on localhost and its LAN address; keep the first.
  const found = new Map()
  for (let i = 0; i < hosts.length; i += 64) {
    const batch = await Promise.all(hosts.slice(i, i + 64).map((h) => probe(h, 900)))
    for (const s of batch) if (s && !found.has(s.id)) found.set(s.id, s)
  }
  return [...found.values()]
}

module.exports = { probe, probeTyped, addressCandidates, scan }
