import { useState, type FormEvent } from 'react'
import { api } from '../api'
import { useApi } from '../hooks'
import type { Collection } from '../types'
import { Modal } from './Modal'

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
  const [error, setError] = useState('')
  const { data: labels } = useApi<string[]>(`/api/admin/libraries/${libraryId}/labels`)

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      if (collection) await api.put(`/api/admin/collections/${collection.id}`, { name, overview, labelRule })
      else await api.post(`/api/admin/libraries/${libraryId}/collections`, { name, labelRule })
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
          <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus placeholder="e.g. Original Disney Films" />
        </label>
        {collection && (
          <label>
            Summary
            <textarea rows={3} value={overview} onChange={(e) => setOverview(e.target.value)} />
          </label>
        )}
        <label>
          Smart rule: include everything labelled
          <input list="bb-labels" value={labelRule} onChange={(e) => setLabelRule(e.target.value)} placeholder="Leave empty for a hand-picked collection" />
          <datalist id="bb-labels">
            {labels?.map((l) => <option key={l} value={l} />)}
          </datalist>
        </label>
        <p className="muted small">
          Add labels to movies or shows from their Edit → Tags tab. Anything with this label joins the collection automatically.
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
