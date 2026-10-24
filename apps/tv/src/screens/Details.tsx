import React, { useCallback, useEffect, useState } from 'react'
import { ActivityIndicator, ImageBackground, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, image } from '../api'
import { Button, Center, Focusable, PosterCard } from '../components'
import { useNav } from '../nav'
import { scaled, colors, font, gutter } from '../theme'
import type { Item, ItemDetail } from '../types'

function fmtDuration(min?: number) {
  if (!min) return ''
  const h = Math.floor(min / 60)
  return h ? `${h}h ${min % 60}m` : `${min}m`
}

/** A movie, show, season or episode, with Play and its seasons/episodes. */
export default function Details({ id }: { id: number }) {
  const nav = useNav()
  const [d, setD] = useState<ItemDetail | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api.get<ItemDetail>(`/api/items/${id}`).then(setD, (e) => setError(e.message))
  }, [id])
  useEffect(load, [load])

  if (error) return <Center><Text style={st.error}>{error}</Text></Center>
  if (!d) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>

  const it = d.item
  const playable = it.kind === 'movie' || it.kind === 'episode'
  // Shows and seasons play their next unwatched episode.
  const target: Item | undefined = playable ? it : d.upNext
  const resume = target && !target.watch?.watched && target.watch?.position > 30 ? target.watch.position : 0
  const bg = image(it.backdrop || it.showBackdrop || d.show?.backdrop, 'w1280')
  const meta = [it.year, it.contentRating, fmtDuration(it.runtime), it.rating ? `★ ${it.rating.toFixed(1)}` : '', it.genres?.slice(0, 3).join(', ')]
    .filter(Boolean)
    .join('   ·   ')
  const heading = it.kind === 'episode' ? `${it.showTitle ?? d.show?.title ?? ''}` : it.kind === 'season' ? d.show?.title ?? '' : ''

  return (
    <ImageBackground source={bg ?? {}} style={st.screen} imageStyle={st.bgImg}>
      <ScrollView contentContainerStyle={st.body}>
        {!!heading && <Text style={st.heading}>{heading}</Text>}
        <Text style={st.title}>
          {it.kind === 'episode' ? `S${it.seasonNumber} E${it.episodeNumber} · ${it.title}` : it.title}
        </Text>
        {!!meta && <Text style={st.meta}>{meta}</Text>}
        {!!it.tagline && <Text style={st.tagline}>{it.tagline}</Text>}
        {!!it.overview && <Text style={st.overview} numberOfLines={5}>{it.overview}</Text>}
        <View style={st.buttons}>
          {target ? (
            <>
              <Button
                label={resume ? `Resume from ${Math.floor(resume / 60)}:${String(Math.floor(resume % 60)).padStart(2, '0')}` : playable ? 'Play' : `Play S${target.seasonNumber} E${target.episodeNumber}`}
                primary
                preferFocus
                onPress={() => nav.push({ name: 'player', id: target.id, start: resume })}
              />
              {resume > 0 && <Button label="From the start" onPress={() => nav.push({ name: 'player', id: target.id, start: 0 })} />}
            </>
          ) : (
            !d.children?.length && <Text style={st.muted}>Nothing to play here yet.</Text>
          )}
          {(it.kind === 'movie' || it.kind === 'show') && (
            <Button
              label={it.onWatchlist ? 'On your watchlist ✓' : 'Add to watchlist'}
              onPress={async () => {
                await (it.onWatchlist ? api.del(`/api/watchlist/${it.id}`) : api.post(`/api/watchlist/${it.id}`)).catch(() => undefined)
                load()
              }}
            />
          )}
          <Button
            label={it.watch?.watched || (it.childCount && it.unwatchedCount === 0) ? 'Mark unwatched' : 'Mark watched'}
            onPress={async () => {
              const watched = it.watch?.watched || (it.childCount && it.unwatchedCount === 0)
              await api.post(`/api/items/${it.id}/${watched ? 'unwatched' : 'watched'}`).catch(() => undefined)
              load()
            }}
          />
        </View>

        {it.kind === 'show' && !!d.children?.length && (
          <>
            <Text style={st.section}>Seasons</Text>
            <ScrollView horizontal contentContainerStyle={st.hlist}>
              {d.children.map((c) => (
                <PosterCard key={c.id} item={c} onPress={() => nav.push({ name: 'details', id: c.id })} preferFocus={!target && c === d.children![0]} />
              ))}
            </ScrollView>
          </>
        )}
        {it.kind === 'season' && !!d.children?.length && (
          <>
            <Text style={st.section}>Episodes</Text>
            {d.children.map((ep, i) => (
              <Focusable key={ep.id} onPress={() => nav.push({ name: 'details', id: ep.id })} style={st.episode} hasTVPreferredFocus={!target && i === 0}>
                <View style={st.epThumb}>
                  <WideCardImage item={ep} />
                </View>
                <View style={st.flex}>
                  <Text style={st.epTitle} numberOfLines={1}>
                    {ep.episodeNumber}. {ep.title} {ep.watch?.watched ? ' ✓' : ''}
                  </Text>
                  <Text style={st.epOverview} numberOfLines={2}>{ep.overview}</Text>
                </View>
              </Focusable>
            ))}
          </>
        )}
      </ScrollView>
    </ImageBackground>
  )
}

// A non-focusable thumbnail (the row around it takes focus).
function WideCardImage({ item }: { item: Item }) {
  const src = image(item.backdrop || item.poster, 'w342')
  return src ? <ImageBackground source={src} style={st.flex} imageStyle={st.thumbImg} /> : <View style={[st.flex, st.thumbFallback]} />
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg },
  bgImg: { opacity: 0.3 },
  flex: { flex: 1 },
  body: { paddingHorizontal: gutter * 1.5, paddingTop: 60, paddingBottom: 80 },
  heading: { color: colors.muted, fontSize: font.heading, fontWeight: '700', marginBottom: 6 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '900', maxWidth: 1600 },
  meta: { color: colors.muted, fontSize: font.body, marginTop: 10 },
  tagline: { color: colors.text, fontSize: font.body, fontStyle: 'italic', marginTop: 14, opacity: 0.85 },
  overview: { color: colors.text, fontSize: font.body, lineHeight: 40, marginTop: 16, maxWidth: 1500, opacity: 0.9 },
  buttons: { flexDirection: 'row', alignItems: 'center', marginTop: 30, marginBottom: 20 },
  muted: { color: colors.muted, fontSize: font.body, marginRight: 20 },
  error: { color: colors.danger, fontSize: font.body },
  section: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginTop: 20, marginBottom: 10 },
  hlist: { paddingVertical: 10 },
  episode: { flexDirection: 'row', alignItems: 'center', backgroundColor: 'rgba(20,24,29,0.85)', padding: 20, marginBottom: 16, maxWidth: 1600 },
  epThumb: { width: 320, height: 180, marginRight: 30 },
  thumbFallback: { backgroundColor: colors.surface3, borderRadius: 8 },
  thumbImg: { borderRadius: 8 },
  epTitle: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  epOverview: { color: colors.muted, fontSize: font.small, marginTop: 8, lineHeight: 30 },
}))
