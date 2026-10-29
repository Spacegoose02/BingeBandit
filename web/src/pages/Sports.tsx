import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useBackState } from '../backState'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useApi, useDocumentTitle } from '../hooks'
import type { SportsGame, SportsHome, SportsLeague, SportsSide, SportsTeam } from '../types'

/** ESPN's day (US Eastern) as YYYYMMDD, days from today. */
function espnDay(offset: number) {
  const d = new Date(Date.now() + offset * 86400000)
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(d)
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return get('year') + get('month') + get('day')
}

function startLabel(iso: string) {
  const d = new Date(iso)
  const today = new Date()
  const tomorrow = new Date(Date.now() + 86400000)
  const time = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
  if (d.toDateString() === today.toDateString()) return `Today ${time}`
  if (d.toDateString() === tomorrow.toDateString()) return `Tomorrow ${time}`
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }) + ' ' + time
}

/** Scores and schedules for the teams each person follows, from ESPN. */
export default function SportsPage() {
  useDocumentTitle('Sports')
  const { data, error, reload } = useApi<SportsHome>('/api/sports', 30000)
  const [picking, setPicking] = useState(false)
  const leagues = data?.leagues ?? []
  const leagueName = (id: string) => leagues.find((l) => l.id === id)?.name ?? id
  const live = data?.games.filter((g) => g.state === 'in') ?? []
  const soon = data?.games.filter((g) => g.state === 'pre') ?? []
  const done = data?.games.filter((g) => g.state === 'post') ?? []

  return (
    <div className="page sports">
      <header className="page-head">
        <div>
          <h1>Sports</h1>
          <div className="muted">Your teams' games, live scores and results, from ESPN.</div>
        </div>
        <button className="btn btn-primary" onClick={() => setPicking(true)}>
          <Icon name="trophy" size={18} /> {data?.teams.length ? 'Change teams' : 'Pick your teams'}
        </button>
      </header>
      {(error || data?.error) && <div className="form-error">{error || data?.error}</div>}
      {data && data.teams.length === 0 && (
        <div className="empty">
          Pick the teams you follow, from the NFL and college football to the NBA, MLB, NHL and MLS. Their games show up here, with a
          heads-up before each one starts (you can change that under Account → Playback).
        </div>
      )}
      {data && data.teams.length > 0 && (
        <div className="sports-teams">
          {data.teams.map((t) => (
            <span key={t.league + t.id} className="sports-team-chip" title={`${t.name} · ${leagueName(t.league)}`}>
              <TeamLogo team={t} size={22} /> {t.shortName || t.name}
            </span>
          ))}
        </div>
      )}
      {live.length > 0 && <GameSection title="Live now" games={live} leagueName={leagueName} />}
      {soon.length > 0 && <GameSection title="Coming up" games={soon} leagueName={leagueName} />}
      {done.length > 0 && <GameSection title="Results" games={done} leagueName={leagueName} />}
      {data && data.teams.length > 0 && data.games.length === 0 && !data.error && (
        <div className="empty">No games for your teams from two days ago to a week from now.</div>
      )}
      {leagues.length > 0 && <Scoreboard leagues={leagues} first={data?.teams[0]?.league} />}
      {picking && data && (
        <TeamPicker
          leagues={leagues}
          chosen={data.teams}
          onClose={() => setPicking(false)}
          onSaved={() => {
            setPicking(false)
            reload()
          }}
        />
      )}
    </div>
  )
}

function GameSection({ title, games, leagueName }: { title: string; games: SportsGame[]; leagueName: (id: string) => string }) {
  return (
    <section className="sports-section">
      <h2>{title}</h2>
      <div className="sports-grid">
        {games.map((g) => (
          <GameCard key={g.id} game={g} leagueName={leagueName(g.league)} />
        ))}
      </div>
    </section>
  )
}

function TeamLogo({ team, size }: { team: SportsTeam; size: number }) {
  return team.logo ? (
    <img src={team.logo} alt="" width={size} height={size} className="sports-logo" loading="lazy" />
  ) : (
    <span className="sports-logo sports-logo-blank" style={{ width: size, height: size, background: team.color ? `#${team.color}` : undefined }} />
  )
}

function GameCard({ game: g, leagueName }: { game: SportsGame; leagueName: string }) {
  const side = (s: SportsSide) => (
    <div className={`sports-side${g.state === 'post' && s.winner ? ' winner' : ''}${g.mine?.includes(s.id) ? ' mine' : ''}`}>
      <TeamLogo team={s} size={32} />
      <span className="sports-name">
        {s.shortName || s.name}
        {s.record && <span className="muted small"> {s.record}</span>}
      </span>
      {g.state !== 'pre' && <span className="sports-score">{s.score}</span>}
    </div>
  )
  return (
    <div className={`sports-card panel${g.state === 'in' ? ' live' : ''}`}>
      <div className="sports-card-head">
        <span className="muted small">{leagueName}</span>
        {g.state === 'in' ? (
          <span className="sports-live">
            <span className="sports-dot" /> {g.detail}
          </span>
        ) : (
          <span className="muted small">{g.state === 'pre' ? startLabel(g.start) : g.detail}</span>
        )}
      </div>
      {side(g.away)}
      {side(g.home)}
      {(g.broadcasts?.length || g.venue) && (
        <div className="muted small sports-where">{[g.broadcasts?.join(', '), g.state !== 'post' ? g.venue : ''].filter(Boolean).join(' · ')}</div>
      )}
      {g.channels && g.channels.length > 0 && (
        <div className="sports-channels">
          {g.channels.map((c) => (
            <Link key={c.id} to={`/live/${c.id}`} className="btn btn-glass btn-sm" title={c.title}>
              <Icon name="play" size={14} fill /> {c.name}
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

/** Every game in one league on one day. */
function Scoreboard({ leagues, first }: { leagues: SportsLeague[]; first?: string }) {
  const [league, setLeague] = useBackState('scoreboard.league', first ?? leagues[0].id)
  const [offset, setOffset] = useBackState('scoreboard.day', 0)
  const { data, error } = useApi<SportsGame[]>(`/api/sports/${league}/scores?date=${espnDay(offset)}`, offset === 0 ? 30000 : undefined)
  const dayLabel = offset === 0 ? 'Today' : offset === -1 ? 'Yesterday' : offset === 1 ? 'Tomorrow' : new Date(Date.now() + offset * 86400000).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
  const name = leagues.find((l) => l.id === league)?.name ?? league
  return (
    <section className="sports-section">
      <div className="sports-board-head">
        <h2>All scores</h2>
        <select value={league} onChange={(e) => setLeague(e.target.value)} aria-label="League">
          {leagues.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
        <div className="segmented">
          <button onClick={() => setOffset(offset - 1)} aria-label="Day before">
            <Icon name="back" size={14} />
          </button>
          <button className="active" onClick={() => setOffset(0)}>
            {dayLabel}
          </button>
          <button onClick={() => setOffset(offset + 1)} aria-label="Day after">
            <Icon name="next" size={14} />
          </button>
        </div>
      </div>
      {error && <div className="form-error">{error}</div>}
      {data?.length === 0 && <div className="empty">No {name} games {offset === 0 ? 'today' : 'that day'}.</div>}
      <div className="sports-grid">
        {data?.map((g) => (
          <GameCard key={g.id} game={g} leagueName={name} />
        ))}
      </div>
    </section>
  )
}

function TeamPicker({ leagues, chosen, onClose, onSaved }: { leagues: SportsLeague[]; chosen: SportsTeam[]; onClose: () => void; onSaved: () => void }) {
  const { user } = useAuth()
  const [league, setLeague] = useState(chosen[0]?.league ?? leagues[0].id)
  const [picked, setPicked] = useState<SportsTeam[]>(chosen)
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const { data: teams, error: loadError } = useApi<SportsTeam[]>(`/api/sports/${league}/teams`)
  const key = (t: SportsTeam) => t.league + '/' + t.id
  const has = useMemo(() => new Set(picked.map(key)), [picked])
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (teams ?? []).filter((t) => !q || [t.name, t.shortName, t.abbr].some((n) => n?.toLowerCase().includes(q)))
  }, [teams, query])
  const toggle = (t: SportsTeam) => setPicked(has.has(key(t)) ? picked.filter((p) => key(p) !== key(t)) : [...picked, t])
  const save = async () => {
    setSaving(true)
    setError('')
    try {
      await api.put('/api/sports/teams', picked.map((t) => ({ league: t.league, id: t.id })))
      onSaved()
    } catch (e) {
      setError((e as Error).message)
      setSaving(false)
    }
  }
  return (
    <Modal title={user?.profileName ? `${user.profileName}'s teams` : 'Your teams'} onClose={onClose} wide>
      <div className="form">
        {picked.length > 0 && (
          <div className="chips">
            {picked.map((t) => (
              <span key={key(t)} className="chip removable">
                <TeamLogo team={t} size={16} /> {t.name}
                <button type="button" className="icon-btn" onClick={() => toggle(t)} aria-label={`Remove ${t.name}`}>
                  <Icon name="x" size={14} />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="tabs sports-tabs">
          {leagues.map((l) => (
            <button key={l.id} type="button" className={l.id === league ? 'active' : ''} onClick={() => setLeague(l.id)}>
              {l.name}
            </button>
          ))}
        </div>
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search teams" autoFocus />
        {loadError && <div className="form-error">{loadError}</div>}
        {!teams && !loadError && <div className="muted small">Loading teams…</div>}
        <div className="sports-pick-list">
          {shown.map((t) => (
            <button key={t.id} type="button" className={`sports-pick${has.has(key(t)) ? ' on' : ''}`} onClick={() => toggle(t)}>
              <TeamLogo team={t} size={28} />
              <span className="sports-pick-name">{t.name}</span>
              {has.has(key(t)) && <Icon name="check" size={16} />}
            </button>
          ))}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button type="button" className="btn btn-primary" onClick={save} disabled={saving}>
            Save {picked.length} {picked.length === 1 ? 'team' : 'teams'}
          </button>
          <button type="button" className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}
