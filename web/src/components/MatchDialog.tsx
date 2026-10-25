import { useEffect, useState, type FormEvent } from 'react'
import { api, img } from '../api'
import type { Item } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

interface Result {
  tmdbId?: number
  tvdbId?: number
  title: string
  year?: number
  overview?: string
  poster?: string
}

const orders = [
  { value: '', label: 'Aired order' },
  { value: 'dvd', label: 'DVD order' },
  { value: 'absolute', label: 'Absolute order (anime)' },
]

/**
 * Search TMDB and re-point a movie or show at the chosen result. A show can
 * use TheTVDB instead, with its aired, DVD or absolute episode numbering.
 */
export function MatchDialog({
  item,
  tvdb,
  onClose,
  onMatched,
}: {
  item: Item
  tvdb?: { id: number; order: string }
  onClose: () => void
  onMatched: () => void
}) {
  const [q, setQ] = useState(item.title)
  const [year, setYear] = useState(item.year ? String(item.year) : '')
  const [results, setResults] = useState<Result[] | null>(null)
  const [busy, setBusy] = useState<number | 'search' | null>(null)
  const [error, setError] = useState('')
  const kind = item.kind === 'show' ? 'show' : 'movie'
  const [provider, setProvider] = useState<'tmdb' | 'tvdb'>(tvdb?.id ? 'tvdb' : 'tmdb')
  const [order, setOrder] = useState(tvdb?.order ?? '')
  const idOf = (r: Result) => (provider === 'tvdb' ? r.tvdbId : r.tmdbId) ?? 0
  const currentId = provider === 'tvdb' ? tvdb?.id : tvdb?.id ? undefined : item.tmdbId

  const search = async (e?: FormEvent, p = provider) => {
    e?.preventDefault()
    setBusy('search')
    setError('')
    try {
      setResults(await api.get<Result[]>(`/api/admin/match?kind=${kind}&provider=${p}&q=${encodeURIComponent(q)}&year=${year}`))
    } catch (err) {
      setResults(null)
      setError((err as Error).message)
    } finally {
      setBusy(null)
    }
  }

  useEffect(() => {
    search()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const switchProvider = (p: 'tmdb' | 'tvdb') => {
    setProvider(p)
    setResults(null)
    search(undefined, p)
  }

  const choose = async (r: Result) => {
    setBusy(idOf(r))
    setError('')
    try {
      await api.post(`/api/admin/items/${item.id}/match`, provider === 'tvdb' ? { tvdbId: r.tvdbId, tvdbOrder: order } : { tmdbId: r.tmdbId })
      onMatched()
    } catch (err) {
      setError((err as Error).message)
      setBusy(null)
    }
  }

  return (
    <Modal title={`Fix match: ${item.title}`} onClose={onClose} wide>
      {kind === 'show' && (
        <div className="match-provider">
          <div className="segmented">
            <button type="button" className={provider === 'tmdb' ? 'active' : ''} onClick={() => switchProvider('tmdb')}>TMDB</button>
            <button type="button" className={provider === 'tvdb' ? 'active' : ''} onClick={() => switchProvider('tvdb')}>TheTVDB</button>
          </div>
          {provider === 'tvdb' && (
            <select value={order} onChange={(e) => setOrder(e.target.value)} aria-label="Episode order">
              {orders.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          )}
        </div>
      )}
      {provider === 'tvdb' && (
        <p className="muted small">
          Titles, episode names and season numbering come from TheTVDB, in the order your files are numbered. TMDB still adds the logo,
          trailer and rating when it knows the show.
        </p>
      )}
      <form className="match-search" onSubmit={search}>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Title" autoFocus />
        <input value={year} onChange={(e) => setYear(e.target.value.replace(/\D/g, '').slice(0, 4))} placeholder="Year" className="year" />
        <button className="btn btn-primary" disabled={busy !== null}>
          <Icon name="search" size={16} /> Search
        </button>
      </form>
      {error && <div className="form-error">{error}</div>}
      <div className="match-results">
        {results?.length === 0 && <div className="muted">No results. Try the original title or drop the year.</div>}
        {results?.map((r) => (
          <button key={idOf(r)} className={`match-row ${idOf(r) === currentId ? 'current' : ''}`} onClick={() => choose(r)} disabled={busy !== null}>
            <div className="match-poster">{r.poster ? <img src={img(r.poster, 'w92')} alt="" /> : <Icon name="image" />}</div>
            <div className="grow">
              <div className="match-title">
                {r.title} {r.year ? <span className="muted">({r.year})</span> : null}
                {idOf(r) === currentId && <span className="pill ok">Current</span>}
              </div>
              {r.overview && <div className="muted small clamp-2">{r.overview}</div>}
            </div>
            {busy === idOf(r) && <Icon name="refresh" size={18} className="spin" />}
          </button>
        ))}
      </div>
    </Modal>
  )
}
