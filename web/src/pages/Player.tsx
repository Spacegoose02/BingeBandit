import Hls from 'hls.js'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { PreRoll } from '../components/PreRoll'
import { desktop } from '../desktop'
import DesktopPlayer from './DesktopPlayer'
import { roomPosition, TogetherPanel, useTogether, type TogetherRoom } from '../components/Together'
import { hueArea, hueSyncOn } from '../devicePrefs'
import { hueModeNames, useHueMode, type HueMode } from '../lights'
import { useApi, useDocumentTitle } from '../hooks'
import { channelLabel, commonLanguages, episodeLabel, formatClock, languageName, normLang, resolutionLabel } from '../format'
import { IMAGE_SUBS, TEXT_SUBS, pickTracks } from '../tracks'
import type { Item, ItemDetail, MediaFile, MediaStream, PreRoll as PreRollData, Prefs, SubtitleResult, TrickplayInfo } from '../types'
import { ScoreBug } from '../components/ScoreBug'

const LADDER = [2160, 1080, 720, 480]

export default function Player() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const { data, error } = useApi<ItemDetail>(`/api/items/${id}`)
  const { data: prefs } = useApi<Prefs>('/api/me/prefs')
  const startAt = Number(params.get('t')) || 0
  const playlistId = Number(params.get('playlist')) || 0
  const nav = useNavigate()
  const loc = useLocation()
  // Watch Together: join the room first, so playback starts where everyone is
  // (or follow the room to what it is playing now).
  const roomParam = params.get('together')
  const [joined, setJoined] = useState<{ roomId: string | null; start: number; announce: boolean; error?: string } | undefined>()
  useEffect(() => {
    setJoined(undefined)
    if (!roomParam) {
      setJoined({ roomId: null, start: 0, announce: false })
      return
    }
    let stale = false
    const moved = !!(loc.state as { togetherMove?: boolean } | null)?.togetherMove
    api.get<TogetherRoom>(`/api/together/${roomParam}`).then(
      (r) => {
        if (stale) return
        const here = r.state.itemId === Number(id)
        if (!here && !moved) {
          nav(`/play/${r.state.itemId}?together=${roomParam}`, { replace: true })
          return
        }
        setJoined({ roomId: roomParam, start: here ? roomPosition(r.state, r.now - Date.now()) : 0, announce: !here })
      },
      (e) => !stale && setJoined({ roomId: null, start: 0, announce: false, error: (e as Error).message }),
    )
    return () => {
      stale = true
    }
    // Only on arriving at an item: starting a room from the player adds the parameter without rejoining.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  // Trailers and the intro video play only when a movie starts from the beginning.
  const [preRoll, setPreRoll] = useState<PreRollData | 'done' | undefined>()
  const isMovie = data?.item.kind === 'movie'
  useEffect(() => {
    setPreRoll(undefined)
    if (!prefs || !isMovie) return
    if (startAt > 0 || playlistId || roomParam || (!prefs.preRollTrailers && !prefs.preRollIntro)) {
      setPreRoll('done')
      return
    }
    let stale = false
    api
      .get<PreRollData>(`/api/items/${id}/preroll`)
      .then((p) => !stale && setPreRoll(p.trailers.length || p.intro ? p : 'done'))
      .catch(() => !stale && setPreRoll('done'))
    return () => {
      stale = true
    }
  }, [id, isMovie, prefs, startAt, playlistId])
  const preRollDone = useCallback(() => setPreRoll('done'), [])
  useDocumentTitle(data ? (data.item.kind === 'episode' ? `${data.item.showTitle} ${episodeLabel(data.item)}` : data.item.title) : 'Player')
  if (error) return <div className="player"><div className="player-error">{error}</div></div>
  if (!data || !prefs || !joined || (isMovie && !preRoll)) return <div className="player"><div className="spinner" /></div>
  if (isMovie && preRoll && preRoll !== 'done') return <PreRoll data={preRoll} movieTitle={data.item.title} onDone={preRollDone} />
  // In the Windows app, mpv plays the file (not in a watch party, which needs the web player).
  if (desktop && !joined.roomId) {
    return (
      <DesktopPlayer
        key={id}
        detail={data}
        prefs={prefs}
        startAt={startAt}
        fileId={Number(params.get('file')) || 0}
        playlistId={playlistId}
      />
    )
  }
  return (
    <PlayerView
      key={id}
      detail={data}
      prefs={prefs}
      startAt={joined.roomId ? joined.start : startAt}
      fileId={Number(params.get('file')) || 0}
      playlistId={playlistId}
      together={joined}
    />
  )
}

/** Whether this browser can play the file as-is (no server work at all). */
function canDirectPlay(f: MediaFile, video: HTMLVideoElement): boolean {
  if (f.hdr) return false
  const mime = ({ mp4: 'video/mp4', m4v: 'video/mp4', mov: 'video/mp4', webm: 'video/webm' } as Record<string, string>)[f.container]
  // Ask about the file's bit depth too: browsers that take 8-bit H.264 or
  // HEVC often can't decode 10-bit, and then play the sound over a black picture.
  const deep = f.bitDepth > 8
  if (deep && f.videoCodec === 'h264') return false // High 10 doesn't decode in browsers
  const v = (
    deep
      ? { hevc: 'hvc1.2.4.L120.90', vp9: 'vp09.02.10.10', av1: 'av01.0.08M.10' }
      : { h264: 'avc1.640028', hevc: 'hvc1.1.6.L120.90', vp9: 'vp09.00.10.08', av1: 'av01.0.08M.08' }
  )[f.videoCodec as 'hevc']
  const a = ({ aac: 'mp4a.40.2', mp3: 'mp3', opus: 'opus', flac: 'flac', vorbis: 'vorbis' } as Record<string, string>)[f.audioCodec]
  if (!mime || !v || (f.audioCodec && !a)) return false
  return video.canPlayType(`${mime}; codecs="${v}${a ? `,${a}` : ''}"`) !== ''
}

let outputChannels: number | undefined

/** Speaker channels this device outputs (2 unless a surround system is connected). */
function speakerChannels(): number {
  if (outputChannels === undefined) {
    outputChannels = 2
    try {
      const ctx = new AudioContext()
      outputChannels = Math.max(2, ctx.destination.maxChannelCount)
      ctx.close().catch(() => undefined)
    } catch {
      // No Web Audio: assume stereo.
    }
  }
  return outputChannels
}

/**
 * Audio this browser can take inside the server's HLS stream, so surround
 * tracks are copied or kept 5.1 instead of mixed down to stereo. hls.js can
 * pass AC-3 through from MPEG-TS (not E-AC-3); Safari's own player takes both.
 */
function audioCaps(video: HTMLVideoElement, stereoOnly: boolean): { audioCodecs: string[]; maxAudioChannels: number } {
  if (stereoOnly) return { audioCodecs: ['aac'], maxAudioChannels: 2 }
  const codecs = ['aac']
  const ok = (type: string) =>
    Hls.isSupported() ? (window.MediaSource?.isTypeSupported(type) ?? false) : video.canPlayType(type) !== ''
  if (ok('audio/mp4; codecs="ac-3"')) codecs.push('ac3')
  if (!Hls.isSupported() && ok('audio/mp4; codecs="ec-3"')) codecs.push('eac3')
  return { audioCodecs: codecs, maxAudioChannels: Math.min(speakerChannels(), 8) }
}

interface Choice {
  quality: number // max height; 0 = original
  convert: boolean // at original quality: have the server convert the video instead of playing the file as is
  audio: number // stream index; -1 = file default
  burn: number // image subtitle stream burned in by the server; -1 = none
}

interface Cue {
  start: number
  end: number
  text: string
}

function parseVtt(text: string): Cue[] {
  const toSec = (t: string) => {
    const p = t.trim().split(':').map(Number)
    return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : p[0] * 60 + p[1]
  }
  const cues: Cue[] = []
  for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
    const lines = block.split('\n')
    const i = lines.findIndex((l) => l.includes('-->'))
    if (i < 0) continue
    const [a, b] = lines[i].split('-->')
    const body = lines.slice(i + 1).join('\n').replace(/<[^>]+>/g, '').trim()
    if (body) cues.push({ start: toSec(a), end: toSec(b.trim().split(' ')[0]), text: body })
  }
  return cues
}

function streamLabel(s: MediaStream): string {
  const bits = [languageName(s.language)]
  if (s.type === 'audio') bits.push(`${s.codec.toUpperCase()} ${channelLabel(s.channels ?? 2)}`)
  if (s.title) bits.push(s.title)
  if (s.forced) bits.push('Forced')
  if (s.hearingImpaired) bits.push('SDH')
  if (s.external) bits.push('External')
  if (IMAGE_SUBS.has(s.codec)) bits.push('Burned in')
  return bits.join(' · ')
}

function PlayerView({
  detail,
  prefs,
  startAt,
  fileId,
  playlistId,
  together,
}: {
  detail: ItemDetail
  prefs: Prefs
  startAt: number
  fileId: number
  playlistId: number // continue through this playlist instead of the show
  together: { roomId: string | null; announce: boolean; error?: string }
}) {
  const { item, files = [] } = detail
  const baseFile = files.find((f) => f.id === fileId) ?? files[0]
  // Subtitles downloaded while playing add to the file's streams.
  const [streams, setStreams] = useState<MediaStream[] | null>(null)
  const file = useMemo(() => (baseFile && streams ? { ...baseFile, streams } : baseFile), [baseFile, streams])
  const { user } = useAuth()
  const nav = useNavigate()
  const initial = useRef(file ? pickTracks(file, prefs) : { audio: -1 })

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const offsetRef = useRef(0) // file time at video time 0 (transcodes start mid-file)
  const transcodeRef = useRef<string | null>(null)
  const startedRef = useRef(false)
  const forceTranscode = useRef(false) // direct play failed once; don't try again
  const adaptiveRef = useRef(false) // the stream has several qualities; the player picks
  const stereoOnly = useRef(false) // surround audio failed once; ask for stereo
  const settlingRef = useRef(true) // a load or seek is in progress, so waiting isn't a stall
  const stalls = useRef<number[]>([])
  const retries = useRef(0)
  // Seconds actually played (for watch history): small forward steps of
  // currentTime while playing; seeks and pauses add nothing.
  const playedRef = useRef(0)
  const lastTick = useRef<number | null>(null)
  const videoCopyRef = useRef(false) // the conversion copies the video as is (H.264 at its own size)
  const directSince = useRef(0) // where direct play started, to check that the picture shows too
  const playSession = useRef(Math.random().toString(36).slice(2) + Date.now().toString(36))
  const hideTimer = useRef<number>()

  const initialSub = initial.current.sub
  const [choice, setChoice] = useState<Choice>({
    quality: 0,
    convert: false,
    audio: initial.current.audio,
    burn: initialSub && IMAGE_SUBS.has(initialSub.codec) ? initialSub.index : -1,
  })
  const choiceRef = useRef(choice)
  choiceRef.current = choice
  const [mode, setMode] = useState<'direct' | 'transcode'>('direct')
  const [subIndex, setSubIndex] = useState(initialSub && TEXT_SUBS.has(initialSub.codec) ? initialSub.index : -1)
  const [cues, setCues] = useState<Cue[]>([])
  const [time, setTime] = useState(startAt)
  const [paused, setPaused] = useState(true)
  const [buffering, setBuffering] = useState(true)
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('bb.volume') ?? 1))
  const [muted, setMuted] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [controls, setControls] = useState(true)
  const [menu, setMenu] = useState<null | 'settings' | 'together' | 'summary' | 'subsearch'>(null)
  const menuRef = useRef(menu)
  menuRef.current = menu
  // Seconds the subtitles are moved: positive shows them later.
  const [subDelay, setSubDelay] = useState(0)
  const [thumbs, setThumbs] = useState<TrickplayInfo | null>(null)
  const [hover, setHover] = useState<{ t: number; x: number; width: number } | null>(null)
  const seekRef = useRef<HTMLDivElement>(null)
  // Hue Sync's quick switch, once this profile turned it on (Account).
  const hue = useHueMode()
  const [hueSeen, setHueSeen] = useState(false)
  useEffect(() => {
    if (hue.mode && hue.mode !== 'off') setHueSeen(true)
  }, [hue.mode])
  const [error, setError] = useState('')
  const [toast, setToast] = useState(together.error ? `Couldn't join the watch party: ${together.error}` : '')
  const [upNext, setUpNext] = useState<Item | null>(null)
  const [countdown, setCountdown] = useState(10)

  const duration = file?.duration || (item.runtime ?? 0) * 60
  const position = () => offsetRef.current + (videoRef.current?.currentTime ?? 0)
  const audioStreams = file?.streams.filter((s) => s.type === 'audio') ?? []
  const subStreams = file?.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec))) ?? []

  const stopTranscode = useCallback(() => {
    const id = transcodeRef.current
    transcodeRef.current = null
    if (id) api.del(`/api/transcode/${id}`, { keepalive: true }).catch(() => undefined)
  }, [])

  const report = useCallback(
    (stopped = false, finished = false) => {
      const video = videoRef.current
      if (!video || !file || !startedRef.current) return
      api
        .post(
          '/api/progress',
          {
            playSessionId: playSession.current,
            itemId: item.id,
            fileId: file.id,
            // Skipping the credits counts as finishing, wherever they start.
            position: video.ended || finished ? duration : position(),
            duration,
            paused: video.paused,
            stopped,
            mode,
            transcodeId: transcodeRef.current ?? '',
            client: 'Web',
            lights: hueSyncOn(),
            lightsArea: hueArea(),
            played: Math.round(playedRef.current),
          },
          { keepalive: true },
        )
        .catch(() => undefined)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [file, item.id, duration, mode],
  )

  const load = useCallback(
    async (at: number, c: Choice) => {
      const video = videoRef.current
      if (!video || !file) return
      setError('')
      setBuffering(true)
      settlingRef.current = true
      hlsRef.current?.destroy()
      hlsRef.current = null
      const retry = (why: string) => {
        if (retries.current < 2) {
          retries.current++
          stereoOnly.current = true // in case surround audio was the problem
          // Resume from where the viewer is, not where this load began.
          const resumeAt = startedRef.current ? offsetRef.current + (video.currentTime || 0) : at
          window.setTimeout(() => load(resumeAt, choiceRef.current), 1500)
        } else setError(why)
      }
      if (c.quality === 0 && !c.convert && c.audio < 0 && c.burn < 0 && !forceTranscode.current && canDirectPlay(file, video)) {
        stopTranscode()
        offsetRef.current = 0
        setMode('direct')
        directSince.current = at
        adaptiveRef.current = false
        video.src = `/api/files/${file.id}/direct`
        video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
      } else {
        try {
          const res = await api.post<{ session: { id: string; variants?: number[]; videoCopy?: boolean }; playlist: string }>(`/api/files/${file.id}/transcode`, {
            start: at,
            maxHeight: c.quality,
            audioStream: c.audio,
            burnSubtitle: c.burn,
            replace: transcodeRef.current ?? '',
            // At "original" quality, let the player switch between qualities
            // itself when the server offers it. A picked quality stays fixed.
            adaptive: c.quality === 0,
            // H.264 at its own size would otherwise be copied, which doesn't
            // help a device that can't keep up with the file itself.
            encodeVideo: c.convert,
            ...audioCaps(video, stereoOnly.current),
          })
          transcodeRef.current = res.session.id
          videoCopyRef.current = !!res.session.videoCopy
          adaptiveRef.current = (res.session.variants?.length ?? 0) > 1
          offsetRef.current = at
          setMode('transcode')
          if (Hls.isSupported()) {
            const hls = new Hls({ startPosition: 0, maxBufferLength: 60, backBufferLength: 120 })
            let mediaErrors = 0
            hls.on(Hls.Events.ERROR, (_e, d) => {
              if (!d.fatal) return
              if (d.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad()
              // A second decode failure starts over (with stereo audio, in case surround was the problem).
              else if (d.type === Hls.ErrorTypes.MEDIA_ERROR && mediaErrors++ === 0) hls.recoverMediaError()
              else retry('Playback failed. The server could not convert this file.')
            })
            hls.loadSource(res.playlist)
            hls.attachMedia(video)
            hlsRef.current = hls
          } else {
            video.src = res.playlist // Safari plays HLS natively
          }
        } catch (e) {
          retry((e as Error).message)
          return
        }
      }
      video.play().catch(() => setPaused(true))
    },
    [file, stopTranscode],
  )

  const apply = (c: Choice) => {
    setChoice(c)
    setMenu(null)
    load(position(), c)
  }

  // First load, and cleanup when leaving the player.
  useEffect(() => {
    if (!file) return
    load(startAt, choiceRef.current)
    // Closing the tab or reloading skips React cleanup; pagehide still fires.
    const onHide = () => {
      report(true)
      stopTranscode()
    }
    window.addEventListener('pagehide', onHide)
    return () => {
      window.removeEventListener('pagehide', onHide)
      report(true)
      hlsRef.current?.destroy()
      stopTranscode()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const t = window.setInterval(() => report(), 10000)
    return () => window.clearInterval(t)
  }, [report])

  useEffect(() => {
    if (videoRef.current) {
      videoRef.current.volume = volume
      videoRef.current.muted = muted
    }
    localStorage.setItem('bb.volume', String(volume))
  }, [volume, muted])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(''), 6000)
    return () => window.clearTimeout(t)
  }, [toast])

  useEffect(() => {
    if (subIndex < 0 || !file) return setCues([])
    let cancelled = false
    fetch(`/api/files/${file.id}/subtitles/${subIndex}.vtt`, { credentials: 'same-origin' })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.statusText))))
      .then((t) => !cancelled && setCues(parseVtt(t)))
      .catch(() => !cancelled && setToast('Could not load those subtitles'))
    return () => {
      cancelled = true
    }
  }, [subIndex, file])

  // Scrub previews; missing ones are made by the server for next time.
  const fileKey = file?.id
  useEffect(() => {
    setThumbs(null)
    if (!fileKey) return
    let stale = false
    api.get<TrickplayInfo>(`/api/files/${fileKey}/trickplay`).then(
      (t) => !stale && setThumbs(t),
      () => undefined,
    )
    return () => {
      stale = true
    }
  }, [fileKey])
  const hoverAt = (clientX: number) => {
    const r = seekRef.current?.getBoundingClientRect()
    if (!r || !duration) return
    const f = Math.max(0, Math.min(1, (clientX - r.left) / r.width))
    setHover({ t: f * duration, x: f * r.width, width: r.width })
  }
  const nudgeSubDelay = (d: number) => {
    const next = Math.round((subDelay + d) * 10) / 10
    setSubDelay(next)
    setToast(`Subtitles: ${delayLabel(next)}`)
  }

  const seekTo = (t: number) => {
    const video = videoRef.current
    if (!video) return
    t = Math.max(0, Math.min(t, Math.max(duration - 1, 0)))
    setTime(t)
    settlingRef.current = true
    if (mode === 'direct') {
      video.currentTime = t
      return
    }
    const local = t - offsetRef.current
    const s = video.seekable
    if (local >= 0 && s.length && local <= s.end(s.length - 1)) video.currentTime = local
    else load(t, choiceRef.current) // outside what has been transcoded: restart there
  }

  // Three stalls within a minute (not caused by seeking) means the
  // connection can't keep up: drop one quality step.
  const onStall = () => {
    setBuffering(true)
    if (!prefs.autoQuality || adaptiveRef.current || settlingRef.current || !startedRef.current || !file) return
    const now = Date.now()
    stalls.current = [...stalls.current.filter((t) => now - t < 60000), now]
    if (stalls.current.length < 3) return
    stalls.current = []
    const current = choiceRef.current.quality || file.height
    const lower = LADDER.find((q) => q < current && q < file.height)
    if (!lower) return
    setToast(`Playback kept pausing to buffer, so quality was lowered to ${resolutionLabel(lower)}.`)
    apply({ ...choiceRef.current, quality: lower })
  }

  // A device too slow for the file (a high bitrate, 4K, a heavy profile)
  // plays it without buffering but drops frames, so it looks laggy. Two
  // checks in a row dropping over a fifth of the frames: convert instead.
  useEffect(() => {
    const video = videoRef.current
    if (!video?.getVideoPlaybackQuality || !file || !prefs.autoQuality || choice.convert || choice.quality) return
    if (mode !== 'direct' && !videoCopyRef.current) return // already converted
    let last = video.getVideoPlaybackQuality()
    let bad = 0
    const t = window.setInterval(() => {
      const q = video.getVideoPlaybackQuality()
      const total = q.totalVideoFrames - last.totalVideoFrames
      const dropped = q.droppedVideoFrames - last.droppedVideoFrames
      last = q
      if (video.paused || settlingRef.current || total < 50) return
      bad = dropped / total > 0.2 ? bad + 1 : 0
      if (bad < 2) return
      window.clearInterval(t)
      setToast('This device was dropping frames, so the server is converting the video for it.')
      apply({ ...choiceRef.current, convert: true })
    }, 5000)
    return () => window.clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file, mode, choice.convert, choice.quality, prefs.autoQuality])

  // Watch Together: everyone else's play, pause, seek and next item land
  // here; this viewer's own go out through share().
  const [, setParams] = useSearchParams()
  const [roomId, setRoomId] = useState(together.roomId)
  const roomJoined = useRef(false)
  const pauseOnStart = useRef(false) // joined a paused room: stop once the first load starts playing
  const tg = useTogether(roomId, (s, skew) => {
    if (s.itemId !== item.id) {
      nav(`/play/${s.itemId}?together=${roomId}`, { replace: true })
      return
    }
    const v = videoRef.current
    if (!v) return
    const target = roomPosition(s, skew)
    const wasPaused = v.paused
    const jump = Math.abs(position() - target) > 2
    if (jump) seekTo(target)
    if (s.paused && !v.paused) v.pause()
    else if (!s.paused && v.paused) v.play().catch(() => setToast('Press play to catch up with the others'))
    const what = s.paused !== wasPaused ? (s.paused ? 'paused' : 'pressed play') : jump ? `skipped to ${formatClock(target)}` : ''
    if (what && s.byName) setToast(`${s.byName} ${what}`)
  })
  const share = (paused: boolean, at = position()) => {
    if (roomId) tg.send({ paused, position: at })
  }
  // On joining: match the room's play/pause, and tell the room when this viewer moved it to the next item.
  useEffect(() => {
    if (!tg.room || roomJoined.current) return
    roomJoined.current = true
    if (together.announce && roomId === together.roomId) tg.send({ itemId: item.id, paused: false, position: 0 })
    else if (tg.room.state.paused) {
      const v = videoRef.current
      if (v && !v.paused && startedRef.current) v.pause()
      else pauseOnStart.current = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tg.room])
  // Stay within a few seconds of the room while it plays.
  useEffect(() => {
    if (!roomId) return
    const t = window.setInterval(() => {
      const v = videoRef.current
      const r = tg.room
      if (!v || !r || r.state.paused || v.paused || settlingRef.current) return
      if (Math.abs(position() - tg.position()) > 3) seekTo(tg.position())
    }, 5000)
    return () => window.clearInterval(t)
  })
  useEffect(() => {
    if (!tg.ended) return
    setToast('The watch party has ended.')
    setRoomId(null)
  }, [tg.ended])
  const startRoom = async () => {
    const v = videoRef.current
    const r = await api.post<TogetherRoom>('/api/together', { itemId: item.id, position: position(), paused: v?.paused ?? true })
    roomJoined.current = true
    setRoomId(r.id)
    setParams((p) => {
      p.set('together', r.id)
      p.delete('t')
      return p
    }, { replace: true })
  }
  const leaveRoom = () => {
    setRoomId(null)
    setMenu(null)
    setParams((p) => {
      p.delete('together')
      return p
    }, { replace: true })
  }

  const seek = (t: number) => {
    seekTo(t)
    share(videoRef.current?.paused ?? true, Math.max(0, Math.min(t, Math.max(duration - 1, 0))))
  }

  const togglePlay = () => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) {
      v.play().catch(() => undefined)
      share(false)
    } else {
      v.pause()
      share(true)
    }
  }

  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen()
    else containerRef.current?.requestFullscreen().catch(() => undefined)
  }

  const poke = useCallback(() => {
    setControls(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => {
      // The subtitle search stays open while it's being used.
      if (!videoRef.current?.paused && menuRef.current !== 'subsearch') {
        setControls(false)
        setMenu(null)
      }
    }, 3000)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).tagName === 'SELECT') return
      const actions: Record<string, () => void> = {
        ' ': togglePlay,
        k: togglePlay,
        ArrowLeft: () => seek(position() - 10),
        ArrowRight: () => seek(position() + 10),
        j: () => seek(position() - 10),
        l: () => seek(position() + 30),
        f: toggleFullscreen,
        // As in VLC: G shows subtitles earlier, H later.
        g: () => nudgeSubDelay(-0.5),
        h: () => nudgeSubDelay(0.5),
        m: () => setMuted((m) => !m),
        ArrowUp: () => setVolume((v) => Math.min(1, v + 0.1)),
        ArrowDown: () => setVolume((v) => Math.max(0, v - 0.1)),
      }
      const fn = actions[e.key]
      if (fn) {
        e.preventDefault()
        fn()
        poke()
      }
    }
    const onFs = () => setFullscreen(!!document.fullscreenElement)
    window.addEventListener('keydown', onKey)
    document.addEventListener('fullscreenchange', onFs)
    return () => {
      window.removeEventListener('keydown', onKey)
      document.removeEventListener('fullscreenchange', onFs)
    }
  })

  const onEnded = async () => {
    report(false, true)
    const done = playlistId ? `/playlist/${playlistId}` : item.kind === 'episode' ? `/item/${item.showId}` : `/item/${item.id}`
    if (!playlistId && item.kind !== 'episode') return nav(done, { replace: true })
    try {
      const next = await api.get<Item | undefined>(
        playlistId ? `/api/playlists/${playlistId}/next?after=${item.id}` : `/api/items/${item.id}/next`,
      )
      if (next) {
        setCountdown(10)
        setUpNext(next)
      } else nav(done, { replace: true })
    } catch {
      nav(-1)
    }
  }
  const nextUrl = (next: Item) => {
    const q = new URLSearchParams()
    if (playlistId) q.set('playlist', String(playlistId))
    if (roomId) q.set('together', roomId)
    return `/play/${next.id}${q.toString() ? `?${q}` : ''}`
  }
  // Moving on in a watch party takes everyone along.
  const goNext = (next: Item) => nav(nextUrl(next), { replace: true, state: { togetherMove: !!roomId } })

  // Intro and credits markers: offer a button or act automatically, once per
  // visit, so seeking back into the intro doesn't skip it again.
  const intro = detail.markers?.find((m) => m.kind === 'intro')
  const credits = detail.markers?.find((m) => m.kind === 'credits')
  const introDone = useRef(false)
  const creditsDone = useRef(false)
  const inIntro = !!intro && time >= intro.start && time < intro.end - 1
  const inCredits = !!credits && time >= credits.start && time < Math.min(credits.end, duration) - 1 && !upNext
  const hasNext = item.kind === 'episode' || !!playlistId
  const skipIntro = () => {
    introDone.current = true
    if (intro) seek(intro.end)
  }
  // A movie with a scene after the credits jumps to that scene instead of ending.
  const afterCredits = !hasNext && !!credits && credits.end < duration - 5
  const skipCredits = () => {
    creditsDone.current = true
    if (afterCredits && credits) seek(credits.end)
    else onEnded()
  }
  useEffect(() => {
    if (inIntro && prefs.skipIntro === 'auto' && !introDone.current) {
      introDone.current = true
      setToast('Skipped the intro')
      skipIntro()
    }
    if (inCredits && prefs.skipCredits === 'auto' && !creditsDone.current && (hasNext || afterCredits)) {
      if (afterCredits) setToast('Skipped to the scene after the credits')
      skipCredits()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inIntro, inCredits])

  useEffect(() => {
    if (!upNext || !prefs.autoPlayNext) return
    if (countdown <= 0) {
      goNext(upNext)
      return
    }
    const t = window.setTimeout(() => setCountdown((c) => c - 1), 1000)
    return () => window.clearTimeout(t)
  }, [upNext, countdown, nav, prefs.autoPlayNext])

  const changeAudio = (a: number) => {
    const def = audioStreams.find((s) => s.default) ?? audioStreams[0]
    apply({ ...choice, audio: def && def.index === a ? -1 : a })
  }

  const changeSubtitle = (s: MediaStream | null) => {
    setMenu(null)
    if (s && IMAGE_SUBS.has(s.codec)) {
      setSubIndex(-1)
      if (choice.burn !== s.index) apply({ ...choice, burn: s.index })
      return
    }
    setSubIndex(s ? s.index : -1)
    setSubDelay(0)
    if (choice.burn >= 0) apply({ ...choice, burn: -1 })
  }
  const subtitleDownloaded = (all: MediaStream[], index: number) => {
    setStreams(all)
    setMenu(null)
    const s = all.find((x) => x.index === index)
    if (s) changeSubtitle(s)
    setToast('Subtitles added')
  }

  if (!file) return <div className="player"><div className="player-error">This item has no playable file.</div></div>

  const qualities = [0, ...LADDER.filter((q) => q < file.height)]
  const directOK = videoRef.current ? canDirectPlay(file, videoRef.current) : false
  const modeLabel =
    mode === 'direct'
      ? 'Direct Play'
      : choice.quality
        ? `${resolutionLabel(choice.quality)} Converting`
        : 'Converting'
  const activeCue = cues.find((c) => time - subDelay >= c.start && time - subDelay <= c.end)
  const pct = duration ? (time / duration) * 100 : 0
  const title = item.kind === 'episode' ? item.showTitle : item.title
  const subtitle = item.kind === 'episode' ? `${episodeLabel(item)} · ${item.title}` : item.year ? String(item.year) : ''
  const effectiveAudio = choice.audio >= 0 ? choice.audio : (audioStreams.find((x) => x.default) ?? audioStreams[0])?.index

  return (
    <div
      ref={containerRef}
      className={`player ${controls || paused ? 'show-controls' : ''}`}
      onMouseMove={poke}
      onTouchStart={poke}
    >
      <ScoreBug />
      <video
        ref={videoRef}
        className="player-video"
        playsInline
        onClick={togglePlay}
        onDoubleClick={toggleFullscreen}
        onPlay={() => setPaused(false)}
        onPause={() => {
          setPaused(true)
          report()
        }}
        onSeeked={() => report()}
        onPlaying={() => {
          // Report at once, so plugins following the player (Hue) see starts and resumes.
          if (!startedRef.current || paused) queueMicrotask(() => report())
          if (pauseOnStart.current) {
            pauseOnStart.current = false
            videoRef.current?.pause()
          }
          startedRef.current = true
          settlingRef.current = false
          retries.current = 0
          setBuffering(false)
        }}
        onWaiting={onStall}
        onTimeUpdate={() => {
          const v = videoRef.current
          if (v && !v.paused) {
            const now = v.currentTime
            const step = lastTick.current === null ? 0 : now - lastTick.current
            if (step > 0 && step < 2) playedRef.current += step
            lastTick.current = now
          }
          // A browser that can't decode the picture often just plays the
          // sound over black, with no error. Two seconds in with no frame
          // decoded: let the server convert the video instead.
          if (v && mode === 'direct' && file?.videoCodec && !forceTranscode.current && v.currentTime - directSince.current > 2) {
            const frames = v.getVideoPlaybackQuality?.().totalVideoFrames
            if (v.videoWidth === 0 || frames === 0) {
              forceTranscode.current = true
              load(position(), choiceRef.current)
              return
            }
          }
          setTime(position())
        }}
        onSeeking={() => (lastTick.current = null)}
        onEmptied={() => (lastTick.current = null)}
        onEnded={onEnded}
        onError={() => {
          // The browser couldn't decode the file after all: let the server convert it.
          // A converted stream that fails once gets another go with stereo audio.
          if (mode === 'direct') forceTranscode.current = true
          else if (!stereoOnly.current && !Hls.isSupported()) stereoOnly.current = true
          else return
          load(position(), choiceRef.current)
        }}
      />

      {activeCue && (
        <div className={`subtitle-cue size-${prefs.subtitleSize} ${prefs.subtitleBackground ? 'boxed' : ''}`}>
          <span>{activeCue.text}</span>
        </div>
      )}
      {buffering && !error && <div className="spinner" />}
      {error && <div className="player-error">{error}</div>}
      {toast && <div className="player-toast">{toast}</div>}
      {inIntro && prefs.skipIntro === 'button' && (
        <button className="skip-btn" onClick={skipIntro}>
          Skip Intro <Icon name="next" size={18} />
        </button>
      )}
      {inCredits && prefs.skipCredits === 'button' && (
        <button className="skip-btn" onClick={skipCredits}>
          {hasNext ? 'Next' : 'Skip Credits'} <Icon name="next" size={18} />
        </button>
      )}

      <div className="player-top">
        <button className="icon-btn" onClick={() => nav(-1)} aria-label="Back">
          <Icon name="back" size={28} />
        </button>
        <div>
          <div className="player-title">{title}</div>
          <div className="player-sub">{subtitle}</div>
        </div>
      </div>

      <div className="player-bottom" onClick={(e) => e.stopPropagation()}>
        <div
          className="seek"
          ref={seekRef}
          onMouseMove={(e) => hoverAt(e.clientX)}
          onMouseLeave={() => setHover(null)}
          onTouchMove={(e) => hoverAt(e.touches[0].clientX)}
          onTouchEnd={() => setHover(null)}
        >
          {hover && <ScrubPreview fileId={file.id} info={thumbs} {...hover} />}
          <input
            type="range"
            min={0}
            max={duration || 1}
            step={1}
            value={Math.min(time, duration)}
            onChange={(e) => seek(Number(e.target.value))}
            style={{ '--pct': `${pct}%` } as React.CSSProperties}
            aria-label="Seek"
          />
        </div>
        <div className="player-controls">
          <button className="icon-btn" onClick={togglePlay} aria-label={paused ? 'Play' : 'Pause'}>
            <Icon name={paused ? 'play' : 'pause'} fill size={28} />
          </button>
          <button className="icon-btn" onClick={() => seek(position() - 10)} aria-label="Back 10 seconds">
            <Icon name="replay10" size={24} />
          </button>
          <button className="icon-btn" onClick={() => seek(position() + 30)} aria-label="Forward 30 seconds">
            <Icon name="forward30" size={24} />
          </button>
          <div className="volume">
            <button className="icon-btn" onClick={() => setMuted((m) => !m)} aria-label={muted ? 'Unmute' : 'Mute'}>
              <Icon name={muted || volume === 0 ? 'mute' : 'volume'} size={22} />
            </button>
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              onChange={(e) => {
                setMuted(false)
                setVolume(Number(e.target.value))
              }}
              style={{ '--pct': `${(muted ? 0 : volume) * 100}%` } as React.CSSProperties}
              aria-label="Volume"
            />
          </div>
          <div className="time">
            {formatClock(time)} / {formatClock(duration)}
          </div>
          <div className="spacer" />
          <span className={`mode-pill ${mode}`}>
            {modeLabel}
          </span>
          {!!item.overview && (
            <div className="menu-anchor">
              <button className={`icon-btn ${menu === 'summary' ? 'active' : ''}`} onClick={() => setMenu(menu === 'summary' ? null : 'summary')} aria-label="Summary" title="Summary">
                <Icon name="info" size={22} />
              </button>
              {menu === 'summary' && (
                <div className="menu player-menu player-summary">
                  <div className="menu-section">Summary</div>
                  <p>{item.overview}</p>
                </div>
              )}
            </div>
          )}
          <div className="menu-anchor">
            <button
              className={`icon-btn ${roomId ? 'active' : ''}`}
              onClick={() => setMenu(menu === 'together' ? null : 'together')}
              aria-label="Watch together"
              title={roomId ? `Watching together${tg.room ? ` (${tg.room.members.length})` : ''}` : 'Watch together'}
            >
              <Icon name="users" size={22} />
              {roomId && tg.room && <span className="together-count">{tg.room.members.length}</span>}
            </button>
            {menu === 'together' && (
              <TogetherPanel itemId={item.id} room={tg.room} roomId={roomId} onStart={startRoom} onLeave={leaveRoom} />
            )}
          </div>
          <div className="menu-anchor">
            <button className="icon-btn" onClick={() => setMenu(menu === 'settings' ? null : 'settings')} aria-label="Options" title="Options">
              <Icon name="sliders" size={22} />
            </button>
            {menu === 'settings' && (
              <div className="menu player-menu">
                <button onClick={() => setMenu('together')}>
                  {roomId ? `Watching together${tg.room ? ` (${tg.room.members.length})` : ''}…` : 'Watch together…'}
                </button>
                {hue.mode && hueSeen && (
                  <>
                    <div className="menu-section">Hue Sync</div>
                    {(['sync', 'theater', 'off'] as HueMode[]).map((m) => (
                      <button key={m} className={hue.mode === m ? 'active' : ''} onClick={() => hue.setMode(m).catch((e) => setToast(e.message))}>
                        {hueModeNames[m]}
                      </button>
                    ))}
                  </>
                )}
                <div className="menu-section">Quality</div>
                {/* Original as is, original converted by the server (smoother on
                    a device that can't keep up with the file), or smaller. */}
                {directOK && (
                  <button className={choice.quality === 0 && !choice.convert ? 'active' : ''} onClick={() => apply({ ...choice, quality: 0, convert: false })}>
                    Original ({resolutionLabel(file.height)}) · Direct Play
                  </button>
                )}
                <button
                  className={choice.quality === 0 && (choice.convert || !directOK) ? 'active' : ''}
                  onClick={() => apply({ ...choice, quality: 0, convert: true })}
                >
                  Original ({resolutionLabel(file.height)}) · Convert
                </button>
                {qualities
                  .filter((q) => q > 0)
                  .map((q) => (
                    <button key={q} className={q === choice.quality ? 'active' : ''} onClick={() => apply({ ...choice, quality: q, convert: false })}>
                      {resolutionLabel(q)} · Convert
                    </button>
                  ))}
                {audioStreams.length > 1 && <div className="menu-section">Audio</div>}
                {audioStreams.length > 1 &&
                  audioStreams.map((s) => (
                    <button key={s.index} className={s.index === effectiveAudio ? 'active' : ''} onClick={() => changeAudio(s.index)}>
                      {streamLabel(s)}
                    </button>
                  ))}
                <div className="menu-section">Subtitles</div>
                {subStreams.length > 0 && (
                  <button className={subIndex === -1 && choice.burn === -1 ? 'active' : ''} onClick={() => changeSubtitle(null)}>
                    Off
                  </button>
                )}
                {subStreams.map((s) => (
                  <button
                    key={s.index}
                    className={subIndex === s.index || choice.burn === s.index ? 'active' : ''}
                    onClick={() => changeSubtitle(s)}
                  >
                    {streamLabel(s)}
                  </button>
                ))}
                {!user?.kid && <button onClick={() => setMenu('subsearch')}>Find subtitles online…</button>}
                {subIndex >= 0 && (
                  <>
                    <div className="menu-section">Subtitle timing</div>
                    <div className="menu-stepper">
                      <button onClick={() => nudgeSubDelay(-0.5)} aria-label="Show subtitles earlier" title="Earlier (G)">−0.5s</button>
                      <span>{delayLabel(subDelay)}</span>
                      <button onClick={() => nudgeSubDelay(0.5)} aria-label="Show subtitles later" title="Later (H)">+0.5s</button>
                    </div>
                    {subDelay !== 0 && <button onClick={() => setSubDelay(0)}>Reset timing</button>}
                  </>
                )}
              </div>
            )}
            {menu === 'subsearch' && (
              <SubtitleSearch fileId={file.id} language={prefs.subtitleLanguage} onDone={subtitleDownloaded} />
            )}
          </div>
          <button className="icon-btn" onClick={toggleFullscreen} aria-label="Fullscreen">
            <Icon name={fullscreen ? 'exitFullscreen' : 'fullscreen'} size={22} />
          </button>
        </div>
      </div>

      {upNext && (
        <div className="up-next">
          {img(upNext.backdrop || upNext.showBackdrop, 'w500') && <img src={img(upNext.backdrop || upNext.showBackdrop, 'w500')} alt="" />}
          <div>
            <div className="muted small">{prefs.autoPlayNext ? `Up next in ${countdown}s` : 'Up next'}</div>
            <div className="up-next-title">
              {upNext.kind === 'episode' ? `${upNext.showTitle} · ${episodeLabel(upNext)} · ${upNext.title}` : upNext.title}
            </div>
            <div className="actions">
              <button className="btn btn-primary" onClick={() => goNext(upNext)}>
                <Icon name="play" fill size={16} /> Play now
              </button>
              <button
                className="btn btn-glass"
                onClick={() => nav(playlistId ? `/playlist/${playlistId}` : `/item/${item.showId ?? item.id}`, { replace: true })}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function delayLabel(d: number): string {
  if (d === 0) return 'In sync'
  return `${Math.abs(d).toFixed(1)}s ${d > 0 ? 'later' : 'earlier'}`
}

/** The thumbnail and time above the seek bar where the pointer is. */
function ScrubPreview({ fileId, info, t, x, width }: { fileId: number; info: TrickplayInfo | null; t: number; x: number; width: number }) {
  const w = 240
  let thumb: React.CSSProperties | null = null
  if (info) {
    const scale = w / info.width
    const per = info.columns * info.rows
    const n = Math.min(info.count - 1, Math.floor(t / info.interval))
    const at = n % per
    thumb = {
      width: w,
      height: Math.round(info.height * scale),
      backgroundImage: `url(/api/files/${fileId}/trickplay/${Math.floor(n / per)}.jpg)`,
      backgroundSize: `${info.columns * w}px ${info.rows * info.height * scale}px`,
      backgroundPosition: `-${(at % info.columns) * w}px -${Math.floor(at / info.columns) * info.height * scale}px`,
    }
  }
  const boxW = thumb ? w : 80
  return (
    <div className="scrub-preview" style={{ left: Math.max(0, Math.min(width - boxW, x - boxW / 2)), width: boxW }}>
      {thumb && <div className="scrub-thumb" style={thumb} />}
      <div className="scrub-time">{formatClock(t)}</div>
    </div>
  )
}

/** Searches OpenSubtitles for the file and adds the one picked. */
function SubtitleSearch({ fileId, language, onDone }: { fileId: number; language: string; onDone: (streams: MediaStream[], index: number) => void }) {
  const [lang, setLang] = useState(normLang(language) || 'eng')
  const [results, setResults] = useState<SubtitleResult[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<number | null>(null)
  useEffect(() => {
    let stale = false
    setResults(null)
    setError('')
    api.get<{ results: SubtitleResult[] }>(`/api/files/${fileId}/subtitles/search?lang=${lang}`).then(
      (r) => !stale && setResults(r.results),
      (e) => !stale && setError((e as Error).message),
    )
    return () => {
      stale = true
    }
  }, [fileId, lang])
  const pick = async (r: SubtitleResult) => {
    setBusy(r.fileId)
    setError('')
    try {
      const res = await api.post<{ streams: MediaStream[]; index: number }>(`/api/files/${fileId}/subtitles/download`, {
        fileId: r.fileId,
        language: r.language,
        hearingImpaired: r.hearingImpaired,
      })
      onDone(res.streams, res.index)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(null)
    }
  }
  const langs = commonLanguages.includes(lang) ? commonLanguages : [lang, ...commonLanguages]
  return (
    <div className="menu player-menu subsearch">
      <div className="menu-section">Find subtitles online</div>
      <label className="subsearch-lang">
        <select value={lang} onChange={(e) => setLang(e.target.value)} aria-label="Language">
          {langs.map((l) => (
            <option key={l} value={l}>{languageName(l)}</option>
          ))}
        </select>
      </label>
      {error && <p className="subsearch-note bad">{error}</p>}
      {!results && !error && <p className="subsearch-note">Searching OpenSubtitles…</p>}
      {results?.length === 0 && <p className="subsearch-note">Nothing found in {languageName(lang)}.</p>}
      {results?.slice(0, 30).map((r) => (
        <button key={r.fileId} disabled={busy !== null} onClick={() => pick(r)} title={r.name}>
          <span className="subsearch-name">
            {busy === r.fileId ? 'Downloading…' : r.name}
            <small>
              {[
                r.hashMatch && 'Made for this file',
                `${r.downloads.toLocaleString()} downloads`,
                r.hearingImpaired && 'SDH',
                r.ai && 'Machine translated',
                r.trusted && 'Trusted',
              ]
                .filter(Boolean)
                .join(' · ')}
            </small>
          </span>
        </button>
      ))}
    </div>
  )
}
