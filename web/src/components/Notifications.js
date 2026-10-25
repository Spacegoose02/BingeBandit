import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api, img } from '../api';
import { timeAgo } from '../format';
import { useApi } from '../hooks';
import { Icon } from './Icon';
// NotificationBell sits next to the user chip and opens the newest
// notifications: new episodes of shows you follow and your recordings.
export function NotificationBell() {
    const { data, reload, setData } = useApi('/api/notifications', 60_000);
    const [open, setOpen] = useState(false);
    const loc = useLocation();
    const nav = useNavigate();
    const box = useRef(null);
    useEffect(() => setOpen(false), [loc.pathname]);
    useEffect(() => {
        if (!open)
            return;
        reload();
        const close = (e) => {
            if (box.current && !box.current.contains(e.target))
                setOpen(false);
        };
        document.addEventListener('mousedown', close);
        return () => document.removeEventListener('mousedown', close);
    }, [open, reload]);
    const unread = data?.unread ?? 0;
    const markRead = async (ids) => {
        if (!data)
            return;
        const now = Math.floor(Date.now() / 1000);
        const hit = (n) => !n.readAt && (!ids || ids.includes(n.id));
        setData({
            unread: Math.max(0, data.unread - data.notifications.filter(hit).length),
            notifications: data.notifications.map((n) => (hit(n) ? { ...n, readAt: now } : n)),
        });
        await api.post('/api/notifications/read', ids ? { ids } : {}).catch(() => reload());
    };
    const clear = async () => {
        setData({ notifications: [], unread: 0 });
        await api.del('/api/notifications').catch(() => reload());
    };
    const openOne = (n) => {
        if (!n.readAt)
            markRead([n.id]);
        setOpen(false);
        if (n.link)
            nav(n.link);
    };
    return (_jsxs("div", { className: "notify", ref: box, children: [_jsxs("button", { className: "icon-btn notify-bell", onClick: () => setOpen((o) => !o), "aria-expanded": open, "aria-label": unread ? `Notifications, ${unread} unread` : 'Notifications', title: "Notifications", children: [_jsx(Icon, { name: "bell" }), unread > 0 && _jsx("span", { className: "notify-badge", children: unread > 99 ? '99+' : unread })] }), open && (_jsxs("div", { className: "menu notify-panel", role: "dialog", "aria-label": "Notifications", children: [_jsxs("div", { className: "notify-head", children: [_jsx("strong", { children: "Notifications" }), unread > 0 && (_jsx("button", { className: "link-btn", onClick: () => markRead(), children: "Mark all read" })), unread === 0 && (data?.notifications.length ?? 0) > 0 && (_jsx("button", { className: "link-btn", onClick: clear, children: "Clear all" }))] }), !data?.notifications.length && (_jsx("p", { className: "muted small notify-empty", children: "Nothing yet. You'll hear about new episodes of shows you watch and how your recordings went. Turn these off in Account." })), _jsx("div", { className: "notify-list", children: data?.notifications.map((n) => (_jsxs("button", { className: `notify-item${n.readAt ? '' : ' unread'}`, onClick: () => openOne(n), children: [n.image ? (_jsx("img", { src: img(n.image, 'w92'), alt: "" })) : (_jsx("span", { className: `notify-icon notify-${n.kind}`, children: n.kind === 'recording' ? _jsx("span", { className: "nav-rec" }) : _jsx(Icon, { name: "tv", size: 18 }) })), _jsxs("span", { className: "notify-text", children: [_jsx("span", { className: "notify-title", children: n.title }), n.body && _jsx("span", { className: "notify-body", children: n.body }), _jsx("span", { className: "notify-time", children: timeAgo(n.createdAt) })] })] }, n.id))) })] }))] }));
}
