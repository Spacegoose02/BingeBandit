import React, { memo, useCallback, useEffect, useMemo, useState } from 'react'
import { ActivityIndicator, FlatList, Image, Pressable, StyleSheet, Text, TVFocusGuideView, View } from 'react-native'
import { SvgXml } from 'react-native-svg'
import Video from 'react-native-video'
import { absolute, api, authHeaders } from '../api'
import { Center } from '../components'
import { useNav } from '../nav'
import { colors, font, gutter, px, scaled } from '../theme'
import type { GuideProgram, LiveChannel } from '../types'
import { NO_LIVE_BADGE } from './Player'

// Grid geometry on the 1920×1080 canvas: two hours fill the width beside the channel column.
const SPAN = 2 * 3600
const FETCH = 6 * 3600
const HALF = 1800
const CH_W = 300
const GRID_W = 1920 - gutter - CH_W - 40
const ROW_H = 92
const ROW_GAP = 6
const perSec = GRID_W / SPAN

interface Guide {
  from: number
  to: number
  channels: LiveChannel[]
  programs: Record<string, GuideProgram[]>
}

/** A block in a channel's row: a program, or a gap with no guide data. */
interface Seg {
  key: string
  start: number
  stop: number
  program?: GuideProgram
}

const floorHalf = (t: number) => Math.floor(t / HALF) * HALF

export function clock(t: number): string {
  const d = new Date(t * 1000)
  const h = d.getHours()
  return `${h % 12 || 12}:${String(d.getMinutes()).padStart(2, '0')} ${h < 12 ? 'AM' : 'PM'}`
}

function day(t: number): string {
  const d = new Date(t * 1000)
  const today = new Date()
  const diff = Math.round((new Date(d).setHours(0, 0, 0, 0) - new Date(today).setHours(0, 0, 0, 0)) / 86400000)
  if (diff === 0) return 'Today'
  if (diff === 1) return 'Tomorrow'
  return ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][d.getDay()]
}

function segments(c: LiveChannel, progs: GuideProgram[] | undefined, from: number, to: number): Seg[] {
  const out: Seg[] = []
  let t = from
  for (const p of progs ?? []) {
    if (p.stop <= t || p.start >= to) continue
    if (p.start - t >= 60) out.push({ key: `g${t}`, start: t, stop: p.start })
    out.push({ key: `p${p.start}`, start: Math.max(p.start, from), stop: Math.min(p.stop, to), program: p })
    t = p.stop
  }
  if (to - t >= 60) out.push({ key: `g${t}`, start: t, stop: to })
  return out.length ? out : [{ key: 'none', start: from, stop: to }]
}

/** Joins a channel's stream for the preview once focus settles on it; leaves on change. */
function usePreview(channelId?: number): string | null {
  const [src, setSrc] = useState<string | null>(null)
  useEffect(() => {
    setSrc(null)
    if (!channelId) return
    let cancelled = false
    let viewer = ''
    const t = setTimeout(() => {
      api.post<{ viewerId: string; playlist: string }>(`/api/live/channels/${channelId}/watch`).then(
        (r) => {
          viewer = r.viewerId
          if (cancelled) api.del(`/api/live/viewers/${viewer}`).catch(() => undefined)
          else setSrc(absolute(r.playlist))
        },
        () => undefined,
      )
    }, 1200)
    return () => {
      cancelled = true
      clearTimeout(t)
      if (viewer) api.del(`/api/live/viewers/${viewer}`).catch(() => undefined)
    }
  }, [channelId])
  return src
}

/** The cable-box style guide: what's focused up top, then channels against a timeline. */
export default function LiveGuide() {
  const nav = useNav()
  const [guide, setGuide] = useState<Guide | null>(null)
  const [error, setError] = useState('')
  const [now, setNow] = useState(() => Date.now() / 1000)
  const [viewStart, setViewStart] = useState(() => floorHalf(Date.now() / 1000))
  const [sel, setSel] = useState<{ channel: LiveChannel; seg: Seg } | null>(null)

  const load = useCallback(() => {
    const from = floorHalf(Date.now() / 1000)
    api.get<Guide>(`/api/live/guide?from=${from}&to=${from + FETCH}`).then(
      (g) => {
        setGuide(g)
        setError('')
        setSel((s) => s ?? (g.channels[0] ? { channel: g.channels[0], seg: segments(g.channels[0], g.programs[g.channels[0].epgId ?? ''], g.from, g.to)[0] } : null))
      },
      (e) => setError(e.message),
    )
  }, [])
  useEffect(() => {
    load()
    const reload = setInterval(load, 5 * 60000)
    const tick = setInterval(() => setNow(Date.now() / 1000), 30000)
    return () => {
      clearInterval(reload)
      clearInterval(tick)
    }
  }, [load])

  // Android only moves focus to blocks it can see, so when the focused block
  // reaches an edge of the visible two hours, slide the timeline to show what's next.
  const gridFrom = guide?.from ?? 0
  const gridTo = guide?.to ?? 0
  const onFocusSeg = useCallback(
    (channel: LiveChannel, seg: Seg) => {
      setSel({ channel, seg })
      setViewStart((v) => {
        const atLeft = seg.start <= v && v > gridFrom
        const atRight = seg.stop >= v + SPAN && v + SPAN < gridTo
        let next = v
        if (atRight && !atLeft) next = Math.max(v, Math.ceil(seg.stop / HALF) * HALF - SPAN + HALF)
        else if (atLeft && !atRight) next = Math.min(v, floorHalf(seg.start) - HALF)
        return Math.max(gridFrom, Math.min(next, gridTo - SPAN))
      })
    },
    [gridFrom, gridTo],
  )
  const watch = useCallback((c: LiveChannel) => nav.push({ name: 'live', channelId: c.id }), [nav])
  const preview = usePreview(sel?.channel.id)
  // Built once per load so the memoized rows only redraw when something they show changes.
  const segsByChannel = useMemo(
    () => new Map((guide?.channels ?? []).map((c) => [c.id, segments(c, guide!.programs[c.epgId ?? ''], guide!.from, guide!.to)])),
    [guide],
  )

  if (error) return <Center><Text style={st.error}>{error}</Text></Center>
  if (!guide) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (guide.channels.length === 0) return <Center><Text style={st.muted}>No live channels on this server yet.</Text></Center>

  const p = sel?.seg.program
  const ticks: number[] = []
  for (let t = viewStart; t < viewStart + SPAN; t += HALF) ticks.push(t)
  const nowX = Math.max(0, Math.min(GRID_W, (now - viewStart) * perSec))

  return (
    <View style={st.flex}>
      <View style={st.header}>
        <View style={st.info}>
          <Text style={st.title} numberOfLines={1}>{p?.title ?? sel?.channel.name ?? ''}</Text>
          {sel && (
            <Text style={st.meta} numberOfLines={1}>
              {p ? `${day(p.start)} ${clock(p.start)} – ${clock(p.stop)}  ·  ` : ''}
              {sel.channel.number}  {sel.channel.name}
              {p?.episode ? `  ·  ${p.episode}` : ''}
            </Text>
          )}
          {!!p?.subtitle && <Text style={st.subtitle} numberOfLines={1}>{p.subtitle}</Text>}
          <Text style={st.desc} numberOfLines={3}>{p ? p.description ?? '' : 'No guide information for this time.'}</Text>
        </View>
        <View style={st.preview}>
          {preview ? (
            <Video
              key={preview}
              source={{ uri: preview, type: 'm3u8', headers: authHeaders() }}
              style={StyleSheet.absoluteFill}
              resizeMode="cover"
              muted
              controlsStyles={NO_LIVE_BADGE}
              focusable={false}
            />
          ) : (
            <ActivityIndicator color={colors.accent} />
          )}
          <Text style={st.liveTag}>LIVE</Text>
        </View>
      </View>

      <View style={st.ruler}>
        <Text style={st.rulerDay}>{day(viewStart)}</Text>
        <View style={st.rulerTrack}>
          {ticks.map((t, i) => (
            <Text key={t} style={[st.tick, { left: px((t - viewStart) * perSec) }]}>
              {i === 0 && now >= t && now < t + HALF ? 'On now' : clock(t)}
            </Text>
          ))}
          <View style={st.rulerLine} />
          {nowX > 0 && nowX < GRID_W && (
            <>
              <View style={[st.nowLine, { width: px(nowX) }]} />
              <View style={[st.nowDot, { left: px(nowX) - px(6) }]} />
            </>
          )}
        </View>
      </View>

      {/* Left/Right stay in the grid; at either end of the timeline they do nothing. */}
      <TVFocusGuideView style={st.flex} trapFocusLeft trapFocusRight>
        <FlatList
          data={guide.channels}
          keyExtractor={(c) => String(c.id)}
          extraData={`${viewStart}:${now}:${sel?.channel.id}`}
          getItemLayout={(_, i) => ({ length: px(ROW_H + ROW_GAP), offset: px(ROW_H + ROW_GAP) * i, index: i })}
          initialNumToRender={8}
          windowSize={5}
          contentContainerStyle={st.rows}
          renderItem={({ item }) => (
            <ChannelRow
              channel={item}
              segs={segsByChannel.get(item.id) ?? []}
              from={guide.from}
              viewStart={viewStart}
              now={now}
              active={sel?.channel.id === item.id}
              onFocusSeg={onFocusSeg}
              onWatch={watch}
            />
          )}
        />
      </TVFocusGuideView>
    </View>
  )
}

const ChannelRow = memo(function Row({
  channel,
  segs,
  from,
  viewStart,
  now,
  active,
  onFocusSeg,
  onWatch,
}: {
  channel: LiveChannel
  segs: Seg[]
  from: number
  viewStart: number
  now: number
  active: boolean
  onFocusSeg: (c: LiveChannel, s: Seg) => void
  onWatch: (c: LiveChannel) => void
}) {
  return (
    <View style={st.row}>
      <View style={[st.chCell, active && st.chCellActive]}>
        <Text style={st.chNumber}>{channel.number}</Text>
        <ChannelLogo channel={channel} />
      </View>
      <View style={st.track}>
        <View style={[st.trackInner, { width: px((segs[segs.length - 1].stop - from) * perSec), transform: [{ translateX: -px((viewStart - from) * perSec) }] }]}>
          {segs.map((seg) => (
            <Cell key={seg.key} seg={seg} from={from} viewStart={viewStart} airing={seg.start <= now && now < seg.stop} onFocus={() => onFocusSeg(channel, seg)} onPress={() => onWatch(channel)} />
          ))}
        </View>
      </View>
    </View>
  )
})

/** The channel's logo (Android's Image can't draw SVG, so those go through react-native-svg); the name if there's none. */
function ChannelLogo({ channel }: { channel: LiveChannel }) {
  const [logo, setLogo] = useState<{ svg?: string; uri?: string } | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!channel.logo) return
    const uri = absolute(`/api/live/channels/${channel.id}/logo`)
    fetch(uri, { headers: authHeaders() }).then(
      async (r) => {
        if (!r.ok) return setFailed(true)
        if ((r.headers.get('content-type') ?? '').includes('svg')) setLogo({ svg: await r.text() })
        else setLogo({ uri })
      },
      () => setFailed(true),
    )
  }, [channel.id, channel.logo])
  if (!channel.logo || failed) return <Text style={st.chName} numberOfLines={2}>{channel.name}</Text>
  if (logo?.svg) return <SvgXml xml={logo.svg} width={px(150)} height={px(64)} />
  if (logo?.uri) return <Image source={{ uri: logo.uri, headers: authHeaders() }} style={st.chLogo} resizeMode="contain" onError={() => setFailed(true)} />
  return <View style={st.chLogo} />
}

function Cell({ seg, from, viewStart, airing, onFocus, onPress }: { seg: Seg; from: number; viewStart: number; airing: boolean; onFocus: () => void; onPress: () => void }) {
  const [focused, setFocused] = useState(false)
  const left = (seg.start - from) * perSec
  const width = (seg.stop - seg.start) * perSec
  // Keep the title readable when the block starts left of the visible window.
  const inset = Math.max(0, Math.min(width - 60, (viewStart - seg.start) * perSec))
  const startedEarlier = seg.program && seg.program.start < seg.start
  return (
    <Pressable
      onFocus={() => {
        setFocused(true)
        onFocus()
      }}
      onBlur={() => setFocused(false)}
      onPress={onPress}
      style={[st.cell, airing && st.cellAiring, focused && st.cellFocused, { left: px(left), width: px(width) - px(ROW_GAP), paddingLeft: px(18 + inset) }]}
    >
      <Text style={[st.cellText, !seg.program && st.cellEmpty, focused && st.cellTextFocused]} numberOfLines={1}>
        {startedEarlier || inset > 0 ? '‹ ' : ''}
        {seg.program?.title ?? 'No guide information'}
      </Text>
    </Pressable>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  error: { color: colors.danger, fontSize: font.body, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
  header: { flexDirection: 'row', paddingHorizontal: gutter, paddingTop: 10, height: 300 },
  info: { flex: 1, paddingRight: 40 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800' },
  meta: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  subtitle: { color: colors.text, fontSize: font.body, marginTop: 10, fontWeight: '600' },
  desc: { color: colors.text, fontSize: font.small, marginTop: 10, lineHeight: 32, opacity: 0.85 },
  preview: { width: 480, height: 270, borderRadius: 16, overflow: 'hidden', backgroundColor: colors.surface, alignItems: 'center', justifyContent: 'center' },
  liveTag: { position: 'absolute', top: 12, left: 12, color: colors.accentText, backgroundColor: colors.accent, fontSize: 16, fontWeight: '900', paddingHorizontal: 10, paddingVertical: 3, borderRadius: 6, overflow: 'hidden' },
  ruler: { flexDirection: 'row', alignItems: 'flex-end', paddingLeft: gutter, height: 56, marginBottom: 10 },
  rulerDay: { width: CH_W, color: colors.muted, fontSize: font.small, paddingBottom: 14, paddingLeft: 24 },
  rulerTrack: { width: GRID_W, height: 56, overflow: 'hidden' },
  tick: { position: 'absolute', top: 6, color: colors.muted, fontSize: font.small },
  rulerLine: { position: 'absolute', left: 0, right: 0, bottom: 0, height: 2, backgroundColor: colors.surface3 },
  nowLine: { position: 'absolute', left: 0, bottom: 0, height: 4, backgroundColor: colors.accent },
  nowDot: { position: 'absolute', bottom: -4, width: 12, height: 12, borderRadius: 6, backgroundColor: colors.accent },
  rows: { paddingLeft: gutter, paddingBottom: 40 },
  row: { flexDirection: 'row', height: ROW_H, marginBottom: ROW_GAP },
  chCell: { width: CH_W - ROW_GAP, marginRight: ROW_GAP, flexDirection: 'row', alignItems: 'center', paddingHorizontal: 20, borderRadius: 10 },
  chCellActive: { backgroundColor: colors.surface2 },
  chNumber: { color: colors.text, fontSize: font.body, fontWeight: '700', width: 82 },
  chLogo: { width: 150, height: 64 },
  chName: { color: colors.text, fontSize: font.small, fontWeight: '700', flex: 1 },
  track: { width: GRID_W, overflow: 'hidden' },
  trackInner: { height: ROW_H },
  cell: { position: 'absolute', top: 0, height: ROW_H, justifyContent: 'center', paddingRight: 14, backgroundColor: colors.surface, borderRadius: 10 },
  cellAiring: { backgroundColor: colors.surface3 },
  cellFocused: { backgroundColor: colors.accent },
  cellText: { color: colors.text, fontSize: font.body, fontWeight: '600' },
  cellEmpty: { color: colors.muted, fontWeight: '400' },
  cellTextFocused: { color: colors.accentText },
}))
