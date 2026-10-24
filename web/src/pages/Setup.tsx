import { useState, type FormEvent } from 'react'
import { uploadBackup } from '../api'
import { useAuth } from '../auth'

export function AuthFrame({ children, subtitle }: { children: React.ReactNode; subtitle: string }) {
  return (
    <div className="auth-page">
      <div className="auth-card">
        <img src="/brand/stacked-light.png" alt="BingeBandit" className="auth-logo" />
        <p className="auth-sub">{subtitle}</p>
        {children}
      </div>
    </div>
  )
}

export default function Setup() {
  const { setup, refresh } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [restored, setRestored] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== confirm) return setError('Passwords do not match')
    setBusy(true)
    setError('')
    try {
      await setup(username, password)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const restore = async (file: File) => {
    if (!window.confirm(`Set up this server from ${file.name}? Everyone signs in with the accounts from that server.`)) return
    setBusy(true)
    setError('')
    try {
      await uploadBackup('/api/setup/restore', file)
      setRestored(true)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (restored) {
    return (
      <AuthFrame subtitle="Restored from your backup.">
        <div className="form">
          <p className="muted">
            Sign in with an admin account from your old server. If your media is in different folders here, fix the library folders in Settings, then
            restart the server so folder watching and Live TV pick up the restored settings.
          </p>
          <button className="btn btn-primary btn-block" onClick={() => refresh()}>
            Continue to sign in
          </button>
        </div>
      </AuthFrame>
    )
  }

  return (
    <AuthFrame subtitle="Welcome! Create the admin account for this server.">
      <form onSubmit={submit} className="form">
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus required minLength={2} autoComplete="username" />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        <label>
          Confirm password
          <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        {error && <div className="form-error">{error}</div>}
        <button className="btn btn-primary btn-block" disabled={busy}>
          {busy ? 'Creating…' : 'Create admin account'}
        </button>
        <label className={`btn btn-glass btn-block${busy ? ' disabled' : ''}`}>
          Moving from another server? Restore a backup
          <input
            type="file"
            accept=".db,application/vnd.sqlite3,application/x-sqlite3"
            hidden
            disabled={busy}
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (f) restore(f)
            }}
          />
        </label>
      </form>
    </AuthFrame>
  )
}
