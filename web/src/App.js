import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { lazy, Suspense } from 'react';
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom';
import { useAuth } from './auth';
import Layout from './components/Layout';
import Setup from './pages/Setup';
import Login from './pages/Login';
import Home from './pages/Home';
import LibraryPage from './pages/Library';
import ItemPage from './pages/Item';
import CollectionPage from './pages/Collection';
import { DiscoverPage } from './pages/Discover';
import { PlaylistPage, PlaylistsPage, WatchlistPage } from './pages/Lists';
import ProfilePage from './pages/Profile';
import AdminStatistics from './pages/admin/Statistics';
import AdminLiveTV from './pages/admin/LiveTV';
import LiveGuide from './pages/LiveGuide';
import RecordingsPage from './pages/Recordings';
import Search from './pages/Search';
import Account from './pages/Account';
import LinkDevice from './pages/LinkDevice';
import AdminDashboard from './pages/admin/Dashboard';
import AdminLibraries from './pages/admin/Libraries';
import AdminUsers from './pages/admin/Users';
import AdminSettings from './pages/admin/Settings';
import AdminTasks from './pages/admin/Tasks';
import AdminLogs from './pages/admin/Logs';
import AdminImport from './pages/admin/Import';
import AdminPlugins from './pages/admin/Plugins';
import { PasscodeGate } from './components/Passcode';
import WhoIsWatching from './pages/WhoIsWatching';
import { profilePickPending } from './profilePick';
// hls.js is most of the bundle; only load it when someone presses play.
const Player = lazy(() => import('./pages/Player'));
const LivePlayer = lazy(() => import('./pages/LivePlayer'));
function RequireUser() {
    const { user } = useAuth();
    const loc = useLocation();
    if (!user)
        return _jsx(Navigate, { to: "/login", replace: true, state: { from: loc.pathname + loc.search } });
    if (profilePickPending())
        return _jsx(Navigate, { to: "/who", replace: true, state: { from: loc.pathname + loc.search, opening: true } });
    return _jsx(Outlet, {});
}
function RequireAdmin() {
    const { user } = useAuth();
    return user?.isAdmin ? _jsx(Outlet, {}) : _jsx(Navigate, { to: "/", replace: true });
}
export default function App() {
    const { loading, needsSetup, user } = useAuth();
    if (loading)
        return _jsx("div", { className: "boot", children: _jsx("img", { src: "/brand/mascot.png", alt: "" }) });
    if (needsSetup)
        return _jsx(Setup, {});
    return (_jsxs(Routes, { children: [_jsx(Route, { path: "/login", element: user ? _jsx(Navigate, { to: "/", replace: true }) : _jsx(Login, {}) }), _jsx(Route, { path: "/who", element: user ? _jsx(WhoIsWatching, {}) : _jsx(Navigate, { to: "/login", replace: true }) }), _jsxs(Route, { element: _jsx(RequireUser, {}), children: [_jsx(Route, { path: "/play/:id", element: _jsx(Suspense, { fallback: _jsx("div", { className: "player", children: _jsx("div", { className: "spinner" }) }), children: _jsx(Player, {}) }) }), _jsx(Route, { path: "/live/:id", element: _jsx(Suspense, { fallback: _jsx("div", { className: "player", children: _jsx("div", { className: "spinner" }) }), children: _jsx(LivePlayer, {}) }) }), _jsxs(Route, { element: _jsx(Layout, {}), children: [_jsx(Route, { index: true, element: _jsx(Home, {}) }), _jsx(Route, { path: "/live", element: _jsx(LiveGuide, {}) }), _jsx(Route, { path: "/recordings", element: _jsx(RecordingsPage, {}) }), _jsx(Route, { path: "/library/:id", element: _jsx(LibraryPage, {}) }), _jsx(Route, { path: "/item/:id", element: _jsx(ItemPage, {}) }), _jsx(Route, { path: "/collection/:id", element: _jsx(CollectionPage, {}) }), _jsx(Route, { path: "/discover", element: _jsx(DiscoverPage, {}) }), _jsx(Route, { path: "/watchlist", element: _jsx(WatchlistPage, {}) }), _jsx(Route, { path: "/playlists", element: _jsx(PlaylistsPage, {}) }), _jsx(Route, { path: "/playlist/:id", element: _jsx(PlaylistPage, {}) }), _jsx(Route, { path: "/profile", element: _jsx(ProfilePage, {}) }), _jsx(Route, { path: "/profile/:username", element: _jsx(ProfilePage, {}) }), _jsx(Route, { path: "/search", element: _jsx(Search, {}) }), _jsx(Route, { path: "/account", element: _jsx(PasscodeGate, { children: _jsx(Account, {}) }) }), _jsx(Route, { path: "/link", element: _jsx(PasscodeGate, { children: _jsx(LinkDevice, {}) }) }), _jsx(Route, { path: "/admin", element: _jsx(RequireAdmin, {}), children: _jsxs(Route, { element: _jsx(PasscodeGate, {}), children: [_jsx(Route, { index: true, element: _jsx(AdminDashboard, {}) }), _jsx(Route, { path: "libraries", element: _jsx(AdminLibraries, {}) }), _jsx(Route, { path: "users", element: _jsx(AdminUsers, {}) }), _jsx(Route, { path: "settings", element: _jsx(AdminSettings, {}) }), _jsx(Route, { path: "statistics", element: _jsx(AdminStatistics, {}) }), _jsx(Route, { path: "live", element: _jsx(AdminLiveTV, {}) }), _jsx(Route, { path: "tasks", element: _jsx(AdminTasks, {}) }), _jsx(Route, { path: "logs", element: _jsx(AdminLogs, {}) }), _jsx(Route, { path: "import", element: _jsx(AdminImport, {}) }), _jsx(Route, { path: "plugins", element: _jsx(AdminPlugins, {}) })] }) })] })] }), _jsx(Route, { path: "*", element: _jsx(Navigate, { to: "/", replace: true }) })] }));
}
