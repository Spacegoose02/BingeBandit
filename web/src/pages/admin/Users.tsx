import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { Library, User } from '../../types'

export default function AdminUsers() {
  useDocumentTitle('Users')
  const { user: me } = useAuth()
  const { data: users, reload } = useApi<User[]>('/api/admin/users')
  const [adding, setAdding] = useState(false)
  const [resetting, setResetting] = useState<User | null>(null)
  const [access, setAccess] = useState<User | null>(null)

  const remove = async (u: User) => {
    if (!confirm(`Delete ${u.username}? Their watch history is removed too.`)) return
    await api.del(`/api/admin/users/${u.id}`).catch((e) => alert(e.message))
    reload()
  }
  const clearPasscode = async (u: User) => {
    if (!confirm(`Remove ${u.username}'s settings passcode? They can set a new one under Account.`)) return
    await api.del(`/api/admin/users/${u.id}/passcode`).catch((e) => alert(e.message))
    reload()
  }
  const reset2FA = async (u: User) => {
    if (!confirm(`Turn off two-step sign-in for ${u.username}? They can sign in with just their password until they set it up again.`)) return
    await api.del(`/api/admin/users/${u.id}/2fa`).catch((e) => alert(e.message))
    reload()
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>Users</h1>
        <button className="btn btn-primary" onClick={() => setAdding(true)}>
          <Icon name="plus" size={18} /> Add user
        </button>
      </header>
      <div className="panel list">
        {users?.map((u) => (
          <div key={u.id} className="list-row">
            <span className="avatar">{u.username.slice(0, 1).toUpperCase()}</span>
            <div className="grow">
              <div>
                {u.username} {u.id === me?.id && <span className="muted small">(you)</span>}{' '}
                {!u.hasPassword && <span className="pill" title="Made by single sign-on; no password yet">SSO only</span>}
              </div>
              <div className="muted small">Joined {new Date(u.createdAt * 1000).toLocaleDateString()}</div>
            </div>
            {u.isAdmin ? (
              <span className="pill ok">Admin · all libraries</span>
            ) : (
              <button className="btn btn-glass btn-sm" onClick={() => setAccess(u)}>
                <Icon name="library" size={16} /> {u.allLibraries ? 'All libraries' : `${u.libraryIds?.length ?? 0} libraries`}
              </button>
            )}
            {u.hasPasscode && (
              <button className="btn btn-glass btn-sm" onClick={() => clearPasscode(u)} title="For someone who forgot their settings passcode">
                <Icon name="unlock" size={16} /> Clear passcode
              </button>
            )}
            {u.totpEnabled && (
              <button className="btn btn-glass btn-sm" onClick={() => reset2FA(u)} title="For someone who lost their phone and recovery codes">
                Turn off 2-step
              </button>
            )}
            <button className="btn btn-glass btn-sm" onClick={() => setResetting(u)}>
              <Icon name="key" size={16} /> Reset password
            </button>
            {u.id !== me?.id && (
              <button className="btn btn-glass btn-sm danger" onClick={() => remove(u)} aria-label={`Delete ${u.username}`}>
                <Icon name="trash" size={16} />
              </button>
            )}
          </div>
        ))}
      </div>
      {adding && <AddUser onClose={() => setAdding(false)} onSaved={() => { setAdding(false); reload() }} />}
      {resetting && <ResetPassword user={resetting} onClose={() => setResetting(null)} />}
      {access && <LibraryAccess user={access} onClose={() => setAccess(null)} onSaved={() => { setAccess(null); reload() }} />}
    </div>
  )
}

function LibraryAccess({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const { data: libraries } = useApi<Library[]>('/api/admin/libraries')
  const [all, setAll] = useState(user.allLibraries)
  const [ids, setIds] = useState<Set<number>>(new Set(user.libraryIds ?? []))
  const [error, setError] = useState('')
  const toggle = (id: number) =>
    setIds((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/libraries`, { all, libraryIds: [...ids] })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Libraries for ${user.username}`} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label className="check">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
          All libraries, including ones added later
        </label>
        <div className={`check-list ${all ? 'disabled' : ''}`}>
          {libraries?.map((l) => (
            <label key={l.id} className="check">
              <input type="checkbox" disabled={all} checked={all || ids.has(l.id)} onChange={() => toggle(l.id)} />
              <Icon name={l.kind === 'movies' ? 'film' : 'tv'} size={16} /> {l.name}
            </label>
          ))}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}

function AddUser({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.post('/api/admin/users', { username, password, isAdmin })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title="Add user" onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} required minLength={2} autoFocus />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        <label className="check">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Admin (can manage libraries, users and settings)
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Add user</button>
        </div>
      </form>
    </Modal>
  )
}

function ResetPassword({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/password`, { password })
      onClose()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Reset password for ${user.username}`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <label>
          New password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoFocus autoComplete="new-password" />
        </label>
        <p className="muted small">They will be signed out on all devices.</p>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Reset password</button>
        </div>
      </form>
    </Modal>
  )
}
