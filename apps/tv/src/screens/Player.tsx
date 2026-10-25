import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video, { SelectedTrackType, type OnProgressData, type VideoRef } from 'react-native-video'
import { absolute, api, authHeaders } from '../api'
import { Button } from '../components'
import { backInterceptor, useNav } from '../nav'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { useLock } from '../lock'
import { useLandscape } from '../screen'
import { px, scaled, colors, font, gutter, isPhone } from '../theme'
import { cueAt, IMAGE_SUBS, parseVtt, pickTracks, streamLabel, TEXT_SUBS, type Cue, type MediaStream, type Prefs } from '../tracks'
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
 * Plays the server's intro video first when a movie starts from the
 * beginning and the viewer has it turned on. (Trailers are YouTube videos,
 * which only the web player can show.)
 */
export default function Player({ id, start = 0 }: { id: number; start?: number }) {
  const [intro, setIntro] = useState<string | null | undefined>(start > 0 ? null : undefined)
  useEffect(() => {
    if (start > 0) return
    let stale = false
    api
      .get<{ intro?: { url: string } }>(`/api/items/${id}/preroll`)
      .then((p) => !stale && setIntro(p.intro?.url ?? null))
      .catch(() => !stale && setIntro(null))
    return () => {
      stale = true
    }
  }, [id, start])
  const done = useCallback(() => setIntro(null), [])
  if (intro === undefined) {
    return (
      <View style={st.screen}>
        <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />
      </View>
    )
  }
  if (intro) return <IntroVideo uri={absolute(intro)} onDone={done} />
  return <MediaPlayer id={id} start={start} />
}

function IntroVideo({ uri, onDone }: { uri: string; onDone: () => void }) {
  useLandscape()
  return (
    <View style={st.screen}>
      <Video
        source={{ uri, headers: authHeaders() }}
        style={StyleSheet.absoluteFill}
        resizeMode="contain"
        onEnd={onDone}
        onError={onDone}
      />
      <View style={st.introSkip}>
        <Button label="Start movie" small onPress={onDone} preferFocus />
      </View>
    </View>
  )
}

/**
 * Plays a movie or episode. Direct play first (Android TV decodes most files
 * itself); if the device can't, the server converts it to HLS.
 */
function MediaPlayer({ id, start = 0 }: { id: number; start?: number }) {
  const nav = useNav()
  useLandscape()
  const insets = useSafeAreaInsets()
  const lock = useLock()
  const video = useRef<VideoRef>(null)
  const [item, setItem] = useState<Item | null>(null)
  const [file, setFile] = useState<MediaFile | null>(null)
  const [src, setSrc] = useState<{ uri: string; type?: string; startPosition?: number } | null>(null)
  const [mode, setMode] = useState<'direct' | 'transcode'>('direct')
  const [paused, setPaused] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [pos, setPos] = useState(start)
  const [error, setError] = useState('')
  const [controls, setControls] = useState<'hidden' | 'seek' | 'full'>('hidden')
  const [audio, setAudio] = useState(-1) // stream index; -1 = the file's default
  const [sub, setSub] = useState<MediaStream | undefined>()
  const [cues, setCues] = useState<Cue[]>([])
  const [menu, setMenu] = useState(false)

  const audioRef = useRef(-1) // what the transcoder is told
  const burnRef = useRef(-1) // image subtitle stream burned in by the server
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

  // Load the item, its file and the viewer's track preferences, then start.
  useEffect(() => {
    Promise.all([api.get<ItemDetail>(`/api/items/${id}`), api.get<Prefs>('/api/me/prefs').catch(() => null)]).then(
      ([d, prefs]) => {
        setItem(d.item)
        const f = d.files?.[0]
        if (!f) {
          setError('This item has no playable file.')
          return
        }
        setFile(f)
        const picked = pickTracks(f.streams ?? [], prefs)
        setAudio(picked.audio)
        audioRef.current = picked.audio
        if (picked.sub && IMAGE_SUBS.has(picked.sub.codec)) {
          // Image subtitles need the server to burn them in.
          burnRef.current = picked.sub.index
          setSub(picked.sub)
          startTranscode.current = true
        } else {
          if (picked.sub) setSub(picked.sub)
          setSrc({ uri: absolute(`/api/files/${f.id}/direct`), startPosition: start * 1000 })
        }
      },
      (e) => setError(e.message),
    )
  }, [id, start])

  // Text subtitles are drawn by the app from the server's WebVTT, so they
  // stay in sync in both direct play and conversion (cue times are file time).
  useEffect(() => {
    setCues([])
    if (!file || !sub || !TEXT_SUBS.has(sub.codec)) return
    let stale = false
    fetch(absolute(`/api/files/${file.id}/subtitles/${sub.index}.vtt`), { headers: authHeaders() })
      .then((r) => (r.ok ? r.text() : ''))
      .then((t) => !stale && setCues(parseVtt(t)))
      .catch(() => undefined)
    return () => {
      stale = true
    }
  }, [file, sub])

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
          client: isPhone ? 'Android' : 'Android TV',
          played: Math.round(played.current),
        })
        .catch(() => undefined)
    },
    [item, file, duration, paused, mode],
  )

  // Pausing and resuming are reported at once, for plugins that follow the player (Hue).
  const pauseSeen = useRef(false)
  useEffect(() => {
    if (pauseSeen.current) report()
    pauseSeen.current = true
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paused])

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
          audioStream: audioRef.current,
          burnSubtitle: burnRef.current,
          replace: transcodeId.current,
          adaptive: true, // ExoPlayer switches qualities itself when the server offers several
          // Android's AAC decoder handles 5.1 everywhere (and mixes down for
          // stereo TVs), so surround tracks stay surround. AC-3/E-AC-3 copy
          // waits until passthrough is tested on real boxes.
          audioCodecs: ['aac'],
          maxAudioChannels: 6,
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

  // Burned-in subtitles chosen up front: start converting once the file is known.
  const startTranscode = useRef(false)
  useEffect(() => {
    if (file && startTranscode.current) {
      startTranscode.current = false
      transcode(start)
    }
  }, [file, transcode, start])

  const chooseAudio = (index: number) => {
    setAudio(index)
    audioRef.current = index
    if (mode === 'transcode') transcode(posRef.current) // the server mixes the new track
  }

  const chooseSub = (s?: MediaStream) => {
    setSub(s)
    const burn = s && IMAGE_SUBS.has(s.codec) ? s.index : -1
    if (burn !== burnRef.current) {
      burnRef.current = burn
      transcode(posRef.current)
    }
  }

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

  // Controls: hidden; "seek" (just the progress bar, after ◀/▶ while hidden);
  // or "full" (focusable buttons). They hide again unless paused.
  const showControls = (kind: 'seek' | 'full') => {
    setControls((c) => (c === 'full' ? 'full' : kind))
    if (hideTimer.current) clearTimeout(hideTimer.current)
    hideTimer.current = setTimeout(() => setControls('hidden'), kind === 'full' ? 6000 : 2500)
  }
  useEffect(() => {
    if (paused && hideTimer.current) clearTimeout(hideTimer.current) // stay up while paused
    if (!paused && controls !== 'hidden') showControls(controls)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paused])
  useEffect(() => () => {
    if (hideTimer.current) clearTimeout(hideTimer.current)
  }, [])

  // The next episode, for the Next button.
  const [next, setNext] = useState<Item | null>(null)
  useEffect(() => {
    if (item?.kind === 'episode') api.get<Item | undefined>(`/api/items/${item.id}/next`).then((n) => setNext(n ?? null), () => undefined)
  }, [item])

  // Back closes the menu, then the controls, before leaving the player; a locked player swallows it.
  useEffect(() => {
    backInterceptor.current = () => {
      if (lock.handleBack()) return true
      if (menu) {
        setMenu(false)
        return true
      }
      if (controls === 'full') {
        setControls('hidden')
        return true
      }
      return false
    }
    return () => {
      backInterceptor.current = null
    }
  })

  useTVEventHandler((evt) => {
    // Android TV only delivers the key-up half to JS (key-down drives native
    // focus), so act on key up and ignore any key-down that does arrive.
    if (!evt || evt.eventKeyAction === 0) return
    if (menu) return // the menu's buttons handle the keys
    if (controls === 'full') {
      // The buttons have focus; any key just keeps the controls up.
      if (!paused) showControls('full')
      if (evt.eventType === 'playPause') setPaused((p) => !p)
      return
    }
    switch (evt.eventType) {
      case 'select':
      case 'up':
      case 'down':
      case 'menu':
      case 'info':
        showControls('full')
        break
      case 'playPause':
        setPaused((p) => !p)
        showControls('full')
        break
      case 'play':
        setPaused(false)
        break
      case 'pause':
        setPaused(true)
        showControls('full')
        break
      case 'left':
      case 'rewind':
        seekTo(posRef.current - 10)
        showControls('seek')
        break
      case 'right':
      case 'fastForward':
        seekTo(posRef.current + 30)
        showControls('seek')
        break
    }
  })

  const audios = (file?.streams ?? []).filter((s) => s.type === 'audio')
  const subs = (file?.streams ?? []).filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec)))
  // ExoPlayer numbers audio tracks in file order, so the direct-play choice is a position.
  const audioPos = audios.findIndex((s) => s.index === audio)
  const cue = sub && TEXT_SUBS.has(sub.codec) ? cueAt(cues, pos) : ''

  const title = item ? (item.kind === 'episode' ? `${item.showTitle} · S${item.seasonNumber}E${item.episodeNumber}` : item.title) : ''
  const subtitleLine = item?.kind === 'episode' ? item.title : ''
  const pct = duration ? Math.min(100, (pos / duration) * 100) : 0
  // A sideways phone has the navigation bar (or a camera cut-out) on one side; keep controls clear of it.
  const barsPad = isPhone ? { paddingLeft: insets.left + px(60), paddingRight: insets.right + px(60), paddingBottom: insets.bottom + px(30) } : null

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
          selectedAudioTrack={mode === 'direct' && audioPos >= 0 ? { type: SelectedTrackType.INDEX, value: audioPos } : undefined}
          onProgress={onProgress}
          onLoad={(d) => {
            // ExoPlayer quietly drops a video track this device can't decode
            // (10-bit H.264, some HEVC, VC-1 …) and plays only the sound.
            // No picture in direct play: let the server convert the file.
            if (mode === 'direct' && file?.videoCodec && !d.naturalSize?.width) {
              transcode(posRef.current)
              return
            }
            setBuffering(false)
            showControls('seek') // a glimpse of the title and position
          }}
          onBuffer={({ isBuffering }) => setBuffering(isBuffering)}
          onError={onError}
          onEnd={onEnd}
          progressUpdateInterval={250}
        />
      )}
      {/* TV: something must hold focus or Android never hands the remote's keys to the app.
          Phone: tapping the picture shows or hides the controls. */}
      <Pressable
        style={StyleSheet.absoluteFill}
        hasTVPreferredFocus={!menu && controls !== 'full'}
        focusable={!menu && controls !== 'full'}
        onPress={isPhone ? () => (lock.locked ? lock.poke() : controls === 'full' ? setControls('hidden') : showControls('full')) : undefined}
      />
      {!!cue && (
        <View style={[st.cueWrap, (controls !== 'hidden' || paused || menu) && st.cueRaised]} pointerEvents="none">
          <Text style={st.cue}>{cue}</Text>
        </View>
      )}
      {buffering && !error && <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />}
      {!!error && (
        <View style={st.errorBox}>
          <Text style={st.errorText}>{error}</Text>
          <Text style={st.hint}>Press Back to return.</Text>
        </View>
      )}
      {(controls !== 'hidden' || paused) && !error && !menu && !lock.locked && (
        <Pressable style={[st.overlay, barsPad]} focusable={false} onPress={isPhone ? () => setControls('hidden') : undefined}>
          <View style={st.topRow}>
            {isPhone && (
              <Pressable onPress={nav.back} hitSlop={24} style={st.backBtn}>
                <Text style={st.backText}>‹</Text>
              </Pressable>
            )}
            <View style={st.flex}>
              <Text style={st.title} numberOfLines={1}>{title}</Text>
              {!!subtitleLine && <Text style={st.sub} numberOfLines={1}>{subtitleLine}</Text>}
            </View>
          </View>
          <View>
            {isPhone ? (
              <SeekBar pct={pct} duration={duration} onSeek={(t) => {
                seekTo(t)
                showControls('full')
              }} />
            ) : (
              <View style={st.track}>
                <View style={[st.fill, { width: `${pct}%` }]} />
              </View>
            )}
            <View style={st.times}>
              <Text style={st.time}>{clock(pos)}</Text>
              <Text style={st.mode}>{paused ? 'Paused' : mode === 'transcode' ? 'Converting' : 'Direct play'}</Text>
              <Text style={[st.time, st.timeEnd]}>-{clock(duration - pos)}</Text>
            </View>
            {controls === 'full' ? (
              <View style={st.controlRow}>
                <Button small label="⟲ 10s" onPress={() => seekTo(posRef.current - 10)} />
                <Button small primary preferFocus label={paused ? '▶  Play' : '❚❚  Pause'} onPress={() => setPaused((p) => !p)} />
                <Button small label="30s ⟳" onPress={() => seekTo(posRef.current + 30)} />
                {next && <Button small label="Next episode  ▶▶" onPress={() => nav.replace({ name: 'player', id: next.id, start: 0 })} />}
                <Button small label="Audio & subtitles" onPress={() => setMenu(true)} />
                {isPhone && (
                  <Button
                    small
                    label="Lock"
                    onPress={() => {
                      setControls('hidden')
                      lock.startLock()
                    }}
                  />
                )}
              </View>
            ) : (
              <Text style={st.hint}>{isPhone ? 'Tap for controls' : 'OK for controls   ◀ 10s back   ▶ 30s forward'}</Text>
            )}
          </View>
        </Pressable>
      )}
      {menu && (
        <View style={[st.menu, barsPad]}>
          {audios.length > 1 && (
            <>
              <Text style={st.menuTitle}>Audio</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                {audios.map((a, i) => {
                  const on = audio === a.index || (audio === -1 && (a.default || (i === 0 && !audios.some((x) => x.default))))
                  return <Button key={a.index} small primary={on} label={streamLabel(a)} onPress={() => chooseAudio(a.default || (i === 0 && !audios.some((x) => x.default)) ? -1 : a.index)} />
                })}
              </ScrollView>
            </>
          )}
          <Text style={st.menuTitle}>Subtitles</Text>
          <ScrollView horizontal contentContainerStyle={st.menuRow}>
            <Button small primary={!sub} preferFocus={!sub} label="Off" onPress={() => chooseSub(undefined)} />
            {subs.map((s) => (
              <Button
                key={s.index}
                small
                primary={sub?.index === s.index}
                preferFocus={sub?.index === s.index}
                label={streamLabel(s) + (IMAGE_SUBS.has(s.codec) ? ' · burned in' : '')}
                onPress={() => chooseSub(s)}
              />
            ))}
          </ScrollView>
          {subs.length === 0 && <Text style={st.hint}>This file has no subtitles.</Text>}
          {isPhone ? <Button small label="Done" onPress={() => setMenu(false)} style={st.done} /> : <Text style={st.hint}>Back closes this menu</Text>}
        </View>
      )}
      {lock.layer}
    </View>
  )
}

/** A touch progress bar: drag or tap to pick a time; the video seeks when the finger lifts. */
function SeekBar({ pct, duration, onSeek }: { pct: number; duration: number; onSeek: (t: number) => void }) {
  const bar = useRef<View>(null)
  const box = useRef({ x: 0, width: 1 }) // the bar's position on screen
  const [drag, setDrag] = useState<number | null>(null) // 0..1 while the finger is down
  // Screen coordinates: locationX would be relative to whatever is under the finger (e.g. the time label).
  const at = (pageX: number) => Math.max(0, Math.min(1, (pageX - box.current.x) / box.current.width))
  const shown = drag !== null ? drag * 100 : pct
  return (
    <View
      ref={bar}
      style={st.seekArea}
      onLayout={() => bar.current?.measureInWindow((x, _y, w) => (box.current = { x, width: w || 1 }))}
      onStartShouldSetResponder={() => true}
      onResponderTerminationRequest={() => false}
      onResponderGrant={(e) => setDrag(at(e.nativeEvent.pageX))}
      onResponderMove={(e) => setDrag(at(e.nativeEvent.pageX))}
      onResponderRelease={() => {
        if (drag !== null) onSeek(drag * duration)
        setDrag(null)
      }}
    >
      <View style={st.track} pointerEvents="none">
        <View style={[st.fill, { width: `${shown}%` }]} />
      </View>
      <View style={[st.thumb, { left: `${shown}%` }]} pointerEvents="none" />
      {drag !== null && <Text style={[st.dragTime, { left: `${shown}%` }]} pointerEvents="none">{clock(drag * duration)}</Text>}
    </View>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  introSkip: { position: 'absolute', right: 60, bottom: 40 },
  topRow: { flexDirection: 'row', alignItems: 'center' },
  backBtn: { paddingRight: 24 },
  backText: { color: '#fff', fontSize: 64, fontWeight: '300', lineHeight: 70 },
  seekArea: { height: 60, justifyContent: 'center' },
  thumb: { position: 'absolute', top: 15, width: 30, height: 30, marginLeft: -15, borderRadius: 15, backgroundColor: colors.accent },
  dragTime: { position: 'absolute', top: -50, width: 140, marginLeft: -70, textAlign: 'center', color: '#fff', fontSize: font.body, fontWeight: '700' },
  done: { alignSelf: 'flex-start', marginTop: 16 },
  screen: { flex: 1, backgroundColor: '#000' },
  spinner: { position: 'absolute', top: '50%', left: '50%', marginLeft: -18, marginTop: -18 },
  overlay: { ...StyleSheet.absoluteFillObject, justifyContent: 'space-between', paddingHorizontal: isPhone ? 60 : gutter, paddingVertical: isPhone ? 30 : 60, backgroundColor: 'rgba(0,0,0,0.35)' },
  title: { color: '#fff', fontSize: font.title, fontWeight: '800' },
  sub: { color: '#d6dbe1', fontSize: font.body, marginTop: 6 },
  track: { height: 10, borderRadius: 5, backgroundColor: 'rgba(255,255,255,0.25)' },
  fill: { height: 10, borderRadius: 5, backgroundColor: colors.accent },
  times: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 10 },
  // A minimum width keeps the times from shifting as the digits change.
  time: { color: '#fff', fontSize: font.body, fontVariant: ['tabular-nums'], minWidth: 150 },
  timeEnd: { textAlign: 'right' },
  mode: { color: colors.accent, fontSize: font.small, fontWeight: '700' },
  hint: { color: '#c3c9d0', fontSize: font.small, marginTop: 10, textAlign: 'center' },
  errorBox: { position: 'absolute', top: '40%', alignSelf: 'center', backgroundColor: 'rgba(20,24,29,0.95)', padding: 40, borderRadius: 20, maxWidth: 1300 },
  errorText: { color: colors.text, fontSize: font.body, textAlign: 'center' },
  cueWrap: { position: 'absolute', left: 160, right: 160, bottom: 90, alignItems: 'center' },
  cueRaised: { bottom: 260 },
  cue: { color: '#fff', fontSize: 46, fontWeight: '600', textAlign: 'center', lineHeight: 60, backgroundColor: 'rgba(0,0,0,0.6)', paddingHorizontal: 18, borderRadius: 8 },
  menu: { position: 'absolute', left: 0, right: 0, bottom: 0, paddingHorizontal: gutter, paddingTop: 30, paddingBottom: 50, backgroundColor: 'rgba(11,13,16,0.94)' },
  menuTitle: { color: colors.muted, fontSize: font.small, fontWeight: '700', marginTop: 10, marginBottom: 6 },
  menuRow: { paddingVertical: 10 },
  controlRow: { flexDirection: 'row', justifyContent: 'center', marginTop: 24 },
}))
