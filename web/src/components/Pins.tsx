import { useState } from 'react'
import { api } from '../api'
import type { Library } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

/** Moves the item at from to to, for drag and drop reordering. */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  const next = list.slice()
  const [it] = next.splice(from, 1)
  next.splice(to, 0, it)
  return next
}

/** Fired on window after pins change, so Home can rebuild its rows. */
export const pinsChanged = 'bb:pins-changed'

export async function savePins(libraries: Library[]) {
  await api.put('/api/me/pins', { libraries: libraries.map((l) => ({ id: l.id, pinned: l.pinned })) })
  window.dispatchEvent(new Event(pinsChanged))
}

/**
 * Pick which libraries sit in the sidebar and feed Home, and drag them into
 * order. Shown once after the first sign-in, and from the sidebar's Edit.
 */
export function PinsDialog({ libraries, first, onClose, onSaved }: { libraries: Library[]; first?: boolean; onClose: () => void; onSaved: () => void }) {
  const [list, setList] = useState(() => [...libraries.filter((l) => l.pinned), ...libraries.filter((l) => !l.pinned)])
  const [dragging, setDragging] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const save = async () => {
    setSaving(true)
    try {
      await savePins(list)
      onSaved()
    } catch (e) {
      setError((e as Error).message)
      setSaving(false)
    }
  }
  // Skipping the first-time question keeps everything pinned and stops asking.
  const close = first ? () => savePins(libraries).finally(onClose) : onClose

  return (
    <Modal title={first ? 'What would you like to pin?' : 'Pinned libraries'} onClose={close}>
      <p className="muted small" style={{ marginTop: 0 }}>
        Pinned libraries sit in the sidebar and fill your Home. Drag to change the order. Unpinned ones stay under More.
      </p>
      <div className="pin-list">
        {list.map((l, i) => (
          <div
            key={l.id}
            className={`pin-row ${dragging === i ? 'dragging' : ''}`}
            draggable
            onDragStart={() => setDragging(i)}
            onDragEnd={() => setDragging(null)}
            onDragOver={(e) => {
              e.preventDefault()
              if (dragging !== null && dragging !== i) {
                setList((cur) => moveItem(cur, dragging, i))
                setDragging(i)
              }
            }}
          >
            <span className="drag-handle" aria-hidden="true">
              <Icon name="grip" size={16} />
            </span>
            <Icon name={l.kind === 'movies' ? 'film' : 'tv'} size={18} />
            <span className="pin-name">{l.name}</span>
            <button
              className={`pin-toggle ${l.pinned ? 'on' : ''}`}
              aria-pressed={l.pinned}
              title={l.pinned ? 'Unpin' : 'Pin'}
              onClick={() => setList((cur) => cur.map((x) => (x.id === l.id ? { ...x, pinned: !x.pinned } : x)))}
            >
              <Icon name="pin" size={16} />
            </button>
          </div>
        ))}
      </div>
      {error && <div className="form-error">{error}</div>}
      <div className="actions" style={{ marginTop: 16, justifyContent: 'flex-end' }}>
        {!first && (
          <button className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
        )}
        <button className="btn btn-primary" disabled={saving} onClick={save}>
          {first ? 'Done' : 'Save'}
        </button>
      </div>
    </Modal>
  )
}
