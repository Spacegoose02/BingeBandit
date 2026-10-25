import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import Hls from 'hls.js';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { api, img } from '../api';
import { Icon } from '../components/Icon';
import { PreRoll } from '../components/PreRoll';
import { roomPosition, TogetherPanel, useTogether } from '../components/Together';
import { useApi, useDocumentTitle } from '../hooks';
import { channelLabel, episodeLabel, formatClock, languageName, normLang, resolutionLabel } from '../format';
const TEXT_SUBS = new Set(['subrip', 'srt', 'ass', 'ssa', 'webvtt', 'mov_text', 'text']);
const IMAGE_SUBS = new Set(['hdmv_pgs_subtitle', 'dvd_subtitle', 'dvb_subtitle', 'xsub']);
const LADDER = [2160, 1080, 720, 480];
export default function Player() {
    const { id } = useParams();
    const [params] = useSearchParams();
    const { data, error } = useApi(`/api/items/${id}`);
    const { data: prefs } = useApi('/api/me/prefs');
    const startAt = Number(params.get('t')) || 0;
    const playlistId = Number(params.get('playlist')) || 0;
    const nav = useNavigate();
    const loc = useLocation();
    // Watch Together: join the room first, so playback starts where everyone is
    // (or follow the room to what it is playing now).
    const roomParam = params.get('together');
    const [joined, setJoined] = useState();
    useEffect(() => {
        setJoined(undefined);
        if (!roomParam) {
            setJoined({ roomId: null, start: 0, announce: false });
            return;
        }
        let stale = false;
        const moved = !!loc.state?.togetherMove;
        api.get(`/api/together/${roomParam}`).then((r) => {
            if (stale)
                return;
            const here = r.state.itemId === Number(id);
            if (!here && !moved) {
                nav(`/play/${r.state.itemId}?together=${roomParam}`, { replace: true });
                return;
            }
            setJoined({ roomId: roomParam, start: here ? roomPosition(r.state, r.now - Date.now()) : 0, announce: !here });
        }, (e) => !stale && setJoined({ roomId: null, start: 0, announce: false, error: e.message }));
        return () => {
            stale = true;
        };
        // Only on arriving at an item: starting a room from the player adds the parameter without rejoining.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);
    // Trailers and the intro video play only when a movie starts from the beginning.
    const [preRoll, setPreRoll] = useState();
    const isMovie = data?.item.kind === 'movie';
    useEffect(() => {
        setPreRoll(undefined);
        if (!prefs || !isMovie)
            return;
        if (startAt > 0 || playlistId || roomParam || (!prefs.preRollTrailers && !prefs.preRollIntro)) {
            setPreRoll('done');
            return;
        }
        let stale = false;
        api
            .get(`/api/items/${id}/preroll`)
            .then((p) => !stale && setPreRoll(p.trailers.length || p.intro ? p : 'done'))
            .catch(() => !stale && setPreRoll('done'));
        return () => {
            stale = true;
        };
    }, [id, isMovie, prefs, startAt, playlistId]);
    const preRollDone = useCallback(() => setPreRoll('done'), []);
    useDocumentTitle(data ? (data.item.kind === 'episode' ? `${data.item.showTitle} ${episodeLabel(data.item)}` : data.item.title) : 'Player');
    if (error)
        return _jsx("div", { className: "player", children: _jsx("div", { className: "player-error", children: error }) });
    if (!data || !prefs || !joined || (isMovie && !preRoll))
        return _jsx("div", { className: "player", children: _jsx("div", { className: "spinner" }) });
    if (isMovie && preRoll && preRoll !== 'done')
        return _jsx(PreRoll, { data: preRoll, movieTitle: data.item.title, onDone: preRollDone });
    return (_jsx(PlayerView, { detail: data, prefs: prefs, startAt: joined.roomId ? joined.start : startAt, fileId: Number(params.get('file')) || 0, playlistId: playlistId, together: joined }, id));
}
/** Whether this browser can play the file as-is (no server work at all). */
function canDirectPlay(f, video) {
    if (f.hdr)
        return false;
    const mime = { mp4: 'video/mp4', m4v: 'video/mp4', mov: 'video/mp4', webm: 'video/webm' }[f.container];
    // Ask about the file's bit depth too: browsers that take 8-bit H.264 or
    // HEVC often can't decode 10-bit, and then play the sound over a black picture.
    const deep = f.bitDepth > 8;
    if (deep && f.videoCodec === 'h264')
        return false; // High 10 doesn't decode in browsers
    const v = (deep
        ? { hevc: 'hvc1.2.4.L120.90', vp9: 'vp09.02.10.10', av1: 'av01.0.08M.10' }
        : { h264: 'avc1.640028', hevc: 'hvc1.1.6.L120.90', vp9: 'vp09.00.10.08', av1: 'av01.0.08M.08' })[f.videoCodec];
    const a = { aac: 'mp4a.40.2', mp3: 'mp3', opus: 'opus', flac: 'flac', vorbis: 'vorbis' }[f.audioCodec];
    if (!mime || !v || (f.audioCodec && !a))
        return false;
    return video.canPlayType(`${mime}; codecs="${v}${a ? `,${a}` : ''}"`) !== '';
}
let outputChannels;
/** Speaker channels this device outputs (2 unless a surround system is connected). */
function speakerChannels() {
    if (outputChannels === undefined) {
        outputChannels = 2;
        try {
            const ctx = new AudioContext();
            outputChannels = Math.max(2, ctx.destination.maxChannelCount);
            ctx.close().catch(() => undefined);
        }
        catch {
            // No Web Audio: assume stereo.
        }
    }
    return outputChannels;
}
/**
 * Audio this browser can take inside the server's HLS stream, so surround
 * tracks are copied or kept 5.1 instead of mixed down to stereo. hls.js can
 * pass AC-3 through from MPEG-TS (not E-AC-3); Safari's own player takes both.
 */
function audioCaps(video, stereoOnly) {
    if (stereoOnly)
        return { audioCodecs: ['aac'], maxAudioChannels: 2 };
    const codecs = ['aac'];
    const ok = (type) => Hls.isSupported() ? (window.MediaSource?.isTypeSupported(type) ?? false) : video.canPlayType(type) !== '';
    if (ok('audio/mp4; codecs="ac-3"'))
        codecs.push('ac3');
    if (!Hls.isSupported() && ok('audio/mp4; codecs="ec-3"'))
        codecs.push('eac3');
    return { audioCodecs: codecs, maxAudioChannels: Math.min(speakerChannels(), 8) };
}
/**
 * Picks audio and subtitles from the user's preferences. Returns the audio
 * stream (-1 when it's the file default, so direct play stays possible) and
 * the subtitle stream, which is either rendered by us or burned in.
 */
function pickTracks(f, p) {
    const audios = f.streams.filter((s) => s.type === 'audio');
    const fileDefault = audios.find((s) => s.default) ?? audios[0];
    let chosen = fileDefault;
    if (p.audioLanguage)
        chosen = audios.find((s) => normLang(s.language) === normLang(p.audioLanguage)) ?? fileDefault;
    const audio = chosen && chosen !== fileDefault ? chosen.index : -1;
    const subs = f.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec)));
    const want = normLang(p.subtitleLanguage);
    const inLang = subs.filter((s) => normLang(s.language) === want);
    // Prefer text over image subtitles and plain over SDH.
    const rank = (s) => (IMAGE_SUBS.has(s.codec) ? 2 : 0) + (s.hearingImpaired ? 1 : 0);
    const best = (list) => [...list].sort((a, b) => rank(a) - rank(b))[0];
    const full = best(inLang.filter((s) => !s.forced)) ?? best(subs.filter((s) => !s.language && !s.forced));
    const forced = best(inLang.filter((s) => s.forced));
    const foreignAudio = !!chosen?.language && normLang(chosen.language) !== want;
    let sub;
    switch (p.subtitleMode) {
        case 'always':
            sub = full ?? forced;
            break;
        case 'auto':
            sub = foreignAudio ? full ?? forced : forced;
            break;
        case 'forced':
            sub = forced;
            break;
    }
    return { audio, sub };
}
function parseVtt(text) {
    const toSec = (t) => {
        const p = t.trim().split(':').map(Number);
        return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : p[0] * 60 + p[1];
    };
    const cues = [];
    for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
        const lines = block.split('\n');
        const i = lines.findIndex((l) => l.includes('-->'));
        if (i < 0)
            continue;
        const [a, b] = lines[i].split('-->');
        const body = lines.slice(i + 1).join('\n').replace(/<[^>]+>/g, '').trim();
        if (body)
            cues.push({ start: toSec(a), end: toSec(b.trim().split(' ')[0]), text: body });
    }
    return cues;
}
function streamLabel(s) {
    const bits = [languageName(s.language)];
    if (s.type === 'audio')
        bits.push(`${s.codec.toUpperCase()} ${channelLabel(s.channels ?? 2)}`);
    if (s.title)
        bits.push(s.title);
    if (s.forced)
        bits.push('Forced');
    if (s.hearingImpaired)
        bits.push('SDH');
    if (s.external)
        bits.push('External');
    if (IMAGE_SUBS.has(s.codec))
        bits.push('Burned in');
    return bits.join(' · ');
}
function PlayerView({ detail, prefs, startAt, fileId, playlistId, together, }) {
    const { item, files = [] } = detail;
    const file = files.find((f) => f.id === fileId) ?? files[0];
    const nav = useNavigate();
    const initial = useRef(file ? pickTracks(file, prefs) : { audio: -1 });
    const containerRef = useRef(null);
    const videoRef = useRef(null);
    const hlsRef = useRef(null);
    const offsetRef = useRef(0); // file time at video time 0 (transcodes start mid-file)
    const transcodeRef = useRef(null);
    const startedRef = useRef(false);
    const forceTranscode = useRef(false); // direct play failed once; don't try again
    const adaptiveRef = useRef(false); // the stream has several qualities; the player picks
    const stereoOnly = useRef(false); // surround audio failed once; ask for stereo
    const settlingRef = useRef(true); // a load or seek is in progress, so waiting isn't a stall
    const stalls = useRef([]);
    const retries = useRef(0);
    // Seconds actually played (for watch history): small forward steps of
    // currentTime while playing; seeks and pauses add nothing.
    const playedRef = useRef(0);
    const lastTick = useRef(null);
    const directSince = useRef(0); // where direct play started, to check that the picture shows too
    const playSession = useRef(Math.random().toString(36).slice(2) + Date.now().toString(36));
    const hideTimer = useRef();
    const initialSub = initial.current.sub;
    const [choice, setChoice] = useState({
        quality: 0,
        audio: initial.current.audio,
        burn: initialSub && IMAGE_SUBS.has(initialSub.codec) ? initialSub.index : -1,
    });
    const choiceRef = useRef(choice);
    choiceRef.current = choice;
    const [mode, setMode] = useState('direct');
    const [subIndex, setSubIndex] = useState(initialSub && TEXT_SUBS.has(initialSub.codec) ? initialSub.index : -1);
    const [cues, setCues] = useState([]);
    const [time, setTime] = useState(startAt);
    const [paused, setPaused] = useState(true);
    const [buffering, setBuffering] = useState(true);
    const [volume, setVolume] = useState(() => Number(localStorage.getItem('bb.volume') ?? 1));
    const [muted, setMuted] = useState(false);
    const [fullscreen, setFullscreen] = useState(false);
    const [controls, setControls] = useState(true);
    const [menu, setMenu] = useState(null);
    const [error, setError] = useState('');
    const [toast, setToast] = useState(together.error ? `Couldn't join the watch party: ${together.error}` : '');
    const [upNext, setUpNext] = useState(null);
    const [countdown, setCountdown] = useState(10);
    const duration = file?.duration || (item.runtime ?? 0) * 60;
    const position = () => offsetRef.current + (videoRef.current?.currentTime ?? 0);
    const audioStreams = file?.streams.filter((s) => s.type === 'audio') ?? [];
    const subStreams = file?.streams.filter((s) => s.type === 'subtitle' && (TEXT_SUBS.has(s.codec) || IMAGE_SUBS.has(s.codec))) ?? [];
    const stopTranscode = useCallback(() => {
        const id = transcodeRef.current;
        transcodeRef.current = null;
        if (id)
            api.del(`/api/transcode/${id}`, { keepalive: true }).catch(() => undefined);
    }, []);
    const report = useCallback((stopped = false, finished = false) => {
        const video = videoRef.current;
        if (!video || !file || !startedRef.current)
            return;
        api
            .post('/api/progress', {
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
            played: Math.round(playedRef.current),
        }, { keepalive: true })
            .catch(() => undefined);
    }, 
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [file, item.id, duration, mode]);
    const load = useCallback(async (at, c) => {
        const video = videoRef.current;
        if (!video || !file)
            return;
        setError('');
        setBuffering(true);
        settlingRef.current = true;
        hlsRef.current?.destroy();
        hlsRef.current = null;
        const retry = (why) => {
            if (retries.current < 2) {
                retries.current++;
                stereoOnly.current = true; // in case surround audio was the problem
                // Resume from where the viewer is, not where this load began.
                const resumeAt = startedRef.current ? offsetRef.current + (video.currentTime || 0) : at;
                window.setTimeout(() => load(resumeAt, choiceRef.current), 1500);
            }
            else
                setError(why);
        };
        if (c.quality === 0 && c.audio < 0 && c.burn < 0 && !forceTranscode.current && canDirectPlay(file, video)) {
            stopTranscode();
            offsetRef.current = 0;
            setMode('direct');
            directSince.current = at;
            adaptiveRef.current = false;
            video.src = `/api/files/${file.id}/direct`;
            video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true });
        }
        else {
            try {
                const res = await api.post(`/api/files/${file.id}/transcode`, {
                    start: at,
                    maxHeight: c.quality,
                    audioStream: c.audio,
                    burnSubtitle: c.burn,
                    replace: transcodeRef.current ?? '',
                    // At "original" quality, let the player switch between qualities
                    // itself when the server offers it. A picked quality stays fixed.
                    adaptive: c.quality === 0,
                    ...audioCaps(video, stereoOnly.current),
                });
                transcodeRef.current = res.session.id;
                adaptiveRef.current = (res.session.variants?.length ?? 0) > 1;
                offsetRef.current = at;
                setMode('transcode');
                if (Hls.isSupported()) {
                    const hls = new Hls({ startPosition: 0, maxBufferLength: 60, backBufferLength: 120 });
                    let mediaErrors = 0;
                    hls.on(Hls.Events.ERROR, (_e, d) => {
                        if (!d.fatal)
                            return;
                        if (d.type === Hls.ErrorTypes.NETWORK_ERROR)
                            hls.startLoad();
                        // A second decode failure starts over (with stereo audio, in case surround was the problem).
                        else if (d.type === Hls.ErrorTypes.MEDIA_ERROR && mediaErrors++ === 0)
                            hls.recoverMediaError();
                        else
                            retry('Playback failed. The server could not convert this file.');
                    });
                    hls.loadSource(res.playlist);
                    hls.attachMedia(video);
                    hlsRef.current = hls;
                }
                else {
                    video.src = res.playlist; // Safari plays HLS natively
                }
            }
            catch (e) {
                retry(e.message);
                return;
            }
        }
        video.play().catch(() => setPaused(true));
    }, [file, stopTranscode]);
    const apply = (c) => {
        setChoice(c);
        setMenu(null);
        load(position(), c);
    };
    // First load, and cleanup when leaving the player.
    useEffect(() => {
        if (!file)
            return;
        load(startAt, choiceRef.current);
        // Closing the tab or reloading skips React cleanup; pagehide still fires.
        const onHide = () => {
            report(true);
            stopTranscode();
        };
        window.addEventListener('pagehide', onHide);
        return () => {
            window.removeEventListener('pagehide', onHide);
            report(true);
            hlsRef.current?.destroy();
            stopTranscode();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useEffect(() => {
        const t = window.setInterval(() => report(), 10000);
        return () => window.clearInterval(t);
    }, [report]);
    useEffect(() => {
        if (videoRef.current) {
            videoRef.current.volume = volume;
            videoRef.current.muted = muted;
        }
        localStorage.setItem('bb.volume', String(volume));
    }, [volume, muted]);
    useEffect(() => {
        if (!toast)
            return;
        const t = window.setTimeout(() => setToast(''), 6000);
        return () => window.clearTimeout(t);
    }, [toast]);
    useEffect(() => {
        if (subIndex < 0 || !file)
            return setCues([]);
        let cancelled = false;
        fetch(`/api/files/${file.id}/subtitles/${subIndex}.vtt`, { credentials: 'same-origin' })
            .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.statusText))))
            .then((t) => !cancelled && setCues(parseVtt(t)))
            .catch(() => !cancelled && setToast('Could not load those subtitles'));
        return () => {
            cancelled = true;
        };
    }, [subIndex, file]);
    const seekTo = (t) => {
        const video = videoRef.current;
        if (!video)
            return;
        t = Math.max(0, Math.min(t, Math.max(duration - 1, 0)));
        setTime(t);
        settlingRef.current = true;
        if (mode === 'direct') {
            video.currentTime = t;
            return;
        }
        const local = t - offsetRef.current;
        const s = video.seekable;
        if (local >= 0 && s.length && local <= s.end(s.length - 1))
            video.currentTime = local;
        else
            load(t, choiceRef.current); // outside what has been transcoded: restart there
    };
    // Three stalls within a minute (not caused by seeking) means the
    // connection can't keep up: drop one quality step.
    const onStall = () => {
        setBuffering(true);
        if (!prefs.autoQuality || adaptiveRef.current || settlingRef.current || !startedRef.current || !file)
            return;
        const now = Date.now();
        stalls.current = [...stalls.current.filter((t) => now - t < 60000), now];
        if (stalls.current.length < 3)
            return;
        stalls.current = [];
        const current = choiceRef.current.quality || file.height;
        const lower = LADDER.find((q) => q < current && q < file.height);
        if (!lower)
            return;
        setToast(`Playback kept pausing to buffer, so quality was lowered to ${resolutionLabel(lower)}.`);
        apply({ ...choiceRef.current, quality: lower });
    };
    // Watch Together: everyone else's play, pause, seek and next item land
    // here; this viewer's own go out through share().
    const [, setParams] = useSearchParams();
    const [roomId, setRoomId] = useState(together.roomId);
    const roomJoined = useRef(false);
    const pauseOnStart = useRef(false); // joined a paused room: stop once the first load starts playing
    const tg = useTogether(roomId, (s, skew) => {
        if (s.itemId !== item.id) {
            nav(`/play/${s.itemId}?together=${roomId}`, { replace: true });
            return;
        }
        const v = videoRef.current;
        if (!v)
            return;
        const target = roomPosition(s, skew);
        const wasPaused = v.paused;
        const jump = Math.abs(position() - target) > 2;
        if (jump)
            seekTo(target);
        if (s.paused && !v.paused)
            v.pause();
        else if (!s.paused && v.paused)
            v.play().catch(() => setToast('Press play to catch up with the others'));
        const what = s.paused !== wasPaused ? (s.paused ? 'paused' : 'pressed play') : jump ? `skipped to ${formatClock(target)}` : '';
        if (what && s.byName)
            setToast(`${s.byName} ${what}`);
    });
    const share = (paused, at = position()) => {
        if (roomId)
            tg.send({ paused, position: at });
    };
    // On joining: match the room's play/pause, and tell the room when this viewer moved it to the next item.
    useEffect(() => {
        if (!tg.room || roomJoined.current)
            return;
        roomJoined.current = true;
        if (together.announce && roomId === together.roomId)
            tg.send({ itemId: item.id, paused: false, position: 0 });
        else if (tg.room.state.paused) {
            const v = videoRef.current;
            if (v && !v.paused && startedRef.current)
                v.pause();
            else
                pauseOnStart.current = true;
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tg.room]);
    // Stay within a few seconds of the room while it plays.
    useEffect(() => {
        if (!roomId)
            return;
        const t = window.setInterval(() => {
            const v = videoRef.current;
            const r = tg.room;
            if (!v || !r || r.state.paused || v.paused || settlingRef.current)
                return;
            if (Math.abs(position() - tg.position()) > 3)
                seekTo(tg.position());
        }, 5000);
        return () => window.clearInterval(t);
    });
    useEffect(() => {
        if (!tg.ended)
            return;
        setToast('The watch party has ended.');
        setRoomId(null);
    }, [tg.ended]);
    const startRoom = async () => {
        const v = videoRef.current;
        const r = await api.post('/api/together', { itemId: item.id, position: position(), paused: v?.paused ?? true });
        roomJoined.current = true;
        setRoomId(r.id);
        setParams((p) => {
            p.set('together', r.id);
            p.delete('t');
            return p;
        }, { replace: true });
    };
    const leaveRoom = () => {
        setRoomId(null);
        setMenu(null);
        setParams((p) => {
            p.delete('together');
            return p;
        }, { replace: true });
    };
    const seek = (t) => {
        seekTo(t);
        share(videoRef.current?.paused ?? true, Math.max(0, Math.min(t, Math.max(duration - 1, 0))));
    };
    const togglePlay = () => {
        const v = videoRef.current;
        if (!v)
            return;
        if (v.paused) {
            v.play().catch(() => undefined);
            share(false);
        }
        else {
            v.pause();
            share(true);
        }
    };
    const toggleFullscreen = () => {
        if (document.fullscreenElement)
            document.exitFullscreen();
        else
            containerRef.current?.requestFullscreen().catch(() => undefined);
    };
    const poke = useCallback(() => {
        setControls(true);
        window.clearTimeout(hideTimer.current);
        hideTimer.current = window.setTimeout(() => {
            if (!videoRef.current?.paused) {
                setControls(false);
                setMenu(null);
            }
        }, 3000);
    }, []);
    useEffect(() => {
        const onKey = (e) => {
            if (e.target.tagName === 'SELECT')
                return;
            const actions = {
                ' ': togglePlay,
                k: togglePlay,
                ArrowLeft: () => seek(position() - 10),
                ArrowRight: () => seek(position() + 10),
                j: () => seek(position() - 10),
                l: () => seek(position() + 30),
                f: toggleFullscreen,
                m: () => setMuted((m) => !m),
                ArrowUp: () => setVolume((v) => Math.min(1, v + 0.1)),
                ArrowDown: () => setVolume((v) => Math.max(0, v - 0.1)),
            };
            const fn = actions[e.key];
            if (fn) {
                e.preventDefault();
                fn();
                poke();
            }
        };
        const onFs = () => setFullscreen(!!document.fullscreenElement);
        window.addEventListener('keydown', onKey);
        document.addEventListener('fullscreenchange', onFs);
        return () => {
            window.removeEventListener('keydown', onKey);
            document.removeEventListener('fullscreenchange', onFs);
        };
    });
    const onEnded = async () => {
        report(false, true);
        const done = playlistId ? `/playlist/${playlistId}` : item.kind === 'episode' ? `/item/${item.showId}` : `/item/${item.id}`;
        if (!playlistId && item.kind !== 'episode')
            return nav(done, { replace: true });
        try {
            const next = await api.get(playlistId ? `/api/playlists/${playlistId}/next?after=${item.id}` : `/api/items/${item.id}/next`);
            if (next) {
                setCountdown(10);
                setUpNext(next);
            }
            else
                nav(done, { replace: true });
        }
        catch {
            nav(-1);
        }
    };
    const nextUrl = (next) => {
        const q = new URLSearchParams();
        if (playlistId)
            q.set('playlist', String(playlistId));
        if (roomId)
            q.set('together', roomId);
        return `/play/${next.id}${q.toString() ? `?${q}` : ''}`;
    };
    // Moving on in a watch party takes everyone along.
    const goNext = (next) => nav(nextUrl(next), { replace: true, state: { togetherMove: !!roomId } });
    // Intro and credits markers: offer a button or act automatically, once per
    // visit, so seeking back into the intro doesn't skip it again.
    const intro = detail.markers?.find((m) => m.kind === 'intro');
    const credits = detail.markers?.find((m) => m.kind === 'credits');
    const introDone = useRef(false);
    const creditsDone = useRef(false);
    const inIntro = !!intro && time >= intro.start && time < intro.end - 1;
    const inCredits = !!credits && time >= credits.start && time < Math.min(credits.end, duration) - 1 && !upNext;
    const hasNext = item.kind === 'episode' || !!playlistId;
    const skipIntro = () => {
        introDone.current = true;
        if (intro)
            seek(intro.end);
    };
    // A movie with a scene after the credits jumps to that scene instead of ending.
    const afterCredits = !hasNext && !!credits && credits.end < duration - 5;
    const skipCredits = () => {
        creditsDone.current = true;
        if (afterCredits && credits)
            seek(credits.end);
        else
            onEnded();
    };
    useEffect(() => {
        if (inIntro && prefs.skipIntro === 'auto' && !introDone.current) {
            introDone.current = true;
            setToast('Skipped the intro');
            skipIntro();
        }
        if (inCredits && prefs.skipCredits === 'auto' && !creditsDone.current && (hasNext || afterCredits)) {
            if (afterCredits)
                setToast('Skipped to the scene after the credits');
            skipCredits();
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [inIntro, inCredits]);
    useEffect(() => {
        if (!upNext || !prefs.autoPlayNext)
            return;
        if (countdown <= 0) {
            goNext(upNext);
            return;
        }
        const t = window.setTimeout(() => setCountdown((c) => c - 1), 1000);
        return () => window.clearTimeout(t);
    }, [upNext, countdown, nav, prefs.autoPlayNext]);
    const changeAudio = (a) => {
        const def = audioStreams.find((s) => s.default) ?? audioStreams[0];
        apply({ ...choice, audio: def && def.index === a ? -1 : a });
    };
    const changeSubtitle = (s) => {
        setMenu(null);
        if (s && IMAGE_SUBS.has(s.codec)) {
            setSubIndex(-1);
            if (choice.burn !== s.index)
                apply({ ...choice, burn: s.index });
            return;
        }
        setSubIndex(s ? s.index : -1);
        if (choice.burn >= 0)
            apply({ ...choice, burn: -1 });
    };
    if (!file)
        return _jsx("div", { className: "player", children: _jsx("div", { className: "player-error", children: "This item has no playable file." }) });
    const qualities = [0, ...LADDER.filter((q) => q < file.height)];
    const activeCue = cues.find((c) => time >= c.start && time <= c.end);
    const pct = duration ? (time / duration) * 100 : 0;
    const title = item.kind === 'episode' ? item.showTitle : item.title;
    const subtitle = item.kind === 'episode' ? `${episodeLabel(item)} · ${item.title}` : item.year ? String(item.year) : '';
    const effectiveAudio = choice.audio >= 0 ? choice.audio : (audioStreams.find((x) => x.default) ?? audioStreams[0])?.index;
    return (_jsxs("div", { ref: containerRef, className: `player ${controls || paused ? 'show-controls' : ''}`, onMouseMove: poke, onTouchStart: poke, children: [_jsx("video", { ref: videoRef, className: "player-video", playsInline: true, onClick: togglePlay, onDoubleClick: toggleFullscreen, onPlay: () => setPaused(false), onPause: () => {
                    setPaused(true);
                    report();
                }, onSeeked: () => report(), onPlaying: () => {
                    // Report at once, so plugins following the player (Hue) see starts and resumes.
                    if (!startedRef.current || paused)
                        queueMicrotask(() => report());
                    if (pauseOnStart.current) {
                        pauseOnStart.current = false;
                        videoRef.current?.pause();
                    }
                    startedRef.current = true;
                    settlingRef.current = false;
                    retries.current = 0;
                    setBuffering(false);
                }, onWaiting: onStall, onTimeUpdate: () => {
                    const v = videoRef.current;
                    if (v && !v.paused) {
                        const now = v.currentTime;
                        const step = lastTick.current === null ? 0 : now - lastTick.current;
                        if (step > 0 && step < 2)
                            playedRef.current += step;
                        lastTick.current = now;
                    }
                    // A browser that can't decode the picture often just plays the
                    // sound over black, with no error. Two seconds in with no frame
                    // decoded: let the server convert the video instead.
                    if (v && mode === 'direct' && file?.videoCodec && !forceTranscode.current && v.currentTime - directSince.current > 2) {
                        const frames = v.getVideoPlaybackQuality?.().totalVideoFrames;
                        if (v.videoWidth === 0 || frames === 0) {
                            forceTranscode.current = true;
                            load(position(), choiceRef.current);
                            return;
                        }
                    }
                    setTime(position());
                }, onSeeking: () => (lastTick.current = null), onEmptied: () => (lastTick.current = null), onEnded: onEnded, onError: () => {
                    // The browser couldn't decode the file after all: let the server convert it.
                    // A converted stream that fails once gets another go with stereo audio.
                    if (mode === 'direct')
                        forceTranscode.current = true;
                    else if (!stereoOnly.current && !Hls.isSupported())
                        stereoOnly.current = true;
                    else
                        return;
                    load(position(), choiceRef.current);
                } }), activeCue && (_jsx("div", { className: `subtitle-cue size-${prefs.subtitleSize} ${prefs.subtitleBackground ? 'boxed' : ''}`, children: _jsx("span", { children: activeCue.text }) })), buffering && !error && _jsx("div", { className: "spinner" }), error && _jsx("div", { className: "player-error", children: error }), toast && _jsx("div", { className: "player-toast", children: toast }), inIntro && prefs.skipIntro === 'button' && (_jsxs("button", { className: "skip-btn", onClick: skipIntro, children: ["Skip Intro ", _jsx(Icon, { name: "next", size: 18 })] })), inCredits && prefs.skipCredits === 'button' && (_jsxs("button", { className: "skip-btn", onClick: skipCredits, children: [hasNext ? 'Next' : 'Skip Credits', " ", _jsx(Icon, { name: "next", size: 18 })] })), _jsxs("div", { className: "player-top", children: [_jsx("button", { className: "icon-btn", onClick: () => nav(-1), "aria-label": "Back", children: _jsx(Icon, { name: "back", size: 28 }) }), _jsxs("div", { children: [_jsx("div", { className: "player-title", children: title }), _jsx("div", { className: "player-sub", children: subtitle })] })] }), _jsxs("div", { className: "player-bottom", onClick: (e) => e.stopPropagation(), children: [_jsx("div", { className: "seek", children: _jsx("input", { type: "range", min: 0, max: duration || 1, step: 1, value: Math.min(time, duration), onChange: (e) => seek(Number(e.target.value)), style: { '--pct': `${pct}%` }, "aria-label": "Seek" }) }), _jsxs("div", { className: "player-controls", children: [_jsx("button", { className: "icon-btn", onClick: togglePlay, "aria-label": paused ? 'Play' : 'Pause', children: _jsx(Icon, { name: paused ? 'play' : 'pause', fill: true, size: 28 }) }), _jsx("button", { className: "icon-btn", onClick: () => seek(position() - 10), "aria-label": "Back 10 seconds", children: _jsx(Icon, { name: "replay10", size: 24 }) }), _jsx("button", { className: "icon-btn", onClick: () => seek(position() + 30), "aria-label": "Forward 30 seconds", children: _jsx(Icon, { name: "forward30", size: 24 }) }), _jsxs("div", { className: "volume", children: [_jsx("button", { className: "icon-btn", onClick: () => setMuted((m) => !m), "aria-label": muted ? 'Unmute' : 'Mute', children: _jsx(Icon, { name: muted || volume === 0 ? 'mute' : 'volume', size: 22 }) }), _jsx("input", { type: "range", min: 0, max: 1, step: 0.05, value: muted ? 0 : volume, onChange: (e) => {
                                            setMuted(false);
                                            setVolume(Number(e.target.value));
                                        }, style: { '--pct': `${(muted ? 0 : volume) * 100}%` }, "aria-label": "Volume" })] }), _jsxs("div", { className: "time", children: [formatClock(time), " / ", formatClock(duration)] }), _jsx("div", { className: "spacer" }), _jsx("span", { className: `mode-pill ${mode}`, children: mode === 'direct' ? 'Direct Play' : choice.quality ? `${resolutionLabel(choice.quality)} Transcode` : 'Transcode' }), _jsxs("div", { className: "menu-anchor", children: [_jsxs("button", { className: `icon-btn ${roomId ? 'active' : ''}`, onClick: () => setMenu(menu === 'together' ? null : 'together'), "aria-label": "Watch together", title: roomId ? `Watching together${tg.room ? ` (${tg.room.members.length})` : ''}` : 'Watch together', children: [_jsx(Icon, { name: "users", size: 22 }), roomId && tg.room && _jsx("span", { className: "together-count", children: tg.room.members.length })] }), menu === 'together' && (_jsx(TogetherPanel, { itemId: item.id, room: tg.room, roomId: roomId, onStart: startRoom, onLeave: leaveRoom }))] }), _jsxs("div", { className: "menu-anchor", children: [_jsx("button", { className: "icon-btn", onClick: () => setMenu(menu === 'settings' ? null : 'settings'), "aria-label": "Playback settings", children: _jsx(Icon, { name: "sliders", size: 22 }) }), menu === 'settings' && (_jsxs("div", { className: "menu player-menu", children: [_jsx("div", { className: "menu-section", children: "Quality" }), qualities.map((q) => (_jsx("button", { className: q === choice.quality ? 'active' : '', onClick: () => apply({ ...choice, quality: q }), children: q === 0 ? `Original (${resolutionLabel(file.height)})` : resolutionLabel(q) }, q))), audioStreams.length > 1 && _jsx("div", { className: "menu-section", children: "Audio" }), audioStreams.length > 1 &&
                                                audioStreams.map((s) => (_jsx("button", { className: s.index === effectiveAudio ? 'active' : '', onClick: () => changeAudio(s.index), children: streamLabel(s) }, s.index))), subStreams.length > 0 && _jsx("div", { className: "menu-section", children: "Subtitles" }), subStreams.length > 0 && (_jsx("button", { className: subIndex === -1 && choice.burn === -1 ? 'active' : '', onClick: () => changeSubtitle(null), children: "Off" })), subStreams.map((s) => (_jsx("button", { className: subIndex === s.index || choice.burn === s.index ? 'active' : '', onClick: () => changeSubtitle(s), children: streamLabel(s) }, s.index)))] }))] }), _jsx("button", { className: "icon-btn", onClick: toggleFullscreen, "aria-label": "Fullscreen", children: _jsx(Icon, { name: fullscreen ? 'exitFullscreen' : 'fullscreen', size: 22 }) })] })] }), upNext && (_jsxs("div", { className: "up-next", children: [img(upNext.backdrop || upNext.showBackdrop, 'w500') && _jsx("img", { src: img(upNext.backdrop || upNext.showBackdrop, 'w500'), alt: "" }), _jsxs("div", { children: [_jsx("div", { className: "muted small", children: prefs.autoPlayNext ? `Up next in ${countdown}s` : 'Up next' }), _jsx("div", { className: "up-next-title", children: upNext.kind === 'episode' ? `${upNext.showTitle} · ${episodeLabel(upNext)} · ${upNext.title}` : upNext.title }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-primary", onClick: () => goNext(upNext), children: [_jsx(Icon, { name: "play", fill: true, size: 16 }), " Play now"] }), _jsx("button", { className: "btn btn-glass", onClick: () => nav(playlistId ? `/playlist/${playlistId}` : `/item/${item.showId ?? item.id}`, { replace: true }), children: "Cancel" })] })] })] }))] }));
}
