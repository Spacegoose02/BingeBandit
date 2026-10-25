import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import type { Library, Member } from '../types'
import { Icon } from './Icon'
import { PasscodePrompt } from './Passcode'
import { TogetherInvites } from './Together'

export default function Layout() {
  const { user, logout } = useAuth()
  const { data: household } = useApi<Member[]>(user ? '/api/household' : null)
  const displayName = user?.profileName ?? user?.username ?? ''
  const { data: libraries } = useApi<Library[]>('/api/libraries')
  const [menuOpen, setMenuOpen] = useState(false)
  const loc = useLocation()
  const nav = useNavigate()

  useEffect(() => setMenuOpen(false), [loc.pathname])

  return (
    <div className="shell">
      <aside className="sidebar">
        <Link to="/" className="sidebar-brand" aria-label="Home">
          <img src="/brand/mascot.png" alt="" className="sidebar-mascot" />
          <span className="sidebar-wordmark">BingeBandit</span>
        </Link>
        <nav className="sidebar-nav">
          <NavLink to="/" end className="nav-item">
            <Icon name="home" />
            <span>Home</span>
          </NavLink>
          <NavLink to="/search" className="nav-item">
            <Icon name="search" />
            <span>Search</span>
          </NavLink>
          <NavLink to="/discover" className="nav-item">
            <Icon name="compass" />
            <span>Discover</span>
          </NavLink>
          <NavLink to="/live" className="nav-item">
            <Icon name="broadcast" />
            <span>Live TV</span>
          </NavLink>
          <NavLink to="/recordings" className="nav-item">
            <span className="nav-rec" />
            <span>Recordings</span>
          </NavLink>
          <NavLink to="/watchlist" className="nav-item">
            <Icon name="bookmark" />
            <span>Watchlist</span>
          </NavLink>
          <NavLink to="/playlists" className="nav-item">
            <Icon name="list" />
            <span>Playlists</span>
          </NavLink>
          {libraries && libraries.length > 0 && <div className="nav-section">Libraries</div>}
          {libraries?.map((l) => (
            <NavLink key={l.id} to={`/library/${l.id}`} className="nav-item">
              <Icon name={l.kind === 'movies' ? 'film' : 'tv'} />
              <span>{l.name}</span>
            </NavLink>
          ))}
          {user?.isAdmin && (
            <>
              <div className="nav-section">Server</div>
              <NavLink to="/admin" end className="nav-item">
                <Icon name="dashboard" />
                <span>Dashboard</span>
              </NavLink>
              <NavLink to="/admin/statistics" className="nav-item">
                <Icon name="chart" />
                <span>Statistics</span>
              </NavLink>
              <NavLink to="/admin/live" className="nav-item">
                <Icon name="broadcast" />
                <span>Live TV</span>
              </NavLink>
              <NavLink to="/admin/libraries" className="nav-item">
                <Icon name="library" />
                <span>Libraries</span>
              </NavLink>
              <NavLink to="/admin/users" className="nav-item">
                <Icon name="users" />
                <span>Users</span>
              </NavLink>
              <NavLink to="/admin/import" className="nav-item">
                <Icon name="download" />
                <span>Import</span>
              </NavLink>
              <NavLink to="/admin/plugins" className="nav-item">
                <Icon name="plug" />
                <span>Plugins</span>
              </NavLink>
              <NavLink to="/admin/tasks" className="nav-item">
                <Icon name="clock" />
                <span>Tasks</span>
              </NavLink>
              <NavLink to="/admin/logs" className="nav-item">
                <Icon name="terminal" />
                <span>Logs</span>
              </NavLink>
              <NavLink to="/admin/settings" className="nav-item">
                <Icon name="settings" />
                <span>Settings</span>
              </NavLink>
            </>
          )}
        </nav>
        <div className="sidebar-user">
          <button className="user-chip" onClick={() => setMenuOpen((o) => !o)} aria-expanded={menuOpen}>
            <span className="avatar">{displayName.slice(0, 1).toUpperCase()}</span>
            <span className="user-name">{displayName}</span>
          </button>
          {menuOpen && (
            <div className="menu user-menu" role="menu">
              <button role="menuitem" onClick={() => nav('/profile')}>
                <Icon name="user" size={18} /> My profile
              </button>
              {household && household.length > 1 && (
                <button role="menuitem" onClick={() => nav('/who')}>
                  <Icon name="users" size={18} /> Switch profile
                </button>
              )}
              <button role="menuitem" onClick={() => nav('/account')}>
                <Icon name="key" size={18} /> Account
              </button>
              <button role="menuitem" onClick={logout}>
                <Icon name="logout" size={18} /> Sign out
              </button>
            </div>
          )}
        </div>
      </aside>
      <main className="main">
        <TogetherInvites />
        <Outlet />
      </main>
      <PasscodePrompt />
    </div>
  )
}
