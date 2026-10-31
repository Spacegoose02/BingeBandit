import type { MediaFile, MediaStream } from './types'

// The Windows app (apps/desktop) exposes this to the server's pages: videos
// play in mpv, which takes every file as it is.

export interface DesktopPlay {
  url: string // a /api path on this server
  title: string
  start: number // seconds
  audio: number // mpv audio track id; 0 = the file's default
  sub: number // mpv subtitle track id; 0 = none
  subUrl?: { url: string; title: string; lang: string } // a sidecar subtitle file instead
}

export type DesktopEvent =
  | { type: 'time'; position: number; duration: number; paused: boolean }
  | { type: 'end' }
  | { type: 'key'; name: 'bb-enter' | 'bb-next' }
  | { type: 'closed'; position: number }
  | { type: 'error'; message: string }

export interface DesktopBridge {
  player: 'mpv'
  play(req: DesktopPlay): Promise<void>
  command(name: 'seek', seconds: number): Promise<void>
  command(name: 'pause', paused: boolean): Promise<void>
  command(name: 'osd', text: string): Promise<void>
  command(name: 'stop'): Promise<void>
  release(): Promise<void>
  onEvent(cb: (e: DesktopEvent) => void): () => void
}

/** The Windows app's bridge, when this page runs inside it. */
export const desktop = (window as unknown as { bbDesktop?: DesktopBridge }).bbDesktop

/**
 * mpv numbers each kind of track from 1 in file order, counting only the
 * file's own tracks (sidecar files are added after). 0 when it's a sidecar.
 */
export function mpvTrackId(f: MediaFile, s: MediaStream): number {
  if (s.external) return 0
  const same = f.streams.filter((x) => x.type === s.type && !x.external).sort((a, b) => a.index - b.index)
  return same.findIndex((x) => x.index === s.index) + 1
}
