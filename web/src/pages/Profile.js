import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { Link, useParams } from 'react-router-dom';
import { api, img } from '../api';
import { useAuth } from '../auth';
import { ColumnChart, RankedBars, StatTile } from '../components/Charts';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { timeAgo } from '../format';
export const hours = (h) => (h >= 10 ? Math.round(h).toLocaleString() : h.toFixed(1));
const monthName = (key) => new Date(`${key}-01T00:00:00`).toLocaleDateString(undefined, { month: 'short' });
export function playLink(p) {
    return p.itemId ? `/item/${p.itemId}` : undefined;
}
export function HistoryList({ plays, showUser }) {
    if (!plays.length)
        return _jsx("div", { className: "muted small", children: "Nothing watched yet." });
    return (_jsx("div", { className: "panel list", children: plays.map((p) => (_jsxs("div", { className: "list-row history-row", children: [_jsx("span", { className: "history-poster", children: p.poster && _jsx("img", { src: img(p.poster, 'w92'), alt: "", loading: "lazy" }) }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: playLink(p) ? _jsx(Link, { to: playLink(p), children: p.title }) : p.title }), _jsx("div", { className: "muted small", children: [p.subtitle, showUser && p.username, timeAgo(p.startedAt), `${Math.round(p.watched / 60)} min watched`, p.client]
                                .filter(Boolean)
                                .join(' · ') })] }), showUser && p.ip && _jsx("span", { className: "muted small mono", children: p.ip }), _jsx("span", { className: `pill ${p.mode === 'direct' ? 'ok' : 'warn'}`, children: p.mode === 'direct' ? 'Direct' : 'Transcode' }), p.completed && _jsx(Icon, { name: "check", size: 16, className: "accent" })] }, p.id))) }));
}
export default function ProfilePage() {
    const { username: param } = useParams();
    const { user: me } = useAuth();
    const username = param ?? me?.username ?? '';
    const { data, error, reload } = useApi(`/api/profiles/${encodeURIComponent(username)}`);
    useDocumentTitle(data ? `${data.user.username}'s profile` : 'Profile');
    if (error)
        return _jsx("div", { className: "page", children: _jsx("div", { className: "empty", children: "This profile is private or doesn't exist." }) });
    if (!data)
        return _jsx("div", { className: "page-loading" });
    const { user, stats, recent } = data;
    const toggleShare = async () => {
        await api.put('/api/me/profile', { public: !user.profilePublic });
        reload();
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { className: "profile-head", children: [_jsx("span", { className: "avatar big", children: user.username.slice(0, 1).toUpperCase() }), _jsxs("div", { children: [_jsx("h1", { children: user.username }), _jsxs("div", { className: "muted", children: ["Watching since ", new Date(user.createdAt * 1000).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })] })] })] }), user.isMe && (_jsxs("label", { className: "check share-toggle", children: [_jsxs("span", { className: "switch", children: [_jsx("input", { type: "checkbox", checked: user.profilePublic, onChange: toggleShare }), _jsx("span", {})] }), "Share my profile with other people on this server"] }))] }), _jsxs("div", { className: "stat-grid", children: [_jsx(StatTile, { label: "Hours watched", value: hours(stats.hours) }), _jsx(StatTile, { label: "Movies finished", value: stats.movies.toLocaleString() }), _jsx(StatTile, { label: "Episodes finished", value: stats.episodes.toLocaleString() }), _jsx(StatTile, { label: "Shows watched", value: stats.shows.toLocaleString() }), _jsx(StatTile, { label: "Favorite time to watch", value: stats.favoriteHour < 0 ? '—' : new Date(2000, 0, 1, stats.favoriteHour).toLocaleTimeString([], { hour: 'numeric' }) })] }), _jsx("h2", { className: "section-title", children: "Hours watched per month" }), _jsx("div", { className: "panel", children: _jsx(ColumnChart, { caption: "Month", valueLabel: "Hours", data: stats.monthlyHours.map((m) => ({
                        label: monthName(m.key),
                        value: Math.round(m.hours * 100) / 100,
                        tip: (_jsxs(_Fragment, { children: [_jsx("strong", { children: new Date(`${m.key}-01T00:00:00`).toLocaleDateString(undefined, { month: 'long', year: 'numeric' }) }), _jsxs("div", { children: [hours(m.hours), " hours \u00B7 ", m.plays, " plays"] })] })),
                    })) }) }), _jsxs("div", { className: "two-col", children: [_jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Top genres" }), _jsx("div", { className: "panel", children: _jsx(RankedBars, { rows: stats.topGenres.map((g) => ({ label: g.key, value: g.hours, display: `${hours(g.hours)} h` })) }) })] }), _jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Most watched shows" }), _jsx("div", { className: "panel", children: _jsx(RankedBars, { rows: stats.topShows.map((s) => ({ label: s.key, value: s.hours, display: `${hours(s.hours)} h`, href: `/item/${s.itemId}`, poster: s.poster ?? '' })) }) })] })] }), _jsx("h2", { className: "section-title", children: "Recently watched" }), _jsx(HistoryList, { plays: recent })] }));
}
