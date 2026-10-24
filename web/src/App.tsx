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
import Search from './pages/Search'
import Account from './pages/Account'
import AdminDashboard from './pages/admin/Dashboard'
import AdminLibraries from './pages/admin/Libraries'
import AdminUsers from './pages/admin/Users'
import AdminSettings from './pages/admin/Settings'

// hls.js is most of the bundle; only load it when someone presses play.
const Player = lazy(() => import('./pages/Player'))

function RequireUser() {
  const { user } = useAuth()
  const loc = useLocation()
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
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
      <Route element={<RequireUser />}>
        <Route
          path="/play/:id"
          element={
            <Suspense fallback={<div className="player"><div className="spinner" /></div>}>
              <Player />
            </Suspense>
          }
        />
        <Route element={<Layout />}>
          <Route index element={<Home />} />
          <Route path="/library/:id" element={<LibraryPage />} />
          <Route path="/item/:id" element={<ItemPage />} />
          <Route path="/collection/:id" element={<CollectionPage />} />
          <Route path="/search" element={<Search />} />
          <Route path="/account" element={<Account />} />
          <Route path="/admin" element={<RequireAdmin />}>
            <Route index element={<AdminDashboard />} />
            <Route path="libraries" element={<AdminLibraries />} />
            <Route path="users" element={<AdminUsers />} />
            <Route path="settings" element={<AdminSettings />} />
          </Route>
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
