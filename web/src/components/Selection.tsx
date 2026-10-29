import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { smartRuleText } from '../format'
import type { Collection, Item, Playlist } from '../types'
import { ChipsInput } from './ChipsInput'
import { Icon } from './Icon'
import { Modal } from './Modal'

/**
 * Plex-style multi-select. Hovering a poster shows a circle in its top left
 * corner; ticking it starts a selection, after which clicking any poster (or
 * shift-clicking for a run of them) adds or drops it. A bar along the bottom
 * then adds them all to a playlist, or, for admins, edits their collections
 * and labels in one go. Leaving the page clears the selection.
 */
export type Picked =
  | { key: string; kind: 'item'; item: Item; reload?: () => void }
  | { key: string; kind: 'collection'; collection: Collection; reload?: () => void }

interface Selection {
  picked: Map<string, Picked>
  toggle: (p: Picked, range: boolean) => void
  register: (p: Picked) => () => void
}

const Ctx = createContext<Selection | null>(null)

/** The keys of the selectable cards on the page, top to bottom. */
function pageKeys(): string[] {
  const keys: string[] = []
  document.querySelectorAll<HTMLElement>('[data-select-key]').forEach((el) => {
    const k = el.dataset.selectKey!
    if (!keys.includes(k)) keys.push(k)
  })
  return keys
}

export function SelectionProvider({ children }: { children: ReactNode }) {
  const [picked, setPicked] = useState<Map<string, Picked>>(() => new Map())
  const cards = useRef(new Map<string, Picked>())
  const last = useRef('')
  const loc = useLocation()

  const clear = useCallback(() => setPicked(new Map()), [])
  useEffect(clear, [loc.pathname, clear])

  const register = useCallback((p: Picked) => {
    cards.current.set(p.key, p)
    return () => {
      if (cards.current.get(p.key) === p) cards.current.delete(p.key)
    }
  }, [])

  const toggle = useCallback((p: Picked, range: boolean) => {
    const prev = last.current
    last.current = p.key
    setPicked((cur) => {
      const next = new Map(cur)
      const keys = range && prev && cur.size ? pageKeys() : []
      const from = keys.indexOf(prev)
      const to = keys.indexOf(p.key)
      if (from >= 0 && to >= 0) {
        for (const k of keys.slice(Math.min(from, to), Math.max(from, to) + 1)) {
          const c = cards.current.get(k)
          if (c) next.set(k, c)
        }
      } else if (next.has(p.key)) next.delete(p.key)
      else next.set(p.key, p)
      return next
    })
  }, [])

  const selectAll = () => {
    const next = new Map(picked)
    for (const k of pageKeys()) {
      const c = cards.current.get(k)
      if (c) next.set(k, c)
    }
    setPicked(next)
  }

  const active = picked.size > 0
  useEffect(() => {
    document.body.classList.toggle('selecting', active)
    if (!active) return
    const key = (e: KeyboardEvent) => e.key === 'Escape' && !document.querySelector('.modal-backdrop') && clear()
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [active, clear])

  const value = useMemo(() => ({ picked, toggle, register }), [picked, toggle, register])
  return (
    <Ctx.Provider value={value}>
      {children}
      {active && <SelectionBar picked={picked} clear={clear} selectAll={selectAll} />}
    </Ctx.Provider>
  )
}

/**
 * Hooks a card up to the selection: data-select-key and the click capture go
 * on the card, the circle inside it.
 */
export function useSelectable(p: Picked) {
  const sel = useContext(Ctx)
  const register = sel?.register
  useEffect(() => register?.(p), [register, p])
  if (!sel) return null
  const selected = sel.picked.has(p.key)
  const active = sel.picked.size > 0
  return {
    selected,
    cardProps: {
      'data-select-key': p.key,
      // While selecting, a click anywhere on a card picks it instead of opening it.
      onClickCapture: (e: MouseEvent) => {
        if (!active || (e.target as Element).closest('.card-select')) return
        e.preventDefault()
        e.stopPropagation()
        sel.toggle(p, e.shiftKey)
      },
    },
    circle: (
      <button
        type="button"
        className={`card-select${selected ? ' on' : ''}`}
        role="checkbox"
        aria-checked={selected}
        aria-label={`Select ${p.kind === 'item' ? p.item.title : p.collection.name}`}
        title={selected ? 'Selected' : 'Select'}
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          sel.toggle(p, e.shiftKey)
        }}
      >
        {selected && <Icon name="check" size={16} />}
      </button>
    ),
  }
}

function count(n: number, one: string, many: string) {
  return `${n} ${n === 1 ? one : many}`
}

function SelectionBar({ picked, clear, selectAll }: { picked: Map<string, Picked>; clear: () => void; selectAll: () => void }) {
  const { user } = useAuth()
  const [dialog, setDialog] = useState<null | 'edit' | 'playlist'>(null)
  const [note, setNote] = useState('')
  const picks = [...picked.values()]
  const items = picks.flatMap((p) => (p.kind === 'item' ? [p.item] : []))

  useEffect(() => {
    if (!note) return
    const t = window.setTimeout(() => setNote(''), 3000)
    return () => window.clearTimeout(t)
  }, [note])

  const finish = (msg: string) => {
    setDialog(null)
    setNote(msg)
    new Set(picks.map((p) => p.reload).filter(Boolean)).forEach((f) => f!())
  }

  return (
    <>
      <div className="select-bar" role="toolbar" aria-label="Selected titles">
        <span className="select-count">{picked.size} selected</span>
        <button className="btn btn-glass btn-sm" onClick={selectAll}>Select all</button>
        {items.length > 0 && (
          <button className="btn btn-glass btn-sm" onClick={() => setDialog('playlist')}>
            <Icon name="list" size={16} /> Add to playlist…
          </button>
        )}
        {user?.isAdmin && (
          <button className="btn btn-primary btn-sm" onClick={() => setDialog('edit')}>
            <Icon name="edit" size={16} /> Edit…
          </button>
        )}
        <button className="icon-btn" onClick={clear} aria-label="Clear selection" title="Clear selection (Esc)">
          <Icon name="x" size={18} />
        </button>
      </div>
      {note && <div className="select-note">{note}</div>}
      {dialog === 'edit' && <BulkEditDialog picks={picks} onClose={() => setDialog(null)} onSaved={finish} />}
      {dialog === 'playlist' && <BulkPlaylistDialog items={items} onClose={() => setDialog(null)} onAdded={finish} />}
    </>
  )
}

function BulkPlaylistDialog({ items, onClose, onAdded }: { items: Item[]; onClose: () => void; onAdded: (msg: string) => void }) {
  const [lists, setLists] = useState<Playlist[] | null>(null)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const itemIds = items.map((it) => it.id)
  useEffect(() => {
    api.get<Playlist[]>('/api/playlists').then(setLists, (e) => setError(e.message))
  }, [])

  const run = async (f: () => Promise<string>) => {
    setBusy(true)
    setError('')
    try {
      onAdded(await f())
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }
  const add = (p: Playlist) =>
    run(async () => {
      const r = await api.post<{ added: number }>(`/api/playlists/${p.id}/items`, { itemIds })
      return r.added ? `Added ${count(r.added, 'video', 'videos')} to ${p.name}.` : `Everything was already in ${p.name}.`
    })
  const create = () =>
    run(async () => {
      const p = await api.post<Playlist>('/api/playlists', { name: name.trim(), itemIds })
      return `Made ${p.name}.`
    })

  return (
    <Modal title={`Add ${count(items.length, 'title', 'titles')} to a playlist`} onClose={onClose}>
      <div className="bulk-lists">
        {lists?.map((p) => (
          <button key={p.id} className="bulk-list" disabled={busy} onClick={() => add(p)}>
            <Icon name="list" size={16} /> {p.name}
            <span className="muted small push">{p.itemCount}</span>
          </button>
        ))}
        {lists?.length === 0 && <div className="muted small">No playlists yet.</div>}
      </div>
      <form
        className="field-row"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) create()
        }}
      >
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New playlist name" />
        <button className="btn btn-primary btn-sm" disabled={!name.trim() || busy}>Create</button>
      </form>
      {items.some((it) => it.kind === 'show' || it.kind === 'season') && (
        <p className="muted small">Shows and seasons add all of their episodes.</p>
      )}
      {error && <div className="form-error">{error}</div>}
    </Modal>
  )
}

/** Sorted, case-insensitively unique. */
function uniq(list: string[]) {
  const seen = new Set<string>()
  return list
    .filter((v) => {
      const k = v.toLowerCase()
      return !seen.has(k) && !!seen.add(k)
    })
    .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
}

/**
 * Edits every selected title at once: movies and shows get collections and
 * labels to add or take away; collections only have labels. Seasons and
 * episodes are left alone.
 */
function BulkEditDialog({ picks, onClose, onSaved }: { picks: Picked[]; onClose: () => void; onSaved: (msg: string) => void }) {
  const titles = picks.flatMap((p) => (p.kind === 'item' && (p.item.kind === 'movie' || p.item.kind === 'show') ? [p.item] : []))
  const cols = picks.flatMap((p) => (p.kind === 'collection' ? [p.collection] : []))
  const skipped = picks.length - titles.length - cols.length
  const libs = [...new Set(titles.map((t) => t.libraryId))]

  const [addCols, setAddCols] = useState<string[]>([])
  const [removeCols, setRemoveCols] = useState<string[]>([])
  const [addLabels, setAddLabels] = useState<string[]>([])
  const [removeLabels, setRemoveLabels] = useState<string[]>([])
  const [addColLabels, setAddColLabels] = useState<string[]>([])
  const [removeColLabels, setRemoveColLabels] = useState<string[]>([])
  const [known, setKnown] = useState<{ collections: string[]; labels: string[]; colLabels: string[] }>({ collections: [], labels: [], colLabels: [] })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Suggestions: the hand-picked collections and the labels in the titles' libraries.
  useEffect(() => {
    libs.forEach((id) => {
      api.get<Collection[]>(`/api/libraries/${id}/collections`).then(
        (cs) => setKnown((k) => ({ ...k, collections: uniq([...k.collections, ...cs.filter((c) => !smartRuleText(c)).map((c) => c.name)]) })),
        () => undefined,
      )
      api.get<string[]>(`/api/admin/libraries/${id}/labels`).then((ls) => setKnown((k) => ({ ...k, labels: uniq([...k.labels, ...ls]) })), () => undefined)
    })
    if (cols.length) api.get<string[]>('/api/admin/collection-labels').then((ls) => setKnown((k) => ({ ...k, colLabels: uniq(ls) })), () => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const onTitles = uniq(titles.flatMap((t) => t.labels ?? []))
  const onCols = uniq(cols.flatMap((c) => c.labels ?? []))
  const nothing = !addCols.length && !removeCols.length && !addLabels.length && !removeLabels.length && !addColLabels.length && !removeColLabels.length

  const save = async () => {
    setBusy(true)
    setError('')
    try {
      const changed: string[] = []
      if (titles.length && (addCols.length || removeCols.length || addLabels.length || removeLabels.length)) {
        const r = await api.post<{ items: number }>('/api/admin/bulk', {
          itemIds: titles.map((t) => t.id),
          addCollections: addCols,
          removeCollections: removeCols,
          addLabels,
          removeLabels,
        })
        if (r.items) changed.push(count(r.items, 'title', 'titles'))
      }
      if (cols.length && (addColLabels.length || removeColLabels.length)) {
        const r = await api.post<{ collections: number }>('/api/admin/bulk', {
          collectionIds: cols.map((c) => c.id),
          addLabels: addColLabels,
          removeLabels: removeColLabels,
        })
        if (r.collections) changed.push(count(r.collections, 'collection', 'collections'))
      }
      onSaved(changed.length ? `Updated ${changed.join(' and ')}.` : 'Nothing needed changing.')
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  const what = [titles.length && count(titles.length, 'title', 'titles'), cols.length && count(cols.length, 'collection', 'collections')].filter(Boolean).join(' and ')
  return (
    <Modal title={`Edit ${what || 'selection'}`} onClose={onClose}>
      <div className="bulk-edit">
        {titles.length > 0 && (
          <section>
            {cols.length > 0 && <h4>{count(titles.length, 'movie or show', 'movies and shows')}</h4>}
            <div className="field">
              <span><Icon name="stack" size={14} /> Add to collections</span>
              <ChipsInput value={addCols} onChange={setAddCols} suggestions={known.collections} placeholder="Pick or type a new collection" />
            </div>
            <div className="field">
              <span><Icon name="stack" size={14} /> Remove from collections</span>
              <ChipsInput value={removeCols} onChange={setRemoveCols} suggestions={known.collections} placeholder="Pick a collection" />
            </div>
            <div className="field">
              <span><Icon name="tag" size={14} /> Add labels</span>
              <ChipsInput value={addLabels} onChange={setAddLabels} suggestions={known.labels} placeholder="Pick or type a label" />
            </div>
            <div className="field">
              <span><Icon name="tag" size={14} /> Remove labels</span>
              <ChipsInput value={removeLabels} onChange={setRemoveLabels} suggestions={uniq([...onTitles, ...known.labels])} placeholder="Pick a label" />
            </div>
            <p className="muted small">Labels also feed smart collections with a label rule. New collection names are made in each title's own library.</p>
          </section>
        )}
        {cols.length > 0 && (
          <section>
            {titles.length > 0 && <h4>{count(cols.length, 'collection', 'collections')}</h4>}
            <div className="field">
              <span><Icon name="tag" size={14} /> Add labels</span>
              <ChipsInput value={addColLabels} onChange={setAddColLabels} suggestions={known.colLabels} placeholder="Pick or type a label" />
            </div>
            <div className="field">
              <span><Icon name="tag" size={14} /> Remove labels</span>
              <ChipsInput value={removeColLabels} onChange={setRemoveColLabels} suggestions={uniq([...onCols, ...known.colLabels])} placeholder="Pick a label" />
            </div>
          </section>
        )}
        {skipped > 0 && <p className="muted small">{count(skipped, 'season or episode is', 'seasons and episodes are')} left out: only movies, shows and collections can be edited here.</p>}
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={busy || nothing || (!titles.length && !cols.length)} onClick={save}>
            Apply
          </button>
        </div>
      </div>
    </Modal>
  )
}
