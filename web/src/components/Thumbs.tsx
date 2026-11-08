import { useState } from 'react'
import { api } from '../api'
import type { Item } from '../types'
import { Icon } from './Icon'

/** Thumbs for a movie or show, Netflix style: not for me, I like this, love
 *  this. A thumbs down keeps the title out of the viewer's recommendations;
 *  thumbs up shape the "Because you liked" row on Home. Pressing the lit
 *  button again clears it. */
export function ThumbsButtons({ item, rating, onChange }: { item: Item; rating: number; onChange?: (r: number) => void }) {
  const [now, setNow] = useState(rating)
  const rate = async (r: number) => {
    const next = now === r ? 0 : r
    setNow(next)
    try {
      await api.put(`/api/items/${item.id}/rating`, { rating: next })
      onChange?.(next)
    } catch (e) {
      setNow(now)
      alert((e as Error).message)
    }
  }
  return (
    <>
      <button className="btn btn-glass icon-only" onClick={() => rate(-1)} title={now === -1 ? 'Not for me (press to undo)' : 'Not for me'}>
        <Icon name="thumbDown" size={18} fill={now === -1} className={now === -1 ? 'accent' : ''} />
      </button>
      <button className="btn btn-glass icon-only" onClick={() => rate(1)} title={now === 1 ? 'I like this (press to undo)' : 'I like this'}>
        <Icon name="thumbUp" size={18} fill={now === 1} className={now === 1 ? 'accent' : ''} />
      </button>
      <button className="btn btn-glass icon-only thumbs-love" onClick={() => rate(2)} title={now === 2 ? 'Love this (press to undo)' : 'Love this'}>
        <Icon name="thumbUp" size={18} fill={now === 2} className={now === 2 ? 'accent' : ''} />
        <Icon name="thumbUp" size={18} fill={now === 2} className={now === 2 ? 'accent' : ''} />
      </button>
    </>
  )
}
