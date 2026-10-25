import { useState, type FormEvent } from 'react'
import { api } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import type { DiscoverRow, DiscoverSource, DiscoverVideo } from '../types'

const ago = (t: number) => {
  const days = Math.floor((Date.now() / 1000 - t) / 86400)
  if (days < 1) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 30) return `${days} days ago`
  return new Date(t * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

/** New trailers and shows from YouTube playlists and channels the admin picked. */
export function DiscoverPage() {
  useDocumentTitle('Discover')
  const { user } = useAuth()
  const { data, reload } = useApi<DiscoverRow[]>('/api/discover')
  const [playing, setPlaying] = useState<DiscoverVideo | null>(null)
  const [managing, setManaging] = useState(false)

  return (
    <div className="discover">
      <header className="page-head discover-head">
        <div>
          <h1>Discover</h1>
          <div className="muted">New trailers and shows from YouTube.</div>
        </div>
        {user?.isAdmin && (
          <button className="btn btn-glass" onClick={() => setManaging(true)}>
            <Icon name="settings" size={18} /> Manage
          </button>
        )}
      </header>
      {data?.length === 0 && (
        <div className="empty discover-empty">
          {user?.isAdmin
            ? 'Add a YouTube playlist or channel (a trailer channel, a "new on Netflix" playlist…) with Manage, and its newest videos show up here.'
            : 'Nothing here yet. Your server admin can add YouTube playlists and channels.'}
        </div>
      )}
      <div className="rows">
        {data?.map((row) => (
          <Row key={row.id} title={row.name}>
            {row.videos.map((v) => (
              <div key={v.id} className="wide-card">
                <button className="wide-frame video-card" onClick={() => setPlaying(v)} title={v.description || v.title}>
                  <img src={`https://i.ytimg.com/vi/${v.id}/mqdefault.jpg`} alt="" loading="lazy" />
                  <div className="poster-hover">
                    <Icon name="play" fill size={34} />
                  </div>
                </button>
                <div className="card-title" title={v.title}>{v.title}</div>
                <div className="card-sub">{ago(v.publishedAt)}</div>
              </div>
            ))}
          </Row>
        ))}
      </div>
      {playing && (
        <Modal title={playing.title} onClose={() => setPlaying(null)} wide>
          <div className="video-embed">
            <iframe
              src={`https://www.youtube-nocookie.com/embed/${playing.id}?autoplay=1&rel=0`}
              title={playing.title}
              allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
              allowFullScreen
            />
          </div>
          {playing.description && <p className="muted small discover-desc">{playing.description}</p>}
        </Modal>
      )}
      {managing && (
        <ManageSources
          onClose={() => {
            setManaging(false)
            reload()
          }}
        />
      )}
    </div>
  )
}

function ManageSources({ onClose }: { onClose: () => void }) {
  const { data, reload, setData } = useApi<DiscoverSource[]>('/api/admin/discover')
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState<'add' | 'refresh' | null>(null)
  const [error, setError] = useState('')

  const add = async (e: FormEvent) => {
    e.preventDefault()
    setBusy('add')
    setError('')
    try {
      await api.post('/api/admin/discover', { url, name })
      setUrl('')
      setName('')
      reload()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const refresh = async () => {
    setBusy('refresh')
    setError('')
    try {
      setData(await api.post<DiscoverSource[]>('/api/admin/discover/refresh'))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const move = async (i: number, dir: number) => {
    if (!data) return
    const next = [...data]
    ;[next[i], next[i + dir]] = [next[i + dir], next[i]]
    setData(next)
    await api.put('/api/admin/discover/order', { ids: next.map((s) => s.id) }).catch((e) => setError(e.message))
  }

  const rename = async (s: DiscoverSource) => {
    const n = prompt('Name', s.name)?.trim()
    if (!n || n === s.name) return
    await api.put(`/api/admin/discover/${s.id}`, { name: n }).catch((e) => setError(e.message))
    reload()
  }

  const remove = async (s: DiscoverSource) => {
    if (!confirm(`Remove "${s.name}" from Discover?`)) return
    await api.del(`/api/admin/discover/${s.id}`).catch((e) => setError(e.message))
    reload()
  }

  return (
    <Modal title="Discover sources" onClose={onClose} wide>
      <form className="form" onSubmit={add}>
        <label>
          YouTube playlist or channel
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://www.youtube.com/playlist?list=… or https://www.youtube.com/@channel"
            required
          />
        </label>
        <label>
          Row name (optional)
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Uses the playlist or channel name" />
        </label>
        <div className="actions">
          <button className="btn btn-primary" disabled={busy !== null || !url.trim()}>
            <Icon name="plus" size={16} /> {busy === 'add' ? 'Checking…' : 'Add'}
          </button>
        </div>
      </form>
      {error && <div className="form-error">{error}</div>}
      <div className="discover-sources">
        {data?.map((s, i) => (
          <div key={s.id} className="discover-source">
            <Icon name={s.kind === 'channel' ? 'user' : 'list'} size={18} />
            <div className="grow">
              <div>{s.name}</div>
              <div className={`small ${s.error ? 'form-error' : 'muted'}`}>
                {s.error ? `Last refresh failed: ${s.error}` : `${s.kind === 'channel' ? 'Channel' : 'Playlist'} · updated ${s.fetchedAt ? ago(s.fetchedAt).toLowerCase() : 'never'}`}
              </div>
            </div>
            <button className="icon-btn" disabled={i === 0} onClick={() => move(i, -1)} aria-label="Move up">
              <Icon name="arrowUp" size={16} />
            </button>
            <button className="icon-btn" disabled={i === data.length - 1} onClick={() => move(i, 1)} aria-label="Move down">
              <Icon name="arrowDown" size={16} />
            </button>
            <button className="icon-btn" onClick={() => rename(s)} aria-label="Rename">
              <Icon name="edit" size={16} />
            </button>
            <button className="icon-btn" onClick={() => remove(s)} aria-label="Remove">
              <Icon name="trash" size={16} />
            </button>
          </div>
        ))}
      </div>
      <p className="muted small">
        Each one shows its 15 newest videos (YouTube's feed limit). Older ones stay until there are 50. The “Refresh Discover” task checks every
        few hours.
      </p>
      <div className="actions end">
        {!!data?.length && (
          <button className="btn btn-glass" onClick={refresh} disabled={busy !== null}>
            <Icon name="refresh" size={16} className={busy === 'refresh' ? 'spin' : ''} /> Refresh now
          </button>
        )}
        <button className="btn btn-primary" onClick={onClose}>Done</button>
      </div>
    </Modal>
  )
}
