import React, { useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video, { type OnProgressData, type VideoRef } from 'react-native-video'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { api, authHeaders, type ApiError } from '../api'
import { Button } from '../components'
import { LIVE_BUFFER, LIVE_OFFSET_SECS, fmtBehind, joinChannel, leaveChannel, pingViewer } from '../live'
import { useLock } from '../lock'
import { backInterceptor } from '../nav'
import { useLandscape } from '../screen'
import { px, scaled, colors, font, gutter, isPhone } from '../theme'
import type { LiveChannel } from '../types'
import { NO_LIVE_BADGE } from './Player'

/** How long channel up/down waits for more presses before tuning. */
const TUNE_DELAY = 700

/**
 * Watches a live channel. Up/Down changes channel; OK pauses; Left/Right
 * rewind 10 s and skip 30 s within the server's pause window; the info bar
 * shows what's on.
 */
export default function LivePlayer({ channelId }: { channelId: number }) {
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [id, setId] = useState(channelId) // the channel shown in the info bar
  const [tuned, setTuned] = useState(channelId) // the channel being streamed
  const [attempt, setAttempt] = useState(0) // bumped to rejoin after a failure
  const [src, setSrc] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [reconnecting, setReconnecting] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [paused, setPaused] = useState(false)
  const [behind, setBehind] = useState(0)
  const [info, setInfo] = useState(true)
  const viewer = useRef('')
  const failures = useRef(0)
  const failRef = useRef<(msg: string) => void>(() => undefined)
  const video = useRef<VideoRef>(null)
  const progress = useRef({ pos: 0, window: 0 })
  const hide = useRef<ReturnType<typeof setTimeout>>(undefined)
  const tuneTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const touchY = useRef(0)
  useLandscape()
  const insets = useSafeAreaInsets()
  const lock = useLock()
  // A locked player swallows Back (the keypad unlocks it).
  useEffect(() => {
    backInterceptor.current = lock.handleBack
    return () => {
      backInterceptor.current = null
    }
  })

  useEffect(() => {
    api.get<LiveChannel[]>('/api/live/channels').then(setChannels, () => undefined)
  }, [])

  // A new channel starts with a clean slate.
  useEffect(() => {
    failures.current = 0
    setError('')
  }, [tuned])

  // Join the channel's shared stream; leave it when switching or exiting.
  // Failures rejoin on their own (quickly at first, then every 15 s) so an
  // error never just sits on screen.
  useEffect(() => {
    let cancelled = false
    let retry: ReturnType<typeof setTimeout> | undefined
    let ping: ReturnType<typeof setInterval> | undefined
    setSrc(null)
    setBuffering(true)
    setPaused(false)
    setBehind(0)
    const fail = (msg: string) => {
      if (cancelled) return
      cancelled = true
      setSrc(null)
      leaveChannel(viewer.current)
      viewer.current = ''
      failures.current++
      if (failures.current > 3) setError(msg)
      setReconnecting(true)
      retry = setTimeout(() => setAttempt((n) => n + 1), failures.current > 3 ? 15000 : 1500 * failures.current)
    }
    failRef.current = fail
    joinChannel(tuned).then(
      (r) => {
        if (cancelled) {
          leaveChannel(r.viewerId)
          return
        }
        viewer.current = r.viewerId
        setSrc(r.playlist)
        ping = setInterval(() => {
          pingViewer(r.viewerId).catch((e: ApiError) => e.status === 410 && fail('This channel stopped.'))
        }, 10000)
      },
      (e: Error) => fail(e.message || 'This channel is unavailable right now.'),
    )
    return () => {
      cancelled = true
      if (retry) clearTimeout(retry)
      if (ping) clearInterval(ping)
      leaveChannel(viewer.current)
      viewer.current = ''
    }
  }, [tuned, attempt])

  useEffect(() => () => {
    if (tuneTimer.current) clearTimeout(tuneTimer.current)
    if (hide.current) clearTimeout(hide.current)
  }, [])

  const showInfo = (keep = false) => {
    setInfo(true)
    if (hide.current) clearTimeout(hide.current)
    if (!keep) hide.current = setTimeout(() => setInfo(false), 5000)
  }

  // Flipping through channels only updates the info bar; the stream is
  // joined once the presses stop, so each channel passed over doesn't open
  // (and tie up) a provider connection.
  const step = (dir: number) => {
    if (channels.length === 0) return
    const i = channels.findIndex((c) => c.id === id)
    const next = channels[(i + dir + channels.length) % channels.length].id
    setId(next)
    showInfo()
    if (tuneTimer.current) clearTimeout(tuneTimer.current)
    tuneTimer.current = setTimeout(() => setTuned(next), TUNE_DELAY)
  }

  const onProgress = (p: OnProgressData) => {
    progress.current = { pos: p.currentTime, window: p.seekableDuration }
    const b = p.seekableDuration - p.currentTime - LIVE_OFFSET_SECS
    setBehind(b < 4 ? 0 : Math.round(b))
  }

  const seekBy = (secs: number) => {
    const { pos, window } = progress.current
    if (!window) return
    const live = Math.max(0, window - LIVE_OFFSET_SECS)
    video.current?.seek(Math.min(live, Math.max(1, pos + secs)))
    showInfo(paused)
  }

  const goLive = () => {
    const { window } = progress.current
    if (window) video.current?.seek(Math.max(0, window - LIVE_OFFSET_SECS))
    setPaused(false)
    showInfo()
  }

  const togglePause = () => {
    setPaused(!paused)
    showInfo(!paused) // the bar stays up while paused
  }

  // ExoPlayer doesn't report progress while paused; live keeps moving on.
  useEffect(() => {
    if (!paused) return
    const t = setInterval(() => setBehind((b) => b + 1), 1000)
    return () => clearInterval(t)
  }, [paused])

  useTVEventHandler((evt) => {
    if (!evt || evt.eventKeyAction === 0) return // act on key up, as in Player
    switch (evt.eventType) {
      case 'up':
        step(-1)
        break
      case 'down':
        step(1)
        break
      case 'select':
      case 'playPause':
        togglePause()
        break
      case 'play':
        setPaused(false)
        break
      case 'pause':
        setPaused(true)
        showInfo(true)
        break
      case 'left':
      case 'rewind':
        seekBy(-10)
        break
      case 'right':
      case 'fastForward':
        seekBy(30)
        break
      case 'info':
      case 'menu':
        showInfo()
        break
    }
  })

  const playing = () => {
    setBuffering(false)
    setReconnecting(false)
    setError('')
    failures.current = 0
  }

  const ch = channels.find((c) => c.id === id)
  return (
    <View style={st.screen}>
      {src && (
        <Video
          key={`${src}#${attempt}`}
          ref={video}
          controlsStyles={NO_LIVE_BADGE}
          source={{ uri: src, type: 'm3u8', headers: authHeaders(), bufferConfig: LIVE_BUFFER }}
          style={StyleSheet.absoluteFill}
          resizeMode="contain"
          paused={paused}
          onLoad={playing}
          onBuffer={({ isBuffering }) => (isBuffering ? setBuffering(true) : playing())}
          onProgress={onProgress}
          progressUpdateInterval={1000}
          onError={() => failRef.current('This channel stopped.')}
        />
      )}
      {isPhone ? (
        // Phone: swipe up for the next channel, down for the previous; tap for the info bar.
        <View
          style={StyleSheet.absoluteFill}
          onStartShouldSetResponder={() => true}
          onResponderGrant={(e) => (touchY.current = e.nativeEvent.pageY)}
          onResponderRelease={(e) => {
            const dy = e.nativeEvent.pageY - touchY.current
            if (lock.locked) lock.poke()
            else if (Math.abs(dy) > 60) step(dy < 0 ? 1 : -1)
            else if (info) setInfo(false)
            else showInfo(paused)
          }}
        />
      ) : (
        // Something must hold focus or Android never hands the remote's keys to the app.
        <Pressable style={StyleSheet.absoluteFill} hasTVPreferredFocus focusable />
      )}
      {(buffering || reconnecting || id !== tuned) && !error && !paused && <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />}
      {!!error && id === tuned && (
        <View style={st.error}>
          <Text style={st.errorText}>{error}</Text>
          <Text style={st.errorHint}>Retrying on its own{isPhone ? '' : ' · ▲▼ to pick another channel'}</Text>
        </View>
      )}
      {(info || paused || !!error) && ch && !lock.locked && (
        <View style={[st.bar, isPhone && { left: insets.left + px(gutter), right: insets.right + px(gutter) }]}>
          <Text style={st.number}>{ch.number}</Text>
          <View style={st.flex}>
            <Text style={st.name}>{ch.name}</Text>
            <Text style={st.now} numberOfLines={1}>{ch.now ? `Now: ${ch.now.title}` : 'No guide information'}{ch.next ? `   ·   Next: ${ch.next.title}` : ''}</Text>
          </View>
          {id === tuned && (paused || behind > 0) ? (
            <Text style={st.timeshift}>{paused ? '❚❚ Paused' : '◀◀'}{behind > 0 ? `  −${fmtBehind(behind)}` : ''}</Text>
          ) : (
            id === tuned && <Text style={st.liveTag}>LIVE</Text>
          )}
          {isPhone ? (
            <>
              <Button small label="−10s" style={st.lockBtn} onPress={() => seekBy(-10)} />
              <Button small label={paused ? 'Play' : 'Pause'} style={st.lockBtn} onPress={togglePause} />
              <Button small label="+30s" style={st.lockBtn} onPress={() => seekBy(30)} />
              {behind > 0 && <Button small label="Live" style={st.lockBtn} onPress={goLive} />}
            </>
          ) : (
            <Text style={st.hint}>▲▼ channel   OK pause   ◀▶ rewind / skip</Text>
          )}
          {isPhone && (
            <Button
              small
              label="Lock"
              style={st.lockBtn}
              onPress={() => {
                setInfo(false)
                lock.startLock()
              }}
            />
          )}
        </View>
      )}
      {lock.layer}
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: '#000' },
  flex: { flex: 1 },
  spinner: { position: 'absolute', top: '50%', left: '50%', marginLeft: -18, marginTop: -18 },
  error: { position: 'absolute', top: '40%', alignSelf: 'center', alignItems: 'center', backgroundColor: 'rgba(20,24,29,0.95)', padding: 30, borderRadius: 16 },
  errorText: { color: colors.text, fontSize: font.body },
  errorHint: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  liveTag: { color: '#fff', backgroundColor: '#e11d48', fontSize: font.small, fontWeight: '800', paddingHorizontal: 12, paddingVertical: 4, borderRadius: 6, marginLeft: 20 },
  timeshift: { color: colors.text, fontSize: font.body, fontWeight: '700', marginLeft: 20 },
  bar: { position: 'absolute', left: gutter, right: gutter, bottom: 60, flexDirection: 'row', alignItems: 'center', backgroundColor: 'rgba(20,24,29,0.92)', borderRadius: 22, padding: 34 },
  number: { color: colors.accent, fontSize: font.title, fontWeight: '900', width: 160 },
  name: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  now: { color: colors.muted, fontSize: font.body, marginTop: 4 },
  hint: { color: colors.muted, fontSize: font.small, marginLeft: 20, flexShrink: 0 }, // the Now/Next line gives way instead
  lockBtn: { marginLeft: 24, marginRight: 0, marginBottom: 0 },
}))
