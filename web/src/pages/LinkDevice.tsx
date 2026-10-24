import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi, useDocumentTitle } from '../hooks'
import type { User } from '../types'

/** /link — approve a code shown on a TV (or another browser) to sign it in. */
export default function LinkDevice() {
  useDocumentTitle('Link a device')
  const { user } = useAuth()
  const [params] = useSearchParams()
  const [code, setCode] = useState(params.get('code') ?? '')
  const [userId, setUserId] = useState(0)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const { data: users } = useApi<User[]>(user?.isAdmin ? '/api/admin/users' : null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setMsg(null)
    try {
      const res = await api.post<{ device: string }>('/api/link/approve', { code, userId })
      const who = users?.find((u) => u.id === userId)?.username
      setMsg({ ok: true, text: `Done. The ${res.device} is now signed in${who && userId !== user?.id ? ` as ${who}` : ''}.` })
      setCode('')
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    } finally {
      setBusy(false)
    }
  }

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
        {user?.isAdmin && users && users.length > 1 && (
          <label>
            Sign it in as
            <select value={userId || user.id} onChange={(e) => setUserId(Number(e.target.value))}>
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                  {u.id === user.id ? ' (you)' : ''}
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
    </div>
  )
}
