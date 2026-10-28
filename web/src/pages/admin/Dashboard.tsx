import { Link } from 'react-router-dom'
import { api, img } from '../../api'
import { Icon } from '../../components/Icon'
import { useApi, useDocumentTitle } from '../../hooks'
import { formatBytes, formatClock, timeAgo } from '../../format'
import type { Library, NowPlaying, ScanStatus, Stats, SystemInfo } from '../../types'

export default function AdminDashboard() {
  useDocumentTitle('Dashboard')
  const { data: activity, reload } = useApi<{ nowPlaying: NowPlaying[] }>('/api/admin/activity', 5000)
  const { data: stats } = useApi<Stats>('/api/admin/stats', 30000)
  const { data: system } = useApi<SystemInfo>('/api/admin/system')
  const { data: scans } = useApi<ScanStatus[]>('/api/admin/scan', 3000)
  const { data: libraries } = useApi<Library[]>('/api/admin/libraries', 30000)
  const running = scans?.filter((s) => s.running) ?? []

  const kill = async (id: string) => {
    await api.del(`/api/admin/activity/${id}`)
    reload()
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>Dashboard</h1>
        {system && <div className="muted small">BingeBandit {system.version} · {system.os}</div>}
      </header>

      {system && !system.ffmpeg && (
        <div className="notice warn">FFmpeg was not found. Install it or set BB_FFMPEG, otherwise scanning and transcoding will not work.</div>
      )}
      {system && !system.metadataActive && (
        <div className="notice">
          Add a free TMDB API key in <Link to="/admin/settings">Settings</Link> to get posters, ratings, cast and trailers.
        </div>
      )}

      {stats && (
        <div className="stat-grid">
          <Stat label="Movies" value={stats.movies} />
          <Stat label="Shows" value={stats.shows} />
          <Stat label="Episodes" value={stats.episodes} />
          <Stat label="Files" value={stats.files} />
          <Stat label="Library size" value={formatBytes(stats.totalSize)} />
          <Stat label="Hours of content" value={Math.round(stats.totalHours).toLocaleString()} />
          <Stat label="Users" value={stats.users} />
        </div>
      )}

      <h2 className="section-title">
        Now Playing <span className="count">{activity?.nowPlaying.length ?? 0}</span>
      </h2>
      {activity?.nowPlaying.length === 0 && <div className="empty small">Nobody is watching right now.</div>}
      <div className="np-grid">
        {activity?.nowPlaying.map((np) => (
          <div key={np.id} className="np-card">
            <div className="np-head">
              {np.poster ? <img src={img(np.poster, 'w185')} alt="" /> : <div className="np-poster-fallback" />}
              <div className="np-titles">
                <Link to={`/item/${np.itemId}`} className="np-title">{np.title}</Link>
                {np.subtitle && <div className="muted small">{np.subtitle}</div>}
                <div className="np-state">
                  <Icon name={np.paused ? 'pause' : 'play'} fill size={14} />
                  {np.paused ? 'Paused' : 'Playing'} · {formatClock(np.position)} / {formatClock(np.duration)}
                </div>
              </div>
              <button className="icon-btn danger" title="Stop this stream" onClick={() => kill(np.id)}>
                <Icon name="x" size={18} />
              </button>
            </div>
            <div className="progress inline">
              <div style={{ width: `${np.duration ? (np.position / np.duration) * 100 : 0}%` }} />
            </div>
            <dl className="np-facts">
              <dt>User</dt>
              <dd>
                {np.name && np.name !== np.username ? <>{np.name} <span className="muted small">({np.username})</span></> : np.username}
              </dd>
              <dt>Client</dt>
              <dd>{np.client}</dd>
              <dt>Location</dt>
              <dd>
                <span className={`pill ${np.local ? 'ok' : 'remote'}`}>{np.local ? 'Local' : 'Remote'}</span> {np.ip}
              </dd>
              <dt>Source</dt>
              <dd>{np.source || '—'}</dd>
              <dt>Stream</dt>
              <dd>
                <span className={`pill ${np.mode === 'direct' ? 'ok' : 'warn'}`}>{np.mode === 'direct' ? 'Direct Play' : 'Transcode'}</span> {np.mode !== 'direct' && np.target}
              </dd>
            </dl>
          </div>
        ))}
      </div>

      <h2 className="section-title">Libraries</h2>
      <div className="panel list">
        {libraries?.map((l) => {
          const scan = running.find((s) => s.libraryId === l.id)
          return (
            <div key={l.id} className="list-row">
              <Icon name={l.kind === 'movies' ? 'film' : 'tv'} />
              <div className="grow">
                <div>{l.name}</div>
                <div className="muted small">
                  {l.itemCount} {l.kind === 'movies' ? 'movies' : 'shows'} · scanned {timeAgo(l.lastScan)}
                </div>
              </div>
              {scan && <ScanBadge scan={scan} />}
            </div>
          )
        })}
        {libraries?.length === 0 && (
          <div className="list-row muted">
            No libraries yet. <Link to="/admin/libraries">Add one</Link>
          </div>
        )}
      </div>

      {system && (
        <>
          <h2 className="section-title">System</h2>
          <dl className="panel facts">
            <dt>Data folder</dt>
            <dd className="mono">{system.dataDir}</dd>
            <dt>FFmpeg</dt>
            <dd className="mono">{system.ffmpeg || 'not found'}</dd>
            <dt>FFprobe</dt>
            <dd className="mono">{system.ffprobe || 'not found'}</dd>
          </dl>
        </>
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="stat">
      <div className="stat-value">{typeof value === 'number' ? value.toLocaleString() : value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

export function ScanBadge({ scan }: { scan: ScanStatus }) {
  const label = scan.phase === 'metadata' ? 'Fetching metadata' : scan.phase === 'moving' ? 'Moving a title' : 'Scanning'
  return (
    <span className="pill warn">
      <Icon name="refresh" size={12} className="spin" /> {label} {scan.found ? `${scan.processed}/${scan.found}` : ''}
    </span>
  )
}
