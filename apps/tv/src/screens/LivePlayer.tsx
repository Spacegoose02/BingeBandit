import React, { useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video, { SelectedTrackType, type AudioTrack, type OnProgressData, type TextTrack, type VideoRef } from 'react-native-video'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { api, authHeaders, type ApiError } from '../api'
import { Button } from '../components'
import { DEFAULT_BUFFER_SECS, clock, fmtBehind, programFacts, liveBuffer, reportBuffering, joinChannel, leaveChannel, pingViewer } from '../live'
import { fetchGameLights, saveGameLights, type GameLightsPerson } from '../lights'
import { useLock } from '../lock'
import { refreshScoreBug, scoreBugChannel } from '../scorebug'
import { backInterceptor, showLiveGuide, useNav, useScreen } from '../nav'
import { useLandscape } from '../screen'
import { px, scaled, colors, font, gutter, isPhone, quietPress } from '../theme'
import type { LiveChannel } from '../types'
import { NO_LIVE_BADGE } from './Player'

/** How long channel up/down waits for more presses before tuning. */
const TUNE_DELAY = 700

/** The channel last watched full screen, so the guide's preview can stay on it. */
export const lastChannel = { id: 0 }

/**
 * Watches a live channel. Up/Down changes channel; Left/Right rewind 10 s and
 * skip 30 s within the server's pause window; OK brings up the info bar with
 * buttons (pause, rewind, skip, live, guide); the info bar shows what's on.
 */
export default function LivePlayer({ channelId }: { channelId: number }) {
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [id, setId] = useState(channelId) // the channel shown in the info bar
  const [tuned, setTuned] = useState(channelId) // the channel being streamed
  const [attempt, setAttempt] = useState(0) // bumped to rejoin after a failure
  const [src, setSrc] = useState<string | null>(null)
  const [offset, setOffset] = useState(DEFAULT_BUFFER_SECS) // how far behind live "live" is
  const [error, setError] = useState('')
  const [reconnecting, setReconnecting] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [paused, setPaused] = useState(false)
  const [behind, setBehind] = useState(0)
  const nav = useNav()
  // Hidden under another screen: stay paused and leave the remote alone.
  const { active } = useScreen()
  const [info, setInfo] = useState(true)
  const [controls, setControls] = useState(false) // TV: the bar's buttons are up and hold focus
  // Options: Game Lights and the score bug for an easy on / off, and the
  // channel's sound and caption tracks when it has more than one.
  const [menu, setMenu] = useState(false)
  const [lights, setLights] = useState<GameLightsPerson | null>(null)
  const [scoreBug, setScoreBug] = useState<boolean | null>(null)
  const [audios, setAudios] = useState<AudioTrack[]>([])
  const [texts, setTexts] = useState<TextTrack[]>([])
  const [audio, setAudio] = useState(-1) // -1: the stream's own choice
  const [text, setText] = useState(-2) // -2: as the device's caption setting says; -1: off
  const [notice, setNotice] = useState('')
  const say = (t: string) => {
    setNotice(t)
    setTimeout(() => setNotice((n) => (n === t ? '' : n)), 4000)
  }
  const viewer = useRef('')
  const failures = useRef(0)
  const stallSecs = useRef(12)
  const failRef = useRef<(msg: string) => void>(() => undefined)
  const video = useRef<VideoRef>(null)
  const progress = useRef({ pos: 0, window: 0 })
  const hide = useRef<ReturnType<typeof setTimeout>>(undefined)
  const tuneTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const touchY = useRef(0)
  useLandscape()
  const insets = useSafeAreaInsets()
  const lock = useLock()
  const pausedRef = useRef(false)
  pausedRef.current = paused
  // A locked player swallows Back (the keypad unlocks it); Back then puts the buttons away.
  useEffect(() => {
    if (!active) return
    backInterceptor.current = () => {
      if (lock.handleBack()) return true
      if (menu) {
        setMenu(false)
        return true
      }
      if (controls) {
        setControls(false)
        return true
      }
      return false
    }
    return () => {
      backInterceptor.current = null
    }
  })

  // Refetched now and then so the info bar keeps up with what's on.
  useEffect(() => {
    const load = () => api.get<LiveChannel[]>('/api/live/channels').then(setChannels, () => undefined)
    load()
    const t = setInterval(load, 5 * 60 * 1000)
    return () => clearInterval(t)
  }, [])

  // A new channel starts with a clean slate.
  useEffect(() => {
    failures.current = 0
    setError('')
    setAudios([])
    setTexts([])
    setAudio(-1)
    setText(-2)
  }, [tuned])

  const openMenu = () => {
    setMenu(true)
    setControls(false)
    fetchGameLights().then(setLights)
    api.get<{ scoreBug?: boolean }>('/api/me/prefs').then((p) => setScoreBug(p.scoreBug !== false), () => setScoreBug(null))
  }
  const setLightsRunning = (run: boolean) => {
    if (!lights) return
    const was = lights
    const next = { ...lights, off: !run }
    setLights(next)
    saveGameLights(next).then(
      () => say(run ? 'Game Lights on' : 'Game Lights stopped'),
      (e: Error) => {
        setLights(was)
        say(e.message)
      },
    )
  }
  const pickScoreBug = (on: boolean) => {
    setScoreBug(on)
    api
      .get<Record<string, unknown>>('/api/me/prefs')
      .then((cur) => api.put('/api/me/prefs', { ...cur, scoreBug: on }))
      .then(
        () => {
          refreshScoreBug.current()
          say(on ? 'Score bug on' : 'Score bug off')
        },
        (e: Error) => {
          setScoreBug(!on)
          say(e.message)
        },
      )
  }

  // Join the channel's shared stream; leave it when switching or exiting.
  // Failures rejoin on their own (quickly at first, then every 15 s) so an
  // error never just sits on screen.
  useEffect(() => scoreBugChannel.set(tuned), [tuned])
  useEffect(() => () => scoreBugChannel.set(undefined), [])

  useEffect(() => {
    lastChannel.id = tuned
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
        setOffset(r.bufferSecs)
        stallSecs.current = r.stallSecs
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

  // The bar (and its buttons, if up) hides a while after the last key, unless paused.
  const showInfo = (keep = false, buttons = false) => {
    setInfo(true)
    if (buttons) setControls(true)
    if (hide.current) clearTimeout(hide.current)
    if (!keep) {
      hide.current = setTimeout(() => {
        if (pausedRef.current) return
        setInfo(false)
        setControls(false)
      }, buttons || controls ? 6000 : 5000)
    }
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
    const b = p.seekableDuration - p.currentTime - offset
    setBehind(b < 4 ? 0 : Math.round(b))
  }

  const seekBy = (secs: number) => {
    const { pos, window } = progress.current
    if (!window) return
    const live = Math.max(0, window - offset)
    video.current?.seek(Math.min(live, Math.max(1, pos + secs)))
    showInfo(paused)
  }

  const goLive = () => {
    const { window } = progress.current
    if (window) video.current?.seek(Math.max(0, window - offset))
    setPaused(false)
    showInfo()
  }

  const togglePause = () => {
    setPaused(!paused)
    showInfo(!paused) // the bar stays up while paused
  }

  const openGuide = () => {
    showLiveGuide.current?.()
    nav.back()
  }

  // ExoPlayer doesn't report progress while paused; live keeps moving on.
  useEffect(() => {
    if (!paused) return
    const t = setInterval(() => setBehind((b) => b + 1), 1000)
    return () => clearInterval(t)
  }, [paused])

  // Holding Select opens the guide (on the channel being watched). The key-up
  // that ends the hold mustn't also bring up the buttons.
  const held = useRef(false)
  useTVEventHandler((evt) => {
    if (!evt || !active) return
    if (evt.eventType === 'longSelect') {
      if (!held.current) {
        held.current = true
        openGuide()
      }
      return
    }
    if (evt.eventKeyAction === 0 || !active) return // act on key up, as in Player
    if (held.current && evt.eventType === 'select') {
      held.current = false
      return
    }
    if (menu) return // the menu's buttons handle the keys
    if (controls) {
      // The buttons have focus, so ◀▶ and OK work them. They're one row, so
      // ▲▼ still change channel.
      switch (evt.eventType) {
        case 'up':
          step(1)
          break
        case 'down':
          step(-1)
          break
        case 'channelUp':
          step(1)
          break
        case 'channelDown':
          step(-1)
          break
        case 'playPause':
          togglePause()
          break
        default:
          showInfo(pausedRef.current)
      }
      return
    }
    switch (evt.eventType) {
      // ▲ goes up the numbers, like CH+ (and a swipe up on a phone).
      case 'up':
        step(1)
        break
      case 'down':
        step(-1)
        break
      // The remote's CH+ / CH− (or P+ / P−) buttons, where it has them: up the numbers and down.
      case 'channelUp':
        step(1)
        break
      case 'channelDown':
        step(-1)
        break
      case 'select':
        showInfo(paused, true)
        break
      case 'playPause':
        togglePause()
        break
      case 'play':
        setPaused(false)
        showInfo()
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
        showInfo(paused, true)
        break
    }
  })

  // Buffering for longer than the server's stall time: tell it, so it can
  // move the channel to its next stream (it checks the stream really is the
  // problem first). If it didn't switch, the stream is fine and the player is
  // what's stuck (ExoPlayer can sit buffering for good after a gap in the
  // stream), so start the player over; if it did, give the new stream one
  // more stall period before doing the same.
  useEffect(() => {
    if (!buffering || paused || reconnecting || !src) return
    const started = Date.now()
    let next = stallSecs.current
    let switched = false
    let done = false
    const reload = () => {
      if (done) return
      done = true
      setAttempt((n) => n + 1)
    }
    const t = setInterval(() => {
      const secs = (Date.now() - started) / 1000
      if (secs < next) return
      next += stallSecs.current
      if (switched) return reload()
      reportBuffering(viewer.current, secs).then((s) => (s ? (switched = true) : reload()))
    }, 1000)
    return () => {
      done = true
      clearInterval(t)
    }
  }, [buffering, paused, reconnecting, src])

  const playing = () => {
    setBuffering(false)
    setReconnecting(false)
    setError('')
    failures.current = 0
  }

  const ch = channels.find((c) => c.id === id)
  // The channel list's "now" goes stale as the evening goes on: once it ends, its "next" is on.
  const nowSecs = Date.now() / 1000
  const now = ch?.now && ch.now.stop > nowSecs ? ch.now : ch?.next && ch.next.start <= nowSecs ? ch.next : ch?.now
  return (
    <View style={st.screen}>
      {src && (
        <Video
          key={`${src}#${attempt}`}
          ref={video}
          controlsStyles={NO_LIVE_BADGE}
          source={{ uri: src, type: 'm3u8', headers: authHeaders(), bufferConfig: liveBuffer(offset) }}
          style={StyleSheet.absoluteFill}
          resizeMode="contain"
          paused={paused || !active}
          onLoad={() => {
            playing()
            showInfo(paused) // a new channel shows its info bar, then hides it
          }}
          onBuffer={({ isBuffering }) => (isBuffering ? setBuffering(true) : playing())}
          onProgress={onProgress}
          onAudioTracks={(e) => setAudios(e.audioTracks)}
          onTextTracks={(e) => setTexts(e.textTracks)}
          selectedAudioTrack={audio >= 0 ? { type: SelectedTrackType.INDEX, value: audio } : undefined}
          selectedTextTrack={text >= 0 ? { type: SelectedTrackType.INDEX, value: text } : text === -1 ? { type: SelectedTrackType.DISABLED } : undefined}
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
        <Pressable android_disableSound={quietPress} style={StyleSheet.absoluteFill} hasTVPreferredFocus={!controls && !menu} focusable={!controls && !menu} />
      )}
      {(buffering || reconnecting || id !== tuned) && !error && !paused && <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />}
      {!!error && id === tuned && (
        <View style={st.error}>
          <Text style={st.errorText}>{error}</Text>
          <Text style={st.errorHint}>Retrying on its own{isPhone ? '' : ' · ▲▼ to pick another channel'}</Text>
        </View>
      )}
      {(info || paused || !!error) && ch && !lock.locked && !menu && (
        <View style={[st.bar, isPhone && { left: insets.left + px(gutter), right: insets.right + px(gutter) }]}>
          <View style={st.row}>
            <Text style={st.number}>{ch.number}</Text>
            <View style={st.flex}>
              <Text style={st.name} numberOfLines={1}>{ch.name}</Text>
              {now ? (
                <>
                  <Text style={st.title} numberOfLines={1}>{now.title}{now.year ? ` (${now.year})` : ''}{now.subtitle ? `  —  ${now.subtitle}` : ''}</Text>
                  <Text style={st.facts} numberOfLines={1}>{programFacts(now)}</Text>
                  {!!now.description && <Text style={st.desc} numberOfLines={paused ? 4 : 2}>{now.description}</Text>}
                </>
              ) : (
                <Text style={st.now}>No guide information</Text>
              )}
              {ch.next && <Text style={st.next} numberOfLines={1}>Next at {clock(ch.next.start)}: {ch.next.title}{ch.next.year ? ` (${ch.next.year})` : ''}</Text>}
            </View>
            {id === tuned && (paused || behind > 0) ? (
              <Text style={st.timeshift}>{paused ? '❚❚ Paused' : '◀◀'}{behind > 0 ? `  −${fmtBehind(behind)}` : ''}</Text>
            ) : (
              id === tuned && <Text style={st.liveTag}>LIVE</Text>
            )}
          </View>
          {isPhone || controls ? (
            <View style={st.buttons}>
              <Button small label="⟲ 10s" onPress={() => seekBy(-10)} />
              <Button small primary preferFocus label={paused ? '▶  Play' : '❚❚  Pause'} onPress={togglePause} />
              <Button small label="30s ⟳" onPress={() => seekBy(30)} />
              {/* Always there, so pressing it never leaves the remote's focus on nothing. */}
              <Button small label="Go live" onPress={goLive} />
              <Button small label="Guide" onPress={openGuide} />
              <Button small label="Options" onPress={openMenu} />
              {isPhone && (
                <Button
                  small
                  label="Lock"
                  onPress={() => {
                    setInfo(false)
                    lock.startLock()
                  }}
                />
              )}
            </View>
          ) : (
            <Text style={st.hint}>OK for controls   ▲▼ channel   ◀▶ rewind / skip   hold OK for the guide</Text>
          )}
        </View>
      )}
      {menu && (
        <View style={st.menu}>
          {lights && (
            <>
              <Text style={st.menuTitle}>Game Lights</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                <Button small primary={!lights.off} preferFocus label="On" onPress={() => setLightsRunning(true)} />
                <Button small primary={!!lights.off} label="Stopped" onPress={() => setLightsRunning(false)} />
              </ScrollView>
            </>
          )}
          {scoreBug !== null && (
            <>
              <Text style={st.menuTitle}>Score bug</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                <Button small primary={scoreBug} preferFocus={!lights} label="On" onPress={() => pickScoreBug(true)} />
                <Button small primary={!scoreBug} label="Off" onPress={() => pickScoreBug(false)} />
              </ScrollView>
            </>
          )}
          {audios.length > 1 && (
            <>
              <Text style={st.menuTitle}>Audio</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                {audios.map((a, i) => (
                  <Button
                    key={a.index}
                    small
                    primary={audio >= 0 ? audio === a.index : !!a.selected}
                    label={a.title || a.language || `Track ${i + 1}`}
                    onPress={() => setAudio(a.index)}
                  />
                ))}
              </ScrollView>
            </>
          )}
          {texts.length > 0 && (
            <>
              <Text style={st.menuTitle}>Captions</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                <Button small primary={text === -1 || (text === -2 && !texts.some((t) => t.selected))} label="Off" onPress={() => setText(-1)} />
                {texts.map((t, i) => (
                  <Button key={t.index} small primary={text === t.index || (text === -2 && !!t.selected)} label={t.title || t.language || `Captions ${i + 1}`} onPress={() => setText(t.index)} />
                ))}
              </ScrollView>
            </>
          )}
          <Text style={st.menuTitle}>Guide</Text>
          <ScrollView horizontal contentContainerStyle={st.menuRow}>
            <Button small preferFocus={!lights && scoreBug === null} label="Open the guide" onPress={openGuide} />
          </ScrollView>
          {!!notice && <Text style={st.hint}>{notice}</Text>}
          {isPhone ? <Button small label="Done" onPress={() => setMenu(false)} /> : <Text style={st.menuHint}>Back closes this menu</Text>}
        </View>
      )}
      {!menu && !!notice && <Text style={st.notice}>{notice}</Text>}
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
  bar: { position: 'absolute', left: gutter, right: gutter, bottom: 60, backgroundColor: 'rgba(20,24,29,0.92)', borderRadius: 22, padding: 34 },
  number: { color: colors.accent, fontSize: font.title, fontWeight: '900', width: 160 },
  name: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  now: { color: colors.muted, fontSize: font.body, marginTop: 4 },
  title: { color: colors.text, fontSize: font.body, fontWeight: '700', marginTop: 6 },
  facts: { color: colors.muted, fontSize: font.small, marginTop: 4 },
  desc: { color: colors.text, fontSize: font.small, marginTop: 8, opacity: 0.85 },
  next: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  row: { flexDirection: 'row', alignItems: 'center' },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 24, marginLeft: isPhone ? 0 : 160 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 20, marginLeft: 160 },
  menu: { position: 'absolute', left: 0, right: 0, bottom: 0, paddingHorizontal: gutter, paddingTop: 30, paddingBottom: 50, backgroundColor: 'rgba(11,13,16,0.94)' },
  menuTitle: { color: colors.muted, fontSize: font.small, fontWeight: '700', marginTop: 10, marginBottom: 6 },
  menuRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6 },
  menuHint: { color: colors.muted, fontSize: font.small, marginTop: 16 },
  notice: { position: 'absolute', top: 40, right: 40, color: colors.text, fontSize: font.small, backgroundColor: 'rgba(20,24,29,0.92)', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 12 },
}))
