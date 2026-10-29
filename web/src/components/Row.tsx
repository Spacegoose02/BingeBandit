import { Children, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useBackScrollLeft } from '../backState'
import { Icon } from './Icon'

/** Horizontally scrolling shelf with arrow buttons on hover. With `to`, the
 *  heading links there, a "See all" link sits on the right and a blank
 *  "See all" poster ends the row. */
export function Row({ title, children, action, to }: { title: string; children: ReactNode; action?: ReactNode; to?: string }) {
  const scroller = useRef<HTMLDivElement>(null)
  useBackScrollLeft(`row:${title}`, scroller, Children.count(children) > 0)
  const scroll = (dir: number) => {
    const el = scroller.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: 'smooth' })
  }
  return (
    <section className="row">
      <div className="row-head">
        <h2>{to ? <Link to={to} className="row-title-link">{title}</Link> : title}</h2>
        {action ?? (to && <Link to={to} className="muted small">See all</Link>)}
      </div>
      <div className="row-body">
        <button className="row-arrow left" onClick={() => scroll(-1)} aria-label="Scroll left">
          <Icon name="back" size={28} />
        </button>
        <div className="row-scroller" ref={scroller}>
          {children}
          {to && Children.count(children) > 0 && (
            <div className="poster-card see-all-card">
              <Link to={to} className="poster-link" aria-label={`See all of ${title}`}>
                <div className="poster-frame see-all-frame">
                  <span>See all</span>
                </div>
              </Link>
            </div>
          )}
        </div>
        <button className="row-arrow right" onClick={() => scroll(1)} aria-label="Scroll right">
          <Icon name="next" size={28} />
        </button>
      </div>
    </section>
  )
}
