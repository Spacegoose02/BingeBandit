import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api'
import { useApi, useDocumentTitle } from '../../hooks'
import type { User } from '../../types'

type Source = 'plex' | 'tautulli' | 'tautulli-db' | 'cinemask'

interface SourceUser {
  name: string
  plays?: number
  suggest: number
}

interface Connected {
  users: SourceUser[]
  collections: boolean
  watchedState: boolean
  artwork?: boolean
  labels?: boolean
}

interface ImportStatus {
  running: boolean
  source: string
  dryRun: boolean
  phase: string
  done: number
  total: number
  startedAt: number
  finishedAt: number
  seen: number
  matched: number
  plays: number
  duplicates: number
  watched: number
  resumes: number
  skippedUser: number
  collections: number
  collectionItems: number
  artwork: number
  artworkFailed: number
  labels: number
  unmatched: string[]
  unmatchedCount: number
  error?: string
}

const sourceNames: Record<Source, string> = { plex: 'Plex', tautulli: 'Tautulli', 'tautulli-db': 'Tautulli database file', cinemask: 'Cinemask collections' }

export default function AdminImport() {
  useDocumentTitle('Import')
  const [source, setSource] = useState<Source>('plex')
  const [url, setUrl] = useState('')
  const [token, setToken] = useState('')
  const [path, setPath] = useState('')
  const [connected, setConnected] = useState<Connected | null>(null)
  const [map, setMap] = useState<Record<string, number>>({})
  const [opts, setOpts] = useState({ history: true, watched: true, collections: true, artwork: true, labels: true })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState<ImportStatus | null>(null)
  const { data: users, reload: reloadUsers } = useApi<User[]>('/api/admin/users')

  const body = () => ({ source, url, token, path })

  // Show the last import, and follow one that's running.
  useEffect(() => {
    let timer = 0
    let stopped = false
    const poll = async () => {
      try {
        const s = await api.get<ImportStatus>('/api/admin/import/status')
        if (stopped) return
        setStatus(s.startedAt ? s : null)
        if (s.running) timer = window.setTimeout(poll, 1000)
      } catch {
        /* ignore */
      }
    }
    poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [status?.running])

  const connect = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    setConnected(null)
    try {
      const res = await api.post<Connected>('/api/admin/import/connect', body())
      setConnected(res)
      setMap(Object.fromEntries(res.users.map((u) => [u.name, u.suggest])))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const start = async (dryRun: boolean) => {
    setError('')
    try {
      const s = await api.post<ImportStatus>('/api/admin/import/start', {
        ...body(),
        history: opts.history && !collectionsOnly,
        watched: opts.watched && !collectionsOnly,
        collections: opts.collections && !!connected?.collections,
        artwork: opts.artwork && !!connected?.artwork,
        labels: opts.labels && !!connected?.labels,
        dryRun,
        users: map,
      })
      setStatus(s)
      if (!dryRun && Object.values(map).includes(-1)) {
        // The new users exist now; point the mapping at them so running it again doesn't make more.
        const res = await api.post<{ users: SourceUser[] }>('/api/admin/import/connect', body())
        setMap((m) => Object.fromEntries(Object.entries(m).map(([k, v]) => [k, v === -1 ? res.users.find((u) => u.name === k)?.suggest || -1 : v])))
        reloadUsers()
      }
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const switchSource = (s: Source) => {
    setSource(s)
    setConnected(null)
    setError('')
  }
  const mapped = Object.values(map).filter((v) => v !== 0).length
  // Cinemask only has collections: no users to map, no history.
  const collectionsOnly = source === 'cinemask'
  const running = !!status?.running

  return (
    <div className="page narrow">
      <header className="page-head">
        <div>
          <h1>Import</h1>
          <div className="muted">Bring watch history, watched marks, collections and posters over from Plex or Tautulli, or collections from Cinemask.</div>
        </div>
      </header>

      {status && <ImportResult status={status} />}

      <form className="panel form" onSubmit={connect} style={{ marginBottom: 20 }}>
        <h3>1. Where from</h3>
        <div className="segmented" role="tablist">
          {(Object.keys(sourceNames) as Source[]).map((s) => (
            <button key={s} type="button" className={source === s ? 'active' : ''} onClick={() => switchSource(s)}>
              {sourceNames[s]}
            </button>
          ))}
        </div>
        {source === 'tautulli-db' ? (
          <>
            <label>
              Path to tautulli.db on this server
              <input className="mono" value={path} onChange={(e) => setPath(e.target.value)} required placeholder="/config/tautulli.db" spellCheck={false} />
            </label>
            <p className="muted small">
              Works after Plex and Tautulli are switched off. Copy <code>tautulli.db</code> from Tautulli's data folder somewhere BingeBandit can read
              it. It isn't changed.
            </p>
          </>
        ) : (
          <>
            <label>
              {{ plex: 'Plex server address', cinemask: 'Cinemask address' }[source as string] ?? 'Tautulli address'}
              <input
                className="mono"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                required
                placeholder={{ plex: 'http://192.168.1.10:32400', cinemask: 'http://192.168.1.10:3000' }[source as string] ?? 'http://192.168.1.10:8181'}
                spellCheck={false}
              />
            </label>
            <label>
              {{ plex: 'Plex token', cinemask: 'Cinemask API key' }[source as string] ?? 'Tautulli API key'}
              <input className="mono" type="password" value={token} onChange={(e) => setToken(e.target.value)} required autoComplete="off" />
            </label>
            <p className="muted small">
              {source === 'plex' ? (
                <>
                  Sign in to Plex Web as the server owner, open any movie, choose ⋯ → Get Info → View XML, and copy the{' '}
                  <code>X-Plex-Token=</code> value from the address bar. It's only used for this import and not saved.
                </>
              ) : source === 'cinemask' ? (
                <>
                  In Cinemask: your profile → API keys → New key. Collections are matched by TMDB id, so every title in your libraries is found. The key is
                  only used for this import and not saved.
                </>
              ) : (
                <>In Tautulli: Settings → Web Interface → API. Turn the API on and copy the key. It's only used for this import and not saved.</>
              )}
            </p>
          </>
        )}
        {error && !connected && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy || running}>{busy ? 'Connecting…' : 'Connect'}</button>
        </div>
      </form>

      {connected && (
        <div className="panel form">
          {!collectionsOnly && (
            <>
              <h3>2. Whose history goes where</h3>
              {connected.users.length === 0 ? (
                <p className="muted">No users with history were found.</p>
              ) : (
                <div className="list">
                  {connected.users.map((u) => (
                    <div key={u.name} className="list-row">
                      <div className="grow">
                        <div>{u.name}</div>
                        {u.plays !== undefined && <div className="muted small">{u.plays} plays</div>}
                      </div>
                      <select
                        value={map[u.name] ?? 0}
                        onChange={(e) => setMap({ ...map, [u.name]: Number(e.target.value) })}
                        aria-label={`BingeBandit user for ${u.name}`}
                        style={{ width: 'auto', minWidth: 220 }}
                      >
                        <option value={0}>Skip</option>
                        <option value={-1}>Create a new user</option>
                        {users?.map((b) => (
                          <option key={b.id} value={b.id}>
                            {b.username}
                          </option>
                        ))}
                      </select>
                    </div>
                  ))}
                </div>
              )}
              <p className="muted small">
                New users get no password: link a TV for them under Link a device, or set a password under Users.
              </p>
            </>
          )}
          <h3>{collectionsOnly ? '2.' : '3.'} What to bring over</h3>
          {!collectionsOnly && (
            <>
              <label className="check">
                <input type="checkbox" checked={opts.history} onChange={(e) => setOpts({ ...opts, history: e.target.checked })} />
                Watch history (shows in profiles, statistics and history)
              </label>
              <label className="check">
                <input type="checkbox" checked={opts.watched} onChange={(e) => setOpts({ ...opts, watched: e.target.checked })} />
                Watched marks{connected.watchedState && ' and resume points (Plex reports these for the server owner only)'}
              </label>
            </>
          )}
          {connected.collections && (
            <label className="check">
              <input type="checkbox" checked={opts.collections} onChange={(e) => setOpts({ ...opts, collections: e.target.checked })} />
              Collections (added to the matching library; existing collections with the same name get the new items)
            </label>
          )}
          {connected.labels && (
            <label className="check">
              <input type="checkbox" checked={opts.labels} onChange={(e) => setOpts({ ...opts, labels: e.target.checked })} />
              Labels (the ones Kometa and Cinemask put on titles; a collection with a label rule gathers everything carrying it)
            </label>
          )}
          {connected.artwork && (
            <label className="check">
              <input type="checkbox" checked={opts.artwork} onChange={(e) => setOpts({ ...opts, artwork: e.target.checked })} />
              {collectionsOnly ? (
                'Collection posters (replaces the poster here)'
              ) : (
                <>
                  Posters and backgrounds for movies, shows, seasons{connected.collections && ' and collections'} (replaces the artwork here and keeps it
                  through metadata refreshes, so posters you picked in Plex or set with Kometa come along)
                </>
              )}
            </label>
          )}
          <p className="muted small">
            Items are matched by file name, then TMDB id, then title and year. Anything already imported, or a viewing BingeBandit already has, is skipped,
            so running it again is safe.
          </p>
          {error && <div className="form-error">{error}</div>}
          <div className="actions">
            <button type="button" className="btn btn-glass" disabled={running} onClick={() => start(true)}>
              Preview
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={running || (mapped === 0 && !(opts.collections && connected.collections) && !(opts.artwork && connected.artwork) && !(opts.labels && connected.labels))}
              onClick={() => start(false)}
            >
              Import
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function ImportResult({ status: s }: { status: ImportStatus }) {
  const pct = s.total > 0 ? Math.min(100, Math.round((s.done / s.total) * 100)) : 0
  const cancel = () => api.post('/api/admin/import/cancel').catch(() => undefined)
  const title = s.running ? (s.dryRun ? 'Previewing…' : 'Importing…') : s.dryRun ? 'Preview' : 'Last import'
  const tiles: [string, number, string][] = [
    ['Read', s.seen, ''],
    ['Matched', s.matched, s.seen ? `${Math.round((s.matched / s.seen) * 100)}%` : ''],
    [s.dryRun ? 'Plays to add' : 'Plays added', s.plays, s.duplicates ? `${s.duplicates} already here` : ''],
    [s.dryRun ? 'To mark watched' : 'Marked watched', s.watched, ''],
    ['Resume points', s.resumes, ''],
    ['Collections', s.collections, s.collectionItems ? `${s.collectionItems} items` : ''],
    ['Labels', s.labels ?? 0, ''],
  ]
  if (s.artwork || s.artworkFailed) tiles.push([s.dryRun ? 'Images to bring over' : 'Images brought over', s.artwork, s.artworkFailed ? `${s.artworkFailed} failed` : ''])
  return (
    <section className="panel" style={{ marginBottom: 20 }} aria-live="polite">
      <header className="panel-head">
        <h3>
          {title} <span className="muted small">from {sourceNames[s.source as Source] ?? s.source}</span>
        </h3>
        {s.running && (
          <button className="btn btn-glass btn-sm" onClick={cancel}>Cancel</button>
        )}
      </header>
      {s.running ? (
        <div className="import-progress">
          <div className="muted small">
            {s.phase}
            {s.total > 0 && ` · ${s.done} of ${s.total}`}
          </div>
          <div className="progress-track" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div style={{ width: s.total > 0 ? `${pct}%` : '30%' }} className={s.total > 0 ? '' : 'indeterminate'} />
          </div>
        </div>
      ) : (
        <div className="muted small" style={{ marginBottom: 12 }}>
          {s.error ? <span className="form-error">Stopped: {s.error}</span> : `Finished ${new Date(s.finishedAt * 1000).toLocaleString()}`}
          {s.dryRun && !s.error && '. Nothing was changed; this is what an import would do.'}
        </div>
      )}
      <div className="stat-grid">
        {tiles.map(([label, value, sub]) => (
          <div key={label} className="stat">
            <div className="stat-value">{value.toLocaleString()}</div>
            <div className="stat-label">
              {label}
              {sub && <> · {sub}</>}
            </div>
          </div>
        ))}
      </div>
      {s.skippedUser > 0 && <p className="muted small">{s.skippedUser} records belonged to users you chose to skip.</p>}
      {s.unmatchedCount > 0 && (
        <details className="unmatched">
          <summary>
            {s.unmatchedCount} title{s.unmatchedCount === 1 ? '' : 's'} not found in your libraries
          </summary>
          <p className="muted small">Most-watched first. Add them to a library (or fix their match) and run the import again to pick them up.</p>
          <ul>
            {s.unmatched.map((t) => (
              <li key={t}>{t}</li>
            ))}
            {s.unmatchedCount > s.unmatched.length && <li className="muted">…and {s.unmatchedCount - s.unmatched.length} more</li>}
          </ul>
        </details>
      )}
    </section>
  )
}
