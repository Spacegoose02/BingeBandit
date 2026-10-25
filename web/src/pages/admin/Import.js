import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api } from '../../api';
import { useApi, useDocumentTitle } from '../../hooks';
const sourceNames = { plex: 'Plex', tautulli: 'Tautulli', 'tautulli-db': 'Tautulli database file', cinemask: 'Cinemask collections' };
export default function AdminImport() {
    useDocumentTitle('Import');
    const [source, setSource] = useState('plex');
    const [url, setUrl] = useState('');
    const [token, setToken] = useState('');
    const [path, setPath] = useState('');
    const [connected, setConnected] = useState(null);
    const [map, setMap] = useState({});
    const [opts, setOpts] = useState({ history: true, watched: true, collections: true, artwork: true, labels: true });
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const [status, setStatus] = useState(null);
    const { data: users, reload: reloadUsers } = useApi('/api/admin/users');
    const body = () => ({ source, url, token, path });
    // Show the last import, and follow one that's running.
    useEffect(() => {
        let timer = 0;
        let stopped = false;
        const poll = async () => {
            try {
                const s = await api.get('/api/admin/import/status');
                if (stopped)
                    return;
                setStatus(s.startedAt ? s : null);
                if (s.running)
                    timer = window.setTimeout(poll, 1000);
            }
            catch {
                /* ignore */
            }
        };
        poll();
        return () => {
            stopped = true;
            clearTimeout(timer);
        };
    }, [status?.running]);
    const connect = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        setConnected(null);
        try {
            const res = await api.post('/api/admin/import/connect', body());
            setConnected(res);
            setMap(Object.fromEntries(res.users.map((u) => [u.name, u.suggest])));
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const start = async (dryRun) => {
        setError('');
        try {
            const s = await api.post('/api/admin/import/start', {
                ...body(),
                history: opts.history && !collectionsOnly,
                watched: opts.watched && !collectionsOnly,
                collections: opts.collections && !!connected?.collections,
                artwork: opts.artwork && !!connected?.artwork,
                labels: opts.labels && !!connected?.labels,
                dryRun,
                users: map,
            });
            setStatus(s);
            if (!dryRun && Object.values(map).includes(-1)) {
                // The new users exist now; point the mapping at them so running it again doesn't make more.
                const res = await api.post('/api/admin/import/connect', body());
                setMap((m) => Object.fromEntries(Object.entries(m).map(([k, v]) => [k, v === -1 ? res.users.find((u) => u.name === k)?.suggest || -1 : v])));
                reloadUsers();
            }
        }
        catch (err) {
            setError(err.message);
        }
    };
    const switchSource = (s) => {
        setSource(s);
        setConnected(null);
        setError('');
    };
    const mapped = Object.values(map).filter((v) => v !== 0).length;
    // Cinemask only has collections: no users to map, no history.
    const collectionsOnly = source === 'cinemask';
    const running = !!status?.running;
    return (_jsxs("div", { className: "page narrow", children: [_jsx("header", { className: "page-head", children: _jsxs("div", { children: [_jsx("h1", { children: "Import" }), _jsx("div", { className: "muted", children: "Bring watch history, watched marks, collections and posters over from Plex or Tautulli, or collections from Cinemask." })] }) }), status && _jsx(ImportResult, { status: status }), _jsxs("form", { className: "panel form", onSubmit: connect, style: { marginBottom: 20 }, children: [_jsx("h3", { children: "1. Where from" }), _jsx("div", { className: "segmented", role: "tablist", children: Object.keys(sourceNames).map((s) => (_jsx("button", { type: "button", className: source === s ? 'active' : '', onClick: () => switchSource(s), children: sourceNames[s] }, s))) }), source === 'tautulli-db' ? (_jsxs(_Fragment, { children: [_jsxs("label", { children: ["Path to tautulli.db on this server", _jsx("input", { className: "mono", value: path, onChange: (e) => setPath(e.target.value), required: true, placeholder: "/config/tautulli.db", spellCheck: false })] }), _jsxs("p", { className: "muted small", children: ["Works after Plex and Tautulli are switched off. Copy ", _jsx("code", { children: "tautulli.db" }), " from Tautulli's data folder somewhere BingeBandit can read it. It isn't changed."] })] })) : (_jsxs(_Fragment, { children: [_jsxs("label", { children: [{ plex: 'Plex server address', cinemask: 'Cinemask address' }[source] ?? 'Tautulli address', _jsx("input", { className: "mono", value: url, onChange: (e) => setUrl(e.target.value), required: true, placeholder: { plex: 'http://192.168.1.10:32400', cinemask: 'http://192.168.1.10:3000' }[source] ?? 'http://192.168.1.10:8181', spellCheck: false })] }), _jsxs("label", { children: [{ plex: 'Plex token', cinemask: 'Cinemask API key' }[source] ?? 'Tautulli API key', _jsx("input", { className: "mono", type: "password", value: token, onChange: (e) => setToken(e.target.value), required: true, autoComplete: "off" })] }), _jsx("p", { className: "muted small", children: source === 'plex' ? (_jsxs(_Fragment, { children: ["Sign in to Plex Web as the server owner, open any movie, choose \u22EF \u2192 Get Info \u2192 View XML, and copy the", ' ', _jsx("code", { children: "X-Plex-Token=" }), " value from the address bar. It's only used for this import and not saved."] })) : source === 'cinemask' ? (_jsx(_Fragment, { children: "In Cinemask: your profile \u2192 API keys \u2192 New key. Collections are matched by TMDB id, so every title in your libraries is found. The key is only used for this import and not saved." })) : (_jsx(_Fragment, { children: "In Tautulli: Settings \u2192 Web Interface \u2192 API. Turn the API on and copy the key. It's only used for this import and not saved." })) })] })), error && !connected && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "actions", children: _jsx("button", { className: "btn btn-primary", disabled: busy || running, children: busy ? 'Connecting…' : 'Connect' }) })] }), connected && (_jsxs("div", { className: "panel form", children: [!collectionsOnly && (_jsxs(_Fragment, { children: [_jsx("h3", { children: "2. Whose history goes where" }), connected.users.length === 0 ? (_jsx("p", { className: "muted", children: "No users with history were found." })) : (_jsx("div", { className: "list", children: connected.users.map((u) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { children: u.name }), u.plays !== undefined && _jsxs("div", { className: "muted small", children: [u.plays, " plays"] })] }), _jsxs("select", { value: map[u.name] ?? 0, onChange: (e) => setMap({ ...map, [u.name]: Number(e.target.value) }), "aria-label": `BingeBandit user for ${u.name}`, style: { width: 'auto', minWidth: 220 }, children: [_jsx("option", { value: 0, children: "Skip" }), _jsx("option", { value: -1, children: "Create a new user" }), users?.map((b) => (_jsx("option", { value: b.id, children: b.username }, b.id)))] })] }, u.name))) })), _jsx("p", { className: "muted small", children: "New users get no password: link a TV for them under Link a device, or set a password under Users." })] })), _jsxs("h3", { children: [collectionsOnly ? '2.' : '3.', " What to bring over"] }), !collectionsOnly && (_jsxs(_Fragment, { children: [_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: opts.history, onChange: (e) => setOpts({ ...opts, history: e.target.checked }) }), "Watch history (shows in profiles, statistics and history)"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: opts.watched, onChange: (e) => setOpts({ ...opts, watched: e.target.checked }) }), "Watched marks", connected.watchedState && ' and resume points (Plex reports these for the server owner only)'] })] })), connected.collections && (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: opts.collections, onChange: (e) => setOpts({ ...opts, collections: e.target.checked }) }), "Collections (added to the matching library; existing collections with the same name get the new items)"] })), connected.labels && (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: opts.labels, onChange: (e) => setOpts({ ...opts, labels: e.target.checked }) }), "Labels (the ones Kometa and Cinemask put on titles; a collection with a label rule gathers everything carrying it)"] })), connected.artwork && (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: opts.artwork, onChange: (e) => setOpts({ ...opts, artwork: e.target.checked }) }), collectionsOnly ? ('Collection posters (replaces the poster here)') : (_jsxs(_Fragment, { children: ["Posters and backgrounds for movies, shows, seasons", connected.collections && ' and collections', " (replaces the artwork here and keeps it through metadata refreshes, so posters you picked in Plex or set with Kometa come along)"] }))] })), _jsx("p", { className: "muted small", children: "Items are matched by file name, then TMDB id, then title and year. Anything already imported, or a viewing BingeBandit already has, is skipped, so running it again is safe." }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsx("button", { type: "button", className: "btn btn-glass", disabled: running, onClick: () => start(true), children: "Preview" }), _jsx("button", { type: "button", className: "btn btn-primary", disabled: running || (mapped === 0 && !(opts.collections && connected.collections) && !(opts.artwork && connected.artwork) && !(opts.labels && connected.labels)), onClick: () => start(false), children: "Import" })] })] }))] }));
}
function ImportResult({ status: s }) {
    const pct = s.total > 0 ? Math.min(100, Math.round((s.done / s.total) * 100)) : 0;
    const cancel = () => api.post('/api/admin/import/cancel').catch(() => undefined);
    const title = s.running ? (s.dryRun ? 'Previewing…' : 'Importing…') : s.dryRun ? 'Preview' : 'Last import';
    const tiles = [
        ['Read', s.seen, ''],
        ['Matched', s.matched, s.seen ? `${Math.round((s.matched / s.seen) * 100)}%` : ''],
        [s.dryRun ? 'Plays to add' : 'Plays added', s.plays, s.duplicates ? `${s.duplicates} already here` : ''],
        [s.dryRun ? 'To mark watched' : 'Marked watched', s.watched, ''],
        ['Resume points', s.resumes, ''],
        ['Collections', s.collections, s.collectionItems ? `${s.collectionItems} items` : ''],
        ['Labels', s.labels ?? 0, ''],
    ];
    if (s.artwork || s.artworkFailed)
        tiles.push([s.dryRun ? 'Images to bring over' : 'Images brought over', s.artwork, s.artworkFailed ? `${s.artworkFailed} failed` : '']);
    return (_jsxs("section", { className: "panel", style: { marginBottom: 20 }, "aria-live": "polite", children: [_jsxs("header", { className: "panel-head", children: [_jsxs("h3", { children: [title, " ", _jsxs("span", { className: "muted small", children: ["from ", sourceNames[s.source] ?? s.source] })] }), s.running && (_jsx("button", { className: "btn btn-glass btn-sm", onClick: cancel, children: "Cancel" }))] }), s.running ? (_jsxs("div", { className: "import-progress", children: [_jsxs("div", { className: "muted small", children: [s.phase, s.total > 0 && ` · ${s.done} of ${s.total}`] }), _jsx("div", { className: "progress-track", role: "progressbar", "aria-valuenow": pct, "aria-valuemin": 0, "aria-valuemax": 100, children: _jsx("div", { style: { width: s.total > 0 ? `${pct}%` : '30%' }, className: s.total > 0 ? '' : 'indeterminate' }) })] })) : (_jsxs("div", { className: "muted small", style: { marginBottom: 12 }, children: [s.error ? _jsxs("span", { className: "form-error", children: ["Stopped: ", s.error] }) : `Finished ${new Date(s.finishedAt * 1000).toLocaleString()}`, s.dryRun && !s.error && '. Nothing was changed; this is what an import would do.'] })), _jsx("div", { className: "stat-grid", children: tiles.map(([label, value, sub]) => (_jsxs("div", { className: "stat", children: [_jsx("div", { className: "stat-value", children: value.toLocaleString() }), _jsxs("div", { className: "stat-label", children: [label, sub && _jsxs(_Fragment, { children: [" \u00B7 ", sub] })] })] }, label))) }), s.skippedUser > 0 && _jsxs("p", { className: "muted small", children: [s.skippedUser, " records belonged to users you chose to skip."] }), s.unmatchedCount > 0 && (_jsxs("details", { className: "unmatched", children: [_jsxs("summary", { children: [s.unmatchedCount, " title", s.unmatchedCount === 1 ? '' : 's', " not found in your libraries"] }), _jsx("p", { className: "muted small", children: "Most-watched first. Add them to a library (or fix their match) and run the import again to pick them up." }), _jsxs("ul", { children: [s.unmatched.map((t) => (_jsx("li", { children: t }, t))), s.unmatchedCount > s.unmatched.length && _jsxs("li", { className: "muted", children: ["\u2026and ", s.unmatchedCount - s.unmatched.length, " more"] })] })] }))] }));
}
