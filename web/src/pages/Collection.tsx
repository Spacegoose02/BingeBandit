import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { PosterCard } from '../components/Cards'
import { CollectionEditor } from '../components/CollectionEditor'
import { Icon } from '../components/Icon'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import { smartRuleText } from '../format'
import type { Collection, Item } from '../types'

const PAGE = 120

interface CollectionPageData {
  collection: Collection
  items: Item[]
  total: number
  /** The same-named collections in other libraries the viewer can see. */
  others?: { collection: Collection; library: string; items: Item[] }[]
}

export default function CollectionPage() {
  const { id } = useParams()
  const { user } = useAuth()
  const nav = useNavigate()
  // The first page comes with the collection; the rest load as you scroll.
  const { data, error, reload } = useApi<CollectionPageData>(`/api/collections/${id}?limit=${PAGE}`)
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
    const el = sentinel.current
    if (!el || !data || ended || items.length >= total) return
    const obs = new IntersectionObserver(async (entries) => {
      if (!entries[0].isIntersecting || busy.current) return
      busy.current = true
      try {
        const page = await api.get<CollectionPageData>(`/api/collections/${id}?offset=${items.length}&limit=${PAGE}`)
        if (!page.items.length) setEnded(true)
        const have = new Set(items.map((it) => it.id))
        setMore((m) => [...m, ...page.items.filter((it) => !have.has(it.id))])
      } finally {
        busy.current = false
      }
    }, { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [data, ended, id, items, total])

  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  const { collection } = data
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
            {user?.isAdmin && (
              <div className="actions">
                <button className="btn btn-glass btn-sm" onClick={() => setEditing(true)}>
                  <Icon name="edit" size={16} /> Edit
                </button>
                <button className="btn btn-glass btn-sm danger" onClick={remove} aria-label="Delete collection">
                  <Icon name="trash" size={16} />
                </button>
              </div>
            )}
          </header>
          {items.length === 0 && <div className="empty">Nothing in this collection yet.</div>}
          <div className="poster-grid">
            {items.map((it) => (
              <PosterCard key={it.id} item={it} />
            ))}
          </div>
          <div ref={sentinel} style={{ height: 1 }} />
          {data.others?.map((o) => (
            <Row
              key={o.collection.id}
              title={`${o.collection.name} in ${o.library}`}
              action={<Link to={`/collection/${o.collection.id}`} className="muted small">See all</Link>}
            >
              {o.items.map((it) => (
                <PosterCard key={it.id} item={it} />
              ))}
            </Row>
          ))}
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
            reload()
          }}
        />
      )}
    </div>
  )
}
