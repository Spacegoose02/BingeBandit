import React, { useEffect, useRef, useState } from 'react'
import { ActivityIndicator, StyleSheet, View } from 'react-native'
import Video from 'react-native-video'
import { ApiError, authHeaders } from '../api'
import { DEFAULT_BUFFER_SECS, joinChannel, leaveChannel, liveBuffer, pingViewer, reportBuffering } from '../live'
import { colors } from '../theme'
import { NO_LIVE_BADGE } from './Player'

/**
 * A channel playing small (the guide's corner, the sports preview). It keeps
 * itself going the way the full-screen player does: it sits the server's
 * cushion behind live, pings so the server keeps it counted, and starts over
 * when the stream errors, ends or sits buffering. Without that, the first gap
 * in the stream left ExoPlayer buffering for good (or erroring with nothing
 * to catch it) and the preview just stopped, usually within a minute or so.
 */
export function LivePreview({ channelId, muted }: { channelId?: number; muted?: boolean }) {
  const [src, setSrc] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0) // bumped to start over
  const [buffer, setBuffer] = useState(DEFAULT_BUFFER_SECS)
  const viewer = useRef('')
  const stallSecs = useRef(12)
  const failures = useRef(0)
  const [buffering, setBuffering] = useState(true)
  const restartRef = useRef<() => void>(() => undefined)

  // A new channel starts the retry count over.
  useEffect(() => {
    failures.current = 0
  }, [channelId])

  useEffect(() => {
    setSrc(null)
    if (!channelId) return
    let cancelled = false
    let retry: ReturnType<typeof setTimeout> | undefined
    let ping: ReturnType<typeof setInterval> | undefined
    // Leave and join again, quickly at first and then every 15 s.
    const restart = () => {
      if (cancelled) return
      cancelled = true
      if (ping) clearInterval(ping)
      setSrc(null)
      leaveChannel(viewer.current)
      viewer.current = ''
      failures.current++
      retry = setTimeout(() => setAttempt((n) => n + 1), failures.current > 3 ? 15000 : 1500 * failures.current)
    }
    restartRef.current = restart
    joinChannel(channelId).then(
      (r) => {
        if (cancelled) {
          leaveChannel(r.viewerId)
          return
        }
        viewer.current = r.viewerId
        stallSecs.current = r.stallSecs
        setBuffer(r.bufferSecs)
        setBuffering(true)
        setSrc(r.playlist)
        ping = setInterval(() => {
          pingViewer(r.viewerId).catch((e: ApiError) => e.status === 410 && restart())
        }, 10000)
      },
      () => restart(),
    )
    return () => {
      cancelled = true
      if (retry) clearTimeout(retry)
      if (ping) clearInterval(ping)
      leaveChannel(viewer.current)
      viewer.current = ''
    }
  }, [channelId, attempt])

  // Buffering for longer than the server's stall time: tell the server (it
  // may move the channel to its next stream), then start the preview over
  // unless it did. A switched stream gets one more stall period. The first
  // picture may take a while, so that wait includes the cushion.
  useEffect(() => {
    if (!buffering || !src) return
    const started = Date.now()
    let next = stallSecs.current + buffer
    let switched = false
    let done = false
    const t = setInterval(() => {
      const secs = (Date.now() - started) / 1000
      if (secs < next) return
      next += stallSecs.current
      if (switched) return restartRef.current()
      reportBuffering(viewer.current, secs).then((s) => {
        if (done) return
        if (s === 'switched') switched = true
        else if (s === 'player') restartRef.current() // a late stream: keep waiting
      })
    }, 1000)
    return () => {
      done = true
      clearInterval(t)
    }
  }, [buffering, src, buffer])

  if (!src) return <ActivityIndicator color={colors.accent} />
  return (
    // The video surface would swallow taps meant for the preview (tap to watch).
    <View style={StyleSheet.absoluteFill} pointerEvents="none">
      <Video
        key={`${src}#${attempt}`}
        source={{ uri: src, type: 'm3u8', headers: authHeaders(), bufferConfig: liveBuffer(buffer) }}
        style={StyleSheet.absoluteFill}
        resizeMode="cover"
        muted={muted}
        controlsStyles={NO_LIVE_BADGE}
        focusable={false}
        onLoad={() => setBuffering(false)}
        onBuffer={({ isBuffering }) => {
          setBuffering(isBuffering)
          if (!isBuffering) failures.current = 0 // playing again
        }}
        onEnd={() => restartRef.current()} // a live stream never ends; its playlist looked finished
        onError={() => restartRef.current()}
      />
    </View>
  )
}
