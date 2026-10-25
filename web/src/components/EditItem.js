import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { api, ApiError, img } from '../api';
import { useApi } from '../hooks';
import { formatClock } from '../format';
import { ChipsInput } from './ChipsInput';
import { Icon } from './Icon';
import { Modal } from './Modal';
/** Admin editor for an item: text fields with locks, tags, artwork. */
export function EditItem({ item, collectionNames, markers = [], onClose, onSaved, onChanged, }) {
    const [tab, setTab] = useState('details');
    const [form, setForm] = useState({
        title: item.title,
        sortTitle: item.sortTitle,
        originalTitle: item.originalTitle ?? '',
        edition: item.edition ?? '',
        releaseDate: item.releaseDate ?? '',
        contentRating: item.contentRating ?? '',
        rating: item.rating ?? 0,
        studio: item.studio ?? '',
        tagline: item.tagline ?? '',
        overview: item.overview ?? '',
        genres: item.genres ?? [],
        labels: item.labels ?? [],
        collections: collectionNames,
        episodeOrder: item.episodeOrder ?? '',
    });
    const [locked, setLocked] = useState(new Set(item.locked ?? []));
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const topLevel = item.kind === 'movie' || item.kind === 'show';
    const { data: labelSuggestions } = useApi(`/api/admin/libraries/${item.libraryId}/labels`);
    const { data: libCollections } = useApi(topLevel ? `/api/libraries/${item.libraryId}/collections` : null);
    // Editing a field locks it, like Plex, so the next refresh keeps the edit.
    const set = (key, lockAs = key) => (v) => {
        setForm((f) => ({ ...f, [key]: v }));
        if (lockAs)
            setLocked((l) => new Set(l).add(lockAs));
    };
    const toggleLock = (field) => setLocked((l) => {
        const n = new Set(l);
        if (n.has(field))
            n.delete(field);
        else
            n.add(field);
        return n;
    });
    const save = async (e) => {
        e?.preventDefault();
        setSaving(true);
        setError('');
        try {
            await api.put(`/api/admin/items/${item.id}`, {
                ...form,
                rating: Number(form.rating) || 0,
                locked: [...locked],
                collections: topLevel ? form.collections : undefined,
            });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setSaving(false);
        }
    };
    const field = (label, lockName, input) => (_jsxs("div", { className: "field", children: [_jsxs("div", { className: "field-head", children: [_jsx("span", { children: label }), _jsx("button", { type: "button", className: `lock-btn ${locked.has(lockName) ? 'on' : ''}`, onClick: () => toggleLock(lockName), title: locked.has(lockName) ? 'Locked: metadata refreshes keep this value' : 'Unlocked: metadata refreshes may change this', children: _jsx(Icon, { name: locked.has(lockName) ? 'lock' : 'unlock', size: 14 }) })] }), input] }));
    const tabs = [
        ['details', 'Details'],
        ['tags', 'Tags'],
        ['artwork', 'Artwork'],
    ];
    if (item.kind !== 'season')
        tabs.push(['advanced', 'Advanced']);
    return (_jsxs(Modal, { title: `Edit ${item.title}`, onClose: onClose, wide: true, children: [_jsx("div", { className: "tabs", children: tabs.map(([t, label]) => (_jsx("button", { type: "button", className: tab === t ? 'active' : '', onClick: () => setTab(t), children: label }, t))) }), tab === 'artwork' ? (_jsx(ArtworkTab, { item: item, onChanged: onChanged })) : (_jsxs("form", { className: "form", onSubmit: save, children: [tab === 'details' && (_jsxs("div", { className: "field-grid", children: [field('Title', 'title', _jsx("input", { value: form.title, onChange: (e) => set('title')(e.target.value), required: true })), field('Sort title', 'sortTitle', _jsx("input", { value: form.sortTitle, onChange: (e) => set('sortTitle')(e.target.value), placeholder: "Defaults to the title without \u201CThe/A/An\u201D" })), field('Original title', 'originalTitle', _jsx("input", { value: form.originalTitle, onChange: (e) => set('originalTitle')(e.target.value) })), item.kind === 'movie' && (_jsxs("label", { className: "field", children: [_jsx("div", { className: "field-head", children: _jsx("span", { children: "Edition" }) }), _jsx("input", { value: form.edition, onChange: (e) => set('edition', null)(e.target.value), placeholder: "Director's Cut, Extended\u2026" })] })), field(item.kind === 'show' ? 'First aired' : 'Originally available', 'releaseDate', (_jsx("input", { type: "date", value: form.releaseDate, onChange: (e) => set('releaseDate')(e.target.value) }))), field('Content rating', 'contentRating', _jsx("input", { value: form.contentRating, onChange: (e) => set('contentRating')(e.target.value), placeholder: "PG-13, TV-MA\u2026" })), field('Rating (0–10)', 'rating', (_jsx("input", { type: "number", min: 0, max: 10, step: 0.1, value: form.rating, onChange: (e) => set('rating')(Number(e.target.value)) }))), field(item.kind === 'show' ? 'Network' : 'Studio', 'studio', _jsx("input", { value: form.studio, onChange: (e) => set('studio')(e.target.value) })), _jsx("div", { className: "span-2", children: field('Tagline', 'tagline', _jsx("input", { value: form.tagline, onChange: (e) => set('tagline')(e.target.value) })) }), _jsx("div", { className: "span-2", children: field('Summary', 'overview', _jsx("textarea", { rows: 5, value: form.overview, onChange: (e) => set('overview')(e.target.value) })) })] })), tab === 'tags' && (_jsxs(_Fragment, { children: [field('Genres', 'genres', _jsx(ChipsInput, { value: form.genres, onChange: set('genres'), placeholder: "Add a genre" })), _jsxs("div", { className: "field", children: [_jsx("div", { className: "field-head", children: _jsx("span", { children: "Labels" }) }), _jsx(ChipsInput, { value: form.labels, onChange: set('labels', null), suggestions: labelSuggestions ?? [], placeholder: "e.g. Disney, Christmas, Kids" }), _jsx("p", { className: "muted small", children: "Smart collections pick up every item with their label." })] }), topLevel &&
                                field('Collections', 'collections', (_jsx(ChipsInput, { value: form.collections, onChange: set('collections'), suggestions: libCollections?.map((c) => c.name) ?? [], placeholder: "Type a collection name" })))] })), tab === 'advanced' && item.kind === 'show' && (_jsxs("label", { children: ["Episode order", _jsxs("select", { value: form.episodeOrder, onChange: (e) => set('episodeOrder', null)(e.target.value), children: [_jsx("option", { value: "", children: "Oldest first (aired order)" }), _jsx("option", { value: "desc", children: "Newest first" })] })] })), tab === 'advanced' && (item.kind === 'movie' || item.kind === 'episode') && _jsx(MarkerEditor, { item: item, markers: markers, onSaved: onChanged }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsxs("span", { className: "muted small grow", children: [_jsx(Icon, { name: "lock", size: 12 }), " Locked fields keep your edits when metadata is refreshed."] }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: saving, children: saving ? 'Saving…' : 'Save' })] })] }))] }));
}
/** "1:23" or "1:02:03" → seconds; "" → null. */
function parseClock(v) {
    if (!v.trim())
        return null;
    const parts = v.trim().split(':').map(Number);
    if (parts.some((n) => !Number.isFinite(n) || n < 0) || parts.length > 3)
        return undefined;
    return parts.reduce((acc, n) => acc * 60 + n, 0);
}
/** Intro and credits times for one movie or episode, entered as m:ss. */
function MarkerEditor({ item, markers, onSaved }) {
    const get = (kind) => markers.find((m) => m.kind === kind);
    const fmt = (n) => (n === undefined ? '' : formatClock(n));
    const [v, setV] = useState({
        introStart: fmt(get('intro')?.start),
        introEnd: fmt(get('intro')?.end),
        creditsStart: fmt(get('credits')?.start),
        creditsEnd: fmt(get('credits')?.end),
    });
    const [msg, setMsg] = useState(null);
    const save = async () => {
        const out = [];
        for (const [kind, s, e] of [
            ['intro', v.introStart, v.introEnd],
            ['credits', v.creditsStart, v.creditsEnd],
        ]) {
            const start = parseClock(s);
            const end = parseClock(e);
            if (start === undefined || end === undefined)
                return setMsg({ ok: false, text: 'Use times like 1:05 or 1:02:30' });
            if (start === null && end === null)
                continue;
            if (start === null || end === null)
                return setMsg({ ok: false, text: `Fill in both ${kind} times, or neither` });
            out.push({ kind, start, end });
        }
        try {
            await api.put(`/api/admin/items/${item.id}/markers`, { markers: out });
            setMsg({ ok: true, text: 'Saved. Detection will not change these.' });
            onSaved();
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
    };
    const source = (kind) => {
        const m = get(kind);
        return m ? (m.source === 'manual' ? 'set by hand' : 'detected') : 'not found';
    };
    return (_jsxs("div", { className: "form", children: [_jsx("div", { className: "label", children: "Skip markers" }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Intro starts ", _jsxs("span", { className: "muted small", children: ["(", source('intro'), ")"] }), _jsx("input", { value: v.introStart, onChange: (e) => setV({ ...v, introStart: e.target.value }), placeholder: "0:45" })] }), _jsxs("label", { children: ["Intro ends", _jsx("input", { value: v.introEnd, onChange: (e) => setV({ ...v, introEnd: e.target.value }), placeholder: "1:30" })] }), _jsxs("label", { children: ["Credits start ", _jsxs("span", { className: "muted small", children: ["(", source('credits'), ")"] }), _jsx("input", { value: v.creditsStart, onChange: (e) => setV({ ...v, creditsStart: e.target.value }), placeholder: "42:10" })] }), _jsxs("label", { children: ["Credits end", _jsx("input", { value: v.creditsEnd, onChange: (e) => setV({ ...v, creditsEnd: e.target.value }), placeholder: "43:20" })] })] }), _jsx("p", { className: "muted small", children: "Leave both times of a section empty to let automatic detection handle it." }), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsx("div", { className: "actions", children: _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: save, children: "Save markers" }) })] }));
}
function ArtworkTab({ item, onChanged }) {
    const kinds = item.kind === 'episode'
        ? [['backdrop', 'Thumbnail']]
        : item.kind === 'season'
            ? [['poster', 'Poster']]
            : [['poster', 'Poster'], ['backdrop', 'Background'], ['logo', 'Logo']];
    const [kind, setKind] = useState(kinds[0][0]);
    const [current, setCurrent] = useState({ poster: item.poster, backdrop: item.backdrop, logo: item.logo });
    const { data, loading } = useApi(`/api/admin/items/${item.id}/artwork?type=${kind}`);
    const [url, setUrl] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const fileRef = useRef(null);
    useEffect(() => setError(''), [kind]);
    const apply = async (fn) => {
        setBusy(true);
        setError('');
        try {
            const res = await fn();
            setCurrent((c) => ({ ...c, [kind]: res.url }));
            onChanged();
        }
        catch (e) {
            setError(e.message);
        }
        finally {
            setBusy(false);
        }
    };
    const upload = (file) => apply(async () => {
        const body = new FormData();
        body.append('file', file);
        const res = await fetch(`/api/admin/items/${item.id}/artwork/upload?type=${kind}`, {
            method: 'POST',
            body,
            credentials: 'same-origin',
            headers: { 'X-Requested-With': 'BingeBandit' },
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok)
            throw new ApiError(res.status, data.error ?? res.statusText);
        return data;
    });
    const shape = kind === 'poster' ? 'poster' : kind === 'logo' ? 'logo' : 'wide';
    return (_jsxs("div", { className: "artwork", children: [kinds.length > 1 && (_jsx("div", { className: "segmented", children: kinds.map(([k, label]) => (_jsx("button", { className: k === kind ? 'active' : '', onClick: () => setKind(k), children: label }, k))) })), _jsxs("div", { className: "artwork-current", children: [_jsx("div", { className: `art-thumb ${shape} current`, children: current[kind] ? _jsx("img", { src: img(current[kind], 'w500'), alt: "" }) : _jsx("span", { className: "muted small", children: "None" }) }), _jsxs("div", { className: "artwork-sources", children: [_jsxs("div", { className: "url-row", children: [_jsx("input", { value: url, onChange: (e) => setUrl(e.target.value), placeholder: "Paste an image URL (ThePosterDB, Fanart.tv\u2026)" }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: !url || busy, onClick: () => apply(() => api.put(`/api/admin/items/${item.id}/artwork`, { type: kind, url })).then(() => setUrl('')), children: [_jsx(Icon, { name: "link", size: 16 }), " Use URL"] })] }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: busy, onClick: () => fileRef.current?.click(), children: [_jsx(Icon, { name: "upload", size: 16 }), " Upload image"] }), _jsx("input", { ref: fileRef, type: "file", accept: "image/jpeg,image/png,image/webp", hidden: true, onChange: (e) => e.target.files?.[0] && upload(e.target.files[0]) }), error && _jsx("div", { className: "form-error small", children: error }), _jsx("p", { className: "muted small", children: "Choosing artwork locks it, so metadata refreshes won't replace it." })] })] }), kind === 'poster' && _jsx(TPDbSearch, { item: item, busy: busy, onPick: (url) => apply(() => api.put(`/api/admin/items/${item.id}/artwork`, { type: kind, url })) }), _jsx("div", { className: "label", children: "From TMDB" }), loading && _jsx("div", { className: "muted small", children: "Loading\u2026" }), data?.notice && _jsx("div", { className: "muted small", children: data.notice }), data && !data.notice && data.options.length === 0 && _jsx("div", { className: "muted small", children: "TMDB has no alternatives for this item." }), _jsx("div", { className: `art-grid ${shape}`, children: data?.options.map((o) => (_jsxs("button", { className: `art-thumb ${shape}`, disabled: busy, onClick: () => apply(() => api.put(`/api/admin/items/${item.id}/artwork`, { type: kind, ref: o.ref })), title: `${o.width}×${o.height}${o.lang ? ` · ${o.lang}` : ''}`, children: [_jsx("img", { src: img(o.url, kind === 'backdrop' ? 'w500' : 'w342'), alt: "", loading: "lazy" }), o.lang && _jsx("span", { className: "art-lang", children: o.lang })] }, o.ref))) })] }));
}
/** Posters from ThePosterDB, searched on demand (it has no API, so the server reads its pages). */
function TPDbSearch({ item, busy, onPick }) {
    const [q, setQ] = useState('');
    const [res, setRes] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const search = async (e) => {
        e?.preventDefault();
        setLoading(true);
        setError('');
        try {
            setRes(await api.get(`/api/admin/items/${item.id}/tpdb${q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ''}`));
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setLoading(false);
        }
    };
    const term = q.trim() || res?.query || item.title;
    const posters = res?.titles.reduce((n, t) => n + t.posters.length, 0) ?? 0;
    return (_jsxs(_Fragment, { children: [_jsx("div", { className: "label", children: "From ThePosterDB" }), _jsxs("form", { className: "url-row", onSubmit: search, children: [_jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: `Search for “${item.title}”, or paste a ThePosterDB link` }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: loading, children: [_jsx(Icon, { name: "search", size: 16 }), " ", loading ? 'Searching…' : 'Search'] }), _jsxs("a", { className: "btn btn-glass btn-sm", href: `https://theposterdb.com/search?term=${encodeURIComponent(term)}`, target: "_blank", rel: "noreferrer", children: [_jsx(Icon, { name: "link", size: 16 }), " Open site"] })] }), error && _jsx("div", { className: "form-error small", children: error }), res && posters === 0 && _jsxs("div", { className: "muted small", children: ["No posters found for \u201C", res.query, "\u201D. Try another title, or paste a link from their site."] }), res?.titles.filter((t) => t.posters.length > 0).map((t) => (_jsxs("div", { className: "tpdb-title", children: [_jsx("a", { className: "small", href: t.url, target: "_blank", rel: "noreferrer", children: t.title || t.url }), _jsx("div", { className: "art-grid poster", children: t.posters.map((p) => (_jsxs("button", { className: "art-thumb poster", disabled: busy, onClick: () => onPick(p.download), title: [p.title, p.uploader && `by ${p.uploader}`].filter(Boolean).join(' · '), children: [_jsx("img", { src: `/api/admin/tpdb/${p.id}/thumb`, alt: "", loading: "lazy" }), p.uploader && _jsx("span", { className: "art-lang", children: p.uploader })] }, p.id))) })] }, `${t.id}:${t.url}`)))] }));
}
