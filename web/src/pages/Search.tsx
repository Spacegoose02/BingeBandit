import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useAuth } from '../auth'
import { api } from '../api'
import { PosterCard } from '../components/Cards'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks'
import type { Item } from '../types'

export default function Search() {
  useDocumentTitle('Search')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [results, setResults] = useState<Item[] | null>(null)

  useEffect(() => {
    const term = q.trim()
    const t = window.setTimeout(async () => {
      setParams(term ? { q: term } : {}, { replace: true })
      setResults(term ? await api.get<Item[]>(`/api/search?q=${encodeURIComponent(term)}`) : null)
    }, 250)
    return () => window.clearTimeout(t)
  }, [q, setParams])

  return (
    <div className="page">
      <div className="search-box">
        <Icon name="search" size={22} />
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search movies and shows" autoFocus />
      </div>
      {results && results.length === 0 && <div className="empty">No matches for “{q}”.</div>}
      {results && !user?.kid && (
        <p className="muted search-request">
          Not what you’re after? <Link to={`/requests?tab=mine&q=${encodeURIComponent(q.trim())}`}>Request a movie or show</Link>
        </p>
      )}
      <div className="poster-grid">
        {results?.map((it) => (
          <PosterCard key={it.id} item={it} />
        ))}
      </div>
    </div>
  )
}
