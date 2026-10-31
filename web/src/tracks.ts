import { normLang } from './format'
import type { MediaFile, MediaStream, Prefs } from './types'

export const TEXT_SUBS = new Set(['subrip', 'srt', 'ass', 'ssa', 'webvtt', 'mov_text', 'text'])
export const IMAGE_SUBS = new Set(['hdmv_pgs_subtitle', 'dvd_subtitle', 'dvb_subtitle', 'xsub'])

/**
 * Picks audio and subtitles from the user's preferences. Returns the audio
 * stream (-1 when it's the file default, so direct play stays possible) and
 * the subtitle stream, which is either rendered by us or burned in.
 */
export function pickTracks(f: MediaFile, p: Prefs): { audio: number; sub?: MediaStream } {
  const audios = f.streams.filter((s) => s.type === 'audio')
  const fileDefault = audios.find((s) => s.default) ?? audios[0]
  let chosen = fileDefault
  if (p.audioLanguage) chosen = audios.find((s) => normLang(s.language) === normLang(p.audioLanguage)) ?? fileDefault
  const audio = chosen && chosen !== fileDefault ? chosen.index : -1

  const subs = f.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec)))
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
