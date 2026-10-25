import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useRef } from 'react';
import { Icon } from './Icon';
/** Horizontally scrolling shelf with arrow buttons on hover. */
export function Row({ title, children, action }) {
    const scroller = useRef(null);
    const scroll = (dir) => {
        const el = scroller.current;
        if (el)
            el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: 'smooth' });
    };
    return (_jsxs("section", { className: "row", children: [_jsxs("div", { className: "row-head", children: [_jsx("h2", { children: title }), action] }), _jsxs("div", { className: "row-body", children: [_jsx("button", { className: "row-arrow left", onClick: () => scroll(-1), "aria-label": "Scroll left", children: _jsx(Icon, { name: "back", size: 28 }) }), _jsx("div", { className: "row-scroller", ref: scroller, children: children }), _jsx("button", { className: "row-arrow right", onClick: () => scroll(1), "aria-label": "Scroll right", children: _jsx(Icon, { name: "next", size: 28 }) })] })] }));
}
