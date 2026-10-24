import { useState, type FormEvent } from 'react'
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
  const { setup } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

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
      </form>
    </AuthFrame>
  )
}
