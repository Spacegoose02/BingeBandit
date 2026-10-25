import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAuth } from '../auth';
import { PosterCard, WideCard } from '../components/Cards';
import { Hero } from '../components/Hero';
import { Icon } from '../components/Icon';
import { pinsChanged } from '../components/Pins';
import { Row } from '../components/Row';
import { useApi, useDocumentTitle } from '../hooks';
import { api, img } from '../api';
/** What people who chose to share are watching right now. */
function FriendsRow() {
    const { data } = useApi('/api/activity/friends', 30000);
    if (!data?.length)
        return null;
    return (_jsx(Row, { title: "Friends are watching", children: data.map((f) => {
            const poster = img(f.poster, 'w342');
            return (_jsxs(Link, { to: `/item/${f.itemId}`, className: "poster-card friend-card", children: [_jsxs("div", { className: "poster-frame", children: [poster ? _jsx("img", { src: poster, alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: f.title }), _jsxs("span", { className: "friend-badge", children: [_jsx("span", { className: "avatar", children: f.username.slice(0, 1).toUpperCase() }), f.username] }), f.duration > 0 && (_jsx("div", { className: "progress", children: _jsx("div", { style: { width: `${Math.min(100, (f.position / f.duration) * 100)}%` } }) }))] }), _jsx("div", { className: "card-title", title: f.title, children: f.title }), _jsx("div", { className: "card-sub", children: f.paused ? 'Paused' : f.subtitle || 'Watching now' })] }, f.username));
        }) }));
}
/** Titles people recommended to you, with who did; each can be dismissed. */
function RecommendedRow({ title, items, by }) {
    const [hidden, setHidden] = useState([]);
    const shown = items.filter((it) => !hidden.includes(it.id));
    if (!shown.length)
        return null;
    const dismiss = (id) => {
        setHidden((h) => [...h, id]);
        api.del(`/api/recommendations/${id}`).catch(() => undefined);
    };
    return (_jsx(Row, { title: title, children: shown.map((it) => {
            const rec = by[it.id];
            const from = rec ? `From ${rec.from.join(', ')}` : undefined;
            return (_jsxs("div", { className: "recommended-card", title: rec?.notes?.join('\n'), children: [_jsx(PosterCard, { item: it, subtitle: from }), !!rec?.notes?.length && _jsxs("div", { className: "rec-note", children: ["\u201C", rec.notes[0].split(': ').slice(1).join(': '), "\u201D"] }), _jsx("button", { className: "rec-dismiss", onClick: () => dismiss(it.id), title: "Not interested", children: _jsx(Icon, { name: "x", size: 14 }) })] }, it.id));
        }) }));
}
export default function Home() {
    useDocumentTitle('Home');
    const { user } = useAuth();
    const { data, loading, error, reload } = useApi('/api/home');
    useEffect(() => {
        window.addEventListener(pinsChanged, reload);
        return () => window.removeEventListener(pinsChanged, reload);
    }, [reload]);
    if (loading)
        return _jsx("div", { className: "page-loading" });
    if (error)
        return _jsx("div", { className: "page", children: _jsxs("div", { className: "empty", children: ["Could not load home: ", error] }) });
    if (!data)
        return null;
    const empty = data.rows.length === 0 && data.continueWatching.length === 0;
    if (empty) {
        return (_jsx("div", { className: "page", children: _jsxs("div", { className: "empty-hero", children: [_jsx("img", { src: "/brand/mascot.png", alt: "" }), _jsx("h1", { children: "Nothing to binge yet" }), user?.isAdmin ? (_jsxs(_Fragment, { children: [_jsx("p", { children: "Add a library folder and BingeBandit will scan it and pull artwork, ratings and cast from TMDB." }), _jsxs(Link, { to: "/admin/libraries", className: "btn btn-primary", children: [_jsx(Icon, { name: "plus", size: 18 }), " Add a library"] })] })) : (_jsx("p", { children: "Your server admin has not added any libraries yet." }))] }) }));
    }
    return (_jsxs("div", { className: "home", children: [_jsx(Hero, { items: data.hero }), _jsxs("div", { className: "rows", children: [data.continueWatching.length > 0 && (_jsx(Row, { title: "Continue Watching", children: data.continueWatching.map((it) => (_jsx(WideCard, { item: it }, it.id))) })), _jsx(FriendsRow, {}), data.rows.map((row) => row.id === 'recommended' ? (_jsx(RecommendedRow, { title: row.title, items: row.items, by: data.recommendedBy ?? {} }, row.id)) : (_jsx(Row, { title: row.title, action: row.collectionId ? _jsx(Link, { to: `/collection/${row.collectionId}`, className: "muted small", children: "See all" }) : undefined, children: row.items.map((it) => (_jsx(PosterCard, { item: it }, it.id))) }, row.id)))] })] }));
}
