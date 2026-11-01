import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, img } from '../api'
import { useApi } from '../hooks'
import type { RequestableTitle, RequestTitleInfo, TitleRequest } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'
import { Row } from './Row'

/** What each request status is called, and its pill colour. */
export const statusPill: Record<TitleRequest['status'], [string, string]> = {
  parent: ['warn', 'Waiting for a parent'],
  pending: ['warn', 'Waiting for approval'],
  approved: ['', 'Approved'],
  sent: ['ok', 'On its way'],
  failed: ['danger', 'Couldn’t fetch'],
  declined: ['danger', 'Declined'],
  available: ['ok', 'Ready to watch'],
}

const stageNames: Record<NonNullable<TitleRequest['stage']>, string> = {
  searching: 'Searching',
  downloading: 'Downloading',
  importing: 'Almost ready',
  waiting: 'Not out yet',
}

/** "in 12 min", "in 3 h", "Tue". */
export function etaText(unix: number) {
  const s = unix - Date.now() / 1000
  if (s <= 60) return 'any minute'
  if (s < 3600) return `in ${Math.round(s / 60)} min`
  if (s < 86400) return `in ${Math.round(s / 3600)} h`
  return new Date(unix * 1000).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
}

export const seasonText = (r: { kind: string; seasons?: number[] }) =>
  r.kind !== 'show' ? '' : !r.seasons?.length ? 'All seasons' : r.seasons.length === 1 ? `Season ${r.seasons[0]}` : `Seasons ${r.seasons.join(', ')}`

/** The status pill, with a progress bar and ETA while it downloads. */
export function RequestStatus({ r, compact }: { r: TitleRequest; compact?: boolean }) {
  const [cls, label] = statusPill[r.status]
  if (r.status === 'sent' && r.stage) {
    const pct = Math.round((r.progress ?? 0) * 100)
    const text =
      r.stage === 'downloading'
        ? `${pct}%${r.eta ? ` · ${etaText(r.eta)}` : ''}`
        : r.stage === 'waiting' && r.releaseDate
          ? `Out ${new Date(r.releaseDate + 'T12:00').toLocaleDateString([], { month: 'short', day: 'numeric' })}`
          : ''
    return (
      <div className={`request-progress ${compact ? 'compact' : ''}`} title={`${stageNames[r.stage]} ${text}`}>
        <div className="small">
          <strong>{stageNames[r.stage]}</strong> {text && <span className="muted">{text}</span>}
        </div>
        {(r.stage === 'downloading' || r.stage === 'importing') && (
          <div className="request-bar">
            <span style={{ width: `${r.stage === 'importing' ? 100 : pct}%` }} />
          </div>
        )}
      </div>
    )
  }
  return <span className={`pill ${cls}`}>{label}</span>
}

/** A TMDB title with what can be done with it: watch, see its request, or ask. */
export function RequestCard({ title: r, onAsk }: { title: RequestableTitle; onAsk: () => void }) {
  const req = r.request
  const soon = r.releaseDate && r.releaseDate > new Date().toISOString().slice(0, 10)
  return (
    <div className="poster-card request-card">
      <button className="poster-link plain" onClick={r.itemId ? undefined : onAsk} aria-label={`Request ${r.title}`} disabled={!!r.itemId}>
        <div className="poster-frame">
          {r.poster ? <img src={img(r.poster, 'w342')} alt="" loading="lazy" /> : <div className="poster-fallback">{r.title}</div>}
          {req && req.people > 1 && (
            <span className="request-votes" title={`${req.people} people want this`}>
              <Icon name="heart" fill size={12} /> {req.people}
            </span>
          )}
          {soon && <span className="request-soon">Coming {new Date(r.releaseDate + 'T12:00').toLocaleDateString([], { month: 'short', day: 'numeric' })}</span>}
        </div>
      </button>
      <div className="card-title" title={r.title}>{r.title}</div>
      <div className="card-sub">{r.role ? r.role : [r.year, r.kind === 'show' ? 'Show' : 'Movie'].filter(Boolean).join(' · ')}</div>
      <div className="request-action">
        {r.itemId ? (
          <Link className="btn btn-glass btn-sm" to={`/item/${r.itemId}`}><Icon name="play" fill size={14} /> Watch</Link>
        ) : r.onServer && r.kind === 'movie' ? (
          <span className="pill">On the server</span>
        ) : req && req.status !== 'declined' && (req.mine || req.status === 'available') ? (
          <RequestStatus r={req} compact />
        ) : (
          <button className="btn btn-primary btn-sm" onClick={onAsk}>
            {req ? <><Icon name="heart" size={14} /> Me too</> : <><Icon name="plus" size={14} /> Request</>}
          </button>
        )}
      </div>
    </div>
  )
}

/**
 * Asking for a title: the seasons of a show (what's missing ticked), the
 * 4K copy when allowed, and a note. Shows the weekly limit and who else
 * asked.
 */
export function RequestModal({
  title,
  season,
  onClose,
  onDone,
}: {
  title: Pick<RequestableTitle, 'kind' | 'tmdbId' | 'title'> & Partial<RequestableTitle>
  /** A season to start with ticked (from a "season 3 is out" notification). */
  season?: number
  onClose: () => void
  onDone: (r: TitleRequest) => void
}) {
  const { data: info, error: infoError } = useApi<RequestTitleInfo>(`/api/requests/title/${title.kind}/${title.tmdbId}`)
  const [note, setNote] = useState('')
  const [picked, setPicked] = useState<Set<number> | null>(null)
  const [fourK, setFourK] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const today = new Date().toISOString().slice(0, 10)

  // Start with the seasons the server is missing (or the one asked about) ticked.
  useEffect(() => {
    if (!info?.seasons || picked) return
    const missing = info.seasons.filter((s) => s.onServer < s.episodes && !s.requested).map((s) => s.number)
    setPicked(new Set(season ? [season] : missing))
  }, [info, picked, season])

  const t = info?.title ?? (title as RequestableTitle)
  const req = t.request
  const seasons = info?.seasons ?? []
  const offerable = seasons.filter((s) => !(s.onServer >= s.episodes && s.episodes > 0) && !s.requested)
  const allPicked = picked && offerable.length > 0 && offerable.every((s) => picked.has(s.number))
  const hasAny = seasons.some((s) => s.onServer > 0)
  const limit = info?.limit
  const overLimit = !!limit && limit.limit > 0 && limit.used >= limit.limit && !req?.mine

  const send = async () => {
    setBusy(true)
    setError('')
    try {
      const body: Record<string, unknown> = {
        kind: t.kind, tmdbId: t.tmdbId, title: t.title, year: t.year, poster: t.poster, overview: t.overview, releaseDate: t.releaseDate, note, fourK,
      }
      // Every season and nothing on the server yet: ask for the whole show, so later seasons come too.
      if (t.kind === 'show' && picked && !(allPicked && !hasAny)) body.seasons = [...picked].sort((a, b) => a - b)
      onDone(await api.post<TitleRequest>('/api/requests', body))
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }
  const toggle = (n: number) =>
    setPicked((p) => {
      const next = new Set(p)
      if (next.has(n)) next.delete(n)
      else next.add(n)
      return next
    })
  const nothingPicked = t.kind === 'show' && !!info?.seasons && (!picked || picked.size === 0) && !fourK

  return (
    <Modal title={`Request ${t.title}`} onClose={onClose}>
      <div className="form request-modal">
        {t.poster && <img className="request-modal-poster" src={img(t.poster, 'w185')} alt="" />}
        {req && req.status !== 'declined' && (
          <p className="muted">
            {req.mine ? 'You asked for this already.' : `${req.requestedBy}${req.people > 1 ? ` and ${req.people - 1} more` : ''} already asked for this.`}{' '}
            {!req.mine && 'Asking too means it moves up the list, and you’ll hear when it’s here.'}
          </p>
        )}
        {t.overview && <p className="muted small request-overview">{t.overview}</p>}
        {infoError && <div className="notice warn">{infoError}</div>}
        {t.kind === 'show' && !info && !infoError && <div className="muted small">Loading seasons…</div>}
        {seasons.length > 0 && (
          <div>
            <div className="label">
              Seasons
              {offerable.length > 1 && (
                <button className="link-btn small" onClick={() => setPicked(new Set(allPicked ? [] : offerable.map((s) => s.number)))}>
                  {allPicked ? 'None' : 'All missing'}
                </button>
              )}
            </div>
            <div className="check-list season-list">
              {seasons.map((s) => {
                const full = s.onServer >= s.episodes && s.episodes > 0
                const upcoming = !s.airDate || s.airDate > today
                return (
                  <label key={s.number} className={`check ${full || s.requested ? 'muted' : ''}`}>
                    <input type="checkbox" disabled={full || s.requested} checked={full || s.requested || !!picked?.has(s.number)} onChange={() => toggle(s.number)} />
                    {s.name || `Season ${s.number}`}
                    <span className="muted small">
                      {full
                        ? ' · on the server'
                        : s.requested
                          ? ' · requested'
                          : s.onServer > 0
                            ? ` · ${s.onServer} of ${s.episodes} here`
                            : upcoming
                              ? ` · ${s.airDate ? `starts ${new Date(s.airDate + 'T12:00').toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })}` : 'date not set'}`
                              : ` · ${s.episodes} episodes`}
                    </span>
                  </label>
                )
              })}
            </div>
          </div>
        )}
        {info?.fourK && !info.has4K && (
          <label className="check">
            <input type="checkbox" checked={fourK} onChange={(e) => setFourK(e.target.checked)} />
            Also get the 4K copy
          </label>
        )}
        <label>
          Note (optional)
          <input value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} placeholder="Anything the admin should know" />
        </label>
        {limit && limit.limit > 0 && (
          <div className={`small ${overLimit ? 'form-error' : 'muted'}`}>
            {overLimit
              ? `You’ve used your ${limit.limit} ${t.kind === 'show' ? 'show' : 'movie'} requests this week. The next one frees up ${limit.resetsAt ? new Date(limit.resetsAt * 1000).toLocaleDateString([], { weekday: 'long' }) : 'soon'}.`
              : `${limit.limit - limit.used} of ${limit.limit} ${t.kind === 'show' ? 'show' : 'movie'} requests left this week.`}
          </div>
        )}
        {info?.kidsWait && <div className="muted small">A grown-up says yes first.</div>}
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy || overLimit || nothingPicked} onClick={send}>
            {req && !req.mine && req.status !== 'declined' ? 'Me too' : 'Request'}
          </button>
          <button className="btn btn-glass" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </Modal>
  )
}

/**
 * A row of titles the server doesn't have, from one of the request lists
 * (a person's other work, titles like this one, a collection's missing
 * ones). Nothing shows when requests are off for the viewer or the list is
 * empty.
 */
export function RequestRow({ title, path }: { title: string; path: string }) {
  const { data, reload } = useApi<RequestableTitle[]>(path)
  const [asking, setAsking] = useState<RequestableTitle | null>(null)
  const [list, setList] = useState<RequestableTitle[] | null>(null)
  useEffect(() => setList(data), [data])
  if (!list || list.length === 0) return null
  return (
    <>
      <Row title={title}>
        {list.map((t) => (
          <RequestCard key={`${t.kind}:${t.tmdbId}`} title={t} onAsk={() => setAsking(t)} />
        ))}
      </Row>
      {asking && (
        <RequestModal
          title={asking}
          onClose={() => setAsking(null)}
          onDone={(req) => {
            setList((l) => l?.map((t) => (t.kind === asking.kind && t.tmdbId === asking.tmdbId ? { ...t, request: req } : t)) ?? null)
            setAsking(null)
            reload()
          }}
        />
      )}
    </>
  )
}

/**
 * On a show's page: the seasons TMDB has that the server doesn't (out
 * already, or coming), with a button to ask for them.
 */
export function MissingSeasons({ tmdbId }: { tmdbId: number }) {
  const { data, reload } = useApi<RequestTitleInfo>(`/api/requests/title/show/${tmdbId}`)
  const [open, setOpen] = useState(false)
  if (!data?.seasons) return null
  const today = new Date().toISOString().slice(0, 10)
  const missing = data.seasons.filter((s) => s.onServer < s.episodes && s.episodes > 0)
  if (missing.length === 0) return null
  const asked = missing.filter((s) => s.requested)
  const open_ = missing.filter((s) => !s.requested)
  const names = (list: typeof missing) =>
    list.length === 1 ? `Season ${list[0].number}` : `Seasons ${list.map((s) => s.number).join(', ')}`
  const upcoming = open_.find((s) => !s.airDate || s.airDate > today)
  const req = data.title.request
  return (
    <div className="panel missing-seasons">
      <Icon name="tv" size={20} />
      <div className="grow">
        {open_.length > 0 ? (
          <>
            <strong>{names(open_)}</strong> {open_.length === 1 ? 'isn’t' : 'aren’t'} here
            {open_.some((s) => s.onServer > 0) ? ' in full' : ''} yet.
            {upcoming?.airDate && (
              <span className="muted"> Season {upcoming.number} starts {new Date(upcoming.airDate + 'T12:00').toLocaleDateString([], { month: 'long', day: 'numeric' })}.</span>
            )}
          </>
        ) : (
          <>
            <strong>{names(asked)}</strong> requested.
          </>
        )}
        {req && req.status === 'sent' && <RequestStatus r={req} />}
      </div>
      {open_.length > 0 && (
        <button className="btn btn-primary btn-sm" onClick={() => setOpen(true)}>
          <Icon name="plus" size={14} /> Request
        </button>
      )}
      {open && (
        <RequestModal
          title={data.title}
          onClose={() => setOpen(false)}
          onDone={() => {
            setOpen(false)
            reload()
          }}
        />
      )}
    </div>
  )
}
