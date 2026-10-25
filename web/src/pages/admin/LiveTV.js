import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../../api';
import { Icon } from '../../components/Icon';
import { IPTVLinks } from '../../components/IPTVLinks';
import { Modal } from '../../components/Modal';
import { useApi, useDocumentTitle } from '../../hooks';
import { formatBytes, formatClock, timeAgo } from '../../format';
import { ChannelLogo, channelNo } from '../LiveGuide';
import { CustomChannelEditor } from './CustomChannel';
import { DispatcharrImport } from './DispatcharrImport';
import { Logos } from './Logos';
import { LogoField } from '../../components/ChannelLogos';
export default function AdminLiveTV() {
    useDocumentTitle('Live TV');
    const [params, setParams] = useSearchParams();
    const tab = params.get('tab') || 'activity';
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Live TV" }), _jsx("div", { className: "muted", children: "Playlists and guides from your IPTV providers, the channels built from them, and who's watching." })] }), _jsx("div", { className: "segmented", children: ['activity', 'channels', 'logos', 'sources'].map((t) => (_jsx("button", { className: tab === t ? 'active' : '', onClick: () => setParams({ tab: t }, { replace: true }), children: { activity: 'Activity', channels: 'Channels', logos: 'Logos', sources: 'Sources' }[t] }, t))) })] }), tab === 'sources' && _jsx(Sources, {}), tab === 'channels' && _jsx(Channels, {}), tab === 'logos' && _jsx(Logos, {}), tab === 'activity' && _jsx(Activity, {})] }));
}
// ---- Sources ---------------------------------------------------------------
function Sources() {
    const { data, reload } = useApi('/api/admin/live/sources', 5000);
    const [editing, setEditing] = useState(null);
    const [filtering, setFiltering] = useState(null);
    const [importing, setImporting] = useState(false);
    const [busy, setBusy] = useState(null);
    const refresh = async (s) => {
        setBusy(s.id);
        await api.post(`/api/admin/live/sources/${s.id}/refresh`).catch((e) => alert(e.message));
        setBusy(null);
        reload();
    };
    const remove = async (s) => {
        if (!confirm(`Remove "${s.name}"? ${s.kind === 'm3u' ? 'Channels lose the streams it provided.' : 'Its guide data is deleted.'}`))
            return;
        await api.del(`/api/admin/live/sources/${s.id}`);
        reload();
    };
    const section = (kind, title, blurb) => (_jsxs("section", { children: [_jsxs("div", { className: "section-title", style: { justifyContent: 'space-between' }, children: [_jsx("span", { children: title }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing({ kind, refreshHours: kind === 'm3u' ? 24 : 12, enabled: true, maxStreams: 0 }), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add"] })] }), _jsx("p", { className: "muted small", style: { marginTop: -6 }, children: blurb }), _jsxs("div", { className: "panel list", children: [data?.filter((s) => s.kind === kind).map((s) => (_jsxs("div", { className: "list-row", children: [_jsx(Icon, { name: kind === 'm3u' ? 'tv' : 'list' }), _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [s.name, " ", !s.enabled && _jsx("span", { className: "pill", children: "Disabled" })] }), _jsxs("div", { className: "muted small", children: [s.entryCount.toLocaleString(), " ", kind === 'm3u' ? 'streams' : 'programs', " \u00B7 refreshed ", timeAgo(s.lastRefresh), " \u00B7 every ", s.refreshHours, "h", kind === 'm3u' && s.maxStreams > 0 && ` · max ${s.maxStreams} connections`] }), s.lastError && _jsxs("div", { className: "form-error small", children: ["Last refresh failed: ", s.lastError] })] }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: busy === s.id, onClick: () => refresh(s), children: [_jsx(Icon, { name: "refresh", size: 16, className: busy === s.id ? 'spin' : '' }), " Refresh"] }), kind === 'm3u' && _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setFiltering(s), children: "Groups" }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(s), children: _jsx(Icon, { name: "edit", size: 16 }) }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(s), "aria-label": "Remove source", children: _jsx(Icon, { name: "trash", size: 16 }) })] }, s.id))), data?.filter((s) => s.kind === kind).length === 0 && _jsx("div", { className: "list-row muted", children: "None yet." })] })] }));
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "panel list-row", style: { marginBottom: 16 }, children: [_jsx(Icon, { name: "download" }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: "Moving from Dispatcharr?" }), _jsx("div", { className: "muted small", children: "Import its provider accounts, guides and channels in one go." })] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setImporting(true), children: "Import from Dispatcharr" })] }), section('m3u', 'M3U playlists', 'Your IPTV provider accounts. Set the connection limit to what your plan allows; BingeBandit shares one connection per channel between viewers and skips a provider when it is full.'), section('epg', 'Guides (XMLTV)', 'Program guide data. Channels match guide entries by their tvg-id; you can change the match per channel.'), _jsx("h2", { className: "section-title", children: "Use these channels in other apps" }), _jsx(IPTVLinks, {}), editing && _jsx(SourceEditor, { source: editing, onClose: () => setEditing(null), onSaved: () => { setEditing(null); reload(); } }), importing && _jsx(DispatcharrImport, { onClose: () => setImporting(false), onChanged: reload }), filtering && _jsx(GroupFilter, { source: filtering, onClose: () => setFiltering(null), onSaved: () => { setFiltering(null); reload(); } })] }));
}
// GroupFilter picks which of a playlist's groups are imported. Streams in a
// disabled group are dropped on the next refresh, unless a channel uses them.
function GroupFilter({ source, onClose, onSaved }) {
    const { data } = useApi(`/api/admin/live/sources/${source.id}/groups`);
    const [enabled, setEnabled] = useState(null);
    const [newGroups, setNewGroups] = useState(true);
    const [q, setQ] = useState('');
    const [show, setShow] = useState('all');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    useEffect(() => {
        if (!data || enabled)
            return;
        setEnabled(new Map(data.groups.map((g) => [g.name, g.enabled])));
        setNewGroups(data.newGroupsEnabled);
    }, [data, enabled]);
    const on = (name) => enabled?.get(name) ?? true;
    const needle = q.trim().toLowerCase();
    const visible = (data?.groups ?? []).filter((g) => (!needle || (g.name || '(no group)').toLowerCase().includes(needle)) && (show === 'all' || (show === 'on') === on(g.name)));
    const setAll = (names, value) => setEnabled((m) => {
        const n = new Map(m);
        names.forEach((name) => n.set(name, value));
        return n;
    });
    const onCount = data?.groups.filter((g) => on(g.name)).length ?? 0;
    const onStreams = data?.groups.reduce((sum, g) => sum + (on(g.name) ? g.streams : 0), 0) ?? 0;
    const save = async () => {
        setBusy(true);
        setError('');
        try {
            await api.put(`/api/admin/live/sources/${source.id}/groups`, {
                newGroupsEnabled: newGroups,
                groups: [...(enabled ?? new Map())].map(([name, value]) => ({ name, enabled: value })),
            });
            await api.post(`/api/admin/live/sources/${source.id}/refresh`);
            onSaved();
        }
        catch (e) {
            setError(e.message);
            setBusy(false);
        }
    };
    return (_jsx(Modal, { title: `Groups in ${source.name}`, onClose: onClose, wide: true, children: _jsxs("div", { className: "form", children: [_jsx("p", { className: "muted small", style: { margin: 0 }, children: "Only streams in enabled groups are imported, so Add channels has less to search. Turning a group off never breaks a channel: streams your channels already use are kept." }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: newGroups, onChange: (e) => setNewGroups(e.target.checked) }), " Enable groups that appear in future refreshes"] }), _jsxs("div", { className: "actions", children: [_jsx("input", { className: "grow", value: q, onChange: (e) => setQ(e.target.value), placeholder: "Filter groups\u2026", style: { minWidth: 180 } }), _jsx("div", { className: "segmented", children: ['all', 'on', 'off'].map((v) => (_jsx("button", { className: show === v ? 'active' : '', onClick: () => setShow(v), children: v === 'all' ? 'All' : v === 'on' ? 'Enabled' : 'Disabled' }, v))) }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setAll(visible.map((g) => g.name), true), children: "Enable shown" }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setAll(visible.map((g) => g.name), false), children: "Disable shown" })] }), _jsxs("div", { className: "group-grid", children: [visible.map((g) => (_jsxs("button", { type: "button", className: `group-toggle ${on(g.name) ? 'on' : ''}`, onClick: () => setAll([g.name], !on(g.name)), children: [_jsx(Icon, { name: on(g.name) ? 'check' : 'x', size: 14 }), _jsx("span", { className: "grow", title: g.name, children: g.name || '(no group)' }), _jsx("span", { className: "small", children: g.streams.toLocaleString() })] }, g.name))), data && visible.length === 0 && _jsx("div", { className: "muted small", children: data.groups.length ? 'No groups match.' : 'No groups yet. Refresh the playlist first.' })] }), _jsxs("div", { className: "actions end", children: [_jsxs("span", { className: "muted small grow", children: [onCount, " of ", data?.groups.length ?? 0, " groups on \u00B7 ", onStreams.toLocaleString(), " streams"] }), error && _jsx("span", { className: "form-error small", children: error }), _jsx("button", { className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: !enabled || busy, onClick: save, children: busy ? 'Refreshing…' : 'Save and refresh' })] })] }) }));
}
function SourceEditor({ source, onClose, onSaved }) {
    const [s, setS] = useState(source);
    const [error, setError] = useState('');
    const isM3U = s.kind === 'm3u';
    const save = async (e) => {
        e.preventDefault();
        try {
            if (s.id)
                await api.put(`/api/admin/live/sources/${s.id}`, s);
            else
                await api.post('/api/admin/live/sources', s);
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: `${s.id ? 'Edit' : 'Add'} ${isM3U ? 'M3U playlist' : 'guide'}`, onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("label", { children: ["Name", _jsx("input", { value: s.name ?? '', onChange: (e) => setS({ ...s, name: e.target.value }), required: true, placeholder: isM3U ? 'My IPTV provider' : 'Provider guide' })] }), _jsxs("label", { children: ["URL or file path", _jsx("input", { className: "mono", value: s.url ?? '', onChange: (e) => setS({ ...s, url: e.target.value }), required: true, placeholder: isM3U ? 'http://provider/get.php?username=…&type=m3u_plus' : 'http://provider/xmltv.php or .xml.gz' })] }), _jsxs("div", { className: "field-grid", children: [isM3U && (_jsxs("label", { children: ["Connection limit (0 = unlimited)", _jsx("input", { type: "number", min: 0, value: s.maxStreams ?? 0, onChange: (e) => setS({ ...s, maxStreams: Number(e.target.value) }) })] })), _jsxs("label", { children: ["Refresh every (hours)", _jsx("input", { type: "number", min: 1, value: s.refreshHours ?? 24, onChange: (e) => setS({ ...s, refreshHours: Number(e.target.value) }) })] })] }), isM3U && (_jsxs("label", { children: ["User agent (optional)", _jsx("input", { value: s.userAgent ?? '', onChange: (e) => setS({ ...s, userAgent: e.target.value }), placeholder: "Default: VLC" })] })), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: s.enabled ?? true, onChange: (e) => setS({ ...s, enabled: e.target.checked }) }), " Enabled"] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: s.id ? 'Save' : 'Add and import' })] })] }) }));
}
// ---- Channels --------------------------------------------------------------
function Channels() {
    const { data, reload } = useApi('/api/admin/live/channels');
    const [adding, setAdding] = useState(false);
    const [editing, setEditing] = useState(null);
    const [custom, setCustom] = useState(null);
    const [q, setQ] = useState('');
    const nextNumber = Math.floor(Math.max(0, ...(data ?? []).map((c) => c.number))) + 1;
    const toggle = async (c) => {
        await api.put(`/api/admin/live/channels/${c.id}`, { ...c, enabled: !c.enabled, streamIds: null });
        reload();
    };
    const remove = async (c) => {
        if (!confirm(`Delete channel ${channelNo(c.number)} ${c.name}?`))
            return;
        await api.del(`/api/admin/live/channels/${c.id}`);
        reload();
    };
    const shown = data?.filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q));
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "actions", style: { marginBottom: 16 }, children: [_jsxs("button", { className: "btn btn-primary", onClick: () => setAdding(true), children: [_jsx(Icon, { name: "plus", size: 18 }), " Add channels"] }), _jsxs("button", { className: "btn btn-glass", onClick: () => setCustom('new'), children: [_jsx(Icon, { name: "film", size: 18 }), " New custom channel"] }), _jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Find a channel", style: { width: 220 } }), _jsxs("span", { className: "muted small", children: [data?.length ?? 0, " channels"] })] }), _jsxs("div", { className: "panel list", children: [shown?.map((c) => (_jsxs("div", { className: `list-row channel-row ${c.enabled ? '' : 'disabled'}`, children: [_jsx("span", { className: "ch-num big", children: channelNo(c.number) }), _jsx(ChannelLogo, { channel: { name: c.name, logo: c.logoUrl }, size: 30 }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: c.name }), _jsx("div", { className: "muted small", children: c.custom
                                            ? [c.group, 'custom channel from your library'].filter(Boolean).join(' · ')
                                            : [c.group, c.epgId ? `guide: ${c.epgId}` : 'no guide match', `${c.streams?.length ?? 0} stream${c.streams?.length === 1 ? '' : 's'}`]
                                                .filter(Boolean)
                                                .join(' · ') })] }), _jsxs("label", { className: "switch", title: c.enabled ? 'Enabled' : 'Hidden from viewers', children: [_jsx("input", { type: "checkbox", checked: c.enabled, onChange: () => toggle(c) }), _jsx("span", {})] }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => (c.custom ? setCustom(c) : setEditing(c)), children: [_jsx(Icon, { name: "edit", size: 16 }), " Edit"] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(c), "aria-label": "Delete channel", children: _jsx(Icon, { name: "trash", size: 16 }) })] }, c.id))), data?.length === 0 && _jsx("div", { className: "list-row muted", children: "No channels yet. Add an M3U playlist under Sources, then use Add channels." })] }), adding && _jsx(AddChannels, { onClose: () => setAdding(false), onDone: () => { setAdding(false); reload(); }, nextNumber: nextNumber }), custom && (_jsx(CustomChannelEditor, { channel: custom === 'new' ? undefined : custom, nextNumber: nextNumber, onClose: () => setCustom(null), onSaved: reload })), editing && _jsx(ChannelEditor, { channel: editing, onClose: () => setEditing(null), onSaved: () => { setEditing(null); reload(); } })] }));
}
function AddChannels({ onClose, onDone, nextNumber }) {
    const { data: sources } = useApi('/api/admin/live/sources');
    const providers = sources?.filter((s) => s.kind === 'm3u') ?? [];
    const [source, setSource] = useState(0);
    const { data: groups } = useApi(`/api/admin/live/groups?source=${source}`);
    const [group, setGroup] = useState('');
    const [q, setQ] = useState('');
    const { data } = useApi(`/api/admin/live/streams?limit=200&source=${source}&group=${encodeURIComponent(group)}&q=${encodeURIComponent(q)}`);
    const [picked, setPicked] = useState(new Set());
    const [start, setStart] = useState(nextNumber);
    const [error, setError] = useState('');
    const toggle = (id) => setPicked((s) => {
        const n = new Set(s);
        if (n.has(id))
            n.delete(id);
        else
            n.add(id);
        return n;
    });
    const unused = data?.streams.filter((s) => !s.channelIds?.length) ?? [];
    const create = async () => {
        try {
            // Keep the playlist's order so numbering follows it.
            const ids = (data?.streams ?? []).filter((s) => picked.has(s.id)).map((s) => s.id);
            await api.post('/api/admin/live/channels', { streamIds: ids, startNumber: start });
            onDone();
        }
        catch (e) {
            setError(e.message);
        }
    };
    return (_jsx(Modal, { title: "Add channels", onClose: onClose, wide: true, children: _jsxs("div", { className: "form", children: [_jsxs("div", { className: "field-grid", children: [providers.length > 1 && (_jsxs("label", { className: "span-2", children: ["Provider", _jsxs("select", { value: source, onChange: (e) => { setSource(Number(e.target.value)); setGroup(''); }, children: [_jsx("option", { value: 0, children: "All providers" }), providers.map((p) => (_jsxs("option", { value: p.id, children: [p.name, " (", p.entryCount.toLocaleString(), ")"] }, p.id)))] })] })), _jsxs("label", { children: ["Group", _jsxs("select", { value: group, onChange: (e) => setGroup(e.target.value), children: [_jsx("option", { value: "", children: "All groups" }), groups?.map((g) => (_jsxs("option", { value: g.name, children: [g.name || '(no group)', " (", g.count, ")"] }, g.name)))] })] }), _jsxs("label", { children: ["Search", _jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Channel name" })] })] }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setPicked(new Set([...picked, ...unused.map((s) => s.id)])), children: ["Select all ", unused.length, " not yet added"] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setPicked(new Set()), children: "Clear" }), _jsxs("span", { className: "muted small", children: [data?.total ?? 0, " streams", (data?.total ?? 0) > 200 ? ' (showing 200; narrow with a group or search)' : ''] })] }), _jsx("div", { className: "stream-pick", children: data?.streams.map((s) => (_jsxs("label", { className: `stream-row ${s.channelIds?.length ? 'used' : ''}`, children: [_jsx("input", { type: "checkbox", checked: picked.has(s.id), onChange: () => toggle(s.id) }), _jsx(ChannelLogo, { channel: s, size: 22 }), _jsx("span", { className: "grow", children: s.name }), _jsx("span", { className: "muted small", children: providers.length > 1 && !source ? [s.group, s.sourceName].filter(Boolean).join(' · ') : s.group }), s.channelIds?.length ? _jsx("span", { className: "pill", children: "in use" }) : null] }, s.id))) }), _jsxs("div", { className: "actions end", children: [_jsxs("label", { className: "select-label", children: ["Number from", _jsx("input", { type: "number", min: 1, value: start, onChange: (e) => setStart(Number(e.target.value)), style: { width: 90 } })] }), _jsx("span", { className: "muted small grow", children: "Streams with a tvg-chno keep that number when it's free." }), error && _jsx("span", { className: "form-error small", children: error }), _jsx("button", { className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsxs("button", { className: "btn btn-primary", disabled: picked.size === 0, onClick: create, children: ["Create ", picked.size, " channels"] })] })] }) }));
}
function ChannelEditor({ channel, onClose, onSaved }) {
    const [c, setC] = useState(channel);
    const [streams, setStreams] = useState(channel.streams ?? []);
    const [epgQ, setEpgQ] = useState('');
    const [streamQ, setStreamQ] = useState('');
    const { data: epg } = useApi(epgQ ? `/api/admin/live/epg-channels?q=${encodeURIComponent(epgQ)}` : null);
    const { data: found } = useApi(streamQ ? `/api/admin/live/streams?limit=20&q=${encodeURIComponent(streamQ)}` : null);
    const [error, setError] = useState('');
    const move = (i, dir) => {
        const n = [...streams];
        const j = i + dir;
        if (j < 0 || j >= n.length)
            return;
        [n[i], n[j]] = [n[j], n[i]];
        setStreams(n);
    };
    const save = async (e) => {
        e.preventDefault();
        try {
            await api.put(`/api/admin/live/channels/${c.id}`, { ...c, streamIds: streams.map((s) => s.streamId) });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: `Edit ${channel.name}`, onClose: onClose, wide: true, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Number", _jsx("input", { type: "number", step: "0.1", min: 0.1, value: c.number, onChange: (e) => setC({ ...c, number: Number(e.target.value) }), required: true })] }), _jsxs("label", { children: ["Name", _jsx("input", { value: c.name, onChange: (e) => setC({ ...c, name: e.target.value }), required: true })] }), _jsxs("label", { children: ["Category", _jsx("input", { value: c.group ?? '', onChange: (e) => setC({ ...c, group: e.target.value }), placeholder: "News, Sports\u2026" })] })] }), _jsx("div", { className: "label", children: "Logo" }), _jsx(LogoField, { value: c.logo ?? '', onChange: (logo) => setC({ ...c, logo }) }), _jsx("div", { className: "label", children: "Guide match" }), _jsxs("div", { className: "actions", children: [_jsx("span", { className: "chip strong", children: c.epgId || 'none' }), _jsx("input", { value: epgQ, onChange: (e) => setEpgQ(e.target.value), placeholder: "Search guide channels", style: { width: 240 } }), c.epgId && _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setC({ ...c, epgId: '' }), children: "Clear" })] }), epgQ && (_jsxs("div", { className: "pick-list", children: [epg?.map((g) => (_jsxs("button", { type: "button", onClick: () => { setC({ ...c, epgId: g.id }); setEpgQ(''); }, children: [g.displayName, " ", _jsx("span", { className: "muted small mono", children: g.id })] }, g.id))), epg?.length === 0 && _jsx("div", { className: "muted small menu-pad", children: "No matches" })] })), _jsx("div", { className: "label", children: "Streams, in fallback order" }), _jsx("p", { className: "muted small", style: { margin: 0 }, children: "The first stream plays; if it fails or stalls, the next one takes over without viewers reconnecting." }), _jsxs("div", { className: "panel list compact", children: [streams.map((s, i) => (_jsxs("div", { className: "list-row", children: [_jsx("span", { className: "muted", children: i + 1 }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: s.name }), _jsx("div", { className: "muted small", children: s.sourceName })] }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => move(i, -1), disabled: i === 0, "aria-label": "Move up", children: _jsx(Icon, { name: "arrowUp", size: 16 }) }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => move(i, 1), disabled: i === streams.length - 1, "aria-label": "Move down", children: _jsx(Icon, { name: "arrowDown", size: 16 }) }), _jsx("button", { type: "button", className: "icon-btn danger", onClick: () => setStreams(streams.filter((x) => x !== s)), "aria-label": "Remove stream", children: _jsx(Icon, { name: "x", size: 16 }) })] }, s.streamId))), streams.length === 0 && _jsx("div", { className: "list-row muted", children: "No streams: viewers can't watch this channel." })] }), _jsx("input", { value: streamQ, onChange: (e) => setStreamQ(e.target.value), placeholder: "Add a fallback stream: search all playlists" }), streamQ && (_jsx("div", { className: "pick-list", children: found?.streams
                        .filter((s) => !streams.some((x) => x.streamId === s.id))
                        .map((s) => (_jsxs("button", { type: "button", onClick: () => { setStreams([...streams, { streamId: s.id, name: s.name, sourceId: s.sourceId, sourceName: s.sourceName }]); setStreamQ(''); }, children: [s.name, " ", _jsxs("span", { className: "muted small", children: [s.sourceName, " \u00B7 ", s.group] })] }, s.id))) })), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Save" })] })] }) }));
}
// ---- Activity --------------------------------------------------------------
const mbps = (bps) => `${(bps / 1e6).toFixed(2)} Mbps`;
function Activity() {
    const { data, reload } = useApi('/api/admin/live/activity', 3000);
    const [, tick] = useState(0);
    useEffect(() => {
        const t = window.setInterval(() => tick((n) => n + 1), 1000);
        return () => window.clearInterval(t);
    }, []);
    const now = Date.now() / 1000;
    const stop = async (r) => {
        await api.del(`/api/admin/live/activity/${r.channelId}`);
        reload();
    };
    const kick = async (id) => {
        await api.del(`/api/admin/live/viewers/${id}`);
        reload();
    };
    const logs = (_jsxs("p", { className: "muted small", children: ["Starts, stream switches and errors are in the ", _jsx(Link, { to: "/admin/logs?only=live", children: "Live TV logs" }), "."] }));
    if (data && data.length === 0)
        return _jsxs(_Fragment, { children: [logs, _jsx("div", { className: "empty", children: "No live channels are being watched right now." })] });
    return (_jsxs(_Fragment, { children: [logs, _jsx("div", { className: "relay-grid", children: data?.map((r) => (_jsxs("div", { className: "panel relay-card", children: [_jsxs("div", { className: "relay-head", children: [_jsx(ChannelLogo, { channel: r, size: 44 }), _jsxs("div", { className: "grow", children: [_jsxs("div", { className: "relay-name", children: [_jsx("span", { className: "ch-num", children: channelNo(r.number) }), " ", r.name] }), r.now && _jsxs("div", { className: "np-state", children: [_jsx(Icon, { name: "activity", size: 14 }), " Now playing: ", r.now.title] })] }), _jsx("span", { className: "muted small", children: formatClock(now - r.started) }), _jsx("button", { className: "icon-btn danger", onClick: () => stop(r), title: "Stop this channel for everyone", children: _jsx(Icon, { name: "x", size: 18 }) })] }), _jsxs("dl", { className: "np-facts", children: [_jsx("dt", { children: "Stream" }), _jsxs("dd", { children: [r.stream, " ", _jsxs("span", { className: "muted small", children: ["(", r.source, ")"] }), r.failovers > 0 && _jsxs("span", { className: "pill warn", children: [r.failovers, " fail-over", r.failovers > 1 ? 's' : ''] })] }), r.video && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Web video" }), _jsxs("dd", { children: [_jsx("span", { className: `pill ${r.video === 'copy' ? 'ok' : 'warn'}`, children: r.video === 'copy' ? 'Passed through' : `Converted · ${encoderName(r.encoder)}` }), r.video === 'copy' && _jsx("span", { className: "muted small", children: " (already H.264, so no hardware encoder is needed)" })] })] })), r.hwError && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Hardware encoder" }), _jsxs("dd", { className: "muted small", children: ["Failed, so this channel is converted on the CPU: ", r.hwError] })] })), !!r.behind && r.behind >= 2 && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Pace" }), _jsx("dd", { children: _jsxs("span", { className: "pill warn", children: [Math.round(r.behind), " s behind real time"] }) })] })), r.lastError && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Last error" }), _jsx("dd", { className: "muted small", children: r.lastError })] }))] }), _jsxs("div", { className: "relay-stats", children: [_jsxs("span", { children: [_jsx(Icon, { name: "activity", size: 14 }), " ", mbps(r.currentBps)] }), _jsxs("span", { children: ["Avg ", mbps(r.avgBps)] }), _jsxs("span", { children: [_jsx(Icon, { name: "download", size: 14 }), " ", formatBytes(r.bytes)] }), _jsxs("span", { children: [_jsx(Icon, { name: "users", size: 14 }), " ", r.viewers.length] })] }), _jsxs("table", { className: "viewer-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "IP address" }), _jsx("th", { children: "User" }), _jsx("th", { children: "Player" }), _jsx("th", { children: "Connected" }), _jsx("th", {})] }) }), _jsx("tbody", { children: r.viewers.map((v) => (_jsxs("tr", { children: [_jsx("td", { className: "mono", children: v.ip }), _jsx("td", { children: v.username }), _jsx("td", { children: v.kind === 'hls' ? 'Web' : 'App (TS)' }), _jsx("td", { children: formatClock(now - v.connected) }), _jsx("td", { children: _jsx("button", { className: "icon-btn danger", onClick: () => kick(v.id), "aria-label": "Disconnect viewer", children: _jsx(Icon, { name: "x", size: 14 }) }) })] }, v.id))) })] })] }, r.channelId))) })] }));
}
function encoderName(enc) {
    const names = { none: 'CPU', nvenc: 'NVIDIA NVENC', qsv: 'Intel Quick Sync', vaapi: 'VAAPI', amf: 'AMD AMF', videotoolbox: 'VideoToolbox' };
    return names[enc ?? ''] ?? 'CPU';
}
