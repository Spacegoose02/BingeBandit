import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api'
import { Modal } from '../../components/Modal'

interface PlanItem {
  name: string
  detail?: string
  skip?: string
}

interface Plan {
  accounts: PlanItem[]
  guides: PlanItem[]
  channels: number
  groups: number
}

interface Status {
  running: boolean
  phase: string
  startedAt: number
  sources: number
  channels: number
  existing: number
  notes: string[]
  error?: string
}

// DispatcharrImport copies provider accounts, guides and channels from a
// Dispatcharr server. The login is only used for the import.
export function DispatcharrImport({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const [form, setForm] = useState({ url: '', username: '', password: '', apiKey: '' })
  const [useKey, setUseKey] = useState(false)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [status, setStatus] = useState<Status | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const body = () => (useKey ? { url: form.url, apiKey: form.apiKey } : { url: form.url, username: form.username, password: form.password })

  // Follow a running import (also one started before the dialog opened).
  useEffect(() => {
    let timer = 0
    let stopped = false
    const poll = async () => {
      try {
        const s = await api.get<Status>('/api/admin/live/dispatcharr/import')
        if (stopped) return
        if (s.startedAt) setStatus(s)
        if (s.running) timer = window.setTimeout(poll, 1000)
        else if (status?.running) onChanged()
      } catch {
        /* ignore */
      }
    }
    poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.running])

  const connect = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    setPlan(null)
    try {
      setPlan(await api.post<Plan>('/api/admin/live/dispatcharr/preview', body()))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const start = async () => {
    setBusy(true)
    setError('')
    try {
      setStatus(await api.post<Status>('/api/admin/live/dispatcharr/import', body()))
      setPlan(null)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const running = !!status?.running

  return (
    <Modal title="Import from Dispatcharr" onClose={onClose} wide>
      <div className="form">
        {status && (
          <div className="panel" style={{ padding: 14 }}>
            <div>
              <strong>{status.running ? `${status.phase}…` : status.error ? 'The import stopped' : 'Import finished'}</strong>
            </div>
            <div className="muted small">
              {status.sources} playlists and guides added · {status.channels} channels created
              {status.existing > 0 && ` · ${status.existing} channels were already here`}
            </div>
            {status.error && <div className="form-error small">{status.error}</div>}
            {status.notes.length > 0 && (
              <ul className="small" style={{ margin: '8px 0 0', paddingLeft: 18 }}>
                {status.notes.map((n, i) => <li key={i}>{n}</li>)}
              </ul>
            )}
          </div>
        )}

        {!plan && !running && (
          <form className="form" onSubmit={connect}>
            <p className="muted small" style={{ margin: 0 }}>
              Copies your provider accounts (with connection limits and user agents), guides, and channels with their
              numbers, groups, logos, guide matches and fallback streams. BingeBandit then connects to your providers
              itself. Your login is only used for this import and isn't saved. Running it again skips what's already here.
            </p>
            <label>
              Dispatcharr address
              <input className="mono" value={form.url} onChange={set('url')} required placeholder="http://192.168.1.10:9191" />
            </label>
            <div className="segmented">
              <button type="button" className={!useKey ? 'active' : ''} onClick={() => setUseKey(false)}>Username and password</button>
              <button type="button" className={useKey ? 'active' : ''} onClick={() => setUseKey(true)}>API key</button>
            </div>
            {useKey ? (
              <label>
                API key (an admin's)
                <input className="mono" value={form.apiKey} onChange={set('apiKey')} required />
              </label>
            ) : (
              <div className="field-grid">
                <label>
                  Admin username
                  <input value={form.username} onChange={set('username')} required autoComplete="off" />
                </label>
                <label>
                  Password
                  <input type="password" value={form.password} onChange={set('password')} required autoComplete="new-password" />
                </label>
              </div>
            )}
            {error && <div className="form-error">{error}</div>}
            <div className="actions end">
              <button type="button" className="btn btn-glass" onClick={onClose}>Close</button>
              <button className="btn btn-primary" disabled={busy}>{busy ? 'Connecting…' : 'Connect'}</button>
            </div>
          </form>
        )}

        {plan && !running && (
          <>
            <h3 style={{ margin: 0 }}>This will be imported</h3>
            <div className="panel list">
              {plan.accounts.map((a) => <PlanRow key={`a${a.name}`} item={a} />)}
              {plan.guides.map((g) => <PlanRow key={`g${g.name}`} item={g} />)}
              <div className="list-row">
                <div className="grow">
                  <div>{plan.channels.toLocaleString()} channels</div>
                  <div className="muted small">in {plan.groups} {plan.groups === 1 ? 'group' : 'groups'}, with their numbers, logos, guide matches and fallback streams</div>
                </div>
              </div>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Playlists and guides are read first, which can take a few minutes for big providers. Channels whose streams
              can't be found are skipped and listed when it's done.
            </p>
            {error && <div className="form-error">{error}</div>}
            <div className="actions end">
              <button className="btn btn-glass" onClick={() => setPlan(null)}>Back</button>
              <button className="btn btn-primary" disabled={busy} onClick={start}>{busy ? 'Starting…' : 'Import'}</button>
            </div>
          </>
        )}

        {running && (
          <div className="actions end">
            <span className="muted small grow">You can close this; the import keeps going.</span>
            <button className="btn btn-glass" onClick={onClose}>Close</button>
          </div>
        )}
      </div>
    </Modal>
  )
}

function PlanRow({ item }: { item: PlanItem }) {
  return (
    <div className="list-row">
      <div className="grow">
        <div>
          {item.name} {item.skip && <span className="pill">Skipped</span>}
        </div>
        <div className="muted small">{item.skip || item.detail}</div>
      </div>
    </div>
  )
}
