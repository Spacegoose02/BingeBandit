import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api } from '../api';
import { CategoryMenu, ManageCategories } from '../components/ChannelCategories';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
const HOURS = 4; // guide window width
const SLOT = 30 * 60; // ruler step, seconds
export const clock = (unix) => new Date(unix * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
export const channelNo = (n) => (Number.isInteger(n) ? String(n) : n.toFixed(1));
export function ChannelLogo({ channel, size = 40 }) {
    const [broken, setBroken] = useState(false);
    if (!channel.logo || broken)
        return _jsx("span", { className: "ch-logo-fallback", style: { width: size * 1.5, height: size }, children: channel.name.slice(0, 3).toUpperCase() });
    return _jsx("img", { className: "ch-logo", src: channel.logo, alt: "", style: { height: size, maxWidth: size * 1.6 }, onError: () => setBroken(true), loading: "lazy" });
}
export default function LiveGuide() {
    useDocumentTitle('Live TV');
    const nav = useNavigate();
    const [from, setFrom] = useState(() => Math.floor(Date.now() / 1000 / SLOT) * SLOT);
    const to = from + HOURS * 3600;
    const { data, reload } = useApi(`/api/live/guide?from=${from}&to=${to}`, 60000);
    const [filter, setFilter] = useState(() => localStorage.getItem('bb.guideFilter') ?? 'all');
    const [selected, setSelected] = useState(null);
    const [now, setNow] = useState(Date.now() / 1000);
    const grid = useRef(null);
    const [width, setWidth] = useState(900);
    useEffect(() => {
        const t = window.setInterval(() => setNow(Date.now() / 1000), 30000);
        return () => window.clearInterval(t);
    }, []);
    useEffect(() => {
        const el = grid.current;
        if (!el)
            return;
        const ro = new ResizeObserver(() => setWidth(el.clientWidth));
        ro.observe(el);
        return () => ro.disconnect();
    }, [data]);
    useEffect(() => localStorage.setItem('bb.guideFilter', filter), [filter]);
    const [managing, setManaging] = useState(false);
    const groups = useMemo(() => [...new Set((data?.channels ?? []).map((c) => c.group).filter(Boolean))], [data]);
    const categories = data?.categories ?? [];
    // Your own categories are "my:<id>"; anything else is all, favourites or a provider category.
    const myCategory = filter.startsWith('my:') ? categories.find((c) => `my:${c.id}` === filter) : undefined;
    useEffect(() => {
        if (data && filter.startsWith('my:') && !myCategory)
            setFilter('all'); // deleted
    }, [data, filter, myCategory]);
    const inCategory = useMemo(() => new Set(myCategory?.channelIds ?? []), [myCategory]);
    const channels = (data?.channels ?? []).filter((c) => filter === 'all' || (filter === 'fav' ? c.favorite : myCategory ? inCategory.has(c.id) : c.group === filter));
    const labelW = width < 560 ? 120 : Math.min(270, Math.max(210, width * 0.26));
    const pxPerSec = (width - labelW) / (to - from);
    const x = (t) => (Math.max(Math.min(t, to), from) - from) * pxPerSec;
    const hero = selected ?? (channels[0] ? { channel: channels[0], program: channels[0].now } : null);
    const toggleFav = async (c) => {
        await api[c.favorite ? 'del' : 'post'](`/api/live/channels/${c.id}/favorite`);
        reload();
    };
    const [note, setNote] = useState('');
    const record = async (channel, program, series) => {
        try {
            const rec = await api.post('/api/dvr/recordings', { channelId: channel.id, programId: program.id, series });
            setNote((rec.status === 'pending'
                ? 'Sent to an admin for approval.'
                : series
                    ? `Recording every “${program.title}”.`
                    : `“${program.title}” will be recorded.`) + (rec.conflict ? ` It may not record: ${rec.conflict}.` : ''));
            reload();
        }
        catch (e) {
            setNote(e.message);
        }
    };
    const recStatus = (p) => (p ? data?.recordings?.[`${p.epgId}:${p.start}`] : undefined);
    if (data && data.channels.length === 0) {
        return (_jsx("div", { className: "page", children: _jsxs("div", { className: "empty-hero", children: [_jsx(Icon, { name: "tv", size: 64 }), _jsx("h1", { children: "No live channels yet" }), _jsx("p", { children: "An admin can add an M3U playlist and an XMLTV guide under Server \u2192 Live TV." })] }) }));
    }
    return (_jsxs("div", { className: "page live-guide", children: [hero && (_jsxs("section", { className: "guide-hero", children: [_jsxs("div", { className: "guide-hero-text", children: [_jsxs("div", { className: "muted small", children: [channelNo(hero.channel.number), " \u00B7 ", hero.channel.name, hero.program && ` · ${clock(hero.program.start)}–${clock(hero.program.stop)}`] }), _jsx("h1", { children: hero.program?.title ?? hero.channel.name }), hero.program?.subtitle && _jsx("div", { className: "guide-sub", children: hero.program.subtitle }), hero.program?.description && _jsx("p", { className: "guide-desc", children: hero.program.description }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-primary", onClick: () => nav(`/live/${hero.channel.id}`), children: [_jsx(Icon, { name: "play", fill: true, size: 18 }), " Watch ", hero.channel.name] }), data?.canRecord && hero.program && hero.program.stop > now && !recStatus(hero.program) && (_jsxs(_Fragment, { children: [_jsxs("button", { className: "btn btn-glass", onClick: () => record(hero.channel, hero.program, false), children: [_jsx("span", { className: "rec-dot" }), " Record"] }), _jsxs("button", { className: "btn btn-glass", onClick: () => record(hero.channel, hero.program, true), children: [_jsx("span", { className: "rec-dot" }), " Record series"] })] })), recStatus(hero.program) && (_jsxs(Link, { to: "/recordings", className: "btn btn-glass", children: [_jsx("span", { className: "rec-dot" }), ' ', { pending: 'Waiting for approval', recording: 'Recording now', completed: 'Recorded' }[recStatus(hero.program)] ??
                                                'Set to record'] })), _jsx(CategoryMenu, { channel: hero.channel, categories: categories, onChanged: reload }), note && _jsx("span", { className: "muted small", children: note })] })] }), _jsx(ChannelLogo, { channel: hero.channel, size: 72 })] })), _jsxs("div", { className: "guide-toolbar", children: [_jsxs("select", { value: filter, onChange: (e) => setFilter(e.target.value), "aria-label": "Filter channels", children: [_jsx("option", { value: "all", children: "All channels" }), _jsx("option", { value: "fav", children: "Favorites" }), categories.length > 0 && (_jsx("optgroup", { label: "My categories", children: categories.map((c) => (_jsx("option", { value: `my:${c.id}`, children: c.name }, c.id))) })), groups.length > 0 && (_jsx("optgroup", { label: "Provider categories", children: groups.map((g) => (_jsx("option", { value: g, children: g }, g))) }))] }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setManaging(true), children: [_jsx(Icon, { name: "list", size: 16 }), " My categories"] }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setFrom(from - 2 * 3600), "aria-label": "Earlier", children: _jsx(Icon, { name: "back", size: 16 }) }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setFrom(Math.floor(Date.now() / 1000 / SLOT) * SLOT), children: "Now" }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setFrom(from + 2 * 3600), "aria-label": "Later", children: _jsx(Icon, { name: "next", size: 16 }) })] }), _jsx("span", { className: "muted small", children: new Date(from * 1000).toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' }) })] }), managing && data && (_jsx(ManageCategories, { categories: categories, channels: data.channels, onClose: () => setManaging(false), onChanged: reload })), _jsxs("div", { className: "guide-grid", ref: grid, children: [_jsxs("div", { className: "guide-ruler", style: { paddingLeft: labelW }, children: [Array.from({ length: (HOURS * 3600) / SLOT }, (_, i) => from + i * SLOT).map((t) => (_jsx("span", { style: { left: labelW + x(t) }, children: clock(t) }, t))), now >= from && now < to && _jsx("span", { className: "guide-now-label", style: { left: labelW + x(now) }, children: "Now" })] }), channels.length === 0 && (_jsx("div", { className: "empty", children: myCategory ? `“${myCategory.name}” has no channels yet. Add some with My categories, or “Add to category” above.` : 'No channels match this filter.' })), channels.map((c) => {
                        const progs = (c.epgId && data?.programs[c.epgId]) || [];
                        return (_jsxs("div", { className: "guide-row", children: [_jsxs("div", { className: "guide-channel", style: { width: labelW }, children: [_jsx("button", { className: `fav-star ${c.favorite ? 'on' : ''}`, onClick: () => toggleFav(c), "aria-label": c.favorite ? 'Remove favorite' : 'Add favorite', children: _jsx(Icon, { name: "star", fill: c.favorite, size: 14 }) }), _jsxs("button", { className: "guide-channel-btn", onClick: () => nav(`/live/${c.id}`), title: `Watch ${c.name}`, children: [_jsx("span", { className: "ch-num", children: channelNo(c.number) }), _jsx(ChannelLogo, { channel: c, size: 28 }), _jsx("span", { className: "ch-name", children: c.name })] })] }), _jsxs("div", { className: "guide-track", children: [progs.length === 0 && (_jsxs("button", { className: "guide-prog", style: { left: 0, width: width - labelW - 4 }, onClick: () => setSelected({ channel: c }), children: [_jsx("span", { className: "gp-title", children: c.name }), _jsx("span", { className: "gp-time", children: "No guide information" })] })), progs.map((p) => {
                                            const left = x(p.start);
                                            const w = x(p.stop) - left;
                                            if (w <= 2)
                                                return null;
                                            const live = p.start <= now && p.stop > now;
                                            const isSel = selected?.program?.id === p.id;
                                            return (_jsxs("button", { className: `guide-prog ${live ? 'live' : ''} ${p.stop <= now ? 'past' : ''} ${isSel ? 'selected' : ''}`, style: { left, width: w - 4 }, onClick: () => (isSel && live ? nav(`/live/${c.id}`) : setSelected({ channel: c, program: p })), onDoubleClick: () => live && nav(`/live/${c.id}`), title: `${p.title} · ${clock(p.start)}–${clock(p.stop)}`, children: [p.start < from && _jsx(Icon, { name: "back", size: 12, className: "gp-cont" }), _jsxs("span", { className: "gp-title", children: [recStatus(p) && _jsx("span", { className: `rec-dot ${recStatus(p) === 'pending' ? 'pending' : ''}`, title: "Set to record" }), p.title] }), w > 120 && _jsxs("span", { className: "gp-time", children: [clock(p.start), "\u2013", clock(p.stop)] })] }, p.id));
                                        }), now >= from && now < to && _jsx("span", { className: "guide-now-line", style: { left: x(now) } })] })] }, c.id));
                    })] })] }));
}
