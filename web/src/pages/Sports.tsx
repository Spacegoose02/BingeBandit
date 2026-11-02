import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useBackState } from '../backState'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useApi, useDocumentTitle } from '../hooks'
import { MAX_SCREENS, byState, gamesByChannel, isSportsChannel, multiViewPicks, multiViewUrl, setMultiViewPicks } from '../sportsHub'
import type { LiveChannel, Prefs, SportsDay, SportsGame, SportsHome, SportsLeader, SportsLeague, SportsSide, SportsSummary, SportsTeam } from '../types'

/**
 * The Sports Hub on the web, laid out like the TV app's: Scores, My Teams
 * and Channels tabs, a day picker, the focused game's line score up top,
 * sport chips and "With a channel", then Live now, Your teams and a row per
 * league in this profile's order (Rows in the gear). Clicking a game opens
 * its box score, stats, leaders and standings.
 */

/** What every game card needs from the page. */
interface Hub {
  picks: number[]
  pick: (channelId: number) => void
  spoilerFree: boolean
  revealed: Set<string>
  reveal: (gameId: string) => void
  leagueOf: (id: string) => SportsLeague | undefined
  open: (g: SportsGame) => void
  focus: (g: SportsGame) => void
}
const HubContext = createContext<Hub>({
  picks: [],
  pick: () => undefined,
  spoilerFree: false,
  revealed: new Set(),
  reveal: () => undefined,
  leagueOf: () => undefined,
  open: () => undefined,
  focus: () => undefined,
})

type Tab = 'scores' | 'teams' | 'channels'
const tabNames: Record<Tab, string> = { scores: 'Scores', teams: 'My Teams', channels: 'Channels' }
const DAYS = [-3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7]

/** Each sport's stripe colour on cards and chips (the TV app's). */
const sportColors: Record<string, string> = {
  football: '#f5b942',
  basketball: '#f59e42',
  baseball: '#f87171',
  hockey: '#60a5fa',
  soccer: '#4ade80',
  mma: '#e5e7eb',
  racing: '#facc15',
}
const sportNames: Record<string, string> = {
  football: 'Football',
  basketball: 'Basketball',
  baseball: 'Baseball',
  hockey: 'Hockey',
  soccer: 'Soccer',
  mma: 'UFC',
  racing: 'NASCAR',
}

/** UFC and NASCAR have fighters and drivers, not teams to follow or box scores. */
const hasTeams = (sport?: string) => sport !== 'mma' && sport !== 'racing'
const isRace = (g: SportsGame) => !!g.race || (!g.home.id && !g.away.id)

/** ESPN's day (US Eastern) as YYYYMMDD, days from today. */
function espnDay(offset: number) {
  const d = new Date(Date.now() + offset * 86400000)
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(d)
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return get('year') + get('month') + get('day')
}

function dayLabel(offset: number) {
  if (offset === 0) return 'Today'
  if (offset === -1) return 'Yesterday'
  if (offset === 1) return 'Tomorrow'
  return new Date(Date.now() + offset * 86400000).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

function startLabel(iso: string) {
  const d = new Date(iso)
  const time = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
  if (d.toDateString() === new Date().toDateString()) return `Today ${time}`
  if (d.toDateString() === new Date(Date.now() + 86400000).toDateString()) return `Tomorrow ${time}`
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }) + ' ' + time
}

const gameStatus = (g: SportsGame) => (g.state === 'pre' ? startLabel(g.start) : g.detail)

/** Starting within the next hour. */
function startingSoon(g: SportsGame) {
  const t = new Date(g.start).getTime() - Date.now()
  return g.state === 'pre' && t > -15 * 60000 && t < 60 * 60000
}

function leaderText(l?: SportsLeader, long?: boolean) {
  if (!l) return ''
  const who = long ? [l.name, l.position].filter(Boolean).join(' · ') : l.shortName
  return [who, l.value].filter(Boolean).join(long ? '  ·  ' : ' ')
}

/** Changes some preferences, keeping the rest as the server has them. */
async function patchPrefs(patch: Partial<Prefs>) {
  const cur = await api.get<Prefs>('/api/me/prefs')
  return api.put<Prefs>('/api/me/prefs', { ...cur, ...patch })
}

export default function SportsPage() {
  useDocumentTitle('Sports')
  const navigate = useNavigate()
  const [tab, setTab] = useBackState<Tab>('sports.tab', 'scores')
  const [offset, setOffset] = useBackState('sports.day', 0)
  const [sport, setSport] = useBackState('sports.sport', 'all')
  const [withChannel, setWithChannel] = useBackState('sports.withChannel', false)
  const { data: day, error: dayError, reload: reloadDay } = useApi<SportsDay>(`/api/sports/scores?date=${espnDay(offset)}`, 30000)
  const { data: home, reload: reloadHome } = useApi<SportsHome>('/api/sports', 30000)
  const { data: prefs, reload: reloadPrefs } = useApi<Prefs>('/api/me/prefs')
  const { data: channels } = useApi<LiveChannel[]>(tab === 'channels' ? '/api/live/channels' : null)
  const [picking, setPicking] = useState(false)
  const [rowsOpen, setRowsOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [details, setDetails] = useState<SportsGame | null>(null)
  const [focused, setFocused] = useState<SportsGame | null>(null)
  const [picks, setPicks] = useState(multiViewPicks)
  const [notice, setNotice] = useState('')
  const [revealed, setRevealed] = useState<Set<string>>(() => new Set())
  const say = useCallback((msg: string) => {
    setNotice(msg)
    window.setTimeout(() => setNotice((n) => (n === msg ? '' : n)), 4000)
  }, [])
  const reload = useCallback(() => {
    reloadDay()
    reloadHome()
  }, [reloadDay, reloadHome])

  const leagues = useMemo(() => day?.leagues ?? home?.leagues ?? [], [day, home])
  const allLeagues = useMemo(() => day?.allLeagues ?? home?.allLeagues ?? leagues, [day, home, leagues])
  const leagueOf = useCallback((id: string) => allLeagues.find((l) => l.id === id), [allLeagues])
  const followed = useMemo(() => home?.teams ?? day?.teams ?? [], [home, day])

  const pick = useCallback(
    (id: number) => {
      const cur = multiViewPicks()
      if (cur.includes(id)) return say('Already in Multi-View.')
      if (cur.length >= MAX_SCREENS) return say(`Multi-View is full: ${MAX_SCREENS} screens.`)
      cur.push(id)
      setMultiViewPicks(cur)
      setPicks(multiViewPicks())
      say(`Added to Multi-View (${cur.length} of ${MAX_SCREENS}).`)
    },
    [say],
  )
  const spoilerFree = !!prefs?.spoilerFree
  const reveal = useCallback((id: string) => setRevealed((r) => new Set(r).add(id)), [])
  const hub = useMemo<Hub>(
    () => ({ picks, pick, spoilerFree, revealed, reveal, leagueOf, open: setDetails, focus: setFocused }),
    [picks, pick, spoilerFree, revealed, reveal, leagueOf],
  )

  const follow = async (s: SportsSide, on: boolean) => {
    const teams = on ? [...followed, s] : followed.filter((t) => !(t.league === s.league && t.id === s.id))
    try {
      await api.put('/api/sports/teams', teams.map((t) => ({ league: t.league, id: t.id })))
      say(on ? `Following ${s.name}.` : `Stopped following ${s.name}.`)
      reload()
    } catch (e) {
      say((e as Error).message)
    }
  }
  const setPref = async (patch: Partial<Prefs>) => {
    try {
      await patchPrefs(patch)
      reloadPrefs()
    } catch (e) {
      say((e as Error).message)
    }
  }

  // Scores: live and starting soon, your teams, then each league in this profile's order.
  const sportOf = (g: SportsGame) => leagueOf(g.league)?.sport ?? ''
  const games = day?.games ?? []
  const all = games.filter((g) => (sport === 'all' || sportOf(g) === sport) && (!withChannel || g.channels?.length))
  const sports = Array.from(new Set(games.map(sportOf))).filter(Boolean)
  const mineFirst = (a: SportsGame, b: SportsGame) => (b.mine?.length ? 1 : 0) - (a.mine?.length ? 1 : 0) || byState(a, b)
  const liveGames = all.filter((g) => g.state === 'in' || startingSoon(g)).sort(mineFirst)
  const inProgress = all.filter((g) => g.state === 'in').length
  const yours = all.filter((g) => g.mine?.length).sort(byState)
  const byLeague = leagues.map((l) => [l, all.filter((g) => g.league === l.id).sort(byState)] as const).filter(([, gs]) => gs.length)
  const featured = (focused && games.find((g) => g.id === focused.id)) ?? focused ?? liveGames[0] ?? all[0]
  const error = dayError || day?.error

  return (
    <HubContext.Provider value={hub}>
      <div className="page sports">
        <header className="sports-hub-head">
          <h1>Sports</h1>
          <div className="sports-view-tabs" role="tablist">
            {(Object.keys(tabNames) as Tab[]).map((t) => (
              <button key={t} role="tab" aria-selected={tab === t} className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
                {tabNames[t]}
              </button>
            ))}
          </div>
          <div className="sports-actions">
            {tab === 'scores' && (
              <select value={offset} onChange={(e) => setOffset(Number(e.target.value))} aria-label="Day">
                {DAYS.map((d) => (
                  <option key={d} value={d}>
                    {dayLabel(d)}
                  </option>
                ))}
              </select>
            )}
            <button className="icon-btn" onClick={reload} aria-label="Refresh" title="Refresh">
              <Icon name="refresh" />
            </button>
            <Link className="btn btn-glass" to={multiViewUrl(picks)}>
              <Icon name="stack" size={18} /> Multi-View{picks.length ? ` (${picks.length})` : ''}
            </Link>
            <button className="icon-btn" onClick={() => setSettingsOpen(true)} aria-label="Sports settings" title="Sports settings">
              <Icon name="settings" />
            </button>
          </div>
        </header>

        {notice && (
          <div className="sports-notice" role="status">
            {notice}
            {picks.length > 0 && notice.startsWith('Added') && (
              <Link to={multiViewUrl(picks)} className="btn btn-sm btn-primary">
                Open Multi-View
              </Link>
            )}
          </div>
        )}

        {featured && <Featured game={featured} />}
        {error && <div className="form-error">{error}</div>}

        {tab === 'scores' && (
          <>
            <div className="sports-chips">
              <button className={`filter-toggle${sport === 'all' ? ' active' : ''}`} onClick={() => setSport('all')}>
                All
              </button>
              {sports.map((s) => (
                <button
                  key={s}
                  className={`filter-toggle${sport === s ? ' active' : ''}`}
                  style={{ borderLeft: `3px solid ${sportColors[s] ?? 'var(--border)'}` }}
                  onClick={() => setSport(s)}
                >
                  {sportNames[s] ?? s}
                </button>
              ))}
              <span className="sports-chip-gap" />
              <button className={`filter-toggle${withChannel ? ' active' : ''}`} onClick={() => setWithChannel(!withChannel)}>
                With a channel
              </button>
            </div>
            {!day && !dayError && <div className="muted">Loading scores…</div>}
            {day && all.length === 0 && (
              <div className="empty">
                No games {withChannel ? 'on your channels ' : ''}
                {offset === 0 ? 'today' : `on ${dayLabel(offset)}`}.
              </div>
            )}
            {liveGames.length > 0 && <Row title="Live now" sub={`${inProgress} in progress · ${liveGames.length - inProgress} starting soon`} games={liveGames} />}
            {yours.length > 0 && <Row title="Your teams" games={yours} />}
            {byLeague.map(([l, gs]) => (
              <Row key={l.id} title={l.name} games={gs} color={sportColors[l.sport]} />
            ))}
            {day && leagues.length < allLeagues.length && (
              <p className="muted small sports-rows-note">
                {allLeagues.length - leagues.length} of {allLeagues.length} leagues are turned off.{' '}
                <button className="link-btn" onClick={() => setRowsOpen(true)}>
                  Choose rows
                </button>
              </p>
            )}
          </>
        )}

        {tab === 'teams' && (
          <>
            <div className="sports-teams-head">
              <button className={`btn ${followed.length ? 'btn-glass' : 'btn-primary'}`} onClick={() => setPicking(true)}>
                <Icon name="trophy" size={18} /> {followed.length ? 'Edit teams' : 'Follow teams'}
              </button>
            </div>
            {home && followed.length === 0 && (
              <div className="empty">
                Follow teams from any league and each gets its own row here, a star everywhere in Sports, and a heads-up before its games (Account → Playback).
              </div>
            )}
            {followed.map((t) => {
              const gs = (home?.games ?? []).filter((g) => g.league === t.league && (g.home.id === t.id || g.away.id === t.id)).sort(byState)
              return gs.length ? (
                <Row key={`${t.league}/${t.id}`} title={t.name} sub={leagueOf(t.league)?.name} games={gs} team={t} />
              ) : (
                <section key={`${t.league}/${t.id}`} className="sports-section">
                  <div className="sports-row-head">
                    <TeamLogo team={t} size={28} />
                    <h2>{t.name}</h2>
                  </div>
                  <div className="muted small">No games from two days ago to a week from now.</div>
                </section>
              )
            })}
          </>
        )}

        {tab === 'channels' && <ChannelsTab channels={channels ?? undefined} games={[...games, ...(home?.games ?? [])]} />}

        {details && (
          <GameDetails
            game={details}
            followed={followed}
            onFollow={follow}
            onWatch={details.channels?.length ? () => navigate(`/live/${details.channels![0].id}`) : undefined}
            onMultiView={details.channels?.length ? () => pick(details.channels![0].id) : undefined}
            onClose={() => setDetails(null)}
          />
        )}
        {settingsOpen && (
          <SportsSettings
            prefs={prefs ?? undefined}
            followed={followed.length}
            rows={leagues.length}
            allRows={allLeagues.length}
            onPrefs={setPref}
            onTeams={() => {
              setSettingsOpen(false)
              setPicking(true)
            }}
            onRows={() => {
              setSettingsOpen(false)
              setRowsOpen(true)
            }}
            onClose={() => setSettingsOpen(false)}
          />
        )}
        {rowsOpen && (
          <RowsEditor
            leagues={allLeagues}
            hidden={prefs?.sportsHidden ?? []}
            onSave={async (sportsOrder, sportsHidden) => {
              await patchPrefs({ sportsOrder, sportsHidden })
              reloadPrefs()
              reload()
              setRowsOpen(false)
              say('Sports rows saved.')
            }}
            onClose={() => setRowsOpen(false)}
          />
        )}
        {picking && (
          <TeamPicker
            leagues={allLeagues}
            chosen={followed}
            onClose={() => setPicking(false)}
            onSaved={() => {
              setPicking(false)
              reload()
            }}
          />
        )}
      </div>
    </HubContext.Provider>
  )
}

/** A horizontal row of games, as on the TV. */
function Row({ title, sub, games, color, team }: { title: string; sub?: string; games: SportsGame[]; color?: string; team?: SportsTeam }) {
  return (
    <section className="sports-section">
      <div className="sports-row-head" style={color ? { borderLeft: `3px solid ${color}`, paddingLeft: 10 } : undefined}>
        {team && <TeamLogo team={team} size={28} />}
        <h2>{title}</h2>
        {sub && <span className="sports-row-sub">{sub}</span>}
      </div>
      <div className="sports-row">
        {games.map((g) => (
          <GameCard key={g.id} game={g} />
        ))}
      </div>
    </section>
  )
}

/** The focused game up top: both sides with the score by period, the standouts and where it's on. */
function Featured({ game: g }: { game: SportsGame }) {
  const { spoilerFree, revealed, reveal, leagueOf, open } = useContext(HubContext)
  const l = leagueOf(g.league)
  const shown = g.state !== 'pre' && (!spoilerFree || revealed.has(g.id))
  const periods = Math.max(g.away.lines?.length ?? 0, g.home.lines?.length ?? 0)
  return (
    <div className="sports-featured panel" style={{ borderLeftColor: sportColors[l?.sport ?? ''] }}>
      <div className="sports-featured-head">
        <span className="muted small">{l?.name}</span>
        {g.state === 'in' ? (
          <span className="sports-live">
            <span className="sports-dot" /> {g.detail}
          </span>
        ) : (
          <span className="muted small">{gameStatus(g)}</span>
        )}
      </div>
      {isRace(g) ? (
        <>
          <div className="sports-featured-race">{g.name}</div>
          {shown && <div className="muted">{(g.race ?? []).slice(0, 5).map((d) => `${d.place}. ${d.shortName}`).join('   ')}</div>}
        </>
      ) : (
        <table className="sports-line">
          {periods > 0 && shown && (
            <thead>
              <tr>
                <th />
                {Array.from({ length: periods }, (_, i) => (
                  <th key={i}>{i + 1}</th>
                ))}
                <th>T</th>
              </tr>
            </thead>
          )}
          <tbody>
            {[g.away, g.home].map((s) => (
              <tr key={s.id} className={g.mine?.includes(s.id) ? 'mine' : ''}>
                <td className="sports-line-team">
                  <TeamLogo team={s} size={30} />
                  <span>
                    {s.name}
                    {s.record && <span className="muted small"> {s.record}</span>}
                  </span>
                </td>
                {shown && periods > 0 && Array.from({ length: periods }, (_, i) => <td key={i}>{s.lines?.[i] ?? ''}</td>)}
                <td className="sports-line-total">{shown ? s.score : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {shown && (g.away.leader || g.home.leader) && (
        <div className="muted small">
          {[g.away, g.home]
            .filter((s) => s.leader)
            .map((s) => `${s.abbr}: ${leaderText(s.leader)}`)
            .join('   ·   ')}
        </div>
      )}
      <div className="sports-featured-foot">
        <span className="muted small">{[g.broadcasts?.join(', '), g.state !== 'post' ? g.venue : ''].filter(Boolean).join(' · ')}</span>
        <span className="sports-channels">
          {g.channels?.[0] && g.state !== 'post' && (
            <Link to={`/live/${g.channels[0].id}`} className="btn btn-sm btn-primary">
              <Icon name="play" size={14} fill /> Watch on {g.channels[0].name}
            </Link>
          )}
          {!shown && g.state !== 'pre' && (
            <button className="btn btn-glass btn-sm" onClick={() => reveal(g.id)}>
              Show the score
            </button>
          )}
          {(hasTeams(l?.sport) || isRace(g)) && (
            <button className="btn btn-glass btn-sm" onClick={() => open(g)}>
              Box score & stats
            </button>
          )}
        </span>
      </div>
    </div>
  )
}

function TeamLogo({ team, size }: { team: Pick<SportsTeam, 'logo' | 'color'>; size: number }) {
  return team.logo ? (
    <img src={team.logo} alt="" width={size} height={size} className="sports-logo" loading="lazy" />
  ) : (
    <span className="sports-logo sports-logo-blank" style={{ width: size, height: size, background: team.color ? `#${team.color}` : undefined }} />
  )
}

/** One game in a row: click for its details, hover to show it up top. */
function GameCard({ game: g }: { game: SportsGame }) {
  const { picks, pick, spoilerFree, revealed, reveal, leagueOf, open, focus } = useContext(HubContext)
  const l = leagueOf(g.league)
  const hidden = spoilerFree && g.state !== 'pre' && !revealed.has(g.id)
  const side = (s: SportsSide) => (
    <div className={`sports-side${g.state === 'post' && s.winner ? ' winner' : ''}${g.mine?.includes(s.id) ? ' mine' : ''}`}>
      <TeamLogo team={s} size={30} />
      <span className="sports-name">
        {g.mine?.includes(s.id) && <span className="sports-star">★ </span>}
        {s.shortName || s.name}
        {s.record && <span className="muted small"> {s.record}</span>}
      </span>
      {g.state !== 'pre' && !hidden && <span className="sports-score">{s.score}</span>}
    </div>
  )
  return (
    <div
      className={`sports-card panel${g.state === 'in' ? ' live' : ''}`}
      style={{ borderLeft: `3px solid ${sportColors[l?.sport ?? ''] ?? 'var(--border)'}` }}
      onMouseEnter={() => focus(g)}
      onFocus={() => focus(g)}
    >
      <button className="sports-card-open" onClick={() => open(g)} aria-label={`Details: ${g.name}`}>
        <div className="sports-card-head">
          <span className="muted small">{l?.name ?? g.league}</span>
          {g.state === 'in' ? (
            <span className="sports-live">
              <span className="sports-dot" /> {hidden ? 'Live' : g.detail}
            </span>
          ) : (
            <span className="muted small">{g.state === 'pre' ? startLabel(g.start) : hidden ? 'Final' : g.detail}</span>
          )}
        </div>
        {isRace(g) ? (
          <>
            <div className="sports-side">
              <span className="sports-name">{g.name}</span>
            </div>
            {g.state !== 'pre' && !hidden && <div className="muted small">{(g.race ?? []).slice(0, 3).map((d) => `${d.place}. ${d.shortName}`).join('   ')}</div>}
          </>
        ) : (
          <>
            {side(g.away)}
            {side(g.home)}
          </>
        )}
        {!hidden && g.state === 'in' && (g.away.leader || g.home.leader) && (
          <div className="muted small sports-leaders">{[g.away.leader, g.home.leader].filter(Boolean).map((x) => leaderText(x)).join(' · ')}</div>
        )}
        {(g.broadcasts?.length || g.venue) && (
          <div className="muted small sports-where">{[g.broadcasts?.join(', '), g.state !== 'post' ? g.venue : ''].filter(Boolean).join(' · ')}</div>
        )}
      </button>
      {(hidden || (g.channels && g.channels.length > 0)) && (
        <div className="sports-channels">
          {g.state !== 'post' &&
            g.channels?.map((c) => (
              <Link key={c.id} to={`/live/${c.id}`} className="btn btn-glass btn-sm" title={c.title}>
                <Icon name="play" size={14} fill /> {c.name}
              </Link>
            ))}
          {g.channels?.[0] && g.state !== 'post' && <MultiViewButton channelId={g.channels[0].id} picked={picks.includes(g.channels[0].id)} onPick={pick} />}
          {hidden && (
            <button className="btn btn-glass btn-sm" onClick={() => reveal(g.id)}>
              Show score
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function MultiViewButton({ channelId, picked, onPick }: { channelId: number; picked: boolean; onPick: (id: number) => void }) {
  return (
    <button className="btn btn-glass btn-sm" onClick={() => onPick(channelId)} disabled={picked} title="Add to Multi-View">
      <Icon name={picked ? 'check' : 'plus'} size={14} /> Multi-View
    </button>
  )
}

/** Channels showing a game, then the other sports channels. */
function ChannelsTab({ channels, games }: { channels: LiveChannel[] | undefined; games: SportsGame[] }) {
  const { picks, pick, open } = useContext(HubContext)
  const onChannel = useMemo(() => gamesByChannel(games), [games])
  if (!channels) return <div className="muted">Loading channels…</div>
  const shown = channels.filter((c) => onChannel.has(c.id) || isSportsChannel(c))
  if (!shown.length) return <div className="empty">No sports channels in your Live TV lineup.</div>
  const withGames = shown.filter((c) => onChannel.has(c.id))
  const rest = shown.filter((c) => !onChannel.has(c.id))
  const grid = (list: LiveChannel[]) => (
    <div className="sports-grid">
      {list.map((c) => {
        const g = onChannel.get(c.id)
        return (
          <div key={c.id} className="sports-card panel">
            <div className="sports-card-head">
              <strong>
                {c.number} {c.name}
              </strong>
              {g?.state === 'in' && (
                <span className="sports-live">
                  <span className="sports-dot" /> Live
                </span>
              )}
            </div>
            {g ? (
              <button className="link-btn sports-where" onClick={() => open(g)}>
                {g.name || `${g.away.name} at ${g.home.name}`} · {gameStatus(g)}
              </button>
            ) : (
              <div className="muted small sports-where">{c.now?.title ?? ' '}</div>
            )}
            <div className="sports-channels">
              <Link to={`/live/${c.id}`} className="btn btn-glass btn-sm">
                <Icon name="play" size={14} fill /> Watch
              </Link>
              <MultiViewButton channelId={c.id} picked={picks.includes(c.id)} onPick={pick} />
            </div>
          </div>
        )
      })}
    </div>
  )
  return (
    <>
      {withGames.length > 0 && (
        <section className="sports-section">
          <h2>Showing games</h2>
          {grid(withGames)}
        </section>
      )}
      {rest.length > 0 && (
        <section className="sports-section">
          <h2>Sports channels</h2>
          {grid(rest)}
        </section>
      )}
    </>
  )
}

/** The gear: spoiler-free, the score bug, your teams and your rows (the TV hub's settings). */
function SportsSettings({
  prefs,
  followed,
  rows,
  allRows,
  onPrefs,
  onTeams,
  onRows,
  onClose,
}: {
  prefs: Prefs | undefined
  followed: number
  rows: number
  allRows: number
  onPrefs: (patch: Partial<Prefs>) => void
  onTeams: () => void
  onRows: () => void
  onClose: () => void
}) {
  return (
    <Modal title="Sports settings" onClose={onClose}>
      <div className="form">
        <label className="check">
          <input type="checkbox" checked={!!prefs?.spoilerFree} onChange={(e) => onPrefs({ spoilerFree: e.target.checked })} />
          Spoiler-free: hide scores, stats and the score bug until I choose to see a game's score
        </label>
        <label className="check">
          <input type="checkbox" checked={prefs?.scoreBug !== false} onChange={(e) => onPrefs({ scoreBug: e.target.checked })} />
          Score bug: my teams' scores in a corner while I watch something else
        </label>
        <div className="sports-setting-row">
          <div>
            <strong>Favorite teams: {followed}</strong>
            <div className="muted small">Follow and unfollow teams in any league.</div>
          </div>
          <button className="btn btn-glass" onClick={onTeams}>
            Edit teams
          </button>
        </div>
        <div className="sports-setting-row">
          <div>
            <strong>Sports rows: {rows === allRows ? 'all' : `${rows} of ${allRows}`}</strong>
            <div className="muted small">Choose which leagues get a row under Scores, and put your favorites first. Your TVs follow it too.</div>
          </div>
          <button className="btn btn-glass" onClick={onRows}>
            Choose rows
          </button>
        </div>
        <div className="sports-setting-row">
          <div>
            <strong>Game alerts and Game Lights</strong>
            <div className="muted small">Heads-ups before games and final scores are under Account → Playback; Hue effects under My Game Lights.</div>
          </div>
          <Link className="btn btn-glass" to="/account">
            Account
          </Link>
        </div>
        <div className="actions">
          <button className="btn btn-primary" onClick={onClose}>
            Done
          </button>
        </div>
      </div>
    </Modal>
  )
}

/** This profile's rows: which leagues show under Scores and in what order. */
function RowsEditor({
  leagues,
  hidden: hiddenAtStart,
  onSave,
  onClose,
}: {
  leagues: SportsLeague[]
  hidden: string[]
  onSave: (order: string[], hidden: string[]) => Promise<void>
  onClose: () => void
}) {
  const [order, setOrder] = useState(leagues)
  const [hidden, setHidden] = useState(() => new Set(hiddenAtStart))
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const { user } = useAuth()
  const move = (i: number, by: number) => {
    const j = i + by
    if (j < 0 || j >= order.length) return
    const next = [...order]
    ;[next[i], next[j]] = [next[j], next[i]]
    setOrder(next)
  }
  const toggle = (id: string) => {
    const next = new Set(hidden)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setHidden(next)
  }
  const save = async (o: string[], h: string[]) => {
    setSaving(true)
    setError('')
    try {
      await onSave(o, h)
    } catch (e) {
      setError((e as Error).message)
      setSaving(false)
    }
  }
  return (
    <Modal title="Sports rows" onClose={onClose}>
      <div className="form">
        <p className="muted small">
          Tick the leagues that get a row under Scores and move your favorites up. Your teams' games always show under Your teams. This is just for{' '}
          {user?.profileName ?? 'you'}; the TV and Roku apps use it too.
          {user?.isAdmin && ' Leagues taken out for everyone are under Server → Settings → Sports data.'}
        </p>
        <ol className="sports-rows-list">
          {order.map((l, i) => (
            <li key={l.id} style={{ borderLeftColor: sportColors[l.sport] }}>
              <label className="check">
                <input type="checkbox" checked={!hidden.has(l.id)} onChange={() => toggle(l.id)} /> {l.name}
              </label>
              <button type="button" className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label={`Move ${l.name} up`}>
                <Icon name="back" size={14} className="rot-up" />
              </button>
              <button type="button" className="icon-btn" onClick={() => move(i, 1)} disabled={i === order.length - 1} aria-label={`Move ${l.name} down`}>
                <Icon name="back" size={14} className="rot-down" />
              </button>
            </li>
          ))}
        </ol>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button
            type="button"
            className="btn btn-primary"
            disabled={saving}
            onClick={() => save(order.map((l) => l.id), order.filter((l) => hidden.has(l.id)).map((l) => l.id))}
          >
            Save rows
          </button>
          <button type="button" className="btn btn-glass" disabled={saving} onClick={() => save([], [])}>
            Usual order, all on
          </button>
          <button type="button" className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}

type DetailTab = 'box' | 'stats' | 'leaders' | 'standings'
const detailNames: Record<DetailTab, string> = { box: 'Box score', stats: 'Stats', leaders: 'Leaders', standings: 'Standings' }

/** A game's box score, team and player stats, leaders and standings, with Follow, Watch and Multi-View. */
function GameDetails({
  game,
  followed,
  onFollow,
  onWatch,
  onMultiView,
  onClose,
}: {
  game: SportsGame
  followed: SportsTeam[]
  onFollow: (team: SportsSide, follow: boolean) => void
  onWatch?: () => void
  onMultiView?: () => void
  onClose: () => void
}) {
  const { spoilerFree, revealed, reveal, leagueOf } = useContext(HubContext)
  const l = leagueOf(game.league)
  const sport = l?.sport
  const fights = !hasTeams(sport)
  const racing = sport === 'racing'
  const [tab, setTab] = useState<DetailTab>('box')
  const [sum, setSum] = useState<SportsSummary | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (fights) return
    let on = true
    const load = () =>
      api.get<SportsSummary>(`/api/sports/${game.league}/games/${encodeURIComponent(game.id)}`).then(
        (s) => on && setSum(s),
        (e) => on && setError((e as Error).message),
      )
    load()
    const t = game.state === 'in' ? window.setInterval(load, 20000) : undefined
    return () => {
      on = false
      if (t) window.clearInterval(t)
    }
  }, [game.league, game.id, game.state, fights])
  const g = sum?.game?.id ? { ...game, ...sum.game, channels: game.channels, mine: game.mine } : game
  const shown = g.state !== 'pre' && (!spoilerFree || revealed.has(g.id))
  const hidden = !shown && g.state !== 'pre' && tab !== 'standings'
  const isFollowed = (s: SportsSide) => followed.some((t) => t.league === g.league && t.id === s.id)
  const title = racing || isRace(g) ? g.name : `${g.away.name} at ${g.home.name}`
  const sub = [gameStatus(g), shown && !racing ? `${g.away.abbr || g.away.shortName} ${g.away.score ?? ''} · ${g.home.abbr || g.home.shortName} ${g.home.score ?? ''}` : '', l?.name]
    .filter(Boolean)
    .join(' · ')
  return (
    <Modal title={title} onClose={onClose} wide>
      <div className="sports-details">
        <div className="muted">{sub}</div>
        <div className="sports-details-buttons">
          {!fights &&
            [g.away, g.home].map((s) => (
              <button key={s.id} className="btn btn-glass btn-sm" onClick={() => onFollow(s, !isFollowed(s))}>
                <Icon name="star" size={14} fill={isFollowed(s)} /> {isFollowed(s) ? 'Following' : 'Follow'} {s.shortName || s.name}
              </button>
            ))}
          {onWatch && g.state !== 'post' && (
            <button className="btn btn-primary btn-sm" onClick={onWatch}>
              <Icon name="play" size={14} fill /> Watch
            </button>
          )}
          {onMultiView && g.state !== 'post' && (
            <button className="btn btn-glass btn-sm" onClick={onMultiView}>
              <Icon name="plus" size={14} /> Multi-View
            </button>
          )}
          {!shown && g.state !== 'pre' && (
            <button className="btn btn-glass btn-sm" onClick={() => reveal(g.id)}>
              Show the score
            </button>
          )}
        </div>
        {!fights && (
          <div className="sports-view-tabs small" role="tablist">
            {(Object.keys(detailNames) as DetailTab[]).map((t) => (
              <button key={t} role="tab" aria-selected={tab === t} className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
                {detailNames[t]}
              </button>
            ))}
          </div>
        )}
        <div className="sports-details-body">
          {racing || isRace(g) ? (
            g.state === 'pre' ? (
              <div className="muted">{[g.detail, g.venue, g.broadcasts?.join(', ')].filter(Boolean).join(' · ')}</div>
            ) : !shown ? (
              <div className="muted">Spoiler-free: the running order stays hidden until you choose Show the score.</div>
            ) : (
              <StatTable labels={['#']} rows={(g.race ?? []).map((d) => ({ key: String(d.place), name: `${d.place}. ${d.name}`, stats: [d.number ?? ''], strong: d.place === 1 }))} />
            )
          ) : fights ? (
            <div>
              {[g.away, g.home].map((s) => (
                <div key={s.id} className="sports-side">
                  <TeamLogo team={s} size={36} />
                  <span className="sports-name">
                    {s.name}
                    {s.record && <span className="muted small"> {s.record}</span>}
                    {shown && s.winner ? '  ✓ Won' : ''}
                  </span>
                </div>
              ))}
              <div className="muted small">{[g.detail, g.venue, g.broadcasts?.join(', ')].filter(Boolean).join(' · ')}</div>
            </div>
          ) : hidden ? (
            <div className="muted">Spoiler-free: the score, stats and leaders stay hidden until you choose Show the score.</div>
          ) : !sum ? (
            error ? <div className="form-error">{error}</div> : <div className="muted">Loading…</div>
          ) : tab === 'box' ? (
            <BoxScore sum={sum} game={g} sport={sport} />
          ) : tab === 'stats' ? (
            sum.stats.length ? (
              <StatTable labels={[g.away.abbr || g.away.shortName, g.home.abbr || g.home.shortName]} rows={sum.stats.map((s) => ({ key: s.label, name: s.label, stats: [s.away, s.home] }))} />
            ) : (
              <div className="muted">No team stats yet.</div>
            )
          ) : tab === 'leaders' ? (
            sum.leaders.length ? (
              <div className="sports-leader-grid">
                {sum.leaders.map((x) => (
                  <div key={x.title} className="panel">
                    <div className="muted small sports-leader-title">{x.title}</div>
                    <div>
                      <strong>{g.away.abbr}</strong> {leaderText(x.away, true) || '—'}
                    </div>
                    <div>
                      <strong>{g.home.abbr}</strong> {leaderText(x.home, true) || '—'}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="muted">No leaders yet.</div>
            )
          ) : sum.standings.length ? (
            sum.standings.map((t) => (
              <StatTable
                key={t.title}
                title={t.title}
                labels={t.labels}
                rows={t.rows.map((r) => ({ key: r.id || r.name, name: r.name, stats: r.stats, strong: r.id === g.home.id || r.id === g.away.id }))}
              />
            ))
          ) : (
            <div className="muted">No standings for this league.</div>
          )}
        </div>
      </div>
    </Modal>
  )
}

function BoxScore({ sum, game: g, sport }: { sum: SportsSummary; game: SportsGame; sport?: string }) {
  // Baseball always shows nine innings, filling in as they're played.
  const periods = Math.max(g.away.lines?.length ?? 0, g.home.lines?.length ?? 0, sport === 'baseball' ? 9 : 0)
  return (
    <>
      <table className="sports-line">
        <thead>
          <tr>
            <th />
            {Array.from({ length: periods }, (_, i) => (
              <th key={i}>{i + 1}</th>
            ))}
            <th>{sport === 'baseball' ? 'R' : 'T'}</th>
          </tr>
        </thead>
        <tbody>
          {[g.away, g.home].map((s) => (
            <tr key={s.id}>
              <td className="sports-line-team">
                <TeamLogo team={s} size={28} />
                <span>{s.name}</span>
              </td>
              {Array.from({ length: periods }, (_, i) => (
                <td key={i}>{s.lines?.[i] ?? ''}</td>
              ))}
              <td className="sports-line-total">{s.score ?? ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {sum.players.map((p, i) => (
        <StatTable
          key={i}
          title={`${(p.team === g.home.id ? g.home : g.away).shortName || ''} ${p.title}`}
          labels={p.labels}
          rows={p.rows.map((r) => ({ key: r.name + i, name: r.position ? `${r.name} · ${r.position}` : r.name, stats: r.stats }))}
        />
      ))}
    </>
  )
}

function StatTable({ title, labels, rows }: { title?: string; labels: string[]; rows: { key: string; name: string; stats: string[]; strong?: boolean }[] }) {
  return (
    <div className="sports-table-wrap">
      <table className="sports-table">
        <thead>
          <tr>
            <th>{title ?? ''}</th>
            {labels.map((x, i) => (
              <th key={i}>{x}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.key} className={r.strong ? 'strong' : ''}>
              <td>{r.name}</td>
              {r.stats.map((v, i) => (
                <td key={i}>{v}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function TeamPicker({ leagues, chosen, onClose, onSaved }: { leagues: SportsLeague[]; chosen: SportsTeam[]; onClose: () => void; onSaved: () => void }) {
  const { user } = useAuth()
  const pickable = useMemo(() => leagues.filter((l) => hasTeams(l.sport)), [leagues]) // fighters and drivers, no teams
  const [league, setLeague] = useState(chosen[0]?.league ?? pickable[0]?.id ?? '')
  const [picked, setPicked] = useState<SportsTeam[]>(chosen)
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const { data: teams, error: loadError } = useApi<SportsTeam[]>(league ? `/api/sports/${league}/teams` : null)
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
        <div className="sports-picker-bar">
          <select value={league} onChange={(e) => setLeague(e.target.value)} aria-label="League">
            {pickable.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search teams" autoFocus />
        </div>
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
