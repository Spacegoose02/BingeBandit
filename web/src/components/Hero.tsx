import { useEffect, useRef, useState } from 'react'
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

/** Rotating spotlight at the top of Home. A title with a YouTube trailer
 *  plays it, muted, after a few seconds on screen (Netflix style); the
 *  spotlight moves on when it ends. Not for people who ask for less motion. */
export function Hero({ items }: { items: Item[] }) {
  const [index, setIndex] = useState(0)
  const [trailer, setTrailer] = useState<string | null>(null) // the item's trailer playing now
  const [muted, setMuted] = useState(true)
  const frame = useRef<HTMLIFrameElement>(null)
  const item = items[index % Math.max(items.length, 1)]

  // Rotate, except while a trailer plays.
  useEffect(() => {
    if (items.length < 2 || trailer) return
    const t = window.setInterval(() => setIndex((i) => (i + 1) % items.length), 9000)
    return () => window.clearInterval(t)
  }, [items.length, trailer])

  // Start the trailer after the art has had a moment.
  useEffect(() => {
    setTrailer(null)
    if (!item?.trailer || !trailersWanted()) return
    const t = window.setTimeout(() => setTrailer(item.trailer!), 3500)
    return () => window.clearTimeout(t)
  }, [item?.id, item?.trailer])

  // YouTube says when the trailer ends (state 0): back to the art and on to the next.
  useEffect(() => {
    if (!trailer) return
    const onMsg = (e: MessageEvent) => {
      if (!/youtube(-nocookie)?\.com$/.test(new URL(e.origin).hostname)) return
      try {
        const d = typeof e.data === 'string' ? JSON.parse(e.data) : e.data
        if (d?.event === 'infoDelivery' && d.info?.playerState === 0) {
          setTrailer(null)
          if (items.length > 1) setIndex((i) => (i + 1) % items.length)
        }
      } catch {
        /* not one of YouTube's */
      }
    }
    window.addEventListener('message', onMsg)
    // A trailer that never reports (blocked, region-locked) gives way after two minutes.
    const giveUp = window.setTimeout(() => setTrailer(null), 120000)
    return () => {
      window.removeEventListener('message', onMsg)
      window.clearTimeout(giveUp)
    }
  }, [trailer, items.length])

  const command = (func: string) => frame.current?.contentWindow?.postMessage(JSON.stringify({ event: 'command', func, args: [] }), '*')
  const toggleMute = () => {
    command(muted ? 'unMute' : 'mute')
    setMuted(!muted)
  }

  if (!item) return null
  const logo = img(item.logo, 'w500')
  return (
    <section className="hero">
      {items.map((it, i) => (
        <img key={it.id} className={`hero-art ${i === index ? 'active' : ''}`} src={img(it.backdrop, 'w1280')} alt="" />
      ))}
      {trailer && (
        <iframe
          ref={frame}
          key={trailer}
          className="hero-trailer"
          src={`https://www.youtube-nocookie.com/embed/${trailer}?autoplay=1&mute=1&controls=0&playsinline=1&rel=0&modestbranding=1&iv_load_policy=3&enablejsapi=1&origin=${encodeURIComponent(window.location.origin)}`}
          allow="autoplay; encrypted-media"
          title=""
          tabIndex={-1}
          onLoad={() => {
            frame.current?.contentWindow?.postMessage(JSON.stringify({ event: 'listening', id: 'hero' }), '*')
            setMuted(true)
          }}
        />
      )}
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
          {trailer && (
            <button className="btn btn-glass icon-only" onClick={toggleMute} title={muted ? 'Sound on' : 'Sound off'}>
              <Icon name={muted ? 'mute' : 'volume'} size={18} />
            </button>
          )}
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

/** Trailers on Home, unless this browser asked for less motion or turned them off. */
export function trailersWanted(): boolean {
  if (typeof window === 'undefined' || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return false
  try {
    return localStorage.getItem('bb.heroTrailers') !== 'off'
  } catch {
    return true
  }
}
