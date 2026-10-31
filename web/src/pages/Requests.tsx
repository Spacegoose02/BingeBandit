import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useApi, useDocumentTitle } from '../hooks'
import type { RequestableTitle, TitleRequest } from '../types'

interface RequestList {
  requests: TitleRequest[]
  enabled: boolean
  canRequest: boolean
  reason?: string
  /** Admins only: the plugins that fetch approved requests. */
  handlers?: string[]
}

const statusPill: Record<TitleRequest['status'], [string, string]> = {
  pending: ['warn', 'Waiting for approval'],
  approved: ['', 'Approved'],
  sent: ['ok', 'Downloading'],
  failed: ['danger', 'Couldn’t fetch'],
  declined: ['danger', 'Declined'],
  available: ['ok', 'Ready to watch'],
}

const tmdbLink = (r: { kind: string; tmdbId: number }) => `https://www.themoviedb.org/${r.kind === 'show' ? 'tv' : 'movie'}/${r.tmdbId}`

const day = (unix: number) => new Date(unix * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

/**
 * Ask for a movie or show the server doesn't have. Admins also see every
 * request here and approve or decline them; approved ones go to Cinemask.
 */
export default function RequestsPage() {
  useDocumentTitle('Requests')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const admin = !!user?.isAdmin
  const tab = admin && params.get('tab') !== 'mine' ? 'all' : 'mine'
  const { data, reload } = useApi<RequestList>(tab === 'all' ? '/api/requests?all=1' : '/api/requests', 30000)
  const [q, setQ] = useState(params.get('q') ?? '')
  const [results, setResults] = useState<RequestableTitle[] | null>(null)
  const [searchError, setSearchError] = useState('')
  const [asking, setAsking] = useState<RequestableTitle | null>(null)

  useEffect(() => {
    const term = q.trim()
    const t = window.setTimeout(async () => {
      setParams((p) => {
        const next = new URLSearchParams(p)
        if (term) next.set('q', term)
        else next.delete('q')
        return next
      }, { replace: true })
      if (!term) {
        setResults(null)
        return
      }
      try {
        setResults(await api.get<RequestableTitle[]>(`/api/requests/search?q=${encodeURIComponent(term)}`))
        setSearchError('')
      } catch (e) {
        setSearchError((e as Error).message)
      }
    }, 350)
    return () => window.clearTimeout(t)
  }, [q, setParams])

  if (!data) return <div className="page-loading" />
  const pending = data.requests.filter((r) => r.status === 'pending')
  const rest = data.requests.filter((r) => r.status !== 'pending')

  const act = async (fn: () => Promise<unknown>) => {
    await fn().catch((e) => alert(e.message))
    reload()
  }
  const decline = (r: TitleRequest) => {
    const reason = prompt(`Decline "${r.title}"? You can say why (optional).`, '')
    if (reason === null) return
    act(() => api.post(`/api/admin/requests/${r.id}/decline`, { reason }))
  }

  const row = (r: TitleRequest) => {
    const [cls, label] = statusPill[r.status]
    const people = r.people > 1 ? `${r.requestedBy} and ${r.people - 1} more` : r.requestedBy
    return (
      <div key={r.id} className="list-row request-row">
        <div className="request-poster">{r.poster ? <img src={img(r.poster, 'w92')} alt="" loading="lazy" /> : <Icon name={r.kind === 'show' ? 'tv' : 'film'} />}</div>
        <div className="grow">
          <div>
            {r.title}
            {r.year ? <span className="muted"> ({r.year})</span> : null}
          </div>
          <div className="muted small">
            {[r.kind === 'show' ? 'Show' : 'Movie', (admin || r.people > 1) && people, day(r.createdAt)].filter(Boolean).join(' · ')}
          </div>
          {r.note && <div className="small">“{r.note}”</div>}
          {r.reason && <div className={`small ${r.status === 'failed' ? 'form-error' : 'muted'}`}>{r.reason}</div>}
        </div>
        <span className={`pill ${cls}`}>{label}</span>
        {admin && (
          <a className="btn btn-glass btn-sm" href={r.link || tmdbLink(r)} target="_blank" rel="noreferrer">
            {r.link ? 'Cinemask' : 'TMDB'}
          </a>
        )}
        {admin && tab === 'all' && ['pending', 'declined', 'failed'].includes(r.status) && (
          <button className="btn btn-primary btn-sm" onClick={() => act(() => api.post(`/api/admin/requests/${r.id}/approve`))}>
            {r.status === 'failed' ? 'Try again' : 'Approve'}
          </button>
        )}
        {admin && tab === 'all' && ['pending', 'approved', 'failed'].includes(r.status) && (
          <button className="btn btn-glass btn-sm danger" onClick={() => decline(r)}>Decline</button>
        )}
        {admin && tab === 'all' ? (
          <button
            className="btn btn-glass btn-sm danger"
            aria-label="Delete request"
            onClick={() => confirm(`Delete the request for "${r.title}"?`) && act(() => api.del(`/api/requests/${r.id}?all=1`))}
          >
            <Icon name="trash" size={16} />
          </button>
        ) : (
          r.mine && ['pending', 'declined', 'available'].includes(r.status) && (
            <button
              className="btn btn-glass btn-sm danger"
              onClick={() => act(() => api.del(`/api/requests/${r.id}`))}
            >
              {r.status === 'pending' ? 'Cancel' : 'Remove'}
            </button>
          )
        )}
      </div>
    )
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Requests</h1>
          <div className="muted">Can’t find a movie or show? Ask for it here. Once it’s approved and added, you’ll get a notification.</div>
        </div>
      </header>

      {admin && (
        <div className="tabs">
          <button className={tab === 'all' ? 'active' : ''} onClick={() => setParams({}, { replace: true })}>
            All requests {pending.length > 0 && tab === 'all' && <span className="count">{pending.length}</span>}
          </button>
          <button className={tab === 'mine' ? 'active' : ''} onClick={() => setParams({ tab: 'mine' }, { replace: true })}>
            Ask for something
          </button>
        </div>
      )}

      {tab === 'all' && data.handlers && data.handlers.length === 0 && (
        <div className="notice warn">
          Nothing fetches approved requests yet. Turn on the <Link to="/admin/plugins">Cinemask Upgrades plugin</Link> and set its
          Cinemask address and API key; approved requests wait until then.
        </div>
      )}
      {tab === 'all' && !data.enabled && (
        <div className="notice">Requests are turned off, so nobody can ask for new titles. Turn them on in <Link to="/admin/settings">Settings</Link>.</div>
      )}

      {tab === 'mine' && (
        <>
          {!data.canRequest ? (
            <div className="notice">You can’t request titles: {data.reason}.</div>
          ) : (
            <>
              <div className="search-box">
                <Icon name="search" size={22} />
                <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search for a movie or show" autoFocus />
              </div>
              {searchError && <div className="notice warn">{searchError}</div>}
              {results && results.length === 0 && <div className="empty">No matches for “{q}”.</div>}
              {results && results.length > 0 && (
                <div className="poster-grid request-results">
                  {results.map((r) => (
                    <RequestCard key={`${r.kind}:${r.tmdbId}`} title={r} onAsk={() => setAsking(r)} />
                  ))}
                </div>
              )}
            </>
          )}
        </>
      )}

      {tab === 'all' && (
        <>
          <h2 className="section-title">Waiting for approval <span className="count">{pending.length}</span></h2>
          <div className="panel list">
            {pending.map(row)}
            {pending.length === 0 && <div className="list-row muted">Nothing to approve.</div>}
          </div>
        </>
      )}
      <h2 className="section-title">
        {tab === 'all' ? 'Everything else' : 'Your requests'} <span className="count">{tab === 'all' ? rest.length : data.requests.length}</span>
      </h2>
      <div className="panel list">
        {(tab === 'all' ? rest : data.requests).map(row)}
        {(tab === 'all' ? rest : data.requests).length === 0 && <div className="list-row muted">No requests yet.</div>}
      </div>

      {asking && (
        <AskModal
          title={asking}
          onClose={() => setAsking(null)}
          onDone={(req) => {
            setResults((list) => list?.map((r) => (r.kind === asking.kind && r.tmdbId === asking.tmdbId ? { ...r, request: req } : r)) ?? null)
            setAsking(null)
            reload()
          }}
        />
      )}
    </div>
  )
}

function RequestCard({ title: r, onAsk }: { title: RequestableTitle; onAsk: () => void }) {
  const status = r.request && statusPill[r.request.status]
  return (
    <div className="poster-card">
      <div className="poster-frame">
        {r.poster ? <img src={img(r.poster, 'w342')} alt="" loading="lazy" /> : <div className="poster-fallback">{r.title}</div>}
      </div>
      <div className="card-title" title={r.title}>{r.title}</div>
      <div className="card-sub">{[r.year, r.kind === 'show' ? 'Show' : 'Movie'].filter(Boolean).join(' · ')}</div>
      <div className="request-action">
        {r.itemId ? (
          <Link className="btn btn-glass btn-sm" to={`/item/${r.itemId}`}><Icon name="play" fill size={14} /> On the server</Link>
        ) : r.onServer ? (
          <span className="pill">Already on the server</span>
        ) : status && !(r.request?.status === 'pending' && !r.request.mine) ? (
          <span className={`pill ${status[0]}`}>{status[1]}</span>
        ) : (
          <button className="btn btn-primary btn-sm" onClick={onAsk}>
            <Icon name="plus" size={14} /> {r.request ? 'Ask too' : 'Request'}
          </button>
        )}
      </div>
    </div>
  )
}

function AskModal({ title, onClose, onDone }: { title: RequestableTitle; onClose: () => void; onDone: (r: TitleRequest) => void }) {
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const send = async () => {
    setBusy(true)
    try {
      const { kind, tmdbId, title: name, year, poster, overview } = title
      onDone(await api.post<TitleRequest>('/api/requests', { kind, tmdbId, title: name, year, poster, overview, note }))
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <Modal title={`Request ${title.title}`} onClose={onClose}>
      <div className="form">
        {title.request && <p className="muted">{title.request.requestedBy} already asked for this. Asking too means you’ll hear when it’s added.</p>}
        {title.overview && <p className="muted small request-overview">{title.overview}</p>}
        <label>
          Note for the admin (optional)
          <input value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} placeholder="Season 1 is enough, or the 4K version" />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy} onClick={send}>Request</button>
          <button className="btn btn-glass" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </Modal>
  )
}
