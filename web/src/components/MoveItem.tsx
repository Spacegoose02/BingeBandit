import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Item } from '../types'
import { Modal } from './Modal'

export interface ItemLocation {
  location: { libraryId: number; library: string; root: string; paths: string[]; problem?: string }
  targets: { libraryId: number; library: string; path: string }[]
}

export const locationPath = (id: number) => `/api/admin/items/${id}/location`

/**
 * Where a movie or show is on disk (to check it matched the right title),
 * and moving its folder to another library of the same kind: TV Shows to
 * Kids TV Shows and back, Movies to 4K or Christmas, and so on.
 */
export function MoveDialog({ item, onClose, onMoved }: { item: Item; onClose: () => void; onMoved: (msg: string) => void }) {
  const [data, setData] = useState<ItemLocation | null>(null)
  const [pick, setPick] = useState(-1)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.get<ItemLocation>(locationPath(item.id)).then(
      (d) => {
        setData(d)
        // Another library first: moving between a library's own folders is rarer.
        const other = d.targets.findIndex((t) => t.libraryId !== d.location.libraryId)
        setPick(other >= 0 ? other : d.targets.length ? 0 : -1)
      },
      (e) => setError(e.message),
    )
  }, [item.id])

  const loc = data?.location
  const target = data && pick >= 0 ? data.targets[pick] : undefined
  const many = (libraryId: number) => (data?.targets.filter((t) => t.libraryId === libraryId).length ?? 0) > 1
  const move = async () => {
    if (!target) return
    setBusy(true)
    setError('')
    try {
      const r = await api.post<{ copying: boolean }>(`/api/admin/items/${item.id}/move`, { libraryId: target.libraryId, path: target.path })
      onMoved(
        r.copying
          ? `Copying ${item.title} to ${target.library} in the background, since it's on another drive. It switches over when the copy is done; the Dashboard shows the progress.`
          : `Moved ${item.title} to ${target.library}.`,
      )
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal title={`Move ${item.title}`} onClose={onClose}>
      <div className="form">
        {loc && (
          <>
            <div>
              <div className="muted small">In {loc.library}</div>
              {loc.paths.map((p) => (
                <div key={p} className="track-line path">{p}</div>
              ))}
            </div>
            {loc.problem && <div className="form-error">{loc.problem}</div>}
          </>
        )}
        {data && !loc?.problem && (
          <>
            {data.targets.length === 0 ? (
              <p className="muted">There's no other {item.kind === 'show' ? 'TV' : 'movie'} library to move it to. Add one in Settings → Libraries.</p>
            ) : (
              <>
                <div className="muted small">Move it to</div>
                <div className="check-list">
                  {data.targets.map((t, i) => (
                    <label key={`${t.libraryId}:${t.path}`} className="check">
                      <input type="radio" name="move-target" checked={pick === i} onChange={() => setPick(i)} />
                      <span>
                        {t.library}
                        {(many(t.libraryId) || t.libraryId === loc?.libraryId) && <span className="muted small"> · {t.path}</span>}
                      </span>
                    </label>
                  ))}
                </div>
                <p className="muted small">
                  {item.kind === 'show' ? "The show's folder moves" : 'The movie moves'} on disk. Watched progress, artwork, edits and kid approvals stay with it, and
                  who can see it follows the new library's access.
                </p>
              </>
            )}
          </>
        )}
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy || !target || !!loc?.problem} onClick={move}>
            {busy ? 'Moving…' : 'Move'}
          </button>
          <button className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}
