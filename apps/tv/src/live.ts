import type { BufferConfig } from 'react-native-video'
import { absolute, api, request } from './api'
import type { GuideProgram } from './types'

/**
 * Joins a channel's shared stream. The server answers once the first
 * segments exist (up to ~25 s while the provider connects), so ExoPlayer
 * never waits on a slow playlist and times out.
 */
export async function joinChannel(channelId: number): Promise<{ viewerId: string; playlist: string; bufferSecs: number; stallSecs: number }> {
  const r = await request<{ viewerId: string; playlist: string; bufferSecs?: number; stallSecs?: number }>(
    'POST', `/api/live/channels/${channelId}/watch`, {}, undefined, 40000)
  return { viewerId: r.viewerId, playlist: absolute(r.playlist), bufferSecs: r.bufferSecs ?? DEFAULT_BUFFER_SECS, stallSecs: r.stallSecs ?? 12 }
}

/**
 * Tells the server this viewer has been buffering for `seconds`; past the
 * stall time it may move the channel to its next stream.
 */
/** Tells the server the player has been buffering; resolves to whether it moved the channel to another stream. */
export async function reportBuffering(viewerId: string, seconds: number): Promise<boolean> {
  if (!viewerId) return false
  const r = await api
    .post<{ switched?: boolean }>(`/api/live/viewers/${viewerId}/buffering`, { seconds: Math.round(seconds) })
    .catch(() => undefined)
  return !!r?.switched
}

export function leaveChannel(viewerId: string) {
  if (viewerId) api.del(`/api/live/viewers/${viewerId}`).catch(() => undefined)
}

/** Keeps the viewer counted while paused (a paused player stops polling). */
export function pingViewer(viewerId: string): Promise<unknown> {
  return api.post(`/api/live/viewers/${viewerId}/ping`)
}

/** How far behind live players sit unless the server says otherwise. */
export const DEFAULT_BUFFER_SECS = 10

/**
 * Plays `secs` behind live (the admin's buffer setting), so a provider hiccup
 * or a slow segment eats into that cushion instead of stalling the picture,
 * and waits for up to 5 s of video before resuming after a stall. Speed stays
 * at 1× so the audio never wobbles while ExoPlayer chases the live edge; Go
 * live catches up instead.
 */
export function liveBuffer(secs: number): BufferConfig {
  return {
    bufferForPlaybackMs: 2500,
    bufferForPlaybackAfterRebufferMs: Math.min(5000, secs * 1000),
    live: { targetOffsetMs: secs * 1000, minPlaybackSpeed: 1, maxPlaybackSpeed: 1 },
  }
}

export function fmtBehind(secs: number): string {
  const m = Math.floor(secs / 60)
  const s = Math.floor(secs % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

export function clock(t: number): string {
  const d = new Date(t * 1000)
  const h = d.getHours()
  return `${h % 12 || 12}:${String(d.getMinutes()).padStart(2, '0')} ${h < 12 ? 'AM' : 'PM'}`
}

/**
 * A guide episode number as people read it. Guides give either an onscreen
 * label ("S1E2", shown as is) or xmltv_ns ("0.1.0/1": zero-based season,
 * episode and part), shown as "S1 E2".
 */
export function episodeLabel(ep: string): string {
  const m = /^\s*(\d*)(?:\/\d+)?\s*\.\s*(\d*)(?:\/\d+)?\s*\.[^.]*$/.exec(ep)
  if (!m) return ep.trim()
  const season = m[1] ? `S${Number(m[1]) + 1}` : ''
  const episode = m[2] ? `E${Number(m[2]) + 1}` : ''
  return [season, episode].filter(Boolean).join(' ')
}

/** A programme's facts in one line: time, age rating, genres, episode and stars. */
export function programFacts(p: GuideProgram): string {
  const parts = [`${clock(p.start)} – ${clock(p.stop)}`]
  if (p.rating) parts.push(p.rating)
  if (p.genres || p.category) parts.push(p.genres || p.category!)
  if (p.episode) {
    const ep = episodeLabel(p.episode)
    if (ep) parts.push(ep)
  }
  if (p.stars) parts.push(`★ ${p.stars}`)
  return parts.join('   ·   ')
}
