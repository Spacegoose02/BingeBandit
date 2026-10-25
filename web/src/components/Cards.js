import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, img } from '../api';
import { episodeLabel, progressFraction, smartRuleText } from '../format';
import { Icon } from './Icon';
function Badge({ item }) {
    if (item.kind === 'show' || item.kind === 'season') {
        if (item.childCount && item.unwatchedCount === 0)
            return _jsx("span", { className: "badge-watched", children: _jsx(Icon, { name: "check", size: 14 }) });
        if (item.unwatchedCount)
            return _jsx("span", { className: "badge-count", children: item.unwatchedCount });
        return null;
    }
    if (item.watch.watched)
        return _jsx("span", { className: "badge-watched", children: _jsx(Icon, { name: "check", size: 14 }) });
    return null;
}
function Progress({ item }) {
    const f = progressFraction(item);
    if (!f)
        return null;
    return (_jsx("div", { className: "progress", children: _jsx("div", { style: { width: `${f * 100}%` } }) }));
}
export function PosterCard({ item, subtitle }) {
    const poster = img(item.poster || item.showPoster, 'w342');
    return (_jsxs(Link, { to: `/item/${item.id}`, className: "poster-card", children: [_jsxs("div", { className: "poster-frame", children: [poster ? _jsx("img", { src: poster, alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: item.title }), _jsx(Badge, { item: item }), _jsx(Progress, { item: item }), _jsx("div", { className: "poster-hover", children: _jsx(Icon, { name: "play", fill: true, size: 34 }) })] }), _jsx("div", { className: "card-title", title: item.title, children: item.title }), _jsx("div", { className: "card-sub", children: subtitle ?? (item.kind === 'season' ? `${item.childCount ?? 0} episodes` : item.year || '') })] }));
}
/** Landscape card for Continue Watching and episode lists. Clicking plays. */
export function WideCard({ item }) {
    const art = img(item.backdrop || item.showBackdrop, 'w780');
    const isEp = item.kind === 'episode';
    return (_jsxs("div", { className: "wide-card", children: [_jsxs(Link, { to: `/play/${item.id}${item.watch.position > 15 ? `?t=${Math.floor(item.watch.position)}` : ''}`, className: "wide-frame", children: [art ? _jsx("img", { src: art, alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: item.title }), _jsx(Progress, { item: item }), _jsx("div", { className: "poster-hover", children: _jsx(Icon, { name: "play", fill: true, size: 34 }) })] }), _jsx(Link, { to: `/item/${isEp ? item.showId : item.id}`, className: "card-title", children: isEp ? item.showTitle : item.title }), _jsx("div", { className: "card-sub", children: isEp ? `${episodeLabel(item)} · ${item.title}` : item.year || '' })] }));
}
export function CollectionCard({ collection, onChanged }) {
    const poster = img(collection.poster, 'w342');
    const card = (_jsxs(Link, { to: `/collection/${collection.id}`, className: "poster-card", children: [_jsxs("div", { className: "poster-frame collection-frame", children: [poster ? _jsx("img", { src: poster, alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: collection.name }), _jsx("span", { className: "badge-count", children: collection.itemCount }), smartRuleText(collection) && (_jsxs("span", { className: "badge-smart", title: `Smart: ${smartRuleText(collection)}`, children: [_jsx(Icon, { name: "stack", size: 12 }), " Smart"] }))] }), _jsx("div", { className: "card-title", title: collection.name, children: collection.name }), _jsxs("div", { className: "card-sub", children: [collection.itemCount, " ", collection.itemCount === 1 ? 'item' : 'items'] })] }));
    // Admins get a "Visible on" menu, like Plex's collection menu.
    if (!collection.visible)
        return card;
    return (_jsxs("div", { className: "collection-card", children: [card, _jsx(VisibleOnMenu, { collection: collection, onChanged: onChanged })] }));
}
const places = [
    ['library', 'Library'],
    ['home', 'Home'],
    ['friends', "Friends' Home"],
];
function VisibleOnMenu({ collection, onChanged }) {
    const [open, setOpen] = useState(false);
    const [visible, setVisible] = useState(collection.visible);
    const ref = useRef(null);
    useEffect(() => {
        if (!open)
            return;
        const close = (e) => ref.current && !ref.current.contains(e.target) && setOpen(false);
        document.addEventListener('mousedown', close);
        return () => document.removeEventListener('mousedown', close);
    }, [open]);
    const toggle = async (place) => {
        const next = { ...visible, [place]: !visible[place] };
        setVisible(next);
        try {
            await api.put(`/api/admin/collections/${collection.id}/visibility`, next);
            onChanged?.();
        }
        catch {
            setVisible(visible);
        }
    };
    const shown = places.filter(([p]) => visible[p]).map(([, label]) => label);
    return (_jsxs("div", { className: "visible-on", ref: ref, children: [_jsx("button", { className: "visible-on-btn", "aria-label": `Where ${collection.name} shows`, "aria-expanded": open, title: shown.length ? `Visible on ${shown.join(', ')}` : 'Not shown as a row', onClick: () => setOpen((o) => !o), children: _jsx(Icon, { name: "more", size: 18 }) }), open && (_jsxs("div", { className: "menu visible-on-menu", role: "menu", children: [_jsx("div", { className: "menu-section", children: "Visible on" }), places.map(([p, label]) => (_jsx("button", { role: "menuitemcheckbox", "aria-checked": visible[p], className: visible[p] ? 'active' : '', onClick: () => toggle(p), children: label }, p)))] }))] }));
}
export function PersonCard({ name, role, photo }) {
    const src = img(photo, 'w185');
    return (_jsxs("div", { className: "person-card", children: [_jsx("div", { className: "person-photo", children: src ? _jsx("img", { src: src, alt: "", loading: "lazy" }) : _jsx(Icon, { name: "user", size: 36 }) }), _jsx("div", { className: "card-title", title: name, children: name }), _jsx("div", { className: "card-sub", title: role, children: role })] }));
}
