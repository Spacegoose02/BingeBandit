import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import { Icon } from './Icon';
/** "Add to playlist" button with a dropdown of the user's playlists. */
export function AddToPlaylist({ item }) {
    const [open, setOpen] = useState(false);
    const [name, setName] = useState('');
    const [note, setNote] = useState('');
    const { data: lists, reload } = useApi(open ? '/api/playlists' : null);
    const ref = useRef(null);
    useEffect(() => {
        if (!open)
            return;
        const close = (e) => !ref.current?.contains(e.target) && setOpen(false);
        document.addEventListener('mousedown', close);
        return () => document.removeEventListener('mousedown', close);
    }, [open]);
    useEffect(() => {
        if (!note)
            return;
        const t = window.setTimeout(() => setNote(''), 2500);
        return () => window.clearTimeout(t);
    }, [note]);
    const what = item.kind === 'show' ? 'all episodes' : item.kind === 'season' ? 'the season' : 'it';
    const add = async (p) => {
        const res = await api.post(`/api/playlists/${p.id}/items`, { itemIds: [item.id] });
        setNote(res.added ? `Added ${what} to ${p.name}` : `Already in ${p.name}`);
        setOpen(false);
    };
    const create = async () => {
        if (!name.trim())
            return;
        const p = await api.post('/api/playlists', { name, itemIds: [item.id] });
        setNote(`Created ${p.name}`);
        setName('');
        setOpen(false);
        reload();
    };
    return (_jsxs("div", { className: "menu-anchor", ref: ref, children: [_jsx("button", { className: "btn btn-glass icon-only", onClick: () => setOpen((o) => !o), title: "Add to playlist", children: _jsx(Icon, { name: "plus", size: 18 }) }), note && _jsx("div", { className: "inline-note", children: note }), open && (_jsxs("div", { className: "menu dropdown", children: [_jsx("div", { className: "menu-section", children: "Add to playlist" }), lists?.map((p) => (_jsxs("button", { onClick: () => add(p), children: [_jsx(Icon, { name: "list", size: 16 }), " ", p.name, _jsx("span", { className: "muted small push", children: p.itemCount })] }, p.id))), lists?.length === 0 && _jsx("div", { className: "muted small menu-pad", children: "No playlists yet" }), _jsxs("form", { className: "menu-create", onSubmit: (e) => {
                            e.preventDefault();
                            create();
                        }, children: [_jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: "New playlist name" }), _jsx("button", { className: "btn btn-primary btn-sm", disabled: !name.trim(), children: "Create" })] })] }))] }));
}
/** Toggle for adding a movie or show to the watchlist. */
export function WatchlistButton({ item, onChange }) {
    const toggle = async () => {
        await api[item.onWatchlist ? 'del' : 'post'](`/api/watchlist/${item.id}`);
        onChange();
    };
    return (_jsx("button", { className: "btn btn-glass icon-only", onClick: toggle, title: item.onWatchlist ? 'Remove from watchlist' : 'Add to watchlist', children: _jsx(Icon, { name: "bookmark", size: 18, fill: item.onWatchlist, className: item.onWatchlist ? 'accent' : '' }) }));
}
