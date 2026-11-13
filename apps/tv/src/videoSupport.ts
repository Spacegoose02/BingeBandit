import { NativeModules } from 'react-native'
import { isPhone } from './theme'

/** What this box decodes and what its screen shows (VideoSupportModule). */
export interface VideoCaps {
  codecs: Record<string, { maxHeight: number; tenBit: boolean }>
  dolbyVision: boolean
  hdr: string[] // hdr10, hlg, dv, hdr10plus
}

const VideoSupport = NativeModules.VideoSupport as
  | {
      capabilities(): Promise<VideoCaps>
      matchFrameRate(fps: number): Promise<number>
      resetFrameRate(): void
    }
  | undefined

let caps: VideoCaps | undefined

/**
 * Asks the box again (the TV on the other end of the HDMI cable, and so the
 * HDR formats, can change). Undefined on an app build without the module or
 * if asking fails: then every format counts as playable, as before.
 */
export async function refreshVideoSupport(): Promise<VideoCaps | undefined> {
  try {
    caps = VideoSupport ? await VideoSupport.capabilities() : undefined
  } catch {
    caps = undefined
  }
  return caps
}

export interface VideoFacts {
  codec?: string
  height: number
  bitDepth?: number
  hdr: boolean
  range?: string // sdr, hdr10, hlg, dv
}

/**
 * Whether this box can decode the video and show it right: a decoder for
 * the codec at that size (and 10-bit when it's 10-bit), and for HDR a
 * screen that takes it. Without the screen's support Android shows HDR
 * washed out, so the server converts it (tone mapped) instead.
 */
export function videoPlayable(v: VideoFacts): boolean {
  if (!caps || !v.codec) return true
  const c = caps.codecs[v.codec]
  if (!c) return false
  if (v.height > c.maxHeight * 1.05) return false
  if ((v.bitDepth ?? 8) > 8 && !c.tenBit) return false
  return !v.hdr || hdrShown(v.range)
}

/** Whether the screen shows this HDR format (Dolby Vision files carry HDR10 for screens without it). */
export function hdrShown(range?: string): boolean {
  if (!caps) return true
  if (range === 'hlg') return caps.hdr.includes('hlg') || caps.hdr.includes('hdr10')
  return caps.hdr.includes('hdr10') || caps.hdr.includes('dv')
}

// Switching back waits a moment, so the next episode (a new player)
// doesn't send the TV through two mode changes.
let resetTimer: ReturnType<typeof setTimeout> | undefined

/** Switches the screen to a refresh rate that fits the video. Resolves once it's asked. */
export async function matchFrameRate(fps: number | undefined): Promise<void> {
  if (resetTimer) clearTimeout(resetTimer)
  resetTimer = undefined
  if (!VideoSupport || isPhone || !fps) return
  try {
    if ((await VideoSupport.matchFrameRate(fps)) > 0) {
      // The TV goes blank while it changes mode; starting now would lose the first seconds.
      await new Promise<void>((r) => setTimeout(r, 1500))
    }
  } catch {
    // Not every box lets apps pick the mode.
  }
}

export function resetFrameRate() {
  if (!VideoSupport || isPhone) return
  if (resetTimer) clearTimeout(resetTimer)
  resetTimer = setTimeout(() => {
    resetTimer = undefined
    VideoSupport.resetFrameRate()
  }, 3000)
}
