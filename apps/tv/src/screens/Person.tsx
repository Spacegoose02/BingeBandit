import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, Image, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, cached, image } from '../api'
import { Button, Center, PosterCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { Person } from '../types'

/** A cast or crew member, from a title's Cast row: everything in the libraries that credits them. */
export default function PersonScreen({ name }: { name: string }) {
  const nav = useNav()
  const path = `/api/people?name=${encodeURIComponent(name)}`
  const [p, setP] = useState<Person | null>(() => cached<Person>(path) ?? null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    setError('')
    api.get<Person>(path).then(setP, (e) => setError(e.message))
  }, [path])
  useEffect(load, [load])
  if (!p && error)
    return (
      <View style={st.screen}>
        <Center>
          <Text style={st.muted}>{error}</Text>
          <Button label="Back" preferFocus onPress={nav.back} />
        </Center>
      </View>
    )
  if (!p) return <View style={st.screen}><Center><ActivityIndicator color={colors.accent} size="large" /></Center></View>
  const src = image(p.photo, 'w342')
  const acting = p.titles.filter((t) => t.kind === 'cast')
  const crew = p.titles.filter((t) => t.kind === 'crew')
  const section = (title: string, list: Person['titles'], first: boolean) =>
    list.length > 0 && (
      <View>
        <Text style={st.section}>{title}</Text>
        <View style={st.grid}>
          {list.map((t, i) => (
            <View key={t.item.id} style={st.tile}>
              <PosterCard item={t.item} preferFocus={first && i === 0} onPress={() => nav.push({ name: 'details', id: t.item.id })} />
              <Text style={st.tileTitle} numberOfLines={1}>{t.item.title}</Text>
              {!!t.role && <Text style={st.tileRole} numberOfLines={1}>{t.kind === 'cast' ? `as ${t.role}` : t.role}</Text>}
            </View>
          ))}
        </View>
      </View>
    )
  return (
    <ScrollView style={st.screen} contentContainerStyle={st.body}>
      <View style={st.head}>
        {src ? <Image source={src} style={st.photo} resizeMethod="resize" /> : <View style={[st.photo, st.photoFallback]} />}
        <View style={st.headText}>
          <Text style={st.name}>{p.name}</Text>
          <Text style={st.muted}>{p.titles.length} {p.titles.length === 1 ? 'title' : 'titles'} in your libraries</Text>
        </View>
      </View>
      {section('Acting', acting, true)}
      {section(acting.length ? 'Behind the camera' : 'Credits', crew, acting.length === 0)}
    </ScrollView>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  body: { paddingHorizontal: isPhone ? gutter : gutter * 1.5, paddingTop: isPhone ? 30 : 60, paddingBottom: 80 },
  head: { flexDirection: 'row', alignItems: 'center', marginBottom: 20 },
  photo: { width: isPhone ? 140 : 200, aspectRatio: 2 / 3, borderRadius: 12, marginRight: 40 },
  photoFallback: { backgroundColor: colors.surface3 },
  headText: { flex: 1 },
  name: { color: colors.text, fontSize: font.hero, fontWeight: '900', marginBottom: 10 },
  muted: { color: colors.muted, fontSize: font.body, marginBottom: 20 },
  section: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginTop: 20, marginBottom: 16 },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  tile: { marginBottom: 30 },
  tileTitle: { color: colors.text, fontSize: font.small, marginTop: 8, width: 230 },
  tileRole: { color: colors.muted, fontSize: font.small, marginTop: 2, width: 230 },
}))
