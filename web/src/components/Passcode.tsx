import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Outlet } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { Icon } from './Icon'
import { Modal } from './Modal'

// The settings passcode: Account changes and the Admin pages answer 423 until
// the passcode (or the account password) is entered in this browser session.

interface Status {
  set: boolean
  unlocked: boolean
  unlockedUntil?: number
}

export const LOCKED_EVENT = 'bb:locked'
const UNLOCKED_EVENT = 'bb:unlocked'

// Gates on screen take care of a lapsed unlock themselves, so the pop-up
// prompt stays away while one is mounted.
let gates = 0

function PasscodeForm({ onUnlocked, hasPassword }: { onUnlocked: () => void; hasPassword: boolean }) {
  const [usePassword, setUsePassword] = useState(false)
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/api/me/passcode/unlock', usePassword ? { password: value } : { passcode: value })
      window.dispatchEvent(new Event(UNLOCKED_EVENT))
      onUnlocked()
    } catch (err) {
      setError((err as Error).message)
      setValue('')
    }
    setBusy(false)
  }

  return (
    <form className="form" onSubmit={submit}>
      <label>
        {usePassword ? 'Account password' : 'Settings passcode'}
        {usePassword ? (
          <input type="password" value={value} onChange={(e) => setValue(e.target.value)} autoFocus required autoComplete="current-password" />
        ) : (
          <input
            className="passcode-input"
            type="password"
            inputMode="numeric"
            pattern="[0-9]*"
            maxLength={8}
            value={value}
            onChange={(e) => setValue(e.target.value.replace(/\D/g, ''))}
            autoFocus
            required
            autoComplete="off"
          />
        )}
      </label>
      {error && <div className="form-error">{error}</div>}
      <div className="actions">
        <button className="btn btn-primary" disabled={busy || !value}>
          <Icon name="unlock" size={16} /> Unlock
        </button>
        {hasPassword && (
          <button type="button" className="btn btn-glass" onClick={() => { setUsePassword(!usePassword); setValue(''); setError('') }}>
            {usePassword ? 'Use the passcode' : 'Forgot it? Use your password'}
          </button>
        )}
      </div>
    </form>
  )
}

/** Route wrapper for Account and Admin: asks for the passcode before showing them. */
export function PasscodeGate({ children }: { children?: ReactNode }) {
  const { user } = useAuth()
  const [status, setStatus] = useState<Status | null>(null)
  const check = useCallback(() => api.get<Status>('/api/me/passcode').then(setStatus).catch(() => setStatus({ set: false, unlocked: true })), [])

  useEffect(() => {
    gates++
    check()
    const locked = () => setStatus((s) => (s ? { ...s, unlocked: false } : s))
    window.addEventListener(LOCKED_EVENT, locked)
    window.addEventListener(UNLOCKED_EVENT, check)
    return () => {
      gates--
      window.removeEventListener(LOCKED_EVENT, locked)
      window.removeEventListener(UNLOCKED_EVENT, check)
    }
  }, [check, user?.hasPasscode])

  if (!status) return null
  if (status.set && !status.unlocked) {
    return (
      <div className="page narrow">
        <div className="panel passcode-panel">
          <Icon name="lock" size={28} />
          <h2>Settings are locked</h2>
          <p className="muted">Enter your settings passcode to change your account or the server.</p>
          <PasscodeForm onUnlocked={check} hasPassword={!!user?.hasPassword} />
        </div>
      </div>
    )
  }
  return <>{children ?? <Outlet />}</>
}

/** Pop-up prompt for a locked action outside the gated pages (e.g. editing an item). */
export function PasscodePrompt() {
  const { user } = useAuth()
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const show = () => gates === 0 && setOpen(true)
    window.addEventListener(LOCKED_EVENT, show)
    return () => window.removeEventListener(LOCKED_EVENT, show)
  }, [])
  if (!open) return null
  return (
    <Modal title="Settings are locked" onClose={() => setOpen(false)}>
      <p className="muted">Enter your settings passcode, then try again.</p>
      <PasscodeForm onUnlocked={() => setOpen(false)} hasPassword={!!user?.hasPassword} />
    </Modal>
  )
}

/** Account section: set, change or remove the passcode, or lock now. */
export function PasscodeSettings() {
  const { user, refresh } = useAuth()
  const [value, setValue] = useState('')
  const [editing, setEditing] = useState(false)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  if (!user) return null

  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put('/api/me/passcode', { passcode: value })
      setMsg({ ok: true, text: user.hasPasscode ? 'Passcode changed.' : 'Passcode set. Other browsers and devices now need it to change settings.' })
      setValue('')
      setEditing(false)
      refresh()
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }
  const remove = async () => {
    if (!confirm('Remove the settings passcode? Anyone signed in as you can then change settings.')) return
    await api.del('/api/me/passcode').catch((e) => alert(e.message))
    setMsg({ ok: true, text: 'Passcode removed.' })
    refresh()
  }
  const lockNow = async () => {
    await api.post('/api/me/passcode/lock').catch(() => {})
    window.dispatchEvent(new Event(LOCKED_EVENT))
  }

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Settings passcode</h3>
      <p className="muted small">
        Lock your account settings{user.isAdmin && ' and the server admin pages'} behind a 4 to 8 digit passcode, so whoever uses this
        account can watch but not change anything. Once entered, settings stay unlocked for 15 minutes of use. Apps and API keys
        aren't affected.
      </p>
      {editing || !user.hasPasscode ? (
        <form onSubmit={save} className="field-row">
          <input
            className="passcode-input"
            type="password"
            inputMode="numeric"
            pattern="[0-9]{4,8}"
            minLength={4}
            maxLength={8}
            placeholder={user.hasPasscode ? 'New passcode' : 'Choose a passcode'}
            value={value}
            onChange={(e) => setValue(e.target.value.replace(/\D/g, ''))}
            autoComplete="off"
            required
          />
          <button className="btn btn-primary">{user.hasPasscode ? 'Change' : 'Set passcode'}</button>
          {editing && (
            <button type="button" className="btn btn-glass" onClick={() => setEditing(false)}>
              Cancel
            </button>
          )}
        </form>
      ) : (
        <div className="actions">
          <button className="btn btn-glass" onClick={lockNow}>
            <Icon name="lock" size={16} /> Lock now
          </button>
          <button className="btn btn-glass" onClick={() => setEditing(true)}>
            Change passcode
          </button>
          <button className="btn btn-glass danger" onClick={remove}>
            Remove
          </button>
        </div>
      )}
      {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
    </div>
  )
}
