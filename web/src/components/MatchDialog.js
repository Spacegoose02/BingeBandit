import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api, img } from '../api';
import { Icon } from './Icon';
import { Modal } from './Modal';
const orders = [
    { value: '', label: 'Aired order' },
    { value: 'dvd', label: 'DVD order' },
    { value: 'absolute', label: 'Absolute order (anime)' },
];
/**
 * Search TMDB and re-point a movie or show at the chosen result. A show can
 * use TheTVDB instead, with its aired, DVD or absolute episode numbering.
 */
export function MatchDialog({ item, tvdb, onClose, onMatched, }) {
    const [q, setQ] = useState(item.title);
    const [year, setYear] = useState(item.year ? String(item.year) : '');
    const [results, setResults] = useState(null);
    const [busy, setBusy] = useState(null);
    const [error, setError] = useState('');
    const kind = item.kind === 'show' ? 'show' : 'movie';
    const [provider, setProvider] = useState(tvdb?.id ? 'tvdb' : 'tmdb');
    const [order, setOrder] = useState(tvdb?.order ?? '');
    const idOf = (r) => (provider === 'tvdb' ? r.tvdbId : r.tmdbId) ?? 0;
    const currentId = provider === 'tvdb' ? tvdb?.id : tvdb?.id ? undefined : item.tmdbId;
    const search = async (e, p = provider) => {
        e?.preventDefault();
        setBusy('search');
        setError('');
        try {
            setResults(await api.get(`/api/admin/match?kind=${kind}&provider=${p}&q=${encodeURIComponent(q)}&year=${year}`));
        }
        catch (err) {
            setResults(null);
            setError(err.message);
        }
        finally {
            setBusy(null);
        }
    };
    useEffect(() => {
        search();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    const switchProvider = (p) => {
        setProvider(p);
        setResults(null);
        search(undefined, p);
    };
    const choose = async (r) => {
        setBusy(idOf(r));
        setError('');
        try {
            await api.post(`/api/admin/items/${item.id}/match`, provider === 'tvdb' ? { tvdbId: r.tvdbId, tvdbOrder: order } : { tmdbId: r.tmdbId });
            onMatched();
        }
        catch (err) {
            setError(err.message);
            setBusy(null);
        }
    };
    return (_jsxs(Modal, { title: `Fix match: ${item.title}`, onClose: onClose, wide: true, children: [kind === 'show' && (_jsxs("div", { className: "match-provider", children: [_jsxs("div", { className: "segmented", children: [_jsx("button", { type: "button", className: provider === 'tmdb' ? 'active' : '', onClick: () => switchProvider('tmdb'), children: "TMDB" }), _jsx("button", { type: "button", className: provider === 'tvdb' ? 'active' : '', onClick: () => switchProvider('tvdb'), children: "TheTVDB" })] }), provider === 'tvdb' && (_jsx("select", { value: order, onChange: (e) => setOrder(e.target.value), "aria-label": "Episode order", children: orders.map((o) => (_jsx("option", { value: o.value, children: o.label }, o.value))) }))] })), provider === 'tvdb' && (_jsx("p", { className: "muted small", children: "Titles, episode names and season numbering come from TheTVDB, in the order your files are numbered. TMDB still adds the logo, trailer and rating when it knows the show." })), _jsxs("form", { className: "match-search", onSubmit: search, children: [_jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Title", autoFocus: true }), _jsx("input", { value: year, onChange: (e) => setYear(e.target.value.replace(/\D/g, '').slice(0, 4)), placeholder: "Year", className: "year" }), _jsxs("button", { className: "btn btn-primary", disabled: busy !== null, children: [_jsx(Icon, { name: "search", size: 16 }), " Search"] })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "match-results", children: [results?.length === 0 && _jsx("div", { className: "muted", children: "No results. Try the original title or drop the year." }), results?.map((r) => (_jsxs("button", { className: `match-row ${idOf(r) === currentId ? 'current' : ''}`, onClick: () => choose(r), disabled: busy !== null, children: [_jsx("div", { className: "match-poster", children: r.poster ? _jsx("img", { src: img(r.poster, 'w92'), alt: "" }) : _jsx(Icon, { name: "image" }) }), _jsxs("div", { className: "grow", children: [_jsxs("div", { className: "match-title", children: [r.title, " ", r.year ? _jsxs("span", { className: "muted", children: ["(", r.year, ")"] }) : null, idOf(r) === currentId && _jsx("span", { className: "pill ok", children: "Current" })] }), r.overview && _jsx("div", { className: "muted small clamp-2", children: r.overview })] }), busy === idOf(r) && _jsx(Icon, { name: "refresh", size: 18, className: "spin" })] }, idOf(r))))] })] }));
}
