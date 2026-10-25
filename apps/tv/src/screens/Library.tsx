import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api } from '../api'
import { Button, Center, CollectionCard, Focusable, PosterCard, Row } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { Collection, HubRow, Item, Library } from '../types'

type Section = 'recommended' | 'library' | 'collections'
const sectionNames: Record<Section, string> = { recommended: 'Recommended', library: 'Library', collections: 'Collections' }
const PAGE = 120
const ALPHABET = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ']

/** Where titles starting with a letter begin, from the server ("#" is everything before A). */
interface Letter {
  letter: string
  offset: number
}

// Remembered per library so Back from details lands on the same section.
const lastSection: Record<number, Section> = {}

/** One pinned library: its Recommended rows, every title, and its collections. */
export default function LibraryTab({ library }: { library: Library }) {
  const [section, setSectionState] = useState<Section>(lastSection[library.id] ?? 'recommended')
  const setSection = (s: Section) => {
    lastSection[library.id] = s
    setSectionState(s)
  }
  return (
    <View style={st.flex}>
      <View style={st.sections}>
        {(Object.keys(sectionNames) as Section[]).map((s) => (
          <Focusable key={s} onPress={() => setSection(s)} onFocus={() => !isPhone && setSection(s)} style={section === s ? { ...st.chip, ...st.chipActive } : st.chip}>
            <Text style={[st.chipText, section === s && st.chipTextActive]}>{sectionNames[s]}</Text>
          </Focusable>
        ))}
      </View>
      {section === 'recommended' && <Recommended library={library} />}
      {section === 'library' && <AllTitles library={library} />}
      {section === 'collections' && <Collections library={library} />}
    </View>
  )
}

function useLoad<T>(path: string) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    setError('')
    api.get<T>(path).then(setData, (e) => setError(e.message))
  }, [path])
  useEffect(load, [load])
  return { data, error, load }
}

function Problem({ error, retry }: { error: string; retry: () => void }) {
  return (
    <Center>
      <Text style={st.error}>{error}</Text>
      <Button label="Try again" primary onPress={retry} />
    </Center>
  )
}

function Recommended({ library }: { library: Library }) {
  const nav = useNav()
  const { data, error, load } = useLoad<HubRow[]>(`/api/libraries/${library.id}/hubs`)
  if (error) return <Problem error={error} retry={load} />
  if (!data) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (data.length === 0) return <Center><Text style={st.muted}>Nothing to recommend yet. Library has every title.</Text></Center>
  return (
    <ScrollView style={st.flex} contentContainerStyle={st.rows}>
      {data.map((row) => (
        <Row key={row.id} title={row.title}>
          {row.items.map((it) => (
            <PosterCard key={it.id} item={it} onPress={() => nav.push({ name: 'details', id: it.id })} />
          ))}
        </Row>
      ))}
    </ScrollView>
  )
}

function AllTitles({ library }: { library: Library }) {
  const nav = useNav()
  const [items, setItems] = useState<Item[]>([])
  const [collections, setCollections] = useState<Collection[]>([])
  const [letters, setLetters] = useState<Letter[]>([])
  const [total, setTotal] = useState(-1)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // The title a letter jump lands on (focused and scrolled to), and the one focused now.
  const [target, setTarget] = useState(-1)
  const [focused, setFocused] = useState(-1)
  const scroller = useRef<ScrollView>(null)
  const starts = useRef<Record<number, number>>({}) // offset of a letter's first title → its y in the grid
  const more = useCallback(
    (offset: number) => {
      setBusy(true)
      api.get<{ items: Item[]; total: number; collections?: Collection[]; letters?: Letter[] }>(`/api/libraries/${library.id}/items?sort=title&offset=${offset}&limit=${PAGE}`).then(
        (p) => {
          setItems((cur) => (offset === 0 ? p.items : [...cur, ...p.items]))
          // The server sends collections unless the library hides them; they go first.
          if (offset === 0) {
            setCollections(p.collections ?? [])
            setLetters(p.letters ?? [])
          }
          setTotal(p.total)
          setBusy(false)
        },
        (e) => {
          setError(e.message)
          setBusy(false)
        },
      )
    },
    [library.id],
  )
  useEffect(() => more(0), [more])

  // Jumping to a letter loads every page up to it, then focuses its first title.
  const jump = async (l: Letter) => {
    if (busy) return
    let loaded = items
    if (loaded.length <= l.offset) {
      setBusy(true)
      try {
        while (loaded.length <= l.offset && loaded.length < total) {
          const p = await api.get<{ items: Item[] }>(`/api/libraries/${library.id}/items?sort=title&offset=${loaded.length}&limit=500`)
          if (!p.items.length) break
          loaded = [...loaded, ...p.items]
        }
        setItems(loaded)
      } catch (e) {
        setError((e as Error).message)
      } finally {
        setBusy(false)
      }
    }
    if (l.offset >= loaded.length) return
    // Clear first so picking the same letter again still moves focus there.
    setTarget(-1)
    requestAnimationFrame(() => setTarget(l.offset))
    const y = starts.current[l.offset]
    if (y !== undefined) scroller.current?.scrollTo({ y, animated: true })
  }

  if (error) return <Problem error={error} retry={() => more(items.length)} />
  if (total < 0) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (total === 0 && collections.length === 0) return <Center><Text style={st.muted}>This library is empty.</Text></Center>
  const starting = new Set(letters.map((l) => l.offset))
  const current = letters.filter((l) => l.offset <= Math.max(focused, 0)).pop()?.letter
  return (
    <View style={st.titles}>
      <ScrollView ref={scroller} style={st.flex} contentContainerStyle={st.grid}>
        {collections.map((c) => (
          <CollectionCard key={`c${c.id}`} collection={c} onPress={() => nav.push({ name: 'collection', id: c.id })} />
        ))}
        {items.map((it, i) => (
          <View
            key={it.id}
            // Only the first title of each letter is measured, for jumping there.
            onLayout={
              starting.has(i)
                ? (e) => {
                    starts.current[i] = e.nativeEvent.layout.y
                    if (i === target) scroller.current?.scrollTo({ y: e.nativeEvent.layout.y, animated: true })
                  }
                : undefined
            }
          >
            <PosterCard
              item={it}
              preferFocus={i === target}
              onPress={() => nav.push({ name: 'details', id: it.id })}
              onFocus={() => {
                setFocused(i)
                // Reaching the last rows loads the next page.
                if (!busy && items.length < total && i >= items.length - 12) more(items.length)
              }}
            />
            <Text style={st.gridTitle} numberOfLines={1}>{it.title}</Text>
          </View>
        ))}
        {isPhone && items.length < total && <Button label="Load more" onPress={() => more(items.length)} />}
      </ScrollView>
      {letters.length > 1 && <LetterBar letters={letters} current={current} onPick={jump} />}
    </View>
  )
}

/**
 * Plex-style A–Z column beside the grid. Right from the last poster in a row
 * reaches it; up and down move between letters (missing ones are skipped);
 * OK jumps there.
 */
function LetterBar({ letters, current, onPick }: { letters: Letter[]; current?: string; onPick: (l: Letter) => void }) {
  return (
    <View style={st.bar}>
      {ALPHABET.map((ch) => {
        const l = letters.find((x) => x.letter === ch)
        if (!l) {
          return <View key={ch} style={st.letter}><Text style={[st.letterText, st.letterMissing]}>{ch}</Text></View>
        }
        return <LetterButton key={ch} letter={l} active={ch === current} onPick={onPick} />
      })}
    </View>
  )
}

function LetterButton({ letter, active, onPick }: { letter: Letter; active: boolean; onPick: (l: Letter) => void }) {
  const [focused, setFocused] = useState(false)
  return (
    <Pressable
      onPress={() => onPick(letter)}
      onFocus={() => setFocused(!isPhone)}
      onBlur={() => setFocused(false)}
      style={[st.letter, focused && st.letterFocused]}
      accessibilityLabel={letter.letter === '#' ? 'Numbers and symbols' : letter.letter}
    >
      <Text style={[st.letterText, active && st.letterActive, focused && st.letterTextFocused]}>{letter.letter}</Text>
    </Pressable>
  )
}

function Collections({ library }: { library: Library }) {
  const nav = useNav()
  const { data, error, load } = useLoad<Collection[]>(`/api/libraries/${library.id}/collections`)
  if (error) return <Problem error={error} retry={load} />
  if (!data) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  const shown = data.filter((c) => c.itemCount > 0)
  if (shown.length === 0) return <Center><Text style={st.muted}>No collections in {library.name} yet.</Text></Center>
  return (
    <ScrollView style={st.flex} contentContainerStyle={st.grid}>
      {shown.map((c) => (
        <CollectionCard key={c.id} collection={c} onPress={() => nav.push({ name: 'collection', id: c.id })} />
      ))}
    </ScrollView>
  )
}

/** A collection's titles, pushed from a library's Collections section. */
export function CollectionScreen({ id }: { id: number }) {
  const nav = useNav()
  const { data, error, load } = useLoad<{ collection: Collection; items: Item[] }>(`/api/collections/${id}`)
  if (error) return <View style={st.screen}><Problem error={error} retry={load} /></View>
  if (!data) return <View style={st.screen}><Center><ActivityIndicator color={colors.accent} size="large" /></Center></View>
  return (
    <ScrollView style={st.screen} contentContainerStyle={st.collection}>
      <Text style={st.title}>{data.collection.name}</Text>
      {!!data.collection.overview && <Text style={st.overview} numberOfLines={3}>{data.collection.overview}</Text>}
      <View style={st.gridInner}>
        {data.items.map((it, i) => (
          <View key={it.id}>
            <PosterCard item={it} preferFocus={i === 0} onPress={() => nav.push({ name: 'details', id: it.id })} />
            <Text style={st.gridTitle} numberOfLines={1}>{it.title}</Text>
          </View>
        ))}
      </View>
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  screen: { flex: 1, backgroundColor: colors.bg },
  sections: { flexDirection: 'row', paddingHorizontal: gutter, paddingTop: 10, paddingBottom: 10 },
  chip: { paddingHorizontal: 30, paddingVertical: 12, borderRadius: 40, marginRight: 16, backgroundColor: colors.surface2 },
  chipActive: { backgroundColor: colors.surface3 },
  chipText: { color: colors.muted, fontSize: font.body, fontWeight: '700' },
  chipTextActive: { color: colors.text },
  rows: { paddingBottom: 60, paddingTop: 10 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', paddingHorizontal: gutter, paddingTop: 20, paddingBottom: 60 },
  titles: { flex: 1, flexDirection: 'row' },
  bar: { justifyContent: 'center', alignItems: 'center', paddingVertical: 16, paddingRight: isPhone ? 8 : 30, paddingLeft: 4 },
  letter: { width: 44, flex: 1, maxHeight: 34, minHeight: 20, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  letterFocused: { backgroundColor: colors.accent, transform: [{ scale: 1.3 }] },
  letterText: { color: colors.muted, fontSize: 20, fontWeight: '800' },
  letterTextFocused: { color: colors.accentText },
  letterActive: { color: colors.accent },
  letterMissing: { opacity: 0.25 },
  gridInner: { flexDirection: 'row', flexWrap: 'wrap' },
  gridTitle: { color: colors.text, fontSize: font.small, marginTop: 8, marginBottom: 30, width: 230 },
  collection: { paddingHorizontal: gutter, paddingTop: 50, paddingBottom: 60 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '900', marginBottom: 12 },
  overview: { color: colors.muted, fontSize: font.body, marginBottom: 30, maxWidth: 1500, lineHeight: 40 },
  error: { color: colors.danger, fontSize: font.body, marginBottom: 20, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
}))
