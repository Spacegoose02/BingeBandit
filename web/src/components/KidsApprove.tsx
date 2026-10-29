import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import type { Collection, Item } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

export interface KidState {
  id: number
  name: string
  approved: boolean
  avatar?: string
  /** A collection approved for the kid that has this title. */
  via?: string
  /** Kept out even though `via` has it. */
  blocked?: boolean
}

/**
 * Button and dialog for a parent (or an admin) to approve a movie or show for
 * kid profiles, who then find it under Approved Movies or Approved Shows even
 * without access to its library.
 */
export function KidsApproveButton({ item }: { item: Item }) {
  const where = item.kind === 'show' ? 'Approved Shows' : 'Approved Movies'
  return (
    <ApproveButton
      path={`/api/items/${item.id}/kids`}
      name={item.title}
      what={`${item.kind === 'show' ? 'every episode of this show' : 'this movie'} from ${where}`}
    />
  )
}

/**
 * The same for a whole collection: the kids get every title in it, including
 * ones added later, under Approved Movies or Approved Shows.
 */
export function KidsApproveCollectionButton({ collection, shows }: { collection: Collection; shows: boolean }) {
  return (
    <ApproveButton
      path={`/api/collections/${collection.id}/kids`}
      name={collection.name}
      what={`everything in this collection, and anything added to it later, from ${shows ? 'Approved Shows' : 'Approved Movies'}`}
      collection
    />
  )
}

function ApproveButton({ path, name, what, collection }: { path: string; name: string; what: string; collection?: boolean }) {
  const { user } = useAuth()
  const canApprove = !!user && (user.isAdmin || !user.ownerId)
  const { data: kids, reload } = useApi<KidState[]>(canApprove ? path : null)
  const [open, setOpen] = useState(false)
  if (!kids || kids.length === 0) return null
  const approved = kids.filter((k) => k.approved)
  const label = collection ? 'Approve collection for kids' : 'Approve for kids'
  return (
    <>
      <button
        className={collection ? 'btn btn-glass btn-sm' : 'btn btn-glass icon-only'}
        onClick={() => setOpen(true)}
        title={approved.length ? `Approved for ${approved.map((k) => k.name).join(', ')}` : label}
        aria-label={label}
      >
        <Icon name="kids" size={collection ? 16 : 18} className={approved.length ? 'accent' : ''} />
        {collection && (approved.length ? ' Approved for kids' : ' Approve for kids')}
      </button>
      {open && (
        <KidsDialog
          path={path}
          name={name}
          what={what}
          kids={kids}
          onClose={(saved) => {
            setOpen(false)
            if (saved) reload()
          }}
        />
      )}
    </>
  )
}

export function KidsDialog({ path, name, what, kids, onClose }: { path: string; name: string; what: string; kids: KidState[]; onClose: (saved?: boolean) => void }) {
  const [picked, setPicked] = useState(new Set(kids.filter((k) => k.approved).map((k) => k.id)))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const toggle = (id: number) => {
    const next = new Set(picked)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setPicked(next)
  }
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      await api.put(path, { userIds: [...picked] })
      onClose(true)
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal title={`${name} for kids`} onClose={() => onClose()}>
      <div className="form">
        <p className="muted">
          Kids you tick can watch {what}, even without access to the library it's in. Mark a profile as a kid in{' '}
          <Link to="/account">Account → Profiles</Link>.
        </p>
        <div className="check-list">
          {kids.map((k) => (
            <label key={k.id} className="check">
              <input type="checkbox" checked={picked.has(k.id)} onChange={() => toggle(k.id)} />
              {k.name}
              {k.via && (
                <span className="muted">
                  {' '}
                  {picked.has(k.id) ? `(from ${k.via}; untick to keep it out)` : `(kept out of ${k.via})`}
                </span>
              )}
            </label>
          ))}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy} onClick={save}>
            Save
          </button>
          <button className="btn btn-glass" onClick={() => onClose()}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}
