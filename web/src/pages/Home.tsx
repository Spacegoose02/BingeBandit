import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { PosterCard, WideCard } from '../components/Cards'
import { Hero } from '../components/Hero'
import { Icon } from '../components/Icon'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import { img } from '../api'
import type { FriendActivity, HomeData } from '../types'

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
                <span className="avatar">{f.username.slice(0, 1).toUpperCase()}</span>
                {f.username}
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

export default function Home() {
  useDocumentTitle('Home')
  const { user } = useAuth()
  const { data, loading, error } = useApi<HomeData>('/api/home')

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
        {data.rows.map((row) => (
          <Row key={row.id} title={row.title}>
            {row.items.map((it) => (
              <PosterCard key={it.id} item={it} />
            ))}
          </Row>
        ))}
      </div>
    </div>
  )
}
