import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
let ytReady;
/** Loads YouTube's iframe API once; false when it can't be reached. */
function loadYouTube() {
    if (window.YT?.Player)
        return Promise.resolve(true);
    ytReady ??= new Promise((resolve) => {
        const timer = setTimeout(() => resolve(false), 8000);
        const prev = window.onYouTubeIframeAPIReady;
        window.onYouTubeIframeAPIReady = () => {
            prev?.();
            clearTimeout(timer);
            resolve(true);
        };
        const s = document.createElement('script');
        s.src = 'https://www.youtube.com/iframe_api';
        s.onerror = () => {
            clearTimeout(timer);
            ytReady = undefined;
            resolve(false);
        };
        document.head.appendChild(s);
    });
    return ytReady;
}
/**
 * Plays trailers of other movies and then the server's intro video before a
 * movie. Anything that fails to load is skipped; Skip and Esc move on.
 */
export function PreRoll({ data, movieTitle, onDone }) {
    const steps = [
        ...data.trailers.map((t) => ({ kind: 'trailer', title: t.year ? `${t.title} (${t.year})` : t.title, youtube: t.youtube })),
        ...(data.intro ? [{ kind: 'intro', url: data.intro.url }] : []),
    ];
    const [i, setI] = useState(0);
    const step = steps[i];
    const next = () => setI((n) => n + 1);
    const ytHost = useRef(null);
    useEffect(() => {
        if (!step)
            onDone();
    }, [step, onDone]);
    useEffect(() => {
        const onKey = (e) => {
            if (e.key === 'Escape')
                onDone();
            else if (e.key === 'Enter' || e.key === 'ArrowRight')
                setI((n) => n + 1);
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onDone]);
    useEffect(() => {
        if (step?.kind !== 'trailer')
            return;
        let player;
        let gone = false;
        loadYouTube().then((ok) => {
            if (gone)
                return;
            if (!ok || !ytHost.current)
                return next();
            const el = document.createElement('div');
            ytHost.current.replaceChildren(el);
            player = new window.YT.Player(el, {
                videoId: step.youtube,
                width: '100%',
                height: '100%',
                host: 'https://www.youtube-nocookie.com',
                playerVars: { autoplay: 1, rel: 0, controls: 0, modestbranding: 1, playsinline: 1, iv_load_policy: 3 },
                events: {
                    onStateChange: (e) => e.data === 0 && next(), // ended
                    onError: () => next(),
                },
            });
        });
        return () => {
            gone = true;
            player?.destroy();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [i]);
    if (!step)
        return _jsx("div", { className: "player", children: _jsx("div", { className: "spinner" }) });
    return (_jsxs("div", { className: "player preroll show-controls", children: [step.kind === 'trailer' ? (_jsx("div", { className: "preroll-yt", ref: ytHost })) : (_jsx("video", { className: "player-video", src: step.url, playsInline: true, onEnded: next, onError: next, ref: (v) => {
                    // Browsers can block autoplay with sound; play muted rather than sit still.
                    if (v && !v.dataset.started) {
                        v.dataset.started = '1';
                        v.play().catch(() => {
                            v.muted = true;
                            v.play().catch(next);
                        });
                    }
                } }, step.url)), _jsxs("div", { className: "preroll-bar", children: [_jsxs("div", { className: "muted small", children: [step.kind === 'trailer' ? _jsxs(_Fragment, { children: ["Trailer \u00B7 ", step.title] }) : _jsxs(_Fragment, { children: ["Up next: ", movieTitle] }), steps.length > 1 && _jsxs("span", { children: [" \u00B7 ", i + 1, " of ", steps.length] })] }), _jsxs("div", { className: "preroll-actions", children: [i < steps.length - 1 && (_jsx("button", { className: "btn btn-glass btn-sm", onClick: next, children: "Skip" })), _jsx("button", { className: "btn btn-primary btn-sm", onClick: onDone, children: i < steps.length - 1 ? 'Skip all' : 'Start movie' })] })] })] }));
}
