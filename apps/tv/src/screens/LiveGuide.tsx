import React, { memo, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { ActivityIndicator, FlatList, Image, PanResponder, Pressable, ScrollView, StyleSheet, Text, TVFocusGuideView, View, useWindowDimensions } from 'react-native'
import { SvgXml } from 'react-native-svg'
import Video from 'react-native-video'
import { absolute, api, authHeaders } from '../api'
import { Center } from '../components'
import { joinChannel, leaveChannel } from '../live'
import { FocusMemoryContext, useNav, useScreen, type FocusTarget } from '../nav'
import { colors, font, gutter, isPhone, px, scaled, units } from '../theme'
import type { ChannelCategory, GuideProgram, LiveChannel } from '../types'
import { lastChannel } from './LivePlayer'
import { NO_LIVE_BADGE } from './Player'

const FETCH = 6 * 3600
const HALF = 1800
const CH_W = isPhone ? 190 : 300 // channel column, canvas units
const ROW_H = 92
const ROW_GAP = 6

/** How the timeline maps to the screen: the grid's width in canvas units, the time it shows, and units per second. */
interface Geo {
  gridW: number
  span: number
  perSec: number
}

interface Guide {
  from: number
  to: number
  channels: LiveChannel[]
  programs: Record<string, GuideProgram[]>
  categories?: ChannelCategory[] // the viewer's own; older servers send none
}

/** The guide's channel filter: all, favourites, one of your categories ("my:<id>") or a provider category. Kept while the app runs. */
let lastFilter = 'all'

function filterChannels(g: Guide, filter: string): LiveChannel[] {
  if (filter === 'all') return g.channels
  if (filter === 'fav') return g.channels.filter((c) => c.favorite)
  if (filter.startsWith('my:')) {
    const ids = new Set(g.categories?.find((c) => `my:${c.id}` === filter)?.channelIds ?? [])
    return g.channels.filter((c) => ids.has(c.id))
  }
  return g.channels.filter((c) => c.group === filter)
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

/** Joins a channel's stream for the preview; leaves on change. */
function usePreview(channelId?: number): string | null {
  const [src, setSrc] = useState<string | null>(null)
  useEffect(() => {
    setSrc(null)
    if (!channelId) return
    let cancelled = false
    let viewer = ''
    joinChannel(channelId).then(
      (r) => {
        viewer = r.viewerId
        if (cancelled) leaveChannel(viewer)
        else setSrc(r.playlist)
      },
      () => undefined,
    )
    return () => {
      cancelled = true
      leaveChannel(viewer)
    }
  }, [channelId])
  return src
}

/** The cable-box style guide: what's selected up top, then channels against a timeline. */
export default function LiveGuide() {
  const nav = useNav()
  const [guide, setGuide] = useState<Guide | null>(null)
  const [error, setError] = useState('')
  const [now, setNow] = useState(() => Date.now() / 1000)
  const [viewStart, setViewStart] = useState(() => floorHalf(Date.now() / 1000))
  const [sel, setSel] = useState<{ channel: LiveChannel; seg: Seg } | null>(null)
  // The preview stays on the channel you were watching while you browse; only choosing one changes it.
  const [tuned, setTuned] = useState<LiveChannel | null>(null)
  const [filter, setFilterState] = useState(lastFilter)
  const setFilter = useCallback((f: string) => {
    lastFilter = f
    setFilterState(f)
  }, [])

  // Two hours across on a TV or a sideways phone, one hour on a phone held upright.
  const { width, height } = useWindowDimensions()
  const wide = isPhone && width > height // a sideways phone: shorter header, more rows
  const geo: Geo = useMemo(() => {
    const gridW = units(width) - gutter - CH_W - (isPhone ? gutter : 40)
    const span = gridW >= 1100 ? 2 * 3600 : 3600
    return { gridW, span, perSec: gridW / span }
  }, [width])

  const load = useCallback(() => {
    const from = floorHalf(Date.now() / 1000)
    api.get<Guide>(`/api/live/guide?from=${from}&to=${from + FETCH}`).then(
      (g) => {
        setGuide(g)
        setError('')
        const start = g.channels.find((c) => c.id === lastChannel.id) ?? g.channels[0]
        setTuned((t) => t ?? start ?? null)
        setSel((s) => s ?? (start ? { channel: start, seg: segments(start, g.programs[start.epgId ?? ''], g.from, g.to)[0] } : null))
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

  const gridFrom = guide?.from ?? 0
  const gridTo = guide?.to ?? 0
  const clampStart = useCallback((v: number) => Math.max(gridFrom, Math.min(v, gridTo - geo.span)), [gridFrom, gridTo, geo.span])

  // TV: Android only moves focus to blocks it can see, so when the focused block
  // reaches an edge of the visible window, slide the timeline to show what's next.
  const onFocusSeg = useCallback(
    (channel: LiveChannel, seg: Seg) => {
      setFocusSel(0)
      setSel({ channel, seg })
      setViewStart((v) => {
        const atLeft = seg.start <= v && v > gridFrom
        const atRight = seg.stop >= v + geo.span && v + geo.span < gridTo
        let next = v
        if (atRight && !atLeft) next = Math.max(v, Math.ceil(seg.stop / HALF) * HALF - geo.span + HALF)
        else if (atLeft && !atRight) next = Math.min(v, floorHalf(seg.start) - HALF)
        return clampStart(next)
      })
    },
    [gridFrom, gridTo, geo.span, clampStart],
  )
  const watch = useCallback((c: LiveChannel) => nav.push({ name: 'live', channelId: c.id }), [nav])
  // Phone: the first tap on a block shows it up top; tapping it again tunes in.
  const selRef = useRef(sel)
  selRef.current = sel
  const onTapSeg = useCallback(
    (channel: LiveChannel, seg: Seg) => {
      const s = selRef.current
      if (s && s.channel.id === channel.id && s.seg.key === seg.key) watch(channel)
      else setSel({ channel, seg })
    },
    [watch],
  )

  // Phone: drag sideways anywhere on the grid to move through time.
  const dragFrom = useRef(0)
  const pan = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponderCapture: (_, g) => isPhone && Math.abs(g.dx) > 12 && Math.abs(g.dx) > Math.abs(g.dy) * 1.5,
        onPanResponderGrant: () => setViewStart((v) => (dragFrom.current = v)),
        onPanResponderMove: (_, g) => setViewStart(clampStart(dragFrom.current - units(g.dx) / geo.perSec)),
        onPanResponderTerminationRequest: () => false,
      }),
    [clampStart, geo.perSec],
  )

  const filters = useMemo(() => {
    if (!guide) return []
    const out = [{ key: 'all', label: 'All channels' }]
    if (guide.channels.some((c) => c.favorite)) out.push({ key: 'fav', label: '★ Favourites' })
    for (const c of guide.categories ?? []) out.push({ key: `my:${c.id}`, label: c.name })
    for (const g of new Set(guide.channels.map((c) => c.group).filter(Boolean) as string[])) out.push({ key: g, label: g })
    return out
  }, [guide])
  // A filter that went away (a deleted category, no favourites left) falls back to all.
  const activeFilter = filters.some((f) => f.key === filter) ? filter : 'all'
  const shown = useMemo(() => (guide ? filterChannels(guide, activeFilter) : []), [guide, activeFilter])

  // Only while the guide is on screen: watching a channel leaves the preview.
  const { active, returns } = useScreen()
  // Back from watching full screen: the preview carries on with the channel
  // that was being watched (which may have been flipped to there), and the
  // guide's selection moves to it, with focus on what's on now. The menu
  // can't do that: the guide's blocks aren't remembered like other buttons,
  // so focus went back to the Live TV tab.
  const synced = useRef(lastChannel.id)
  const restored = useRef(returns)
  const [focusSel, setFocusSel] = useState(0)
  useEffect(() => {
    if (!active || !guide) return
    if (lastChannel.id !== synced.current) {
      synced.current = lastChannel.id
      const c = guide.channels.find((ch) => ch.id === lastChannel.id)
      if (c && selRef.current?.channel.id !== c.id) {
        const t = Date.now() / 1000
        const segs = segments(c, guide.programs[c.epgId ?? ''], guide.from, guide.to)
        setTuned(c)
        setSel({ channel: c, seg: segs.find((s) => s.start <= t && t < s.stop) ?? segs[0] })
        setViewStart(clampStart(floorHalf(t)))
        if (!shown.some((x) => x.id === c.id)) setFilter('all') // flipped to a channel the filter hides
      }
    }
    if (returns > restored.current) {
      restored.current = returns
      if (!isPhone) setFocusSel(returns)
    }
  }, [active, guide, returns, clampStart, setFilter, shown])
  const preview = usePreview(active ? tuned?.id : undefined)
  // Built once per load so the memoized rows only redraw when something they show changes.
  // Bring the selected row into view so its block can take focus.
  const list = useRef<FlatList<LiveChannel>>(null)
  useEffect(() => {
    if (!focusSel || !sel) return
    const i = shown.findIndex((c) => c.id === sel.channel.id)
    if (i >= 0) list.current?.scrollToIndex({ index: i, animated: false, viewPosition: 0.3 })
  }, [focusSel, sel, shown])
  const segsByChannel = useMemo(
    () => new Map((guide?.channels ?? []).map((c) => [c.id, segments(c, guide!.programs[c.epgId ?? ''], guide!.from, guide!.to)])),
    [guide],
  )

  if (error) return <Center><Text style={st.error}>{error}</Text></Center>
  if (!guide) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (guide.channels.length === 0) return <Center><Text style={st.muted}>No live channels on this server yet.</Text></Center>

  const p = sel?.seg.program
  const ticks: number[] = []
  for (let t = Math.ceil(viewStart / HALF) * HALF; t < viewStart + geo.span; t += HALF) ticks.push(t)
  const nowX = (now - viewStart) * geo.perSec

  return (
    <View style={st.flex}>
      <View style={[st.header, wide && st.headerWide]}>
        <View style={st.info}>
          <Text style={st.title} numberOfLines={isPhone ? 2 : 1}>{p?.title ?? sel?.channel.name ?? ''}</Text>
          {sel && (
            <Text style={st.meta} numberOfLines={2}>
              {p ? `${day(p.start)} ${clock(p.start)} – ${clock(p.stop)}  ·  ` : ''}
              {sel.channel.number}  {sel.channel.name}
              {p?.episode ? `  ·  ${p.episode}` : ''}
            </Text>
          )}
          {!!p?.subtitle && <Text style={st.subtitle} numberOfLines={1}>{p.subtitle}</Text>}
          <Text style={st.desc} numberOfLines={wide ? 1 : isPhone ? 2 : 3}>{p ? p.description ?? '' : 'No guide information for this time.'}</Text>
        </View>
        <Pressable style={[st.preview, wide && st.previewWide]} focusable={false} onPress={tuned ? () => watch(tuned) : undefined}>
          {preview ? (
            // The video surface would swallow taps meant for the preview (tap to watch).
            <View style={StyleSheet.absoluteFill} pointerEvents="none">
              <Video
                key={preview}
                source={{ uri: preview, type: 'm3u8', headers: authHeaders() }}
                style={StyleSheet.absoluteFill}
                resizeMode="cover"
                muted
                controlsStyles={NO_LIVE_BADGE}
                focusable={false}
              />
            </View>
          ) : (
            <ActivityIndicator color={colors.accent} />
          )}
          <Text style={st.liveTag}>{isPhone ? 'LIVE · TAP TO WATCH' : 'LIVE'}</Text>
          {tuned && <Text style={st.tunedTag} numberOfLines={1}>{tuned.number}  {tuned.name}</Text>}
        </Pressable>
      </View>

      {filters.length > 1 && (
        <ScrollView horizontal style={st.filters} contentContainerStyle={st.filtersRow} showsHorizontalScrollIndicator={false}>
          {filters.map((f) => (
            <FilterChip key={f.key} label={f.label} on={f.key === activeFilter} onPress={() => setFilter(f.key)} />
          ))}
        </ScrollView>
      )}

      <View style={st.flex} {...pan.panHandlers}>
        <View style={st.ruler}>
          <Text style={st.rulerDay}>{day(viewStart)}</Text>
          <View style={[st.rulerTrack, { width: px(geo.gridW) }]}>
            {ticks.map((t) => (
              <Text key={t} style={[st.tick, { left: px((t - viewStart) * geo.perSec) }]}>
                {t === floorHalf(now) ? 'On now' : clock(t)}
              </Text>
            ))}
            <View style={st.rulerLine} />
            {nowX > 0 && nowX < geo.gridW && (
              <>
                <View style={[st.nowLine, { width: px(nowX) }]} />
                <View style={[st.nowDot, { left: px(nowX) - px(6) }]} />
              </>
            )}
          </View>
        </View>

        {/* TV: Left/Right stay in the grid; at either end of the timeline they do nothing. */}
        <TVFocusGuideView style={st.flex} trapFocusLeft trapFocusRight>
          {shown.length === 0 && <Text style={[st.muted, st.noneShown]}>No channels in this category yet.</Text>}
          <FlatList
            ref={list}
            data={shown}
            keyExtractor={(c) => String(c.id)}
            extraData={`${viewStart}:${now}:${sel?.channel.id}:${sel?.seg.key}:${geo.gridW}:${focusSel}`}
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
                geo={geo}
                active={sel?.channel.id === item.id}
                selKey={sel?.channel.id === item.id ? sel.seg.key : undefined}
                focusSel={sel?.channel.id === item.id ? focusSel : 0}
                onFocusSeg={onFocusSeg}
                onTapSeg={onTapSeg}
                onWatch={watch}
              />
            )}
          />
        </TVFocusGuideView>
      </View>
    </View>
  )
}

const ChannelRow = memo(function Row({
  channel,
  segs,
  from,
  viewStart,
  now,
  geo,
  active,
  selKey,
  focusSel,
  onFocusSeg,
  onTapSeg,
  onWatch,
}: {
  channel: LiveChannel
  segs: Seg[]
  from: number
  viewStart: number
  now: number
  geo: Geo
  active: boolean
  selKey?: string
  focusSel: number
  onFocusSeg: (c: LiveChannel, s: Seg) => void
  onTapSeg: (c: LiveChannel, s: Seg) => void
  onWatch: (c: LiveChannel) => void
}) {
  return (
    <View style={st.row}>
      <View style={[st.chCell, active && st.chCellActive]}>
        <Text style={st.chNumber}>{channel.number}</Text>
        <ChannelLogo channel={channel} />
      </View>
      <View style={[st.track, { width: px(geo.gridW) }]}>
        <View style={[st.trackInner, { width: px((segs[segs.length - 1].stop - from) * geo.perSec), transform: [{ translateX: -px((viewStart - from) * geo.perSec) }] }]}>
          {segs.map((seg) => (
            <Cell
              key={seg.key}
              seg={seg}
              from={from}
              viewStart={viewStart}
              perSec={geo.perSec}
              airing={seg.start <= now && now < seg.stop}
              selected={selKey === seg.key}
              focusNow={selKey === seg.key ? focusSel : 0}
              onFocus={() => onFocusSeg(channel, seg)}
              onPress={() => (isPhone ? onTapSeg(channel, seg) : onWatch(channel))}
            />
          ))}
        </View>
      </View>
    </View>
  )
})

function FilterChip({ label, on, onPress }: { label: string; on: boolean; onPress: () => void }) {
  const [focused, setFocused] = useState(false)
  return (
    <Pressable
      onPress={onPress}
      onFocus={() => setFocused(!isPhone)}
      onBlur={() => setFocused(false)}
      style={({ pressed }) => [st.chip, on && st.chipOn, focused && st.chipFocused, pressed && st.chipPressed]}
    >
      <Text style={[st.chipText, on && st.chipTextOn, focused && st.chipTextFocused]} numberOfLines={1}>{label}</Text>
    </Pressable>
  )
}

/** The channel's logo (Android's Image can't draw SVG, so those go through react-native-svg); the name if there's none. */
function ChannelLogo({ channel }: { channel: LiveChannel }) {
  const [logo, setLogo] = useState<{ svg?: string; uri?: string } | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!channel.logo) return
    const uri = absolute(channel.logo) // the server's logo URL, versioned so a new logo isn't cached away
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
  if (logo?.svg) return <SvgXml xml={logo.svg} width={px(LOGO_W)} height={px(LOGO_H)} />
  if (logo?.uri) return <Image source={{ uri: logo.uri, headers: authHeaders() }} style={st.chLogo} resizeMode="contain" onError={() => setFailed(true)} />
  return <View style={st.chLogo} />
}

function Cell({
  seg,
  from,
  viewStart,
  perSec,
  airing,
  selected,
  focusNow,
  onFocus,
  onPress,
}: {
  seg: Seg
  from: number
  viewStart: number
  perSec: number
  airing: boolean
  selected: boolean
  focusNow: number // when it changes, take focus
  onFocus: () => void
  onPress: () => void
}) {
  const [focused, setFocused] = useState(false)
  const ref = useRef<View>(null)
  const remember = useContext(FocusMemoryContext)
  useEffect(() => {
    if (!focusNow) return
    // After the screen's own focus restore, which lands on the block last
    // focused rather than the channel flipped to.
    const t = setTimeout(() => (ref.current as FocusTarget | null)?.requestTVFocus?.(), 120)
    return () => clearTimeout(t)
  }, [focusNow])
  const lit = isPhone ? selected : focused // a TV lights the focused block; a phone the tapped one
  const left = (seg.start - from) * perSec
  const width = (seg.stop - seg.start) * perSec
  // Keep the title readable when the block starts left of the visible window.
  const inset = Math.max(0, Math.min(width - 60, (viewStart - seg.start) * perSec))
  const startedEarlier = seg.program && seg.program.start < seg.start
  return (
    <Pressable
      ref={ref}
      onFocus={() => {
        setFocused(true)
        remember(ref.current)
        onFocus()
      }}
      onBlur={() => setFocused(false)}
      onPress={onPress}
      style={[st.cell, airing && st.cellAiring, lit && st.cellFocused, { left: px(left), width: px(width) - px(ROW_GAP), paddingLeft: px(18 + inset) }]}
    >
      <Text style={[st.cellText, !seg.program && st.cellEmpty, lit && st.cellTextFocused]} numberOfLines={1}>
        {startedEarlier || inset > 0 ? '‹ ' : ''}
        {seg.program?.title ?? 'No guide information'}
      </Text>
    </Pressable>
  )
}

const LOGO_W = isPhone ? 100 : 150
const LOGO_H = isPhone ? 50 : 64

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  error: { color: colors.danger, fontSize: font.body, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
  header: { flexDirection: 'row', paddingHorizontal: gutter, paddingTop: isPhone ? 24 : 10, height: isPhone ? 260 : 300 },
  headerWide: { height: 190, paddingTop: 12 },
  previewWide: { width: 300, height: 169 },
  info: { flex: 1, paddingRight: isPhone ? 24 : 40 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800' },
  meta: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  subtitle: { color: colors.text, fontSize: font.body, marginTop: 10, fontWeight: '600' },
  desc: { color: colors.text, fontSize: font.small, marginTop: 10, lineHeight: 32, opacity: 0.85 },
  preview: { width: isPhone ? 300 : 480, height: isPhone ? 169 : 270, borderRadius: 16, overflow: 'hidden', backgroundColor: colors.surface, alignItems: 'center', justifyContent: 'center' },
  tunedTag: { position: 'absolute', left: 0, right: 0, bottom: 0, color: colors.text, backgroundColor: 'rgba(0,0,0,0.6)', fontSize: font.small, fontWeight: '700', paddingHorizontal: 12, paddingVertical: 4 },
  liveTag: { position: 'absolute', top: 12, left: 12, color: colors.accentText, backgroundColor: colors.accent, fontSize: 16, fontWeight: '900', paddingHorizontal: 10, paddingVertical: 3, borderRadius: 6, overflow: 'hidden' },
  filters: { flexGrow: 0, marginBottom: isPhone ? 6 : 4 },
  filtersRow: { paddingHorizontal: gutter, gap: 12, alignItems: 'center' },
  chip: { paddingHorizontal: 22, paddingVertical: 8, borderRadius: 999, backgroundColor: colors.surface, borderWidth: 2, borderColor: 'transparent' },
  chipOn: { borderColor: colors.accent },
  chipFocused: { backgroundColor: colors.accent },
  chipPressed: { opacity: 0.6 },
  chipText: { color: colors.muted, fontSize: font.small, fontWeight: '700' },
  chipTextOn: { color: colors.text },
  chipTextFocused: { color: colors.accentText },
  noneShown: { marginTop: 40 },
  ruler: { flexDirection: 'row', alignItems: 'flex-end', paddingLeft: gutter, height: 56, marginBottom: 10 },
  rulerDay: { width: CH_W, color: colors.muted, fontSize: font.small, paddingBottom: 14, paddingLeft: isPhone ? 12 : 24 },
  rulerTrack: { height: 56, overflow: 'hidden' },
  tick: { position: 'absolute', top: 6, color: colors.muted, fontSize: font.small },
  rulerLine: { position: 'absolute', left: 0, right: 0, bottom: 0, height: 2, backgroundColor: colors.surface3 },
  nowLine: { position: 'absolute', left: 0, bottom: 0, height: 4, backgroundColor: colors.accent },
  nowDot: { position: 'absolute', bottom: -4, width: 12, height: 12, borderRadius: 6, backgroundColor: colors.accent },
  rows: { paddingLeft: gutter, paddingBottom: 40 },
  row: { flexDirection: 'row', height: ROW_H, marginBottom: ROW_GAP },
  chCell: { width: CH_W - ROW_GAP, marginRight: ROW_GAP, flexDirection: 'row', alignItems: 'center', paddingHorizontal: isPhone ? 12 : 20, borderRadius: 10 },
  chCellActive: { backgroundColor: colors.surface2 },
  chNumber: { color: colors.text, fontSize: font.body, fontWeight: '700', width: isPhone ? 60 : 82 },
  chLogo: { width: LOGO_W, height: LOGO_H },
  chName: { color: colors.text, fontSize: font.small, fontWeight: '700', flex: 1 },
  track: { overflow: 'hidden' },
  trackInner: { height: ROW_H },
  cell: { position: 'absolute', top: 0, height: ROW_H, justifyContent: 'center', paddingRight: 14, backgroundColor: colors.surface, borderRadius: 10 },
  cellAiring: { backgroundColor: colors.surface3 },
  cellFocused: { backgroundColor: colors.accent },
  cellText: { color: colors.text, fontSize: font.body, fontWeight: '600' },
  cellEmpty: { color: colors.muted, fontWeight: '400' },
  cellTextFocused: { color: colors.accentText },
}))
