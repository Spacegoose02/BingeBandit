import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { RequestCard, RequestModal, RequestStatus, seasonText } from '../components/RequestKit'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import type { ProblemReport, RequestableTitle, RequestLimit, TitleRequest } from '../types'

interface RequestList {
  requests: TitleRequest[]
  enabled: boolean
  canRequest: boolean
  reason?: string
  limits: { movie: RequestLimit; show: RequestLimit }
  fourK: boolean
  /** The viewer's Trakt watchlist is requested for them. */
  watchlist: boolean
  /** An admin set up Trakt. */
  trakt: boolean
  /** The viewer is a kid whose requests go to a parent. */
  kidsWait: boolean
  /** A parent's kids' requests waiting for them. */
  household: TitleRequest[]
  /** Admins only: the plugins that fetch approved requests. */
  handlers?: string[]
  problems?: number
  cleanupDays?: number
}

interface DiscoverRow {
  key: string
  title: string
  items: RequestableTitle[]
}

type Tab = 'discover' | 'mine' | 'all' | 'problems' | 'people' | 'cleanup'

const tmdbLink = (r: { kind: string; tmdbId: number }) => `https://www.themoviedb.org/${r.kind === 'show' ? 'tv' : 'movie'}/${r.tmdbId}`

const day = (unix: number) => new Date(unix * 1000).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

/**
 * Ask for movies and shows the server doesn't have: search, or pick from
 * rows of what's like what you watch, trending and coming soon. Your
 * requests show how far along they are. Parents answer their kids'
 * requests here; admins approve, set each person's rules, handle problem
 * reports and see requested titles nobody watched.
 */
export default function RequestsPage() {
  useDocumentTitle('Requests')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const admin = !!user?.isAdmin
  const tab = (params.get('tab') as Tab) || (admin ? 'all' : 'discover')
  const { data, reload } = useApi<RequestList>(tab === 'all' ? '/api/requests?all=1' : '/api/requests', 15000)
  const [q, setQ] = useState(params.get('q') ?? '')
  const [results, setResults] = useState<RequestableTitle[] | null>(null)
  const [searchError, setSearchError] = useState('')
  const [asking, setAsking] = useState<{ title: RequestableTitle; season?: number } | null>(null)
  const [sort, setSort] = useState<'new' | 'wanted'>('new')

  const go = (t: Tab) => setParams(t === (admin ? 'all' : 'discover') ? {} : { tab: t }, { replace: true })

  // A link from a "season 3 is out" notification opens the request straight away.
  useEffect(() => {
    const m = /^(movie|show):(\d+)$/.exec(params.get('title') ?? '')
    if (!m) return
    const season = Number(params.get('season')) || undefined
    setAsking({ title: { kind: m[1] as 'movie' | 'show', tmdbId: Number(m[2]), title: '' }, season })
    setParams((p) => {
      const next = new URLSearchParams(p)
      next.delete('title')
      next.delete('season')
      if (!admin) next.delete('tab')
      else next.set('tab', 'discover')
      return next
    }, { replace: true })
  }, [params, setParams, admin])

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
  const pending = data.requests.filter((r) => r.status === 'pending' || r.status === 'parent')
  const restAll = data.requests.filter((r) => r.status !== 'pending' && r.status !== 'parent')
  const rest = sort === 'wanted' ? [...restAll].sort((a, b) => b.people - a.people) : restAll
  const pendingSorted = sort === 'wanted' ? [...pending].sort((a, b) => b.people - a.people) : pending

  const act = async (fn: () => Promise<unknown>) => {
    await fn().catch((e) => alert(e.message))
    reload()
  }
  const decline = (r: TitleRequest, path = `/api/admin/requests/${r.id}/decline`) => {
    const reason = prompt(`Decline "${r.title}"? You can say why (optional).`, '')
    if (reason === null) return
    act(() => api.post(path, { reason }))
  }

  const row = (r: TitleRequest, opts: { parent?: boolean } = {}) => {
    const people = r.people > 1 ? `${r.requestedBy} and ${r.people - 1} more` : r.requestedBy
    const allTab = admin && tab === 'all'
    return (
      <div key={r.id} className="list-row request-row">
        <div className="request-poster">{r.poster ? <img src={img(r.poster, 'w92')} alt="" loading="lazy" /> : <Icon name={r.kind === 'show' ? 'tv' : 'film'} />}</div>
        <div className="grow">
          <div>
            {r.title}
            {r.year ? <span className="muted"> ({r.year})</span> : null}
            {r.fourK && <span className="pill small-pill">4K</span>}
          </div>
          <div className="muted small">
            {[r.kind === 'show' ? seasonText(r) : 'Movie', (admin || r.people > 1 || opts.parent) && people, day(r.createdAt)].filter(Boolean).join(' · ')}
          </div>
          {r.note && <div className="small">“{r.note}”</div>}
          {r.reason && <div className={`small ${r.status === 'failed' ? 'form-error' : 'muted'}`}>{r.reason}</div>}
        </div>
        {r.people > 1 && (
          <span className="request-votes inline" title={`${r.people} people want this`}>
            <Icon name="heart" fill size={12} /> {r.people}
          </span>
        )}
        <RequestStatus r={r} />
        {opts.parent && (
          <>
            <button className="btn btn-primary btn-sm" onClick={() => act(() => api.post(`/api/requests/${r.id}/parent`))}>Yes</button>
            <button className="btn btn-glass btn-sm danger" onClick={() => decline(r, `/api/requests/${r.id}/parent?answer=no`)}>No</button>
          </>
        )}
        {admin && !opts.parent && (
          <a className="btn btn-glass btn-sm" href={r.link || tmdbLink(r)} target="_blank" rel="noreferrer">
            {r.link ? 'Open' : 'TMDB'}
          </a>
        )}
        {allTab && ['pending', 'parent', 'declined', 'failed'].includes(r.status) && (
          <button className="btn btn-primary btn-sm" onClick={() => act(() => api.post(`/api/admin/requests/${r.id}/approve`))}>
            {r.status === 'failed' ? 'Try again' : 'Approve'}
          </button>
        )}
        {allTab && ['pending', 'parent', 'approved', 'failed', 'sent'].includes(r.status) && (
          <button className="btn btn-glass btn-sm danger" onClick={() => decline(r)}>Decline</button>
        )}
        {allTab ? (
          <button
            className="btn btn-glass btn-sm danger"
            aria-label="Delete request"
            onClick={() => confirm(`Delete the request for "${r.title}"?`) && act(() => api.del(`/api/requests/${r.id}?all=1`))}
          >
            <Icon name="trash" size={16} />
          </button>
        ) : (
          !opts.parent &&
          r.mine &&
          ['pending', 'parent', 'declined', 'available'].includes(r.status) && (
            <button className="btn btn-glass btn-sm danger" onClick={() => act(() => api.del(`/api/requests/${r.id}`))}>
              {r.status === 'pending' || r.status === 'parent' ? 'Cancel' : 'Remove'}
            </button>
          )
        )}
      </div>
    )
  }

  const mineCount = admin ? 0 : data.requests.filter((r) => r.status !== 'available' && r.status !== 'declined').length
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Requests</h1>
          <div className="muted">Can’t find a movie or show? Ask for it here. You’ll see it on its way, and get a notification when it’s ready.</div>
        </div>
      </header>

      <div className="tabs">
        {admin && (
          <button className={tab === 'all' ? 'active' : ''} onClick={() => go('all')}>
            All requests {pending.length > 0 && tab === 'all' && <span className="count">{pending.length}</span>}
          </button>
        )}
        <button className={tab === 'discover' ? 'active' : ''} onClick={() => go('discover')}>
          {admin ? 'Ask for something' : 'Discover'}
        </button>
        {!admin && (
          <button className={tab === 'mine' ? 'active' : ''} onClick={() => go('mine')}>
            Your requests {mineCount > 0 && <span className="count">{mineCount}</span>}
          </button>
        )}
        <button className={tab === 'problems' ? 'active' : ''} onClick={() => go('problems')}>
          {admin ? 'Problems' : 'Your reports'} {admin && !!data.problems && <span className="count">{data.problems}</span>}
        </button>
        {admin && (
          <>
            <button className={tab === 'people' ? 'active' : ''} onClick={() => go('people')}>People & limits</button>
            <button className={tab === 'cleanup' ? 'active' : ''} onClick={() => go('cleanup')}>Never watched</button>
          </>
        )}
      </div>

      {data.household.length > 0 && tab !== 'problems' && (
        <>
          <h2 className="section-title">Your kids would like <span className="count">{data.household.length}</span></h2>
          <div className="panel list">{data.household.map((r) => row(r, { parent: true }))}</div>
        </>
      )}

      {tab === 'all' && data.handlers && data.handlers.length === 0 && (
        <div className="notice warn">
          Nothing fetches approved requests yet. Turn on the <Link to="/admin/plugins">Sonarr & Radarr or Cinemask Upgrades plugin</Link> and fill in its
          settings; approved requests wait until then.
        </div>
      )}
      {tab === 'all' && !data.enabled && (
        <div className="notice">Requests are turned off, so nobody can ask for new titles. Turn them on in <Link to="/admin/settings">Settings</Link>.</div>
      )}

      {tab === 'discover' &&
        (!data.canRequest ? (
          <div className="notice">You can’t request titles: {data.reason}.</div>
        ) : (
          <>
            <div className="search-box">
              <Icon name="search" size={22} />
              <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search for a movie or show" autoFocus />
            </div>
            <LimitsLine limits={data.limits} kidsWait={data.kidsWait} />
            {searchError && <div className="notice warn">{searchError}</div>}
            {results && results.length === 0 && <div className="empty">No matches for “{q}”.</div>}
            {results && results.length > 0 && (
              <div className="poster-grid request-results">
                {results.map((r) => (
                  <RequestCard key={`${r.kind}:${r.tmdbId}`} title={r} onAsk={() => setAsking({ title: r })} />
                ))}
              </div>
            )}
            {!results && <DiscoverRows onAsk={(t) => setAsking({ title: t })} />}
            {!results && data.trakt && <WatchlistSwitch on={data.watchlist} onChange={reload} />}
          </>
        ))}

      {tab === 'all' && (
        <>
          <div className="request-sort">
            <span className="muted small">Sort</span>
            <button className={`chip ${sort === 'new' ? 'active' : ''}`} onClick={() => setSort('new')}>Newest</button>
            <button className={`chip ${sort === 'wanted' ? 'active' : ''}`} onClick={() => setSort('wanted')}>Most wanted</button>
          </div>
          <h2 className="section-title">Waiting for approval <span className="count">{pending.length}</span></h2>
          <div className="panel list">
            {pendingSorted.map((r) => row(r))}
            {pending.length === 0 && <div className="list-row muted">Nothing to approve.</div>}
          </div>
          <h2 className="section-title">Everything else <span className="count">{rest.length}</span></h2>
          <div className="panel list">
            {rest.map((r) => row(r))}
            {rest.length === 0 && <div className="list-row muted">No requests yet.</div>}
          </div>
        </>
      )}

      {(tab === 'mine' || (admin && tab === 'discover')) && (
        <>
          <h2 className="section-title">Your requests <span className="count">{data.requests.length}</span></h2>
          <div className="panel list">
            {data.requests.map((r) => row(r))}
            {data.requests.length === 0 && <div className="list-row muted">No requests yet. Find something under Discover.</div>}
          </div>
        </>
      )}

      {tab === 'problems' && <Problems admin={admin} onChange={reload} />}
      {admin && tab === 'people' && <PeopleRules />}
      {admin && tab === 'cleanup' && <NeverWatched days={data.cleanupDays} />}

      {asking && (
        <RequestModal
          title={asking.title}
          season={asking.season}
          onClose={() => setAsking(null)}
          onDone={(req) => {
            setResults((list) => list?.map((r) => (r.kind === req.kind && r.tmdbId === req.tmdbId ? { ...r, request: req } : r)) ?? null)
            setAsking(null)
            reload()
          }}
        />
      )}
    </div>
  )
}

function LimitsLine({ limits, kidsWait }: { limits: RequestList['limits']; kidsWait: boolean }) {
  const parts = (['movie', 'show'] as const)
    .filter((k) => limits[k].limit > 0)
    .map((k) => `${Math.max(limits[k].limit - limits[k].used, 0)} of ${limits[k].limit} ${k} requests left this week`)
  if (!parts.length && !kidsWait) return null
  return <p className="muted small request-limits">{[...parts, kidsWait && 'a grown-up says yes to each one first'].filter(Boolean).join(' · ')}</p>
}

/** The Discover rows: like what you watched, trending, coming soon. */
function DiscoverRows({ onAsk }: { onAsk: (t: RequestableTitle) => void }) {
  const { data, error } = useApi<DiscoverRow[]>('/api/requests/discover')
  if (error) return <div className="notice warn">{error}</div>
  if (!data) return <div className="muted">Finding things you might like…</div>
  return (
    <>
      {data.map((row) => (
        <Row key={row.key} title={row.title}>
          {row.items.map((t) => (
            <RequestCard key={`${t.kind}:${t.tmdbId}`} title={t} onAsk={() => onAsk(t)} />
          ))}
        </Row>
      ))}
    </>
  )
}

function WatchlistSwitch({ on, onChange }: { on: boolean; onChange: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const flip = async (v: boolean) => {
    setBusy(true)
    setError('')
    try {
      await api.put('/api/requests/settings', { watchlist: v })
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <div className="panel request-watchlist">
      <label className="check">
        <input type="checkbox" checked={on} disabled={busy} onChange={(e) => flip(e.target.checked)} />
        Request what I add to my Trakt watchlist
      </label>
      <div className="muted small">
        Anything on your <Link to="/account">Trakt</Link> watchlist that isn’t here gets requested for you, checked every 6 hours. It doesn’t count toward
        your weekly limit.
      </div>
      {error && <div className="form-error small">{error}</div>}
    </div>
  )
}

const problemNames: Record<ProblemReport['kind'], string> = {
  video: 'Picture',
  audio: 'Sound',
  subtitles: 'Subtitles',
  wrong: 'Wrong file',
  other: 'Something else',
}

const problemPill: Record<ProblemReport['status'], [string, string]> = {
  open: ['warn', 'Open'],
  fixing: ['', 'Getting a new copy'],
  resolved: ['ok', 'Fixed'],
}

export const problemTitle = (p: ProblemReport) =>
  p.itemKind === 'episode' && p.seasonNumber != null && p.episodeNumber != null
    ? `${p.showTitle} S${String(p.seasonNumber).padStart(2, '0')}E${String(p.episodeNumber).padStart(2, '0')}`
    : p.title

const clock = (s: number) => `${Math.floor(s / 3600) ? `${Math.floor(s / 3600)}:` : ''}${String(Math.floor((s % 3600) / 60)).padStart(Math.floor(s / 3600) ? 2 : 1, '0')}:${String(Math.floor(s % 60)).padStart(2, '0')}`

/** Problem reports: the viewer's own, or for admins everyone's open ones. */
function Problems({ admin, onChange }: { admin: boolean; onChange: () => void }) {
  const [showResolved, setShowResolved] = useState(false)
  const { data, reload } = useApi<ProblemReport[]>(admin ? `/api/problems?all=1${showResolved ? '&resolved=1' : ''}` : '/api/problems', 30000)
  const act = async (p: ProblemReport, action: 'resolve' | 'reopen' | 'refetch') => {
    const reply = action === 'resolve' ? prompt(`Mark "${problemTitle(p)}" fixed? You can say what you did (optional).`, '') : ''
    if (reply === null) return
    await api.post(`/api/admin/problems/${p.id}`, { action, reply }).catch((e) => alert(e.message))
    reload()
    onChange()
  }
  if (!data) return <div className="page-loading" />
  return (
    <>
      {admin && (
        <label className="check request-sort">
          <input type="checkbox" checked={showResolved} onChange={(e) => setShowResolved(e.target.checked)} /> Show fixed ones too
        </label>
      )}
      {!admin && <p className="muted">Something wrong with a movie or episode? Use <strong>Report a problem</strong> in the player or on its page.</p>}
      <div className="panel list">
        {data.map((p) => {
          const [cls, label] = problemPill[p.status]
          return (
            <div key={p.id} className="list-row request-row">
              <div className="request-poster">{p.poster ? <img src={img(p.poster, 'w92')} alt="" loading="lazy" /> : <Icon name="film" />}</div>
              <div className="grow">
                <div>
                  <Link to={`/item/${p.itemId}`}>{problemTitle(p)}</Link>
                </div>
                <div className="muted small">
                  {[problemNames[p.kind], admin && p.user, p.position ? `at ${clock(p.position)}` : '', day(p.createdAt)].filter(Boolean).join(' · ')}
                </div>
                {p.note && <div className="small">“{p.note}”</div>}
                {p.reply && <div className="small muted">↳ {p.reply}</div>}
              </div>
              <span className={`pill ${cls}`}>{label}</span>
              {admin && p.status !== 'resolved' && (
                <>
                  <button className="btn btn-glass btn-sm" onClick={() => act(p, 'refetch')} title="Blocklist this copy and get another">
                    <Icon name="refresh" size={14} /> New copy
                  </button>
                  <button className="btn btn-primary btn-sm" onClick={() => act(p, 'resolve')}>Fixed</button>
                </>
              )}
              {admin && p.status === 'resolved' && (
                <button className="btn btn-glass btn-sm" onClick={() => act(p, 'reopen')}>Reopen</button>
              )}
              <button
                className="btn btn-glass btn-sm danger"
                aria-label="Delete report"
                onClick={() => confirm('Delete this report?') && api.del(`/api/problems/${p.id}`).then(reload, (e) => alert(e.message))}
              >
                <Icon name="trash" size={16} />
              </button>
            </div>
          )
        })}
        {data.length === 0 && <div className="list-row muted">{admin ? 'No problems reported. Nice.' : 'You haven’t reported anything.'}</div>}
      </div>
    </>
  )
}

interface PersonRules {
  id: number
  name: string
  isAdmin: boolean
  kid: boolean
  profile: boolean
  rules: { autoApprove: boolean; movieLimit: number; showLimit: number }
  movies: RequestLimit
  shows: RequestLimit
}

/** Admins: each person's auto-approve and weekly limits. */
function PeopleRules() {
  const { data, reload } = useApi<PersonRules[]>('/api/admin/requests/rules')
  const settings = useApi<Record<string, string>>('/api/admin/settings')
  const s = settings.data
  const people = useMemo(() => (data ?? []).filter((p) => !p.isAdmin), [data])
  if (!data || !s) return <div className="page-loading" />
  const save = async (p: PersonRules, change: Partial<PersonRules['rules']>) => {
    await api.put(`/api/admin/requests/rules/${p.id}`, { ...p.rules, ...change }).catch((e) => alert(e.message))
    reload()
  }
  const limitInput = (p: PersonRules, key: 'movieLimit' | 'showLimit', used: RequestLimit) => (
    <select value={String(p.rules[key])} onChange={(e) => save(p, { [key]: Number(e.target.value) })} aria-label={key}>
      <option value="-1">Server default ({Number(s[key === 'movieLimit' ? 'request_movie_limit' : 'request_show_limit'] ?? 0) || 'no limit'})</option>
      <option value="0">No limit</option>
      {[1, 2, 3, 5, 7, 10, 15, 20].map((n) => (
        <option key={n} value={n}>{n} a week{used.limit === n ? ` (${used.used} used)` : ''}</option>
      ))}
    </select>
  )
  return (
    <>
      <p className="muted">
        Admins’ requests are always approved at once. Server-wide limits, kids’ requests and 4K are in{' '}
        <Link to="/admin/settings">Settings → Library</Link>.
      </p>
      <div className="panel list">
        {people.map((p) => (
          <div key={p.id} className="list-row request-row">
            <div className="grow">
              <div>{p.name}</div>
              <div className="muted small">{[p.kid && 'Kid (a parent approves first)', p.profile && 'Profile'].filter(Boolean).join(' · ') || 'Account'}</div>
            </div>
            <label className="check small">
              <input type="checkbox" checked={p.rules.autoApprove} onChange={(e) => save(p, { autoApprove: e.target.checked })} /> Approve at once
            </label>
            <label className="small inline-field">Movies {limitInput(p, 'movieLimit', p.movies)}</label>
            <label className="small inline-field">Shows {limitInput(p, 'showLimit', p.shows)}</label>
          </div>
        ))}
        {people.length === 0 && <div className="list-row muted">No one but admins yet.</div>}
      </div>
    </>
  )
}

/** Admins: titles that came in through requests and nobody who asked has played. */
function NeverWatched({ days: setDays }: { days?: number }) {
  const [days, setDays_] = useState(setDays || 60)
  const { data } = useApi<{ days: number; requests: (TitleRequest & { items: number[] })[] }>(`/api/admin/requests/unwatched?days=${days}`)
  return (
    <>
      <p className="muted">
        Requested titles that have been here for at least{' '}
        <select value={days} onChange={(e) => setDays_(Number(e.target.value))} aria-label="Days">
          {[14, 30, 60, 90, 180, 365].map((d) => (
            <option key={d} value={d}>{d} days</option>
          ))}
        </select>{' '}
        that none of the people who asked has played. Open one to delete it from the server.
      </p>
      <div className="panel list">
        {data?.requests.map((r) => (
          <div key={r.id} className="list-row request-row">
            <div className="request-poster">{r.poster ? <img src={img(r.poster, 'w92')} alt="" loading="lazy" /> : <Icon name={r.kind === 'show' ? 'tv' : 'film'} />}</div>
            <div className="grow">
              <div>
                {r.title}
                {r.year ? <span className="muted"> ({r.year})</span> : null}
              </div>
              <div className="muted small">
                {[r.people > 1 ? `${r.requestedBy} and ${r.people - 1} more` : r.requestedBy, r.availableAt && `here since ${day(r.availableAt)}`].filter(Boolean).join(' · ')}
              </div>
            </div>
            {r.items.map((id) => (
              <Link key={id} className="btn btn-glass btn-sm" to={`/item/${id}`}>Open</Link>
            ))}
          </div>
        ))}
        {data && data.requests.length === 0 && <div className="list-row muted">Everything requested that long ago got watched.</div>}
      </div>
    </>
  )
}
