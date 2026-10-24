import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { CollectionCard, PosterCard } from '../components/Cards'
import { CollectionEditor } from '../components/CollectionEditor'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { Collection, Item, Library } from '../types'

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
  const tab = params.get('tab') === 'collections' ? 'collections' : 'library'
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
  }, [items.length, total, load])

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
          <button className={tab === 'library' ? 'active' : ''} onClick={() => setParams({}, { replace: true })}>Library</button>
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
      {tab === 'collections' ? (
        <CollectionsTab libraryId={Number(id)} />
      ) : (
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
          <span className="muted small">Add movies from their Edit dialog, or give a collection a label to fill it automatically.</span>
        </div>
      )}
      {data?.length === 0 && <div className="empty">No collections yet. TMDB collections appear here once metadata is fetched.</div>}
      <div className="poster-grid">
        {data?.map((c) => (
          <CollectionCard key={c.id} collection={c} />
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
