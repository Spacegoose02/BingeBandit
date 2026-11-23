import { useEffect, useMemo, useState } from 'react'
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

const anywhere = (r: LibraryRow) => r.library || r.home || r.friends

/**
 * Plex's Manage Recommendations: the library's built-in rows and the
 * collections an admin opted in (from a collection's "Visible on" menu, or
 * the Add collection box here), dragged into order, each ticked for the
 * library's Recommended tab, Home, and Friends' Home. Collections that show
 * nowhere stay out of the list, so a big library doesn't list hundreds.
 */
export function RecommendationsEditor({ library, onClose, onSaved }: { library: Library; onClose: () => void; onSaved?: () => void }) {
  // Every row the library has, in order; `shown` are the ones listed. A
  // collection unticked everywhere stays listed until the editor closes, so
  // a slip of the mouse can be undone.
  const [rows, setRows] = useState<LibraryRow[] | null>(null)
  const [shown, setShown] = useState<Set<string>>(new Set())
  const [dragging, setDragging] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')

  const load = (all: LibraryRow[]) => {
    setRows(all)
    setShown(new Set(all.filter((r) => !r.collectionId || anywhere(r)).map((r) => r.key)))
  }

  useEffect(() => {
    api.get<LibraryRow[]>(`/api/admin/libraries/${library.id}/rows`).then(load, (e) => setError(e.message))
  }, [library.id])

  const listed = useMemo(() => (rows ?? []).filter((r) => shown.has(r.key)), [rows, shown])
  const matches = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q || !rows) return []
    return rows.filter((r) => r.collectionId && !shown.has(r.key) && r.title.toLowerCase().includes(q)).slice(0, 8)
  }, [rows, shown, query])

  const toggle = (key: string, col: keyof Visibility) =>
    setRows((cur) => cur && cur.map((r) => (r.key === key ? { ...r, [col]: !r[col] } : r)))

  // An added collection goes to the end of the listed rows, on the
  // Recommended tab and Home.
  const add = (key: string) => {
    setRows((cur) => {
      if (!cur) return cur
      const row = cur.find((r) => r.key === key)
      if (!row) return cur
      const rest = cur.filter((r) => r.key !== key)
      const lastListed = rest.reduce((at, r, i) => (shown.has(r.key) ? i : at), -1)
      rest.splice(lastListed + 1, 0, { ...row, library: true, home: true })
      return rest
    })
    setShown((cur) => new Set(cur).add(key))
    setQuery('')
  }

  // Drags move rows among the listed ones; unlisted rows keep their place
  // after them.
  const dragOver = (key: string) => {
    if (!dragging || dragging === key) return
    setRows((cur) => {
      if (!cur) return cur
      const vis = cur.filter((r) => shown.has(r.key))
      const from = vis.findIndex((r) => r.key === dragging)
      const to = vis.findIndex((r) => r.key === key)
      if (from < 0 || to < 0) return cur
      return [...moveItem(vis, from, to), ...cur.filter((r) => !shown.has(r.key))]
    })
  }

  const save = async () => {
    if (!rows) return
    setSaving(true)
    try {
      await api.put(`/api/admin/libraries/${library.id}/rows`, [...listed, ...rows.filter((r) => !shown.has(r.key))])
      onSaved?.()
      onClose()
    } catch (e) {
      setError((e as Error).message)
      setSaving(false)
    }
  }

  const reset = async () => {
    if (!confirm(`Reset ${library.name}'s rows to the defaults? Every collection is taken off the Recommended tab, Home and Friends' Home.`)) return
    setSaving(true)
    try {
      load(await api.del<LibraryRow[]>(`/api/admin/libraries/${library.id}/rows`))
      onSaved?.()
    } catch (e) {
      setError((e as Error).message)
    }
    setSaving(false)
  }

  return (
    <Modal title={`Manage recommendations · ${library.name}`} onClose={onClose} wide>
      <p className="muted small" style={{ marginTop: 0 }}>
        Drag rows into order. Library Recommended is the library's front page. Home rows from every pinned library are merged, so all
        movie libraries share one Recently Added Movies row. Friends' Home is Home for people outside your household. Collections show here
        once they're visible somewhere: add one below, or use "Visible on" in a collection's menu.
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
          {listed.map((r) => (
            <div
              key={r.key}
              role="row"
              className={`rec-row ${dragging === r.key ? 'dragging' : ''}`}
              draggable
              onDragStart={() => setDragging(r.key)}
              onDragEnd={() => setDragging(null)}
              onDragOver={(e) => {
                e.preventDefault()
                dragOver(r.key)
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
      {rows && rows.some((r) => r.collectionId && !shown.has(r.key)) && (
        <div className="rec-add">
          <input
            type="search"
            placeholder="Add a collection…"
            aria-label="Add a collection"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && matches[0] && add(matches[0].key)}
          />
          {matches.length > 0 && (
            <div className="rec-add-results" role="listbox">
              {matches.map((r) => (
                <button key={r.key} role="option" aria-selected={false} onClick={() => add(r.key)}>
                  <Icon name="stack" size={14} /> {r.title}
                </button>
              ))}
            </div>
          )}
          {query.trim() && matches.length === 0 && <div className="muted small">No hidden collection matches "{query.trim()}".</div>}
        </div>
      )}
      {error && <div className="form-error">{error}</div>}
      <div className="actions" style={{ marginTop: 16 }}>
        <button className="btn btn-glass" disabled={!rows || saving} onClick={reset}>
          <Icon name="refresh" size={16} /> Reset customization
        </button>
        <span style={{ flex: 1 }} />
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
