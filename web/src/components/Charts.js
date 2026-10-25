import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { img } from '../api';
// Chart marks use a darker teal than the UI accent: it passes the palette
// validator's lightness band and 3:1 contrast on the dark chart surface.
const MARK = 'var(--chart-mark)';
function niceMax(v) {
    if (v <= 0)
        return 1;
    const p = Math.pow(10, Math.floor(Math.log10(v)));
    for (const m of [1, 2, 2.5, 5, 10])
        if (m * p >= v)
            return m * p;
    return 10 * p;
}
// Tick labels: 0 / 0.25 / 2.5 / 1K, never rounding a tick to a different value.
const compact = (n) => {
    if (n >= 1000)
        return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K`;
    return String(Number(n.toFixed(2)));
};
/** Single-series column chart with gridlines, sparse x labels and hover tooltips. */
export function ColumnChart({ data, height = 200, labelEvery = 1, caption, valueLabel, }) {
    const box = useRef(null);
    const [width, setWidth] = useState(600);
    const [hover, setHover] = useState(null);
    useEffect(() => {
        const el = box.current;
        if (!el)
            return;
        const ro = new ResizeObserver(() => setWidth(el.clientWidth));
        ro.observe(el);
        setWidth(el.clientWidth);
        return () => ro.disconnect();
    }, []);
    const pad = { top: 10, right: 8, bottom: 26, left: 40 };
    const plotW = Math.max(width - pad.left - pad.right, 50);
    const plotH = height - pad.top - pad.bottom;
    const max = niceMax(Math.max(...data.map((d) => d.value), 0));
    const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * max);
    const band = plotW / Math.max(data.length, 1);
    const barW = Math.max(Math.min(24, band - 2), 2);
    const y = (v) => pad.top + plotH - (v / max) * plotH;
    return (_jsxs("figure", { className: "chart", "aria-label": caption, children: [_jsxs("div", { className: "chart-box", ref: box, onMouseLeave: () => setHover(null), children: [_jsxs("svg", { width: width, height: height, role: "img", "aria-label": caption, children: [ticks.map((t) => (_jsxs("g", { children: [_jsx("line", { x1: pad.left, x2: width - pad.right, y1: y(t), y2: y(t), className: "chart-grid" }), _jsx("text", { x: pad.left - 8, y: y(t), className: "chart-tick", textAnchor: "end", dominantBaseline: "middle", children: compact(t) })] }, t))), data.map((d, i) => {
                                const cx = pad.left + band * i + band / 2;
                                const h = Math.max(y(0) - y(d.value), d.value > 0 ? 2 : 0);
                                const x = cx - barW / 2;
                                const top = y(0) - h;
                                const r = Math.min(4, barW / 2, h);
                                return (_jsxs("g", { children: [h > 0 && (
                                        // Rounded data end, square at the baseline.
                                        _jsx("path", { d: `M${x},${y(0)} V${top + r} Q${x},${top} ${x + r},${top} H${x + barW - r} Q${x + barW},${top} ${x + barW},${top + r} V${y(0)} Z`, fill: MARK, opacity: hover === null || hover === i ? 1 : 0.55 })), i % labelEvery === 0 && (_jsx("text", { x: cx, y: height - 8, className: "chart-tick", textAnchor: "middle", children: d.label })), _jsx("rect", { x: pad.left + band * i, y: pad.top, width: band, height: plotH, fill: "transparent", onMouseEnter: () => setHover(i) })] }, i));
                            })] }), hover !== null && data[hover] && (_jsx("div", { className: "chart-tip", style: { left: Math.min(Math.max(pad.left + band * hover + band / 2, 70), width - 70), top: Math.max(y(data[hover].value) - 8, 0) }, children: data[hover].tip }))] }), _jsxs("details", { className: "chart-table", children: [_jsx("summary", { children: "Show as table" }), _jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: caption }), _jsx("th", { children: valueLabel })] }) }), _jsx("tbody", { children: data.map((d, i) => (_jsxs("tr", { children: [_jsx("td", { children: d.label }), _jsx("td", { children: d.value % 1 ? d.value.toFixed(1) : d.value })] }, i))) })] })] })] }));
}
/** Horizontal ranked bars: label, bar, value at the tip. */
export function RankedBars({ rows, empty = 'No data yet' }) {
    if (!rows.length)
        return _jsx("div", { className: "muted small", children: empty });
    const max = Math.max(...rows.map((r) => r.value), 1);
    return (_jsx("ol", { className: "ranked", children: rows.map((r, i) => (_jsxs("li", { children: [r.poster !== undefined && _jsx("span", { className: "ranked-poster", children: r.poster && _jsx("img", { src: img(r.poster, 'w92'), alt: "", loading: "lazy" }) }), _jsxs("div", { className: "ranked-body", children: [_jsx("div", { className: "ranked-label", children: r.href ? _jsx(Link, { to: r.href, children: r.label }) : r.label }), _jsxs("div", { className: "ranked-track", children: [_jsx("span", { className: "ranked-bar", style: { width: `${Math.max((r.value / max) * 100, 2)}%` } }), _jsx("span", { className: "ranked-value", children: r.display })] })] })] }, `${r.label}-${i}`))) }));
}
export function StatTile({ label, value }) {
    return (_jsxs("div", { className: "stat", children: [_jsx("div", { className: "stat-value", children: value }), _jsx("div", { className: "stat-label", children: label })] }));
}
