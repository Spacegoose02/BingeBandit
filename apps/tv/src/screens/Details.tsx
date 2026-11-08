import React, { useCallback, useContext, useEffect, useState } from 'react'
import { ActivityIndicator, ImageBackground, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, cached, image, writesDone } from '../api'
import { Button, Center, Focusable, PosterCard } from '../components'
import { ItemMenuContext, useNav, useScreen } from '../nav'
import { scaled, colors, font, gutter, isPhone, quietPress } from '../theme'
import type { Credit, Item, ItemDetail, MediaFile, Quality } from '../types'
import { addDownloads, downloaded, downloadFor, downloadsSupported, estimateBytes, formatBytes, useDownloads } from '../downloads'
import { canDirectPlay } from './Player'

/** A resume point as a clock: 4:05, or 1:35:12 past an hour. */
function fmtClock(t: number) {
  t = Math.floor(t)
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = String(t % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

function fmtDuration(min?: number) {
  if (!min) return ''
  const h = Math.floor(min / 60)
  return h ? `${h}h ${min % 60}m` : `${min}m`
}

const resolutionText: Record<string, string> = { '4k': '4K', '1080': '1080p', '720': '720p', sd: 'SD' }
const rangeText: Record<string, string> = { dv: 'Dolby Vision', hdr10: 'HDR10', hlg: 'HLG', hdr: 'HDR' }
const audioText: Record<string, string> = { atmos: 'Atmos', dtsx: 'DTS:X', truehd: 'TrueHD', dtshd: 'DTS-HD MA', '7.1': '7.1', '5.1': '5.1' }

/**
 * The best copy's badges: 4K, Dolby Vision, Atmos. The server works them out
 * (a show's from its best episode); an older server only sends the files, so
 * a movie or episode falls back to its first file's size and HDR flag.
 */
function qualityBadges(q: Quality | undefined, file: MediaFile | undefined): string[] {
  if (!q && file) {
    const w = file.width, h = file.height
    const resolution = w >= 3200 || h >= 1800 ? '4k' : w >= 1700 || h >= 1000 ? '1080' : w >= 1200 || h >= 700 ? '720' : w > 0 ? 'sd' : undefined
    const audio = file.audioChannels >= 8 ? '7.1' : file.audioChannels >= 6 ? '5.1' : undefined
    q = { resolution, range: file.hdr ? 'hdr' : undefined, audio }
  }
  if (!q) return []
  return [q.resolution && resolutionText[q.resolution], q.range && rangeText[q.range], q.audio && audioText[q.audio]].filter((b): b is string => !!b)
}

/** A movie, show, season or episode, with Play and its seasons/episodes. */
export default function Details({ id }: { id: number }) {
  const nav = useNav()
  const menu = useContext(ItemMenuContext)
  const [d, setD] = useState<ItemDetail | null>(() => cached<ItemDetail>(`/api/items/${id}`) ?? null)
  const [error, setError] = useState('')
  const [failed, setFailed] = useState('') // a button's change that didn't save
  useDownloads() // redraw as a download moves along
  const [picking, setPicking] = useState(false) // choosing a download quality

  const load = useCallback(() => {
    writesDone()
      .then(() => api.get<ItemDetail>(`/api/items/${id}`))
      .then(
        (detail) => {
          setD(detail)
          setError('') // a blip earlier doesn't stay on screen once it loads
        },
        (e) => setError(e.message),
      )
  }, [id])
  // Watchlist and watched: say so when the server didn't take it.
  const change = async (p: Promise<unknown>) => {
    setFailed('')
    await p.catch((e: Error) => setFailed(e.message))
    load()
  }
  // Again each time Back comes here (from the player: the new resume point).
  const { returns } = useScreen()
  useEffect(load, [load, returns])

  if (error && !d)
    return (
      <Center>
        <Text style={st.error}>{error}</Text>
        <Button label="Try again" primary preferFocus onPress={load} />
      </Center>
    )
  if (!d) return <Center><ActivityIndicator color={colors.accent} size="large" /></Center>

  const it = d.item
  const playable = it.kind === 'movie' || it.kind === 'episode'
  // Shows and seasons play their next unwatched episode.
  const target: Item | undefined = playable ? it : d.upNext
  // A finished title being watched again resumes too; finishing resets the position.
  // The same 15 s as Continue Watching on Home.
  const resume = target && target.watch?.position > 15 ? target.watch.position : 0
  const bg = image(it.backdrop || it.showBackdrop || d.show?.backdrop, 'w1280')
  const meta = [it.year, it.contentRating, fmtDuration(it.runtime), it.rating ? `★ ${it.rating.toFixed(1)}` : '', it.genres?.slice(0, 3).join(', ')]
    .filter(Boolean)
    .join('   ·   ')
  const badges = qualityBadges(d.quality, playable ? d.files?.[0] : undefined)
  // Older servers leave out unwatchedCount when it is 0.
  const fullyWatched = playable ? !!it.watch?.watched : !!it.childCount && !it.unwatchedCount
  // Seasons and episodes show their show's cast.
  const cast = ((it.kind === 'season' || it.kind === 'episode' ? d.show?.credits : it.credits) ?? []).filter((c) => c.kind === 'cast').slice(0, 30)
  // Phones save movies, episodes and whole seasons to watch offline.
  const episodes = it.kind === 'season' ? (d.children ?? []).filter((c) => c.kind === 'episode') : []
  const canSave = isPhone && downloadsSupported && !!d.canDownload && ((playable && !!d.files?.length) || episodes.length > 0)
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
        {badges.length > 0 && (
          <View style={st.badges}>
            {badges.map((b) => (
              <View key={b} style={st.badge}><Text style={st.badgeText}>{b}</Text></View>
            ))}
          </View>
        )}
        {!!it.tagline && <Text style={st.tagline}>{it.tagline}</Text>}
        {!!it.overview && <Text style={st.overview} numberOfLines={5}>{it.overview}</Text>}
        <View style={st.buttons}>
          {target ? (
            <>
              <Button
                label={resume ? `Resume from ${fmtClock(resume)}` : playable ? 'Play' : `Play S${target.seasonNumber} E${target.episodeNumber}`}
                primary
                preferFocus
                // A title on this phone plays from the download, saving data.
                onPress={() => nav.push({ name: 'player', id: target.id, start: resume, offline: !!downloaded(target.id) || undefined })}
              />
              {resume > 0 && <Button label="From the start" onPress={() => nav.push({ name: 'player', id: target.id, start: 0, offline: !!downloaded(target.id) || undefined })} />}
              {isPhone && <Button label="Play on a TV" onPress={() => nav.push({ name: 'cast', id: target.id, start: resume })} />}
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
              onPress={() => change(it.onWatchlist ? api.del(`/api/watchlist/${it.id}`) : api.post(`/api/watchlist/${it.id}`))}
            />
          )}
          <Button
            label={fullyWatched ? 'Mark unwatched' : 'Mark watched'}
            onPress={() => change(api.post(`/api/items/${it.id}/${fullyWatched ? 'unwatched' : 'watched'}`))}
          />
          {(it.kind === 'movie' || it.kind === 'show') && d.myRating !== undefined && (
            <Thumbs rating={d.myRating} onRate={(r) => change(api.put(`/api/items/${it.id}/rating`, { rating: r }))} />
          )}
          {canSave && <DownloadButton item={it} episodes={episodes} onPick={() => setPicking((p) => !p)} />}
          {it.kind === 'show' && !!it.tmdbId && <MissingSeasonsButton item={it} />}
        </View>
        {canSave && picking && (
          <DownloadChoices
            file={playable ? d.files?.[0] : undefined}
            onPick={(h) => {
              setPicking(false)
              addDownloads(playable ? [it] : episodes, h)
            }}
            onCancel={() => setPicking(false)}
          />
        )}
        {!!failed && <Text style={st.error}>{failed}</Text>}

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
        {(it.kind === 'movie' || it.kind === 'show') && <MoreLikeThis id={it.id} />}
      </ScrollView>
    </ImageBackground>
  )
}

/** Thumbs down, up and two up (love). Pressing the lit one clears it. */
function Thumbs({ rating, onRate }: { rating: number; onRate: (r: number) => void }) {
  const opts: [number, string][] = [[-1, 'Not for me'], [1, 'Like'], [2, 'Love']]
  return (
    <>
      {opts.map(([r, label]) => (
        <Button key={r} small label={`${r === -1 ? '👎' : r === 1 ? '👍' : '👍👍'} ${label}${rating === r ? ' ✓' : ''}`} onPress={() => onRate(rating === r ? 0 : r)} />
      ))}
    </>
  )
}

/** Titles on the server like this one; an older server without the route shows nothing. */
function MoreLikeThis({ id }: { id: number }) {
  const nav = useNav()
  const [items, setItems] = useState<Item[]>([])
  useEffect(() => {
    let stale = false
    api.get<Item[]>(`/api/items/${id}/similar`).then((l) => !stale && setItems(Array.isArray(l) ? l : []), () => undefined)
    return () => {
      stale = true
    }
  }, [id])
  if (!items.length) return null
  return (
    <>
      <Text style={st.section}>More like this</Text>
      <ScrollView horizontal contentContainerStyle={st.hlist}>
        {items.map((m) => (
          <PosterCard key={m.id} item={m} onPress={() => nav.push({ name: 'details', id: m.id })} />
        ))}
      </ScrollView>
    </>
  )
}

/** Download, or how the download is going (tapping it then opens Downloads). */
function DownloadButton({ item, episodes, onPick }: { item: Item; episodes: Item[]; onPick: () => void }) {
  const nav = useNav()
  if (item.kind === 'season') {
    const dls = episodes.map((e) => downloadFor(e.id))
    const have = dls.filter((x) => x?.state === 'done').length
    const going = dls.some((x) => x && x.state !== 'done' && x.state !== 'failed')
    if (have === episodes.length) return <Button label="Season downloaded ✓" onPress={() => nav.push({ name: 'downloads' })} />
    if (going) return <Button label={`Downloading season · ${have} of ${episodes.length}`} onPress={() => nav.push({ name: 'downloads' })} />
    return <Button label={have ? `Download the other ${episodes.length - have}` : 'Download season'} onPress={onPick} />
  }
  const dl = downloadFor(item.id)
  if (!dl || dl.state === 'failed') return <Button label={dl ? 'Download failed · try again' : 'Download'} onPress={onPick} />
  if (dl.state === 'done') return <Button label="Downloaded ✓" onPress={() => nav.push({ name: 'downloads' })} />
  const pct = dl.state === 'downloading' && dl.total ? ` ${Math.round((dl.done / dl.total) * 100)}%` : ''
  return <Button label={dl.state === 'paused' ? 'Download paused' : `Downloading${pct}`} onPress={() => nav.push({ name: 'downloads' })} />
}

/** The download qualities: the original when this phone plays it, or a smaller converted copy. */
function DownloadChoices({ file, onPick, onCancel }: { file?: MediaFile; onPick: (height: number) => void; onCancel: () => void }) {
  const heights = [1080, 720, 480].filter((h) => !file?.height || h <= file.height)
  if (!heights.length) heights.push(480)
  const size = (h: number) => (file?.duration ? ` · ${formatBytes(estimateBytes(file, h))}` : '')
  const original = !file || canDirectPlay(file, -1)
  return (
    <View>
      <Text style={st.muted}>Download quality{file ? '' : ' for each episode'}</Text>
      <View style={st.buttons}>
        {original && <Button small label={`Original${file?.size ? ` · ${formatBytes(file.size)}` : ''}`} onPress={() => onPick(0)} />}
        {heights.map((h, i) => (
          <Button key={h} small primary={i === 0} label={`${h}p${size(h)}`} onPress={() => onPick(h)} />
        ))}
        <Button small label="Cancel" onPress={onCancel} />
      </View>
      {!!file && <Text style={st.muted}>Converted copies are made by the server first, then saved.</Text>}
    </View>
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
  badges: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 14 },
  badge: { borderWidth: 2, borderColor: colors.muted, borderRadius: 8, paddingHorizontal: 12, paddingVertical: 3, marginRight: 12, marginBottom: 6 },
  badgeText: { color: colors.text, fontSize: font.small, fontWeight: '800', letterSpacing: 1 },
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

interface SeasonsInfo {
  title: { kind: 'show'; tmdbId: number; title: string; year?: number; poster?: string; overview?: string }
  seasons?: { number: number; episodes: number; airDate?: string; onServer: number; requested: boolean }[]
}

/** On a show: the seasons the server doesn't have (out or coming), requested in one press. */
function MissingSeasonsButton({ item }: { item: Item }) {
  const [info, setInfo] = useState<SeasonsInfo | null>(null)
  const [msg, setMsg] = useState('')
  useEffect(() => {
    // Requests off, or no TMDB key: no button.
    api.get<SeasonsInfo>(`/api/requests/title/show/${item.tmdbId}`).then(setInfo, () => setInfo(null))
  }, [item.tmdbId])
  if (msg) return <Button label={msg} onPress={() => undefined} />
  const missing = (info?.seasons ?? []).filter((s) => s.onServer < s.episodes && s.episodes > 0)
  if (!info || missing.length === 0) return null
  const open = missing.filter((s) => !s.requested)
  if (open.length === 0) return <Button label={`Season${missing.length > 1 ? 's' : ''} ${missing.map((s) => s.number).join(', ')} requested ✓`} onPress={() => undefined} />
  const label = `Request season${open.length > 1 ? 's' : ''} ${open.map((s) => s.number).join(', ')}`
  return (
    <Button
      label={label}
      onPress={() => {
        const { kind, tmdbId, title, year, poster, overview } = info.title
        api.post('/api/requests', { kind, tmdbId, title, year, poster, overview, seasons: open.map((s) => s.number) }).then(
          () => setMsg('Requested ✓'),
          (e: Error) => setMsg(e.message),
        )
      }}
    />
  )
}
