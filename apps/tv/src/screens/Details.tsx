import React, { useCallback, useContext, useEffect, useState } from 'react'
import { ActivityIndicator, ImageBackground, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, cached, image, writesDone } from '../api'
import { Button, Center, Focusable, PosterCard } from '../components'
import { ItemMenuContext, useNav, useScreen } from '../nav'
import { scaled, colors, font, gutter, isPhone, quietPress } from '../theme'
import type { Credit, Item, ItemDetail } from '../types'

function fmtDuration(min?: number) {
  if (!min) return ''
  const h = Math.floor(min / 60)
  return h ? `${h}h ${min % 60}m` : `${min}m`
}

/** A movie, show, season or episode, with Play and its seasons/episodes. */
export default function Details({ id }: { id: number }) {
  const nav = useNav()
  const menu = useContext(ItemMenuContext)
  const [d, setD] = useState<ItemDetail | null>(() => cached<ItemDetail>(`/api/items/${id}`) ?? null)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    writesDone()
      .then(() => api.get<ItemDetail>(`/api/items/${id}`))
      .then(setD, (e) => setError(e.message))
  }, [id])
  // Again each time Back comes here (from the player: the new resume point).
  const { returns } = useScreen()
  useEffect(load, [load, returns])

  if (error) return <Center><Text style={st.error}>{error}</Text></Center>
  if (!d) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>

  const it = d.item
  const playable = it.kind === 'movie' || it.kind === 'episode'
  // Shows and seasons play their next unwatched episode.
  const target: Item | undefined = playable ? it : d.upNext
  // A finished title being watched again resumes too; finishing resets the position.
  const resume = target && target.watch?.position > 30 ? target.watch.position : 0
  const bg = image(it.backdrop || it.showBackdrop || d.show?.backdrop, 'w1280')
  const meta = [it.year, it.contentRating, fmtDuration(it.runtime), it.rating ? `★ ${it.rating.toFixed(1)}` : '', it.genres?.slice(0, 3).join(', ')]
    .filter(Boolean)
    .join('   ·   ')
  // Older servers leave out unwatchedCount when it is 0.
  const fullyWatched = playable ? !!it.watch?.watched : !!it.childCount && !it.unwatchedCount
  // Seasons and episodes show their show's cast.
  const cast = ((it.kind === 'season' || it.kind === 'episode' ? d.show?.credits : it.credits) ?? []).filter((c) => c.kind === 'cast').slice(0, 30)
  const heading = it.kind === 'episode' ? `${it.showTitle ?? d.show?.title ?? ''}` : it.kind === 'season' ? d.show?.title ?? '' : ''

  return (
    <ImageBackground source={bg ?? {}} style={st.screen} imageStyle={st.bgImg}>
      <ScrollView contentContainerStyle={st.body}>
        {isPhone && (
          <Pressable android_disableSound={quietPress} onPress={nav.back} hitSlop={20} style={st.back}>
            <Text style={st.backText}>‹ Back</Text>
          </Pressable>
        )}
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
          {!!d.trailers?.length && (
            <Button label="Trailer" onPress={() => nav.push({ name: 'trailer', url: d.trailers![0].url, title: it.title })} />
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
            label={fullyWatched ? 'Mark unwatched' : 'Mark watched'}
            onPress={async () => {
              await api.post(`/api/items/${it.id}/${fullyWatched ? 'unwatched' : 'watched'}`).catch(() => undefined)
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
              <Focusable
                key={ep.id}
                onPress={() => nav.push({ name: 'details', id: ep.id })}
                onLongPress={() => menu(ep)} // the hold menu, to favorite an episode and the rest
                style={st.episode}
                hasTVPreferredFocus={!target && i === 0}
              >
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
        {cast.length > 0 && (
          <>
            <Text style={st.section}>Cast</Text>
            <ScrollView horizontal contentContainerStyle={st.hlist}>
              {cast.map((c, i) => (
                <PersonCard key={`${c.name}-${i}`} person={c} />
              ))}
            </ScrollView>
          </>
        )}
        {d.collections?.map(({ collection, items }) => (
          <React.Fragment key={collection.id}>
            {/* The row holds only the collection's titles; its name opens the whole collection. */}
            <Focusable onPress={() => nav.push({ name: 'collection', id: collection.id })} style={st.sectionLink}>
              <Text style={[st.section, st.sectionLinkText]}>{collection.name} ›</Text>
            </Focusable>
            <ScrollView horizontal contentContainerStyle={st.hlist}>
              {items.map((m) => (
                <PosterCard key={m.id} item={m} onPress={() => nav.push({ name: 'details', id: m.id })} />
              ))}
            </ScrollView>
          </React.Fragment>
        ))}
      </ScrollView>
    </ImageBackground>
  )
}

// An actor's photo, name and character. OK opens everything else they're in.
function PersonCard({ person }: { person: Credit }) {
  const nav = useNav()
  const src = image(person.photo, 'w185')
  return (
    <Focusable onPress={() => nav.push({ name: 'person', person: person.name })} style={st.person}>
      {src ? (
        <ImageBackground source={src} style={st.personImg} imageStyle={st.thumbImg} resizeMethod="resize" />
      ) : (
        <View style={[st.personImg, st.thumbFallback, st.initials]}>
          <Text style={st.initialsText}>{initials(person.name)}</Text>
        </View>
      )}
      <Text style={st.personName} numberOfLines={1}>{person.name}</Text>
      {!!person.role && <Text style={st.personRole} numberOfLines={1}>{person.role}</Text>}
    </Focusable>
  )
}

function initials(name: string) {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('')
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
  body: { paddingHorizontal: isPhone ? gutter : gutter * 1.5, paddingTop: isPhone ? 30 : 60, paddingBottom: 80 },
  back: { alignSelf: 'flex-start', marginBottom: 24 },
  backText: { color: colors.accent, fontSize: font.body, fontWeight: '700' },
  heading: { color: colors.muted, fontSize: font.heading, fontWeight: '700', marginBottom: 6 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '900', maxWidth: 1600 },
  meta: { color: colors.muted, fontSize: font.body, marginTop: 10 },
  tagline: { color: colors.text, fontSize: font.body, fontStyle: 'italic', marginTop: 14, opacity: 0.85 },
  overview: { color: colors.text, fontSize: font.body, lineHeight: 40, marginTop: 16, maxWidth: 1500, opacity: 0.9 },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginTop: 30, marginBottom: 20 },
  muted: { color: colors.muted, fontSize: font.body, marginRight: 20 },
  error: { color: colors.danger, fontSize: font.body },
  section: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginTop: 20, marginBottom: 10 },
  sectionLink: { alignSelf: 'flex-start', borderRadius: 10, paddingHorizontal: 10, marginLeft: -10, marginTop: 20, marginBottom: 10 },
  sectionLinkText: { marginTop: 0, marginBottom: 0 },
  hlist: { paddingVertical: 10 },
  episode: { flexDirection: 'row', alignItems: 'center', backgroundColor: 'rgba(20,24,29,0.85)', padding: 20, marginBottom: 16, maxWidth: 1600 },
  epThumb: { width: isPhone ? 240 : 320, height: isPhone ? 135 : 180, marginRight: isPhone ? 20 : 30 },
  thumbFallback: { backgroundColor: colors.surface3, borderRadius: 8 },
  thumbImg: { borderRadius: 8 },
  person: { width: isPhone ? 150 : 200, marginRight: isPhone ? 16 : 24, padding: 4 },
  personImg: { width: '100%', aspectRatio: 2 / 3 },
  initials: { alignItems: 'center', justifyContent: 'center' },
  initialsText: { color: colors.muted, fontSize: font.heading, fontWeight: '800' },
  personName: { color: colors.text, fontSize: font.small, fontWeight: '700', marginTop: 10 },
  personRole: { color: colors.muted, fontSize: font.small, marginTop: 2 },
  epTitle: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  epOverview: { color: colors.muted, fontSize: font.small, marginTop: 8, lineHeight: 30 },
}))
