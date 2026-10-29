import { useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'

interface TPDbTitle {
  id: number
  title: string
  url: string
  posters: { id: number; title?: string; uploader?: string; download: string }[]
}

/**
 * Posters from ThePosterDB, searched on demand (it has no API, so the server reads its pages).
 * endpoint is the item's or collection's tpdb search; title is what an empty search looks for.
 * Not a <form>, so it can sit inside another form (the collection editor).
 */
export function TPDbSearch({ endpoint, title, busy, onPick }: { endpoint: string; title: string; busy: boolean; onPick: (url: string) => void }) {
  const [q, setQ] = useState('')
  const [res, setRes] = useState<{ query: string; titles: TPDbTitle[] } | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const search = async () => {
    setLoading(true)
    setError('')
    try {
      const r = await api.get<{ query: string; titles: TPDbTitle[] | null }>(`${endpoint}${q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ''}`)
      // Older servers send null for an empty list; keep a bad response from blanking the page.
      setRes({ query: r.query, titles: (r.titles ?? []).map((t) => ({ ...t, posters: t.posters ?? [] })) })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }
  const term = q.trim() || res?.query || title
  const posters = res?.titles.reduce((n, t) => n + t.posters.length, 0) ?? 0
  return (
    <>
      <div className="label">From ThePosterDB</div>
      <div className="url-row">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              search()
            }
          }}
          placeholder={`Search for “${title}”, or paste a ThePosterDB link`}
        />
        <button type="button" className="btn btn-glass btn-sm" disabled={loading} onClick={search}>
          <Icon name="search" size={16} /> {loading ? 'Searching…' : 'Search'}
        </button>
        <a className="btn btn-glass btn-sm" href={`https://theposterdb.com/search?term=${encodeURIComponent(term)}`} target="_blank" rel="noreferrer">
          <Icon name="link" size={16} /> Open site
        </a>
      </div>
      {error && <div className="form-error small">{error}</div>}
      {res && posters === 0 && <div className="muted small">No posters found for “{res.query}”. Try another title, or paste a link from their site.</div>}
      {res?.titles.filter((t) => t.posters.length > 0).map((t) => (
        <div key={`${t.id}:${t.url}`} className="tpdb-title">
          <a className="small" href={t.url} target="_blank" rel="noreferrer">{t.title || t.url}</a>
          <div className="art-grid poster">
            {t.posters.map((p) => (
              <button type="button" key={p.id} className="art-thumb poster" disabled={busy} onClick={() => onPick(p.download)} title={[p.title, p.uploader && `by ${p.uploader}`].filter(Boolean).join(' · ')}>
                <img src={`/api/admin/tpdb/${p.id}/thumb`} alt="" loading="lazy" />
                {p.uploader && <span className="art-lang">{p.uploader}</span>}
              </button>
            ))}
          </div>
        </div>
      ))}
    </>
  )
}
