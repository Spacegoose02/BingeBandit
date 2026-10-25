import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import { timeAgo } from '../../format'
import type { CollectionMode, Library, ScanStatus } from '../../types'
import { ScanBadge } from './Dashboard'

export default function AdminLibraries() {
  useDocumentTitle('Libraries')
  const { data: libraries, reload } = useApi<Library[]>('/api/admin/libraries')
  const { data: scans } = useApi<ScanStatus[]>('/api/admin/scan', 2000)
  const [editing, setEditing] = useState<Library | 'new' | null>(null)

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      reload()
    } catch (e) {
      alert((e as Error).message)
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>Libraries</h1>
        <div className="actions">
          <button className="btn btn-glass" onClick={() => act(() => api.post('/api/admin/scan'))}>
            <Icon name="refresh" size={18} /> Scan all
          </button>
          <button className="btn btn-primary" onClick={() => setEditing('new')}>
            <Icon name="plus" size={18} /> Add library
          </button>
        </div>
      </header>

      <div className="lib-grid">
        {libraries?.map((l) => {
          const scan = scans?.find((s) => s.libraryId === l.id)
          return (
            <div key={l.id} className="panel lib-card">
              <div className="lib-card-head">
                <Icon name={l.kind === 'movies' ? 'film' : 'tv'} size={24} />
                <div className="grow">
                  <h3>{l.name}</h3>
                  <div className="muted small">
                    {l.kind === 'movies' ? 'Movies' : 'TV Shows'} · {l.itemCount} items · scanned {timeAgo(l.lastScan)}
                  </div>
                </div>
              </div>
              <ul className="path-list">
                {l.paths?.map((p) => (
                  <li key={p} className="mono small"><Icon name="folder" size={14} /> {p}</li>
                ))}
              </ul>
              {scan?.running && <ScanBadge scan={scan} />}
              {scan && !scan.running && scan.error && <div className="form-error small">Last scan failed: {scan.error}</div>}
              <div className="actions">
                <button className="btn btn-glass btn-sm" disabled={scan?.running} onClick={() => act(() => api.post(`/api/admin/libraries/${l.id}/scan`))}>
                  <Icon name="refresh" size={16} /> Scan
                </button>
                <button className="btn btn-glass btn-sm" disabled={scan?.running} onClick={() => act(() => api.post(`/api/admin/libraries/${l.id}/refresh`))}>
                  <Icon name="download" size={16} /> Refresh metadata
                </button>
                <button className="btn btn-glass btn-sm" onClick={() => setEditing(l)}>
                  <Icon name="edit" size={16} /> Edit
                </button>
                <button
                  className="btn btn-glass btn-sm danger"
                  onClick={() => confirm(`Remove "${l.name}"? Your files stay on disk; watch history for this library is lost.`) && act(() => api.del(`/api/admin/libraries/${l.id}`))}
                >
                  <Icon name="trash" size={16} />
                </button>
              </div>
            </div>
          )
        })}
      </div>
      {libraries?.length === 0 && <div className="empty">No libraries yet. Add a folder of movies or TV shows to get started.</div>}

      {editing && (
        <LibraryEditor
          library={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
    </div>
  )
}

function LibraryEditor({ library, onClose, onSaved }: { library: Library | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(library?.name ?? '')
  const [kind, setKind] = useState<'movies' | 'shows'>(library?.kind ?? 'movies')
  const [paths, setPaths] = useState<string[]>(library?.paths ?? [])
  const [picking, setPicking] = useState(paths.length === 0)
  const [detect, setDetect] = useState(!library?.skipMarkers)
  const [collectionMode, setCollectionMode] = useState<CollectionMode>(library?.collectionMode ?? 'show')
  const [error, setError] = useState('')

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      const body = { name, kind, paths, skipMarkers: !detect, collectionMode }
      if (library) await api.put(`/api/admin/libraries/${library.id}`, body)
      else await api.post('/api/admin/libraries', body)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <Modal title={library ? `Edit ${library.name}` : 'Add library'} onClose={onClose} wide>
      <form className="form" onSubmit={save}>
        {!library && (
          <div className="kind-picker">
            {(['movies', 'shows'] as const).map((k) => (
              <button type="button" key={k} className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>
                <Icon name={k === 'movies' ? 'film' : 'tv'} size={26} />
                {k === 'movies' ? 'Movies' : 'TV Shows'}
              </button>
            ))}
          </div>
        )}
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === 'movies' ? 'Movies' : 'TV Shows'} required />
        </label>
        <div>
          <div className="label">Folders</div>
          {paths.map((p) => (
            <div key={p} className="path-row">
              <Icon name="folder" size={16} />
              <span className="mono grow">{p}</span>
              <button type="button" className="icon-btn" onClick={() => setPaths(paths.filter((x) => x !== p))} aria-label="Remove folder">
                <Icon name="x" size={16} />
              </button>
            </div>
          ))}
          {picking ? (
            <FolderPicker
              onPick={(p) => {
                if (!paths.includes(p)) setPaths([...paths, p])
                setPicking(false)
              }}
              onCancel={paths.length ? () => setPicking(false) : undefined}
            />
          ) : (
            <button type="button" className="btn btn-glass btn-sm" onClick={() => setPicking(true)}>
              <Icon name="plus" size={16} /> Add folder
            </button>
          )}
        </div>
        <p className="muted small">
          {kind === 'movies'
            ? 'Tip: name folders "Movie Title (Year)" for the best matches.'
            : 'Tip: use "Show Name/Season 01/Show Name - S01E01.mkv" for the best matches.'}
        </p>
        <label className="check">
          <input type="checkbox" checked={detect} onChange={(e) => setDetect(e.target.checked)} />
          {kind === 'movies' ? 'Detect end credits (for the Skip Credits button)' : 'Detect intros and end credits (for the Skip buttons)'}
        </label>
        <label>
          Collections on the Library tab
          <select value={collectionMode} onChange={(e) => setCollectionMode(e.target.value as CollectionMode)}>
            <option value="show">Show collections and their items</option>
            <option value="hide">Hide collections but show their items</option>
            <option value="collapse">Hide items which are in collections</option>
          </select>
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={!name || paths.length === 0}>{library ? 'Save' : 'Add library'}</button>
        </div>
      </form>
    </Modal>
  )
}

interface Browse {
  path: string
  parent: string
  dirs: { name: string; path: string }[]
}

function FolderPicker({ onPick, onCancel }: { onPick: (path: string) => void; onCancel?: () => void }) {
  const [path, setPath] = useState('')
  const [typed, setTyped] = useState('')
  const { data, error } = useApi<Browse>(`/api/admin/browse?path=${encodeURIComponent(path)}`)
  return (
    <div className="folder-picker">
      <div className="folder-bar">
        <button type="button" className="icon-btn" disabled={!data?.path} onClick={() => setPath(data?.parent ?? '')} aria-label="Up">
          <Icon name="back" size={18} />
        </button>
        <input
          className="mono"
          value={typed || data?.path || ''}
          placeholder="Type a path or browse below"
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              setPath(typed)
              setTyped('')
            }
          }}
        />
      </div>
      {error && <div className="form-error small">{error}</div>}
      <div className="folder-list">
        {data?.dirs.map((d) => (
          <button type="button" key={d.path} onClick={() => { setPath(d.path); setTyped('') }}>
            <Icon name="folder" size={16} /> {d.name}
          </button>
        ))}
        {data && data.dirs.length === 0 && <div className="muted small pad">No sub-folders</div>}
      </div>
      <div className="actions end">
        {onCancel && <button type="button" className="btn btn-glass btn-sm" onClick={onCancel}>Cancel</button>}
        <button type="button" className="btn btn-primary btn-sm" disabled={!data?.path} onClick={() => data && onPick(data.path)}>
          Use this folder
        </button>
      </div>
    </div>
  )
}
