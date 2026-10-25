import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api, img } from '../api';
import { PosterCard } from '../components/Cards';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { episodeLabel, formatRuntime } from '../format';
export function WatchlistPage() {
    useDocumentTitle('Watchlist');
    const { data } = useApi('/api/watchlist');
    return (_jsxs("div", { className: "page", children: [_jsx("header", { className: "page-head", children: _jsxs("div", { children: [_jsx("h1", { children: "Watchlist" }), _jsx("div", { className: "muted", children: "Movies and shows you want to watch. Finished movies leave the list on their own." })] }) }), data?.length === 0 && (_jsxs("div", { className: "empty", children: ["Nothing saved yet. Use the ", _jsx(Icon, { name: "bookmark", size: 14 }), " button on any movie or show."] })), _jsx("div", { className: "poster-grid", children: data?.map((it) => (_jsx(PosterCard, { item: it }, it.id))) })] }));
}
export function PlaylistsPage() {
    useDocumentTitle('Playlists');
    const { data, reload } = useApi('/api/playlists');
    const [name, setName] = useState('');
    const create = async () => {
        if (!name.trim())
            return;
        await api.post('/api/playlists', { name });
        setName('');
        reload();
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Playlists" }), _jsx("div", { className: "muted", children: "Line up movies and episodes to watch back to back." })] }), _jsxs("form", { className: "actions", onSubmit: (e) => {
                            e.preventDefault();
                            create();
                        }, children: [_jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: "New playlist name", style: { width: 220 } }), _jsxs("button", { className: "btn btn-primary", disabled: !name.trim(), children: [_jsx(Icon, { name: "plus", size: 18 }), " Create"] })] })] }), data?.length === 0 && _jsx("div", { className: "empty", children: "No playlists yet. Create one here, or use the + button on any movie, show or episode." }), _jsx("div", { className: "poster-grid", children: data?.map((p) => (_jsxs(Link, { to: `/playlist/${p.id}`, className: "poster-card", children: [_jsxs("div", { className: "poster-frame collection-frame", children: [p.poster ? _jsx("img", { src: img(p.poster, 'w342'), alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: _jsx(Icon, { name: "list", size: 36 }) }), _jsx("span", { className: "badge-count", children: p.itemCount })] }), _jsx("div", { className: "card-title", children: p.name }), _jsxs("div", { className: "card-sub", children: [p.itemCount, " items \u00B7 ", formatRuntime(Math.round(p.duration / 60))] })] }, p.id))) })] }));
}
export function PlaylistPage() {
    const { id } = useParams();
    const nav = useNavigate();
    const { data, error, reload, setData } = useApi(`/api/playlists/${id}`);
    useDocumentTitle(data?.playlist.name);
    if (error)
        return _jsx("div", { className: "page", children: _jsx("div", { className: "empty", children: error }) });
    if (!data)
        return _jsx("div", { className: "page-loading" });
    const { playlist, items } = data;
    const move = async (index, dir) => {
        const order = items.map((i) => i.id);
        const j = index + dir;
        if (j < 0 || j >= order.length)
            return;
        [order[index], order[j]] = [order[j], order[index]];
        setData({ playlist, items: order.map((oid) => items.find((i) => i.id === oid)) });
        await api.put(`/api/playlists/${playlist.id}/order`, { itemIds: order });
    };
    const remove = async (itemId) => {
        await api.del(`/api/playlists/${playlist.id}/items/${itemId}`);
        reload();
    };
    const rename = async () => {
        const name = prompt('Rename playlist', playlist.name);
        if (!name?.trim())
            return;
        await api.put(`/api/playlists/${playlist.id}`, { name });
        reload();
    };
    const destroy = async () => {
        if (!confirm(`Delete the playlist "${playlist.name}"?`))
            return;
        await api.del(`/api/playlists/${playlist.id}`);
        nav('/playlists', { replace: true });
    };
    const playFrom = (it) => nav(`/play/${it.id}?playlist=${playlist.id}`);
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("div", { className: "muted small", children: "Playlist" }), _jsx("h1", { children: playlist.name }), _jsxs("div", { className: "muted", children: [items.length, " items \u00B7 ", formatRuntime(Math.round(playlist.duration / 60))] })] }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-primary", disabled: !items.length, onClick: () => playFrom(items[0]), children: [_jsx(Icon, { name: "play", fill: true, size: 18 }), " Play all"] }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: rename, children: [_jsx(Icon, { name: "edit", size: 16 }), " Rename"] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: destroy, "aria-label": "Delete playlist", children: _jsx(Icon, { name: "trash", size: 16 }) })] })] }), items.length === 0 && _jsx("div", { className: "empty", children: "This playlist is empty. Add things with the + button on any item page." }), _jsx("div", { className: "panel list", children: items.map((it, i) => {
                    const art = img(it.kind === 'episode' ? it.backdrop || it.showBackdrop : it.poster, 'w342');
                    return (_jsxs("div", { className: "list-row playlist-row", children: [_jsx("span", { className: "muted pl-num", children: i + 1 }), _jsxs("button", { className: `pl-art ${it.kind === 'episode' ? 'wide' : ''}`, onClick: () => playFrom(it), "aria-label": `Play ${it.title}`, children: [art ? _jsx("img", { src: art, alt: "", loading: "lazy" }) : _jsx(Icon, { name: "film" }), _jsx("span", { className: "pl-play", children: _jsx(Icon, { name: "play", fill: true, size: 20 }) })] }), _jsxs("div", { className: "grow", children: [_jsx(Link, { to: `/item/${it.id}`, children: it.kind === 'episode' ? it.showTitle : it.title }), _jsxs("div", { className: "muted small", children: [it.kind === 'episode' ? `${episodeLabel(it)} · ${it.title}` : it.year, it.watch.watched && ' · watched'] })] }), _jsx("button", { className: "icon-btn", onClick: () => move(i, -1), disabled: i === 0, "aria-label": "Move up", children: _jsx(Icon, { name: "arrowUp", size: 16 }) }), _jsx("button", { className: "icon-btn", onClick: () => move(i, 1), disabled: i === items.length - 1, "aria-label": "Move down", children: _jsx(Icon, { name: "arrowDown", size: 16 }) }), _jsx("button", { className: "icon-btn danger", onClick: () => remove(it.id), "aria-label": "Remove from playlist", children: _jsx(Icon, { name: "x", size: 16 }) })] }, it.id));
                }) })] }));
}
