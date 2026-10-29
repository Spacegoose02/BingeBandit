import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { img } from '../api'
import { episodeLabel, progressFraction, smartRuleText } from '../format'
import type { Collection, Item } from '../types'
import { CollectionMenu, ItemMenu } from './CardMenu'
import { Icon } from './Icon'
import { useSelectable, type Picked } from './Selection'

/**
 * Card artwork. A poster that fails to load (the server couldn't reach TMDB
 * in time, say) is asked for once more a little later, then shows the title.
 */
function Art({ src, label }: { src?: string; label: string }) {
  const [tries, setTries] = useState(0)
  useEffect(() => setTries(0), [src])
  if (!src || tries > 1) return <div className="poster-fallback">{label}</div>
  return (
    <img
      key={tries}
      src={tries ? `${src}${src.includes('?') ? '&' : '?'}retry=1` : src}
      alt=""
      loading="lazy"
      onError={() => window.setTimeout(() => setTries((t) => t + 1), tries ? 0 : 3000)}
    />
  )
}

function Badge({ item }: { item: Item }) {
  if (item.kind === 'show' || item.kind === 'season') {
    if (item.childCount && !item.unwatchedCount) return <span className="badge-watched"><Icon name="check" size={14} /></span>
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

/**
 * Movie, show or season poster. The ⋮ in its corner (on hover, or always on
 * touch screens) opens the options menu; changes made there show on the card
 * straight away. onMembership runs when the menu may have taken the title out
 * of the page's list (collections, edit, move), so the page can reload.
 */
export function PosterCard({
  item: given,
  subtitle,
  domId,
  onMembership,
}: {
  item: Item
  subtitle?: string
  domId?: string
  onMembership?: () => void
}) {
  const [changed, setChanged] = useState<Item | null>(null)
  const [deleted, setDeleted] = useState(false)
  useEffect(() => setChanged(null), [given])
  const item = changed ?? given
  const poster = img(item.poster || item.showPoster, 'w342')
  const pick = useMemo<Picked>(() => ({ key: `i${item.id}`, kind: 'item', item, reload: onMembership }), [item, onMembership])
  const sel = useSelectable(pick)
  if (deleted) return null
  return (
    <div className={`poster-card has-menu${sel?.selected ? ' selected' : ''}`} id={domId} {...sel?.cardProps}>
      <Link to={`/item/${item.id}`} className="poster-link">
        <div className="poster-frame">
          <Art src={poster} label={item.title} />
          <Badge item={item} />
          <Progress item={item} />
          <div className="poster-hover">
            <Icon name="play" fill size={34} />
          </div>
        </div>
        <div className="card-title" title={item.title}>{item.title}</div>
        <div className="card-sub">{subtitle ??
            (item.kind === 'season'
              ? `${item.childCount ?? 0} episodes`
              : item.kind === 'episode'
                ? [item.showTitle, episodeLabel(item)].filter(Boolean).join(' · ')
                : item.year || '')}</div>
      </Link>
      <ItemMenu item={item} onChanged={(it) => setChanged({ ...item, ...it })} onMembership={onMembership} onDeleted={() => setDeleted(true)} />
      {sel?.circle}
    </div>
  )
}

/** Landscape card for Continue Watching and episode lists. Clicking plays. */
export function WideCard({ item }: { item: Item }) {
  const art = img(item.backdrop || item.showBackdrop, 'w780')
  const isEp = item.kind === 'episode'
  return (
    <div className="wide-card">
      <Link to={`/play/${item.id}${item.watch.position > 15 ? `?t=${Math.floor(item.watch.position)}` : ''}`} className="wide-frame">
        <Art src={art} label={item.title} />
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

export function CollectionCard({ collection, libraryKind, onChanged }: { collection: Collection; libraryKind?: string; onChanged?: () => void }) {
  const poster = img(collection.poster, 'w342')
  const pick = useMemo<Picked>(() => ({ key: `c${collection.id}`, kind: 'collection', collection, reload: onChanged }), [collection, onChanged])
  const sel = useSelectable(pick)
  return (
    <div className={`poster-card has-menu${sel?.selected ? ' selected' : ''}`} {...sel?.cardProps}>
      <Link to={`/collection/${collection.id}`} className="poster-link">
        <div className="poster-frame collection-frame">
          <Art src={poster} label={collection.name} />
          <span className="badge-count">{collection.itemCount}</span>
          {smartRuleText(collection) && (
            <span className="badge-smart" title={`Smart: ${smartRuleText(collection)}`}>
              <Icon name="stack" size={12} /> Smart
            </span>
          )}
        </div>
        <div className="card-title" title={collection.name}>{collection.name}</div>
        <div className="card-sub">{collection.itemCount} {collection.itemCount === 1 ? 'item' : 'items'}</div>
      </Link>
      <CollectionMenu collection={collection} libraryKind={libraryKind} onChanged={onChanged} />
      {sel?.circle}
    </div>
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
