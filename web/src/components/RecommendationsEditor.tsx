import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Library, LibraryRow, Visibility } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'
import { moveItem } from './Pins'

const columns: [keyof Visibility, string][] = [
  ['library', 'Library Recommended'],
  ['home', 'Home'],
  ['friends', "Friends' Home"],
]

/**
 * Plex's Manage Recommendations: every row a library can show (built-in rows
 * and its collections), dragged into order, each ticked for the library's
 * Recommended tab, Home, and Friends' Home.
 */
export function RecommendationsEditor({ library, onClose, onSaved }: { library: Library; onClose: () => void; onSaved?: () => void }) {
  const [rows, setRows] = useState<LibraryRow[] | null>(null)
  const [dragging, setDragging] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.get<LibraryRow[]>(`/api/admin/libraries/${library.id}/rows`).then(setRows, (e) => setError(e.message))
  }, [library.id])

  const toggle = (key: string, col: keyof Visibility) =>
    setRows((cur) => cur && cur.map((r) => (r.key === key ? { ...r, [col]: !r[col] } : r)))

  const save = async () => {
    if (!rows) return
    setSaving(true)
    try {
      await api.put(`/api/admin/libraries/${library.id}/rows`, rows)
      onSaved?.()
      onClose()
    } catch (e) {
      setError((e as Error).message)
      setSaving(false)
    }
  }

  return (
    <Modal title={`Manage recommendations · ${library.name}`} onClose={onClose} wide>
      <p className="muted small" style={{ marginTop: 0 }}>
        Drag rows into order. Library Recommended is the library's front page. Home rows from every pinned library are merged, so all
        movie libraries share one Recently Added Movies row. Friends' Home is Home for people outside your household.
      </p>
      {!rows && !error && <div className="page-loading" />}
      {rows && (
        <div className="rec-table" role="table">
          <div className="rec-row rec-head" role="row">
            <span />
            <span role="columnheader">{library.name}</span>
            {columns.map(([c, label]) => (
              <span key={c} role="columnheader" className="rec-col">
                {label}
              </span>
            ))}
          </div>
          {rows.map((r, i) => (
            <div
              key={r.key}
              role="row"
              className={`rec-row ${dragging === i ? 'dragging' : ''}`}
              draggable
              onDragStart={() => setDragging(i)}
              onDragEnd={() => setDragging(null)}
              onDragOver={(e) => {
                e.preventDefault()
                if (dragging !== null && dragging !== i) {
                  setRows((cur) => cur && moveItem(cur, dragging, i))
                  setDragging(i)
                }
              }}
            >
              <span className="drag-handle" aria-hidden="true">
                <Icon name="grip" size={16} />
              </span>
              <span className="rec-title">
                {r.collectionId && <Icon name="stack" size={14} />} {r.title}
              </span>
              {columns.map(([c, label]) => (
                <span key={c} className="rec-col">
                  <input
                    type="checkbox"
                    aria-label={`${r.title}: ${label}`}
                    checked={r[c]}
                    disabled={r.libraryOnly && c !== 'library'}
                    title={r.libraryOnly && c !== 'library' ? 'Home has its own Continue Watching' : undefined}
                    onChange={() => toggle(r.key, c)}
                  />
                </span>
              ))}
            </div>
          ))}
        </div>
      )}
      {error && <div className="form-error">{error}</div>}
      <div className="actions" style={{ marginTop: 16, justifyContent: 'flex-end' }}>
        <button className="btn btn-glass" onClick={onClose}>
          Cancel
        </button>
        <button className="btn btn-primary" disabled={!rows || saving} onClick={save}>
          Save
        </button>
      </div>
    </Modal>
  )
}
