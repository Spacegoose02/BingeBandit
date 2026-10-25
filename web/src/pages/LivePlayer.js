import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import Hls from 'hls.js';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../api';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { ChannelLogo, channelNo, clock } from './LiveGuide';
/** Full-screen live channel player with channel up/down and a mini guide. */
export default function LivePlayer() {
    const { id } = useParams();
    const nav = useNavigate();
    const { data: channels, reload } = useApi('/api/live/channels', 60000);
    const channel = channels?.find((c) => String(c.id) === id);
    useDocumentTitle(channel ? `${channelNo(channel.number)} ${channel.name}` : 'Live TV');
    const videoRef = useRef(null);
    const containerRef = useRef(null);
    const hlsRef = useRef(null);
    const hideTimer = useRef();
    const failures = useRef(0);
    const viewerRef = useRef('');
    const stallSecs = useRef(12);
    const [attempt, setAttempt] = useState(0); // bumped to rejoin after a failure
    const [error, setError] = useState('');
    const [reconnecting, setReconnecting] = useState(false);
    const [buffering, setBuffering] = useState(true);
    const [paused, setPaused] = useState(false);
    const [behind, setBehind] = useState(0); // seconds behind live
    const [guide, setGuide] = useState(() => localStorage.getItem('bb.miniGuide') === '1');
    const [controls, setControls] = useState(true);
    const [muted, setMuted] = useState(false);
    const [, tick] = useState(0);
    // A new channel starts with a clean slate.
    useEffect(() => {
        failures.current = 0;
        setError('');
    }, [id]);
    // Start (or switch) the stream whenever the channel changes, and rejoin
    // after a failure.
    useEffect(() => {
        if (!id)
            return;
        const video = videoRef.current;
        if (!video)
            return;
        let hls = null;
        let viewer = '';
        let cancelled = false;
        let retryTimer = 0;
        let ping = 0;
        setBuffering(true);
        setPaused(false);
        setBehind(0);
        // Rejoin on its own instead of leaving an error up: streams drop, providers
        // hiccup and the server restarts the conversion. Quick retries first, then
        // every 15 s while the error shows.
        const fail = (msg) => {
            if (cancelled)
                return;
            cancelled = true;
            hls?.destroy();
            hls = null;
            hlsRef.current = null;
            failures.current++;
            if (failures.current > 3)
                setError(msg);
            setReconnecting(true);
            retryTimer = window.setTimeout(() => setAttempt((n) => n + 1), failures.current > 3 ? 15000 : 1500 * failures.current);
        };
        api
            .post(`/api/live/channels/${id}/watch`)
            .then((res) => {
            if (cancelled) {
                api.del(`/api/live/viewers/${res.viewerId}`).catch(() => undefined);
                return;
            }
            viewer = res.viewerId;
            viewerRef.current = viewer;
            stallSecs.current = res.stallSecs ?? 12;
            // Paused players stop fetching the playlist; this keeps the seat.
            ping = window.setInterval(() => {
                api.post(`/api/live/viewers/${viewer}/ping`).catch((e) => e.status === 410 && fail('This channel stopped.'));
            }, 10000);
            if (Hls.isSupported()) {
                // Start behind live (the admin's buffer setting, in 2 s segments) so short
                // upstream hiccups don't stall playback; the server keeps a longer
                // window for pausing and rewinding.
                const segs = Math.max(2, Math.ceil((res.bufferSecs ?? 8) / 2));
                hls = new Hls({ liveSyncDurationCount: segs, manifestLoadingTimeOut: 30000, manifestLoadingMaxRetry: 6, levelLoadingMaxRetry: 6, fragLoadingMaxRetry: 6 });
                hlsRef.current = hls;
                hls.on(Hls.Events.ERROR, (_e, d) => {
                    if (!d.fatal)
                        return;
                    if (d.type === Hls.ErrorTypes.NETWORK_ERROR) {
                        const status = d.response?.code;
                        if (status === 503 || status === 410 || status === 404)
                            fail('This channel is unavailable right now.');
                        else
                            hls?.startLoad();
                    }
                    else if (d.type === Hls.ErrorTypes.MEDIA_ERROR)
                        hls?.recoverMediaError();
                    else
                        fail('Playback failed.');
                });
                hls.loadSource(res.playlist);
                hls.attachMedia(video);
            }
            else {
                video.src = res.playlist;
            }
            video.play().catch(() => undefined);
        })
            .catch((e) => fail(e.message || 'This channel is unavailable right now.'));
        const leave = () => viewer && api.del(`/api/live/viewers/${viewer}`, { keepalive: true }).catch(() => undefined);
        window.addEventListener('pagehide', leave);
        return () => {
            cancelled = true;
            window.clearTimeout(retryTimer);
            window.clearInterval(ping);
            window.removeEventListener('pagehide', leave);
            hls?.destroy();
            hlsRef.current = null;
            viewerRef.current = '';
            leave();
        };
    }, [id, attempt]);
    // Buffering for longer than the server's stall time: tell it, so it can
    // move the channel to its next stream (it checks the stream really is the
    // problem first). Repeats every stall period while it lasts.
    useEffect(() => {
        if (!buffering || paused || reconnecting)
            return;
        const started = Date.now();
        let next = stallSecs.current;
        const t = window.setInterval(() => {
            const secs = (Date.now() - started) / 1000;
            if (secs < next || !viewerRef.current)
                return;
            next += stallSecs.current;
            api.post(`/api/live/viewers/${viewerRef.current}/buffering`, { seconds: Math.round(secs) }).catch(() => undefined);
        }, 1000);
        return () => window.clearInterval(t);
    }, [buffering, paused, reconnecting, attempt]);
    const onPlaying = () => {
        setBuffering(false);
        setReconnecting(false);
        setError('');
        failures.current = 0;
    };
    // How far behind live we are, for the badge and the "Go live" button.
    const liveEdge = () => {
        const video = videoRef.current;
        if (!video)
            return null;
        const sync = hlsRef.current?.liveSyncPosition;
        if (sync)
            return sync;
        return video.seekable.length ? video.seekable.end(video.seekable.length - 1) : null;
    };
    const onTimeUpdate = () => {
        const video = videoRef.current;
        const edge = liveEdge();
        if (!video || edge == null)
            return;
        const b = Math.max(0, edge - video.currentTime);
        setBehind(b < 4 ? 0 : Math.round(b));
    };
    const togglePause = useCallback(() => {
        const video = videoRef.current;
        if (!video)
            return;
        if (video.paused)
            video.play().catch(() => undefined);
        else
            video.pause();
    }, []);
    const seekBy = useCallback((secs) => {
        const video = videoRef.current;
        if (!video || !video.seekable.length)
            return;
        const start = video.seekable.start(0);
        const end = liveEdge() ?? video.seekable.end(video.seekable.length - 1);
        video.currentTime = Math.min(end, Math.max(start + 1, video.currentTime + secs));
    }, []);
    const goLive = useCallback(() => {
        const video = videoRef.current;
        const edge = liveEdge();
        if (video && edge != null)
            video.currentTime = edge;
        video?.play().catch(() => undefined);
    }, []);
    useEffect(() => {
        const t = window.setInterval(() => tick((n) => n + 1), 15000); // progress bar
        return () => window.clearInterval(t);
    }, []);
    useEffect(() => localStorage.setItem('bb.miniGuide', guide ? '1' : '0'), [guide]);
    useEffect(() => {
        if (videoRef.current)
            videoRef.current.muted = muted;
    }, [muted]);
    const step = useCallback((dir) => {
        if (!channels?.length)
            return;
        const i = channels.findIndex((c) => String(c.id) === id);
        const next = channels[(i + dir + channels.length) % channels.length];
        nav(`/live/${next.id}`, { replace: true });
    }, [channels, id, nav]);
    const poke = useCallback(() => {
        setControls(true);
        window.clearTimeout(hideTimer.current);
        hideTimer.current = window.setTimeout(() => setControls(false), 4000);
    }, []);
    useEffect(() => {
        const onKey = (e) => {
            const map = {
                ArrowUp: () => step(-1),
                PageUp: () => step(-1),
                ArrowDown: () => step(1),
                PageDown: () => step(1),
                g: () => setGuide((g) => !g),
                m: () => setMuted((m) => !m),
                ' ': togglePause,
                k: togglePause,
                ArrowLeft: () => seekBy(-10),
                ArrowRight: () => seekBy(30),
                l: goLive,
                f: () => (document.fullscreenElement ? document.exitFullscreen() : containerRef.current?.requestFullscreen()),
                Escape: () => !document.fullscreenElement && nav('/live'),
            };
            if (map[e.key]) {
                e.preventDefault();
                map[e.key]();
                poke();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [step, poke, nav, togglePause, seekBy, goLive]);
    const now = Date.now() / 1000;
    const prog = channel?.now;
    const pct = prog ? Math.min(100, ((now - prog.start) / (prog.stop - prog.start)) * 100) : 0;
    const toggleFav = async () => {
        if (!channel)
            return;
        await api[channel.favorite ? 'del' : 'post'](`/api/live/channels/${channel.id}/favorite`);
        reload();
    };
    return (_jsxs("div", { ref: containerRef, className: `live-player ${guide ? 'with-guide' : ''} ${controls ? 'show-controls' : ''}`, onMouseMove: poke, children: [_jsxs("div", { className: "live-stage", children: [_jsx("video", { ref: videoRef, className: "player-video", playsInline: true, autoPlay: true, onPlaying: onPlaying, onWaiting: () => setBuffering(true), onPause: () => setPaused(true), onPlay: () => setPaused(false), onTimeUpdate: onTimeUpdate, onClick: () => setGuide((g) => !g) }), (buffering || reconnecting) && !error && !paused && _jsx("div", { className: "spinner" }), error && (_jsxs("div", { className: "player-error", children: [_jsx("div", { children: error }), _jsx("div", { className: "muted small", children: "Retrying on its own. You can also pick another channel." }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setError(''), children: "Dismiss" })] })), _jsxs("div", { className: "player-top", children: [_jsx("button", { className: "icon-btn", onClick: () => nav('/live'), "aria-label": "Back to the guide", children: _jsx(Icon, { name: "back", size: 28 }) }), channel && (_jsxs("div", { className: "live-info", children: [_jsx(ChannelLogo, { channel: channel, size: 36 }), _jsxs("div", { children: [_jsxs("div", { className: "player-title", children: [_jsx("span", { className: "ch-num", children: channelNo(channel.number) }), " ", channel.name] }), _jsxs("div", { className: "player-sub", children: [prog ? `${prog.title}${prog.subtitle ? ` — ${prog.subtitle}` : ''} · ${clock(prog.start)}–${clock(prog.stop)}` : 'Live', channel.next && ` · Next: ${channel.next.title} at ${clock(channel.next.start)}`] }), prog && (_jsx("div", { className: "progress inline live-progress", children: _jsx("div", { style: { width: `${pct}%` } }) }))] })] })), _jsx("div", { className: "spacer" }), _jsx("button", { className: `live-badge ${behind ? 'behind' : ''}`, onClick: goLive, title: behind ? 'Jump to live (L)' : 'Watching live', children: behind ? `−${fmtBehind(behind)} · GO LIVE` : 'LIVE' })] }), _jsxs("div", { className: "live-controls", children: [_jsx("button", { className: "icon-btn", onClick: () => step(-1), "aria-label": "Previous channel", children: _jsx(Icon, { name: "arrowUp", size: 22 }) }), _jsx("button", { className: "icon-btn", onClick: () => step(1), "aria-label": "Next channel", children: _jsx(Icon, { name: "arrowDown", size: 22 }) }), _jsx("button", { className: "icon-btn", onClick: () => seekBy(-10), "aria-label": "Back 10 seconds", children: _jsx(Icon, { name: "replay10", size: 22 }) }), _jsx("button", { className: "icon-btn", onClick: togglePause, "aria-label": paused ? 'Play' : 'Pause', children: _jsx(Icon, { name: paused ? 'play' : 'pause', size: 22 }) }), _jsx("button", { className: "icon-btn", onClick: () => seekBy(30), "aria-label": "Forward 30 seconds", children: _jsx(Icon, { name: "forward30", size: 22 }) }), _jsx("button", { className: "icon-btn", onClick: () => setMuted((m) => !m), "aria-label": muted ? 'Unmute' : 'Mute', children: _jsx(Icon, { name: muted ? 'mute' : 'volume', size: 22 }) }), channel && (_jsx("button", { className: "icon-btn", onClick: toggleFav, "aria-label": channel.favorite ? 'Remove favorite' : 'Add favorite', children: _jsx(Icon, { name: "star", fill: channel.favorite, size: 20, className: channel.favorite ? 'accent' : '' }) })), _jsx("div", { className: "spacer" }), _jsxs("button", { className: `btn btn-glass btn-sm ${guide ? 'active' : ''}`, onClick: () => setGuide((g) => !g), children: [_jsx(Icon, { name: "list", size: 16 }), " Guide"] }), _jsx("button", { className: "icon-btn", onClick: () => (document.fullscreenElement ? document.exitFullscreen() : containerRef.current?.requestFullscreen()), "aria-label": "Fullscreen", children: _jsx(Icon, { name: "fullscreen", size: 22 }) })] })] }), guide && (_jsx("div", { className: "mini-guide", children: channels?.map((c) => {
                    const p = c.now;
                    const cpct = p ? Math.min(100, ((now - p.start) / (p.stop - p.start)) * 100) : 0;
                    return (_jsxs("button", { className: `mini-ch ${String(c.id) === id ? 'current' : ''}`, onClick: () => nav(`/live/${c.id}`, { replace: true }), children: [_jsxs("div", { className: "mini-ch-head", children: [_jsx("span", { className: "ch-num", children: channelNo(c.number) }), _jsx(ChannelLogo, { channel: c, size: 24 }), _jsx("span", { className: "ch-name", children: c.name })] }), _jsx("div", { className: "mini-ch-now", children: p?.title ?? 'No guide information' }), p && (_jsx("div", { className: "progress inline", children: _jsx("div", { style: { width: `${cpct}%` } }) })), c.next && _jsxs("div", { className: "muted small", children: ["Next: ", c.next.title] })] }, c.id));
                }) }))] }));
}
function fmtBehind(secs) {
    const m = Math.floor(secs / 60);
    const s = secs % 60;
    return `${m}:${String(s).padStart(2, '0')}`;
}
