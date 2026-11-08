// Offline downloads on phones and tablets: titles saved in the app's own
// storage to watch with no connection (on the road, on a plane). The server
// prepares the copy (the original file, or a one-off conversion at a chosen
// height: POST /api/files/{id}/offline); the native Downloads module saves
// it with resume; the player plays the local file and keeps the position
// here, which is sent to the server once it answers again.

import { useEffect, useState } from 'react'
import { NativeEventEmitter, NativeModules } from 'react-native'
import AsyncStorage from '@react-native-async-storage/async-storage'
import NetInfo from '@react-native-community/netinfo'
import { ApiError, absolute, api, current } from './api'
import { pickTracks, TEXT_SUBS, IMAGE_SUBS, type MediaStream, type Prefs } from './tracks'
import type { Item, ItemDetail, MediaFile } from './types'

interface Native {
  dir: string
  freeBytes(): Promise<number>
  fileSize(path: string): Promise<number>
  deleteFile(path: string): Promise<boolean>
  readText(path: string): Promise<string>
  writeText(path: string, text: string): Promise<boolean>
  listFiles(): Promise<string[]>
  start(id: string, url: string, token: string, path: string): Promise<boolean>
  pause(id: string): Promise<boolean>
  keepAlive(on: boolean, title: string): Promise<boolean>
}

const Native: Native | undefined = NativeModules.Downloads

/** Whether this build can download (the native module is there). */
export const downloadsSupported = !!Native

export type DownloadState = 'waiting' | 'preparing' | 'downloading' | 'paused' | 'done' | 'failed'

/** A subtitle saved beside the video, as WebVTT. */
export interface SavedSub {
  index: number
  codec: string
  language?: string
  title?: string
  forced?: boolean
  hearingImpaired?: boolean
  path: string
}

export interface Download {
  key: string // server, profile and title: each profile has its own downloads
  server: string
  userId: number
  itemId: number
  item: Item // as it was when saved, for the list and the player
  height: number // 0: the original file; else the converted height
  state: DownloadState
  /** Why it waits or failed, to show under the title. */
  note?: string
  prepare?: number // the server's conversion progress, 0..1
  done: number // bytes saved
  total: number // bytes expected (0 until known)
  fileId?: number
  file?: MediaFile // the saved copy: the original's streams, or the converted copy's
  path?: string
  poster?: string // local poster file
  subs: SavedSub[]
  addedAt: number
  // Watching offline: kept here and sent once the server answers.
  position: number
  duration: number
  watched: boolean
  dirty: boolean
  watchedAt: number // unix seconds of the last local change
  retries?: number // connection drops in a row, resumed by themselves
  auto?: boolean // queued by itself (the next episodes), not by hand
}

export interface DownloadPrefs {
  wifiOnly: boolean
  limitGB: number // 0: no limit
  /** Keep this many unwatched episodes ahead of each show you've downloaded (0: off). */
  autoNext: number
  /** Delete an episode's download once it's watched. */
  autoRemove: boolean
}

const KEY = 'bb.downloads'
const PREFS_KEY = 'bb.downloadPrefs'
const POLL_MS = 3000

let list: Download[] = []
let prefs: DownloadPrefs = { wifiOnly: true, limitGB: 20, autoNext: 0, autoRemove: false }
let loaded: Promise<void> | null = null
const listeners = new Set<() => void>()
let activeId = '' // the download the native module is working on
let pumping = false

function changed() {
  for (const l of listeners) l()
}

function persist() {
  AsyncStorage.setItem(KEY, JSON.stringify(list)).catch(() => undefined)
  changed()
}

function update(key: string, patch: Partial<Download>) {
  list = list.map((d) => (d.key === key ? { ...d, ...patch } : d))
  persist()
}

function find(key: string) {
  return list.find((d) => d.key === key)
}

/** Reads what's saved; called once at start. */
export function loadDownloads(): Promise<void> {
  if (!loaded)
    loaded = (async () => {
      try {
        list = JSON.parse((await AsyncStorage.getItem(KEY)) || '[]')
        prefs = { ...prefs, ...JSON.parse((await AsyncStorage.getItem(PREFS_KEY)) || '{}') }
      } catch {
        list = []
      }
      // The app was closed mid-download: it picks up where it was.
      list = list.map((d) => (d.state === 'downloading' || d.state === 'preparing' ? { ...d, state: 'waiting' } : d))
      changed()
    })()
  return loaded
}

const keyFor = (server: string, userId: number, itemId: number) => `${server}|${userId}|${itemId}`

// The server may be reached at another of its addresses later (home or
// away); downloads still belong to it.
function mine(d: Download): boolean {
  const s = current()
  if (!s) return false
  return d.userId === s.user.id && (d.server === s.server || !!s.alternates?.includes(d.server))
}

/** This profile's downloads, newest first. */
export function myDownloads(): Download[] {
  return list.filter(mine).sort((a, b) => b.addedAt - a.addedAt)
}

/** The finished download of a title, if this profile has one. */
export function downloaded(itemId: number): Download | undefined {
  return list.find((d) => mine(d) && d.itemId === itemId && d.state === 'done')
}

export function downloadFor(itemId: number): Download | undefined {
  return list.find((d) => mine(d) && d.itemId === itemId)
}

/** Redraws when the downloads change. */
export function useDownloads(): Download[] {
  const [, redraw] = useState(0)
  useEffect(() => {
    const l = () => redraw((n) => n + 1)
    listeners.add(l)
    loadDownloads().then(l)
    return () => {
      listeners.delete(l)
    }
  }, [])
  return myDownloads()
}

export function downloadPrefs(): DownloadPrefs {
  return prefs
}

export async function setDownloadPrefs(p: Partial<DownloadPrefs>) {
  prefs = { ...prefs, ...p }
  await AsyncStorage.setItem(PREFS_KEY, JSON.stringify(prefs)).catch(() => undefined)
  changed()
  if (p.wifiOnly !== undefined) netChanged()
  if (p.autoNext || p.autoRemove) syncDownloads().catch(() => undefined)
}

/** Bytes all downloads take (or will take) on this phone. */
export function usedBytes(): number {
  return list.reduce((n, d) => n + Math.max(d.done, d.total), 0)
}

/** Rough size of a copy: the file for the original, else the copy's bitrate. */
export function estimateBytes(file: { size?: number; duration: number }, height: number): number {
  if (height <= 0) return file.size ?? 0
  const kbps = (height >= 1080 ? 5000 : height >= 720 ? 2500 : 1000) + 160
  return (kbps * 1000 * file.duration) / 8
}

export function formatBytes(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`
  if (n >= 1e6) return `${Math.round(n / 1e6)} MB`
  return `${Math.max(0, Math.round(n / 1e3))} KB`
}

/** Queues titles (a movie, or a season's episodes) at a quality. */
export async function addDownloads(items: Item[], height: number, auto = false) {
  await loadDownloads()
  const s = current()
  if (!s || !Native) return
  const now = Date.now()
  for (const [i, it] of items.entries()) {
    const key = keyFor(s.server, s.user.id, it.id)
    const old = find(key)
    if (old && old.state !== 'failed') continue
    if (old) list = list.filter((d) => d.key !== key)
    list.push({
      key, server: s.server, userId: s.user.id, itemId: it.id, item: it, height,
      state: 'waiting', done: 0, total: 0, subs: [], addedAt: now - i, // a season keeps its order
      position: it.watch?.position ?? 0, duration: it.watch?.duration ?? 0, watched: !!it.watch?.watched, dirty: false, watchedAt: 0,
      auto: auto || undefined,
    })
  }
  persist()
  pump()
}

/** Deletes a download and its files. */
export async function removeDownload(key: string) {
  const d = find(key)
  if (!d) return
  if (activeId === key) {
    await Native?.pause(key)
    activeId = ''
  }
  for (const p of [d.path, d.poster, ...d.subs.map((x) => x.path)]) if (p) await Native?.deleteFile(p).catch(() => undefined)
  list = list.filter((x) => x.key !== key)
  persist()
  pump()
}

/** Removes every download of a profile (signing out of it on this phone). */
export async function removeDownloadsFor(server: string, userId: number) {
  await loadDownloads()
  for (const d of list.filter((x) => x.server === server && x.userId === userId)) await removeDownload(d.key)
}

export async function pauseDownload(key: string) {
  if (activeId === key) await Native?.pause(key)
  update(key, { state: 'paused', note: undefined })
}

export function resumeDownload(key: string) {
  update(key, { state: 'waiting', note: undefined })
  pump()
}

// ---- the queue ----

let onWifi = true
let online = true

function netChanged() {
  if (prefs.wifiOnly && !onWifi && activeId) {
    const k = activeId
    Native?.pause(k)
    update(k, { state: 'waiting', note: 'Waiting for Wi-Fi' })
  }
  pump()
}

let started = false
/** Starts following the network and the native module; called once the app is signed in. */
export function startDownloads() {
  if (started || !Native) return
  started = true
  const events = new NativeEventEmitter(NativeModules.Downloads)
  events.addListener('DownloadProgress', (e: { id: string; done: number; total: number }) => {
    const d = find(e.id)
    if (d) {
      list = list.map((x) => (x.key === e.id ? { ...x, done: e.done, total: e.total > 0 ? e.total : x.total } : x))
      changed() // saved on the next state change, not every megabyte
    }
  })
  events.addListener('DownloadEnd', (e: { id: string; ok: boolean; paused: boolean; error: string; size: number }) => {
    if (e.id !== activeId) return
    activeId = ''
    const d = find(e.id)
    if (d) {
      const retries = (d.retries ?? 0) + 1
      if (e.ok) update(e.id, { state: 'done', done: e.size, total: e.size, note: undefined, retries: 0 })
      // A dropped connection resumes by itself a few times; an answer from the server is final.
      else if (!e.paused && retries <= 3 && !/answered/.test(e.error)) update(e.id, { state: 'waiting', done: e.size, note: 'Connection lost, resuming', retries })
      else if (!e.paused) update(e.id, { state: 'failed', done: e.size, note: e.error || 'The download stopped.', retries: 0 })
      else update(e.id, { done: e.size })
    }
    pump()
  })
  NetInfo.addEventListener((s) => {
    const wifi = s.type === 'wifi' || s.type === 'ethernet'
    const up = s.isConnected !== false
    const back = (up && !online) || (wifi && !onWifi)
    onWifi = wifi
    online = up
    netChanged()
    if (back) syncDownloads().catch(() => undefined)
  })
  loadDownloads().then(() => {
    cleanOrphans()
    syncDownloads().catch(() => undefined)
    pump()
  })
}

// Files left by downloads that are gone (the app was closed mid-delete).
async function cleanOrphans() {
  if (!Native) return
  const known = new Set(list.flatMap((d) => [d.path, d.poster, ...d.subs.map((x) => x.path)]).filter(Boolean))
  for (const p of await Native.listFiles().catch(() => [] as string[])) if (!known.has(p)) Native.deleteFile(p).catch(() => undefined)
}

async function keepAlive(on: boolean, title = '') {
  await Native?.keepAlive(on, title).catch(() => undefined)
}

/** Works through the queue, one download at a time. */
async function pump() {
  if (pumping || activeId || !Native) return
  pumping = true
  try {
    for (;;) {
      const next = list.find((d) => mine(d) && d.state === 'waiting')
      if (!next) break
      if (prefs.wifiOnly && !onWifi) {
        if (next.note !== 'Waiting for Wi-Fi') update(next.key, { note: 'Waiting for Wi-Fi' })
        break
      }
      if (!online) break
      const r = await begin(next)
      if (r === 'started') return // the native module reports the end, then pumps again
      if (r === 'offline') break // NetInfo or the next visit tries again
    }
    await keepAlive(false)
  } finally {
    pumping = false
  }
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

function safeName(key: string) {
  return key.replace(/[^a-zA-Z0-9]+/g, '_')
}

/**
 * Prepares one download and starts saving it: started, skipped (failed,
 * paused or removed meanwhile; go on to the next) or offline (stop for now).
 */
async function begin(d: Download): Promise<'started' | 'skipped' | 'offline'> {
  const s = current()
  if (!s || !Native) return 'offline'
  const key = d.key
  const still = () => find(key)?.state === 'preparing'
  update(key, { state: 'preparing', note: undefined, prepare: 0 })
  try {
    const [detail, userPrefs] = await Promise.all([
      api.get<ItemDetail & { canDownload?: boolean }>(`/api/items/${d.itemId}`),
      api.get<Prefs>('/api/me/prefs').catch(() => null),
    ])
    const f = detail.files?.[0]
    if (!f) throw new Error('This title has no file to download.')
    if (detail.canDownload === false) throw new Error('Downloads are turned off on this server.')
    const picked = pickTracks(f.streams ?? [], userPrefs)
    const burn = picked.sub && IMAGE_SUBS.has(picked.sub.codec) ? picked.sub.index : -1
    await keepAlive(true, `Downloading ${titleOf(detail.item)}`)
    // The server converts first (for a copy): ask until it's ready.
    let res: { status: string; progress: number; url?: string; size?: number; height?: number; error?: string }
    for (;;) {
      res = await api.post(`/api/files/${f.id}/offline`, { height: d.height, audioStream: d.height ? picked.audio : -1, burnSubtitle: d.height ? burn : -1 })
      if (!still()) return 'skipped' // paused or removed meanwhile
      if (res.status === 'ready' || res.status === 'failed') break
      update(key, { prepare: res.progress, note: res.status === 'queued' ? 'Waiting for the server' : 'The server is preparing it' })
      await sleep(POLL_MS)
      if (!still()) return 'skipped'
    }
    if (res.status === 'failed' || !res.url) throw new Error(res.error || 'The server could not prepare this download.')
    const size = res.size ?? 0
    // Room on the phone, and within the limit set in Downloads.
    const others = list.filter((x) => x.key !== key).reduce((n, x) => n + Math.max(x.done, x.total), 0)
    if (prefs.limitGB > 0 && others + size > prefs.limitGB * 1e9) throw new Error(`Over your ${prefs.limitGB} GB download limit. Remove a download or raise the limit.`)
    const free = await Native.freeBytes()
    if (size - d.done > free - 200e6) throw new Error(`Not enough room on this phone (${formatBytes(size)} needed).`)

    const base = `${Native.dir}/${safeName(key)}`
    // Text subtitles as WebVTT, and the poster, before the video.
    const subs: SavedSub[] = []
    for (const st of (f.streams ?? []).filter((x: MediaStream) => x.type === 'subtitle' && TEXT_SUBS.has(x.codec)).slice(0, 8)) {
      const text = await fetch(absolute(`/api/files/${f.id}/subtitles/${st.index}.vtt`), { headers: { Authorization: `Bearer ${s.token}` } })
        .then((r) => (r.ok ? r.text() : ''))
        .catch(() => '')
      if (!text) continue
      const path = `${base}.sub${st.index}.vtt`
      if (await Native.writeText(path, text).catch(() => false)) {
        subs.push({ index: st.index, codec: 'webvtt', language: st.language, title: st.title, forced: st.forced, hearingImpaired: st.hearingImpaired, path })
      }
    }
    const art = detail.item.poster || detail.item.showPoster
    let poster: string | undefined
    if (art) {
      const url = art.startsWith('/') ? absolute(art) + (art.includes('/api/images/') ? '?w=w342' : '') : art
      poster = (await saveSmall(`${key}#poster`, url, s.token, `${base}.poster.jpg`)) ? `${base}.poster.jpg` : undefined
    }
    // The converted copy has one audio track and no subtitle streams of its own.
    const file: MediaFile = d.height
      ? { ...f, container: 'mp4', videoCodec: 'h264', height: res.height || d.height, width: 0, audioCodec: 'aac', audioChannels: 2, streams: [] }
      : f
    if (!still()) return 'skipped'
    // ExoPlayer goes by the extension; a copy is always MP4.
    const path = `${base}.${d.height ? 'mp4' : (f.container || 'mkv').replace(/[^a-z0-9]/gi, '') || 'mkv'}`
    // What arrived before is kept only if it's the same copy (a converted
    // copy made again after the server cleared it isn't byte for byte).
    const resumable = d.path === path && d.fileId === f.id && d.total === size && d.done > 0
    update(key, {
      state: 'downloading', note: undefined, item: detail.item, fileId: f.id, file, path, poster, subs, total: size,
      // A fresh start (another quality, or a failed try) drops what came before.
      done: resumable ? d.done : 0,
    })
    if (!resumable) await Native.deleteFile(path).catch(() => undefined)
    activeId = key
    const url = /^https?:/.test(res.url) ? res.url : absolute(res.url)
    await Native.start(key, url, s.token, path)
    return 'started'
  } catch (e) {
    activeId = ''
    if (!find(key)) return 'skipped'
    // Offline for now: try again later instead of failing.
    if (e instanceof ApiError && e.status === 0) {
      update(key, { state: 'waiting', note: 'Waiting for the server' })
      return 'offline'
    }
    update(key, { state: 'failed', note: (e as Error).message })
    return 'skipped'
  }
}

// A small file (the poster) saved through the native module; resolves when it ends.
function saveSmall(id: string, url: string, token: string, path: string): Promise<boolean> {
  return new Promise((resolve) => {
    if (!Native) return resolve(false)
    const events = new NativeEventEmitter(NativeModules.Downloads)
    const sub = events.addListener('DownloadEnd', (e: { id: string; ok: boolean }) => {
      if (e.id !== id) return
      sub.remove()
      resolve(e.ok)
    })
    // The session token goes only to the server, never to TMDB.
    Native.start(id, url, url.startsWith(absolute('')) ? token : '', path).catch(() => {
      sub.remove()
      resolve(false)
    })
  })
}

export function titleOf(it: Item): string {
  return it.kind === 'episode' ? `${it.showTitle ?? ''} · S${it.seasonNumber}E${it.episodeNumber}` : it.title
}

// ---- watching offline ----

/** The player's position for a download, kept until the server has it. */
export function saveOfflineProgress(itemId: number, position: number, duration: number, finished: boolean) {
  const d = downloaded(itemId)
  if (!d) return
  const watched = finished || (duration > 0 && position / duration >= 0.9)
  update(d.key, { position: watched ? 0 : position, duration, watched: d.watched || watched, dirty: true, watchedAt: Math.floor(Date.now() / 1000) })
  // Finishing an episode lines up the next one, once the server answers.
  if (watched && !d.watched) syncDownloads().catch(() => undefined)
}

/** The server took the position: nothing left to send. */
export function offlineProgressSent(itemId: number) {
  const d = downloaded(itemId)
  if (d?.dirty) update(d.key, { dirty: false })
}

/** Text of a saved subtitle file. */
export function readSaved(path: string): Promise<string> {
  return Native?.readText(path) ?? Promise.resolve('')
}

/** The next downloaded episode of the same show, for the player's Next. */
export function nextDownloaded(it: Item): Download | undefined {
  if (it.kind !== 'episode' || !it.showId) return undefined
  const order = (x: Item) => (x.seasonNumber ?? 0) * 10000 + (x.episodeNumber ?? 0)
  return myDownloads()
    .filter((d) => d.state === 'done' && d.item.showId === it.showId && order(d.item) > order(it))
    .sort((a, b) => order(a.item) - order(b.item))[0]
}

let syncing = false
/**
 * Once the server answers: sends positions watched offline (unless the
 * server has something newer from another device), and deletes downloads
 * this profile may no longer keep (downloads turned off, the library taken
 * away, a kid approval removed).
 */
export async function syncDownloads() {
  await loadDownloads()
  const mineNow = list.filter(mine)
  if (syncing || !mineNow.length) return
  syncing = true
  try {
    const check = await api.post<{ canDownload: boolean; allowed: number[]; watched?: number[] }>('/api/offline/check', { items: mineNow.map((d) => d.itemId) })
    const allowed = new Set(check.allowed)
    for (const d of mineNow) if (!allowed.has(d.itemId)) await removeDownload(d.key)
    // Watched on another device counts too.
    for (const id of check.watched ?? []) {
      const d = list.find((x) => mine(x) && x.itemId === id)
      if (d && !d.watched && !d.dirty) update(d.key, { watched: true, position: 0 })
    }
    for (const d of list.filter((x) => mine(x) && x.dirty)) {
      const detail = await api.get<ItemDetail>(`/api/items/${d.itemId}`).catch(() => null)
      if (detail && (detail.item.watch?.updatedAt ?? 0) <= d.watchedAt) {
        await api.post('/api/progress', {
          itemId: d.itemId,
          fileId: d.fileId,
          position: d.watched && !d.position ? d.duration : d.position,
          duration: d.duration,
        })
      }
      update(d.key, { dirty: false })
    }
    if (check.canDownload) await autoDownloads()
  } finally {
    syncing = false
  }
}

const episodeOrder = (x: Item) => (x.seasonNumber ?? 0) * 10000 + (x.episodeNumber ?? 0)

/**
 * Automatic downloads (Downloads → Settings): for each show with an
 * episode downloaded, deletes watched episodes (when chosen) and keeps the
 * next few unwatched ones queued, at the quality of the last one.
 */
async function autoDownloads() {
  if (!prefs.autoNext && !prefs.autoRemove) return
  const shows = new Map<number, Download[]>()
  for (const d of list.filter((x) => mine(x) && x.item.kind === 'episode' && x.item.showId)) {
    shows.set(d.item.showId!, [...(shows.get(d.item.showId!) ?? []), d])
  }
  for (const eps of shows.values()) {
    eps.sort((a, b) => episodeOrder(a.item) - episodeOrder(b.item))
    const last = eps[eps.length - 1]
    if (prefs.autoRemove) {
      // Never the last one: it's where the next episodes are found from.
      for (const d of eps) if (d !== last && d.watched && !d.dirty && d.state === 'done') await removeDownload(d.key)
    }
    if (!prefs.autoNext) continue
    // Only shows being watched: the newest download is watched or under way.
    const ahead = eps.filter((d) => !d.watched && d.state !== 'failed').length
    const watching = eps.some((d) => d.watched || d.position > 60)
    if (!watching || ahead >= prefs.autoNext) continue
    const add: Item[] = []
    let from = last.itemId
    for (let n = ahead; n < prefs.autoNext; n++) {
      const next = await api.get<Item | null>(`/api/items/${from}/next`).catch(() => null)
      if (!next || !next.id) break
      if (!next.watch?.watched) add.push(next)
      from = next.id
    }
    if (add.length) await addDownloads(add, last.height, true)
    if (prefs.autoRemove && last.watched && !last.dirty && last.state === 'done' && add.length) await removeDownload(last.key)
  }
}
