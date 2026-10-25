import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect } from 'react';
import { Icon } from './Icon';
export function Modal({ title, onClose, children, wide }) {
    useEffect(() => {
        const onKey = (e) => e.key === 'Escape' && onClose();
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose]);
    return (_jsx("div", { className: "modal-backdrop", onMouseDown: (e) => e.target === e.currentTarget && onClose(), children: _jsxs("div", { className: `modal ${wide ? 'modal-wide' : ''}`, role: "dialog", "aria-modal": "true", "aria-label": title, children: [_jsxs("div", { className: "modal-head", children: [_jsx("h3", { children: title }), _jsx("button", { className: "icon-btn", onClick: onClose, "aria-label": "Close", children: _jsx(Icon, { name: "x" }) })] }), _jsx("div", { className: "modal-body", children: children })] }) }));
}
