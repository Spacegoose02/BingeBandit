import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useState } from 'react';
import { ColumnChart, RankedBars, StatTile } from '../../components/Charts';
import { useApi, useDocumentTitle } from '../../hooks';
import { HistoryList, hours } from '../Profile';
const ranges = [
    [7, '7 days'],
    [30, '30 days'],
    [90, '90 days'],
    [365, '12 months'],
];
const dayLabel = (key, opts) => new Date(`${key}T00:00:00`).toLocaleDateString(undefined, opts);
export default function AdminStatistics() {
    useDocumentTitle('Statistics');
    const [days, setDays] = useState(30);
    const [page, setPage] = useState(0);
    const { data } = useApi(`/api/admin/statistics?days=${days}`);
    const { data: history } = useApi(`/api/admin/history?limit=25&offset=${page * 25}`);
    const plays = (rows) => rows.map((r) => ({ label: r.key, value: r.plays, display: `${r.plays} plays` }));
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Statistics" }), _jsx("div", { className: "muted", children: "What's being watched on this server and how it's streamed." })] }), _jsx("div", { className: "segmented", children: ranges.map(([d, label]) => (_jsx("button", { className: d === days ? 'active' : '', onClick: () => setDays(d), children: label }, d))) })] }), data && (_jsxs(_Fragment, { children: [_jsxs("div", { className: "stat-grid", children: [_jsx(StatTile, { label: "Plays", value: data.totalPlays.toLocaleString() }), _jsx(StatTile, { label: "Hours streamed", value: hours(data.totalHours) }), _jsx(StatTile, { label: "Active viewers", value: data.topUsers.length })] }), _jsx("h2", { className: "section-title", children: "Plays per day" }), _jsx("div", { className: "panel", children: _jsx(ColumnChart, { caption: "Day", valueLabel: "Plays", labelEvery: days <= 7 ? 1 : days <= 31 ? 5 : days <= 90 ? 14 : 60, data: data.daily.map((d) => ({
                                label: dayLabel(d.key, days <= 7 ? { weekday: 'short' } : { month: 'short', day: 'numeric' }),
                                value: d.plays,
                                tip: (_jsxs(_Fragment, { children: [_jsx("strong", { children: dayLabel(d.key, { weekday: 'long', month: 'short', day: 'numeric' }) }), _jsxs("div", { children: [d.plays, " plays \u00B7 ", hours(d.hours), " hours"] })] })),
                            })) }) }), _jsxs("div", { className: "two-col", children: [_jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Top movies" }), _jsx("div", { className: "panel", children: _jsx(RankedBars, { rows: data.topMovies.map((r) => ({ ...plays([r])[0], href: r.itemId ? `/item/${r.itemId}` : undefined, poster: r.poster ?? '' })) }) })] }), _jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Top shows" }), _jsx("div", { className: "panel", children: _jsx(RankedBars, { rows: data.topShows.map((r) => ({ ...plays([r])[0], href: `/item/${r.itemId}`, poster: r.poster ?? '' })) }) })] }), _jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Top viewers" }), _jsx("div", { className: "panel", children: _jsx(RankedBars, { rows: data.topUsers.map((r) => ({ label: r.key, value: r.hours, display: `${hours(r.hours)} h · ${r.plays} plays`, href: `/profile/${encodeURIComponent(r.key)}` })) }) })] }), _jsxs("section", { children: [_jsx("h2", { className: "section-title", children: "Streaming" }), _jsxs("div", { className: "panel", children: [_jsx(RankedBars, { rows: plays(data.modes) }), _jsx("div", { className: "label", style: { margin: '18px 0 10px' }, children: "Clients" }), _jsx(RankedBars, { rows: plays(data.clients) })] })] })] })] })), _jsx("h2", { className: "section-title", children: "Watch history" }), history && _jsx(HistoryList, { plays: history, showUser: true }), _jsxs("div", { className: "actions", style: { marginTop: 12 }, children: [_jsx("button", { className: "btn btn-glass btn-sm", disabled: page === 0, onClick: () => setPage(page - 1), children: "Newer" }), _jsx("button", { className: "btn btn-glass btn-sm", disabled: (history?.length ?? 0) < 25, onClick: () => setPage(page + 1), children: "Older" })] })] }));
}
