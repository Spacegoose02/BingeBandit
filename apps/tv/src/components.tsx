import React, { useState } from 'react'
import { Image, Pressable, ScrollView, StyleSheet, Text, View, type PressableProps, type ViewStyle } from 'react-native'
import { image } from './api'
import { scaled, colors, font, gutter, isPhone } from './theme'
import type { Item } from './types'

/** A remote-friendly button: grows and lights up when focused. */
export function Button({
  label,
  onPress,
  primary,
  small,
  preferFocus,
  style,
}: {
  label: string
  onPress: () => void
  primary?: boolean
  small?: boolean
  preferFocus?: boolean
  style?: ViewStyle
}) {
  const [focused, setFocused] = useState(false)
  return (
    <Pressable
      onPress={onPress}
      onFocus={() => setFocused(!isPhone)} // phones can hold focus too, but show touch feedback instead
      onBlur={() => setFocused(false)}
      hasTVPreferredFocus={preferFocus}
      style={({ pressed }) => [
        s.button,
        small && s.buttonSmall,
        primary && s.buttonPrimary,
        focused && (primary ? s.buttonPrimaryFocused : s.buttonFocused),
        pressed && s.pressed,
        style,
      ]}
    >
      <Text style={[s.buttonText, small && s.buttonTextSmall, (primary || focused) && s.buttonTextDark]}>{label}</Text>
    </Pressable>
  )
}

/** Wraps anything focusable with a teal ring and a slight zoom. */
export function Focusable({ children, style, ...props }: PressableProps & { style?: ViewStyle; children: React.ReactNode }) {
  const [focused, setFocused] = useState(false)
  return (
    <Pressable
      {...props}
      onFocus={(e) => {
        setFocused(!isPhone)
        props.onFocus?.(e)
      }}
      onBlur={(e) => {
        setFocused(false)
        props.onBlur?.(e)
      }}
      style={({ pressed }) => [style, s.focusable, focused && s.focused, pressed && s.pressed]}
    >
      {children}
    </Pressable>
  )
}

function progress(it: Item): number {
  const w = it.watch
  if (!w || w.watched || !w.duration || w.position < 30) return 0
  return Math.min(1, w.position / w.duration)
}

export function PosterCard({ item, onPress, onFocus, preferFocus }: { item: Item; onPress: () => void; onFocus?: () => void; preferFocus?: boolean }) {
  const src = image(item.poster || item.showPoster, 'w342')
  const p = progress(item)
  return (
    <Focusable onPress={onPress} onFocus={onFocus} hasTVPreferredFocus={preferFocus} style={s.poster}>
      {src ? <Image source={src} style={s.posterImg} /> : <View style={[s.posterImg, s.fallback]}><Text style={s.fallbackText}>{item.title}</Text></View>}
      {item.watch?.watched && item.kind !== 'show' && <View style={s.badge}><Text style={s.badgeText}>✓</Text></View>}
      {!!item.unwatchedCount && <View style={s.badge}><Text style={s.badgeText}>{item.unwatchedCount}</Text></View>}
      {p > 0 && <View style={s.progressTrack}><View style={[s.progressFill, { width: `${p * 100}%` }]} /></View>}
    </Focusable>
  )
}

export function WideCard({ item, onPress, onFocus, preferFocus }: { item: Item; onPress: () => void; onFocus?: () => void; preferFocus?: boolean }) {
  const src = image(item.backdrop || item.showBackdrop || item.poster, 'w780')
  const p = progress(item)
  const label = item.kind === 'episode' ? `${item.showTitle} · S${item.seasonNumber}E${item.episodeNumber}` : item.title
  return (
    <View style={s.wideWrap}>
      <Focusable onPress={onPress} onFocus={onFocus} hasTVPreferredFocus={preferFocus} style={s.wide}>
        {src ? <Image source={src} style={s.wideImg} /> : <View style={[s.wideImg, s.fallback]} />}
        {p > 0 && <View style={s.progressTrack}><View style={[s.progressFill, { width: `${p * 100}%` }]} /></View>}
      </Focusable>
      <Text style={s.cardTitle} numberOfLines={1}>{label}</Text>
    </View>
  )
}

export function Row({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <View style={s.row}>
      <Text style={s.rowTitle}>{title}</Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.rowBody}>
        {children}
      </ScrollView>
    </View>
  )
}

export function Center({ children }: { children: React.ReactNode }) {
  return <View style={s.center}>{children}</View>
}

const s = StyleSheet.create(scaled({
  button: { paddingVertical: 22, paddingHorizontal: 44, borderRadius: 18, backgroundColor: colors.surface3, marginRight: 24, marginBottom: isPhone ? 20 : 0, alignItems: 'center' },
  buttonSmall: { paddingVertical: 14, paddingHorizontal: 30 },
  buttonPrimary: { backgroundColor: colors.accent },
  buttonFocused: { backgroundColor: colors.text, transform: [{ scale: 1.06 }] },
  buttonPrimaryFocused: { backgroundColor: '#5eead4', transform: [{ scale: 1.06 }] },
  buttonText: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  buttonTextSmall: { fontSize: font.small },
  buttonTextDark: { color: colors.accentText },
  focusable: { borderRadius: 16, borderWidth: 5, borderColor: 'transparent' },
  focused: { borderColor: colors.accent, transform: [{ scale: 1.07 }] },
  pressed: { opacity: 0.6 }, // touch feedback on phones
  poster: { width: 230, height: 345, marginRight: 30, overflow: 'hidden', backgroundColor: colors.surface2 },
  posterImg: { width: '100%', height: '100%', borderRadius: 11 },
  wideWrap: { marginRight: 34, width: 420 },
  wide: { width: 420, height: 236, overflow: 'hidden', backgroundColor: colors.surface2 },
  wideImg: { width: '100%', height: '100%', borderRadius: 11 },
  cardTitle: { color: colors.text, fontSize: font.small, marginTop: 12, fontWeight: '600' },
  fallback: { alignItems: 'center', justifyContent: 'center', padding: 16, backgroundColor: colors.surface3 },
  fallbackText: { color: colors.muted, fontSize: font.small, textAlign: 'center' },
  badge: { position: 'absolute', top: 12, right: 12, minWidth: 44, height: 44, borderRadius: 22, backgroundColor: colors.accent, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 8 },
  badgeText: { color: colors.accentText, fontWeight: '800', fontSize: 22 },
  progressTrack: { position: 'absolute', left: 14, right: 14, bottom: 14, height: 8, borderRadius: 4, backgroundColor: 'rgba(0,0,0,0.6)' },
  progressFill: { height: 8, borderRadius: 4, backgroundColor: colors.accent },
  row: { marginBottom: 40 },
  rowTitle: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginLeft: gutter, marginBottom: 6 },
  rowBody: { paddingHorizontal: gutter, paddingVertical: 20 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: gutter },
}))
