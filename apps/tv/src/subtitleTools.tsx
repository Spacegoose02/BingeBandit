// Scrub thumbnails and subtitle search for the player (web: pages/Player.tsx).
import React, { useEffect, useState } from 'react'
import { Image, ScrollView, StyleSheet, Text, View } from 'react-native'
import { absolute, api, authHeaders } from './api'
import { Button } from './components'
import { colors, font, gutter, px, scaled } from './theme'
import { languageName, normLang, type MediaStream } from './tracks'

/** Scrub previews: thumbnail n (at n × interval seconds) is in sheet n / (columns × rows). */
export interface TrickplayInfo {
  interval: number
  width: number
  height: number
  columns: number
  rows: number
  count: number
  sheets: number
}

/** A subtitle found on OpenSubtitles. */
interface SubtitleResult {
  fileId: number
  name: string
  language: string
  downloads: number
  hearingImpaired: boolean
  hashMatch: boolean
  trusted: boolean
  ai: boolean
}

// The languages offered for searching (639-2 codes, as in playback preferences).
const LANGUAGES = ['eng', 'spa', 'fre', 'ger', 'ita', 'por', 'dut', 'swe', 'nor', 'dan', 'fin', 'pol', 'rus', 'jpn', 'kor', 'chi', 'hin', 'ara']

/** The file's scrub previews, or null while the server has none (it makes them for next time). */
export function useTrickplay(fileId?: number): TrickplayInfo | null {
  const [info, setInfo] = useState<TrickplayInfo | null>(null)
  useEffect(() => {
    setInfo(null)
    if (!fileId) return
    let stale = false
    api.get<TrickplayInfo>(`/api/files/${fileId}/trickplay`).then(
      (t) => !stale && setInfo(t),
      () => undefined,
    )
    return () => {
      stale = true
    }
  }, [fileId])
  return info
}

export const THUMB_WIDTH = px(320)

/** The thumbnail for time t, cut from its sheet. */
export function ScrubThumb({ fileId, info, t }: { fileId: number; info: TrickplayInfo; t: number }) {
  const scale = THUMB_WIDTH / info.width
  const h = info.height * scale
  const per = info.columns * info.rows
  const n = Math.max(0, Math.min(info.count - 1, Math.floor(t / info.interval)))
  const at = n % per
  return (
    <View style={[st.thumb, { width: THUMB_WIDTH, height: h }]} pointerEvents="none">
      <Image
        source={{ uri: absolute(`/api/files/${fileId}/trickplay/${Math.floor(n / per)}.jpg`), headers: authHeaders() }}
        style={[st.sheet, {
          width: info.columns * THUMB_WIDTH,
          height: info.rows * h,
          left: -(at % info.columns) * THUMB_WIDTH,
          top: -Math.floor(at / info.columns) * h,
        }]}
        fadeDuration={0}
      />
    </View>
  )
}

export function delayLabel(d: number): string {
  if (d === 0) return 'In sync'
  return `${Math.abs(d).toFixed(1)}s ${d > 0 ? 'later' : 'earlier'}`
}

/** Searches OpenSubtitles for the file and adds the one picked. */
export function SubtitleSearch({
  fileId,
  language,
  onDone,
  style,
}: {
  fileId: number
  language?: string
  onDone: (streams: MediaStream[], index: number) => void
  style?: object | null
}) {
  const [lang, setLang] = useState(normLang(language) || 'eng')
  const [results, setResults] = useState<SubtitleResult[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<number | null>(null)
  useEffect(() => {
    let stale = false
    setResults(null)
    setError('')
    api.get<{ results: SubtitleResult[] }>(`/api/files/${fileId}/subtitles/search?lang=${lang}`).then(
      (r) => !stale && setResults(r.results),
      (e) => !stale && setError((e as Error).message),
    )
    return () => {
      stale = true
    }
  }, [fileId, lang])
  const pick = async (r: SubtitleResult) => {
    setBusy(r.fileId)
    setError('')
    try {
      const res = await api.post<{ streams: MediaStream[]; index: number }>(`/api/files/${fileId}/subtitles/download`, {
        fileId: r.fileId,
        language: r.language,
        hearingImpaired: r.hearingImpaired,
      })
      onDone(res.streams, res.index)
    } catch (e) {
      setError((e as Error).message)
      setBusy(null)
    }
  }
  const langs = LANGUAGES.includes(lang) ? LANGUAGES : [lang, ...LANGUAGES]
  return (
    <View style={[st.panel, style]}>
      <Text style={st.title}>Find subtitles online</Text>
      <ScrollView horizontal contentContainerStyle={st.row}>
        {langs.map((l) => (
          <Button key={l} small primary={l === lang} label={languageName(l)} onPress={() => setLang(l)} />
        ))}
      </ScrollView>
      {!!error && <Text style={[st.note, st.bad]}>{error}</Text>}
      {!results && !error && <Text style={st.note}>Searching OpenSubtitles…</Text>}
      {results?.length === 0 && <Text style={st.note}>Nothing found in {languageName(lang)}.</Text>}
      <ScrollView style={st.results}>
        {results?.slice(0, 30).map((r, i) => {
          const meta = [
            r.hashMatch && 'Made for this file',
            `${r.downloads.toLocaleString()} downloads`,
            r.hearingImpaired && 'SDH',
            r.ai && 'Machine translated',
            r.trusted && 'Trusted',
          ]
            .filter(Boolean)
            .join(' · ')
          return (
            <Button
              key={r.fileId}
              small
              preferFocus={i === 0}
              label={busy === r.fileId ? 'Downloading…' : `${r.name}  ·  ${meta}`}
              onPress={() => busy === null && pick(r)}
              style={st.result}
            />
          )
        })}
      </ScrollView>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  sheet: { position: 'absolute' },
  thumb: { overflow: 'hidden', borderRadius: 8, borderWidth: 3, borderColor: '#fff', backgroundColor: '#000' },
  panel: { position: 'absolute', left: 0, right: 0, bottom: 0, top: 120, paddingHorizontal: gutter, paddingTop: 30, paddingBottom: 40, backgroundColor: 'rgba(11,13,16,0.96)' },
  title: { color: colors.text, fontSize: font.title, fontWeight: '700', marginBottom: 6 },
  row: { paddingVertical: 10 },
  note: { color: colors.muted, fontSize: font.body, marginVertical: 12 },
  bad: { color: '#f2626b' },
  results: { flex: 1, marginTop: 6 },
  result: { alignSelf: 'flex-start', marginBottom: 10 },
}))
