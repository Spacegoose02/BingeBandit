import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { PosterCard } from '../components/Cards'
import { CollectionEditor } from '../components/CollectionEditor'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { smartRuleText } from '../format'
import type { Collection, Item } from '../types'

export default function CollectionPage() {
  const { id } = useParams()
  const { user } = useAuth()
  const nav = useNavigate()
  const { data, error, reload } = useApi<{ collection: Collection; items: Item[] }>(`/api/collections/${id}`)
  const [editing, setEditing] = useState(false)
  useDocumentTitle(data?.collection.name)

  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  const { collection, items } = data
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
              <div className="muted">{items.length} {items.length === 1 ? 'item' : 'items'}</div>
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
        </div>
      </div>
      {editing && (
        <CollectionEditor
          libraryId={collection.libraryId}
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
