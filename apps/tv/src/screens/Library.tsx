import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api } from '../api'
import { Button, Center, CollectionCard, Focusable, PosterCard, Row } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { Collection, HubRow, Item, Library } from '../types'

type Section = 'recommended' | 'library' | 'collections'
const sectionNames: Record<Section, string> = { recommended: 'Recommended', library: 'Library', collections: 'Collections' }
const PAGE = 120

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
  const [total, setTotal] = useState(-1)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const more = useCallback(
    (offset: number) => {
      setBusy(true)
      api.get<{ items: Item[]; total: number }>(`/api/libraries/${library.id}/items?sort=title&offset=${offset}&limit=${PAGE}`).then(
        (p) => {
          setItems((cur) => (offset === 0 ? p.items : [...cur, ...p.items]))
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
  if (error) return <Problem error={error} retry={() => more(items.length)} />
  if (total < 0) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>
  if (total === 0) return <Center><Text style={st.muted}>This library is empty.</Text></Center>
  return (
    <ScrollView style={st.flex} contentContainerStyle={st.grid}>
      {items.map((it, i) => (
        <View key={it.id}>
          <PosterCard
            item={it}
            onPress={() => nav.push({ name: 'details', id: it.id })}
            // Reaching the last rows loads the next page.
            onFocus={() => !busy && items.length < total && i >= items.length - 12 && more(items.length)}
          />
          <Text style={st.gridTitle} numberOfLines={1}>{it.title}</Text>
        </View>
      ))}
      {isPhone && items.length < total && <Button label="Load more" onPress={() => more(items.length)} />}
    </ScrollView>
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
  gridInner: { flexDirection: 'row', flexWrap: 'wrap' },
  gridTitle: { color: colors.text, fontSize: font.small, marginTop: 8, marginBottom: 30, width: 230 },
  collection: { paddingHorizontal: gutter, paddingTop: 50, paddingBottom: 60 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '900', marginBottom: 12 },
  overview: { color: colors.muted, fontSize: font.body, marginBottom: 30, maxWidth: 1500, lineHeight: 40 },
  error: { color: colors.danger, fontSize: font.body, marginBottom: 20, textAlign: 'center' },
  muted: { color: colors.muted, fontSize: font.body, textAlign: 'center' },
}))
