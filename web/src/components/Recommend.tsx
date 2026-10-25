import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useApi } from '../hooks'
import type { Item } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

interface Recipient {
  id: number
  name: string
}

/** Button and dialog for recommending a movie or show to other people. */
export function RecommendButton({ item }: { item: Item }) {
  const [open, setOpen] = useState(false)
  const [done, setDone] = useState('')
  return (
    <div className="menu-anchor">
      <button className="btn btn-glass icon-only" onClick={() => setOpen(true)} title="Recommend to someone">
        <Icon name="users" size={18} />
      </button>
      {done && <div className="inline-note">{done}</div>}
      {open && (
        <RecommendDialog
          item={item}
          onClose={(msg) => {
            setOpen(false)
            if (msg) {
              setDone(msg)
              window.setTimeout(() => setDone(''), 2500)
            }
          }}
        />
      )}
    </div>
  )
}

function RecommendDialog({ item, onClose }: { item: Item; onClose: (msg?: string) => void }) {
  const { data: people } = useApi<Recipient[]>(`/api/items/${item.id}/recommend`)
  const [picked, setPicked] = useState<number[]>([])
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const toggle = (id: number) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  const send = async () => {
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ sent: number }>(`/api/items/${item.id}/recommend`, { userIds: picked, note })
      onClose(`Recommended to ${res.sent} ${res.sent === 1 ? 'person' : 'people'}`)
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <Modal title={`Recommend ${item.title}`} onClose={() => onClose()}>
      <div className="form">
        {people && people.length === 0 && (
          <p className="muted">
            Nobody can get recommendations yet. People turn them on under <Link to="/account">Account → Sharing</Link>, and they only see titles
            from libraries they can watch.
          </p>
        )}
        {people && people.length > 0 && (
          <>
            <div className="check-list">
              {people.map((p) => (
                <label key={p.id} className="check">
                  <input type="checkbox" checked={picked.includes(p.id)} onChange={() => toggle(p.id)} />
                  {p.name}
                </label>
              ))}
            </div>
            <label>
              Note (optional)
              <textarea rows={2} maxLength={280} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Why they'll like it" />
            </label>
          </>
        )}
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={busy || picked.length === 0} onClick={send}>
            Recommend
          </button>
          <button className="btn btn-glass" onClick={() => onClose()}>Cancel</button>
        </div>
      </div>
    </Modal>
  )
}
