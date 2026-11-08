import React, { useEffect, useState } from 'react'
import { FlatList, Image, Pressable, StyleSheet, Text, View } from 'react-native'
import { api, image } from '../api'
import { Button, Center, Focusable, PosterCard } from '../components'
import { useNav } from '../nav'
import { playTracks, seekMusic, skipTrack, stopMusic, toggleMusic, useMusic } from '../music'
import { colors, font, gutter, isPhone, quietPress, scaled } from '../theme'
import type { ItemDetail, Track } from '../types'

function clock(t: number) {
  const s = Math.max(0, Math.floor(t))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

/** The music player's screen: what's playing, the controls and the queue. */
export default function NowPlaying() {
  const nav = useNav()
  const m = useMusic()
  const t = m.queue[m.index]
  if (!t)
    return (
      <Center>
        <Text style={st.muted}>Nothing is playing.</Text>
        <Button label="Back" primary preferFocus onPress={nav.back} />
      </Center>
    )
  const art = image(t.item.poster, 'w500')
  const dur = t.file.duration
  return (
    <View style={st.screen}>
      {isPhone && (
        <Pressable android_disableSound={quietPress} onPress={nav.back} hitSlop={20}>
          <Text style={st.back}>‹ Back</Text>
        </Pressable>
      )}
      <View style={[st.top, isPhone && st.topPhone]}>
        {art ? <Image source={art} style={st.art} /> : <View style={[st.art, st.artEmpty]} />}
        <View style={st.info}>
          <Text style={st.title} numberOfLines={2}>{t.item.title}</Text>
          <Text style={st.sub} numberOfLines={1}>{[t.item.studio, t.album].filter(Boolean).join(' · ')}</Text>
          <View style={st.bar}>
            <View style={[st.barFill, { width: `${dur ? Math.min(100, (m.position / dur) * 100) : 0}%` }]} />
          </View>
          <Text style={st.time}>{clock(m.position)} / {clock(dur)}</Text>
          <View style={st.controls}>
            <Button small label="◀◀" onPress={() => skipTrack(-1)} />
            <Button small primary preferFocus label={m.playing ? 'Pause' : 'Play'} onPress={toggleMusic} />
            <Button small label="▶▶" onPress={() => skipTrack(1)} />
            <Button small label="−10s" onPress={() => seekMusic(m.position - 10)} />
            <Button small label="+30s" onPress={() => seekMusic(m.position + 30)} />
            <Button small label="Shuffle" onPress={() => playTracks(m.queue, 0, true)} />
            <Button small label="Stop" onPress={() => { stopMusic(); nav.back() }} />
          </View>
          {!!m.error && <Text style={st.error}>{m.error}</Text>}
        </View>
      </View>
      <Text style={st.section}>Up next</Text>
      <FlatList
        data={m.queue.map((tr, i) => ({ tr, i })).slice(m.index + 1, m.index + 100)}
        keyExtractor={(x) => `${x.i}:${x.tr.item.id}`}
        renderItem={({ item: x }) => <TrackRow track={x.tr} onPress={() => playTracks(m.queue, x.i)} />}
        ListEmptyComponent={<Text style={st.muted}>The end of the queue.</Text>}
      />
    </View>
  )
}

export function TrackRow({ track, number, playing, onPress }: { track: Track; number?: number; playing?: boolean; onPress: () => void }) {
  return (
    <Focusable onPress={onPress} style={st.track}>
      <Text style={[st.trackNo, playing && st.accent]}>{playing ? '♪' : number ?? ''}</Text>
      <View style={st.flex}>
        <Text style={[st.trackTitle, playing && st.accent]} numberOfLines={1}>{track.item.title}</Text>
        {!!(track.item.studio || track.album) && <Text style={st.muted} numberOfLines={1}>{[track.item.studio, track.album].filter(Boolean).join(' · ')}</Text>}
      </View>
      <Text style={st.muted}>{clock(track.file.duration)}</Text>
    </Focusable>
  )
}

/** An album's or artist's page: play, shuffle, the tracks and (an artist's) albums. */
export function MusicDetails({ d }: { d: ItemDetail }) {
  const nav = useNav()
  const m = useMusic()
  const [tracks, setTracks] = useState<Track[] | null>(null)
  const [error, setError] = useState('')
  const it = d.item
  useEffect(() => {
    api.get<Track[]>(`/api/items/${it.id}/tracks`).then(setTracks, (e: Error) => setError(e.message))
  }, [it.id])
  const playing = m.queue[m.index]
  const start = (i: number, shuffle = false) => {
    if (!tracks?.length) return
    playTracks(tracks, i, shuffle)
    nav.push({ name: 'music' })
  }
  const albums = it.kind === 'artist' ? (d.children ?? []) : []
  const art = image(it.poster, 'w500')
  return (
    <View style={st.screen}>
      {isPhone && (
        <Pressable android_disableSound={quietPress} onPress={nav.back} hitSlop={20}>
          <Text style={st.back}>‹ Back</Text>
        </Pressable>
      )}
      <FlatList
        data={tracks ?? []}
        keyExtractor={(tr) => String(tr.item.id)}
        ListHeaderComponent={
          <View>
            <View style={[st.top, isPhone && st.topPhone]}>
              {art ? <Image source={art} style={st.art} /> : <View style={[st.art, st.artEmpty]} />}
              <View style={st.info}>
                <Text style={st.title} numberOfLines={2}>{it.title}</Text>
                <Text style={st.sub}>{[(it as { studio?: string }).studio, it.year, tracks ? `${tracks.length} tracks` : ''].filter(Boolean).join(' · ')}</Text>
                <View style={st.controls}>
                  <Button primary preferFocus label="Play" onPress={() => start(0)} />
                  <Button label="Shuffle" onPress={() => start(0, true)} />
                  {!!playing && <Button label="Now playing" onPress={() => nav.push({ name: 'music' })} />}
                </View>
                {!!error && <Text style={st.error}>{error}</Text>}
              </View>
            </View>
            {albums.length > 0 && (
              <>
                <Text style={st.section}>Albums</Text>
                <FlatList
                  horizontal
                  data={albums}
                  keyExtractor={(a) => String(a.id)}
                  renderItem={({ item: a }) => <PosterCard item={a} onPress={() => nav.push({ name: 'details', id: a.id })} />}
                />
              </>
            )}
            <Text style={st.section}>Tracks</Text>
          </View>
        }
        renderItem={({ item: tr, index }) => (
          <TrackRow track={tr} number={it.kind === 'album' ? tr.item.episodeNumber ?? index + 1 : index + 1} playing={playing?.item.id === tr.item.id} onPress={() => start(index)} />
        )}
      />
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg, paddingHorizontal: gutter, paddingTop: 30 },
  flex: { flex: 1 },
  back: { color: colors.text, fontSize: font.body, marginBottom: 10 },
  top: { flexDirection: 'row', gap: 40, marginBottom: 20 },
  topPhone: { flexDirection: 'column', gap: 16 },
  art: { width: 320, height: 320, borderRadius: 12 },
  artEmpty: { backgroundColor: colors.surface2 },
  info: { flex: 1, justifyContent: 'center' },
  title: { color: colors.text, fontSize: font.title, fontWeight: '700' },
  sub: { color: colors.muted, fontSize: font.body, marginTop: 6 },
  bar: { height: 6, backgroundColor: 'rgba(255,255,255,0.15)', borderRadius: 3, marginTop: 24, overflow: 'hidden' },
  barFill: { height: 6, backgroundColor: colors.accent },
  time: { color: colors.muted, fontSize: font.small, marginTop: 6 },
  controls: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginTop: 20 },
  error: { color: colors.danger, fontSize: font.small, marginTop: 10 },
  section: { color: colors.text, fontSize: font.body, fontWeight: '700', marginTop: 16, marginBottom: 8 },
  muted: { color: colors.muted, fontSize: font.small },
  accent: { color: colors.accent },
  track: { flexDirection: 'row', alignItems: 'center', gap: 20, paddingVertical: 12, paddingHorizontal: 16, borderRadius: 8 },
  trackNo: { width: 40, color: colors.muted, fontSize: font.body, textAlign: 'right' },
  trackTitle: { color: colors.text, fontSize: font.body },
}))

