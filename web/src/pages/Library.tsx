import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { CollectionCard, PosterCard } from '../components/Cards'
import { CollectionEditor } from '../components/CollectionEditor'
import { Icon } from '../components/Icon'
import { RecommendationsEditor } from '../components/RecommendationsEditor'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import type { Collection, HubRow, Item, Library } from '../types'

const PAGE = 120
const sorts = [
  ['title', 'Title'],
  ['added', 'Date added'],
  ['release', 'Release date'],
  ['year', 'Year'],
  ['rating', 'Rating'],
] as const

interface Page {
  library: Library
  items: Item[]
  total: number
}

// Keyed by id so switching libraries resets paging and restores that library's sort.
export default function LibraryPage() {
  const { id } = useParams()
  return <LibraryView key={id} id={id!} />
}

function LibraryView({ id }: { id: string }) {
  const [params, setParams] = useSearchParams()
  // Recommended is the library's front page (Plex's default tab).
  const tab = params.get('tab') === 'collections' ? 'collections' : params.get('tab') === 'library' ? 'library' : 'recommended'
  const [sort, setSort] = useState(() => localStorage.getItem(`bb.sort.${id}`) ?? 'title')
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const sentinel = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(false)
  useDocumentTitle(library?.name)

  const load = useCallback(
    async (offset: number) => {
      if (loadingRef.current) return
      loadingRef.current = true
      setLoading(true)
      try {
        const page = await api.get<Page>(`/api/libraries/${id}/items?sort=${sort}&offset=${offset}&limit=${PAGE}`)
        setLibrary(page.library)
        setTotal(page.total)
        setItems((prev) => (offset === 0 ? page.items : [...prev, ...page.items]))
      } finally {
        loadingRef.current = false
        setLoading(false)
      }
    },
    [id, sort],
  )

  useEffect(() => {
    setItems([])
    load(0)
  }, [load])

  useEffect(() => {
    const el = sentinel.current
    if (!el) return
    const obs = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && items.length < total) load(items.length)
    }, { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [items.length, total, load, tab])

  const changeSort = (s: string) => {
    localStorage.setItem(`bb.sort.${id}`, s)
    setSort(s)
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{library?.name ?? ' '}</h1>
          <div className="muted">
            {total} {library?.kind === 'shows' ? (total === 1 ? 'show' : 'shows') : total === 1 ? 'movie' : 'movies'}
          </div>
        </div>
        <div className="segmented">
          <button className={tab === 'recommended' ? 'active' : ''} onClick={() => setParams({}, { replace: true })}>Recommended</button>
          <button className={tab === 'library' ? 'active' : ''} onClick={() => setParams({ tab: 'library' }, { replace: true })}>Library</button>
          <button className={tab === 'collections' ? 'active' : ''} onClick={() => setParams({ tab: 'collections' }, { replace: true })}>
            Collections
          </button>
        </div>
        {tab === 'library' && (
          <label className="select-label">
            Sort
            <select value={sort} onChange={(e) => changeSort(e.target.value)}>
              {sorts.map(([v, label]) => (
                <option key={v} value={v}>{label}</option>
              ))}
            </select>
          </label>
        )}
      </header>
      {tab === 'recommended' && library && <RecommendedTab library={library} />}
      {tab === 'collections' && <CollectionsTab libraryId={Number(id)} />}
      {tab === 'library' && (
        <>
          {!loading && items.length === 0 && <div className="empty">This library is empty. Scans can take a few minutes for big folders.</div>}
          <div className="poster-grid">
            {items.map((it) => (
              <PosterCard key={it.id} item={it} />
            ))}
          </div>
          <div ref={sentinel} style={{ height: 1 }} />
        </>
      )}
    </div>
  )
}

function CollectionsTab({ libraryId }: { libraryId: number }) {
  const { user } = useAuth()
  const { data, reload } = useApi<Collection[]>(`/api/libraries/${libraryId}/collections`)
  const [creating, setCreating] = useState(false)
  return (
    <>
      {user?.isAdmin && (
        <div className="actions" style={{ marginBottom: 20 }}>
          <button className="btn btn-glass btn-sm" onClick={() => setCreating(true)}>
            <Icon name="plus" size={16} /> New collection
          </button>
          <span className="muted small">Add movies from their Edit dialog, or give a collection rules (genre, studio, years or a label) to fill it automatically.</span>
        </div>
      )}
      {data?.length === 0 && <div className="empty">No collections yet. TMDB collections appear here once metadata is fetched.</div>}
      <div className="poster-grid">
        {data?.map((c) => (
          <CollectionCard key={c.id} collection={c} onChanged={reload} />
        ))}
      </div>
      {creating && (
        <CollectionEditor
          libraryId={libraryId}
          onClose={() => setCreating(false)}
          onSaved={() => {
            setCreating(false)
            reload()
          }}
        />
      )}
    </>
  )
}

/** The library's front page: its rows, in the order an admin set. */
function RecommendedTab({ library }: { library: Library }) {
  const { user } = useAuth()
  const { data, loading, reload } = useApi<HubRow[]>(`/api/libraries/${library.id}/hubs`)
  const [managing, setManaging] = useState(false)
  return (
    <>
      {user?.isAdmin && (
        <div className="actions" style={{ marginBottom: 20 }}>
          <button className="btn btn-glass btn-sm" onClick={() => setManaging(true)}>
            <Icon name="sliders" size={16} /> Manage recommendations
          </button>
          <span className="muted small">Pick which rows and collections show here, on Home and on Friends' Home.</span>
        </div>
      )}
      {loading && <div className="page-loading" />}
      {data?.length === 0 && <div className="empty">Nothing to recommend yet. The Library tab has everything.</div>}
      <div className="library-hubs">
        {data?.map((row) => (
          <Row
            key={row.id}
            title={row.title}
            action={row.collectionId ? <Link to={`/collection/${row.collectionId}`} className="muted small">See all</Link> : undefined}
          >
            {row.items.map((it) => (
              <PosterCard key={it.id} item={it} />
            ))}
          </Row>
        ))}
      </div>
      {managing && <RecommendationsEditor library={library} onClose={() => setManaging(false)} onSaved={reload} />}
    </>
  )
}
