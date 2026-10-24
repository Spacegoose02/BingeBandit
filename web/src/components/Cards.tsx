import { Link } from 'react-router-dom'
import { img } from '../api'
import { episodeLabel, progressFraction } from '../format'
import type { Collection, Item } from '../types'
import { Icon } from './Icon'

function Badge({ item }: { item: Item }) {
  if (item.kind === 'show' || item.kind === 'season') {
    if (item.childCount && item.unwatchedCount === 0) return <span className="badge-watched"><Icon name="check" size={14} /></span>
    if (item.unwatchedCount) return <span className="badge-count">{item.unwatchedCount}</span>
    return null
  }
  if (item.watch.watched) return <span className="badge-watched"><Icon name="check" size={14} /></span>
  return null
}

function Progress({ item }: { item: Item }) {
  const f = progressFraction(item)
  if (!f) return null
  return (
    <div className="progress">
      <div style={{ width: `${f * 100}%` }} />
    </div>
  )
}

export function PosterCard({ item, subtitle }: { item: Item; subtitle?: string }) {
  const poster = img(item.poster || item.showPoster, 'w342')
  return (
    <Link to={`/item/${item.id}`} className="poster-card">
      <div className="poster-frame">
        {poster ? <img src={poster} alt="" loading="lazy" /> : <div className="poster-fallback">{item.title}</div>}
        <Badge item={item} />
        <Progress item={item} />
        <div className="poster-hover">
          <Icon name="play" fill size={34} />
        </div>
      </div>
      <div className="card-title" title={item.title}>{item.title}</div>
      <div className="card-sub">{subtitle ?? (item.kind === 'season' ? `${item.childCount ?? 0} episodes` : item.year || '')}</div>
    </Link>
  )
}

/** Landscape card for Continue Watching and episode lists. Clicking plays. */
export function WideCard({ item }: { item: Item }) {
  const art = img(item.backdrop || item.showBackdrop, 'w780')
  const isEp = item.kind === 'episode'
  return (
    <div className="wide-card">
      <Link to={`/play/${item.id}${item.watch.position > 15 ? `?t=${Math.floor(item.watch.position)}` : ''}`} className="wide-frame">
        {art ? <img src={art} alt="" loading="lazy" /> : <div className="poster-fallback">{item.title}</div>}
        <Progress item={item} />
        <div className="poster-hover">
          <Icon name="play" fill size={34} />
        </div>
      </Link>
      <Link to={`/item/${isEp ? item.showId : item.id}`} className="card-title">
        {isEp ? item.showTitle : item.title}
      </Link>
      <div className="card-sub">{isEp ? `${episodeLabel(item)} · ${item.title}` : item.year || ''}</div>
    </div>
  )
}

export function CollectionCard({ collection }: { collection: Collection }) {
  const poster = img(collection.poster, 'w342')
  return (
    <Link to={`/collection/${collection.id}`} className="poster-card">
      <div className="poster-frame collection-frame">
        {poster ? <img src={poster} alt="" loading="lazy" /> : <div className="poster-fallback">{collection.name}</div>}
        <span className="badge-count">{collection.itemCount}</span>
        {collection.labelRule && (
          <span className="badge-smart" title={`Everything labelled “${collection.labelRule}”`}>
            <Icon name="stack" size={12} /> Smart
          </span>
        )}
      </div>
      <div className="card-title" title={collection.name}>{collection.name}</div>
      <div className="card-sub">{collection.itemCount} {collection.itemCount === 1 ? 'item' : 'items'}</div>
    </Link>
  )
}

export function PersonCard({ name, role, photo }: { name: string; role: string; photo?: string }) {
  const src = img(photo, 'w185')
  return (
    <div className="person-card">
      <div className="person-photo">{src ? <img src={src} alt="" loading="lazy" /> : <Icon name="user" size={36} />}</div>
      <div className="card-title" title={name}>{name}</div>
      <div className="card-sub" title={role}>{role}</div>
    </div>
  )
}
