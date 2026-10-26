import { useEffect, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, ApiError } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { passkeysSupported } from '../passkeys'
import { donePicking } from '../profilePick'
import type { User } from '../types'
import { AuthFrame } from './Setup'

export default function Login() {
  const { login, loginWithPasskey, serverName } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [needsCode, setNeedsCode] = useState(false)
  const [withCode, setWithCode] = useState(false)
  const [error, setError] = useState(() => new URLSearchParams(loc.search).get('sso_error') ?? '')
  const [busy, setBusy] = useState(false)
  const [sso, setSSO] = useState<{ id: number; name: string }[]>([])
  useEffect(() => {
    api.get<{ id: number; name: string }[]>('/api/auth/oidc').then(setSSO, () => undefined)
  }, [])
  const next = (loc.state as { from?: string } | null)?.from ?? '/'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await login(username, password, needsCode ? code : undefined)
      nav(next, { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.data?.needsCode) {
        // Only show the message once they've actually tried a code.
        if (needsCode) setError(err.message)
        setNeedsCode(true)
        setCode('')
      } else {
        setError((err as Error).message)
      }
    } finally {
      setBusy(false)
    }
  }

  const passkey = async () => {
    setBusy(true)
    setError('')
    try {
      if (await loginWithPasskey()) nav(next, { replace: true })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (withCode) {
    return (
      <AuthFrame subtitle={`Sign in to ${serverName}`}>
        <LinkCode onCancel={() => setWithCode(false)} />
      </AuthFrame>
    )
  }

  return (
    <AuthFrame subtitle={`Sign in to ${serverName}`}>
      <form onSubmit={submit} className="form">
        {needsCode ? (
          <label>
            Code from your authenticator app
            <input
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoFocus
              required
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
            />
            <span className="muted small">Lost your phone? Enter one of your recovery codes instead.</span>
          </label>
        ) : (
          <>
            <label>
              Username
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus required autoComplete="username" />
            </label>
            <label>
              Password
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="current-password" />
            </label>
          </>
        )}
        {error && <div className="form-error">{error}</div>}
        <button className="btn btn-primary btn-block" disabled={busy}>
          {busy ? 'Signing in…' : needsCode ? 'Verify' : 'Sign in'}
        </button>
        {needsCode ? (
          <button type="button" className="btn btn-glass btn-block" onClick={() => { setNeedsCode(false); setError('') }}>
            Back
          </button>
        ) : (
          <>
            {passkeysSupported() && (
              <button type="button" className="btn btn-glass btn-block" onClick={passkey} disabled={busy}>
                <Icon name="key" size={18} /> Sign in with a passkey
              </button>
            )}
            <button type="button" className="btn btn-glass btn-block" onClick={() => setWithCode(true)}>
              Sign in with a code instead
            </button>
            {sso.length > 0 && <div className="or-divider">or</div>}
            {sso.map((p) => (
              <a key={p.id} className="btn btn-glass btn-block" href={`/api/auth/oidc/${p.id}/login?next=${encodeURIComponent(next)}`}>
                Continue with {p.name}
              </a>
            ))}
          </>
        )}
      </form>
    </AuthFrame>
  )
}

/** Shows a code to enter on a signed-in phone or computer — for TVs, where typing a password is painful. */
function LinkCode({ onCancel }: { onCancel: () => void }) {
  const { signedIn } = useAuth()
  const [link, setLink] = useState<{ code: string; pollToken: string; linkUrl: string } | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let stopped = false
    let timer = 0
    setError('')
    setLink(null)
    api
      .post<{ code: string; pollToken: string; linkUrl: string }>('/api/link/start', { device: 'Web (linked)' })
      .then((l) => {
        if (stopped) return
        setLink(l)
        const poll = async () => {
          try {
            const res = await api.get<{ user: User; profileChosen?: boolean } | undefined>(`/api/link/poll?web=1&token=${l.pollToken}`)
            if (stopped) return
            if (res) {
              if (res.profileChosen) donePicking() // linked straight to a profile: no "Who's watching?"
              signedIn(res.user)
              return
            }
            timer = window.setTimeout(poll, 2000)
          } catch (err) {
            if (!stopped) setError((err as Error).message)
          }
        }
        timer = window.setTimeout(poll, 2000)
      })
      .catch((err) => !stopped && setError((err as Error).message))
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [attempt, signedIn])

  return (
    <div className="form link-code">
      <p className="muted">
        On a phone or computer that's already signed in, go to
        <br />
        <strong>{link ? link.linkUrl.replace(/\?code=.*$/, '') : '…'}</strong>
        <br />
        and enter this code:
      </p>
      <div className="link-code-value" aria-live="polite">
        {link ? `${link.code.slice(0, 3)}-${link.code.slice(3)}` : '······'}
      </div>
      {error ? (
        <>
          <div className="form-error">{error}</div>
          <button type="button" className="btn btn-primary btn-block" onClick={() => setAttempt((a) => a + 1)}>
            Get a new code
          </button>
        </>
      ) : (
        <p className="muted small">Waiting for you to approve it… The code works for 10 minutes.</p>
      )}
      <button type="button" className="btn btn-glass btn-block" onClick={onCancel}>
        Use a password instead
      </button>
    </div>
  )
}
