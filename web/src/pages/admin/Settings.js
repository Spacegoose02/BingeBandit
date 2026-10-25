import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../../api';
import { useAuth } from '../../auth';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi, useDocumentTitle } from '../../hooks';
const encoders = [
    ['none', 'Software (CPU, libx264)'],
    ['nvenc', 'NVIDIA NVENC'],
    ['qsv', 'Intel Quick Sync'],
    ['vaapi', 'VAAPI (Linux / Docker / Unraid)'],
    ['amf', 'AMD AMF (Windows)'],
    ['videotoolbox', 'Apple VideoToolbox'],
];
const tonemapAlgorithms = [
    ['hable', 'Hable (default: filmic, keeps highlight detail)'],
    ['mobius', 'Möbius (brighter, closer to the original colours)'],
    ['reinhard', 'Reinhard (simple and soft)'],
    ['clip', 'Clip (accurate midtones, blown-out highlights)'],
    ['linear', 'Linear (darkest, no curve)'],
];
export default function AdminSettings() {
    useDocumentTitle('Settings');
    const { refresh } = useAuth();
    const { data } = useApi('/api/admin/settings');
    const [s, setS] = useState({});
    const [msg, setMsg] = useState(null);
    const [hwTest, setHwTest] = useState(null);
    const testEncoder = async () => {
        const enc = s.hw_encoder ?? 'none';
        setHwTest({ text: 'Testing…' });
        try {
            const r = await api.post(`/api/admin/live/test-encoder?encoder=${encodeURIComponent(enc)}`);
            setHwTest(r.ok ? { ok: true, text: 'Works: a 1080i test picture was converted with this encoder.' } : { ok: false, text: r.error ?? 'Failed.' });
        }
        catch (err) {
            setHwTest({ ok: false, text: err.message });
        }
    };
    useEffect(() => {
        if (data)
            setS(data);
    }, [data]);
    const set = (k) => (e) => {
        const v = e.target instanceof HTMLInputElement && e.target.type === 'checkbox' ? String(e.target.checked) : e.target.value;
        setS({ ...s, [k]: v });
    };
    const save = async (e) => {
        e.preventDefault();
        try {
            setS(await api.put('/api/admin/settings', s));
            setMsg({ ok: true, text: 'Settings saved.' });
            refresh();
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
    };
    if (!data)
        return _jsx("div", { className: "page-loading" });
    return (_jsxs("div", { className: "page narrow", children: [_jsx("h1", { children: "Settings" }), _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "General" }), _jsxs("label", { children: ["Server name", _jsx("input", { value: s.server_name ?? '', onChange: set('server_name') })] })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Metadata" }), _jsxs("label", { children: ["TMDB API key or read access token", _jsx("input", { value: s.tmdb_api_key ?? '', onChange: set('tmdb_api_key'), placeholder: "Paste your key", autoComplete: "off", spellCheck: false })] }), _jsxs("p", { className: "muted small", children: ["Free at", ' ', _jsx("a", { href: "https://www.themoviedb.org/settings/api", target: "_blank", rel: "noreferrer", children: "themoviedb.org/settings/api" }), ". After adding it, use \u201CRefresh metadata\u201D on each library."] }), _jsxs("label", { children: ["Metadata language", _jsx("input", { value: s.metadata_language ?? '', onChange: set('metadata_language'), placeholder: "en-US" })] }), _jsxs("label", { children: ["TheTVDB API key (optional)", _jsx("input", { value: s.tvdb_api_key ?? '', onChange: set('tvdb_api_key'), placeholder: "Paste your key", autoComplete: "off", spellCheck: false })] }), s.tvdb_api_key && (_jsxs("label", { children: ["TheTVDB subscriber PIN", _jsx("input", { value: s.tvdb_pin ?? '', onChange: set('tvdb_pin'), placeholder: "Only for a user-supported key", autoComplete: "off", spellCheck: false })] })), _jsxs("p", { className: "muted small", children: ["From", ' ', _jsx("a", { href: "https://thetvdb.com/api-information", target: "_blank", rel: "noreferrer", children: "thetvdb.com/api-information" }), ". Lets a show use TheTVDB instead of TMDB (Fix match on the show), for aired, DVD or absolute episode numbering."] }), _jsxs("label", { children: ["IMDb and Rotten Tomatoes ratings", _jsxs("select", { value: s.ratings_provider ?? '', onChange: set('ratings_provider'), children: [_jsx("option", { value: "", children: "Off" }), _jsx("option", { value: "mdblist", children: "MDBList (IMDb, Rotten Tomatoes critics and audience, Metacritic)" }), _jsx("option", { value: "omdb", children: "OMDb (IMDb, Rotten Tomatoes critics, Metacritic; needs the TMDB key)" })] })] }), s.ratings_provider && (_jsxs(_Fragment, { children: [_jsxs("label", { children: [s.ratings_provider === 'omdb' ? 'OMDb' : 'MDBList', " API key", _jsx("input", { value: s.ratings_api_key ?? '', onChange: set('ratings_api_key'), placeholder: "Paste your key", autoComplete: "off", spellCheck: false })] }), _jsxs("p", { className: "muted small", children: ["Free at", ' ', s.ratings_provider === 'omdb' ? (_jsx("a", { href: "https://www.omdbapi.com/apikey.aspx", target: "_blank", rel: "noreferrer", children: "omdbapi.com" })) : (_jsx("a", { href: "https://mdblist.com/preferences/", target: "_blank", rel: "noreferrer", children: "mdblist.com/preferences" })), ' ', "(about 1,000 lookups a day). Scores fill in when a title is opened, and the \u201CRefresh IMDb and Rotten Tomatoes ratings\u201D task keeps them current."] })] }))] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Transcoding" }), _jsxs("label", { children: ["Hardware acceleration", _jsx("select", { value: s.hw_encoder ?? 'none', onChange: set('hw_encoder'), children: encoders.map(([v, label]) => (_jsx("option", { value: v, children: label }, v))) })] }), _jsx("div", { children: _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: testEncoder, children: "Test encoder" }) }), hwTest && _jsx("pre", { className: `hw-test ${hwTest.ok === false ? 'bad' : hwTest.ok ? 'ok' : ''}`, children: hwTest.text }), ['vaapi', 'qsv', 'nvenc'].includes(s.hw_encoder ?? '') && (_jsxs("label", { children: ["Hardware device (for servers with more than one GPU)", _jsx("input", { className: "mono", value: s.hw_device ?? '', onChange: set('hw_device'), placeholder: s.hw_encoder === 'nvenc' ? 'GPU number, e.g. 1 (blank = first GPU)' : '/dev/dri/renderD129 (blank = renderD128)' })] })), (s.hw_encoder ?? 'none') !== 'none' && (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.hw_decode === 'true', onChange: set('hw_decode') }), "Decode on the GPU too (less CPU when converting; falls back to the CPU for codecs the GPU can't decode)"] })), _jsx("p", { className: "muted small", children: "If the hardware encoder fails during Live TV, channels are converted on the CPU for the next 30 minutes and Live TV \u2192 Activity says why. Channels that are already H.264 are passed through without converting, so they never use the encoder." }), _jsxs("label", { children: ["Temporary transcode folder", _jsx("input", { value: s.transcode_dir ?? '', onChange: set('transcode_dir'), placeholder: "Default: inside the data folder", className: "mono" })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.tonemap_enabled === 'true', onChange: set('tonemap_enabled') }), "Tone map HDR to SDR when transcoding"] }), s.tonemap_enabled === 'true' && (_jsxs("label", { children: ["Tone mapping algorithm", _jsx("select", { value: s.tonemap_algorithm ?? 'hable', onChange: set('tonemap_algorithm'), children: tonemapAlgorithms.map(([v, label]) => (_jsx("option", { value: v, children: label }, v))) })] })), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.transcode_throttle === 'true', onChange: set('transcode_throttle') }), "Throttle transcodes (stay about 2\u00D7 ahead of playback instead of converting the whole file at full speed)"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.adaptive_streaming === 'true', onChange: set('adaptive_streaming') }), "Adaptive streaming: when a video has to be converted, make up to three qualities at once so the player can switch on slow connections (uses more CPU or GPU per viewer)"] })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Library" }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.watch_folders === 'true', onChange: set('watch_folders') }), "Watch library folders and add new files automatically"] }), _jsxs("p", { className: "muted small", children: ["Full rescans, backups and cleanup are under ", _jsx(Link, { to: "/admin/tasks", children: "Scheduled Tasks" }), ". Some network shares don't report changes; the scheduled scan still finds those."] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.allow_downloads === 'true', onChange: set('allow_downloads') }), "Let users download original files"] }), _jsxs("label", { children: ["Intro videos before movies", _jsx("textarea", { className: "mono", rows: 2, value: s.preroll_paths ?? '', onChange: (e) => setS({ ...s, preroll_paths: e.target.value }), placeholder: "Files or folders, one per line (e.g. /media/prerolls)", spellCheck: false })] }), _jsx("p", { className: "muted small", children: "One is picked at random each time, like Plex's movie pre-roll. Each person turns it off, or adds trailers, in their playback settings. Use MP4 (H.264 and AAC) so every browser can play it." })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Live TV playback" }), _jsxs("label", { children: ["Pause and rewind window (minutes)", _jsx("input", { type: "number", min: 0, max: 240, value: s.live_timeshift_minutes ?? '15', onChange: set('live_timeshift_minutes') })] }), _jsx("p", { className: "muted small", children: "How far back viewers can pause or rewind a channel. Each channel being watched keeps this much video in the transcode folder (about 40 MB per minute for HD), so keep it modest if that folder is in RAM. 0 keeps only a short buffer." }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Buffer (seconds behind live)", _jsx("input", { type: "number", min: 2, max: 60, value: s.live_buffer_seconds ?? '10', onChange: set('live_buffer_seconds') })] }), _jsxs("label", { children: ["Switch streams after buffering for (seconds)", _jsx("input", { type: "number", min: 3, max: 120, value: s.live_stall_seconds ?? '12', onChange: set('live_stall_seconds') })] })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.live_switch_slow !== 'false', onChange: set('live_switch_slow') }), "Also switch when a stream falls behind (not only when it stops)"] }), _jsx("p", { className: "muted small", children: "A bigger buffer rides out provider hiccups but starts each channel further behind live. A channel with fallback streams moves to the next one when its stream sends nothing for the switch time. With the box ticked, it also moves when the stream falls so far behind real time that viewers would have been buffering that long, but it won't go back to a stream it left for that within 10 minutes. Fallback streams are often a few seconds apart, so each switch can repeat or skip a moment; untick it if switches happen more than the buffering did." }), _jsxs("label", { children: ["Proxy for IPTV providers", _jsx("input", { className: "mono", value: s.live_proxy ?? '', onChange: set('live_proxy'), placeholder: "None (http://host:port or socks5://user:pass@host:port)", autoComplete: "off", spellCheck: false })] }), _jsx("p", { className: "muted small", children: "Streams, playlists, guides and logos are fetched through this, for example a VPN container. Leave it empty to connect directly (or use the container's HTTP_PROXY). HLS provider streams can only use an http:// proxy." })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Live TV recording (DVR)" }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.dvr_allow_users === 'true', onChange: set('dvr_allow_users') }), "Let users (not just admins) record"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.dvr_require_approval === 'true', onChange: set('dvr_require_approval') }), "Ask an admin to approve users' recordings first"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.dvr_priority === 'true', onChange: set('dvr_priority') }), "Recordings come first: when a provider is at its connection limit, stop a live channel someone is watching to record"] }), _jsxs("label", { children: ["Recordings folder", _jsx("input", { className: "mono", value: s.dvr_folder ?? '', onChange: set('dvr_folder'), placeholder: "Default: recordings inside the data folder" })] }), _jsxs("label", { children: ["Save recordings", _jsxs("select", { value: s.dvr_naming ?? 'library', onChange: set('dvr_naming'), children: [_jsx("option", { value: "library", children: "Named like a TV library (Show/Season 01/Show - S01E02.ts)" }), _jsx("option", { value: "folder", children: "All in one folder (Show - date.ts)" })] })] }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Start early (minutes)", _jsx("input", { type: "number", min: 0, max: 60, value: s.dvr_padding_before ?? '1', onChange: set('dvr_padding_before') })] }), _jsxs("label", { children: ["Keep going after (minutes)", _jsx("input", { type: "number", min: 0, max: 120, value: s.dvr_padding_after ?? '3', onChange: set('dvr_padding_after') })] })] })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Trakt" }), _jsxs("p", { className: "muted small", children: ["Lets people sync their watch history with Trakt from their Account page. Create an app at", ' ', _jsx("a", { href: "https://trakt.tv/oauth/applications/new", target: "_blank", rel: "noreferrer", children: "trakt.tv/oauth/applications" }), " with the redirect URI", ' ', _jsx("code", { children: "urn:ietf:wg:oauth:2.0:oob" }), ", then paste its keys here."] }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Client ID", _jsx("input", { className: "mono", value: s.trakt_client_id ?? '', onChange: set('trakt_client_id'), autoComplete: "off", spellCheck: false })] }), _jsxs("label", { children: ["Client secret", _jsx("input", { className: "mono", type: "password", value: s.trakt_client_secret ?? '', onChange: set('trakt_client_secret'), autoComplete: "new-password" })] })] })] }), _jsxs("section", { className: "panel form", children: [_jsx("h3", { children: "Network" }), _jsxs("label", { children: ["Public URL", _jsx("input", { className: "mono", value: s.public_url ?? '', onChange: set('public_url'), placeholder: "https://tv.example.com (optional)", spellCheck: false }), _jsx("span", { className: "muted small", children: "How people reach this server from outside. Used for single sign-on redirects. Leave empty to use whatever address the browser used." })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.remote_access !== 'false', onChange: set('remote_access') }), "Allow connections from outside the home network"] }), _jsxs("label", { children: ["Extra local networks", _jsx("input", { className: "mono", value: s.lan_networks ?? '', onChange: set('lan_networks'), placeholder: "e.g. 100.64.0.0/10 for Tailscale", spellCheck: false }), _jsx("span", { className: "muted small", children: "Private ranges (192.168.x, 10.x, 172.16\u201331.x) always count as local. Add others, comma-separated." })] }), _jsxs("label", { children: ["Trusted reverse proxies", _jsx("input", { className: "mono", value: s.trusted_proxies ?? '', onChange: set('trusted_proxies'), placeholder: "e.g. 172.17.0.1 or 10.0.0.0/8", spellCheck: false }), _jsx("span", { className: "muted small", children: "If you use Nginx, Caddy, Traefik or a Cloudflare tunnel, list its address here so BingeBandit sees the real client address and https. Only list proxies you run: their X-Forwarded-For headers are believed." })] })] }), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsx("div", { className: "actions", children: _jsx("button", { className: "btn btn-primary", children: "Save settings" }) })] }), _jsx(SingleSignOn, {}), _jsx(Webhooks, {})] }));
}
/** OpenID Connect providers people can sign in with. */
function SingleSignOn() {
    const { data, reload } = useApi('/api/admin/oidc');
    const [editing, setEditing] = useState(null);
    const remove = async (p) => {
        if (!confirm(`Remove ${p.name}? Accounts linked to it are unlinked, and people whose account was made by ${p.name} can only sign in again with a password an admin sets.`))
            return;
        await api.del(`/api/admin/oidc/${p.id}`).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs("section", { className: "panel", style: { marginTop: 20 }, children: [_jsxs("header", { className: "panel-head", children: [_jsx("h3", { children: "Single sign-on" }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing({ enabled: true, autoCreate: false, scopes: '' }), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add provider"] })] }), _jsx("p", { className: "muted small", children: "Let people sign in with Google, TinyAuth, Authelia, Authentik, Keycloak or any OpenID Connect provider. Register BingeBandit with the provider using this redirect URL:" }), data && _jsx("code", { className: "totp-secret", children: data.redirectUrl }), _jsx("div", { className: "list", children: data?.providers.map((p) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsxs("div", { children: [p.name, " ", !p.enabled && _jsx("span", { className: "pill", children: "Off" })] }), _jsx("div", { className: "muted small mono", style: { wordBreak: 'break-all' }, children: p.issuer }), _jsx("div", { className: "muted small", children: p.autoCreate ? 'Creates accounts for new people' : 'Only for people who linked it under Account' })] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(p), children: "Edit" }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(p), "aria-label": `Remove ${p.name}`, children: _jsx(Icon, { name: "trash", size: 16 }) })] }, p.id))) }), editing && (_jsx(ProviderEditor, { provider: editing, onClose: () => setEditing(null), onSaved: () => {
                    setEditing(null);
                    reload();
                } }))] }));
}
function ProviderEditor({ provider, onClose, onSaved }) {
    const [p, setP] = useState({ ...provider, clientSecret: '' });
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const save = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        try {
            if (p.id)
                await api.put(`/api/admin/oidc/${p.id}`, p);
            else
                await api.post('/api/admin/oidc', p);
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const presetGoogle = () => setP({ ...p, name: p.name || 'Google', issuer: 'https://accounts.google.com' });
    return (_jsx(Modal, { title: p.id ? `Edit ${provider.name}` : 'Add sign-in provider', onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("label", { children: ["Name on the sign-in button", _jsx("input", { value: p.name ?? '', onChange: (e) => setP({ ...p, name: e.target.value }), required: true, autoFocus: true, placeholder: "Google" })] }), _jsxs("label", { children: [_jsxs("span", { className: "field-head", children: ["Issuer URL", !p.id && (_jsx("button", { type: "button", className: "link-btn small", onClick: presetGoogle, children: "Use Google" }))] }), _jsx("input", { className: "mono", value: p.issuer ?? '', onChange: (e) => setP({ ...p, issuer: e.target.value }), required: true, placeholder: "https://auth.example.com", spellCheck: false })] }), _jsxs("label", { children: ["Client ID", _jsx("input", { className: "mono", value: p.clientId ?? '', onChange: (e) => setP({ ...p, clientId: e.target.value }), required: true, spellCheck: false, autoComplete: "off" })] }), _jsxs("label", { children: ["Client secret", _jsx("input", { className: "mono", type: "password", value: p.clientSecret ?? '', onChange: (e) => setP({ ...p, clientSecret: e.target.value }), placeholder: p.hasSecret ? 'Saved; leave empty to keep it' : '', autoComplete: "new-password" })] }), _jsxs("label", { children: ["Extra scopes (optional)", _jsx("input", { className: "mono", value: p.scopes ?? '', onChange: (e) => setP({ ...p, scopes: e.target.value }), placeholder: "openid, profile and email are always asked for", spellCheck: false })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.autoCreate ?? false, onChange: (e) => setP({ ...p, autoCreate: e.target.checked }) }), "Create an account for anyone who signs in with it"] }), _jsx("p", { className: "muted small", style: { marginTop: -6 }, children: "Only turn this on for a provider that just lets in people you trust (your own TinyAuth or Authelia). With Google it would let anyone with a Google account in. New accounts can see all libraries; adjust them under Users." }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.enabled ?? true, onChange: (e) => setP({ ...p, enabled: e.target.checked }) }), "Show on the sign-in page"] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: busy, children: busy ? 'Checking…' : 'Save' })] })] }) }));
}
const eventLabels = {
    'playback.start': 'Playback started',
    'playback.stop': 'Playback stopped',
    'playback.finished': 'Finished watching',
    'player.play': 'Player opened (at once)',
    'player.pause': 'Paused',
    'player.resume': 'Resumed',
    'player.stop': 'Player closed',
    'recording.started': 'Recording started',
    'recording.completed': 'Recording completed',
    'recording.failed': 'Recording failed',
};
/** Webhooks POST signed JSON to other services (Home Assistant, Discord bots, n8n…) when things happen. */
function Webhooks() {
    const { data, reload } = useApi('/api/admin/webhooks');
    const [editing, setEditing] = useState(null);
    const [results, setResults] = useState({});
    const test = async (h) => {
        setResults((r) => ({ ...r, [h.id]: 'Sending…' }));
        try {
            const res = await api.post(`/api/admin/webhooks/${h.id}/test`);
            setResults((r) => ({ ...r, [h.id]: res.status }));
        }
        catch (err) {
            setResults((r) => ({ ...r, [h.id]: err.message }));
        }
        reload();
    };
    const remove = async (h) => {
        if (!confirm(`Delete the webhook "${h.name}"?`))
            return;
        await api.del(`/api/admin/webhooks/${h.id}`).catch((e) => alert(e.message));
        reload();
    };
    const toggle = async (h, enabled) => {
        await api.put(`/api/admin/webhooks/${h.id}`, { ...h, enabled }).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs("section", { className: "panel", style: { marginTop: 20 }, children: [_jsxs("header", { className: "panel-head", children: [_jsx("h3", { children: "Webhooks" }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing({ enabled: true, events: [] }), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add webhook"] })] }), _jsxs("p", { className: "muted small", children: ["BingeBandit sends a JSON POST to each URL when a chosen event happens. With a secret set, the body is signed in the", ' ', _jsx("code", { children: "X-BingeBandit-Signature" }), " header (HMAC-SHA256)."] }), _jsx("div", { className: "list", children: data?.webhooks.map((h) => (_jsxs("div", { className: "list-row", children: [_jsxs("label", { className: "switch", title: h.enabled ? 'On' : 'Off', children: [_jsx("input", { type: "checkbox", checked: h.enabled, onChange: (e) => toggle(h, e.target.checked), "aria-label": `${h.name} enabled` }), _jsx("span", {})] }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: h.name }), _jsx("div", { className: "muted small mono", style: { wordBreak: 'break-all' }, children: h.url }), _jsxs("div", { className: "muted small", children: [h.events.length ? h.events.map((e) => eventLabels[e] ?? e).join(', ') : 'No events chosen', (results[h.id] || h.lastStatus) && _jsxs(_Fragment, { children: [" \u00B7 last response: ", results[h.id] || h.lastStatus] })] })] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => test(h), children: "Send test" }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(h), children: "Edit" }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(h), "aria-label": `Delete ${h.name}`, children: _jsx(Icon, { name: "trash", size: 16 }) })] }, h.id))) }), editing && data && (_jsx(WebhookEditor, { hook: editing, events: data.events, onClose: () => setEditing(null), onSaved: () => {
                    setEditing(null);
                    reload();
                } }))] }));
}
function WebhookEditor({ hook, events, onClose, onSaved }) {
    const [h, setH] = useState(hook);
    const [error, setError] = useState('');
    const toggle = (e) => {
        const cur = new Set(h.events ?? []);
        if (cur.has(e))
            cur.delete(e);
        else
            cur.add(e);
        setH({ ...h, events: events.filter((x) => cur.has(x)) });
    };
    const save = async (e) => {
        e.preventDefault();
        try {
            if (h.id)
                await api.put(`/api/admin/webhooks/${h.id}`, h);
            else
                await api.post('/api/admin/webhooks', h);
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: h.id ? 'Edit webhook' : 'Add webhook', onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("label", { children: ["Name", _jsx("input", { value: h.name ?? '', onChange: (e) => setH({ ...h, name: e.target.value }), required: true, autoFocus: true, placeholder: "Home Assistant" })] }), _jsxs("label", { children: ["URL", _jsx("input", { className: "mono", type: "url", value: h.url ?? '', onChange: (e) => setH({ ...h, url: e.target.value }), required: true, placeholder: "https://\u2026" })] }), _jsxs("label", { children: ["Format", _jsxs("select", { value: h.format ?? '', onChange: (e) => setH({ ...h, format: e.target.value }), children: [_jsx("option", { value: "", children: "BingeBandit JSON" }), _jsx("option", { value: "plex", children: "Plex-style (for Yamtrack and other apps that accept Plex webhooks; playback events only)" })] })] }), _jsxs("label", { children: ["Secret (optional, for signing)", _jsx("input", { className: "mono", value: h.secret ?? '', onChange: (e) => setH({ ...h, secret: e.target.value }), autoComplete: "off" })] }), _jsx("fieldset", { className: "webhook-events", children: events.map((ev) => (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: h.events?.includes(ev) ?? false, onChange: () => toggle(ev) }), eventLabels[ev] ?? ev] }, ev))) }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Save" })] })] }) }));
}
