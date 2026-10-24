import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { User } from '../../types'

export default function AdminUsers() {
  useDocumentTitle('Users')
  const { user: me } = useAuth()
  const { data: users, reload } = useApi<User[]>('/api/admin/users')
  const [adding, setAdding] = useState(false)
  const [resetting, setResetting] = useState<User | null>(null)

  const remove = async (u: User) => {
    if (!confirm(`Delete ${u.username}? Their watch history is removed too.`)) return
    await api.del(`/api/admin/users/${u.id}`).catch((e) => alert(e.message))
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
                {u.username} {u.id === me?.id && <span className="muted small">(you)</span>}
              </div>
              <div className="muted small">Joined {new Date(u.createdAt * 1000).toLocaleDateString()}</div>
            </div>
            {u.isAdmin && <span className="pill ok">Admin</span>}
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
    </div>
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
