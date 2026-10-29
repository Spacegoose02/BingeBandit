import { useState, type FormEvent } from 'react'
import { api, img } from '../api'
import { chartsFor } from '../charts'
import { useApi } from '../hooks'
import type { Collection, CollectionMode, CollectionSort } from '../types'
import { ChipsInput } from './ChipsInput'
import { Modal } from './Modal'
import { TPDbSearch } from './TPDbSearch'

interface Facets {
  labels: string[]
  genres: string[]
  studios: string[]
}

// Plex's per-collection Collection Mode, which overrides the library's.
const modes: [CollectionMode | '', string][] = [
  ['', 'Library default'],
  ['collapse', 'Hide items in this collection'],
  ['show', 'Show this collection and its items'],
  ['hide', 'Hide this collection but show its items'],
]

// Plex's Collection Sort.
const sorts: [CollectionSort, string][] = [
  ['', 'Release date (oldest first)'],
  ['release_desc', 'Release date (newest first)'],
  ['title', 'Title'],
  ['added', 'Recently added'],
  ['rating', 'Rating'],
  ['list', 'List order (the lists it follows)'],
]

const yearValue = (s: string) => (s ? Number(s) : 0)

/** Create (libraryId) or edit (collection) a collection. */
export function CollectionEditor({
  libraryId,
  libraryKind = 'movies',
  collection,
  onClose,
  onSaved,
}: {
  libraryId: number
  libraryKind?: string
  collection?: Collection
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(collection?.name ?? '')
  const [overview, setOverview] = useState(collection?.overview ?? '')
  const [sortTitle, setSortTitle] = useState(collection?.sortTitle ?? '')
  const [listUrl, setListUrl] = useState(collection?.listUrl ?? '')
  const [mode, setMode] = useState<CollectionMode | ''>(collection?.mode ?? '')
  const [sort, setSort] = useState<CollectionSort>(collection?.sort ?? '')
  const [labelRule, setLabelRule] = useState(collection?.labelRule ?? '')
  const [genreRule, setGenreRule] = useState(collection?.genreRule ?? '')
  const [studioRule, setStudioRule] = useState(collection?.studioRule ?? '')
  const [yearFrom, setYearFrom] = useState(collection?.yearFrom ? String(collection.yearFrom) : '')
  const [yearTo, setYearTo] = useState(collection?.yearTo ? String(collection.yearTo) : '')
  const [labels, setLabels] = useState<string[]>(collection?.labels ?? [])
  const { data: usedLabels } = useApi<string[]>(`/api/admin/collection-labels?library=${libraryId}`)
  const [error, setError] = useState('')
  const [poster, setPoster] = useState(collection?.poster ?? '')
  const [posterUrl, setPosterUrl] = useState('')
  const [posterBusy, setPosterBusy] = useState(false)
  const { data: facets } = useApi<Facets>(`/api/admin/libraries/${libraryId}/facets`)

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    const rules = { listUrl, sort, labels, labelRule, genreRule, studioRule, yearFrom: yearValue(yearFrom), yearTo: yearValue(yearTo) }
    try {
      if (collection) await api.put(`/api/admin/collections/${collection.id}`, { name, overview, mode, sortTitle, ...rules })
      else await api.post(`/api/admin/libraries/${libraryId}/collections`, { name, ...rules })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const addChart = (url: string) => {
    if (!url) return
    setListUrl((cur) => (cur.trim() ? `${cur.trim()}\n${url}` : url))
    if (!collection && !listUrl.trim() && sort === '') setSort('list')
  }

  const changePoster = async (body: { url: string } | File) => {
    if (!collection) return
    setError('')
    setPosterBusy(true)
    try {
      let c: Collection
      if (body instanceof File) {
        const form = new FormData()
        form.append('file', body)
        c = await api.post<Collection>(`/api/admin/collections/${collection.id}/artwork?type=poster`, form)
      } else {
        c = await api.put<Collection>(`/api/admin/collections/${collection.id}/artwork`, { type: 'poster', url: body.url })
      }
      setPoster(c.poster ?? '')
      setPosterUrl('')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setPosterBusy(false)
    }
  }

  return (
    <Modal title={collection ? `Edit ${collection.name}` : 'New collection'} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus placeholder="e.g. 80s Horror" />
        </label>
        {collection && (
          <label>
            Sort title
            <input value={sortTitle} onChange={(e) => setSortTitle(e.target.value)} placeholder={name} spellCheck={false} />
            <span className="muted small">Orders this collection among the others. Start it with something like “+_1” to put it first.</span>
          </label>
        )}
        {collection && (
          <label>
            Summary
            <textarea rows={3} value={overview} onChange={(e) => setOverview(e.target.value)} />
          </label>
        )}
        {collection && (
          <label>
            Collection mode
            <select value={mode} onChange={(e) => setMode(e.target.value as CollectionMode | '')}>
              {modes.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
            <span className="muted small">How this collection and its titles show on the library's Library tab.</span>
          </label>
        )}
        <label>
          Sort titles by
          <select value={sort} onChange={(e) => setSort(e.target.value as CollectionSort)}>
            {sorts.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
        </label>
        {collection && (
          <div className="collection-poster">
            {poster && <img src={img(poster, 'w185')} alt="" width={92} />}
            <div className="grow">
              <div className="label">Poster</div>
              <div className="actions">
                <input
                  type="url"
                  value={posterUrl}
                  onChange={(e) => setPosterUrl(e.target.value)}
                  placeholder="Image or ThePosterDB link"
                  spellCheck={false}
                />
                <button type="button" className="btn btn-glass btn-sm" disabled={!posterUrl.trim() || posterBusy} onClick={() => changePoster({ url: posterUrl })}>
                  Use link
                </button>
                <label className="btn btn-glass btn-sm">
                  Upload
                  <input
                    type="file"
                    accept="image/*"
                    hidden
                    disabled={posterBusy}
                    onChange={(e) => e.target.files?.[0] && changePoster(e.target.files[0])}
                  />
                </label>
                {poster && collection.poster && (
                  <button type="button" className="btn btn-glass btn-sm" disabled={posterBusy} onClick={() => changePoster({ url: '' })}>
                    Reset
                  </button>
                )}
              </div>
              <TPDbSearch endpoint={`/api/admin/collections/${collection.id}/tpdb`} title={collection.name} busy={posterBusy} onPick={(url) => changePoster({ url })} />
            </div>
          </div>
        )}
        <div className="field">
          Labels
          <ChipsInput value={labels} onChange={setLabels} suggestions={usedLabels ?? []} placeholder="Add a label" />
          <span className="muted small">Tags for the collection itself, to keep collections organised. They don't change which titles are in it.</span>
        </div>
        <label>
          Follow lists
          <textarea
            rows={Math.min(6, Math.max(2, listUrl.split('\n').length + 1))}
            value={listUrl}
            onChange={(e) => setListUrl(e.target.value)}
            placeholder={'https://mdblist.com/lists/…\nhttps://trakt.tv/movies/trending?limit=40'}
            spellCheck={false}
            className="mono"
          />
          <select value="" onChange={(e) => addChart(e.target.value)}>
            <option value="">Add a chart…</option>
            {(['TMDB', 'Trakt', 'IMDb'] as const).map((source) => (
              <optgroup key={source} label={source}>
                {chartsFor(libraryKind)
                  .filter((c) => c.source === source)
                  .map((c) => (
                    <option key={c.url} value={c.url}>
                      {c.name}
                    </option>
                  ))}
              </optgroup>
            ))}
          </select>
          <span className="muted small">
            One link per line: an MDBList list, a TMDB, Trakt or IMDb chart or list, a Yamtrack list, or an RSS feed (Letterboxd lists have one).
            Add <code>?limit=40</code> to keep a chart's top 40. With several lists their titles are taken in turn. Titles in this library join the
            collection, and the “Sync list collections” task keeps them up to date. Keys and sign-ins go in Settings → Metadata.
          </span>
          {collection?.listUrl && collection.listUrl === listUrl && (
            <span className={collection.listError ? 'form-error' : 'muted small'}>
              {collection.listError
                ? `Last sync failed: ${collection.listError}`
                : collection.listSyncedAt
                  ? `Synced ${new Date(collection.listSyncedAt * 1000).toLocaleString()}`
                  : 'Not synced yet'}
            </span>
          )}
        </label>
        {collection && (collection.tmdbId || collection.listUrl) ? <CinemaskSwitches collectionId={collection.id} /> : null}
        <div className="label">Smart rules: include everything that matches all of these</div>
        <div className="field-grid">
          <label>
            Genre
            <input list="bb-genres" value={genreRule} onChange={(e) => setGenreRule(e.target.value)} placeholder="Any" />
          </label>
          <label>
            Studio or network
            <input list="bb-studios" value={studioRule} onChange={(e) => setStudioRule(e.target.value)} placeholder="Any" />
          </label>
          <label>
            From year
            <input type="number" min={1870} max={2200} value={yearFrom} onChange={(e) => setYearFrom(e.target.value)} placeholder="Any" />
          </label>
          <label>
            To year
            <input type="number" min={1870} max={2200} value={yearTo} onChange={(e) => setYearTo(e.target.value)} placeholder="Any" />
          </label>
          <label className="span-2">
            Label
            <input list="bb-labels" value={labelRule} onChange={(e) => setLabelRule(e.target.value)} placeholder="Any" />
          </label>
        </div>
        <datalist id="bb-genres">{facets?.genres.map((g) => <option key={g} value={g} />)}</datalist>
        <datalist id="bb-studios">{facets?.studios.map((g) => <option key={g} value={g} />)}</datalist>
        <datalist id="bb-labels">{facets?.labels.map((l) => <option key={l} value={l} />)}</datalist>
        <p className="muted small">
          Leave every rule empty for a hand-picked collection. Labels come from a movie's or show's Edit → Tags tab. Anything that
          matches joins the collection automatically, alongside anything added by hand.
        </p>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">{collection ? 'Save' : 'Create'}</button>
        </div>
      </form>
    </Modal>
  )
}

interface CinemaskState {
  enabled: boolean
  monitor: boolean
  search: boolean
  lastRun?: number
  error?: string
  missing?: number
  added?: number
}

/** The Cinemask Upgrades plugin's switches for a collection's missing
 * titles. Hidden unless the plugin runs with collections turned on in its
 * settings; each switch saves at once. */
function CinemaskSwitches({ collectionId }: { collectionId: number }) {
  const path = `/api/plugins/cinemask-upgrades/collections/${collectionId}`
  const { data, reload } = useApi<CinemaskState>(path)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (!data?.enabled) return null
  const set = async (monitor: boolean, search: boolean) => {
    setBusy(true)
    setError('')
    try {
      await api.put(path, { monitor, search })
      reload()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="check-list">
      <div className="label">Cinemask</div>
      <label className="check">
        <input type="checkbox" checked={data.monitor} disabled={busy} onChange={(e) => set(e.target.checked, e.target.checked && data.search)} />
        Monitor missing titles in Cinemask
      </label>
      <label className="check">
        <input type="checkbox" checked={data.search} disabled={busy} onChange={(e) => set(data.monitor || e.target.checked, e.target.checked)} />
        Search for them too
      </label>
      <span className="muted small">
        Titles from this collection's TMDB collection or lists that no library has yet are added to Cinemask, now and every six hours.
        {data.lastRun
          ? ` Last checked ${new Date(data.lastRun * 1000).toLocaleString()}: ${data.missing ?? 0} missing, ${data.added ?? 0} handed to Cinemask.`
          : ''}
      </span>
      {(error || data.error) && <span className="form-error">{error || data.error}</span>}
    </div>
  )
}
