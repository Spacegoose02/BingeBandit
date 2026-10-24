import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video, { type OnProgressData, type VideoRef } from 'react-native-video'
import { absolute, api, authHeaders } from '../api'
import { useNav } from '../nav'
import { scaled, colors, font, gutter } from '../theme'
import type { Item, ItemDetail, MediaFile } from '../types'

const REPORT_EVERY = 10 // seconds
const STALL_MS = 8000 // direct play with no progress this long falls back to the transcoder
export const NO_LIVE_BADGE = { hideLiveBadge: true }

function clock(t: number) {
  t = Math.max(0, Math.floor(t))
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = String(t % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

function sessionId() {
  return `tv-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

/**
 * Plays a movie or episode. Direct play first (Android TV decodes most files
 * itself); if the device can't, the server converts it to HLS.
 */
export default function Player({ id, start = 0 }: { id: number; start?: number }) {
  const nav = useNav()
  const video = useRef<VideoRef>(null)
  const [item, setItem] = useState<Item | null>(null)
  const [file, setFile] = useState<MediaFile | null>(null)
  const [src, setSrc] = useState<{ uri: string; type?: string; startPosition?: number } | null>(null)
  const [mode, setMode] = useState<'direct' | 'transcode'>('direct')
  const [paused, setPaused] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [pos, setPos] = useState(start)
  const [error, setError] = useState('')
  const [controls, setControls] = useState(true)

  const offset = useRef(0) // transcodes start at `offset` seconds into the file
  const transcodeId = useRef('')
  const playSession = useRef(sessionId())
  const played = useRef(0)
  const lastTime = useRef(0)
  const lastReport = useRef(0)
  const posRef = useRef(start)
  const hideTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const finished = useRef(false)

  const duration = file?.duration || (item?.runtime ? item.runtime * 60 : 0)

  // Load the item and its file.
  useEffect(() => {
    api.get<ItemDetail>(`/api/items/${id}`).then(
      (d) => {
        setItem(d.item)
        const f = d.files?.[0]
        if (!f) {
          setError('This item has no playable file.')
          return
        }
        setFile(f)
        setSrc({ uri: absolute(`/api/files/${f.id}/direct`), startPosition: start * 1000 })
      },
      (e) => setError(e.message),
    )
  }, [id, start])

  const report = useCallback(
    (stopped = false, done = false) => {
      if (!item || !file) return
      api
        .post('/api/progress', {
          playSessionId: playSession.current,
          itemId: item.id,
          fileId: file.id,
          position: done ? duration : posRef.current,
          duration,
          paused,
          stopped,
          mode,
          transcodeId: transcodeId.current,
          client: 'Android TV',
          played: Math.round(played.current),
        })
        .catch(() => undefined)
    },
    [item, file, duration, paused, mode],
  )

  // Report a final position and free the transcoder when leaving.
  const reportRef = useRef(report)
  reportRef.current = report
  useEffect(
    () => () => {
      reportRef.current(true, finished.current)
      if (transcodeId.current) api.del(`/api/transcode/${transcodeId.current}`).catch(() => undefined)
    },
    [],
  )

  const transcode = useCallback(
    async (at: number) => {
      if (!file) return
      setBuffering(true)
      setError('')
      try {
        const res = await api.post<{ session: { id: string }; playlist: string }>(`/api/files/${file.id}/transcode`, {
          start: at,
          maxHeight: 0,
          audioStream: -1,
          burnSubtitle: -1,
          replace: transcodeId.current,
        })
        transcodeId.current = res.session.id
        offset.current = at
        lastTime.current = 0
        setMode('transcode')
        setSrc({ uri: absolute(res.playlist), type: 'm3u8' })
      } catch (e) {
        setError((e as Error).message)
      }
    },
    [file],
  )

  const onError = useCallback(() => {
    if (mode === 'direct') transcode(posRef.current)
    else setError('Playback failed. The server could not convert this file.')
  }, [mode, transcode])

  // Some TV decoders reject a stream frame by frame without ever raising an
  // error (e.g. 4:4:4 HEVC on Amlogic chips): playback just never starts. If
  // direct play makes no progress for a while, let the server convert it.
  const lastAdvance = useRef(Date.now())
  useEffect(() => {
    lastAdvance.current = Date.now()
  }, [src, paused])
  useEffect(() => {
    if (mode !== 'direct' || !src) return
    const t = setInterval(() => {
      if (!paused && Date.now() - lastAdvance.current > STALL_MS) {
        clearInterval(t)
        transcode(posRef.current)
      }
    }, 2000)
    return () => clearInterval(t)
  }, [mode, src, paused, transcode])

  const onProgress = (p: OnProgressData) => {
    const step = p.currentTime - lastTime.current
    if (step > 0) lastAdvance.current = Date.now()
    lastTime.current = p.currentTime
    if (!paused && step > 0 && step < 2) played.current += step
    posRef.current = offset.current + p.currentTime
    setPos(posRef.current)
    if (posRef.current - lastReport.current >= REPORT_EVERY || lastReport.current > posRef.current) {
      lastReport.current = posRef.current
      report()
    }
  }

  const onEnd = async () => {
    finished.current = true
    report(false, true)
    if (item?.kind === 'episode') {
      const next = await api.get<Item | undefined>(`/api/items/${item.id}/next`).catch(() => undefined)
      if (next?.id) {
        nav.replace({ name: 'player', id: next.id, start: 0 })
        return
      }
    }
    nav.back()
  }

  const seekTo = (t: number) => {
    t = Math.max(0, duration ? Math.min(t, duration - 2) : t)
    posRef.current = t
    setPos(t)
    if (mode === 'direct') video.current?.seek(t)
    else transcode(t) // the server converts from the new point
  }

  const showControls = () => {
    setControls(true)
    if (hideTimer.current) clearTimeout(hideTimer.current)
    hideTimer.current = setTimeout(() => setControls(false), 4000)
  }
  useEffect(() => {
    if (!paused && !buffering) showControls()
    return () => {
      if (hideTimer.current) clearTimeout(hideTimer.current)
    }
  }, [paused, buffering])

  useTVEventHandler((evt) => {
    // Android TV only delivers the key-up half to JS (key-down drives native
    // focus), so act on key up and ignore any key-down that does arrive.
    if (!evt || evt.eventKeyAction === 0) return
    switch (evt.eventType) {
      case 'select':
      case 'playPause':
        setPaused((p) => !p)
        showControls()
        break
      case 'play':
        setPaused(false)
        break
      case 'pause':
        setPaused(true)
        showControls()
        break
      case 'left':
      case 'rewind':
        seekTo(posRef.current - 10)
        showControls()
        break
      case 'right':
      case 'fastForward':
        seekTo(posRef.current + 30)
        showControls()
        break
      case 'up':
      case 'down':
        showControls()
        break
    }
  })

  const title = item ? (item.kind === 'episode' ? `${item.showTitle} · S${item.seasonNumber}E${item.episodeNumber}` : item.title) : ''
  const sub = item?.kind === 'episode' ? item.title : ''
  const pct = duration ? Math.min(100, (pos / duration) * 100) : 0

  return (
    <View style={st.screen}>
      {src && (
        <Video
          // A fresh player per source: after a failed hardware decode the old
          // surface can stay black even though the converted stream plays.
          key={src.uri}
          ref={video}
          // The server's conversion stream is an HLS "event" playlist, which ExoPlayer badges as LIVE.
          controlsStyles={NO_LIVE_BADGE}
          source={{ ...src, headers: authHeaders() }}
          style={StyleSheet.absoluteFill}
          resizeMode="contain"
          paused={paused}
          onProgress={onProgress}
          onLoad={() => setBuffering(false)}
          onBuffer={({ isBuffering }) => setBuffering(isBuffering)}
          onError={onError}
          onEnd={onEnd}
          progressUpdateInterval={500}
        />
      )}
      {/* Something must hold focus or Android never hands the remote's keys to the app. */}
      <Pressable style={StyleSheet.absoluteFill} hasTVPreferredFocus focusable />
      {buffering && !error && <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />}
      {!!error && (
        <View style={st.errorBox}>
          <Text style={st.errorText}>{error}</Text>
          <Text style={st.hint}>Press Back to return.</Text>
        </View>
      )}
      {(controls || paused) && !error && (
        <View style={st.overlay}>
          <View>
            <Text style={st.title} numberOfLines={1}>{title}</Text>
            {!!sub && <Text style={st.sub} numberOfLines={1}>{sub}</Text>}
          </View>
          <View>
            <View style={st.track}>
              <View style={[st.fill, { width: `${pct}%` }]} />
            </View>
            <View style={st.times}>
              <Text style={st.time}>{clock(pos)}</Text>
              <Text style={st.mode}>{paused ? 'Paused' : mode === 'transcode' ? 'Converting' : 'Direct play'}</Text>
              <Text style={st.time}>-{clock(duration - pos)}</Text>
            </View>
            <Text style={st.hint}>OK: play/pause   ◀ 10s back   ▶ 30s forward</Text>
          </View>
        </View>
      )}
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: '#000' },
  spinner: { position: 'absolute', top: '50%', left: '50%', marginLeft: -18, marginTop: -18 },
  overlay: { ...StyleSheet.absoluteFillObject, justifyContent: 'space-between', paddingHorizontal: gutter, paddingVertical: 60, backgroundColor: 'rgba(0,0,0,0.35)' },
  title: { color: '#fff', fontSize: font.title, fontWeight: '800' },
  sub: { color: '#d6dbe1', fontSize: font.body, marginTop: 6 },
  track: { height: 10, borderRadius: 5, backgroundColor: 'rgba(255,255,255,0.25)' },
  fill: { height: 10, borderRadius: 5, backgroundColor: colors.accent },
  times: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 10 },
  time: { color: '#fff', fontSize: font.body, fontVariant: ['tabular-nums'] },
  mode: { color: colors.accent, fontSize: font.small, fontWeight: '700' },
  hint: { color: '#c3c9d0', fontSize: font.small, marginTop: 10, textAlign: 'center' },
  errorBox: { position: 'absolute', top: '40%', alignSelf: 'center', backgroundColor: 'rgba(20,24,29,0.95)', padding: 40, borderRadius: 20, maxWidth: 1300 },
  errorText: { color: colors.text, fontSize: font.body, textAlign: 'center' },
}))
