import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi, useDocumentTitle } from '../hooks'
import type { Member, User } from '../types'

/**
 * /link — approve a code shown on a TV (or another browser) to sign it in,
 * as yourself, one of your profiles, or (admins) anyone. Picking a profile
 * makes the TV open as it; picking an account asks who's watching there.
 */
export default function LinkDevice() {
  useDocumentTitle('Link a device')
  const { user } = useAuth()
  const [params] = useSearchParams()
  const [code, setCode] = useState(params.get('code') ?? '')
  const [accountId, setAccountId] = useState(0)
  const [profileId, setProfileId] = useState(0)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const isAccount = !!user && !user.ownerId
  const { data: users } = useApi<User[]>(user?.isAdmin ? '/api/admin/users' : null)
  const { data: household } = useApi<Member[]>(isAccount && !user?.isAdmin ? '/api/household' : null)
  const { data: info } = useApi<{ serverUrl: string }>('/api/link/info')

  const account = accountId || user?.id || 0
  const accounts = user?.isAdmin ? (users ?? []).filter((u) => !u.ownerId) : []
  const profiles: { id: number; name: string }[] = user?.isAdmin
    ? (users ?? []).filter((u) => u.ownerId === account).map((u) => ({ id: u.id, name: u.profileName || u.username }))
    : isAccount
      ? (household ?? []).filter((m) => !m.isOwner).map((m) => ({ id: m.id, name: m.name }))
      : []

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setMsg(null)
    try {
      const res = await api.post<{ device: string; name: string }>('/api/link/approve', { code, userId: profileId || account })
      setMsg({
        ok: true,
        text: profileId
          ? `Done. The ${res.device} is now signed in as ${res.name}.`
          : `Done. The ${res.device} is now signed in${account !== user?.id ? ` as ${res.name}` : ''}.`,
      })
      setCode('')
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    } finally {
      setBusy(false)
    }
  }

  const host = info?.serverUrl.replace(/^https?:\/\//, '')
  return (
    <div className="page narrow">
      <h1>Link a device</h1>
      <p className="muted">Enter the code shown on your TV or other device to sign it in without typing a password.</p>
      <form className="panel form" onSubmit={submit}>
        <label>
          Code
          <input
            className="link-code-input"
            value={code}
            onChange={(e) => setCode(e.target.value.toUpperCase())}
            placeholder="ABC-123"
            autoFocus
            required
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        {accounts.length > 1 && (
          <label>
            Sign it in as
            <select
              value={account}
              onChange={(e) => {
                setAccountId(Number(e.target.value))
                setProfileId(0)
              }}
            >
              {accounts.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                  {u.id === user?.id ? ' (you)' : ''}
                </option>
              ))}
            </select>
          </label>
        )}
        {profiles.length > 0 && (
          <label>
            Profile
            <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
              <option value={0}>Ask who's watching</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  Always {p.name}
                </option>
              ))}
            </select>
          </label>
        )}
        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <button className="btn btn-primary" disabled={busy}>
          {busy ? 'Linking…' : 'Link device'}
        </button>
      </form>
      {host && (
        <div className="panel">
          <h3>Setting up a TV in another home?</h3>
          <p className="muted">
            Install the BingeBandit app on the TV. If it doesn't find this server by itself, choose <strong>Type an address</strong> and enter{' '}
            <strong className="mono">{host}</strong>. The TV then shows a code to enter above.
          </p>
        </div>
      )}
    </div>
  )
}
