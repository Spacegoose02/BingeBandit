import { useRef, type ReactNode } from 'react'
import { Icon } from './Icon'

/** Horizontally scrolling shelf with arrow buttons on hover. */
export function Row({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  const scroller = useRef<HTMLDivElement>(null)
  const scroll = (dir: number) => {
    const el = scroller.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: 'smooth' })
  }
  return (
    <section className="row">
      <div className="row-head">
        <h2>{title}</h2>
        {action}
      </div>
      <div className="row-body">
        <button className="row-arrow left" onClick={() => scroll(-1)} aria-label="Scroll left">
          <Icon name="back" size={28} />
        </button>
        <div className="row-scroller" ref={scroller}>
          {children}
        </div>
        <button className="row-arrow right" onClick={() => scroll(1)} aria-label="Scroll right">
          <Icon name="next" size={28} />
        </button>
      </div>
    </section>
  )
}
