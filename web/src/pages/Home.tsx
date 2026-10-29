import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { PosterCard, WideCard } from '../components/Cards'
import { Hero } from '../components/Hero'
import { Icon } from '../components/Icon'
import { favoritesChanged } from '../components/AddToPlaylist'
import { pinsChanged } from '../components/Pins'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import { api, img } from '../api'
import type { FriendActivity, HomeData, Item, Recommendation } from '../types'

/** What people who chose to share are watching right now. */
function FriendsRow() {
  const { data } = useApi<FriendActivity[]>('/api/activity/friends', 30000)
  if (!data?.length) return null
  return (
    <Row title="Friends are watching">
      {data.map((f) => {
        const poster = img(f.poster, 'w342')
        return (
          <Link key={f.username} to={`/item/${f.itemId}`} className="poster-card friend-card">
            <div className="poster-frame">
              {poster ? <img src={poster} alt="" loading="lazy" /> : <div className="poster-fallback">{f.title}</div>}
              <span className="friend-badge">
                <span className="avatar">{(f.name || f.username).slice(0, 1).toUpperCase()}</span>
                {f.name || f.username}
              </span>
              {f.duration > 0 && (
                <div className="progress">
                  <div style={{ width: `${Math.min(100, (f.position / f.duration) * 100)}%` }} />
                </div>
              )}
            </div>
            <div className="card-title" title={f.title}>{f.title}</div>
            <div className="card-sub">{f.paused ? 'Paused' : f.subtitle || 'Watching now'}</div>
          </Link>
        )
      })}
    </Row>
  )
}

/** Titles people recommended to you, with who did; each can be dismissed. */
function RecommendedRow({ title, items, by }: { title: string; items: Item[]; by: Record<string, Recommendation> }) {
  const [hidden, setHidden] = useState<number[]>([])
  const shown = items.filter((it) => !hidden.includes(it.id))
  if (!shown.length) return null
  const dismiss = (id: number) => {
    setHidden((h) => [...h, id])
    api.del(`/api/recommendations/${id}`).catch(() => undefined)
  }
  return (
    <Row title={title}>
      {shown.map((it) => {
        const rec = by[it.id]
        const from = rec ? `From ${rec.from.join(', ')}` : undefined
        return (
          <div key={it.id} className="recommended-card" title={rec?.notes?.join('\n')}>
            <PosterCard item={it} subtitle={from} />
            {!!rec?.notes?.length && <div className="rec-note">“{rec.notes[0].split(': ').slice(1).join(': ')}”</div>}
            <button className="rec-dismiss" onClick={() => dismiss(it.id)} title="Not interested">
              <Icon name="x" size={14} />
            </button>
          </div>
        )
      })}
    </Row>
  )
}

export default function Home() {
  useDocumentTitle('Home')
  const { user } = useAuth()
  const { data, loading, error, reload } = useApi<HomeData>('/api/home')
  useEffect(() => {
    window.addEventListener(pinsChanged, reload)
    window.addEventListener(favoritesChanged, reload)
    return () => {
      window.removeEventListener(pinsChanged, reload)
      window.removeEventListener(favoritesChanged, reload)
    }
  }, [reload])

  if (loading) return <div className="page-loading" />
  if (error) return <div className="page"><div className="empty">Could not load home: {error}</div></div>
  if (!data) return null

  const empty = data.rows.length === 0 && data.continueWatching.length === 0
  if (empty) {
    return (
      <div className="page">
        <div className="empty-hero">
          <img src="/brand/mascot.png" alt="" />
          <h1>Nothing to binge yet</h1>
          {user?.isAdmin ? (
            <>
              <p>Add a library folder and BingeBandit will scan it and pull artwork, ratings and cast from TMDB.</p>
              <Link to="/admin/libraries" className="btn btn-primary">
                <Icon name="plus" size={18} /> Add a library
              </Link>
            </>
          ) : (
            <p>Your server admin has not added any libraries yet.</p>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="home">
      <Hero items={data.hero} />
      <div className="rows">
        {data.continueWatching.length > 0 && (
          <Row title="Continue Watching">
            {data.continueWatching.map((it) => (
              <WideCard key={it.id} item={it} />
            ))}
          </Row>
        )}
        <FriendsRow />
        {data.rows.map((row) =>
          row.id === 'recommended' ? (
            <RecommendedRow key={row.id} title={row.title} items={row.items} by={data.recommendedBy ?? {}} />
          ) : (
            <Row
              key={row.id}
              title={row.title}
              to={row.collectionId ? `/collection/${row.collectionId}` : row.id === 'favorites' ? '/favorites' : row.id === 'watchlist' ? '/watchlist' : undefined}
            >
              {row.items.map((it) => (
                <PosterCard key={it.id} item={it} />
              ))}
            </Row>
          ),
        )}
      </div>
    </div>
  )
}
