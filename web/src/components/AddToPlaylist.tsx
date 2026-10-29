import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { useApi } from '../hooks'
import type { Item, Playlist } from '../types'
import { Icon } from './Icon'

/** "Add to playlist" button with a dropdown of the user's playlists. */
export function AddToPlaylist({ item }: { item: Item }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [note, setNote] = useState('')
  const { data: lists, reload } = useApi<Playlist[]>(open ? '/api/playlists' : null)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  useEffect(() => {
    if (!note) return
    const t = window.setTimeout(() => setNote(''), 2500)
    return () => window.clearTimeout(t)
  }, [note])

  const what = item.kind === 'show' ? 'all episodes' : item.kind === 'season' ? 'the season' : 'it'
  const add = async (p: Playlist) => {
    const res = await api.post<{ added: number }>(`/api/playlists/${p.id}/items`, { itemIds: [item.id] })
    setNote(res.added ? `Added ${what} to ${p.name}` : `Already in ${p.name}`)
    setOpen(false)
  }
  const create = async () => {
    if (!name.trim()) return
    const p = await api.post<Playlist>('/api/playlists', { name, itemIds: [item.id] })
    setNote(`Created ${p.name}`)
    setName('')
    setOpen(false)
    reload()
  }

  return (
    <div className="menu-anchor" ref={ref}>
      <button className="btn btn-glass icon-only" onClick={() => setOpen((o) => !o)} title="Add to playlist">
        <Icon name="plus" size={18} />
      </button>
      {note && <div className="inline-note">{note}</div>}
      {open && (
        <div className="menu dropdown">
          <div className="menu-section">Add to playlist</div>
          {lists?.map((p) => (
            <button key={p.id} onClick={() => add(p)}>
              <Icon name="list" size={16} /> {p.name}
              <span className="muted small push">{p.itemCount}</span>
            </button>
          ))}
          {lists?.length === 0 && <div className="muted small menu-pad">No playlists yet</div>}
          <form
            className="menu-create"
            onSubmit={(e) => {
              e.preventDefault()
              create()
            }}
          >
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New playlist name" />
            <button className="btn btn-primary btn-sm" disabled={!name.trim()}>Create</button>
          </form>
        </div>
      )}
    </div>
  )
}

/** Toggle for adding a movie or show to the watchlist. */
/** Fired on window after a favorite is added or removed, so Favorites and Home reload. */
export const favoritesChanged = 'bb:favorites-changed'

export async function toggleFavorite(item: Item) {
  await api[item.favorite ? 'del' : 'post'](`/api/favorites/${item.id}`)
  window.dispatchEvent(new Event(favoritesChanged))
}

export function FavoriteButton({ item, onChange }: { item: Item; onChange: () => void }) {
  return (
    <button
      className="btn btn-glass icon-only"
      onClick={() => toggleFavorite(item).then(onChange)}
      title={item.favorite ? 'Remove from favorites' : 'Add to favorites'}
    >
      <Icon name="heart" size={18} fill={item.favorite} className={item.favorite ? 'accent' : ''} />
    </button>
  )
}

export function WatchlistButton({ item, onChange }: { item: Item; onChange: () => void }) {
  const toggle = async () => {
    await api[item.onWatchlist ? 'del' : 'post'](`/api/watchlist/${item.id}`)
    onChange()
  }
  return (
    <button className="btn btn-glass icon-only" onClick={toggle} title={item.onWatchlist ? 'Remove from watchlist' : 'Add to watchlist'}>
      <Icon name="bookmark" size={18} fill={item.onWatchlist} className={item.onWatchlist ? 'accent' : ''} />
    </button>
  )
}
