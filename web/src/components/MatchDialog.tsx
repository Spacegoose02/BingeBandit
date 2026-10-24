import { useEffect, useState, type FormEvent } from 'react'
import { api, img } from '../api'
import type { Item } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

interface Result {
  tmdbId: number
  title: string
  year?: number
  overview?: string
  poster?: string
}

/** Search TMDB and re-point a movie or show at the chosen result. */
export function MatchDialog({ item, onClose, onMatched }: { item: Item; onClose: () => void; onMatched: () => void }) {
  const [q, setQ] = useState(item.title)
  const [year, setYear] = useState(item.year ? String(item.year) : '')
  const [results, setResults] = useState<Result[] | null>(null)
  const [busy, setBusy] = useState<number | 'search' | null>(null)
  const [error, setError] = useState('')
  const kind = item.kind === 'show' ? 'show' : 'movie'

  const search = async (e?: FormEvent) => {
    e?.preventDefault()
    setBusy('search')
    setError('')
    try {
      setResults(await api.get<Result[]>(`/api/admin/match?kind=${kind}&q=${encodeURIComponent(q)}&year=${year}`))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(null)
    }
  }

  useEffect(() => {
    search()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const choose = async (r: Result) => {
    setBusy(r.tmdbId)
    setError('')
    try {
      await api.post(`/api/admin/items/${item.id}/match`, { tmdbId: r.tmdbId })
      onMatched()
    } catch (err) {
      setError((err as Error).message)
      setBusy(null)
    }
  }

  return (
    <Modal title={`Fix match: ${item.title}`} onClose={onClose} wide>
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
          <button key={r.tmdbId} className={`match-row ${r.tmdbId === item.tmdbId ? 'current' : ''}`} onClick={() => choose(r)} disabled={busy !== null}>
            <div className="match-poster">{r.poster ? <img src={img(r.poster, 'w92')} alt="" /> : <Icon name="image" />}</div>
            <div className="grow">
              <div className="match-title">
                {r.title} {r.year ? <span className="muted">({r.year})</span> : null}
                {r.tmdbId === item.tmdbId && <span className="pill ok">Current</span>}
              </div>
              {r.overview && <div className="muted small clamp-2">{r.overview}</div>}
            </div>
            {busy === r.tmdbId && <Icon name="refresh" size={18} className="spin" />}
          </button>
        ))}
      </div>
    </Modal>
  )
}
