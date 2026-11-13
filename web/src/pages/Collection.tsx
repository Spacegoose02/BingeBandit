import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { readBack, writeBack } from '../backState'
import { useAuth } from '../auth'
import { PosterCard } from '../components/Cards'
import { CollectionEditor } from '../components/CollectionEditor'
import { Icon } from '../components/Icon'
import { RequestRow } from '../components/RequestKit'
import { KidsApproveCollectionButton } from '../components/KidsApprove'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import { smartRuleText } from '../format'
import type { Collection, Item } from '../types'

const PAGE = 120

// The orders a viewer can pick for themselves, as the server names them;
// "release" asks for the default oldest-first order (reported back as "").
const viewerSorts: [string, string][] = [
  ['release', 'Release date (oldest first)'],
  ['release_desc', 'Release date (newest first)'],
  ['title', 'Title'],
  ['added', 'Recently added'],
  ['rating', 'Rating'],
  ['list', 'List order'],
]

function readSort(id?: string): string {
  try {
    return localStorage.getItem(`bb.collectionSort.${id}`) ?? ''
  } catch {
    return ''
  }
}

interface CollectionPageData {
  collection: Collection
  items: Item[]
  total: number
  /** The order the titles came in: the viewer's pick, else the collection's own. */
  sort?: string
  /** The same-named collections in other libraries the viewer can see. */
  others?: { collection: Collection; library: string; items: Item[] }[]
}

export default function CollectionPage() {
  const { id } = useParams()
  const { user } = useAuth()
  const nav = useNavigate()
  const { key } = useLocation()
  // The first page comes with the collection; the rest load as you scroll.
  // Back to it loads as many as were showing, so the scroll position is there.
  const first = useMemo(() => Math.min(500, Math.max(PAGE, readBack<number>(key, 'loaded') ?? 0)), [id, key]) // eslint-disable-line react-hooks/exhaustive-deps
  // An order picked here sticks for this collection on this device; "" uses the collection's own.
  const [picked, setPicked] = useState(() => readSort(id))
  useEffect(() => setPicked(readSort(id)), [id])
  const sortQuery = picked ? `&sort=${picked}` : ''
  const pickSort = (v: string) => {
    try {
      if (v) localStorage.setItem(`bb.collectionSort.${id}`, v)
      else localStorage.removeItem(`bb.collectionSort.${id}`)
    } catch {
      // Private windows can refuse storage; the pick still applies until you leave.
    }
    setPicked(v)
  }
  const { data, error, reload, setData } = useApi<CollectionPageData>(`/api/collections/${id}?limit=${first}${sortQuery}`)
  const [more, setMore] = useState<Item[]>([])
  const [ended, setEnded] = useState(false)
  const [editing, setEditing] = useState(false)
  const sentinel = useRef<HTMLDivElement>(null)
  const busy = useRef(false)
  useDocumentTitle(data?.collection.name)
  useEffect(() => {
    setMore([])
    setEnded(false)
  }, [data])

  const items = data ? [...data.items, ...more] : []
  const total = data?.total ?? 0
  useEffect(() => {
    if (items.length) writeBack(key, 'loaded', items.length)
  }, [key, items.length])
  useEffect(() => {
    const el = sentinel.current
    if (!el || !data || ended || items.length >= total) return
    const obs = new IntersectionObserver(async (entries) => {
      if (!entries[0].isIntersecting || busy.current) return
      busy.current = true
      try {
        const page = await api.get<CollectionPageData>(`/api/collections/${id}?offset=${items.length}&limit=${PAGE}${sortQuery}`)
        if (!page.items.length) setEnded(true)
        const have = new Set(items.map((it) => it.id))
        setMore((m) => [...m, ...page.items.filter((it) => !have.has(it.id))])
      } finally {
        busy.current = false
      }
    }, { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [data, ended, id, items, total, sortQuery])

  // A title taken out of (or added to) this collection from its ⋮ menu shows
  // at once: everything loaded so far comes again in one go.
  const refresh = () =>
    api.get<CollectionPageData>(`/api/collections/${id}?limit=${Math.max(PAGE, items.length + 1)}${sortQuery}`).then(setData, () => undefined)

  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  const { collection } = data
  const sort = picked || (data.sort ?? collection.sort) || 'release'
  const sorts = viewerSorts.filter(([v]) => v !== 'list' || !!collection.listUrl || sort === 'list')
  const backdrop = img(collection.backdrop || items.find((i) => i.backdrop)?.backdrop, 'original')

  const remove = async () => {
    if (!confirm(`Delete the collection "${collection.name}"? The movies and shows stay in your library.`)) return
    await api.del(`/api/admin/collections/${collection.id}`)
    nav(`/library/${collection.libraryId}?tab=collections`, { replace: true })
  }

  return (
    <div className="detail">
      {backdrop && <div className="detail-backdrop short" style={{ backgroundImage: `url(${backdrop})` }} />}
      <div className="detail-shade short" />
      <div className="detail-content">
        <div className="page" style={{ paddingTop: 0 }}>
          <header className="page-head">
            <div className="collection-head">
              <div className="muted small">
                <Icon name="stack" size={14} /> Collection{smartRuleText(collection) ? ` · smart: ${smartRuleText(collection)}` : ''}
              </div>
              <h1 className="detail-title">{collection.name}</h1>
              <div className="muted">{total} {total === 1 ? 'item' : 'items'}</div>
              {collection.overview && <p className="overview">{collection.overview}</p>}
            </div>
            <div className="actions">
              {items.length > 1 && (
                <label className="select-label">
                  Sort
                  <select value={sort} onChange={(e) => pickSort(e.target.value)}>
                    {sorts.map(([v, label]) => (
                      <option key={v} value={v}>{label}</option>
                    ))}
                  </select>
                </label>
              )}
              <KidsApproveCollectionButton collection={collection} shows={items[0]?.kind === 'show'} />
              {user?.isAdmin && (
                <>
                  <button className="btn btn-glass btn-sm" onClick={() => setEditing(true)}>
                    <Icon name="edit" size={16} /> Edit
                  </button>
                  <button className="btn btn-glass btn-sm danger" onClick={remove} aria-label="Delete collection">
                    <Icon name="trash" size={16} />
                  </button>
                </>
              )}
            </div>
          </header>
          {items.length === 0 && <div className="empty">Nothing in this collection yet.</div>}
          <div className="poster-grid">
            {items.map((it) => (
              <PosterCard key={it.id} item={it} onMembership={refresh} />
            ))}
          </div>
          <div ref={sentinel} style={{ height: 1 }} />
          {data.others?.map((o) => (
            <Row
              key={o.collection.id}
              title={`${o.collection.name} in ${o.library}`}
              to={`/collection/${o.collection.id}`}
            >
              {o.items.map((it) => (
                <PosterCard key={it.id} item={it} onMembership={refresh} />
              ))}
            </Row>
          ))}
          <RequestRow title="Missing from this collection" path={`/api/requests/collection/${collection.id}`} />
        </div>
      </div>
      {editing && (
        <CollectionEditor
          libraryId={collection.libraryId}
          libraryKind={items[0]?.kind === 'show' ? 'shows' : 'movies'}
          collection={collection}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            // A new saved order should show, not one picked here earlier.
            if (picked) pickSort('')
            else reload()
          }}
        />
      )}
    </div>
  )
}
