import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../api';
import { useAuth } from '../auth';
import { Icon } from '../components/Icon';
import { Modal } from '../components/Modal';
import { Row } from '../components/Row';
import { useApi, useDocumentTitle } from '../hooks';
const ago = (t) => {
    const days = Math.floor((Date.now() / 1000 - t) / 86400);
    if (days < 1)
        return 'Today';
    if (days === 1)
        return 'Yesterday';
    if (days < 30)
        return `${days} days ago`;
    return new Date(t * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
};
/** New trailers and shows from YouTube playlists and channels the admin picked. */
export function DiscoverPage() {
    useDocumentTitle('Discover');
    const { user } = useAuth();
    const { data, reload } = useApi('/api/discover');
    const [playing, setPlaying] = useState(null);
    const [managing, setManaging] = useState(false);
    return (_jsxs("div", { className: "discover", children: [_jsxs("header", { className: "page-head discover-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Discover" }), _jsx("div", { className: "muted", children: "New trailers and shows from YouTube." })] }), user?.isAdmin && (_jsxs("button", { className: "btn btn-glass", onClick: () => setManaging(true), children: [_jsx(Icon, { name: "settings", size: 18 }), " Manage"] }))] }), data?.length === 0 && (_jsx("div", { className: "empty discover-empty", children: user?.isAdmin
                    ? 'Add a YouTube playlist or channel (a trailer channel, a "new on Netflix" playlist…) with Manage, and its newest videos show up here.'
                    : 'Nothing here yet. Your server admin can add YouTube playlists and channels.' })), _jsx("div", { className: "rows", children: data?.map((row) => (_jsx(Row, { title: row.name, children: row.videos.map((v) => (_jsxs("div", { className: "wide-card", children: [_jsxs("button", { className: "wide-frame video-card", onClick: () => setPlaying(v), title: v.description || v.title, children: [_jsx("img", { src: `https://i.ytimg.com/vi/${v.id}/mqdefault.jpg`, alt: "", loading: "lazy" }), _jsx("div", { className: "poster-hover", children: _jsx(Icon, { name: "play", fill: true, size: 34 }) })] }), _jsx("div", { className: "card-title", title: v.title, children: v.title }), _jsx("div", { className: "card-sub", children: ago(v.publishedAt) })] }, v.id))) }, row.id))) }), playing && (_jsxs(Modal, { title: playing.title, onClose: () => setPlaying(null), wide: true, children: [_jsx("div", { className: "video-embed", children: _jsx("iframe", { src: `https://www.youtube-nocookie.com/embed/${playing.id}?autoplay=1&rel=0`, title: playing.title, allow: "autoplay; encrypted-media; picture-in-picture; fullscreen", allowFullScreen: true }) }), playing.description && _jsx("p", { className: "muted small discover-desc", children: playing.description })] })), managing && (_jsx(ManageSources, { onClose: () => {
                    setManaging(false);
                    reload();
                } }))] }));
}
function ManageSources({ onClose }) {
    const { data, reload, setData } = useApi('/api/admin/discover');
    const [url, setUrl] = useState('');
    const [name, setName] = useState('');
    const [busy, setBusy] = useState(null);
    const [error, setError] = useState('');
    const add = async (e) => {
        e.preventDefault();
        setBusy('add');
        setError('');
        try {
            await api.post('/api/admin/discover', { url, name });
            setUrl('');
            setName('');
            reload();
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(null);
        }
    };
    const refresh = async () => {
        setBusy('refresh');
        setError('');
        try {
            setData(await api.post('/api/admin/discover/refresh'));
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(null);
        }
    };
    const move = async (i, dir) => {
        if (!data)
            return;
        const next = [...data];
        [next[i], next[i + dir]] = [next[i + dir], next[i]];
        setData(next);
        await api.put('/api/admin/discover/order', { ids: next.map((s) => s.id) }).catch((e) => setError(e.message));
    };
    const rename = async (s) => {
        const n = prompt('Name', s.name)?.trim();
        if (!n || n === s.name)
            return;
        await api.put(`/api/admin/discover/${s.id}`, { name: n }).catch((e) => setError(e.message));
        reload();
    };
    const remove = async (s) => {
        if (!confirm(`Remove "${s.name}" from Discover?`))
            return;
        await api.del(`/api/admin/discover/${s.id}`).catch((e) => setError(e.message));
        reload();
    };
    return (_jsxs(Modal, { title: "Discover sources", onClose: onClose, wide: true, children: [_jsxs("form", { className: "form", onSubmit: add, children: [_jsxs("label", { children: ["YouTube playlist or channel", _jsx("input", { value: url, onChange: (e) => setUrl(e.target.value), placeholder: "https://www.youtube.com/playlist?list=\u2026 or https://www.youtube.com/@channel", required: true })] }), _jsxs("label", { children: ["Row name (optional)", _jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: "Uses the playlist or channel name" })] }), _jsx("div", { className: "actions", children: _jsxs("button", { className: "btn btn-primary", disabled: busy !== null || !url.trim(), children: [_jsx(Icon, { name: "plus", size: 16 }), " ", busy === 'add' ? 'Checking…' : 'Add'] }) })] }), error && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "discover-sources", children: data?.map((s, i) => (_jsxs("div", { className: "discover-source", children: [_jsx(Icon, { name: s.kind === 'channel' ? 'user' : 'list', size: 18 }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: s.name }), _jsx("div", { className: `small ${s.error ? 'form-error' : 'muted'}`, children: s.error ? `Last refresh failed: ${s.error}` : `${s.kind === 'channel' ? 'Channel' : 'Playlist'} · updated ${s.fetchedAt ? ago(s.fetchedAt).toLowerCase() : 'never'}` })] }), _jsx("button", { className: "icon-btn", disabled: i === 0, onClick: () => move(i, -1), "aria-label": "Move up", children: _jsx(Icon, { name: "arrowUp", size: 16 }) }), _jsx("button", { className: "icon-btn", disabled: i === data.length - 1, onClick: () => move(i, 1), "aria-label": "Move down", children: _jsx(Icon, { name: "arrowDown", size: 16 }) }), _jsx("button", { className: "icon-btn", onClick: () => rename(s), "aria-label": "Rename", children: _jsx(Icon, { name: "edit", size: 16 }) }), _jsx("button", { className: "icon-btn", onClick: () => remove(s), "aria-label": "Remove", children: _jsx(Icon, { name: "trash", size: 16 }) })] }, s.id))) }), _jsx("p", { className: "muted small", children: "Each one shows its 15 newest videos (YouTube's feed limit). Older ones stay until there are 50. The \u201CRefresh Discover\u201D task checks every few hours." }), _jsxs("div", { className: "actions end", children: [!!data?.length && (_jsxs("button", { className: "btn btn-glass", onClick: refresh, disabled: busy !== null, children: [_jsx(Icon, { name: "refresh", size: 16, className: busy === 'refresh' ? 'spin' : '' }), " Refresh now"] })), _jsx("button", { className: "btn btn-primary", onClick: onClose, children: "Done" })] })] }));
}
