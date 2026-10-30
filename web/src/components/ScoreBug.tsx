import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Prefs, SportsGame, SportsSide } from '../types'

/** The score of each of your teams' games that's on, in the player's top
 * left corner. Hidden on the channel showing the game (it has its own) and
 * when turned off under Account → Alerts. */
export function ScoreBug({ channelId }: { channelId?: number }) {
  const [games, setGames] = useState<SportsGame[]>([])
  useEffect(() => {
    let on = true
    const look = async () => {
      try {
        const p = await api.get<Prefs>('/api/me/prefs')
        const g = p.scoreBug === false || p.spoilerFree ? [] : (await api.get<{ games: SportsGame[] }>(`/api/sports/live${channelId ? `?channel=${channelId}` : ''}`)).games
        if (on) setGames(g)
      } catch {
        // No sports, or offline: just no bug.
      }
    }
    look()
    const t = window.setInterval(look, 30000)
    return () => {
      on = false
      window.clearInterval(t)
    }
  }, [channelId])
  const shown = games.filter((g) => !g.channels?.some((c) => c.id === channelId)).slice(0, 2)
  if (shown.length === 0) return null
  return (
    <div className="score-bug" aria-label="Your teams' scores">
      {shown.map((g) => (
        <div key={g.id} className="score-bug-game" title={g.name}>
          <BugSide side={g.away} />
          <BugSide side={g.home} />
          <span className="score-bug-detail">{g.detail}</span>
        </div>
      ))}
    </div>
  )
}

function BugSide({ side }: { side: SportsSide }) {
  return (
    <span className="score-bug-side" style={side.color ? { borderLeftColor: `#${side.color}` } : undefined}>
      {side.logo && <img src={side.logo} alt="" />}
      <b>{side.abbr}</b>
      <strong>{side.score ?? '0'}</strong>
    </span>
  )
}
