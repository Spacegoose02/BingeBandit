import { useState } from 'react'
import { ColumnChart, RankedBars, StatTile } from '../../components/Charts'
import { useApi, useDocumentTitle } from '../../hooks'
import type { Play, ServerStats } from '../../types'
import { HistoryList, hours } from '../Profile'

const ranges: [number, string][] = [
  [7, '7 days'],
  [30, '30 days'],
  [90, '90 days'],
  [365, '12 months'],
]

const dayLabel = (key: string, opts: Intl.DateTimeFormatOptions) => new Date(`${key}T00:00:00`).toLocaleDateString(undefined, opts)

export default function AdminStatistics() {
  useDocumentTitle('Statistics')
  const [days, setDays] = useState(30)
  const [page, setPage] = useState(0)
  const { data } = useApi<ServerStats>(`/api/admin/statistics?days=${days}`)
  const { data: history } = useApi<Play[]>(`/api/admin/history?limit=25&offset=${page * 25}`)
  const plays = (rows: ServerStats['topMovies']) => rows.map((r) => ({ label: r.key, value: r.plays, display: `${r.plays} plays` }))

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Statistics</h1>
          <div className="muted">What's being watched on this server and how it's streamed.</div>
        </div>
        <div className="segmented">
          {ranges.map(([d, label]) => (
            <button key={d} className={d === days ? 'active' : ''} onClick={() => setDays(d)}>
              {label}
            </button>
          ))}
        </div>
      </header>

      {data && (
        <>
          <div className="stat-grid">
            <StatTile label="Plays" value={data.totalPlays.toLocaleString()} />
            <StatTile label="Hours streamed" value={hours(data.totalHours)} />
            <StatTile label="Active viewers" value={data.topUsers.length} />
          </div>

          <h2 className="section-title">Plays per day</h2>
          <div className="panel">
            <ColumnChart
              caption="Day"
              valueLabel="Plays"
              labelEvery={days <= 7 ? 1 : days <= 31 ? 5 : days <= 90 ? 14 : 60}
              data={data.daily.map((d) => ({
                label: dayLabel(d.key, days <= 7 ? { weekday: 'short' } : { month: 'short', day: 'numeric' }),
                value: d.plays,
                tip: (
                  <>
                    <strong>{dayLabel(d.key, { weekday: 'long', month: 'short', day: 'numeric' })}</strong>
                    <div>{d.plays} plays · {hours(d.hours)} hours</div>
                  </>
                ),
              }))}
            />
          </div>

          <div className="two-col">
            <section>
              <h2 className="section-title">Top movies</h2>
              <div className="panel">
                <RankedBars
                  rows={data.topMovies.map((r) => ({ ...plays([r])[0], href: r.itemId ? `/item/${r.itemId}` : undefined, poster: r.poster ?? '' }))}
                />
              </div>
            </section>
            <section>
              <h2 className="section-title">Top shows</h2>
              <div className="panel">
                <RankedBars rows={data.topShows.map((r) => ({ ...plays([r])[0], href: `/item/${r.itemId}`, poster: r.poster ?? '' }))} />
              </div>
            </section>
            <section>
              <h2 className="section-title">Top viewers</h2>
              <div className="panel">
                <RankedBars rows={data.topUsers.map((r) => ({ label: r.key, value: r.hours, display: `${hours(r.hours)} h · ${r.plays} plays`, href: `/profile/${encodeURIComponent(r.key)}` }))} />
              </div>
            </section>
            <section>
              <h2 className="section-title">Streaming</h2>
              <div className="panel">
                <RankedBars rows={plays(data.modes)} />
                <div className="label" style={{ margin: '18px 0 10px' }}>Clients</div>
                <RankedBars rows={plays(data.clients)} />
              </div>
            </section>
          </div>
        </>
      )}

      <h2 className="section-title">Watch history</h2>
      {history && <HistoryList plays={history} showUser />}
      <div className="actions" style={{ marginTop: 12 }}>
        <button className="btn btn-glass btn-sm" disabled={page === 0} onClick={() => setPage(page - 1)}>Newer</button>
        <button className="btn btn-glass btn-sm" disabled={(history?.length ?? 0) < 25} onClick={() => setPage(page + 1)}>Older</button>
      </div>
    </div>
  )
}
