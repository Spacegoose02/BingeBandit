import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, img } from '../api'
import { episodeLabel, progressFraction, smartRuleText } from '../format'
import type { Collection, Item, Visibility } from '../types'
import { Icon } from './Icon'

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
        <Art src={poster} label={item.title} />
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

export function CollectionCard({ collection, onChanged }: { collection: Collection; onChanged?: () => void }) {
  const poster = img(collection.poster, 'w342')
  const card = (
    <Link to={`/collection/${collection.id}`} className="poster-card">
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
  )
  // Admins get a "Visible on" menu, like Plex's collection menu.
  if (!collection.visible) return card
  return (
    <div className="collection-card">
      {card}
      <VisibleOnMenu collection={collection} onChanged={onChanged} />
    </div>
  )
}

const places: [keyof Visibility, string][] = [
  ['library', 'Library'],
  ['home', 'Home'],
  ['friends', "Friends' Home"],
]

function VisibleOnMenu({ collection, onChanged }: { collection: Collection; onChanged?: () => void }) {
  const [open, setOpen] = useState(false)
  const [visible, setVisible] = useState<Visibility>(collection.visible!)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])
  const toggle = async (place: keyof Visibility) => {
    const next = { ...visible, [place]: !visible[place] }
    setVisible(next)
    try {
      await api.put(`/api/admin/collections/${collection.id}/visibility`, next)
      onChanged?.()
    } catch {
      setVisible(visible)
    }
  }
  const shown = places.filter(([p]) => visible[p]).map(([, label]) => label)
  return (
    <div className="visible-on" ref={ref}>
      <button
        className="visible-on-btn"
        aria-label={`Where ${collection.name} shows`}
        aria-expanded={open}
        title={shown.length ? `Visible on ${shown.join(', ')}` : 'Not shown as a row'}
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="more" size={18} />
      </button>
      {open && (
        <div className="menu visible-on-menu" role="menu">
          <div className="menu-section">Visible on</div>
          {places.map(([p, label]) => (
            <button key={p} role="menuitemcheckbox" aria-checked={visible[p]} className={visible[p] ? 'active' : ''} onClick={() => toggle(p)}>
              {label}
            </button>
          ))}
        </div>
      )}
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
