import { Link, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { ColumnChart, RankedBars, StatTile } from '../components/Charts'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { timeAgo } from '../format'
import type { Play, UserStats } from '../types'

interface ProfileData {
  user: { username: string; createdAt: number; profilePublic: boolean; isMe: boolean }
  stats: UserStats
  recent: Play[]
}

export const hours = (h: number) => (h >= 10 ? Math.round(h).toLocaleString() : h.toFixed(1))
const monthName = (key: string) => new Date(`${key}-01T00:00:00`).toLocaleDateString(undefined, { month: 'short' })

export function playLink(p: Play) {
  return p.itemId ? `/item/${p.itemId}` : undefined
}

export function HistoryList({ plays, showUser }: { plays: Play[]; showUser?: boolean }) {
  if (!plays.length) return <div className="muted small">Nothing watched yet.</div>
  return (
    <div className="panel list">
      {plays.map((p) => (
        <div key={p.id} className="list-row history-row">
          <span className="history-poster">{p.poster && <img src={img(p.poster, 'w92')} alt="" loading="lazy" />}</span>
          <div className="grow">
            <div>{playLink(p) ? <Link to={playLink(p)!}>{p.title}</Link> : p.title}</div>
            <div className="muted small">
              {[p.subtitle, showUser && p.username, timeAgo(p.startedAt), `${Math.round(p.watched / 60)} min watched`, p.client]
                .filter(Boolean)
                .join(' · ')}
            </div>
          </div>
          {showUser && p.ip && <span className="muted small mono">{p.ip}</span>}
          <span className={`pill ${p.mode === 'direct' ? 'ok' : 'warn'}`}>{p.mode === 'direct' ? 'Direct' : 'Transcode'}</span>
          {p.completed && <Icon name="check" size={16} className="accent" />}
        </div>
      ))}
    </div>
  )
}

export default function ProfilePage() {
  const { username: param } = useParams()
  const { user: me } = useAuth()
  const username = param ?? me?.username ?? ''
  const { data, error, reload } = useApi<ProfileData>(`/api/profiles/${encodeURIComponent(username)}`)
  useDocumentTitle(data ? `${data.user.username}'s profile` : 'Profile')
  if (error) return <div className="page"><div className="empty">This profile is private or doesn't exist.</div></div>
  if (!data) return <div className="page-loading" />
  const { user, stats, recent } = data

  const toggleShare = async () => {
    await api.put('/api/me/profile', { public: !user.profilePublic })
    reload()
  }

  return (
    <div className="page">
      <header className="page-head">
        <div className="profile-head">
          <span className="avatar big">{user.username.slice(0, 1).toUpperCase()}</span>
          <div>
            <h1>{user.username}</h1>
            <div className="muted">Watching since {new Date(user.createdAt * 1000).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</div>
          </div>
        </div>
        {user.isMe && (
          <label className="check share-toggle">
            <span className="switch">
              <input type="checkbox" checked={user.profilePublic} onChange={toggleShare} />
              <span />
            </span>
            Share my profile with other people on this server
          </label>
        )}
      </header>

      <div className="stat-grid">
        <StatTile label="Hours watched" value={hours(stats.hours)} />
        <StatTile label="Movies finished" value={stats.movies.toLocaleString()} />
        <StatTile label="Episodes finished" value={stats.episodes.toLocaleString()} />
        <StatTile label="Shows watched" value={stats.shows.toLocaleString()} />
        <StatTile
          label="Favorite time to watch"
          value={stats.favoriteHour < 0 ? '—' : new Date(2000, 0, 1, stats.favoriteHour).toLocaleTimeString([], { hour: 'numeric' })}
        />
      </div>

      <h2 className="section-title">Hours watched per month</h2>
      <div className="panel">
        <ColumnChart
          caption="Month"
          valueLabel="Hours"
          data={stats.monthlyHours.map((m) => ({
            label: monthName(m.key),
            value: Math.round(m.hours * 100) / 100,
            tip: (
              <>
                <strong>{new Date(`${m.key}-01T00:00:00`).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</strong>
                <div>{hours(m.hours)} hours · {m.plays} plays</div>
              </>
            ),
          }))}
        />
      </div>

      <div className="two-col">
        <section>
          <h2 className="section-title">Top genres</h2>
          <div className="panel">
            <RankedBars rows={stats.topGenres.map((g) => ({ label: g.key, value: g.hours, display: `${hours(g.hours)} h` }))} />
          </div>
        </section>
        <section>
          <h2 className="section-title">Most watched shows</h2>
          <div className="panel">
            <RankedBars
              rows={stats.topShows.map((s) => ({ label: s.key, value: s.hours, display: `${hours(s.hours)} h`, href: `/item/${s.itemId}`, poster: s.poster ?? '' }))}
            />
          </div>
        </section>
      </div>

      <h2 className="section-title">Recently watched</h2>
      <HistoryList plays={recent} />
    </div>
  )
}
