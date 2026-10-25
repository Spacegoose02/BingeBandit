import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { IPTVLinks } from '../components/IPTVLinks';
import { Icon } from '../components/Icon';
import { PasscodeSettings } from '../components/Passcode';
import { Profiles } from '../components/Profiles';
import { useApi, useDocumentTitle } from '../hooks';
import { commonLanguages, languageName } from '../format';
import { createPasskey, passkeyErrorMessage, passkeysSupported } from '../passkeys';
function when(ts) {
    return ts ? new Date(ts * 1000).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : 'never';
}
/** Turns a browser user agent into "Chrome on Windows"; app-provided names pass through. */
function deviceName(d) {
    if (!d)
        return 'Unknown device';
    if (!d.startsWith('Mozilla/'))
        return d;
    const browser = /Edg\//.test(d) ? 'Edge' : /Firefox\//.test(d) ? 'Firefox' : /Chrome\//.test(d) ? 'Chrome' : /Safari\//.test(d) ? 'Safari' : 'A browser';
    const os = /Windows/.test(d) ? 'Windows' : /Android/.test(d) ? 'Android' : /iPhone|iPad/.test(d) ? 'iOS' : /Mac OS X/.test(d) ? 'macOS' : /Linux/.test(d) ? 'Linux' : '';
    return os ? `${browser} on ${os}` : browser;
}
/** Pages that running plugins offer, such as the Watchlist RSS feeds page. */
function PluginPages() {
    const { data } = useApi('/api/plugins');
    if (!data?.length)
        return null;
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Plugins" }), _jsx("div", { className: "list", children: data.flatMap((p) => p.pages.map((pg) => (_jsxs("a", { className: "list-row", href: `/api/plugins/${p.id}${pg.path.startsWith('/') ? '' : '/'}${pg.path}`, target: "_blank", rel: "noreferrer", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { children: pg.title }), _jsx("div", { className: "muted small", children: p.name })] }), _jsx(Icon, { name: "next", size: 16 })] }, p.id + pg.path)))) })] }));
}
function Sharing() {
    const { user, refresh } = useAuth();
    if (!user)
        return null;
    const toggle = async (share) => {
        await api.put('/api/me/share-activity', { share }).catch((e) => alert(e.message));
        refresh();
    };
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Sharing" }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: user.shareActivity, onChange: (e) => toggle(e.target.checked) }), "Show other people on this server what I'm watching right now"] }), _jsx("p", { className: "muted small", children: "They see the title and how far in you are, never your device or location." }), _jsx(RecommendOptIn, {})] }));
}
function RecommendOptIn() {
    const { data, reload } = useApi('/api/me/recommendations');
    if (!data)
        return null;
    const toggle = async (accept) => {
        await api.put('/api/me/recommendations', { accept }).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs(_Fragment, { children: [_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: data.accept, onChange: (e) => toggle(e.target.checked) }), "Let other people on this server recommend movies and shows to me"] }), _jsx("p", { className: "muted small", children: "Recommendations show up in a \u201CRecommended for You\u201D row on Home." })] }));
}
function TwoFactor() {
    const { user, refresh } = useAuth();
    const [setup, setSetup] = useState(null);
    const [code, setCode] = useState('');
    const [password, setPassword] = useState('');
    const [recovery, setRecovery] = useState(null);
    const [error, setError] = useState('');
    if (!user)
        return null;
    const start = async () => {
        setError('');
        try {
            setSetup(await api.post('/api/me/2fa/setup'));
        }
        catch (err) {
            setError(err.message);
        }
    };
    const enable = async (e) => {
        e.preventDefault();
        setError('');
        try {
            const res = await api.post('/api/me/2fa/enable', { code });
            setRecovery(res.recoveryCodes);
            setSetup(null);
            setCode('');
            refresh();
        }
        catch (err) {
            setError(err.message);
        }
    };
    const disable = async (e) => {
        e.preventDefault();
        setError('');
        try {
            await api.post('/api/me/2fa/disable', { password });
            setPassword('');
            setRecovery(null);
            refresh();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Two-step sign-in" }), recovery && (_jsxs("div", { className: "recovery", children: [_jsxs("p", { children: [_jsx("strong", { children: "Two-step sign-in is on." }), " Save these recovery codes somewhere safe. Each one works once if you lose your phone. They won't be shown again."] }), _jsx("ul", { className: "recovery-codes", children: recovery.map((c) => (_jsx("li", { children: c }, c))) }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => navigator.clipboard?.writeText(recovery.join('\n')), children: "Copy codes" })] })), user.totpEnabled ? (_jsxs("form", { className: "form", onSubmit: disable, children: [!recovery && _jsx("p", { className: "muted", children: "On. Signing in asks for a code from your authenticator app." }), _jsxs("label", { children: ["Password to turn it off", _jsx("input", { type: "password", value: password, onChange: (e) => setPassword(e.target.value), required: true, autoComplete: "current-password" })] }), error && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "actions", children: _jsx("button", { className: "btn btn-glass danger", children: "Turn off two-step sign-in" }) })] })) : setup ? (_jsxs("form", { className: "form", onSubmit: enable, children: [_jsx("p", { className: "muted", children: "In your authenticator app (Google Authenticator, 1Password, Authy\u2026), add an account with this key, or open the link on your phone." }), _jsx("code", { className: "totp-secret", children: setup.secret.replace(/(.{4})/g, '$1 ').trim() }), _jsx("a", { className: "small", href: setup.uri, children: "Open in authenticator app" }), _jsxs("label", { children: ["Then enter the 6-digit code it shows", _jsx("input", { value: code, onChange: (e) => setCode(e.target.value), required: true, inputMode: "numeric", autoComplete: "one-time-code", placeholder: "123456" })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-primary", children: "Turn on" }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: () => setSetup(null), children: "Cancel" })] })] })) : (_jsxs(_Fragment, { children: [_jsx("p", { className: "muted", children: "Off. Add a code from your phone on top of your password when you sign in." }), error && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "actions", children: _jsx("button", { type: "button", className: "btn btn-primary", onClick: start, children: "Set up two-step sign-in" }) })] }))] }));
}
function Passkeys() {
    const { user } = useAuth();
    const { data: keys, reload } = useApi('/api/me/passkeys');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    if (!user || user.ownerId || !keys)
        return null;
    const supported = passkeysSupported();
    const add = async () => {
        setError('');
        setBusy(true);
        try {
            const { flow, options } = await api.post('/api/me/passkeys/begin');
            let credential;
            try {
                credential = await createPasskey(options);
            }
            catch (err) {
                setError(passkeyErrorMessage(err));
                return;
            }
            const name = prompt('Name this passkey (for example "MacBook" or "iPhone")', '') ?? '';
            await api.post('/api/me/passkeys', { flow, credential, name });
            reload();
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const rename = async (k) => {
        const name = prompt('New name', k.name);
        if (!name?.trim())
            return;
        await api.put(`/api/me/passkeys/${k.id}`, { name }).catch((e) => setError(e.message));
        reload();
    };
    const remove = async (k) => {
        if (!confirm(`Remove "${k.name}"? Also delete it from the device or password manager that holds it.`))
            return;
        await api.del(`/api/me/passkeys/${k.id}`).catch((e) => setError(e.message));
        reload();
    };
    const when = (unix) => new Date(unix * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' });
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Passkeys" }), _jsx("p", { className: "muted", children: "Sign in with your fingerprint, face or device PIN instead of a password and code. A passkey lives on your phone, computer or password manager and only works for this server." }), keys.length > 0 && (_jsx("div", { className: "list", children: keys.map((k) => (_jsxs("div", { className: "list-row", children: [_jsx(Icon, { name: "key" }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: k.name }), _jsxs("div", { className: "muted small", children: ["Added ", when(k.createdAt), " \u00B7 ", k.lastUsedAt ? `last used ${when(k.lastUsedAt)}` : 'not used yet'] })] }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => rename(k), children: "Rename" }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm danger", onClick: () => remove(k), "aria-label": `Remove ${k.name}`, children: _jsx(Icon, { name: "trash", size: 16 }) })] }, k.id))) })), !supported && (_jsx("p", { className: "muted small", children: "This browser can't make passkeys on this address. Passkeys need the server's https:// address (or localhost on the server itself)." })), error && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "actions", children: _jsxs("button", { type: "button", className: "btn btn-primary", onClick: add, disabled: !supported || busy, children: [_jsx(Icon, { name: "plus", size: 18 }), " Add a passkey"] }) })] }));
}
function LinkedAccounts() {
    const { data: providers } = useApi('/api/auth/oidc');
    const { data: linked, reload } = useApi('/api/me/identities');
    const [params, setParams] = useSearchParams();
    const [error, setError] = useState(params.get('sso_error') ?? '');
    const justLinked = params.get('linked');
    if (!providers?.length && !linked?.length)
        return null;
    const link = async (id) => {
        setError('');
        try {
            const { url } = await api.post(`/api/me/identities/${id}/link`);
            window.location.href = url;
        }
        catch (err) {
            setError(err.message);
        }
    };
    const unlink = async (i) => {
        if (!confirm(`Unlink your ${i.providerName} account? You won't be able to sign in with it until you link it again.`))
            return;
        setError('');
        try {
            await api.del(`/api/me/identities/${i.id}`);
            reload();
        }
        catch (err) {
            setError(err.message);
        }
    };
    const unlinked = (providers ?? []).filter((p) => !linked?.some((i) => i.providerId === p.id));
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Linked accounts" }), _jsx("p", { className: "muted small", children: "Sign in with these instead of your password." }), justLinked && (_jsxs("div", { className: "form-ok", children: ["Your ", justLinked, " account is linked.", ' ', _jsx("button", { type: "button", className: "link-btn", onClick: () => setParams({}, { replace: true }), children: "Dismiss" })] })), linked && linked.length > 0 && (_jsx("div", { className: "list", children: linked.map((i) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { children: i.providerName }), _jsxs("div", { className: "muted small", children: [i.email || 'No email shared', " \u00B7 linked ", when(i.createdAt), " \u00B7 last used ", when(i.lastUsed)] })] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => unlink(i), children: "Unlink" })] }, i.id))) })), unlinked.length > 0 && (_jsx("div", { className: "actions", children: unlinked.map((p) => (_jsxs("button", { type: "button", className: "btn btn-glass", onClick: () => link(p.id), children: ["Link ", p.name] }, p.id))) })), error && _jsx("div", { className: "form-error", children: error })] }));
}
function Trakt() {
    const { user } = useAuth();
    const { data, reload } = useApi('/api/me/trakt');
    const [code, setCode] = useState(null);
    const [msg, setMsg] = useState(null);
    const [busy, setBusy] = useState('');
    // Poll while a code is showing.
    useEffect(() => {
        if (!code)
            return;
        let stopped = false;
        let timer = 0;
        const poll = async () => {
            try {
                const acc = await api.post('/api/me/trakt/poll');
                if (stopped)
                    return;
                if (acc) {
                    setCode(null);
                    setMsg({ ok: true, text: `Connected as ${acc.username}. New things you finish will be added to your Trakt history.` });
                    reload();
                    return;
                }
                timer = window.setTimeout(poll, Math.max(code.interval, 1) * 1000);
            }
            catch (err) {
                if (!stopped) {
                    setCode(null);
                    setMsg({ ok: false, text: err.message });
                }
            }
        };
        timer = window.setTimeout(poll, Math.max(code.interval, 1) * 1000);
        return () => {
            stopped = true;
            clearTimeout(timer);
        };
    }, [code, reload]);
    if (!data)
        return null;
    const act = async (label, f) => {
        setBusy(label);
        setMsg(null);
        try {
            setMsg({ ok: true, text: await f() });
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
        finally {
            setBusy('');
            reload();
        }
    };
    const connect = () => act('connect', async () => {
        setCode(await api.post('/api/me/trakt/connect'));
        return '';
    });
    const importHistory = () => act('import', async () => {
        const s = await api.post('/api/me/trakt/import');
        return `Read ${s.seen} plays from Trakt; ${s.matched} are in your libraries. Added ${s.plays} to your history${s.duplicates ? ` (${s.duplicates} were already here)` : ''} and marked ${s.watched} watched.${s.unmatchedCount ? ` ${s.unmatchedCount} ${s.unmatchedCount === 1 ? "title isn't" : "titles aren't"} on this server.` : ''}`;
    });
    const push = (all) => act('push', async () => {
        const r = await api.post('/api/me/trakt/push', { all });
        return `Sent ${r.movies} movies and ${r.episodes} episodes to Trakt.${r.notFound ? ` Trakt didn't recognise ${r.notFound}.` : ''}`;
    });
    const setAuto = async (autoSync) => {
        await api.put('/api/me/trakt', { autoSync }).catch((e) => alert(e.message));
        reload();
    };
    const disconnect = async () => {
        if (!confirm('Disconnect Trakt? Your Trakt history stays as it is.'))
            return;
        await api.del('/api/me/trakt').catch((e) => alert(e.message));
        setMsg(null);
        reload();
    };
    const acc = data.account;
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Trakt" }), !data.configured && !acc ? (_jsxs("p", { className: "muted small", children: ["Keep your Trakt history (and apps that read it, like Yamtrack) up to date with what you watch here.", ' ', user?.isAdmin ? (_jsxs(_Fragment, { children: ["First add a Trakt app under ", _jsx(Link, { to: "/admin/settings", children: "Settings \u2192 Trakt" }), "."] })) : ('Ask your server admin to set up Trakt first.')] })) : acc ? (_jsxs(_Fragment, { children: [_jsxs("p", { className: "muted small", children: ["Connected as ", _jsx("strong", { children: acc.username }), acc.lastSync > 0 && _jsxs(_Fragment, { children: [" \u00B7 last synced ", when(acc.lastSync)] })] }), acc.lastError && _jsxs("div", { className: "form-error", children: ["Last sync failed: ", acc.lastError] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: acc.autoSync, onChange: (e) => setAuto(e.target.checked) }), "Add what I finish here to my Trakt history automatically"] }), _jsxs("div", { className: "actions", children: [_jsx("button", { type: "button", className: "btn btn-glass", disabled: !!busy, onClick: importHistory, children: busy === 'import' ? 'Importing…' : 'Import my Trakt history' }), (data.pendingAll ?? 0) > 0 && (_jsx("button", { type: "button", className: "btn btn-glass", disabled: !!busy, onClick: () => push(true), children: busy === 'push' ? 'Sending…' : `Send ${data.pendingAll} plays to Trakt` })), _jsx("button", { type: "button", className: "btn btn-glass danger", onClick: disconnect, children: "Disconnect" })] }), (data.pendingAll ?? 0) > 0 && (_jsxs("p", { className: "muted small", children: [data.pendingAll, " finished plays aren't on Trakt yet", (data.pendingNew ?? 0) < (data.pendingAll ?? 0) && ', mostly from before you connected (those are only sent when you ask)', "."] }))] })) : code ? (_jsxs("div", { className: "link-code", children: [_jsxs("p", { className: "muted", children: ["Go to", ' ', _jsx("a", { href: code.verificationUrl, target: "_blank", rel: "noreferrer", children: code.verificationUrl.replace(/^https?:\/\//, '') }), ' ', "and enter"] }), _jsx("div", { className: "link-code-value", children: code.userCode }), _jsx("p", { className: "muted small", children: "Waiting for you to approve it on Trakt\u2026" }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setCode(null), children: "Cancel" })] })) : (_jsxs(_Fragment, { children: [_jsx("p", { className: "muted small", children: "Keep your Trakt history (and apps that read it, like Yamtrack) up to date with what you watch here." }), _jsx("div", { className: "actions", children: _jsx("button", { type: "button", className: "btn btn-primary", disabled: !!busy, onClick: connect, children: "Connect Trakt" }) })] })), msg?.text && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text })] }));
}
function Downloads() {
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Download my history" }), _jsx("p", { className: "muted small", children: "Everything you've watched here, with TMDB ids so other apps can match it." }), _jsxs("div", { className: "actions", children: [_jsxs("a", { className: "btn btn-glass", href: "/api/me/export/history.csv", download: true, children: [_jsx(Icon, { name: "download", size: 16 }), " Watch history (CSV)"] }), _jsxs("a", { className: "btn btn-glass", href: "/api/me/export/letterboxd.csv", download: true, children: [_jsx(Icon, { name: "download", size: 16 }), " Movies for Letterboxd"] })] }), _jsx("p", { className: "muted small", children: "Letterboxd: Settings \u2192 Import & Export \u2192 Import your data, then pick the file." })] }));
}
function Devices() {
    const { data: sessions, reload } = useApi('/api/me/sessions');
    const revoke = async (s) => {
        await api.del(`/api/me/sessions/${s.id}`).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs("div", { className: "panel", style: { marginBottom: 20 }, children: [_jsxs("header", { className: "panel-head", children: [_jsx("h3", { children: "Signed-in devices" }), _jsxs(Link, { to: "/link", className: "btn btn-glass btn-sm", children: [_jsx(Icon, { name: "tv", size: 16 }), " Link a TV"] })] }), _jsx("div", { className: "list", children: sessions?.map((s) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsxs("div", { children: [_jsx("span", { title: s.device, children: deviceName(s.device) }), " ", s.current && _jsx("span", { className: "pill ok", children: "This device" })] }), _jsxs("div", { className: "muted small", children: [s.ip, " \u00B7 last active ", when(s.lastSeen), " \u00B7 signed in ", when(s.createdAt)] })] }), !s.current && (_jsx("button", { className: "btn btn-glass btn-sm", onClick: () => revoke(s), children: "Sign out" }))] }, s.id))) })] }));
}
function APIKeys() {
    const { data: keys, reload } = useApi('/api/me/keys');
    const { data: channels } = useApi('/api/live/channels');
    const [name, setName] = useState('');
    const [created, setCreated] = useState(null);
    const [error, setError] = useState('');
    const create = async (e) => {
        e.preventDefault();
        setError('');
        try {
            const res = await api.post('/api/me/keys', { name });
            setCreated(res.key);
            setName('');
            reload();
        }
        catch (err) {
            setError(err.message);
        }
    };
    const remove = async (k) => {
        if (!confirm(`Delete the key "${k.name}"? Anything using it stops working.`))
            return;
        await api.del(`/api/me/keys/${k.id}`).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "API keys" }), _jsxs("p", { className: "muted small", children: ["For scripts, Home Assistant, or IPTV apps that load the playlist and guide URLs. A key acts as you, with your library access. Send it as ", _jsx("code", { children: "Authorization: Bearer \u2026" }), " or ", _jsx("code", { children: "?token=\u2026" }), "."] }), created && (_jsxs("div", { className: "recovery", children: [_jsx("p", { children: "Copy your new key now. It won't be shown again." }), _jsx("code", { className: "totp-secret", children: created }), _jsxs("div", { className: "actions", children: [_jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => navigator.clipboard?.writeText(created), children: "Copy" }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setCreated(null), children: "Done" })] })] })), keys && keys.length > 0 && (_jsx("div", { className: "list", children: keys.map((k) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { children: k.name }), _jsxs("div", { className: "muted small", children: [_jsxs("code", { children: [k.prefix, "\u2026"] }), " \u00B7 created ", when(k.createdAt), " \u00B7 last used ", when(k.lastUsed)] })] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(k), "aria-label": `Delete ${k.name}`, children: _jsx(Icon, { name: "trash", size: 16 }) })] }, k.id))) })), _jsxs("form", { className: "inline-form", onSubmit: create, children: [_jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: "Key name, e.g. TiviMate", required: true }), _jsx("button", { className: "btn btn-primary", children: "Create key" })] }), error && _jsx("div", { className: "form-error", children: error }), !!channels?.length && (_jsxs(_Fragment, { children: [_jsx("h3", { style: { marginTop: 20 }, children: "Live TV in other apps" }), _jsx("p", { className: "muted small", children: "Your own playlist and guide, with the channels you can watch." }), _jsx(IPTVLinks, { onCreated: reload })] }))] }));
}
function PlaybackPrefs() {
    const { data } = useApi('/api/me/prefs');
    const [p, setP] = useState(null);
    const [msg, setMsg] = useState(null);
    useEffect(() => setP(data), [data]);
    if (!p)
        return null;
    const set = (k, v) => setP({ ...p, [k]: v });
    const save = async (e) => {
        e.preventDefault();
        try {
            setP(await api.put('/api/me/prefs', p));
            setMsg({ ok: true, text: 'Playback settings saved.' });
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
    };
    const langSelect = (value, onChange, emptyLabel) => (_jsxs("select", { value: value, onChange: (e) => onChange(e.target.value), children: [_jsx("option", { value: "", children: emptyLabel }), commonLanguages.map((l) => (_jsx("option", { value: l, children: languageName(l) }, l)))] }));
    return (_jsxs("form", { className: "panel form", onSubmit: save, style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Playback" }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Preferred audio language", langSelect(p.audioLanguage, (v) => set('audioLanguage', v), "File's default track")] }), _jsxs("label", { children: ["Subtitle language", langSelect(p.subtitleLanguage, (v) => set('subtitleLanguage', v), 'None')] }), _jsxs("label", { className: "span-2", children: ["Show subtitles", _jsxs("select", { value: p.subtitleMode, onChange: (e) => set('subtitleMode', e.target.value), children: [_jsx("option", { value: "auto", children: "When the audio is in another language (plus forced subtitles)" }), _jsx("option", { value: "always", children: "Always" }), _jsx("option", { value: "forced", children: "Only forced subtitles (foreign dialogue, signs)" }), _jsx("option", { value: "off", children: "Never" })] })] }), _jsxs("label", { children: ["Subtitle size", _jsxs("select", { value: p.subtitleSize, onChange: (e) => set('subtitleSize', e.target.value), children: [_jsx("option", { value: "small", children: "Small" }), _jsx("option", { value: "medium", children: "Medium" }), _jsx("option", { value: "large", children: "Large" })] })] }), _jsxs("label", { className: "check", style: { alignSelf: 'end' }, children: [_jsx("input", { type: "checkbox", checked: p.subtitleBackground, onChange: (e) => set('subtitleBackground', e.target.checked) }), "Dark box behind subtitles"] })] }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Intros", _jsxs("select", { value: p.skipIntro, onChange: (e) => set('skipIntro', e.target.value), children: [_jsx("option", { value: "button", children: "Show a Skip Intro button" }), _jsx("option", { value: "auto", children: "Skip automatically" }), _jsx("option", { value: "off", children: "Don't offer to skip" })] })] }), _jsxs("label", { children: ["End credits", _jsxs("select", { value: p.skipCredits, onChange: (e) => set('skipCredits', e.target.value), children: [_jsx("option", { value: "button", children: "Show a Next button" }), _jsx("option", { value: "auto", children: "Go to the next episode automatically" }), _jsx("option", { value: "off", children: "Don't offer to skip" })] })] })] }), _jsx("div", { className: "field-grid", children: _jsxs("label", { children: ["Before a movie", _jsxs("select", { value: p.preRollTrailers, onChange: (e) => set('preRollTrailers', Number(e.target.value)), children: [_jsx("option", { value: 0, children: "No trailers" }), [1, 2, 3, 4, 5].map((n) => (_jsxs("option", { value: n, children: [n === 1 ? '1 trailer' : `${n} trailers`, " of other movies"] }, n)))] })] }) }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.preRollIntro, onChange: (e) => set('preRollIntro', e.target.checked) }), "Play the server's intro video before a movie (when your admin has set one)"] }), _jsx("p", { className: "muted small", children: "Trailers are of movies here you haven't watched, newest first, never rated above the movie you're starting. They play only when a movie starts from the beginning, in the web player; the TV app plays the intro." }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.autoQuality, onChange: (e) => set('autoQuality', e.target.checked) }), "Lower the quality automatically when playback keeps buffering"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.autoPlayNext, onChange: (e) => set('autoPlayNext', e.target.checked) }), "Play the next episode automatically"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.notifyEpisodes, onChange: (e) => set('notifyEpisodes', e.target.checked) }), "Tell me when new episodes arrive for shows I watch or have on my watchlist"] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: p.notifyRecordings, onChange: (e) => set('notifyRecordings', e.target.checked) }), "Tell me when my recordings finish or fail"] }), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsx("div", { className: "actions", children: _jsx("button", { className: "btn btn-primary", children: "Save playback settings" }) })] }));
}
export default function Account() {
    useDocumentTitle('Account');
    const { user, refresh } = useAuth();
    const [current, setCurrent] = useState('');
    const [next, setNext] = useState('');
    const [msg, setMsg] = useState(null);
    const hasPassword = user?.hasPassword ?? true;
    const submit = async (e) => {
        e.preventDefault();
        try {
            await api.put('/api/me/password', { current, new: next });
            setMsg({ ok: true, text: hasPassword ? 'Password changed.' : 'Password set. You can now sign in with it too.' });
            setCurrent('');
            setNext('');
            if (!hasPassword)
                refresh();
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
    };
    return (_jsxs("div", { className: "page narrow", children: [_jsx("h1", { children: "Account" }), _jsxs("p", { className: "muted", children: ["Signed in as ", _jsx("strong", { children: user?.profileName ?? user?.username }), user?.isAdmin && ' (admin)'] }), _jsx(Profiles, {}), _jsx(PlaybackPrefs, {}), _jsx(Sharing, {}), _jsx(Trakt, {}), _jsx(PluginPages, {}), _jsx(Downloads, {}), _jsx(LinkedAccounts, {}), hasPassword && _jsx(TwoFactor, {}), _jsx(Passkeys, {}), _jsx(PasscodeSettings, {}), _jsx(Devices, {}), _jsx(APIKeys, {}), _jsxs("form", { className: "panel form", onSubmit: submit, children: [_jsx("h3", { children: hasPassword ? 'Change password' : 'Set a password' }), hasPassword ? (_jsxs("label", { children: ["Current password", _jsx("input", { type: "password", value: current, onChange: (e) => setCurrent(e.target.value), required: true, autoComplete: "current-password" })] })) : (_jsx("p", { className: "muted small", children: "Your account was made by single sign-on. Set a password to also sign in with your username, for example on a TV." })), _jsxs("label", { children: ["New password", _jsx("input", { type: "password", value: next, onChange: (e) => setNext(e.target.value), required: true, minLength: 8, autoComplete: "new-password" })] }), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsx("button", { className: "btn btn-primary", children: hasPassword ? 'Update password' : 'Set password' })] })] }));
}
