// Audio and subtitle choice, ported from the web player (web/src/pages/Player.tsx).

export interface MediaStream {
  index: number
  type: 'video' | 'audio' | 'subtitle'
  codec: string
  language?: string
  title?: string
  channels?: number
  default?: boolean
  forced?: boolean
  hearingImpaired?: boolean
  external?: string
}

export interface Prefs {
  audioLanguage: string
  subtitleMode: 'off' | 'auto' | 'forced' | 'always'
  subtitleLanguage: string
}

export const TEXT_SUBS = new Set(['subrip', 'srt', 'ass', 'ssa', 'webvtt', 'mov_text', 'text'])
export const IMAGE_SUBS = new Set(['hdmv_pgs_subtitle', 'dvd_subtitle', 'dvb_subtitle', 'xsub'])

// 639-2 code, aliases, English name (mirrors web/src/format.ts; Hermes lacks Intl.DisplayNames).
const LANGS: [string, string[], string][] = [
  ['eng', ['en', 'english'], 'English'], ['spa', ['es', 'spanish'], 'Spanish'], ['fre', ['fr', 'fra', 'french'], 'French'],
  ['ger', ['de', 'deu', 'german'], 'German'], ['ita', ['it', 'italian'], 'Italian'], ['por', ['pt', 'portuguese'], 'Portuguese'],
  ['dut', ['nl', 'nld', 'dutch'], 'Dutch'], ['swe', ['sv', 'swedish'], 'Swedish'], ['nor', ['no', 'nb', 'nob', 'norwegian'], 'Norwegian'],
  ['dan', ['da', 'danish'], 'Danish'], ['fin', ['fi', 'finnish'], 'Finnish'], ['pol', ['pl', 'polish'], 'Polish'],
  ['rus', ['ru', 'russian'], 'Russian'], ['ukr', ['uk', 'ukrainian'], 'Ukrainian'], ['cze', ['cs', 'ces', 'czech'], 'Czech'],
  ['gre', ['el', 'ell', 'greek'], 'Greek'], ['tur', ['tr', 'turkish'], 'Turkish'], ['ara', ['ar', 'arabic'], 'Arabic'],
  ['heb', ['he', 'hebrew'], 'Hebrew'], ['hin', ['hi', 'hindi'], 'Hindi'], ['jpn', ['ja', 'japanese'], 'Japanese'],
  ['kor', ['ko', 'korean'], 'Korean'], ['chi', ['zh', 'zho', 'chinese'], 'Chinese'], ['tha', ['th', 'thai'], 'Thai'],
  ['vie', ['vi', 'vietnamese'], 'Vietnamese'], ['ind', ['id', 'indonesian'], 'Indonesian'], ['hun', ['hu', 'hungarian'], 'Hungarian'],
  ['rum', ['ro', 'ron', 'romanian'], 'Romanian'],
]
const aliases: Record<string, string> = {}
const names: Record<string, string> = {}
for (const [code, alts, name] of LANGS) {
  aliases[code] = code
  names[code] = name
  for (const a of alts) aliases[a] = code
}

export function normLang(code?: string): string {
  if (!code) return ''
  const c = code.toLowerCase()
  return aliases[c] ?? c
}

export function languageName(code?: string): string {
  if (!code || code === 'und') return 'Unknown'
  return names[normLang(code)] ?? code
}

function channelLabel(n?: number): string {
  if (!n) return ''
  return n === 1 ? 'Mono' : n === 2 ? 'Stereo' : n === 6 ? '5.1' : n === 8 ? '7.1' : `${n}ch`
}

export function streamLabel(s: MediaStream): string {
  const bits = [languageName(s.language)]
  if (s.type === 'audio') bits.push(`${s.codec.toUpperCase()} ${channelLabel(s.channels)}`.trim())
  if (s.title) bits.push(s.title)
  if (s.forced) bits.push('Forced')
  if (s.hearingImpaired) bits.push('SDH')
  return bits.join(' · ')
}

/**
 * Picks audio and subtitles from the viewer's preferences: the audio stream
 * (-1 when it's the file default, so direct play stays possible) and the
 * subtitle stream to show, if any.
 */
export function pickTracks(streams: MediaStream[], p: Prefs | null): { audio: number; sub?: MediaStream } {
  const audios = streams.filter((s) => s.type === 'audio')
  const fileDefault = audios.find((s) => s.default) ?? audios[0]
  let chosen = fileDefault
  if (p?.audioLanguage) chosen = audios.find((s) => normLang(s.language) === normLang(p.audioLanguage)) ?? fileDefault
  const audio = chosen && chosen !== fileDefault ? chosen.index : -1
  if (!p) return { audio }

  const subs = streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec)))
  const want = normLang(p.subtitleLanguage)
  const inLang = subs.filter((s) => normLang(s.language) === want)
  // Prefer text over image subtitles and plain over SDH.
  const rank = (s: MediaStream) => (IMAGE_SUBS.has(s.codec) ? 2 : 0) + (s.hearingImpaired ? 1 : 0)
  const best = (list: MediaStream[]) => [...list].sort((a, b) => rank(a) - rank(b))[0]
  const full = best(inLang.filter((s) => !s.forced)) ?? best(subs.filter((s) => !s.language && !s.forced))
  const forced = best(inLang.filter((s) => s.forced))
  const foreignAudio = !!chosen?.language && normLang(chosen.language) !== want

  let sub: MediaStream | undefined
  switch (p.subtitleMode) {
    case 'always':
      sub = full ?? forced
      break
    case 'auto':
      sub = foreignAudio ? full ?? forced : forced
      break
    case 'forced':
      sub = forced
      break
  }
  return { audio, sub }
}

export interface Cue {
  start: number
  end: number
  text: string
}

export function parseVtt(text: string): Cue[] {
  const toSec = (t: string) => {
    const p = t.trim().split(':').map(Number)
    return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : p[0] * 60 + p[1]
  }
  const cues: Cue[] = []
  for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
    const lines = block.split('\n')
    const i = lines.findIndex((l) => l.includes('-->'))
    if (i < 0) continue
    const [a, b] = lines[i].split('-->')
    const body = lines.slice(i + 1).join('\n').replace(/<[^>]+>/g, '').trim()
    if (body) cues.push({ start: toSec(a), end: toSec(b.trim().split(' ')[0]), text: body })
  }
  return cues
}

/** The cue showing at time t (cues are sorted by start). */
export function cueAt(cues: Cue[], t: number): string {
  for (const c of cues) {
    if (c.start > t) break
    if (t < c.end) return c.text
  }
  return ''
}
