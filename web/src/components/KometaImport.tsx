import { useState } from 'react'
import { ApiError } from '../api'
import type { Visibility } from '../types'
import { Modal } from './Modal'

interface Planned {
  id?: number
  name: string
  sortTitle?: string
  lists: string[]
  sort: string
  exists: boolean
  visible: Visibility
  skipped?: string[]
}

const sortNames: Record<string, string> = {
  '': 'release date',
  release_desc: 'newest first',
  title: 'title',
  added: 'recently added',
  rating: 'rating',
  list: 'list order',
}

async function send(libraryId: number, file: File, dryRun: boolean, recommended: boolean): Promise<Planned[]> {
  const q = new URLSearchParams({ dryRun: dryRun ? '1' : '', recommended: recommended ? '1' : '' })
  const res = await fetch(`/api/admin/libraries/${libraryId}/kometa?${q}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-Requested-With': 'BingeBandit', 'Content-Type': 'application/yaml' },
    body: file,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data.collections
}

const where = (v: Visibility) => [v.library && 'Recommended', v.home && 'Home', v.friends && "Friends' Home"].filter(Boolean).join(', ') || 'Collections tab only'

/** Reads a Kometa collection file (one per library) into this library's collections. */
export function KometaImport({ libraryId, onClose, onSaved }: { libraryId: number; onClose: () => void; onSaved: () => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [recommended, setRecommended] = useState(true)
  const [plan, setPlan] = useState<Planned[] | null>(null)
  const [done, setDone] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const run = async (dryRun: boolean, f = file, rec = recommended) => {
    if (!f) return
    setBusy(true)
    setError('')
    try {
      setPlan(await send(libraryId, f, dryRun, rec))
      if (!dryRun) {
        setDone(true)
        onSaved()
      }
    } catch (err) {
      setPlan(null)
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Import a Kometa file" onClose={onClose}>
      <div className="form">
        <p className="muted small">
          Pick the Kometa collection file you use for this library (the YAML with <code>collections:</code>). Each collection follows the same
          MDBList, Trakt, TMDB, IMDb and Yamtrack lists, keeps its sort title, summary, poster and sort, and shows where its{' '}
          <code>visible_*</code> settings say. Collections with the same name are updated, so importing again is safe. Radarr and Sonarr settings
          are ignored.
        </p>
        <label>
          Kometa file
          <input
            type="file"
            accept=".yml,.yaml,text/yaml"
            onChange={(e) => {
              const f = e.target.files?.[0] ?? null
              setFile(f)
              setDone(false)
              run(true, f)
            }}
          />
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={recommended}
            onChange={(e) => {
              setRecommended(e.target.checked)
              if (!done) run(true, file, e.target.checked)
            }}
          />
          Show the collections on the Recommended tab, unless the file turns visible_library off
        </label>
        {error && <div className="form-error">{error}</div>}
        {plan && (
          <div className="kometa-plan">
            <div className="muted small">
              {done ? 'Imported' : 'Will import'} {plan.length} collections ({plan.filter((p) => !p.exists).length} new). They're listed in sort
              title order on the Collections tab, the way Plex lists them.
            </div>
            <ul>
              {plan.map((p) => (
                <li key={p.name}>
                  <strong>{p.name}</strong> {p.exists ? <span className="muted small">(update)</span> : <span className="muted small">(new)</span>}
                  <div className="muted small">
                    {p.lists.length} {p.lists.length === 1 ? 'list' : 'lists'} · sorted by {sortNames[p.sort] ?? p.sort} · {where(p.visible)}
                    {p.sortTitle && ` · sort title ${p.sortTitle}`}
                  </div>
                  {p.skipped?.map((s) => (
                    <div key={s} className="small warn-text">{s}</div>
                  ))}
                </li>
              ))}
            </ul>
          </div>
        )}
        {done && (
          <p className="muted small">
            Posters are downloading and lists are syncing in the background, which can take a few minutes. The “Sync list collections” task
            refreshes them every 6 hours. A collection's editor shows if its list couldn't be read.
          </p>
        )}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>
            {done ? 'Close' : 'Cancel'}
          </button>
          {!done && (
            <button className="btn btn-primary" disabled={!plan || busy} onClick={() => run(false)}>
              {busy ? 'Working…' : 'Import'}
            </button>
          )}
        </div>
      </div>
    </Modal>
  )
}
