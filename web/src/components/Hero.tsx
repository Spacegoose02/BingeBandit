import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { img } from '../api'
import { formatRuntime } from '../format'
import type { Item } from '../types'
import { Icon } from './Icon'

export function MetaLine({ item }: { item: Item }) {
  const parts = [
    item.year ? String(item.year) : '',
    item.kind === 'movie' ? formatRuntime(item.runtime) : item.childCount ? `${item.childCount} episodes` : '',
    (item.genres ?? []).slice(0, 3).join(', '),
  ].filter(Boolean)
  return (
    <div className="meta-line">
      {parts.join(' · ')}
      {item.contentRating && <span className="rating-pill">{item.contentRating}</span>}
      {item.edition && <span className="rating-pill">{item.edition}</span>}
      {!!item.rating && (
        <span className="score">
          <Icon name="star" fill size={14} /> {item.rating.toFixed(1)}
        </span>
      )}
    </div>
  )
}

/** Rotating spotlight at the top of Home. */
export function Hero({ items }: { items: Item[] }) {
  const [index, setIndex] = useState(0)
  useEffect(() => {
    if (items.length < 2) return
    const t = window.setInterval(() => setIndex((i) => (i + 1) % items.length), 9000)
    return () => window.clearInterval(t)
  }, [items.length])
  const item = items[index % Math.max(items.length, 1)]
  if (!item) return null
  const logo = img(item.logo, 'w500')
  return (
    <section className="hero">
      {items.map((it, i) => (
        <img key={it.id} className={`hero-art ${i === index ? 'active' : ''}`} src={img(it.backdrop, 'w1280')} alt="" />
      ))}
      <div className="hero-shade" />
      <div className="hero-body" key={item.id}>
        {logo ? <img className="hero-logo" src={logo} alt={item.title} /> : <h1 className="hero-title">{item.title}</h1>}
        <MetaLine item={item} />
        {item.overview && <p className="hero-overview">{item.overview}</p>}
        <div className="actions">
          <Link to={item.kind === 'show' ? `/item/${item.id}` : `/play/${item.id}`} className="btn btn-primary">
            <Icon name="play" fill size={18} /> {item.kind === 'show' ? 'Watch' : 'Play'}
          </Link>
          <Link to={`/item/${item.id}`} className="btn btn-glass">
            <Icon name="info" size={18} /> More info
          </Link>
        </div>
      </div>
      {items.length > 1 && (
        <div className="hero-dots">
          {items.map((it, i) => (
            <button key={it.id} className={i === index ? 'active' : ''} onClick={() => setIndex(i)} aria-label={`Show ${it.title}`} />
          ))}
        </div>
      )}
    </section>
  )
}
