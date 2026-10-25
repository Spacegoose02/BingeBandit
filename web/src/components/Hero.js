import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { img } from '../api';
import { formatRuntime } from '../format';
import { Icon } from './Icon';
export function MetaLine({ item }) {
    const parts = [
        item.year ? String(item.year) : '',
        item.kind === 'movie' ? formatRuntime(item.runtime) : item.childCount ? `${item.childCount} episodes` : '',
        (item.genres ?? []).slice(0, 3).join(', '),
    ].filter(Boolean);
    return (_jsxs("div", { className: "meta-line", children: [parts.join(' · '), item.contentRating && _jsx("span", { className: "rating-pill", children: item.contentRating }), item.edition && _jsx("span", { className: "rating-pill", children: item.edition }), !!item.rating && (_jsxs("span", { className: "score", children: [_jsx(Icon, { name: "star", fill: true, size: 14 }), " ", item.rating.toFixed(1)] }))] }));
}
/** Rotating spotlight at the top of Home. */
export function Hero({ items }) {
    const [index, setIndex] = useState(0);
    useEffect(() => {
        if (items.length < 2)
            return;
        const t = window.setInterval(() => setIndex((i) => (i + 1) % items.length), 9000);
        return () => window.clearInterval(t);
    }, [items.length]);
    const item = items[index % Math.max(items.length, 1)];
    if (!item)
        return null;
    const logo = img(item.logo, 'w500');
    return (_jsxs("section", { className: "hero", children: [items.map((it, i) => (_jsx("img", { className: `hero-art ${i === index ? 'active' : ''}`, src: img(it.backdrop, 'w1280'), alt: "" }, it.id))), _jsx("div", { className: "hero-shade" }), _jsxs("div", { className: "hero-body", children: [logo ? _jsx("img", { className: "hero-logo", src: logo, alt: item.title }) : _jsx("h1", { className: "hero-title", children: item.title }), _jsx(MetaLine, { item: item }), item.overview && _jsx("p", { className: "hero-overview", children: item.overview }), _jsxs("div", { className: "actions", children: [_jsxs(Link, { to: item.kind === 'show' ? `/item/${item.id}` : `/play/${item.id}`, className: "btn btn-primary", children: [_jsx(Icon, { name: "play", fill: true, size: 18 }), " ", item.kind === 'show' ? 'Watch' : 'Play'] }), _jsxs(Link, { to: `/item/${item.id}`, className: "btn btn-glass", children: [_jsx(Icon, { name: "info", size: 18 }), " More info"] })] })] }, item.id), items.length > 1 && (_jsx("div", { className: "hero-dots", children: items.map((it, i) => (_jsx("button", { className: i === index ? 'active' : '', onClick: () => setIndex(i), "aria-label": `Show ${it.title}` }, it.id))) }))] }));
}
