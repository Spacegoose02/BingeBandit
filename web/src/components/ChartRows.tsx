import { useState } from 'react'
import { ApiError, api } from '../api'
import { chartsFor, type Chart } from '../charts'
import type { Collection } from '../types'
import { Modal } from './Modal'

const chartName = (c: Chart) => (c.name.startsWith('IMDb') ? c.name : `${c.source} ${c.name}`)

/** Adds collections that follow TMDB, Trakt or IMDb charts, shown as rows on Recommended (and Home). */
export function ChartRows({ libraryId, libraryKind, onClose, onSaved }: { libraryId: number; libraryKind: string; onClose: () => void; onSaved: () => void }) {
  const options = chartsFor(libraryKind)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [limit, setLimit] = useState('40')
  const [home, setHome] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const toggle = (url: string) =>
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(url)) n.delete(url)
      else n.add(url)
      return n
    })

  const add = async () => {
    setBusy(true)
    setError('')
    const failed: string[] = []
    for (const c of options.filter((c) => picked.has(c.url))) {
      const n = Number(limit)
      const listUrl = n > 0 ? `${c.url}?limit=${n}` : c.url
      try {
        const col = await api.post<Collection>(`/api/admin/libraries/${libraryId}/collections`, { name: chartName(c), listUrl, sort: 'list' })
        await api.put(`/api/admin/collections/${col.id}/visibility`, { library: true, home, friends: home })
        if (col.listError) failed.push(`${chartName(c)}: ${col.listError}`)
      } catch (err) {
        failed.push(err instanceof ApiError && err.status === 409 ? `${chartName(c)} is already a collection` : `${chartName(c)}: ${(err as Error).message}`)
      }
    }
    setBusy(false)
    if (failed.length) {
      setError(failed.join('\n'))
      onSaved()
      return
    }
    onSaved()
    onClose()
  }

  return (
    <Modal title="Add chart rows" onClose={onClose}>
      <div className="form">
        <p className="muted small">
          Each chart becomes a collection of the titles in this library that are on it, in chart order, kept up to date every 6 hours. TMDB charts
          need the TMDB key in Settings → Metadata, and Trakt charts the Trakt client ID; IMDb charts need nothing.
        </p>
        {(['TMDB', 'Trakt', 'IMDb'] as const).map((source) => (
          <div key={source}>
            <div className="label">{source}</div>
            <div className="check-list">
              {options
                .filter((c) => c.source === source)
                .map((c) => (
                  <label key={c.url} className="check">
                    <input type="checkbox" checked={picked.has(c.url)} onChange={() => toggle(c.url)} />
                    {c.name}
                  </label>
                ))}
            </div>
          </div>
        ))}
        <div className="field-grid">
          <label>
            Titles from each chart
            <input type="number" min={0} max={500} value={limit} onChange={(e) => setLimit(e.target.value)} />
            <span className="muted small">0 keeps the whole chart.</span>
          </label>
          <label className="check">
            <input type="checkbox" checked={home} onChange={(e) => setHome(e.target.checked)} />
            Also show on Home
          </label>
        </div>
        {error && <div className="form-error" style={{ whiteSpace: 'pre-line' }}>{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>
            {error ? 'Close' : 'Cancel'}
          </button>
          <button className="btn btn-primary" disabled={busy || picked.size === 0} onClick={add}>
            {busy ? 'Adding…' : `Add ${picked.size || ''} ${picked.size === 1 ? 'row' : 'rows'}`}
          </button>
        </div>
      </div>
    </Modal>
  )
}
