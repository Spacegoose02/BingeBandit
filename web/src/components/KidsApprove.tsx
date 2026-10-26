import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import type { Item } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

interface KidState {
  id: number
  name: string
  approved: boolean
  avatar?: string
}

/**
 * Button and dialog for a parent (or an admin) to approve a movie or show for
 * kid profiles, who then find it under Kids Movies or Kids Shows even
 * without access to its library.
 */
export function KidsApproveButton({ item }: { item: Item }) {
  const { user } = useAuth()
  const canApprove = !!user && (user.isAdmin || !user.ownerId)
  const { data: kids, reload } = useApi<KidState[]>(canApprove ? `/api/items/${item.id}/kids` : null)
  const [open, setOpen] = useState(false)
  if (!kids || kids.length === 0) return null
  const approved = kids.filter((k) => k.approved).length
  return (
    <>
      <button
        className="btn btn-glass icon-only"
        onClick={() => setOpen(true)}
        title={approved ? `Approved for ${kids.filter((k) => k.approved).map((k) => k.name).join(', ')}` : 'Approve for kids'}
      >
        <Icon name="kids" size={18} className={approved ? 'accent' : ''} />
      </button>
      {open && (
        <KidsDialog
          item={item}
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

function KidsDialog({ item, kids, onClose }: { item: Item; kids: KidState[]; onClose: (saved?: boolean) => void }) {
  const [picked, setPicked] = useState(new Set(kids.filter((k) => k.approved).map((k) => k.id)))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const where = item.kind === 'show' ? 'Kids Shows' : 'Kids Movies'

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
      await api.put(`/api/items/${item.id}/kids`, { userIds: [...picked] })
      onClose(true)
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal title={`${item.title} for kids`} onClose={() => onClose()}>
      <div className="form">
        <p className="muted">
          Kids you tick can watch {item.kind === 'show' ? 'every episode of this show' : 'this movie'} from {where}, even without access to the
          library it's in. Mark a profile as a kid in <Link to="/account">Account → Profiles</Link>.
        </p>
        <div className="check-list">
          {kids.map((k) => (
            <label key={k.id} className="check">
              <input type="checkbox" checked={picked.has(k.id)} onChange={() => toggle(k.id)} />
              {k.name}
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
