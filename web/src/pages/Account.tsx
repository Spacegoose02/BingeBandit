import { useEffect, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { IPTVLinks } from '../components/IPTVLinks'
import { Icon } from '../components/Icon'
import { PasscodeSettings } from '../components/Passcode'
import { useApi, useDocumentTitle } from '../hooks'
import { commonLanguages, languageName } from '../format'
import type { APIKey, DeviceSession, Identity, Prefs } from '../types'

function when(ts: number) {
  return ts ? new Date(ts * 1000).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : 'never'
}

/** Turns a browser user agent into "Chrome on Windows"; app-provided names pass through. */
function deviceName(d: string) {
  if (!d) return 'Unknown device'
  if (!d.startsWith('Mozilla/')) return d
  const browser = /Edg\//.test(d) ? 'Edge' : /Firefox\//.test(d) ? 'Firefox' : /Chrome\//.test(d) ? 'Chrome' : /Safari\//.test(d) ? 'Safari' : 'A browser'
  const os = /Windows/.test(d) ? 'Windows' : /Android/.test(d) ? 'Android' : /iPhone|iPad/.test(d) ? 'iOS' : /Mac OS X/.test(d) ? 'macOS' : /Linux/.test(d) ? 'Linux' : ''
  return os ? `${browser} on ${os}` : browser
}

function Sharing() {
  const { user, refresh } = useAuth()
  if (!user) return null
  const toggle = async (share: boolean) => {
    await api.put('/api/me/share-activity', { share }).catch((e) => alert(e.message))
    refresh()
  }
  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Sharing</h3>
      <label className="check">
        <input type="checkbox" checked={user.shareActivity} onChange={(e) => toggle(e.target.checked)} />
        Show other people on this server what I'm watching right now
      </label>
      <p className="muted small">They see the title and how far in you are, never your device or location.</p>
    </div>
  )
}

function TwoFactor() {
  const { user, refresh } = useAuth()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [recovery, setRecovery] = useState<string[] | null>(null)
  const [error, setError] = useState('')
  if (!user) return null

  const start = async () => {
    setError('')
    try {
      setSetup(await api.post<{ secret: string; uri: string }>('/api/me/2fa/setup'))
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const enable = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      const res = await api.post<{ recoveryCodes: string[] }>('/api/me/2fa/enable', { code })
      setRecovery(res.recoveryCodes)
      setSetup(null)
      setCode('')
      refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const disable = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await api.post('/api/me/2fa/disable', { password })
      setPassword('')
      setRecovery(null)
      refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Two-step sign-in</h3>
      {recovery && (
        <div className="recovery">
          <p>
            <strong>Two-step sign-in is on.</strong> Save these recovery codes somewhere safe. Each one works once if you lose
            your phone. They won't be shown again.
          </p>
          <ul className="recovery-codes">
            {recovery.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          <button type="button" className="btn btn-glass btn-sm" onClick={() => navigator.clipboard?.writeText(recovery.join('\n'))}>
            Copy codes
          </button>
        </div>
      )}
      {user.totpEnabled ? (
        <form className="form" onSubmit={disable}>
          {!recovery && <p className="muted">On. Signing in asks for a code from your authenticator app.</p>}
          <label>
            Password to turn it off
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="current-password" />
          </label>
          {error && <div className="form-error">{error}</div>}
          <div className="actions">
            <button className="btn btn-glass danger">Turn off two-step sign-in</button>
          </div>
        </form>
      ) : setup ? (
        <form className="form" onSubmit={enable}>
          <p className="muted">
            In your authenticator app (Google Authenticator, 1Password, Authy…), add an account with this key, or open the link
            on your phone.
          </p>
          <code className="totp-secret">{setup.secret.replace(/(.{4})/g, '$1 ').trim()}</code>
          <a className="small" href={setup.uri}>Open in authenticator app</a>
          <label>
            Then enter the 6-digit code it shows
            <input value={code} onChange={(e) => setCode(e.target.value)} required inputMode="numeric" autoComplete="one-time-code" placeholder="123456" />
          </label>
          {error && <div className="form-error">{error}</div>}
          <div className="actions">
            <button className="btn btn-primary">Turn on</button>
            <button type="button" className="btn btn-glass" onClick={() => setSetup(null)}>Cancel</button>
          </div>
        </form>
      ) : (
        <>
          <p className="muted">Off. Add a code from your phone on top of your password when you sign in.</p>
          {error && <div className="form-error">{error}</div>}
          <div className="actions">
            <button type="button" className="btn btn-primary" onClick={start}>Set up two-step sign-in</button>
          </div>
        </>
      )}
    </div>
  )
}

function LinkedAccounts() {
  const { data: providers } = useApi<{ id: number; name: string }[]>('/api/auth/oidc')
  const { data: linked, reload } = useApi<Identity[]>('/api/me/identities')
  const [params, setParams] = useSearchParams()
  const [error, setError] = useState(params.get('sso_error') ?? '')
  const justLinked = params.get('linked')
  if (!providers?.length && !linked?.length) return null

  const link = async (id: number) => {
    setError('')
    try {
      const { url } = await api.post<{ url: string }>(`/api/me/identities/${id}/link`)
      window.location.href = url
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const unlink = async (i: Identity) => {
    if (!confirm(`Unlink your ${i.providerName} account? You won't be able to sign in with it until you link it again.`)) return
    setError('')
    try {
      await api.del(`/api/me/identities/${i.id}`)
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const unlinked = (providers ?? []).filter((p) => !linked?.some((i) => i.providerId === p.id))

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Linked accounts</h3>
      <p className="muted small">Sign in with these instead of your password.</p>
      {justLinked && (
        <div className="form-ok">
          Your {justLinked} account is linked.{' '}
          <button type="button" className="link-btn" onClick={() => setParams({}, { replace: true })}>Dismiss</button>
        </div>
      )}
      {linked && linked.length > 0 && (
        <div className="list">
          {linked.map((i) => (
            <div key={i.id} className="list-row">
              <div className="grow">
                <div>{i.providerName}</div>
                <div className="muted small">
                  {i.email || 'No email shared'} · linked {when(i.createdAt)} · last used {when(i.lastUsed)}
                </div>
              </div>
              <button className="btn btn-glass btn-sm" onClick={() => unlink(i)}>Unlink</button>
            </div>
          ))}
        </div>
      )}
      {unlinked.length > 0 && (
        <div className="actions">
          {unlinked.map((p) => (
            <button key={p.id} type="button" className="btn btn-glass" onClick={() => link(p.id)}>
              Link {p.name}
            </button>
          ))}
        </div>
      )}
      {error && <div className="form-error">{error}</div>}
    </div>
  )
}

interface TraktState {
  configured: boolean
  account: { username: string; autoSync: boolean; connectedAt: number; lastSync: number; lastError?: string } | null
  pendingNew?: number
  pendingAll?: number
}

function Trakt() {
  const { user } = useAuth()
  const { data, reload } = useApi<TraktState>('/api/me/trakt')
  const [code, setCode] = useState<{ userCode: string; verificationUrl: string; interval: number } | null>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [busy, setBusy] = useState('')

  // Poll while a code is showing.
  useEffect(() => {
    if (!code) return
    let stopped = false
    let timer = 0
    const poll = async () => {
      try {
        const acc = await api.post<TraktState['account'] | undefined>('/api/me/trakt/poll')
        if (stopped) return
        if (acc) {
          setCode(null)
          setMsg({ ok: true, text: `Connected as ${acc.username}. New things you finish will be added to your Trakt history.` })
          reload()
          return
        }
        timer = window.setTimeout(poll, Math.max(code.interval, 1) * 1000)
      } catch (err) {
        if (!stopped) {
          setCode(null)
          setMsg({ ok: false, text: (err as Error).message })
        }
      }
    }
    timer = window.setTimeout(poll, Math.max(code.interval, 1) * 1000)
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [code, reload])

  if (!data) return null
  const act = async (label: string, f: () => Promise<string>) => {
    setBusy(label)
    setMsg(null)
    try {
      setMsg({ ok: true, text: await f() })
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    } finally {
      setBusy('')
      reload()
    }
  }
  const connect = () =>
    act('connect', async () => {
      setCode(await api.post('/api/me/trakt/connect'))
      return ''
    })
  const importHistory = () =>
    act('import', async () => {
      const s = await api.post<{ plays: number; duplicates: number; watched: number; seen: number; matched: number; unmatchedCount: number }>(
        '/api/me/trakt/import',
      )
      return `Read ${s.seen} plays from Trakt; ${s.matched} are in your libraries. Added ${s.plays} to your history${
        s.duplicates ? ` (${s.duplicates} were already here)` : ''
      } and marked ${s.watched} watched.${
        s.unmatchedCount ? ` ${s.unmatchedCount} ${s.unmatchedCount === 1 ? "title isn't" : "titles aren't"} on this server.` : ''
      }`
    })
  const push = (all: boolean) =>
    act('push', async () => {
      const r = await api.post<{ movies: number; episodes: number; notFound: number }>('/api/me/trakt/push', { all })
      return `Sent ${r.movies} movies and ${r.episodes} episodes to Trakt.${r.notFound ? ` Trakt didn't recognise ${r.notFound}.` : ''}`
    })
  const setAuto = async (autoSync: boolean) => {
    await api.put('/api/me/trakt', { autoSync }).catch((e) => alert(e.message))
    reload()
  }
  const disconnect = async () => {
    if (!confirm('Disconnect Trakt? Your Trakt history stays as it is.')) return
    await api.del('/api/me/trakt').catch((e) => alert(e.message))
    setMsg(null)
    reload()
  }
  const acc = data.account

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Trakt</h3>
      {!data.configured && !acc ? (
        <p className="muted small">
          Keep your Trakt history (and apps that read it, like Yamtrack) up to date with what you watch here.{' '}
          {user?.isAdmin ? (
            <>
              First add a Trakt app under <Link to="/admin/settings">Settings → Trakt</Link>.
            </>
          ) : (
            'Ask your server admin to set up Trakt first.'
          )}
        </p>
      ) : acc ? (
        <>
          <p className="muted small">
            Connected as <strong>{acc.username}</strong>
            {acc.lastSync > 0 && <> · last synced {when(acc.lastSync)}</>}
          </p>
          {acc.lastError && <div className="form-error">Last sync failed: {acc.lastError}</div>}
          <label className="check">
            <input type="checkbox" checked={acc.autoSync} onChange={(e) => setAuto(e.target.checked)} />
            Add what I finish here to my Trakt history automatically
          </label>
          <div className="actions">
            <button type="button" className="btn btn-glass" disabled={!!busy} onClick={importHistory}>
              {busy === 'import' ? 'Importing…' : 'Import my Trakt history'}
            </button>
            {(data.pendingAll ?? 0) > 0 && (
              <button type="button" className="btn btn-glass" disabled={!!busy} onClick={() => push(true)}>
                {busy === 'push' ? 'Sending…' : `Send ${data.pendingAll} plays to Trakt`}
              </button>
            )}
            <button type="button" className="btn btn-glass danger" onClick={disconnect}>Disconnect</button>
          </div>
          {(data.pendingAll ?? 0) > 0 && (
            <p className="muted small">
              {data.pendingAll} finished plays aren't on Trakt yet
              {(data.pendingNew ?? 0) < (data.pendingAll ?? 0) && ', mostly from before you connected (those are only sent when you ask)'}.
            </p>
          )}
        </>
      ) : code ? (
        <div className="link-code">
          <p className="muted">
            Go to{' '}
            <a href={code.verificationUrl} target="_blank" rel="noreferrer">
              {code.verificationUrl.replace(/^https?:\/\//, '')}
            </a>{' '}
            and enter
          </p>
          <div className="link-code-value">{code.userCode}</div>
          <p className="muted small">Waiting for you to approve it on Trakt…</p>
          <button type="button" className="btn btn-glass btn-sm" onClick={() => setCode(null)}>Cancel</button>
        </div>
      ) : (
        <>
          <p className="muted small">Keep your Trakt history (and apps that read it, like Yamtrack) up to date with what you watch here.</p>
          <div className="actions">
            <button type="button" className="btn btn-primary" disabled={!!busy} onClick={connect}>Connect Trakt</button>
          </div>
        </>
      )}
      {msg?.text && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
    </div>
  )
}

function Downloads() {
  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Download my history</h3>
      <p className="muted small">Everything you've watched here, with TMDB ids so other apps can match it.</p>
      <div className="actions">
        <a className="btn btn-glass" href="/api/me/export/history.csv" download>
          <Icon name="download" size={16} /> Watch history (CSV)
        </a>
        <a className="btn btn-glass" href="/api/me/export/letterboxd.csv" download>
          <Icon name="download" size={16} /> Movies for Letterboxd
        </a>
      </div>
      <p className="muted small">
        Letterboxd: Settings → Import &amp; Export → Import your data, then pick the file.
      </p>
    </div>
  )
}

function Devices() {
  const { data: sessions, reload } = useApi<DeviceSession[]>('/api/me/sessions')
  const revoke = async (s: DeviceSession) => {
    await api.del(`/api/me/sessions/${s.id}`).catch((e) => alert(e.message))
    reload()
  }
  return (
    <div className="panel" style={{ marginBottom: 20 }}>
      <header className="panel-head">
        <h3>Signed-in devices</h3>
        <Link to="/link" className="btn btn-glass btn-sm">
          <Icon name="tv" size={16} /> Link a TV
        </Link>
      </header>
      <div className="list">
        {sessions?.map((s) => (
          <div key={s.id} className="list-row">
            <div className="grow">
              <div>
                <span title={s.device}>{deviceName(s.device)}</span> {s.current && <span className="pill ok">This device</span>}
              </div>
              <div className="muted small">
                {s.ip} · last active {when(s.lastSeen)} · signed in {when(s.createdAt)}
              </div>
            </div>
            {!s.current && (
              <button className="btn btn-glass btn-sm" onClick={() => revoke(s)}>
                Sign out
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function APIKeys() {
  const { data: keys, reload } = useApi<APIKey[]>('/api/me/keys')
  const { data: channels } = useApi<unknown[]>('/api/live/channels')
  const [name, setName] = useState('')
  const [created, setCreated] = useState<string | null>(null)
  const [error, setError] = useState('')
  const create = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      const res = await api.post<{ key: string }>('/api/me/keys', { name })
      setCreated(res.key)
      setName('')
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const remove = async (k: APIKey) => {
    if (!confirm(`Delete the key "${k.name}"? Anything using it stops working.`)) return
    await api.del(`/api/me/keys/${k.id}`).catch((e) => alert(e.message))
    reload()
  }
  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>API keys</h3>
      <p className="muted small">
        For scripts, Home Assistant, or IPTV apps that load the playlist and guide URLs. A key acts as you, with your library
        access. Send it as <code>Authorization: Bearer …</code> or <code>?token=…</code>.
      </p>
      {created && (
        <div className="recovery">
          <p>Copy your new key now. It won't be shown again.</p>
          <code className="totp-secret">{created}</code>
          <div className="actions">
            <button type="button" className="btn btn-glass btn-sm" onClick={() => navigator.clipboard?.writeText(created)}>Copy</button>
            <button type="button" className="btn btn-glass btn-sm" onClick={() => setCreated(null)}>Done</button>
          </div>
        </div>
      )}
      {keys && keys.length > 0 && (
        <div className="list">
          {keys.map((k) => (
            <div key={k.id} className="list-row">
              <div className="grow">
                <div>{k.name}</div>
                <div className="muted small">
                  <code>{k.prefix}…</code> · created {when(k.createdAt)} · last used {when(k.lastUsed)}
                </div>
              </div>
              <button className="btn btn-glass btn-sm danger" onClick={() => remove(k)} aria-label={`Delete ${k.name}`}>
                <Icon name="trash" size={16} />
              </button>
            </div>
          ))}
        </div>
      )}
      <form className="inline-form" onSubmit={create}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Key name, e.g. TiviMate" required />
        <button className="btn btn-primary">Create key</button>
      </form>
      {error && <div className="form-error">{error}</div>}
      {!!channels?.length && (
        <>
          <h3 style={{ marginTop: 20 }}>Live TV in other apps</h3>
          <p className="muted small">Your own playlist and guide, with the channels you can watch.</p>
          <IPTVLinks onCreated={reload} />
        </>
      )}
    </div>
  )
}

function PlaybackPrefs() {
  const { data } = useApi<Prefs>('/api/me/prefs')
  const [p, setP] = useState<Prefs | null>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  useEffect(() => setP(data), [data])
  if (!p) return null

  const set = <K extends keyof Prefs>(k: K, v: Prefs[K]) => setP({ ...p, [k]: v })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      setP(await api.put<Prefs>('/api/me/prefs', p))
      setMsg({ ok: true, text: 'Playback settings saved.' })
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }
  const langSelect = (value: string, onChange: (v: string) => void, emptyLabel: string) => (
    <select value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{emptyLabel}</option>
      {commonLanguages.map((l) => (
        <option key={l} value={l}>{languageName(l)}</option>
      ))}
    </select>
  )

  return (
    <form className="panel form" onSubmit={save} style={{ marginBottom: 20 }}>
      <h3>Playback</h3>
      <div className="field-grid">
        <label>
          Preferred audio language
          {langSelect(p.audioLanguage, (v) => set('audioLanguage', v), "File's default track")}
        </label>
        <label>
          Subtitle language
          {langSelect(p.subtitleLanguage, (v) => set('subtitleLanguage', v), 'None')}
        </label>
        <label className="span-2">
          Show subtitles
          <select value={p.subtitleMode} onChange={(e) => set('subtitleMode', e.target.value as Prefs['subtitleMode'])}>
            <option value="auto">When the audio is in another language (plus forced subtitles)</option>
            <option value="always">Always</option>
            <option value="forced">Only forced subtitles (foreign dialogue, signs)</option>
            <option value="off">Never</option>
          </select>
        </label>
        <label>
          Subtitle size
          <select value={p.subtitleSize} onChange={(e) => set('subtitleSize', e.target.value as Prefs['subtitleSize'])}>
            <option value="small">Small</option>
            <option value="medium">Medium</option>
            <option value="large">Large</option>
          </select>
        </label>
        <label className="check" style={{ alignSelf: 'end' }}>
          <input type="checkbox" checked={p.subtitleBackground} onChange={(e) => set('subtitleBackground', e.target.checked)} />
          Dark box behind subtitles
        </label>
      </div>
      <div className="field-grid">
        <label>
          Intros
          <select value={p.skipIntro} onChange={(e) => set('skipIntro', e.target.value as Prefs['skipIntro'])}>
            <option value="button">Show a Skip Intro button</option>
            <option value="auto">Skip automatically</option>
            <option value="off">Don't offer to skip</option>
          </select>
        </label>
        <label>
          End credits
          <select value={p.skipCredits} onChange={(e) => set('skipCredits', e.target.value as Prefs['skipCredits'])}>
            <option value="button">Show a Next button</option>
            <option value="auto">Go to the next episode automatically</option>
            <option value="off">Don't offer to skip</option>
          </select>
        </label>
      </div>
      <label className="check">
        <input type="checkbox" checked={p.autoQuality} onChange={(e) => set('autoQuality', e.target.checked)} />
        Lower the quality automatically when playback keeps buffering
      </label>
      <label className="check">
        <input type="checkbox" checked={p.autoPlayNext} onChange={(e) => set('autoPlayNext', e.target.checked)} />
        Play the next episode automatically
      </label>
      {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
      <div className="actions">
        <button className="btn btn-primary">Save playback settings</button>
      </div>
    </form>
  )
}

export default function Account() {
  useDocumentTitle('Account')
  const { user, refresh } = useAuth()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const hasPassword = user?.hasPassword ?? true

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put('/api/me/password', { current, new: next })
      setMsg({ ok: true, text: hasPassword ? 'Password changed.' : 'Password set. You can now sign in with it too.' })
      setCurrent('')
      setNext('')
      if (!hasPassword) refresh()
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }

  return (
    <div className="page narrow">
      <h1>Account</h1>
      <p className="muted">
        Signed in as <strong>{user?.username}</strong>
        {user?.isAdmin && ' (admin)'}
      </p>
      <PlaybackPrefs />
      <Sharing />
      <Trakt />
      <Downloads />
      <LinkedAccounts />
      {/* Two-step sign-in guards the password; single sign-on accounts rely on their provider's. */}
      {hasPassword && <TwoFactor />}
      <PasscodeSettings />
      <Devices />
      <APIKeys />
      <form className="panel form" onSubmit={submit}>
        <h3>{hasPassword ? 'Change password' : 'Set a password'}</h3>
        {hasPassword ? (
          <label>
            Current password
            <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
          </label>
        ) : (
          <p className="muted small">Your account was made by single sign-on. Set a password to also sign in with your username, for example on a TV.</p>
        )}
        <label>
          New password
          <input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <button className="btn btn-primary">{hasPassword ? 'Update password' : 'Set password'}</button>
      </form>
    </div>
  )
}
