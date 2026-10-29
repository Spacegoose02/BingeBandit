import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import type { Library, Member } from '../types'
import { Avatar } from './Avatar'
import { Icon } from './Icon'
import { NotificationBell } from './Notifications'
import { PasscodePrompt } from './Passcode'
import { PinsDialog, savePins } from './Pins'
import { SelectionProvider } from './Selection'
import { TogetherInvites } from './Together'

export default function Layout() {
  const { user, logout } = useAuth()
  const { data: household } = useApi<Member[]>(user ? '/api/household' : null)
  const displayName = user?.profileName ?? user?.username ?? ''
  const { data: libraries, reload: reloadLibraries, setData: setLibraries } = useApi<Library[]>('/api/libraries')
  const { data: pins, reload: reloadPins } = useApi<{ chosen: boolean }>(user ? '/api/me/pins' : null)
  const [editingPins, setEditingPins] = useState(false)
  const [showMore, setShowMore] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const loc = useLocation()
  const nav = useNavigate()

  useEffect(() => setMenuOpen(false), [loc.pathname])

  const unpinned = libraries?.filter((l) => !l.pinned) ?? []
  const togglePin = (l: Library) => {
    if (!libraries) return
    const next = libraries.map((x) => (x.id === l.id ? { ...x, pinned: !x.pinned } : x))
    const ordered = [...next.filter((x) => x.pinned), ...next.filter((x) => !x.pinned)]
    setLibraries(ordered)
    savePins(ordered).finally(reloadLibraries)
  }
  // Ask once, after the first sign-in, which libraries to pin.
  const askPins = !!pins && !pins.chosen && !!libraries && libraries.length > 1

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
          <NavLink to="/sports" className="nav-item">
            <Icon name="trophy" />
            <span>Sports</span>
          </NavLink>
          {user?.liveTV !== false && (
            <>
              <NavLink to="/live" className="nav-item">
                <Icon name="broadcast" />
                <span>Live TV</span>
              </NavLink>
              <NavLink to="/recordings" className="nav-item">
                <span className="nav-rec" />
                <span>Recordings</span>
              </NavLink>
            </>
          )}
          {user?.kid && (
            <>
              <NavLink to="/kids/movies" className="nav-item">
                <Icon name="film" />
                <span>Approved Movies</span>
              </NavLink>
              <NavLink to="/kids/shows" className="nav-item">
                <Icon name="tv" />
                <span>Approved Shows</span>
              </NavLink>
            </>
          )}
          <NavLink to="/watchlist" className="nav-item">
            <Icon name="bookmark" />
            <span>Watchlist</span>
          </NavLink>
          <NavLink to="/playlists" className="nav-item">
            <Icon name="list" />
            <span>Playlists</span>
          </NavLink>
          {libraries && libraries.length > 0 && (
            <div className="nav-section nav-section-row">
              <span>Pinned</span>
              <button className="nav-section-btn" onClick={() => setEditingPins(true)}>
                Edit
              </button>
            </div>
          )}
          {libraries?.filter((l) => l.pinned).map((l) => (
            <LibraryLink key={l.id} library={l} onTogglePin={() => togglePin(l)} />
          ))}
          {unpinned.length > 0 && (
            <>
              <button className="nav-item nav-more" onClick={() => setShowMore((m) => !m)} aria-expanded={showMore}>
                <Icon name="more" />
                <span>{showMore ? 'Less' : `More (${unpinned.length})`}</span>
              </button>
              {showMore && unpinned.map((l) => <LibraryLink key={l.id} library={l} onTogglePin={() => togglePin(l)} />)}
            </>
          )}
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
              <NavLink to="/admin/overlays" className="nav-item">
                <Icon name="image" />
                <span>Overlays</span>
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
          <div className="sidebar-user-row">
            <button className="user-chip" onClick={() => setMenuOpen((o) => !o)} aria-expanded={menuOpen}>
              <Avatar name={displayName} src={user?.avatar} />
              <span className="user-name">{displayName}</span>
            </button>
            <NotificationBell />
          </div>
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
        <SelectionProvider>
          <Outlet />
        </SelectionProvider>
      </main>
      <PasscodePrompt />
      {(editingPins || askPins) && libraries && (
        <PinsDialog
          libraries={libraries}
          first={askPins}
          onClose={() => {
            setEditingPins(false)
            reloadPins()
          }}
          onSaved={() => {
            setEditingPins(false)
            reloadPins()
            reloadLibraries()
          }}
        />
      )}
    </div>
  )
}

function LibraryLink({ library: l, onTogglePin }: { library: Library; onTogglePin: () => void }) {
  return (
    <NavLink to={`/library/${l.id}`} className="nav-item nav-library">
      <Icon name={l.kind === 'movies' ? 'film' : 'tv'} />
      <span>{l.name}</span>
      <button
        className={`nav-pin ${l.pinned ? 'on' : ''}`}
        title={l.pinned ? 'Unpin' : 'Pin'}
        aria-label={l.pinned ? `Unpin ${l.name}` : `Pin ${l.name}`}
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          onTogglePin()
        }}
      >
        <Icon name="pin" size={14} />
      </button>
    </NavLink>
  )
}
