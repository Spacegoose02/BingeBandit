import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, TVFocusGuideView, View, useTVEventHandler } from 'react-native'
import Video, { SelectedTrackType, type OnProgressData, type VideoRef } from 'react-native-video'
import { absolute, api, authHeaders, current, trackWrite } from '../api'
import { refreshAudioSupport, surroundPlayable } from '../audioSupport'
import { hueArea, hueSyncOn } from '../devicePrefs'
import { Button, Focusable } from '../components'
import { fetchHueMode, hueModeNames, saveHueMode, type HueMode } from '../lights'
import { backInterceptor, useNav } from '../nav'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { useLock } from '../lock'
import { useLandscape } from '../screen'
import { roomPosition, TogetherPanel, useTogether } from '../together'
import { px, scaled, colors, font, gutter, isPhone } from '../theme'
import { cueAt, IMAGE_SUBS, parseVtt, pickTracks, streamLabel, TEXT_SUBS, type Cue, type MediaStream, type Prefs } from '../tracks'
import type { Item, ItemDetail, MediaFile } from '../types'

const REPORT_EVERY = 10 // seconds
const STALL_MS = 8000 // direct play with no progress this long falls back to the transcoder
const LADDER = [2160, 1080, 720, 480]

// What ExoPlayer plays everywhere without extra decoders. Containers are the
// file extension; the rest are ffprobe codec names.
const DIRECT_CONTAINERS = new Set(['mp4', 'm4v', 'mov', 'mkv', 'webm', 'ts', 'm2ts', 'mts'])
const DIRECT_VIDEO = new Set(['h264', 'hevc', 'vp9', 'av1'])
// Audio Android decodes in software on every device.
const DIRECT_AUDIO = new Set(['aac', 'mp3', 'opus', 'vorbis', 'flac'])
// Dolby and DTS play only with a decoder on the device or passthrough to the
// TV/receiver. Without either ExoPlayer plays the video silently, so ask first.
const SURROUND_AUDIO = new Set(['ac3', 'eac3', 'dts', 'truehd'])

/** The audio formats to ask the server for when converting. */
function surroundCaps(): { audioCodecs: string[]; surroundCodec?: string } {
  const dolby = ['ac3', 'eac3'].filter(surroundPlayable)
  return { audioCodecs: ['aac', ...dolby], surroundCodec: dolby[0] }
}

/** Whether this device can play the file as is with the chosen audio track. */
function canDirectPlay(f: MediaFile, audioIndex: number): boolean {
  if (!DIRECT_CONTAINERS.has(f.container) || (f.videoCodec && !DIRECT_VIDEO.has(f.videoCodec))) return false
  const audios = (f.streams ?? []).filter((s) => s.type === 'audio')
  const a = audios.find((s) => s.index === audioIndex) ?? audios.find((s) => s.default) ?? audios[0]
  const codec = a?.codec ?? f.audioCodec
  if (!codec) return true // no audio at all
  return DIRECT_AUDIO.has(codec) || (SURROUND_AUDIO.has(codec) && surroundPlayable(codec))
}

function resolutionLabel(h: number): string {
  return h >= 2160 ? '4K' : `${h}p`
}
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

interface PreRollStep {
  url: string
  label: string
}

/**
 * Plays what the viewer turned on before a movie that starts from the
 * beginning: trailer files of other movies, then the server's intro video.
 * (YouTube trailers only play in the web player.)
 */
export default function Player({ id, start = 0, together }: { id: number; start?: number; together?: string }) {
  // Watching together skips the trailers and intro, to stay with the others.
  const [steps, setSteps] = useState<PreRollStep[] | undefined>(start > 0 || together ? [] : undefined)
  useEffect(() => {
    if (start > 0 || together) return
    let stale = false
    api
      .get<{ trailers?: { url?: string; title: string }[]; intro?: { url: string } }>(`/api/items/${id}/preroll`)
      .then((p) => {
        if (stale) return
        const out: PreRollStep[] = []
        for (const t of p.trailers ?? []) if (t.url) out.push({ url: t.url, label: `Trailer · ${t.title}` })
        if (p.intro) out.push({ url: p.intro.url, label: '' })
        setSteps(out)
      })
      .catch(() => !stale && setSteps([]))
    return () => {
      stale = true
    }
  }, [id, start, together])
  const next = useCallback(() => setSteps((s) => s?.slice(1)), [])
  const skipAll = useCallback(() => setSteps([]), [])
  if (steps === undefined) {
    return (
      <View style={st.screen}>
        <ActivityIndicator style={st.spinner} color={colors.accent} size="large" />
      </View>
    )
  }
  if (steps.length) {
    const s = steps[0]
    return <IntroVideo key={s.url} uri={absolute(s.url)} label={s.label} onDone={next} onSkipAll={steps.length > 1 ? skipAll : undefined} />
  }
  return <MediaPlayer id={id} start={start} together={together} />
}

/** A movie's own trailer file, from its details page. Back or the end returns. */
export function TrailerScreen({ url, title }: { url: string; title: string }) {
  const nav = useNav()
  return <IntroVideo uri={absolute(url)} label={`Trailer · ${title}`} onDone={nav.back} doneLabel="Close" />
}

function IntroVideo({
  uri,
  label,
  onDone,
  onSkipAll,
  doneLabel,
}: {
  uri: string
  label?: string
  onDone: () => void
  onSkipAll?: () => void
  doneLabel?: string
}) {
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
      {!!label && <Text style={st.introLabel}>{label}</Text>}
      <View style={st.introSkip}>
        {onSkipAll && <Button label="Skip all" small onPress={onSkipAll} />}
        <Button label={doneLabel ?? (onSkipAll ? 'Skip' : 'Start movie')} small onPress={onDone} preferFocus />
      </View>
    </View>
  )
}

/**
 * Plays a movie or episode. Direct play first (Android TV decodes most files
 * itself); if the device can't, the server converts it to HLS.
 */
function MediaPlayer({ id, start = 0, together }: { id: number; start?: number; together?: string }) {
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
  // -1: play the file as is when this device can; 0: have the server convert
  // it at its own size; otherwise the converted height.
  const [quality, setQuality] = useState(-1)
  const qualityRef = useRef(-1)
  const [notice, setNotice] = useState('')
  const say = (text: string) => {
    setNotice(text)
    setTimeout(() => setNotice((n) => (n === text ? '' : n)), 5000)
  }

  const audioRef = useRef(-1) // what the transcoder is told
  const burnRef = useRef(-1) // image subtitle stream burned in by the server
  const offset = useRef(0) // transcodes start at `offset` seconds into the file
  const transcodeId = useRef('')
  const playSession = useRef(sessionId())
  const played = useRef(0)
  const lastTime = useRef(0)
  const lastReport = useRef(0)
  const lastReportAt = useRef(0) // wall clock of the last report, in ms
  const posRef = useRef(start)
  const hideTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const finished = useRef(false)

  const duration = file?.duration || (item?.runtime ? item.runtime * 60 : 0)

  // Load the item, its file and the viewer's track preferences, then start.
  useEffect(() => {
    Promise.all([
      api.get<ItemDetail>(`/api/items/${id}`),
      api.get<Prefs>('/api/me/prefs').catch(() => null),
      refreshAudioSupport(),
    ]).then(
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
        } else if (!canDirectPlay(f, picked.audio)) {
          // A container, codec or audio format this device can't play: convert from the start.
          if (picked.sub) setSub(picked.sub)
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
      lastReportAt.current = Date.now()
      const sent = api
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
          lights: hueSyncOn(),
          lightsArea: hueArea(),
          played: Math.round(played.current),
        })
        .catch(() => undefined)
      if (stopped) trackWrite(sent)
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
          maxHeight: Math.max(qualityRef.current, 0),
          audioStream: audioRef.current,
          burnSubtitle: burnRef.current,
          replace: transcodeId.current,
          // At the file's own size ExoPlayer switches qualities itself when
          // the server offers several; a picked quality stays fixed.
          adaptive: qualityRef.current <= 0,
          // Picked "Convert", or direct play stuttered: encode H.264 too
          // instead of copying it, so a slow device gets something lighter.
          encodeVideo: qualityRef.current >= 0,
          // Android's AAC decoder handles 5.1 everywhere (and mixes down for
          // stereo TVs), so surround tracks stay surround. AC-3/E-AC-3 the
          // box can decode or pass through to a receiver is copied as is,
          // and DTS/TrueHD becomes Dolby Digital for it (a soundbar on HDMI
          // ARC takes Dolby but not 5.1 PCM).
          ...surroundCaps(),
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

  const chooseQuality = (q: number) => {
    setQuality(q)
    qualityRef.current = q
    setMenu(false)
    if (q < 0 && file && burnRef.current < 0 && canDirectPlay(file, audioRef.current)) {
      if (transcodeId.current) api.del(`/api/transcode/${transcodeId.current}`).catch(() => undefined)
      transcodeId.current = ''
      offset.current = 0
      lastTime.current = 0
      setMode('direct')
      setBuffering(true)
      setSrc({ uri: absolute(`/api/files/${file.id}/direct`), startPosition: posRef.current * 1000 })
    } else {
      transcode(posRef.current)
    }
  }

  // Direct play that keeps stopping to buffer: the device or the network
  // can't keep up with the file, so have the server convert it.
  const stalls = useRef<number[]>([])
  const lastSeek = useRef(0)
  const onStall = () => {
    const now = Date.now()
    // Buffering right after starting or seeking is expected.
    if (mode !== 'direct' || qualityRef.current >= 0 || lastTime.current < 5 || now - lastSeek.current < 4000) return
    stalls.current = [...stalls.current.filter((t) => now - t < 60000), now]
    if (stalls.current.length < 3) return
    stalls.current = []
    setNotice('Playback kept stopping, so the server is converting this video.')
    setTimeout(() => setNotice(''), 6000)
    qualityRef.current = 0
    setQuality(0)
    transcode(posRef.current)
  }

  const chooseAudio = (index: number) => {
    setAudio(index)
    audioRef.current = index
    if (mode === 'transcode') transcode(posRef.current) // the server mixes the new track
    else if (file && !canDirectPlay(file, index)) transcode(posRef.current) // no decoder for that track here
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

  // Position-based reports stop while paused or buffering, and the server
  // drops a player from Now Playing after 45 s without one.
  useEffect(() => {
    const t = setInterval(() => {
      if (lastReportAt.current && Date.now() - lastReportAt.current >= REPORT_EVERY * 1000) report()
    }, 5000)
    return () => clearInterval(t)
  }, [report])

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
        playNext(next.id)
        return
      }
    }
    nav.back()
  }

  const seekTo = (t: number) => {
    t = Math.max(0, duration ? Math.min(t, duration - 2) : t)
    posRef.current = t
    setPos(t)
    lastSeek.current = Date.now()
    if (mode === 'direct') video.current?.seek(t)
    else transcode(t) // the server converts from the new point
  }

  // Watch Together: everyone else's play, pause, seek and next episode
  // arrive here; this viewer's own go out through userPause and userSeek.
  const [roomId, setRoomId] = useState<string | null>(together ?? null)
  const [togetherOpen, setTogetherOpen] = useState(false)
  const [summary, setSummary] = useState(false)
  // Hue Sync's quick switch in Options: only once this profile turned it on.
  const [hueMode, setHueMode] = useState<HueMode | null>(null)
  const [hueSeen, setHueSeen] = useState(false)
  useEffect(() => {
    fetchHueMode().then((m) => {
      setHueMode(m)
      setHueSeen(m !== null && m !== 'off')
    })
  }, [])
  const pickHue = (m: HueMode) => {
    const was = hueMode
    setHueMode(m)
    saveHueMode(m).then(
      () => say(m === 'off' ? 'Hue Sync off' : `Hue Sync: ${hueModeNames[m]}`),
      (e) => {
        setHueMode(was)
        say(e.message)
      },
    )
  }
  // Scrubbing on the focused progress bar (TV): ◀ / ▶ move a marker, faster
  // the more they're pressed in a row; it seeks once they stop, or on OK.
  const [seekFocused, setSeekFocused] = useState(false)
  const [scrub, setScrub] = useState<number | null>(null)
  const scrubRef = useRef<number | null>(null)
  const scrubPresses = useRef({ n: 0, at: 0 })
  const scrubTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const commitScrub = () => {
    if (scrubTimer.current) clearTimeout(scrubTimer.current)
    const t = scrubRef.current
    scrubRef.current = null
    setScrub(null)
    if (t !== null) userSeek(t)
  }
  const nudgeScrub = (dir: 1 | -1) => {
    const now = Date.now()
    const p = scrubPresses.current
    p.n = now - p.at < 700 ? p.n + 1 : 0
    p.at = now
    const step = p.n < 3 ? 10 : p.n < 8 ? 30 : 60
    const from = scrubRef.current ?? posRef.current
    const t = Math.max(0, Math.min(duration || from + step, from + dir * step))
    scrubRef.current = t
    setScrub(t)
    if (scrubTimer.current) clearTimeout(scrubTimer.current)
    scrubTimer.current = setTimeout(commitScrub, 900)
  }
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const me = current()?.user.id
  const tg = useTogether(roomId, (s, skew) => {
    const want = roomPosition(s, skew)
    if (s.itemId !== id) {
      nav.replace({ name: 'player', id: s.itemId, start: Math.max(1, want), together: roomId ?? undefined })
      return
    }
    if (Math.abs(want - posRef.current) > 2) seekTo(want)
    setPaused(s.paused)
    if (s.by !== me && s.byName) say(`${s.byName} ${s.paused ? 'paused' : 'played'}`)
  })
  useEffect(() => {
    if (!tg.ended) return
    setRoomId(null)
    say('The watch party has ended.')
  }, [tg.ended])
  const userPause = (p: boolean) => {
    setPaused(p)
    tg.send({ paused: p, position: posRef.current })
  }
  const userSeek = (t: number) => {
    seekTo(t)
    tg.send({ paused: pausedRef.current, position: posRef.current })
  }
  const playNext = (nextId: number) => {
    tg.send({ itemId: nextId, paused: false, position: 0 })
    nav.replace({ name: 'player', id: nextId, start: 0, together: roomId ?? undefined })
  }
  const startTogether = async () => {
    const r = await api.post<{ id: string }>('/api/together', { itemId: id, position: posRef.current, paused: pausedRef.current })
    setRoomId(r.id)
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
      if (togetherOpen) {
        setTogetherOpen(false)
        return true
      }
      if (summary) {
        setSummary(false)
        return true
      }
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
    if (menu || togetherOpen || summary) return // the menu's buttons handle the keys
    if (controls === 'full') {
      // The buttons have focus; any key just keeps the controls up.
      if (!paused) showControls('full')
      if (seekFocused && (evt.eventType === 'left' || evt.eventType === 'right')) {
        nudgeScrub(evt.eventType === 'left' ? -1 : 1)
        return
      }
      if (evt.eventType === 'playPause') userPause(!pausedRef.current)
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
        userPause(!pausedRef.current)
        showControls('full')
        break
      case 'play':
        userPause(false)
        break
      case 'pause':
        userPause(true)
        showControls('full')
        break
      case 'left':
      case 'rewind':
        userSeek(posRef.current - 10)
        showControls('seek')
        break
      case 'right':
      case 'fastForward':
        userSeek(posRef.current + 30)
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
          onBuffer={({ isBuffering }) => {
            setBuffering(isBuffering)
            if (isBuffering) onStall()
          }}
          onError={onError}
          onEnd={onEnd}
          progressUpdateInterval={250}
        />
      )}
      {/* TV: something must hold focus or Android never hands the remote's keys to the app.
          Phone: tapping the picture shows or hides the controls. */}
      <Pressable
        style={StyleSheet.absoluteFill}
        hasTVPreferredFocus={!menu && !togetherOpen && !summary && controls !== 'full'}
        focusable={!menu && !togetherOpen && !summary && controls !== 'full'}
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
      {(controls !== 'hidden' || paused) && !error && !menu && !togetherOpen && !summary && !lock.locked && (
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
                userSeek(t)
                showControls('full')
              }} />
            ) : (
              <TVFocusGuideView trapFocusLeft trapFocusRight>
                <Focusable
                  focusable={controls === 'full'}
                  onFocus={() => setSeekFocused(true)}
                  onBlur={() => {
                    setSeekFocused(false)
                    if (scrubRef.current !== null) commitScrub()
                  }}
                  onPress={commitScrub}
                  style={st.seekFocus}
                >
                  <View style={st.track}>
                    <View style={[st.fill, { width: `${pct}%` }]} />
                    {scrub !== null && duration > 0 && <View style={[st.scrubMark, { left: `${Math.min(100, (scrub / duration) * 100)}%` }]} />}
                  </View>
                </Focusable>
              </TVFocusGuideView>
            )}
            <View style={st.times}>
              <Text style={st.time}>{clock(scrub ?? pos)}</Text>
              <Text style={st.mode}>
                {scrub !== null
                  ? `Jump to ${clock(scrub)}: OK or wait`
                  : seekFocused
                    ? '◀ ▶ to move through the video'
                    : paused
                      ? 'Paused'
                      : mode === 'direct'
                        ? 'Direct play'
                        : quality > 0
                          ? `Converting · ${resolutionLabel(quality)}`
                          : 'Converting'}
              </Text>
              <Text style={[st.time, st.timeEnd]}>-{clock(duration - pos)}</Text>
            </View>
            {controls === 'full' ? (
              <View style={st.controlRow}>
                <Button small label="⟲ 10s" onPress={() => userSeek(posRef.current - 10)} />
                <Button small primary preferFocus label={paused ? '▶  Play' : '❚❚  Pause'} onPress={() => userPause(!pausedRef.current)} />
                <Button small label="30s ⟳" onPress={() => userSeek(posRef.current + 30)} />
                {next && <Button small label="Next episode  ▶▶" onPress={() => playNext(next.id)} />}
                {!!item?.overview && (
                  <Button
                    small
                    label="Summary"
                    onPress={() => {
                      setControls('hidden')
                      setSummary(true)
                    }}
                  />
                )}
                <Button small label={roomId ? `Options · Together (${tg.room?.members.length ?? 1})` : 'Options'} onPress={() => setMenu(true)} />
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
      {!!notice && (
        <View style={st.notice} pointerEvents="none">
          <Text style={st.noticeText}>{notice}</Text>
        </View>
      )}
      {summary && item && (
        <View style={[st.menu, barsPad]}>
          <Text style={st.summaryTitle}>{title}</Text>
          {!!subtitleLine && <Text style={st.sub}>{subtitleLine}</Text>}
          <Text style={st.summaryMeta}>
            {[item.year, item.runtime ? `${item.runtime} min` : '', item.rating ? `★ ${item.rating.toFixed(1)}` : ''].filter(Boolean).join('  ·  ')}
          </Text>
          <ScrollView style={st.summaryScroll}>
            <Text style={st.summaryText}>{item.overview}</Text>
          </ScrollView>
          <View style={st.controlRow}>
            <Button small preferFocus label="Close" onPress={() => setSummary(false)} />
          </View>
        </View>
      )}
      {menu && (
        <View style={[st.menu, barsPad]}>
          <Text style={st.menuTitle}>Watch together</Text>
          <ScrollView horizontal contentContainerStyle={st.menuRow}>
            <Button
              small
              primary={!!roomId}
              label={roomId ? `Together (${tg.room?.members.length ?? 1}): invite or leave` : 'Watch together'}
              onPress={() => {
                setMenu(false)
                setControls('hidden')
                setTogetherOpen(true)
              }}
            />
          </ScrollView>
          {hueMode !== null && hueSeen && (
            <>
              <Text style={st.menuTitle}>Hue Sync</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                {(['sync', 'theater', 'off'] as HueMode[]).map((m) => (
                  <Button key={m} small primary={hueMode === m} label={hueModeNames[m]} onPress={() => pickHue(m)} />
                ))}
              </ScrollView>
            </>
          )}
          {file && (
            <>
              <Text style={st.menuTitle}>Quality</Text>
              <ScrollView horizontal contentContainerStyle={st.menuRow}>
                {canDirectPlay(file, audioRef.current) && burnRef.current < 0 && (
                  <Button small primary={mode === 'direct'} preferFocus={mode === 'direct'} label={`Original · Direct play`} onPress={() => chooseQuality(-1)} />
                )}
                <Button
                  small
                  primary={mode === 'transcode' && quality <= 0}
                  label={`Original${file.height ? ` (${resolutionLabel(file.height)})` : ''} · Convert`}
                  onPress={() => chooseQuality(0)}
                />
                {LADDER.filter((h) => h < file.height).map((h) => (
                  <Button key={h} small primary={mode === 'transcode' && quality === h} label={`${resolutionLabel(h)} · Convert`} onPress={() => chooseQuality(h)} />
                ))}
              </ScrollView>
            </>
          )}
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
      {togetherOpen && (
        <TogetherPanel
          roomId={roomId}
          room={tg.room}
          onStart={startTogether}
          onLeave={() => {
            setRoomId(null)
            setTogetherOpen(false)
          }}
          onClose={() => setTogetherOpen(false)}
        />
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
  introSkip: { position: 'absolute', right: 60, bottom: 40, flexDirection: 'row' },
  introLabel: { position: 'absolute', left: 60, bottom: 52, color: colors.text, fontSize: font.small, opacity: 0.85 },
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
  notice: { position: 'absolute', top: px(40), left: gutter, right: gutter, alignItems: 'center' },
  noticeText: { color: colors.text, fontSize: font.small, backgroundColor: 'rgba(11,13,16,0.85)', paddingHorizontal: px(20), paddingVertical: px(10), borderRadius: px(8), overflow: 'hidden' },
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
  seekFocus: { borderRadius: 12, paddingVertical: 10, paddingHorizontal: 6 },
  scrubMark: { position: 'absolute', top: -8, width: 6, height: 26, marginLeft: -3, borderRadius: 3, backgroundColor: '#fff' },
  summaryTitle: { color: colors.text, fontSize: font.title, fontWeight: '700' },
  summaryMeta: { color: colors.muted, fontSize: font.small, marginTop: 8 },
  summaryScroll: { maxHeight: 260, marginTop: 14 },
  summaryText: { color: colors.text, fontSize: font.body, lineHeight: 34 },
}))
