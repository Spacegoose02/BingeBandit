import type { Item, MediaFile } from './types'

export function formatClock(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) seconds = 0
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
  return `${h > 0 ? `${h}:` : ''}${mm}:${String(s).padStart(2, '0')}`
}

export function formatRuntime(minutes?: number): string {
  if (!minutes) return ''
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

export function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  while (bytes >= 1024 && i < units.length - 1) {
    bytes /= 1024
    i++
  }
  return `${bytes.toFixed(i > 2 ? 1 : 0)} ${units[i]}`
}

export function timeAgo(unix: number): string {
  if (!unix) return 'never'
  const diff = Date.now() / 1000 - unix
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

export function resolutionLabel(height: number): string {
  if (height >= 2000) return '4K'
  if (height >= 1000) return '1080p'
  if (height >= 700) return '720p'
  if (height >= 560) return '576p'
  if (height > 0) return '480p'
  return 'SD'
}

export function channelLabel(n: number): string {
  return ({ 1: 'Mono', 2: 'Stereo', 6: '5.1', 8: '7.1' } as Record<number, string>)[n] ?? `${n}ch`
}

export function episodeLabel(it: Item): string {
  if (it.kind !== 'episode') return ''
  return `S${it.seasonNumber ?? 0} · E${it.episodeNumber ?? 0}`
}

export function progressFraction(it: Item): number {
  const { position, duration } = it.watch
  return duration > 0 && position > 0 ? Math.min(position / duration, 1) : 0
}

export function fileSummary(f: MediaFile): string {
  const parts = [`${resolutionLabel(f.height)} ${f.videoCodec.toUpperCase()}${f.hdr ? ' HDR' : ''}`]
  if (f.audioCodec) parts.push(`${f.audioCodec.toUpperCase()} ${channelLabel(f.audioChannels)}`)
  return parts.join(' · ')
}

const languageNames = typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['en'], { type: 'language' }) : null

export function languageName(code?: string): string {
  if (!code || code === 'und') return 'Unknown'
  try {
    return languageNames?.of(code) ?? code
  } catch {
    return code
  }
}
