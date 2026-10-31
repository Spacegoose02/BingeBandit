import { lazy, Suspense } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth'
import Layout from './components/Layout'
import Setup from './pages/Setup'
import Login from './pages/Login'
import Home from './pages/Home'
import LibraryPage from './pages/Library'
import ItemPage from './pages/Item'
import CollectionPage from './pages/Collection'
import PersonPage from './pages/Person'
import { DiscoverPage } from './pages/Discover'
import SportsPage from './pages/Sports'
import { FavoritesPage, KidsTitlesPage, PlaylistPage, PlaylistsPage, WatchlistPage } from './pages/Lists'
import ProfilePage from './pages/Profile'
import AdminStatistics from './pages/admin/Statistics'
import AdminLiveTV from './pages/admin/LiveTV'
import LiveGuide from './pages/LiveGuide'
import RecordingsPage from './pages/Recordings'
import Search from './pages/Search'
import Account from './pages/Account'
import TVSetupSheet from './pages/TVSetupSheet'
import LinkDevice from './pages/LinkDevice'
import AdminDashboard from './pages/admin/Dashboard'
import AdminLibraries from './pages/admin/Libraries'
import AdminUsers from './pages/admin/Users'
import AdminSettings from './pages/admin/Settings'
import AdminTasks from './pages/admin/Tasks'
import AdminLogs from './pages/admin/Logs'
import AdminImport from './pages/admin/Import'
import AdminPlugins from './pages/admin/Plugins'
import AdminOverlays from './pages/admin/Overlays'
import { PasscodeGate } from './components/Passcode'
import WhoIsWatching from './pages/WhoIsWatching'
import { profilePickPending } from './profilePick'

// hls.js is most of the bundle; only load it when someone presses play.
const Player = lazy(() => import('./pages/Player'))
const LivePlayer = lazy(() => import('./pages/LivePlayer'))

function RequireUser() {
  const { user } = useAuth()
  const loc = useLocation()
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  if (profilePickPending()) return <Navigate to="/who" replace state={{ from: loc.pathname + loc.search, opening: true }} />
  return <Outlet />
}

function RequireAdmin() {
  const { user } = useAuth()
  return user?.isAdmin ? <Outlet /> : <Navigate to="/" replace />
}

export default function App() {
  const { loading, needsSetup, user } = useAuth()
  if (loading) return <div className="boot"><img src="/brand/mascot.png" alt="" /></div>
  if (needsSetup) return <Setup />

  return (
    <Routes>
      <Route path="/login" element={user ? <Navigate to="/" replace /> : <Login />} />
      <Route path="/who" element={user ? <WhoIsWatching /> : <Navigate to="/login" replace />} />
      <Route element={<RequireUser />}>
        <Route
          path="/play/:id"
          element={
            <Suspense fallback={<div className="player"><div className="spinner" /></div>}>
              <Player />
            </Suspense>
          }
        />
        <Route
          path="/live/:id"
          element={
            <Suspense fallback={<div className="player"><div className="spinner" /></div>}>
              <LivePlayer />
            </Suspense>
          }
        />
        <Route path="/tv-setup" element={<TVSetupSheet />} />
        <Route element={<Layout />}>
          <Route index element={<Home />} />
          <Route path="/live" element={<LiveGuide />} />
          <Route path="/recordings" element={<RecordingsPage />} />
          <Route path="/library/:id" element={<LibraryPage />} />
          <Route path="/item/:id" element={<ItemPage />} />
          <Route path="/collection/:id" element={<CollectionPage />} />
          <Route path="/person" element={<PersonPage />} />
          <Route path="/discover" element={<DiscoverPage />} />
          <Route path="/sports" element={<SportsPage />} />
          <Route path="/watchlist" element={<WatchlistPage />} />
          <Route path="/favorites" element={<FavoritesPage />} />
          <Route path="/kids/movies" element={<KidsTitlesPage key="movie" kind="movie" />} />
          <Route path="/kids/shows" element={<KidsTitlesPage key="show" kind="show" />} />
          <Route path="/playlists" element={<PlaylistsPage />} />
          <Route path="/playlist/:id" element={<PlaylistPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="/profile/:username" element={<ProfilePage />} />
          <Route path="/search" element={<Search />} />
          <Route path="/account" element={<PasscodeGate><Account /></PasscodeGate>} />
          <Route path="/link" element={<PasscodeGate><LinkDevice /></PasscodeGate>} />
          <Route path="/admin" element={<RequireAdmin />}>
            <Route element={<PasscodeGate />}>
              <Route index element={<AdminDashboard />} />
              <Route path="libraries" element={<AdminLibraries />} />
              <Route path="users" element={<AdminUsers />} />
              <Route path="settings" element={<AdminSettings />} />
              <Route path="statistics" element={<AdminStatistics />} />
              <Route path="live" element={<AdminLiveTV />} />
              <Route path="tasks" element={<AdminTasks />} />
              <Route path="logs" element={<AdminLogs />} />
              <Route path="import" element={<AdminImport />} />
              <Route path="plugins" element={<AdminPlugins />} />
              <Route path="overlays" element={<AdminOverlays />} />
            </Route>
          </Route>
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
