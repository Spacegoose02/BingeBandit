import type { BufferConfig } from 'react-native-video'
import { absolute, api, request } from './api'

/**
 * Joins a channel's shared stream. The server answers once the first
 * segments exist (up to ~25 s while the provider connects), so ExoPlayer
 * never waits on a slow playlist and times out.
 */
export async function joinChannel(channelId: number): Promise<{ viewerId: string; playlist: string }> {
  const r = await request<{ viewerId: string; playlist: string }>('POST', `/api/live/channels/${channelId}/watch`, {}, undefined, 40000)
  return { viewerId: r.viewerId, playlist: absolute(r.playlist) }
}

export function leaveChannel(viewerId: string) {
  if (viewerId) api.del(`/api/live/viewers/${viewerId}`).catch(() => undefined)
}

/** Keeps the viewer counted while paused (a paused player stops polling). */
export function pingViewer(viewerId: string): Promise<unknown> {
  return api.post(`/api/live/viewers/${viewerId}/ping`)
}

/**
 * Plays about 10 s behind live, so a provider hiccup or a slow segment eats
 * into that cushion instead of stalling the picture, and waits for 5 s of
 * video before resuming after a stall. Speed stays at 1× so the audio never
 * wobbles while ExoPlayer chases the live edge; Go live catches up instead.
 */
export const LIVE_BUFFER: BufferConfig = {
  bufferForPlaybackMs: 2500,
  bufferForPlaybackAfterRebufferMs: 5000,
  live: { targetOffsetMs: 10000, minPlaybackSpeed: 1, maxPlaybackSpeed: 1 },
}

/** How far a live player sits behind the live edge when "at live". */
export const LIVE_OFFSET_SECS = 10

export function fmtBehind(secs: number): string {
  const m = Math.floor(secs / 60)
  const s = Math.floor(secs % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}
