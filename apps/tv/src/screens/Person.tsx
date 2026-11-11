import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, Image, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, cached, image } from '../api'
import { Button, Center, Focusable, PosterCard } from '../components'
import { useNav } from '../nav'
import { requestLabel } from '../requests'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { Filmography, FilmographyCredit, Person, TitleRequest } from '../types'

type Filter = 'all' | 'movie' | 'show' | 'crew' | 'missing'

const filters: [Filter, string][] = [
  ['all', 'All'],
  ['movie', 'Movies'],
  ['show', 'Shows'],
  ['crew', 'Behind the camera'],
  ['missing', 'Not here'],
]

/** How many filmography posters show before "Show all" (TV boxes are slow to lay out hundreds). */
const firstPage = 40

/**
 * A cast or crew member, from a title's Cast row: everything in the libraries
 * that credits them, then everything they were in (from TMDB), with what's
 * here to watch and what isn't to request.
 */
export default function PersonScreen({ name }: { name: string }) {
  const nav = useNav()
  const q = encodeURIComponent(name)
  const path = `/api/people?name=${q}`
  const filmPath = `/api/people/filmography?name=${q}`
  const [p, setP] = useState<Person | null>(() => cached<Person>(path) ?? null)
  const [error, setError] = useState('')
  const [film, setFilm] = useState<Filmography | null>(() => cached<Filmography>(filmPath) ?? null)
  const [filmDone, setFilmDone] = useState(false)
  const load = useCallback(() => {
    setError('')
    api.get<Person>(path).then(setP, (e) => setError(e.message))
    // No TMDB key, or TMDB doesn't know them: just the library part.
    api
      .get<Filmography>(filmPath)
      .then(setFilm, () => undefined)
      .finally(() => setFilmDone(true))
  }, [path, filmPath])
  useEffect(load, [load])
  if (!p && error && filmDone && !film)
    return (
      <View style={st.screen}>
        <Center>
          <Text style={st.muted}>{error}</Text>
          <Button label="Back" preferFocus onPress={nav.back} />
        </Center>
      </View>
    )
  if (!p && !(error && film)) return <View style={st.screen}><Center><ActivityIndicator color={colors.accent} size="large" /></Center></View>
  const titles = p?.titles ?? []
  const src = image(film?.photo || p?.photo, 'w342')
  const acting = titles.filter((t) => t.kind === 'cast')
  const crew = titles.filter((t) => t.kind === 'crew')
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
  const facts = [
    film?.knownFor,
    film?.birthday && `Born ${longDate(film.birthday)}${film.birthplace ? ` in ${film.birthplace}` : ''}`,
    film?.deathday && `Died ${longDate(film.deathday)}`,
  ].filter(Boolean)
  const split = acting.length > 0 && crew.length > 0
  return (
    <ScrollView style={st.screen} contentContainerStyle={st.body}>
      <View style={st.head}>
        {src ? <Image source={src} style={st.photo} resizeMethod="resize" /> : <View style={[st.photo, st.photoFallback]} />}
        <View style={st.headText}>
          <Text style={st.name}>{film?.name || p?.name || name}</Text>
          {facts.length > 0 && <Text style={st.facts} numberOfLines={2}>{facts.join(' · ')}</Text>}
          <Text style={st.muted}>
            {titles.length} {titles.length === 1 ? 'title' : 'titles'} in your libraries
            {film ? ` · ${film.credits.length} in all` : ''}
          </Text>
          {!!film?.biography && <Text style={st.bio} numberOfLines={isPhone ? 6 : 4}>{film.biography}</Text>}
        </View>
      </View>
      {split ? (
        <>
          {section('In your libraries', acting, true)}
          {section('Behind the camera', crew, false)}
        </>
      ) : (
        section('In your libraries', titles, true)
      )}
      {film && film.credits.length > 0 && <FilmographySection film={film} focusFirst={titles.length === 0} />}
      {!filmDone && <Text style={st.muted}>Looking up the rest of {name}’s work…</Text>}
    </ScrollView>
  )
}

function longDate(d: string) {
  return new Date(d + 'T12:00:00').toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
}

/** Everything the person was in, newest first, with filters. */
function FilmographySection({ film, focusFirst }: { film: Filmography; focusFirst: boolean }) {
  const nav = useNav()
  const [filter, setFilter] = useState<Filter>('all')
  const [all, setAll] = useState(false)
  const [asked, setAsked] = useState<Record<string, TitleRequest>>({})
  const [error, setError] = useState('')
  const count = (f: Filter) =>
    film.credits.filter((t) =>
      f === 'all' ? true : f === 'crew' ? t.crew : f === 'missing' ? !t.itemId : !t.crew && t.kind === f,
    ).length
  const shown = film.credits.filter((t) =>
    filter === 'all' ? true : filter === 'crew' ? t.crew : filter === 'missing' ? !t.itemId : !t.crew && t.kind === filter,
  )
  const ask = (t: FilmographyCredit) => {
    // Anyone can add their name to someone else's request; it moves it up the list.
    if (t.request && (t.request.mine || t.request.status === 'available')) return
    const { kind, tmdbId, title, year, poster, overview } = t
    setError('')
    api.post<TitleRequest>('/api/requests', { kind, tmdbId, title, year, poster, overview }).then(
      (req) => setAsked((a) => ({ ...a, [`${kind}:${tmdbId}`]: req })),
      (e: Error) => setError(e.message),
    )
  }
  const list = all ? shown : shown.slice(0, firstPage)
  return (
    <View>
      <Text style={st.section}>Filmography</Text>
      <View style={st.filters}>
        {filters
          .filter(([k]) => k === 'all' || count(k) > 0)
          .map(([k, label], i) => (
            <Button
              key={k}
              small
              primary={filter === k}
              preferFocus={focusFirst && i === 0}
              label={`${label} ${count(k)}`}
              onPress={() => {
                setFilter(k)
                setAll(false)
              }}
            />
          ))}
      </View>
      {!!error && <Text style={st.error}>{error}</Text>}
      <View style={st.grid}>
        {list.map((t) => {
          const key = `${t.kind}:${t.tmdbId}`
          const credit = asked[key] ? { ...t, request: asked[key] } : t
          return (
            <FilmTile
              key={key}
              t={credit}
              canRequest={film.canRequest}
              onPress={() => (t.itemId ? nav.push({ name: 'details', id: t.itemId }) : film.canRequest && ask(credit))}
            />
          )
        })}
      </View>
      {!all && shown.length > list.length && <Button label={`Show all ${shown.length}`} onPress={() => setAll(true)} />}
    </View>
  )
}

/** One title of a filmography: opens it when it's here; asks for it when it isn't. */
function FilmTile({ t, canRequest, onPress }: { t: FilmographyCredit; canRequest: boolean; onPress: () => void }) {
  const src = image(t.poster, 'w342')
  const role = t.role ? (t.crew ? t.role : `as ${t.role}`) : ''
  const sub = [t.year || 'Coming', t.kind === 'show' ? (t.episodes ? `Show · ${t.episodes} ep` : 'Show') : 'Movie'].join(' · ')
  const status = t.itemId
    ? 'In your libraries'
    : t.onServer && t.kind === 'movie'
      ? 'On the server'
      : canRequest
        ? requestLabel(t)
        : 'Not here'
  return (
    <View style={st.tile}>
      <Focusable onPress={onPress} style={st.poster}>
        {src ? <Image source={src} style={[st.posterImg, !t.itemId && st.dim]} resizeMethod="resize" /> : <Text style={st.fallback}>{t.title}</Text>}
        {!!t.itemId && <View style={st.here}><Text style={st.hereText}>✓ Here</Text></View>}
      </Focusable>
      <Text style={st.tileTitle} numberOfLines={1}>{t.title}</Text>
      <Text style={st.tileRole} numberOfLines={1}>{role || sub}</Text>
      <Text style={[st.tileRole, (!!t.itemId || t.request?.mine) && st.accent]} numberOfLines={1}>{role ? `${t.year || 'Coming'} · ${status}` : status}</Text>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  body: { paddingHorizontal: isPhone ? gutter : gutter * 1.5, paddingTop: isPhone ? 30 : 60, paddingBottom: 80 },
  head: { flexDirection: 'row', alignItems: 'flex-start', marginBottom: 20 },
  photo: { width: isPhone ? 140 : 200, aspectRatio: 2 / 3, borderRadius: 12, marginRight: 40 },
  photoFallback: { backgroundColor: colors.surface3 },
  headText: { flex: 1 },
  name: { color: colors.text, fontSize: font.hero, fontWeight: '900', marginBottom: 10 },
  facts: { color: colors.text, fontSize: font.small, marginBottom: 8 },
  muted: { color: colors.muted, fontSize: font.body, marginBottom: 20 },
  bio: { color: colors.muted, fontSize: font.small, lineHeight: isPhone ? 34 : 32, maxWidth: 1400 },
  error: { color: colors.danger, fontSize: font.small, marginBottom: 16 },
  section: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginTop: 20, marginBottom: 16 },
  filters: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginBottom: 24 },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  tile: { marginBottom: 30 },
  poster: { width: 230, height: 345, marginRight: 30, borderRadius: 11, overflow: 'hidden', backgroundColor: colors.surface2, justifyContent: 'center' },
  posterImg: { width: '100%', height: '100%', borderRadius: 11 },
  dim: { opacity: 0.7 },
  fallback: { color: colors.muted, fontSize: font.small, textAlign: 'center', padding: 12 },
  here: { position: 'absolute', top: 10, left: 10, backgroundColor: colors.accent, borderRadius: 999, paddingHorizontal: 12, paddingVertical: 3 },
  hereText: { color: colors.accentText, fontSize: font.small - 4, fontWeight: '800' },
  tileTitle: { color: colors.text, fontSize: font.small, marginTop: 8, width: 230 },
  tileRole: { color: colors.muted, fontSize: font.small, marginTop: 2, width: 230 },
  accent: { color: colors.accent },
}))
