import { useEffect, useState } from 'react'
import { api } from '../api'
import { episodeLabel, formatBytes } from '../format'
import type { Item } from '../types'
import { Modal } from './Modal'

interface DeletePlan {
  library: string
  paths: string[]
  files: number
  size: number
  also?: string[]
  problem?: string
}

const what = (it: Item) =>
  it.kind === 'show' ? 'show' : it.kind === 'season' ? 'season' : it.kind === 'episode' ? 'episode' : 'movie'

/**
 * Deleting a movie, show, season or episode from the server for good: its
 * files go from disk, not just from the library. The dialog lists exactly
 * what goes, and the admin types the title to confirm.
 */
export function DeleteDialog({ item, onClose, onDeleted }: { item: Item; onClose: () => void; onDeleted: (msg: string) => void }) {
  const [plan, setPlan] = useState<DeletePlan | null>(null)
  const [typed, setTyped] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.get<DeletePlan>(`/api/admin/items/${item.id}/delete`).then(setPlan, (e) => setError(e.message))
  }, [item.id])

  const name =
    item.kind === 'episode' || item.kind === 'season'
      ? `${item.showTitle ? `${item.showTitle} ` : ''}${item.kind === 'episode' ? episodeLabel(item) : item.title}`
      : item.title
  const matches = typed.trim().toLowerCase() === item.title.trim().toLowerCase()
  const go = async () => {
    setBusy(true)
    setError('')
    try {
      await api.post(`/api/admin/items/${item.id}/delete`, { confirm: typed })
      onDeleted(`Deleted ${name} from the server.`)
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal title={`Delete ${name} from the server`} onClose={onClose}>
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          if (matches && plan && !plan.problem && !busy) go()
        }}
      >
        <div className="form-error">
          This deletes the {what(item)}'s files from the disk for good. It can't be undone, and watched progress, ratings and edits for it go too.
        </div>
        {plan && (
          <>
            <div>
              <div className="muted small">
                From {plan.library}: {plan.files} {plan.files === 1 ? 'file' : 'files'}, {formatBytes(plan.size)}
              </div>
              {plan.paths.map((p) => (
                <div key={p} className="track-line path">{p}</div>
              ))}
            </div>
            {!!plan.also?.length && (
              <div className="form-error">It shares a file with {plan.also.join(', ')}, so {plan.also.length === 1 ? 'that episode goes' : 'those episodes go'} too.</div>
            )}
            {plan.problem && <div className="form-error">{plan.problem}</div>}
          </>
        )}
        {plan && !plan.problem && (
          <label>
            <span className="muted small">
              Type <b>{item.title}</b> to confirm
            </span>
            <input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus autoComplete="off" spellCheck={false} />
          </label>
        )}
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button type="submit" className="btn btn-danger" disabled={busy || !plan || !!plan.problem || !matches}>
            {busy ? 'Deleting…' : 'Delete from server'}
          </button>
          <button type="button" className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}
