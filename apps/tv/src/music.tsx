// The music player: a queue of tracks that keeps playing while you browse
// (the Now Playing screen shows and steers it). One hidden audio-only
// player lives at the app's root. Files play as they are when Android can
// (MP3, AAC, FLAC, Ogg, Opus, WAV); anything else (ALAC, WMA, APE...) is
// converted to MP3 by the server from the current position. It reports
// progress like the video player, so plays show on the admin dashboard and
// count toward screen time. Starting a video or Live TV pauses it.

import React, { useEffect, useRef, useState } from 'react'
import { StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video, { type OnProgressData, type VideoRef } from 'react-native-video'
import { absolute, api, ApiError, authHeaders } from './api'
import { colors, font, isPhone, scaled } from './theme'
import type { Track } from './types'

interface MusicState {
  queue: Track[]
  index: number
  playing: boolean
  position: number // seconds into the current track
  error: string
  // Bumped to make the player load the current track again (a new track, or a seek in a converted one).
  load: number
  start: number // where the load starts
}

let state: MusicState = { queue: [], index: 0, playing: false, position: 0, error: '', load: 0, start: 0 }
const listeners = new Set<() => void>()
let seekTo: ((t: number) => void) | null = null

function set(patch: Partial<MusicState>) {
  state = { ...state, ...patch }
  for (const l of listeners) l()
}

/** Redraws with the player. */
export function useMusic(): MusicState {
  const [, redraw] = useState(0)
  useEffect(() => {
    const l = () => redraw((n) => n + 1)
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  }, [])
  return state
}

export function currentTrack(): Track | undefined {
  return state.queue[state.index]
}

function shuffled<T>(list: T[]): T[] {
  const out = [...list]
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return out
}

/** Plays a list of tracks from index (shuffled: in a random order). */
export function playTracks(tracks: Track[], index = 0, shuffle = false) {
  if (!tracks.length) return
  const queue = shuffle ? shuffled(tracks) : tracks
  set({ queue, index: shuffle ? 0 : index, playing: true, position: 0, error: '', load: state.load + 1, start: 0 })
}

/** Plays a track, or loads an album's or artist's tracks and plays them. */
export async function playItem(itemId: number, shuffle = false) {
  const tracks = await api.get<Track[]>(`/api/items/${itemId}/tracks`)
  playTracks(tracks, 0, shuffle)
}

export function toggleMusic() {
  if (currentTrack()) set({ playing: !state.playing })
}

/** Pauses the music (a video is starting). */
export function pauseMusic() {
  if (state.playing) set({ playing: false })
}

export function stopMusic() {
  set({ queue: [], index: 0, playing: false, position: 0 })
}

export function skipTrack(by: 1 | -1) {
  // Back within the first few seconds goes to the previous track, else to the start of this one.
  if (by === -1 && state.position > 4) return seekMusic(0)
  const index = state.index + by
  if (index < 0 || index >= state.queue.length) {
    if (by === 1) set({ playing: false, position: 0 })
    return
  }
  set({ index, position: 0, playing: true, error: '', load: state.load + 1, start: 0 })
}

export function seekMusic(t: number) {
  const tr = currentTrack()
  if (!tr) return
  t = Math.max(0, Math.min(t, tr.file.duration - 1))
  if (seekTo) seekTo(t)
  set({ position: t })
}

// Formats ExoPlayer plays itself; the rest are converted.
const PLAYS = new Set(['mp3', 'm4a', 'mp4', 'aac', 'flac', 'ogg', 'oga', 'opus', 'wav'])
const needsConversion = (t: Track) => !PLAYS.has(t.file.container.toLowerCase()) || t.file.audioCodec === 'alac'

/**
 * The hidden player; mounted once at the app's root while signed in. Away
 * from a video and from Now Playing (overlay), a small tag says what's
 * playing, and the remote's Play/Pause key pauses and resumes the music.
 */
export function MusicHost({ overlay }: { overlay: boolean }) {
  const m = useMusic()
  const overlayRef = useRef(overlay)
  overlayRef.current = overlay
  useTVEventHandler((evt) => {
    if (overlayRef.current && evt?.eventType === 'playPause' && currentTrack()) toggleMusic()
  })
  const video = useRef<VideoRef>(null)
  const session = useRef('')
  const offset = useRef(0)
  const played = useRef(0)
  const lastPos = useRef(0)
  const lastReport = useRef(0)
  const t = m.queue[m.index]
  const convert = !!t && needsConversion(t)

  // A converted track seeks by asking the server again from there.
  seekTo = (to: number) => {
    if (convert) set({ load: state.load + 1, start: to })
    else video.current?.seek(to)
  }

  const report = (stopped = false) => {
    if (!t || !session.current) return
    lastReport.current = Date.now()
    api
      .post('/api/progress', {
        playSessionId: session.current, itemId: t.item.id, fileId: t.file.fileId, position: state.position, duration: t.file.duration,
        paused: !state.playing, stopped, mode: convert ? 'transcode' : 'direct', client: isPhone ? 'Android' : 'Android TV', lights: false, played: Math.round(played.current),
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403 && /^Screen time/.test(e.message)) set({ playing: false, error: e.message })
      })
  }

  // A new track is a new play session; the old one is reported stopped.
  useEffect(() => {
    if (!t) return
    session.current = `tv-audio-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    played.current = 0
    return () => report(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t?.item.id, m.load])
  useEffect(() => {
    if (t) report()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [m.playing])

  if (!t) return null
  offset.current = convert ? m.start : 0
  const uri = convert ? absolute(`/api/files/${t.file.fileId}/audio?start=${m.start.toFixed(2)}`) : absolute(`/api/files/${t.file.fileId}/direct`)
  const onProgress = (p: OnProgressData) => {
    const pos = offset.current + p.currentTime
    const step = pos - lastPos.current
    if (step > 0 && step < 2) played.current += step
    lastPos.current = pos
    set({ position: pos })
    if (Date.now() - lastReport.current > 10_000) report()
  }
  return (
    <>
    {overlay && (
      <View style={st.tag} pointerEvents="none">
        <Text style={st.tagText} numberOfLines={1}>{m.playing ? '♪' : '❚❚'}  {t.item.title}{t.item.studio ? ` · ${t.item.studio}` : ''}</Text>
      </View>
    )}
    <View style={st.hidden} pointerEvents="none">
      <Video
        key={`${t.item.id}:${m.load}`}
        ref={video}
        source={{ uri, headers: authHeaders() }}
        paused={!m.playing}
        playInBackground
        playWhenInactive
        progressUpdateInterval={1000}
        onLoad={() => {
          if (!convert && m.start > 0) video.current?.seek(m.start)
        }}
        onProgress={onProgress}
        onEnd={() => {
          set({ position: t.file.duration })
          report(true)
          skipTrack(1)
        }}
        onError={() => {
          set({ error: `Couldn't play ${t.item.title}.` })
          skipTrack(1)
        }}
        style={st.hidden}
      />
    </View>
    </>
  )
}

const st = StyleSheet.create(scaled({
  hidden: { position: 'absolute', width: 1, height: 1, opacity: 0 },
  tag: { position: 'absolute', right: 24, bottom: 24, maxWidth: 560, paddingHorizontal: 18, paddingVertical: 10, borderRadius: 24, backgroundColor: 'rgba(20,24,29,0.92)' },
  tagText: { color: colors.text, fontSize: font.small },
}))
