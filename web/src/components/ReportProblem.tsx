import { useState } from 'react'
import { api } from '../api'
import type { Item, ProblemReport } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

const kinds: [ProblemReport['kind'], string, string][] = [
  ['video', 'Picture', 'Blocky, frozen, wrong colours'],
  ['audio', 'Sound', 'Missing, out of sync, wrong language'],
  ['subtitles', 'Subtitles', 'Missing, wrong or out of sync'],
  ['wrong', 'Wrong file', 'A different movie or episode'],
  ['other', 'Something else', ''],
]

/**
 * Telling an admin something's wrong with a movie or episode. From the
 * player it also sends where you were, so they can jump to it.
 */
export function ReportProblemDialog({ itemId, title, position, onClose }: { itemId: number; title: string; position?: number; onClose: (msg?: string) => void }) {
  const [kind, setKind] = useState<ProblemReport['kind'] | ''>('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const send = async () => {
    if (!kind) return
    setBusy(true)
    try {
      await api.post(`/api/items/${itemId}/problems`, { kind, note, position: position ?? 0 })
      onClose('Thanks, an admin will take a look.')
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <Modal title={`Report a problem with ${title}`} onClose={() => onClose()}>
      <div className="form">
        <div className="problem-kinds">
          {kinds.map(([k, label, hint]) => (
            <button key={k} className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>
              <div><strong>{label}</strong></div>
              {hint && <div className="muted small">{hint}</div>}
            </button>
          ))}
        </div>
        <label>
          What’s wrong? (optional)
          <input value={note} maxLength={1000} onChange={(e) => setNote(e.target.value)} placeholder="E.g. the sound drops out after 20 minutes" />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={!kind || busy} onClick={send}>Send report</button>
          <button className="btn btn-glass" onClick={() => onClose()}>Cancel</button>
        </div>
      </div>
    </Modal>
  )
}

export function ReportProblemButton({ item }: { item: Item }) {
  const [open, setOpen] = useState(false)
  const [done, setDone] = useState('')
  const title = item.kind === 'episode' && item.showTitle ? `${item.showTitle}: ${item.title}` : item.title
  return (
    <div className="menu-anchor">
      <button className="btn btn-glass icon-only" onClick={() => setOpen(true)} title="Report a problem">
        <Icon name="info" size={18} />
      </button>
      {done && <div className="inline-note">{done}</div>}
      {open && (
        <ReportProblemDialog
          itemId={item.id}
          title={title}
          onClose={(msg) => {
            setOpen(false)
            if (msg) {
              setDone(msg)
              window.setTimeout(() => setDone(''), 3000)
            }
          }}
        />
      )}
    </div>
  )
}
