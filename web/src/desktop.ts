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
  live?: boolean // a Live TV stream: starts at the live edge
}

export type DesktopEvent =
  | { type: 'time'; position: number; duration: number; paused: boolean }
  | { type: 'end' }
  // bb-enter: OK; bb-next: N; bb-pick: a page menu entry (arg is its id); bb-channel: arg is up or down
  | { type: 'key'; name: 'bb-enter' | 'bb-next' | 'bb-pick' | 'bb-channel'; arg?: string }
  | { type: 'buffering'; on: boolean }
  | { type: 'closed'; position: number }
  | { type: 'error'; message: string }

/** What BingeBandit's controls over mpv show (apps/desktop/mpv-scripts/bingebandit.lua). */
export interface DesktopUiState {
  mode: 'vod' | 'live'
  title: string
  subtitle?: string
  markers?: { start: number; end: number }[] // intro and credits, marked on the bar
  prompt?: string // e.g. "Skip intro": OK does it
  upNext?: string // title of what plays next: OK plays it
  badge?: string // e.g. "Watching together · 3"
}

/** The page's own sections in the controls' menu; audio and subtitles come after. */
export interface DesktopMenuSection {
  title: string
  items: { id?: string; label: string; active?: boolean; disabled?: boolean }[]
}

export interface DesktopUpdate {
  current: string
  status: 'off' | 'idle' | 'checking' | 'downloading' | 'ready' | 'available' | 'error'
  version?: string
  percent?: number
  portable: boolean
  message?: string
}

export interface DesktopBridge {
  player: 'mpv'
  // What this copy of the app can do beyond playing files: 'live' (Live TV in
  // mpv), 'together' (Watch Together in mpv), 'ui' (BingeBandit's controls).
  features?: string[]
  play(req: DesktopPlay): Promise<void>
  command(name: 'seek', seconds: number): Promise<void>
  command(name: 'pause', paused: boolean): Promise<void>
  command(name: 'osd', text: string): Promise<void>
  command(name: 'state', state: DesktopUiState): Promise<void>
  command(name: 'menu', sections: DesktopMenuSection[]): Promise<void>
  command(name: 'copy', text: string): Promise<void>
  command(name: 'stop'): Promise<void>
  release(): Promise<void>
  onEvent(cb: (e: DesktopEvent) => void): () => void
  // Older Windows apps don't have these.
  updates?: {
    version: string
    state(): Promise<DesktopUpdate | null>
    check(): Promise<DesktopUpdate | null>
    install(): Promise<void>
    onChange(cb: (s: DesktopUpdate) => void): () => void
  }
  // Starting with Windows: true or false, or null when this copy can't. Older Windows apps don't have it.
  startup?: {
    get(): Promise<boolean | null>
    set(on: boolean): Promise<boolean | null>
  }
}

/** The Windows app's bridge, when this page runs inside it. */
export const desktop = (window as unknown as { bbDesktop?: DesktopBridge }).bbDesktop

/** Whether the Windows app this page runs in can do `feature` (older copies can't). */
export const desktopCan = (feature: 'live' | 'together' | 'ui') => !!desktop?.features?.includes(feature)

/**
 * mpv numbers each kind of track from 1 in file order, counting only the
 * file's own tracks (sidecar files are added after). 0 when it's a sidecar.
 */
export function mpvTrackId(f: MediaFile, s: MediaStream): number {
  if (s.external) return 0
  const same = f.streams.filter((x) => x.type === s.type && !x.external).sort((a, b) => a.index - b.index)
  return same.findIndex((x) => x.index === s.index) + 1
}
