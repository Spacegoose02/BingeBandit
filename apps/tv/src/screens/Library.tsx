import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, FlatList, Pressable, ScrollView, StyleSheet, Text, View, useWindowDimensions } from 'react-native'
import { api, cached } from '../api'
import { Button, Center, CollectionCard, Focusable, ItemRow, PosterCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter, isPhone, px, units } from '../theme'
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

// Grid tiles: a 230-wide poster plus its gap, and a row with the title under it.
const TILE_W = 260
const ROW_H = 435
const BAR_W = isPhone ? 56 : 78

// Remembered per library so Back from details lands on the same section.
const lastSection: Record<number, Section> = {}

/** Opening a library from the menu starts on Recommended again, like Plex. */
export function forgetSection(libraryId: number) {
  delete lastSection[libraryId]
}

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
          // Only OK switches: moving down from the menu lands on whichever
          // chip is below it, and the library should stay on Recommended.
          <Focusable key={s} onPress={() => setSection(s)} style={section === s ? { ...st.chip, ...st.chipActive } : st.chip}>
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
  // Draws the last copy at once (Back from details, or passing through the menu), then refreshes.
  const [data, setData] = useState<T | null>(() => cached<T>(path) ?? null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    setError('')
    api.get<T>(path).then(setData, (e) => setError(e.message))
  }, [path])
  useEffect(load, [load])
  return { data, error: data ? '' : error, load }
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
        <ItemRow key={row.id} title={row.title} items={row.items} renderItem={(it) => <PosterCard item={it} onPress={() => nav.push({ name: 'details', id: it.id })} />} />
      ))}
    </ScrollView>
  )
}

type Tile = { item: Item; index: number } | { collection: Collection }

/** How many tiles fit across, leaving room for the gutters (and the A–Z bar). */
function useColumns(besideBar: boolean) {
  const { width } = useWindowDimensions()
  return Math.max(2, Math.floor((units(width) - gutter * 2 - (besideBar ? BAR_W : 0)) / TILE_W))
}

function chunk<T>(list: T[], size: number): T[][] {
  const rows: T[][] = []
  for (let i = 0; i < list.length; i += size) rows.push(list.slice(i, i + size))
  return rows
}

/**
 * A poster grid that only draws the rows near the screen, so a library of
 * thousands of titles costs no more than a screenful. Rows have a fixed
 * height, which lets a letter jump scroll straight to any row.
 */
function Grid<T>({ rows, keyOf, renderTile, onEnd, busy, header, listRef }: {
  rows: T[][]
  keyOf: (t: T) => string
  renderTile: (t: T) => React.ReactElement
  onEnd?: () => void
  busy?: boolean
  header?: React.ReactElement
  listRef?: React.Ref<FlatList<T[]>>
}) {
  return (
    <FlatList
      ref={listRef}
      style={st.flex}
      data={rows}
      keyExtractor={(row) => keyOf(row[0])}
      renderItem={({ item: row }) => <View style={st.gridRow}>{row.map((t) => <React.Fragment key={keyOf(t)}>{renderTile(t)}</React.Fragment>)}</View>}
      // Offsets only line up without a header above the rows.
      getItemLayout={header ? undefined : (_, i) => ({ length: px(ROW_H), offset: px(ROW_H) * i, index: i })}
      ListHeaderComponent={header}
      ListFooterComponent={busy ? <ActivityIndicator color={colors.accent} style={st.footer} /> : null}
      contentContainerStyle={st.grid}
      initialNumToRender={4}
      maxToRenderPerBatch={3}
      windowSize={5}
      // Clipped rows lose D-pad focus on Android TV.
      removeClippedSubviews={false}
      onEndReached={onEnd}
      onEndReachedThreshold={1.5}
    />
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
  const busyRef = useRef(false)
  // The title a letter jump lands on (focused and scrolled to), and the one focused now.
  const [target, setTarget] = useState(-1)
  const [focused, setFocused] = useState(-1)
  const list = useRef<FlatList<Tile[]>>(null)
  const cols = useColumns(true)
  const more = useCallback(
    (offset: number) => {
      if (offset > 0 && busyRef.current) return
      busyRef.current = true
      setBusy(true)
      api.get<{ items: Item[]; total: number; collections?: Collection[]; letters?: Letter[] }>(`/api/libraries/${library.id}/items?sort=title&offset=${offset}&limit=${PAGE}`).then(
        (p) => {
          setItems((cur) => {
            if (offset === 0) return p.items
            const have = new Set(cur.map((it) => it.id))
            return [...cur, ...p.items.filter((it) => !have.has(it.id))]
          })
          // The server sends collections unless the library hides them; they go first.
          if (offset === 0) {
            setCollections(p.collections ?? [])
            setLetters(p.letters ?? [])
          }
          setTotal(p.total)
          busyRef.current = false
          setBusy(false)
        },
        (e) => {
          setError(e.message)
          busyRef.current = false
          setBusy(false)
        },
      )
    },
    [library.id],
  )
  useEffect(() => more(0), [more])

  // Jumping to a letter loads every page up to it, then focuses its first title.
  const jump = async (l: Letter) => {
    if (busyRef.current) return
    let loaded = items
    if (loaded.length <= l.offset) {
      busyRef.current = true
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
        busyRef.current = false
        setBusy(false)
      }
    }
    if (l.offset >= loaded.length) return
    // Clear first so picking the same letter again still moves focus there.
    setTarget(-1)
    requestAnimationFrame(() => {
      list.current?.scrollToIndex({ index: Math.floor((collections.length + l.offset) / cols), animated: true })
      setTarget(l.offset)
    })
  }

  if (error) return <Problem error={error} retry={() => { setError(''); more(items.length) }} />
  if (total < 0) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (total === 0 && collections.length === 0) return <Center><Text style={st.muted}>This library is empty.</Text></Center>
  const current = letters.filter((l) => l.offset <= Math.max(focused, 0)).pop()?.letter
  const tiles: Tile[] = [...collections.map((collection) => ({ collection })), ...items.map((item, index) => ({ item, index }))]
  return (
    <View style={st.titles}>
      <Grid
        listRef={list}
        rows={chunk(tiles, cols)}
        keyOf={(t) => ('item' in t ? String(t.item.id) : `c${t.collection.id}`)}
        busy={busy && items.length > 0}
        onEnd={() => items.length < total && more(items.length)}
        renderTile={(t) =>
          'collection' in t ? (
            <CollectionCard collection={t.collection} onPress={() => nav.push({ name: 'collection', id: t.collection.id })} />
          ) : (
            <View>
              <PosterCard
                item={t.item}
                preferFocus={t.index === target}
                onPress={() => nav.push({ name: 'details', id: t.item.id })}
                onFocus={() => {
                  setFocused(t.index)
                  // Reaching the last rows loads the next page.
                  if (items.length < total && t.index >= items.length - cols * 3) more(items.length)
                }}
              />
              <Text style={st.gridTitle} numberOfLines={1}>{t.item.title}</Text>
            </View>
          )
        }
      />
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
  const cols = useColumns(false)
  const { data, error, load } = useLoad<Collection[]>(`/api/libraries/${library.id}/collections`)
  if (error) return <Problem error={error} retry={load} />
  if (!data) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  const shown = data.filter((c) => c.itemCount > 0)
  if (shown.length === 0) return <Center><Text style={st.muted}>No collections in {library.name} yet.</Text></Center>
  return (
    <Grid
      rows={chunk(shown, cols)}
      keyOf={(c) => String(c.id)}
      renderTile={(c) => <CollectionCard collection={c} onPress={() => nav.push({ name: 'collection', id: c.id })} />}
    />
  )
}

/** A collection's titles, pushed from a library's Collections section; big ones load a page at a time. */
export function CollectionScreen({ id }: { id: number }) {
  const nav = useNav()
  const cols = useColumns(false)
  const path = `/api/collections/${id}?limit=${PAGE}`
  const { data, error, load } = useLoad<{ collection: Collection; items: Item[]; total?: number }>(path)
  const [more, setMore] = useState<Item[]>([])
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const ended = useRef(false)
  useEffect(() => {
    setMore([])
    ended.current = false
  }, [data])
  if (error) return <View style={st.screen}><Problem error={error} retry={load} /></View>
  if (!data) return <View style={st.screen}><Center><ActivityIndicator color={colors.accent} size="large" /></Center></View>
  const items = [...data.items, ...more]
  const total = data.total ?? data.items.length
  const next = () => {
    if (busyRef.current || ended.current || items.length >= total) return
    busyRef.current = true
    setBusy(true)
    api.get<{ items: Item[] }>(`/api/collections/${id}?offset=${items.length}&limit=${PAGE}`).then(
      (p) => {
        if (!p.items.length) ended.current = true
        const have = new Set(items.map((it) => it.id))
        setMore((m) => [...m, ...p.items.filter((it) => !have.has(it.id))])
      },
      () => {
        ended.current = true
      },
    ).finally(() => {
      busyRef.current = false
      setBusy(false)
    })
  }
  const indexed = items.map((item, index) => ({ item, index }))
  return (
    <View style={st.screen}>
      <Grid
        rows={chunk(indexed, cols)}
        keyOf={(t) => String(t.item.id)}
        busy={busy}
        onEnd={next}
        header={
          <View style={st.collectionHead}>
            <Text style={st.title}>{data.collection.name}</Text>
            {!!data.collection.overview && <Text style={st.overview} numberOfLines={3}>{data.collection.overview}</Text>}
          </View>
        }
        renderTile={(t) => (
          <View>
            <PosterCard item={t.item} preferFocus={t.index === 0} onPress={() => nav.push({ name: 'details', id: t.item.id })} onFocus={() => t.index >= items.length - cols * 3 && next()} />
            <Text style={st.gridTitle} numberOfLines={1}>{t.item.title}</Text>
          </View>
        )}
      />
    </View>
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
  grid: { paddingHorizontal: gutter, paddingTop: 20, paddingBottom: 60 },
  gridRow: { flexDirection: 'row', height: ROW_H },
  footer: { marginVertical: 20 },
  titles: { flex: 1, flexDirection: 'row' },
  bar: { justifyContent: 'center', alignItems: 'center', paddingVertical: 16, paddingRight: isPhone ? 8 : 30, paddingLeft: 4 },
  letter: { width: 44, flex: 1, maxHeight: 34, minHeight: 20, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  letterFocused: { backgroundColor: colors.accent, transform: [{ scale: 1.3 }] },
  letterText: { color: colors.muted, fontSize: 20, fontWeight: '800' },
  letterTextFocused: { color: colors.accentText },
  letterActive: { color: colors.accent },
  letterMissing: { opacity: 0.25 },
  gridTitle: { color: colors.text, fontSize: font.small, marginTop: 8, marginBottom: 30, width: 230 },
  collectionHead: { paddingTop: 30 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '900', marginBottom: 12 },
  overview: { color: colors.muted, fontSize: font.body, marginBottom: 30, maxWidth: 1500, lineHeight: 40 },
  error: { color: colors.danger, fontSize: font.body, marginBottom: 20, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
}))
