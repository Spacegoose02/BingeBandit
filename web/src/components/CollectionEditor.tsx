import { useState, type FormEvent } from 'react'
import { api } from '../api'
import { useApi } from '../hooks'
import type { Collection } from '../types'
import { Modal } from './Modal'

interface Facets {
  labels: string[]
  genres: string[]
  studios: string[]
}

const yearValue = (s: string) => (s ? Number(s) : 0)

/** Create (libraryId) or edit (collection) a collection. */
export function CollectionEditor({
  libraryId,
  collection,
  onClose,
  onSaved,
}: {
  libraryId: number
  collection?: Collection
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(collection?.name ?? '')
  const [overview, setOverview] = useState(collection?.overview ?? '')
  const [labelRule, setLabelRule] = useState(collection?.labelRule ?? '')
  const [genreRule, setGenreRule] = useState(collection?.genreRule ?? '')
  const [studioRule, setStudioRule] = useState(collection?.studioRule ?? '')
  const [yearFrom, setYearFrom] = useState(collection?.yearFrom ? String(collection.yearFrom) : '')
  const [yearTo, setYearTo] = useState(collection?.yearTo ? String(collection.yearTo) : '')
  const [error, setError] = useState('')
  const { data: facets } = useApi<Facets>(`/api/admin/libraries/${libraryId}/facets`)

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    const rules = { labelRule, genreRule, studioRule, yearFrom: yearValue(yearFrom), yearTo: yearValue(yearTo) }
    try {
      if (collection) await api.put(`/api/admin/collections/${collection.id}`, { name, overview, ...rules })
      else await api.post(`/api/admin/libraries/${libraryId}/collections`, { name, ...rules })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <Modal title={collection ? `Edit ${collection.name}` : 'New collection'} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus placeholder="e.g. 80s Horror" />
        </label>
        {collection && (
          <label>
            Summary
            <textarea rows={3} value={overview} onChange={(e) => setOverview(e.target.value)} />
          </label>
        )}
        <div className="label">Smart rules: include everything that matches all of these</div>
        <div className="field-grid">
          <label>
            Genre
            <input list="bb-genres" value={genreRule} onChange={(e) => setGenreRule(e.target.value)} placeholder="Any" />
          </label>
          <label>
            Studio or network
            <input list="bb-studios" value={studioRule} onChange={(e) => setStudioRule(e.target.value)} placeholder="Any" />
          </label>
          <label>
            From year
            <input type="number" min={1870} max={2200} value={yearFrom} onChange={(e) => setYearFrom(e.target.value)} placeholder="Any" />
          </label>
          <label>
            To year
            <input type="number" min={1870} max={2200} value={yearTo} onChange={(e) => setYearTo(e.target.value)} placeholder="Any" />
          </label>
          <label className="span-2">
            Label
            <input list="bb-labels" value={labelRule} onChange={(e) => setLabelRule(e.target.value)} placeholder="Any" />
          </label>
        </div>
        <datalist id="bb-genres">{facets?.genres.map((g) => <option key={g} value={g} />)}</datalist>
        <datalist id="bb-studios">{facets?.studios.map((g) => <option key={g} value={g} />)}</datalist>
        <datalist id="bb-labels">{facets?.labels.map((l) => <option key={l} value={l} />)}</datalist>
        <p className="muted small">
          Leave every rule empty for a hand-picked collection. Labels come from a movie's or show's Edit → Tags tab. Anything that
          matches joins the collection automatically, alongside anything added by hand.
        </p>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">{collection ? 'Save' : 'Create'}</button>
        </div>
      </form>
    </Modal>
  )
}
