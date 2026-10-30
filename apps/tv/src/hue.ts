import AsyncStorage from '@react-native-async-storage/async-storage'
import { api, current } from './api'
import { deviceId } from './devicePrefs'

// A Hue hub this device pairs with itself, for the profile signed in. The
// server can only reach a hub on its own network, so a TV in another home
// talks to its hub directly: it dims the room for Theater, sends the
// picture's colours (read by the server) for Sync, and plays Game Lights
// cues the server queues for the profile.

export interface Hub {
  bridge: string // the hub's address on this network
  username: string // what the hub gave this app when its button was pressed
  area: string // the room or zone (group id) these lights are in; '' for every light
}

export interface HubArea {
  id: string
  name: string
  type: string
  lights: string[]
}

interface HubLight {
  id: string
  colour: boolean
  state: { on: boolean; bri?: number; xy?: [number, number]; ct?: number; colormode?: string }
}

const key = () => {
  const s = current()
  return s ? `bb.hub.${s.server}.${s.user.id}` : ''
}

let hub: Hub | null = null
let loadedFor = ''

/** This profile's hub on this device, or null when it hasn't paired one. */
export async function loadHub(): Promise<Hub | null> {
  const k = key()
  if (k === loadedFor) return hub
  try {
    const v = await AsyncStorage.getItem(k)
    hub = v ? (JSON.parse(v) as Hub) : null
  } catch {
    hub = null
  }
  loadedFor = k
  return hub
}

export async function saveHub(h: Hub | null) {
  const k = key()
  hub = h
  loadedFor = k
  if (h) await AsyncStorage.setItem(k, JSON.stringify(h))
  else await AsyncStorage.removeItem(k)
}

const base = (addr: string) => `${/^https?:\/\//.test(addr) ? addr : `http://${addr}`}`.replace(/\/+$/, '') + '/api'

async function call<T>(addr: string, path: string, method = 'GET', body?: unknown, ms = 4000): Promise<T> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), ms)
  try {
    const res = await fetch(base(addr) + path, {
      method,
      body: body === undefined ? undefined : JSON.stringify(body),
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      signal: ctl.signal,
    })
    if (!res.ok) throw new Error(`The hub said ${res.status}`)
    const out = await res.json()
    const err = Array.isArray(out) ? out.find((e: { error?: { type: number; description: string } }) => e?.error)?.error : undefined
    if (err?.type === 101) throw new LinkButton()
    if (err?.type === 1) throw new Error("The hub doesn't know this TV any more; pair it again.")
    if (err) throw new Error(`The hub said: ${err.description}`)
    return out as T
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw new Error("Couldn't reach the hub at " + addr)
    throw e
  } finally {
    clearTimeout(t)
  }
}

/** The hub wants its round link button pressed first. */
export class LinkButton extends Error {
  constructor() {
    super('Press the round button on top of the Hue hub, then try again within 30 seconds.')
  }
}

/** Hue hubs on this network, from Philips' discovery service. */
export async function discoverHubs(): Promise<string[]> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), 6000)
  try {
    const res = await fetch('https://discovery.meethue.com/', { signal: ctl.signal })
    const list = (await res.json()) as { internalipaddress?: string }[]
    return list.map((h) => h.internalipaddress ?? '').filter(Boolean)
  } catch {
    return []
  } finally {
    clearTimeout(t)
  }
}

/** Asks the hub for a username; throws LinkButton until its button is pressed. */
export async function pairHub(addr: string): Promise<Hub> {
  const out = await call<{ success?: { username: string } }[]>(addr, '', 'POST', { devicetype: 'bingebandit#tv' })
  const username = out.find((r) => r.success)?.success?.username
  if (!username) throw new Error("The hub didn't answer as expected.")
  return { bridge: addr, username, area: '' }
}

/** The hub's rooms and zones that have lights. */
export async function hubAreas(h: Hub): Promise<HubArea[]> {
  const groups = await call<Record<string, { name: string; type: string; lights: string[] }>>(h.bridge, `/${h.username}/groups`)
  return Object.entries(groups)
    .filter(([, g]) => (g.type === 'Room' || g.type === 'Zone') && g.lights.length > 0)
    .map(([id, g]) => ({ id, name: g.name, type: g.type, lights: g.lights }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** The lights in the hub's chosen room, with how they are now. */
async function roomLights(h: Hub): Promise<HubLight[]> {
  const all = await call<Record<string, { type: string; state: HubLight['state'] }>>(h.bridge, `/${h.username}/lights`)
  let ids = Object.keys(all)
  if (h.area) {
    const area = (await hubAreas(h)).find((a) => a.id === h.area)
    if (area) ids = area.lights
  }
  return ids.filter((id) => all[id]).map((id) => ({ id, colour: /colou?r/i.test(all[id].type), state: all[id].state }))
}

function setLight(h: Hub, id: string, state: Record<string, unknown>) {
  return call(h.bridge, `/${h.username}/lights/${id}/state`, 'PUT', state, 1500).catch(() => undefined)
}

/** Puts lights back the way they were. */
async function restore(h: Hub, lights: HubLight[]) {
  for (const l of lights) {
    const s = l.state
    const back: Record<string, unknown> = { on: s.on, transitiontime: 4 }
    if (s.on) {
      back.bri = s.bri
      if (s.colormode === 'xy' && s.xy) back.xy = s.xy
      else if (s.ct) back.ct = s.ct
    }
    await setLight(h, l.id, back)
  }
}

// Picture brightness to the hub's 1–254: dark scenes dim the lights, never quite off (as the server does).
const hueBri = (lum: number) => Math.max(1, Math.min(254, Math.round((0.08 + 0.92 * Math.sqrt(Math.max(0, Math.min(1, lum)))) * 254)))

type Frames = { from: number; fps: number; frames: Record<string, [number, number, number]>[] }

/**
 * Hue Sync on this device's own hub, for one video. The player tells it the
 * position, pauses and the end; it dims the room (Theater) or follows the
 * picture (Sync), and puts the lights back after.
 */
export class LocalSync {
  private lights: HubLight[] = []
  private ready: Promise<void>
  private timer: ReturnType<typeof setInterval> | null = null
  private chunks: Frames[] = []
  private fetching = false
  private paused = false
  private stopped = false
  private last = new Map<string, string>()

  constructor(
    private h: Hub,
    private fileId: number,
    private mode: 'sync' | 'theater',
    private position: () => number,
  ) {
    this.ready = roomLights(h).then(
      (l) => {
        this.lights = l
      },
      () => undefined,
    )
    this.ready.then(() => !this.stopped && this.play())
  }

  private play() {
    if (this.mode === 'theater') {
      for (const l of this.lights) setLight(this.h, l.id, { on: true, bri: 25, ct: 447, transitiontime: 20 })
      return
    }
    if (this.timer) return
    // The hub takes about ten light changes a second, shared by every light.
    const every = Math.max(250, this.lights.length * 100)
    this.timer = setInterval(() => this.tick(every), every)
  }

  private tick(every: number) {
    const pos = this.position()
    const chunk = this.chunks.find((c) => pos >= c.from && pos < c.from + c.frames.length / c.fps)
    const end = this.chunks.reduce((m, c) => Math.max(m, c.from + c.frames.length / c.fps), 0)
    if (!this.fetching && (!chunk || end - pos < 10)) this.fetch(chunk ? end : pos)
    if (!chunk) return
    const z = chunk.frames[Math.floor((pos - chunk.from) * chunk.fps)]?.all
    if (!z) return
    const transitiontime = Math.round(every / 100)
    for (const l of this.lights) {
      const state = l.colour ? { on: true, xy: [z[0], z[1]], bri: hueBri(z[2]), transitiontime } : { on: true, bri: hueBri(z[2]), transitiontime }
      const sig = JSON.stringify(state)
      if (this.last.get(l.id) === sig) continue
      this.last.set(l.id, sig)
      setLight(this.h, l.id, state)
    }
  }

  private fetch(from: number) {
    this.fetching = true
    api
      .get<Frames>(`/api/plugins/hue-sync/me/colours?fileId=${this.fileId}&from=${from.toFixed(2)}&seconds=30`)
      .then(
        (c) => {
          // Keep what's near the playhead; a seek far away starts over.
          const pos = this.position()
          this.chunks = [...this.chunks.filter((x) => x.from + x.frames.length / x.fps > pos - 5 && x.from < pos + 60), c]
        },
        () => undefined,
      )
      .finally(() => {
        this.fetching = false
      })
  }

  /** Theater: the lights come back up while paused. Sync holds the last colour. */
  setPaused(p: boolean) {
    if (p === this.paused || this.stopped) return
    this.paused = p
    if (this.mode !== 'theater') return
    this.ready.then(() => {
      if (this.stopped) return
      if (p) restore(this.h, this.lights)
      else this.play()
    })
  }

  stop() {
    if (this.stopped) return
    this.stopped = true
    if (this.timer) clearInterval(this.timer)
    this.ready.then(() => restore(this.h, this.lights))
  }
}

interface Cue {
  id: number
  inMs: number
  label: string
  steps: { x: number; y: number; bri: number; ms: number }[]
}

/**
 * Plays the Game Lights effects the server queues for this profile on this
 * device's hub, after the delay it gives, one at a time. Returns a stop.
 */
export function followGameLights(h: Hub, device: string): () => void {
  let after: number | null = null
  let stopped = false
  let queue = Promise.resolve()
  const play = (c: Cue) => {
    queue = queue.then(async () => {
      if (stopped) return
      h = (await loadHub()) ?? h // the room may have changed in Settings
      const lights = await roomLights(h).catch(() => [] as HubLight[])
      for (const s of c.steps) {
        const began = Date.now()
        for (const l of lights) setLight(h, l.id, l.colour ? { on: true, xy: [s.x, s.y], bri: s.bri, transitiontime: 1 } : { on: true, bri: s.bri, transitiontime: 1 })
        await new Promise<void>((r) => setTimeout(r, Math.max(s.ms, lights.length * 100) - (Date.now() - began)))
      }
      await restore(h, lights)
    })
  }
  const poll = () =>
    api.get<{ cues: Cue[]; last: number }>(`/api/plugins/game-lights/me/cues?after=${after ?? ''}&device=${encodeURIComponent(device)}`).then(
      (r) => {
        if (stopped) return
        if (after !== null) for (const c of r.cues) setTimeout(() => play(c), Math.max(0, c.inMs))
        after = r.last
      },
      () => undefined,
    )
  poll()
  const t = setInterval(poll, 3000)
  return () => {
    stopped = true
    clearInterval(t)
  }
}

let following: { key: string; stop: () => void } | null = null

/**
 * Starts or stops playing this profile's Game Lights on this device's hub:
 * call it when the app starts and after Settings changes either.
 */
export async function refreshGameLights() {
  const h = await loadHub()
  const id = await deviceId()
  const person = await api.get<{ person?: { enabled?: boolean; devices?: Record<string, { on?: boolean; local?: boolean }> } }>('/api/plugins/game-lights/me/state').then(
    (s) => s.person,
    () => undefined,
  )
  const here = person?.devices?.[id]
  const want = h && person?.enabled && here?.on && here.local ? `${key()}|${h.bridge}|${h.username}` : ''
  if ((following?.key ?? '') === want) return
  following?.stop()
  following = h && want ? { key: want, stop: followGameLights(h, id) } : null
}
