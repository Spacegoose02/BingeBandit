import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useDocumentTitle } from '../hooks'
import { useLiveStream } from '../liveStream'
import { MAX_SCREENS, byState, gamesByChannel, isSportsChannel, matchup, multiViewUrl, scoreLine, setMultiViewPicks } from '../sportsHub'
import type { LiveChannel, Prefs, SportsGame, SportsHome } from '../types'

type Tile = number | null // a channel id, or an empty screen
type Layout = { name: string; channels: number[] }

/**
 * Sports Multi-View: up to four Live TV channels at once, like the TV app's.
 * The screen with the sound has a speaker; clicking a screen (OK on a
 * remote) moves the sound there, double-clicking goes full screen, and its
 * ⋯ button (the menu key) has the rest. The toolbar sets how many screens,
 * finds the next game of your teams and keeps layouts, which follow the profile.
 */
export default function MultiView() {
  useDocumentTitle('Multi-View')
  const nav = useNavigate()
  const [params] = useSearchParams()
  const [tiles, setTiles] = useState<Tile[]>(() => {
    const ids = (params.get('ch') ?? '').split(',').map(Number).filter((n) => n > 0).slice(0, MAX_SCREENS)
    return ids.length ? ids : [null, null]
  })
  const [audio, setAudio] = useState(0)
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [today, setToday] = useState<SportsGame[]>([])
  const [mine, setMine] = useState<SportsGame[]>([])
  const [prefs, setPrefs] = useState<Prefs | null>(null)
  const [picker, setPicker] = useState<number | null>(null)
  const [options, setOptions] = useState<number | null>(null)
  const [layoutsOpen, setLayoutsOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const say = useCallback((t: string) => {
    setNotice(t)
    window.setTimeout(() => setNotice((n) => (n === t ? '' : n)), 4000)
  }, [])

  useEffect(() => {
    api.get<LiveChannel[]>('/api/live/channels').then(setChannels, () => undefined)
    api.get<Prefs>('/api/me/prefs').then(setPrefs, () => undefined)
    // Scores and where games are on; ESPN being away only loses the labels.
    const load = () => {
      api.get<{ games: SportsGame[] }>('/api/sports/scores').then((d) => setToday(d.games ?? []), () => undefined)
      api.get<SportsHome>('/api/sports').then((h) => setMine(h.games ?? []), () => undefined)
    }
    load()
    const t = window.setInterval(load, 30000)
    return () => window.clearInterval(t)
  }, [])

  // The address and the Sports page's picks follow the screens.
  useEffect(() => {
    const ids = tiles.filter((t): t is number => t !== null)
    setMultiViewPicks(ids)
    const url = multiViewUrl(ids)
    if (location.pathname + location.search !== url) nav(url, { replace: true })
  }, [tiles, nav])

  const channel = (id: Tile) => channels.find((c) => c.id === id)
  const gameOn = useMemo(() => gamesByChannel(today, mine), [today, mine])
  const layouts: Layout[] = prefs?.multiViewLayouts ?? []

  const put = (i: number, id: number) => {
    setTiles((ts) => ts.map((t, j) => (j === i ? id : t === id ? null : t)))
    setAudio(i)
  }
  const setCount = (n: number) => {
    setTiles((ts) => (n > ts.length ? [...ts, ...Array<Tile>(n - ts.length).fill(null)] : ts.slice(0, n)))
    setAudio((a) => Math.min(a, n - 1))
  }
  const remove = (i: number) => {
    if (tiles.length === 1) return setTiles([null])
    setTiles((ts) => ts.filter((_, j) => j !== i))
    setAudio((a) => (a === i ? 0 : a > i ? a - 1 : a))
  }
  const fullScreen = (i: number) => {
    const id = tiles[i]
    // Replaced, not pushed, so the other screens stop streaming.
    if (id) nav(`/live/${id}`, { replace: true })
  }

  // One press: the next game of your teams that's on a channel and not up yet.
  const nextGame = () => {
    const g = mine.filter((x) => x.state !== 'post' && x.channels?.length && !x.channels.some((c) => tiles.includes(c.id))).sort(byState)[0]
    if (!g) return say("None of your teams' games is on your channels right now.")
    const id = g.channels![0].id
    const empty = tiles.indexOf(null)
    if (empty >= 0) put(empty, id)
    else if (tiles.length < MAX_SCREENS) {
      setTiles((ts) => [...ts, id])
      setAudio(tiles.length)
    } else put(audio, id)
    say(`${matchup(g)} on ${g.channels![0].name}`)
  }

  const savePrefs = async (next: Layout[]) => {
    try {
      const cur = await api.get<Prefs>('/api/me/prefs')
      setPrefs(await api.put<Prefs>('/api/me/prefs', { ...cur, multiViewLayouts: next }))
      return true
    } catch (e) {
      say((e as Error).message)
      return false
    }
  }
  const saveLayout = async () => {
    const ids = tiles.filter((t): t is number => !!t)
    if (!ids.length) return say('Pick a channel first.')
    const name = ids.map((id) => channel(id)?.name ?? `${id}`).join(' · ').slice(0, 60)
    if (await savePrefs([...layouts.filter((l) => l.name !== name), { name, channels: ids }].slice(-12))) say(`Saved "${name}".`)
  }

  // Escape (Back) leaves, unless a dialog is open; TV mode already goes back itself.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('[role="dialog"]')) nav(-1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [nav])

  const n = tiles.length
  return (
    <div className="multiview">
      <div className="mv-toolbar">
        <div className="segmented" role="group" aria-label="Screens">
          {[1, 2, 3, 4].map((k) => (
            <button key={k} className={k === n ? 'active' : ''} onClick={() => setCount(k)}>
              {k}
            </button>
          ))}
        </div>
        <button className="btn btn-glass btn-sm" onClick={nextGame}>
          <Icon name="play" size={14} fill /> Next game of my teams
        </button>
        <button className="btn btn-glass btn-sm" onClick={saveLayout}>
          Save layout
        </button>
        <button className="btn btn-glass btn-sm" onClick={() => setLayoutsOpen(true)}>
          Layouts ({layouts.length})
        </button>
        <span className="mv-spacer" />
        {notice && <span className="mv-notice">{notice}</span>}
        <button className="btn btn-glass btn-sm" onClick={() => nav(-1)}>
          Exit
        </button>
      </div>
      <div className={`mv-grid mv-${n}`}>
        {tiles.map((id, i) => (
          <Screen
            key={i}
            channelId={id}
            channel={channel(id)}
            game={id ? gameOn.get(id) : undefined}
            spoilerFree={!!prefs?.spoilerFree}
            audio={audio === i}
            first={i === 0}
            onPress={() => (id ? setAudio(i) : setPicker(i))}
            onDouble={() => fullScreen(i)}
            onOptions={() => (id ? setOptions(i) : setPicker(i))}
          />
        ))}
      </div>

      {options !== null && (
        <Modal title={channel(tiles[options])?.name ?? 'Screen'} onClose={() => setOptions(null)}>
          <div className="mv-menu">
            <button className="btn btn-glass" autoFocus onClick={() => (setPicker(options), setOptions(null))}>
              Change channel
            </button>
            <button className="btn btn-glass" onClick={() => (setAudio(options), setOptions(null))}>
              Listen to this one
            </button>
            <button className="btn btn-glass" onClick={() => fullScreen(options)}>
              Full screen
            </button>
            <button className="btn btn-glass" onClick={() => (remove(options), setOptions(null))}>
              Remove this screen
            </button>
          </div>
        </Modal>
      )}
      {picker !== null && (
        <ChannelPicker
          channels={channels}
          gameOn={gameOn}
          mine={mine}
          onPick={(id) => {
            put(picker, id)
            setPicker(null)
          }}
          onClose={() => setPicker(null)}
        />
      )}
      {layoutsOpen && (
        <Modal title="Saved layouts" onClose={() => setLayoutsOpen(false)}>
          {layouts.length === 0 && <div className="muted">Nothing saved yet. Set up the screens, then Save layout in the toolbar.</div>}
          <div className="mv-menu">
            {layouts.map((l, i) => (
              <div key={l.name} className="mv-layout">
                <button
                  className="btn btn-glass"
                  autoFocus={i === 0}
                  onClick={() => {
                    setTiles(l.channels.slice(0, MAX_SCREENS))
                    setAudio(0)
                    setLayoutsOpen(false)
                  }}
                >
                  {l.name} ({l.channels.length})
                </button>
                <button className="btn btn-glass danger" aria-label={`Delete ${l.name}`} onClick={() => savePrefs(layouts.filter((x) => x.name !== l.name))}>
                  <Icon name="trash" size={16} />
                </button>
              </div>
            ))}
          </div>
        </Modal>
      )}
    </div>
  )
}

/** One screen: its channel's stream, muted unless it has the sound, labelled with the channel and the game on it. */
function Screen({
  channelId,
  channel,
  game,
  spoilerFree,
  audio,
  first,
  onPress,
  onDouble,
  onOptions,
}: {
  channelId: Tile
  channel?: LiveChannel
  game?: SportsGame
  spoilerFree: boolean
  audio: boolean
  first: boolean
  onPress: () => void
  onDouble: () => void
  onOptions: () => void
}) {
  const video = useRef<HTMLVideoElement>(null)
  const error = useLiveStream(channelId, video)
  const [playing, setPlaying] = useState(false)
  useEffect(() => setPlaying(false), [channelId])
  const score = game && game.state !== 'pre' && !spoilerFree ? scoreLine(game) : ''
  const label = channel ? `${channel.number} ${channel.name}` : 'Empty screen'
  return (
    <div className={`mv-tile has-menu${audio && channelId ? ' audio' : ''}`}>
      <video ref={video} muted={!audio} playsInline onPlaying={() => setPlaying(true)} />
      <button className="mv-hit" data-tv-start={first || undefined} onClick={onPress} onDoubleClick={onDouble} aria-label={channelId ? `${label}${audio ? ', playing sound' : ', listen'}` : 'Pick a channel'}>
        {!channelId && <span className="mv-empty">Pick a channel</span>}
        {channelId && !playing && !error && <span className="spinner" />}
        {error && <span className="mv-empty">{error}</span>}
      </button>
      {channel && <div className="mv-label">{label}</div>}
      {(score || channel?.now?.title) && <div className="mv-bottom">{score || channel?.now?.title}</div>}
      {audio && channelId && (
        <span className="mv-speaker" aria-hidden>
          <Icon name="volume" size={18} />
        </span>
      )}
      <button className="mv-options" data-menu onClick={onOptions} aria-label={`Options for ${label}`}>
        <Icon name="dots" size={18} />
      </button>
    </div>
  )
}

/** Your teams' games on your channels, then other games on now, then the sports channels; typing searches them all. */
function ChannelPicker({
  channels,
  gameOn,
  mine,
  onPick,
  onClose,
}: {
  channels: LiveChannel[]
  gameOn: Map<number, SportsGame>
  mine: SportsGame[]
  onPick: (id: number) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()
  const mineIds = new Set(mine.filter((g) => g.state !== 'post').flatMap((g) => g.channels?.map((c) => c.id) ?? []))
  const sections: [string, LiveChannel[]][] = q
    ? [['Channels', channels.filter((c) => `${c.number} ${c.name} ${c.now?.title ?? ''}`.toLowerCase().includes(q)).slice(0, 60)]]
    : [
        ['Your teams', channels.filter((c) => mineIds.has(c.id))],
        ['Games on now', channels.filter((c) => !mineIds.has(c.id) && gameOn.get(c.id)?.state === 'in')],
        ['Sports channels', channels.filter((c) => !mineIds.has(c.id) && gameOn.get(c.id)?.state !== 'in' && isSportsChannel(c))],
      ]
  return (
    <Modal title="Pick a channel" onClose={onClose} wide>
      <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search all channels" autoFocus />
      {sections.every(([, list]) => list.length === 0) && <div className="muted">{q ? 'No channel matches.' : 'No sports channels or games on now. Search for any channel above.'}</div>}
      {sections.map(
        ([title, list]) =>
          list.length > 0 && (
            <section key={title} className="mv-pick-section">
              <h3>{title}</h3>
              <div className="mv-pick-list">
                {list.map((c) => {
                  const g = gameOn.get(c.id)
                  return (
                    <button key={c.id} className="mv-pick" onClick={() => onPick(c.id)}>
                      <strong>
                        {c.number} {c.name}
                      </strong>
                      <span className="muted small">{g ? matchup(g) : c.now?.title ?? ''}</span>
                    </button>
                  )
                })}
              </div>
            </section>
          ),
      )}
    </Modal>
  )
}
