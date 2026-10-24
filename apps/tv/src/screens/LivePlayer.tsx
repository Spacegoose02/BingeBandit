import React, { useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video from 'react-native-video'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { absolute, api, authHeaders } from '../api'
import { Button } from '../components'
import { useLock } from '../lock'
import { backInterceptor } from '../nav'
import { useLandscape } from '../screen'
import { px, scaled, colors, font, gutter, isPhone } from '../theme'
import type { LiveChannel } from '../types'
import { NO_LIVE_BADGE } from './Player'

/** The channel last watched full screen, so the guide's preview can stay on it. */
export const lastChannel = { id: 0 }

/** Watches a live channel. Up/Down changes channel; the info bar shows what's on. */
export default function LivePlayer({ channelId }: { channelId: number }) {
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [id, setId] = useState(channelId)
  const [src, setSrc] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [buffering, setBuffering] = useState(true)
  const [info, setInfo] = useState(true)
  const viewer = useRef('')
  const hide = useRef<ReturnType<typeof setTimeout>>(undefined)
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

  // Join the channel's shared stream; leave it when switching or exiting.
  useEffect(() => {
    lastChannel.id = id
    let cancelled = false
    setSrc(null)
    setError('')
    setBuffering(true)
    api.post<{ viewerId: string; playlist: string }>(`/api/live/channels/${id}/watch`).then(
      (r) => {
        if (cancelled) {
          api.del(`/api/live/viewers/${r.viewerId}`).catch(() => undefined)
          return
        }
        viewer.current = r.viewerId
        setSrc(absolute(r.playlist))
      },
      (e) => !cancelled && setError(e.message),
    )
    setInfo(true)
    if (hide.current) clearTimeout(hide.current)
    hide.current = setTimeout(() => setInfo(false), 5000)
    return () => {
      cancelled = true
      if (viewer.current) api.del(`/api/live/viewers/${viewer.current}`).catch(() => undefined)
      viewer.current = ''
    }
  }, [id])

  const step = (dir: number) => {
    if (channels.length === 0) return
    const i = channels.findIndex((c) => c.id === id)
    setId(channels[(i + dir + channels.length) % channels.length].id)
  }

  useTVEventHandler((evt) => {
    if (!evt || evt.eventKeyAction === 0) return // act on key up, as in Player
    if (evt.eventType === 'up') step(-1)
    else if (evt.eventType === 'down') step(1)
    else if (evt.eventType === 'select' || evt.eventType === 'left' || evt.eventType === 'right') showInfo()
  })

  const showInfo = () => {
    setInfo(true)
    if (hide.current) clearTimeout(hide.current)
    hide.current = setTimeout(() => setInfo(false), 5000)
  }

  const ch = channels.find((c) => c.id === id)
  return (
    <View style={st.screen}>
      {src && (
        <Video
          key={src}
          controlsStyles={NO_LIVE_BADGE}
          source={{ uri: src, type: 'm3u8', headers: authHeaders() }}
          style={StyleSheet.absoluteFill}
          resizeMode="contain"
          onLoad={() => setBuffering(false)}
          onBuffer={({ isBuffering }) => setBuffering(isBuffering)}
          onError={() => setError('This channel stopped. Try another one.')}
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
            else showInfo()
          }}
        />
      ) : (
        // Something must hold focus or Android never hands the remote's keys to the app.
        <Pressable style={StyleSheet.absoluteFill} hasTVPreferredFocus focusable />
      )}
      {buffering && !error && <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />}
      {!!error && <Text style={st.error}>{error}</Text>}
      {(info || !!error) && ch && !lock.locked && (
        <View style={[st.bar, isPhone && { left: insets.left + px(gutter), right: insets.right + px(gutter) }]}>
          <Text style={st.number}>{ch.number}</Text>
          <View style={st.flex}>
            <Text style={st.name}>{ch.name}</Text>
            <Text style={st.now} numberOfLines={1}>{ch.now ? `Now: ${ch.now.title}` : 'No guide information'}{ch.next ? `   ·   Next: ${ch.next.title}` : ''}</Text>
          </View>
          <Text style={st.hint}>{isPhone ? 'Swipe up or down to change channel' : '▲▼ change channel'}</Text>
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
  error: { position: 'absolute', top: '45%', alignSelf: 'center', color: colors.text, fontSize: font.body, backgroundColor: 'rgba(20,24,29,0.95)', padding: 30, borderRadius: 16 },
  bar: { position: 'absolute', left: gutter, right: gutter, bottom: 60, flexDirection: 'row', alignItems: 'center', backgroundColor: 'rgba(20,24,29,0.92)', borderRadius: 22, padding: 34 },
  number: { color: colors.accent, fontSize: font.title, fontWeight: '900', width: 160 },
  name: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  now: { color: colors.muted, fontSize: font.body, marginTop: 4 },
  hint: { color: colors.muted, fontSize: font.small, marginLeft: 20, flexShrink: 0 }, // the Now/Next line gives way instead
  lockBtn: { marginLeft: 24, marginRight: 0, marginBottom: 0 },
}))
