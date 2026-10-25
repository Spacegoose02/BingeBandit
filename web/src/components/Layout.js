import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth';
import { useApi } from '../hooks';
import { Avatar } from './Avatar';
import { Icon } from './Icon';
import { NotificationBell } from './Notifications';
import { PasscodePrompt } from './Passcode';
import { PinsDialog, savePins } from './Pins';
import { TogetherInvites } from './Together';
export default function Layout() {
    const { user, logout } = useAuth();
    const { data: household } = useApi(user ? '/api/household' : null);
    const displayName = user?.profileName ?? user?.username ?? '';
    const { data: libraries, reload: reloadLibraries, setData: setLibraries } = useApi('/api/libraries');
    const { data: pins, reload: reloadPins } = useApi(user ? '/api/me/pins' : null);
    const [editingPins, setEditingPins] = useState(false);
    const [showMore, setShowMore] = useState(false);
    const [menuOpen, setMenuOpen] = useState(false);
    const loc = useLocation();
    const nav = useNavigate();
    useEffect(() => setMenuOpen(false), [loc.pathname]);
    const unpinned = libraries?.filter((l) => !l.pinned) ?? [];
    const togglePin = (l) => {
        if (!libraries)
            return;
        const next = libraries.map((x) => (x.id === l.id ? { ...x, pinned: !x.pinned } : x));
        const ordered = [...next.filter((x) => x.pinned), ...next.filter((x) => !x.pinned)];
        setLibraries(ordered);
        savePins(ordered).finally(reloadLibraries);
    };
    // Ask once, after the first sign-in, which libraries to pin.
    const askPins = !!pins && !pins.chosen && !!libraries && libraries.length > 1;
    return (_jsxs("div", { className: "shell", children: [_jsxs("aside", { className: "sidebar", children: [_jsxs(Link, { to: "/", className: "sidebar-brand", "aria-label": "Home", children: [_jsx("img", { src: "/brand/mascot.png", alt: "", className: "sidebar-mascot" }), _jsx("span", { className: "sidebar-wordmark", children: "BingeBandit" })] }), _jsxs("nav", { className: "sidebar-nav", children: [_jsxs(NavLink, { to: "/", end: true, className: "nav-item", children: [_jsx(Icon, { name: "home" }), _jsx("span", { children: "Home" })] }), _jsxs(NavLink, { to: "/search", className: "nav-item", children: [_jsx(Icon, { name: "search" }), _jsx("span", { children: "Search" })] }), _jsxs(NavLink, { to: "/discover", className: "nav-item", children: [_jsx(Icon, { name: "compass" }), _jsx("span", { children: "Discover" })] }), user?.liveTV !== false && (_jsxs(_Fragment, { children: [_jsxs(NavLink, { to: "/live", className: "nav-item", children: [_jsx(Icon, { name: "broadcast" }), _jsx("span", { children: "Live TV" })] }), _jsxs(NavLink, { to: "/recordings", className: "nav-item", children: [_jsx("span", { className: "nav-rec" }), _jsx("span", { children: "Recordings" })] })] })), _jsxs(NavLink, { to: "/watchlist", className: "nav-item", children: [_jsx(Icon, { name: "bookmark" }), _jsx("span", { children: "Watchlist" })] }), _jsxs(NavLink, { to: "/playlists", className: "nav-item", children: [_jsx(Icon, { name: "list" }), _jsx("span", { children: "Playlists" })] }), libraries && libraries.length > 0 && (_jsxs("div", { className: "nav-section nav-section-row", children: [_jsx("span", { children: "Pinned" }), _jsx("button", { className: "nav-section-btn", onClick: () => setEditingPins(true), children: "Edit" })] })), libraries?.filter((l) => l.pinned).map((l) => (_jsx(LibraryLink, { library: l, onTogglePin: () => togglePin(l) }, l.id))), unpinned.length > 0 && (_jsxs(_Fragment, { children: [_jsxs("button", { className: "nav-item nav-more", onClick: () => setShowMore((m) => !m), "aria-expanded": showMore, children: [_jsx(Icon, { name: "more" }), _jsx("span", { children: showMore ? 'Less' : `More (${unpinned.length})` })] }), showMore && unpinned.map((l) => _jsx(LibraryLink, { library: l, onTogglePin: () => togglePin(l) }, l.id))] })), user?.isAdmin && (_jsxs(_Fragment, { children: [_jsx("div", { className: "nav-section", children: "Server" }), _jsxs(NavLink, { to: "/admin", end: true, className: "nav-item", children: [_jsx(Icon, { name: "dashboard" }), _jsx("span", { children: "Dashboard" })] }), _jsxs(NavLink, { to: "/admin/statistics", className: "nav-item", children: [_jsx(Icon, { name: "chart" }), _jsx("span", { children: "Statistics" })] }), _jsxs(NavLink, { to: "/admin/live", className: "nav-item", children: [_jsx(Icon, { name: "broadcast" }), _jsx("span", { children: "Live TV" })] }), _jsxs(NavLink, { to: "/admin/libraries", className: "nav-item", children: [_jsx(Icon, { name: "library" }), _jsx("span", { children: "Libraries" })] }), _jsxs(NavLink, { to: "/admin/users", className: "nav-item", children: [_jsx(Icon, { name: "users" }), _jsx("span", { children: "Users" })] }), _jsxs(NavLink, { to: "/admin/import", className: "nav-item", children: [_jsx(Icon, { name: "download" }), _jsx("span", { children: "Import" })] }), _jsxs(NavLink, { to: "/admin/plugins", className: "nav-item", children: [_jsx(Icon, { name: "plug" }), _jsx("span", { children: "Plugins" })] }), _jsxs(NavLink, { to: "/admin/tasks", className: "nav-item", children: [_jsx(Icon, { name: "clock" }), _jsx("span", { children: "Tasks" })] }), _jsxs(NavLink, { to: "/admin/logs", className: "nav-item", children: [_jsx(Icon, { name: "terminal" }), _jsx("span", { children: "Logs" })] }), _jsxs(NavLink, { to: "/admin/settings", className: "nav-item", children: [_jsx(Icon, { name: "settings" }), _jsx("span", { children: "Settings" })] })] }))] }), _jsxs("div", { className: "sidebar-user", children: [_jsxs("div", { className: "sidebar-user-row", children: [_jsxs("button", { className: "user-chip", onClick: () => setMenuOpen((o) => !o), "aria-expanded": menuOpen, children: [_jsx(Avatar, { name: displayName, src: user?.avatar }), _jsx("span", { className: "user-name", children: displayName })] }), _jsx(NotificationBell, {})] }), menuOpen && (_jsxs("div", { className: "menu user-menu", role: "menu", children: [_jsxs("button", { role: "menuitem", onClick: () => nav('/profile'), children: [_jsx(Icon, { name: "user", size: 18 }), " My profile"] }), household && household.length > 1 && (_jsxs("button", { role: "menuitem", onClick: () => nav('/who'), children: [_jsx(Icon, { name: "users", size: 18 }), " Switch profile"] })), _jsxs("button", { role: "menuitem", onClick: () => nav('/account'), children: [_jsx(Icon, { name: "key", size: 18 }), " Account"] }), _jsxs("button", { role: "menuitem", onClick: logout, children: [_jsx(Icon, { name: "logout", size: 18 }), " Sign out"] })] }))] })] }), _jsxs("main", { className: "main", children: [_jsx(TogetherInvites, {}), _jsx(Outlet, {})] }), _jsx(PasscodePrompt, {}), (editingPins || askPins) && libraries && (_jsx(PinsDialog, { libraries: libraries, first: askPins, onClose: () => {
                    setEditingPins(false);
                    reloadPins();
                }, onSaved: () => {
                    setEditingPins(false);
                    reloadPins();
                    reloadLibraries();
                } }))] }));
}
function LibraryLink({ library: l, onTogglePin }) {
    return (_jsxs(NavLink, { to: `/library/${l.id}`, className: "nav-item nav-library", children: [_jsx(Icon, { name: l.kind === 'movies' ? 'film' : 'tv' }), _jsx("span", { children: l.name }), _jsx("button", { className: `nav-pin ${l.pinned ? 'on' : ''}`, title: l.pinned ? 'Unpin' : 'Pin', "aria-label": l.pinned ? `Unpin ${l.name}` : `Pin ${l.name}`, onClick: (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onTogglePin();
                }, children: _jsx(Icon, { name: "pin", size: 14 }) })] }));
}
