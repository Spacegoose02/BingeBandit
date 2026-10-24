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

// ISO 639-1 codes and names → the 639-2 codes ffprobe reports (mirrors server/internal/library/subtitles.go).
const langAliases: Record<string, string> = {}
for (const row of [
  ['eng', 'en', 'english'], ['spa', 'es', 'spanish'], ['fre', 'fr', 'fra', 'french'], ['ger', 'de', 'deu', 'german'],
  ['ita', 'it', 'italian'], ['por', 'pt', 'portuguese'], ['dut', 'nl', 'nld', 'dutch'], ['swe', 'sv', 'swedish'],
  ['nor', 'no', 'nb', 'nob', 'norwegian'], ['dan', 'da', 'danish'], ['fin', 'fi', 'finnish'], ['pol', 'pl', 'polish'],
  ['rus', 'ru', 'russian'], ['ukr', 'uk', 'ukrainian'], ['cze', 'cs', 'ces', 'czech'], ['gre', 'el', 'ell', 'greek'],
  ['tur', 'tr', 'turkish'], ['ara', 'ar', 'arabic'], ['heb', 'he', 'hebrew'], ['hin', 'hi', 'hindi'],
  ['jpn', 'ja', 'japanese'], ['kor', 'ko', 'korean'], ['chi', 'zh', 'zho', 'chinese'], ['tha', 'th', 'thai'],
  ['vie', 'vi', 'vietnamese'], ['ind', 'id', 'indonesian'], ['hun', 'hu', 'hungarian'], ['rum', 'ro', 'ron', 'romanian'],
]) {
  for (const alias of row) langAliases[alias] = row[0]
}

export function normLang(code?: string): string {
  if (!code) return ''
  const c = code.toLowerCase()
  return langAliases[c] ?? c
}

/** Languages offered in playback preferences (639-2 codes). */
export const commonLanguages = ['eng', 'spa', 'fre', 'ger', 'ita', 'por', 'dut', 'swe', 'nor', 'dan', 'fin', 'pol', 'rus', 'jpn', 'kor', 'chi', 'hin', 'ara']

const languageNames =typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['en'], { type: 'language' }) : null

export function languageName(code?: string): string {
  if (!code || code === 'und') return 'Unknown'
  // Intl knows 639-1 better than 639-2/B codes like "ger" or "fre".
  const short = Object.entries(langAliases).find(([alias, canon]) => canon === normLang(code) && alias.length === 2)?.[0]
  try {
    return languageNames?.of(short ?? code) ?? code
  } catch {
    return code
  }
}

/** Describes a smart collection's rules, e.g. "Horror · 1980–1989", or "" for a hand-picked one. */
export function smartRuleText(c: { labelRule?: string; genreRule?: string; studioRule?: string; yearFrom?: number; yearTo?: number }): string {
  const parts: string[] = []
  if (c.genreRule) parts.push(c.genreRule)
  if (c.studioRule) parts.push(c.studioRule)
  if (c.yearFrom && c.yearTo) parts.push(c.yearFrom === c.yearTo ? `${c.yearFrom}` : `${c.yearFrom}–${c.yearTo}`)
  else if (c.yearFrom) parts.push(`${c.yearFrom} on`)
  else if (c.yearTo) parts.push(`up to ${c.yearTo}`)
  if (c.labelRule) parts.push(`labelled “${c.labelRule}”`)
  return parts.join(' · ')
}
