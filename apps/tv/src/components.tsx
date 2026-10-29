import React, { memo, useCallback, useContext, useRef, useState } from 'react'
import { FlatList, Image, Pressable, ScrollView, StyleSheet, Text, View, type ImageProps, type PressableProps, type ViewStyle } from 'react-native'
import { image } from './api'
import { FocusMemoryContext, ItemMenuContext } from './nav'
import { scaled, colors, font, gutter, isPhone } from './theme'
import type { Collection, Item } from './types'

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
  const ref = useRef<View>(null)
  const remember = useContext(FocusMemoryContext)
  return (
    <Pressable
      ref={ref}
      onPress={onPress}
      onFocus={() => {
        setFocused(!isPhone) // phones can hold focus too, but show touch feedback instead
        remember(ref.current)
      }}
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
export function Focusable({
  children,
  style,
  viewRef,
  ...props
}: PressableProps & { style?: ViewStyle; viewRef?: (view: View | null) => void; children: React.ReactNode }) {
  const [focused, setFocused] = useState(false)
  const ref = useRef<View>(null)
  const remember = useContext(FocusMemoryContext)
  return (
    <Pressable
      {...props}
      ref={(v) => {
        ref.current = v
        viewRef?.(v)
      }}
      onFocus={(e) => {
        setFocused(!isPhone)
        remember(ref.current)
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
  // A finished title that's being watched again shows its new position, so
  // only the position decides (a finish resets it to 0 on the server).
  if (!w || !w.duration || w.position < 30) return 0
  return Math.min(1, w.position / w.duration)
}

/**
 * Artwork for cards. Android decodes at the size shown instead of the file's
 * size (local posters can be thousands of pixels tall) and skips the fade.
 */
function Art(props: ImageProps) {
  return <Image resizeMethod="resize" fadeDuration={120} {...props} />
}

/**
 * Keeps a card's handlers current without re-rendering it: screens pass new
 * arrow functions on every render (a focus move re-renders the whole grid),
 * so the memoized inner card only redraws when what it shows changes.
 */
function useLatest<A extends unknown[]>(fn: ((...a: A) => void) | undefined) {
  const ref = useRef(fn)
  ref.current = fn
  return useCallback((...a: A) => ref.current?.(...a), [])
}

interface CardProps {
  item: Item
  onPress: () => void
  onFocus?: () => void
  preferFocus?: boolean
}

export function PosterCard({ item, onPress, onFocus, preferFocus }: CardProps) {
  return <PosterCardView item={item} preferFocus={preferFocus} onPress={useLatest(onPress)} onFocus={useLatest(onFocus)} />
}

const PosterCardView = memo(function Poster({ item, onPress, onFocus, preferFocus }: CardProps) {
  const src = image(item.poster || item.showPoster, 'w342')
  const p = progress(item)
  const menu = useContext(ItemMenuContext)
  return (
    <Focusable onPress={onPress} onLongPress={() => menu(item)} onFocus={onFocus} hasTVPreferredFocus={preferFocus} style={s.poster}>
      {src ? <Art source={src} style={s.posterImg} /> : <View style={[s.posterImg, s.fallback]}><Text style={s.fallbackText}>{item.title}</Text></View>}
      {item.watch?.watched && item.kind !== 'show' && <View style={s.badge}><Text style={s.badgeText}>✓</Text></View>}
      {!!item.unwatchedCount && <View style={s.badge}><Text style={s.badgeText}>{item.unwatchedCount}</Text></View>}
      {p > 0 && <View style={s.progressTrack}><View style={[s.progressFill, { width: `${p * 100}%` }]} /></View>}
    </Focusable>
  )
})

/** A collection's poster with its name and size under it. */
export function CollectionCard({ collection, onPress, preferFocus }: { collection: Collection; onPress: () => void; preferFocus?: boolean }) {
  return <CollectionCardView collection={collection} preferFocus={preferFocus} onPress={useLatest(onPress)} />
}

const CollectionCardView = memo(function CollectionPoster({ collection, onPress, preferFocus }: { collection: Collection; onPress: () => void; preferFocus?: boolean }) {
  const src = image(collection.poster, 'w342')
  return (
    <View>
      <Focusable onPress={onPress} hasTVPreferredFocus={preferFocus} style={s.poster}>
        {src ? <Art source={src} style={s.posterImg} /> : <View style={[s.posterImg, s.fallback]}><Text style={s.fallbackText}>{collection.name}</Text></View>}
        <View style={s.badge}><Text style={s.badgeText}>{collection.itemCount}</Text></View>
      </Focusable>
      <Text style={[s.cardTitle, s.posterTitle]} numberOfLines={1}>{collection.name}</Text>
    </View>
  )
})

export function WideCard({ item, onPress, onFocus, preferFocus }: CardProps) {
  return <WideCardView item={item} preferFocus={preferFocus} onPress={useLatest(onPress)} onFocus={useLatest(onFocus)} />
}

const WideCardView = memo(function Wide({ item, onPress, onFocus, preferFocus }: CardProps) {
  const src = image(item.backdrop || item.showBackdrop || item.poster, 'w780')
  const p = progress(item)
  const label = item.kind === 'episode' ? `${item.showTitle} · S${item.seasonNumber}E${item.episodeNumber}` : item.title
  const menu = useContext(ItemMenuContext)
  return (
    <View style={s.wideWrap}>
      <Focusable onPress={onPress} onLongPress={() => menu(item)} onFocus={onFocus} hasTVPreferredFocus={preferFocus} style={s.wide}>
        {src ? <Art source={src} style={s.wideImg} /> : <View style={[s.wideImg, s.fallback]} />}
        {p > 0 && <View style={s.progressTrack}><View style={[s.progressFill, { width: `${p * 100}%` }]} /></View>}
      </Focusable>
      <Text style={s.cardTitle} numberOfLines={1}>{label}</Text>
    </View>
  )
})

/**
 * A titled row of items that only builds the cards near the screen, adding
 * more as focus moves along it, instead of every card of every row at once.
 * With onSeeAll (a collection's row), a blank "See all" poster ends it.
 */
export function ItemRow({ title, items, renderItem, onSeeAll }: {
  title: string
  items: Item[]
  renderItem: (it: Item, index: number) => React.ReactElement
  onSeeAll?: () => void
}) {
  return (
    <View style={s.row}>
      <Text style={s.rowTitle}>{title}</Text>
      <FlatList
        horizontal
        data={items}
        keyExtractor={(it) => String(it.id)}
        renderItem={({ item, index }) => renderItem(item, index)}
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={s.rowBody}
        initialNumToRender={8}
        maxToRenderPerBatch={6}
        windowSize={5}
        ListFooterComponent={onSeeAll && items.length > 0 ? <SeeAllCard onPress={onSeeAll} /> : null}
      />
    </View>
  )
}

/** The blank poster at the end of a collection's row that opens it. */
export function SeeAllCard({ onPress }: { onPress: () => void }) {
  return (
    <Focusable onPress={onPress} style={s.poster}>
      <View style={[s.posterImg, s.fallback, s.seeAll]}><Text style={s.seeAllText}>See all</Text></View>
    </Focusable>
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
  posterTitle: { width: 230, marginBottom: 30 },
  fallback: { alignItems: 'center', justifyContent: 'center', padding: 16, backgroundColor: colors.surface3 },
  fallbackText: { color: colors.muted, fontSize: font.small, textAlign: 'center' },
  seeAll: { backgroundColor: colors.surface2 },
  seeAllText: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  badge: { position: 'absolute', top: 12, right: 12, minWidth: 44, height: 44, borderRadius: 22, backgroundColor: colors.accent, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 8 },
  badgeText: { color: colors.accentText, fontWeight: '800', fontSize: 22 },
  progressTrack: { position: 'absolute', left: 14, right: 14, bottom: 14, height: 8, borderRadius: 4, backgroundColor: 'rgba(0,0,0,0.6)' },
  progressFill: { height: 8, borderRadius: 4, backgroundColor: colors.accent },
  row: { marginBottom: 40 },
  rowTitle: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginLeft: gutter, marginBottom: 6 },
  rowBody: { paddingHorizontal: gutter, paddingVertical: 20 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: gutter },
}))
