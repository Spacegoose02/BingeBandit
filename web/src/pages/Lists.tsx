import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { PosterCard } from '../components/Cards'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useApi, useDocumentTitle } from '../hooks'
import { episodeLabel, formatRuntime } from '../format'
import type { Item, Playlist } from '../types'

export function WatchlistPage() {
  useDocumentTitle('Watchlist')
  const { data } = useApi<Item[]>('/api/watchlist')
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Watchlist</h1>
          <div className="muted">Movies and shows you want to watch. Finished movies leave the list on their own.</div>
        </div>
      </header>
      {data?.length === 0 && (
        <div className="empty">
          Nothing saved yet. Use the <Icon name="bookmark" size={14} /> button on any movie or show.
        </div>
      )}
      <div className="poster-grid">
        {data?.map((it) => (
          <PosterCard key={it.id} item={it} />
        ))}
      </div>
    </div>
  )
}

export function PlaylistsPage() {
  useDocumentTitle('Playlists')
  const { data, reload } = useApi<Playlist[]>('/api/playlists')
  const { data: shared } = useApi<Playlist[]>('/api/playlists/shared')
  const [name, setName] = useState('')
  const create = async () => {
    if (!name.trim()) return
    await api.post('/api/playlists', { name })
    setName('')
    reload()
  }
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Playlists</h1>
          <div className="muted">Line up movies and episodes to watch back to back.</div>
        </div>
        <form
          className="actions"
          onSubmit={(e) => {
            e.preventDefault()
            create()
          }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New playlist name" style={{ width: 220 }} />
          <button className="btn btn-primary" disabled={!name.trim()}>
            <Icon name="plus" size={18} /> Create
          </button>
        </form>
      </header>
      {data?.length === 0 && <div className="empty">No playlists yet. Create one here, or use the + button on any movie, show or episode.</div>}
      <PlaylistGrid lists={data} />
      {!!shared?.length && (
        <>
          <h2 className="section-title">Shared with you</h2>
          <PlaylistGrid lists={shared} />
        </>
      )}
    </div>
  )
}

function PlaylistGrid({ lists }: { lists?: Playlist[] | null }) {
  return (
    <div className="poster-grid">
      {lists?.map((p) => (
        <Link key={p.id} to={`/playlist/${p.id}`} className="poster-card">
          <div className="poster-frame collection-frame">
            {p.poster ? <img src={img(p.poster, 'w342')} alt="" loading="lazy" /> : <div className="poster-fallback"><Icon name="list" size={36} /></div>}
            <span className="badge-count">{p.itemCount}</span>
          </div>
          <div className="card-title">{p.name}</div>
          <div className="card-sub">
            {p.owner ? `From ${p.owner} · ` : p.shares ? 'Shared · ' : ''}
            {p.itemCount} items · {formatRuntime(Math.round(p.duration / 60))}
          </div>
        </Link>
      ))}
    </div>
  )
}

interface Person {
  id: number
  name: string
}

/** Pick who can see and play a playlist. */
function ShareDialog({ playlist, onClose }: { playlist: Playlist; onClose: (changed: boolean) => void }) {
  const { data } = useApi<{ people: Person[]; sharedWith: number[] }>(`/api/playlists/${playlist.id}/share`)
  const [picked, setPicked] = useState<number[] | null>(null)
  const [error, setError] = useState('')
  const chosen = picked ?? data?.sharedWith ?? []
  const toggle = (id: number) => setPicked(chosen.includes(id) ? chosen.filter((x) => x !== id) : [...chosen, id])
  const save = async () => {
    setError('')
    try {
      await api.put(`/api/playlists/${playlist.id}/share`, { userIds: chosen })
      onClose(true)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Modal title={`Share ${playlist.name}`} onClose={() => onClose(false)}>
      <div className="form">
        <p className="muted small">They can watch it and see your changes, but not change it.</p>
        {data && data.people.length === 0 && (
          <p className="muted">
            Nobody to share with yet. You can share with your profiles, and with people who turn on recommendations under{' '}
            <Link to="/account">Account → Sharing</Link>.
          </p>
        )}
        {data && data.people.length > 0 && (
          <div className="check-list">
            {data.people.map((p) => (
              <label key={p.id} className="check">
                <input type="checkbox" checked={chosen.includes(p.id)} onChange={() => toggle(p.id)} />
                {p.name}
              </label>
            ))}
          </div>
        )}
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary" onClick={save} disabled={!data}>
            Save
          </button>
          <button className="btn btn-glass" onClick={() => onClose(false)}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}

export function PlaylistPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const { data, error, reload, setData } = useApi<{ playlist: Playlist; items: Item[] }>(`/api/playlists/${id}`)
  const [sharing, setSharing] = useState(false)
  useDocumentTitle(data?.playlist.name)
  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  const { playlist, items } = data
  // A playlist someone shared with you plays, but only its owner changes it.
  const mine = !playlist.owner

  const move = async (index: number, dir: -1 | 1) => {
    const order = items.map((i) => i.id)
    const j = index + dir
    if (j < 0 || j >= order.length) return
    ;[order[index], order[j]] = [order[j], order[index]]
    setData({ playlist, items: order.map((oid) => items.find((i) => i.id === oid)!) })
    await api.put(`/api/playlists/${playlist.id}/order`, { itemIds: order })
  }
  const remove = async (itemId: number) => {
    await api.del(`/api/playlists/${playlist.id}/items/${itemId}`)
    reload()
  }
  const rename = async () => {
    const name = prompt('Rename playlist', playlist.name)
    if (!name?.trim()) return
    await api.put(`/api/playlists/${playlist.id}`, { name })
    reload()
  }
  const destroy = async () => {
    if (!confirm(`Delete the playlist "${playlist.name}"?`)) return
    await api.del(`/api/playlists/${playlist.id}`)
    nav('/playlists', { replace: true })
  }
  const playFrom = (it: Item) => nav(`/play/${it.id}?playlist=${playlist.id}`)

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <div className="muted small">Playlist</div>
          <h1>{playlist.name}</h1>
          <div className="muted">
            {playlist.owner && `From ${playlist.owner} · `}
            {items.length} items · {formatRuntime(Math.round(playlist.duration / 60))}
          </div>
        </div>
        <div className="actions">
          <button className="btn btn-primary" disabled={!items.length} onClick={() => playFrom(items[0])}>
            <Icon name="play" fill size={18} /> Play all
          </button>
          {mine && (
            <>
              <button className="btn btn-glass btn-sm" onClick={() => setSharing(true)}>
                <Icon name="users" size={16} /> {playlist.shares ? `Shared with ${playlist.shares}` : 'Share'}
              </button>
              <button className="btn btn-glass btn-sm" onClick={rename}><Icon name="edit" size={16} /> Rename</button>
              <button className="btn btn-glass btn-sm danger" onClick={destroy} aria-label="Delete playlist"><Icon name="trash" size={16} /></button>
            </>
          )}
        </div>
      </header>
      {sharing && (
        <ShareDialog
          playlist={playlist}
          onClose={(changed) => {
            setSharing(false)
            if (changed) reload()
          }}
        />
      )}
      {items.length === 0 && <div className="empty">This playlist is empty. Add things with the + button on any item page.</div>}
      <div className="panel list">
        {items.map((it, i) => {
          const art = img(it.kind === 'episode' ? it.backdrop || it.showBackdrop : it.poster, 'w342')
          return (
            <div key={it.id} className="list-row playlist-row">
              <span className="muted pl-num">{i + 1}</span>
              <button className={`pl-art ${it.kind === 'episode' ? 'wide' : ''}`} onClick={() => playFrom(it)} aria-label={`Play ${it.title}`}>
                {art ? <img src={art} alt="" loading="lazy" /> : <Icon name="film" />}
                <span className="pl-play"><Icon name="play" fill size={20} /></span>
              </button>
              <div className="grow">
                <Link to={`/item/${it.id}`}>{it.kind === 'episode' ? it.showTitle : it.title}</Link>
                <div className="muted small">
                  {it.kind === 'episode' ? `${episodeLabel(it)} · ${it.title}` : it.year}
                  {it.watch.watched && ' · watched'}
                </div>
              </div>
              {mine && (
                <>
                  <button className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label="Move up"><Icon name="arrowUp" size={16} /></button>
                  <button className="icon-btn" onClick={() => move(i, 1)} disabled={i === items.length - 1} aria-label="Move down"><Icon name="arrowDown" size={16} /></button>
                  <button className="icon-btn danger" onClick={() => remove(it.id)} aria-label="Remove from playlist"><Icon name="x" size={16} /></button>
                </>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
