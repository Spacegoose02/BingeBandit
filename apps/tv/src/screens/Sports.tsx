import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ActivityIndicator, FlatList, Modal, Pressable, ScrollView, StyleSheet, Text, View, useTVEventHandler } from 'react-native'
import Video from 'react-native-video'
import { api, authHeaders } from '../api'
import { refreshGameLights } from '../hue'
import { fetchGameLightsSetup, saveGameLights, type GameLightsSetup } from '../lights'
import { Button, Center, Focusable } from '../components'
import { backInterceptor, useNav, useScreen } from '../nav'
import { refreshScoreBug } from '../scorebug'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { LiveChannel, SportsDay, SportsGame, SportsHome, SportsLeague, SportsSide, SportsTeam } from '../types'
import GameDetails from '../sports/GameDetails'
import GameLights from '../sports/GameLights'
import TeamPicker from '../sports/TeamPicker'
import { ChannelCard, GameCard, Logo, leaderText } from '../sports/parts'
import {
  MAX_SCREENS,
  byState,
  dayKey,
  dayLabel,
  isSportsChannel,
  loadSportsPrefs,
  multiViewPicks,
  patchPrefs,
  pickForMultiView,
  scoreShown,
  sportColors,
  sportNames,
  startingSoon,
  useSpoilers,
  type SportsPrefs,
} from '../sports/hub'
import { usePreview } from './LiveGuide'
import { NO_LIVE_BADGE } from './Player'

type View3 = 'scores' | 'teams' | 'channels'
const viewNames: Record<View3, string> = { scores: 'Scores', teams: 'My Teams', channels: 'Channels' }
const DAYS = [-3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7]

/**
 * The Sports Hub: every league's scores and schedule by day, your teams'
 * own rows, and the sports channels. OK on a game that's on one of your
 * channels previews it in the corner and OK again goes full screen; holding
 * OK opens its box score, stats, leaders and standings; PLAY adds it to
 * Multi-View. The gear has spoiler-free mode, your teams and Game Lights.
 */
export default function Sports() {
  const { active } = useScreen()
  const nav = useNav()
  useSpoilers()
  const [view, setView] = useState<View3>('scores')
  const [offset, setOffset] = useState(0)
  const [day, setDay] = useState<SportsDay | null>(null)
  const [home, setHome] = useState<SportsHome | null>(null)
  const [channels, setChannels] = useState<LiveChannel[] | null>(null)
  const [prefs, setPrefs] = useState<SportsPrefs>({})
  const [error, setError] = useState('')
  const [sport, setSport] = useState('all')
  const [withChannel, setWithChannel] = useState(false)
  const [focused, setFocused] = useState<SportsGame | null>(null)
  const [preview, setPreview] = useState<SportsGame | null>(null)
  const [details, setDetails] = useState<SportsGame | null>(null)
  const [picking, setPicking] = useState(false)
  const [settings, setSettings] = useState(false)
  const [dayMenu, setDayMenu] = useState(false)
  const [lightsPanel, setLightsPanel] = useState(false)
  // Game Lights as Settings → This TV keeps it: null when the plugin isn't running.
  const [lights, setLights] = useState<GameLightsSetup | null>(null)
  const [notice, setNotice] = useState('')
  const [picks, setPicks] = useState(multiViewPicks.length)
  const focusedRef = useRef<SportsGame | null>(null)
  const say = useCallback((t: string) => {
    setNotice(t)
    setTimeout(() => setNotice((n) => (n === t ? '' : n)), 4000)
  }, [])

  useEffect(() => {
    loadSportsPrefs().then(setPrefs, () => undefined)
    fetchGameLightsSetup().then(setLights)
  }, [])
  const load = useCallback(() => {
    api.get<SportsDay>(`/api/sports/scores?date=${dayKey(offset)}`).then(
      (d) => {
        setDay(d)
        setError(d.error ?? '')
      },
      (e) => setError(e.message),
    )
    api.get<SportsHome>('/api/sports').then(setHome, () => undefined)
  }, [offset])
  // Scores refresh every half minute while the hub is on screen.
  useEffect(() => {
    if (!active || picking) return
    load()
    setPicks(multiViewPicks.length)
    const t = setInterval(load, 30000)
    return () => clearInterval(t)
  }, [active, picking, load])
  useEffect(() => {
    if (view === 'channels' && !channels) api.get<LiveChannel[]>('/api/live/channels').then(setChannels, () => setChannels([]))
  }, [view, channels])

  // Back stops the corner preview before it leaves the hub.
  useEffect(() => {
    if (!active || !preview) return
    const mine = () => {
      setPreview(null)
      return true
    }
    backInterceptor.current = mine
    return () => {
      if (backInterceptor.current === mine) backInterceptor.current = null
    }
  }, [active, preview])

  const addToMultiView = useCallback(
    (channelId?: number) => {
      if (!channelId) return say("That game isn't on any of your channels.")
      if (multiViewPicks.includes(channelId)) return say('Already in Multi-View.')
      if (!pickForMultiView(channelId)) return say(`Multi-View is full: ${MAX_SCREENS} screens.`)
      setPicks(multiViewPicks.length)
      say(`Added to Multi-View (${multiViewPicks.length} of ${MAX_SCREENS}). The grid button up top opens it.`)
    },
    [say],
  )
  // PLAY on a focused game adds it to Multi-View.
  useTVEventHandler((evt) => {
    if (!active || details || settings) return
    if (evt?.eventType === 'playPause' && focusedRef.current) addToMultiView(focusedRef.current.channels?.[0]?.id)
  })

  const previewSrc = usePreview(active && preview?.channels?.length ? preview.channels[0].id : undefined)
  const leagues = useMemo(() => day?.leagues ?? home?.leagues ?? [], [day, home])
  const leagueOf = useCallback((id: string) => leagues.find((l) => l.id === id), [leagues])
  const followed = home?.teams ?? day?.teams ?? []

  const press = (g: SportsGame) => {
    const ch = g.channels?.[0]
    if (!ch) return setDetails(g)
    if (preview?.id === g.id) nav.push({ name: 'live', channelId: ch.id })
    else setPreview(g)
  }
  const follow = async (s: SportsSide, on: boolean) => {
    const teams = on ? [...followed, s] : followed.filter((t) => !(t.league === s.league && t.id === s.id))
    try {
      const saved = await api.put<SportsTeam[]>('/api/sports/teams', teams.map((t) => ({ league: t.league, id: t.id })))
      setHome((h) => (h ? { ...h, teams: saved } : h))
      say(on ? `Following ${s.name}.` : `Stopped following ${s.name}.`)
      load()
    } catch (e) {
      say((e as Error).message)
    }
  }
  // The same switches as Settings → This TV: on or off for the profile, and
  // while on, running or stopped for a while.
  const saveLights = (person: GameLightsSetup['person'], done: string) => {
    if (!lights) return
    const was = lights
    setLights({ ...lights, person })
    saveGameLights(person).then(
      () => {
        refreshGameLights()
        say(done)
      },
      (e: Error) => {
        setLights(was)
        say(e.message)
      },
    )
  }
  const switchLights = () => lights && saveLights({ ...lights.person, enabled: !lights.person.enabled, off: false }, lights.person.enabled ? 'Game Lights off.' : 'Game Lights on.')
  const toggleLights = () => lights && saveLights({ ...lights.person, off: !lights.person.off }, lights.person.off ? 'Game Lights running.' : 'Game Lights stopped.')

  if (picking && (day || home)) {
    return (
      <TeamPicker
        leagues={leagues}
        chosen={followed}
        onDone={(teams) => {
          setPicking(false)
          if (teams) setHome((h) => (h ? { ...h, teams } : h))
        }}
      />
    )
  }
  if (!day && !home) return <Center>{error ? <Text style={st.muted}>{error}</Text> : <ActivityIndicator color={colors.accent} size="large" />}</Center>

  const card = (g: SportsGame, i: number, first?: boolean) => {
    const l = leagueOf(g.league)
    return (
      <GameCard
        game={g}
        league={l?.name ?? g.league}
        sport={l?.sport}
        previewing={preview?.id === g.id}
        preferFocus={first && i === 0}
        onPress={() => press(g)}
        onLongPress={() => setDetails(g)}
        onFocus={() => {
          focusedRef.current = g
          setFocused(g)
        }}
      />
    )
  }
  const row = (key: string, title: string, games: SportsGame[], first: boolean, extra?: React.ReactNode, sub?: string) => (
    <View key={key} style={st.section}>
      <View style={st.rowHead}>
        {extra}
        <Text style={st.sectionTitle}>{title}</Text>
        {!!sub && <Text style={st.sectionSub}>{sub}</Text>}
      </View>
      <FlatList
        horizontal
        data={games}
        keyExtractor={(g) => g.id}
        renderItem={({ item, index }) => card(item, index, first)}
        initialNumToRender={5}
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={st.row}
      />
    </View>
  )

  // Scores: live and starting soon, your teams, then each league.
  const sportOf = (g: SportsGame) => leagueOf(g.league)?.sport ?? ''
  const all = (day?.games ?? []).filter((g) => (sport === 'all' || sportOf(g) === sport) && (!withChannel || g.channels?.length))
  const sports = Array.from(new Set((day?.games ?? []).map(sportOf))).filter(Boolean)
  const mineFirst = (a: SportsGame, b: SportsGame) => (b.mine?.length ? 1 : 0) - (a.mine?.length ? 1 : 0) || byState(a, b)
  const liveGames = all.filter((g) => g.state === 'in' || startingSoon(g)).sort(mineFirst)
  const inProgress = all.filter((g) => g.state === 'in').length
  const yours = all.filter((g) => g.mine?.length).sort(byState)
  const byLeague = leagues.map((l) => [l, all.filter((g) => g.league === l.id).sort(byState)] as [SportsLeague, SportsGame[]]).filter(([, gs]) => gs.length)

  const featured = focused ?? preview ?? liveGames[0] ?? all[0]
  let firstRow = true
  const takeFirst = () => {
    const f = firstRow
    firstRow = false
    return f
  }

  return (
    <View style={st.flex}>
      <ScrollView contentContainerStyle={st.screen}>
        <View style={st.head}>
          <Text style={st.title}>Sports Hub</Text>
          {(Object.keys(viewNames) as View3[]).map((v) => (
            <Focusable key={v} onPress={() => setView(v)} style={StyleSheet.flatten([st.viewTab, view === v && st.viewTabOn])}>
              <Text style={[st.viewTabText, view === v && st.viewTabTextOn]}>{viewNames[v].toUpperCase()}</Text>
            </Focusable>
          ))}
          <View style={st.flex} />
          {view === 'scores' && (
            <Focusable onPress={() => setDayMenu(true)} style={st.dayButton}>
              <Text style={st.dayText}>📅 {dayLabel(offset)} ▾</Text>
            </Focusable>
          )}
          <IconButton label="⟳" onPress={load} />
          <IconButton label={picks ? `▦ ${picks}` : '▦'} onPress={() => nav.push({ name: 'multiview', channels: [...multiViewPicks] })} />
          <IconButton label="⚙" onPress={() => setSettings(true)} />
        </View>

        <View style={st.top}>
          <Pressable style={st.preview} focusable={false} onPress={preview?.channels?.length ? () => nav.push({ name: 'live', channelId: preview.channels![0].id }) : undefined}>
            {preview && previewSrc ? (
              <View style={StyleSheet.absoluteFill} pointerEvents="none">
                <Video
                  key={previewSrc}
                  source={{ uri: previewSrc, type: 'm3u8', headers: authHeaders() }}
                  style={StyleSheet.absoluteFill}
                  resizeMode="cover"
                  muted={isPhone}
                  controlsStyles={NO_LIVE_BADGE}
                  focusable={false}
                />
              </View>
            ) : preview ? (
              <ActivityIndicator color={colors.accent} />
            ) : (
              <>
                <Text style={st.previewIcon}>📺</Text>
                <Text style={st.previewText}>{isPhone ? 'Tap a game to preview' : 'Press OK on a game to preview'}</Text>
              </>
            )}
            {preview && (
              <Text style={st.previewTag} numberOfLines={1}>
                {preview.channels?.[0]?.number} {preview.channels?.[0]?.name}
              </Text>
            )}
          </Pressable>
          {featured ? <Featured game={featured} league={leagueOf(featured.league)?.name ?? ''} /> : <View style={st.featured} />}
        </View>

        {!!error && <Text style={st.error}>{error}</Text>}

        {view === 'scores' && (
          <>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={st.chips}>
              <Chip label="All" on={sport === 'all'} onPress={() => setSport('all')} />
              {sports.map((s) => (
                <Chip key={s} label={sportNames[s] ?? s} color={sportColors[s]} on={sport === s} onPress={() => setSport(s)} />
              ))}
              <View style={st.chipGap} />
              <Chip label="With a channel" on={withChannel} onPress={() => setWithChannel(!withChannel)} />
            </ScrollView>
            {!day && <ActivityIndicator color={colors.accent} />}
            {day && all.length === 0 && <Text style={st.muted}>No games {withChannel ? 'on your channels ' : ''}on {dayLabel(offset).toLowerCase()}.</Text>}
            {liveGames.length > 0 &&
              row('live', 'Live now', liveGames, takeFirst(), undefined, `${inProgress} IN PROGRESS · ${liveGames.length - inProgress} STARTING SOON`)}
            {yours.length > 0 && row('yours', 'Your teams', yours, takeFirst())}
            {byLeague.map(([l, gs]) => row(l.id, l.name, gs, takeFirst()))}
          </>
        )}

        {view === 'teams' && (
          <>
            <View style={st.head}>
              <Button label={followed.length ? 'Edit teams' : 'Follow teams'} primary={!followed.length} preferFocus onPress={() => setPicking(true)} />
            </View>
            {followed.length === 0 && (
              <Text style={st.muted}>
                Follow teams from any league and each gets its own row here, a star everywhere in the Sports Hub, and a heads-up before its games.
              </Text>
            )}
            {followed.map((t) => {
              const games = (home?.games ?? []).filter((g) => g.league === t.league && (g.home.id === t.id || g.away.id === t.id)).sort(byState)
              return games.length ? (
                row(`${t.league}/${t.id}`, t.name, games, false, <Logo team={t} size={40} />, leagueOf(t.league)?.name.toUpperCase())
              ) : (
                <View key={`${t.league}/${t.id}`} style={st.section}>
                  <View style={st.rowHead}>
                    <Logo team={t} size={40} />
                    <Text style={st.sectionTitle}>{t.name}</Text>
                  </View>
                  <Text style={st.muted}>No games from two days ago to a week from now.</Text>
                </View>
              )
            })}
          </>
        )}

        {view === 'channels' && (
          <ChannelsView
            active={active}
            channels={channels}
            games={day?.games ?? []}
            onWatch={(c) => nav.push({ name: 'live', channelId: c.id })}
            onMultiView={(c) => addToMultiView(c.id)}
          />
        )}
      </ScrollView>

      {!isPhone && (
        <View style={st.hints} pointerEvents="none">
          <Text style={st.hint}>
            <Text style={st.hintKey}>OK</Text> Preview / Full screen    <Text style={st.hintKey}>Hold OK</Text> Box score & stats    <Text style={st.hintKey}>PLAY</Text> Add to
            Multi-View
          </Text>
        </View>
      )}
      {!!notice && (
        <View style={st.notice} pointerEvents="none">
          <Text style={st.noticeText}>{notice}</Text>
        </View>
      )}

      {details && (
        <GameDetails
          game={details}
          league={leagueOf(details.league)?.name ?? ''}
          sport={leagueOf(details.league)?.sport}
          followed={followed}
          onFollow={follow}
          onWatch={
            details.channels?.length
              ? () => {
                  setDetails(null)
                  nav.push({ name: 'live', channelId: details.channels![0].id })
                }
              : undefined
          }
          onMultiView={details.channels?.length ? () => addToMultiView(details.channels![0].id) : undefined}
          lights={lights?.person.enabled && details.mine?.length ? { running: !lights.person.off, toggle: toggleLights } : undefined}
          onClose={() => setDetails(null)}
        />
      )}
      {dayMenu && (
        <Modal transparent animationType="fade" onRequestClose={() => setDayMenu(false)}>
          <View style={st.scrim}>
            <View style={st.menu}>
              {DAYS.map((d) => (
                <Button
                  key={d}
                  label={dayLabel(d)}
                  primary={d === offset}
                  preferFocus={d === offset}
                  onPress={() => {
                    setDayMenu(false)
                    setDay(null)
                    setOffset(d)
                  }}
                />
              ))}
            </View>
          </View>
        </Modal>
      )}
      {settings && (
        <SportsSettings
          prefs={prefs}
          lights={lights}
          followed={followed.length}
          onPrefs={async (patch) => {
            try {
              setPrefs(await patchPrefs(patch))
              if ('scoreBug' in patch || 'spoilerFree' in patch) refreshScoreBug.current()
            } catch (e) {
              say((e as Error).message)
            }
          }}
          onTeams={() => {
            setSettings(false)
            setPicking(true)
          }}
          onSwitchLights={switchLights}
          onToggleLights={toggleLights}
          onLightsSettings={() => {
            setSettings(false)
            setLightsPanel(true)
          }}
          onClose={() => setSettings(false)}
        />
      )}
      {lightsPanel && (
        <GameLights
          teams={followed}
          onClose={() => {
            setLightsPanel(false)
            fetchGameLightsSetup().then(setLights)
          }}
        />
      )}
    </View>
  )
}

function IconButton({ label, onPress }: { label: string; onPress: () => void }) {
  return (
    <Focusable onPress={onPress} style={st.iconButton}>
      <Text style={st.iconText}>{label}</Text>
    </Focusable>
  )
}

function Chip({ label, on, color, onPress }: { label: string; on: boolean; color?: string; onPress: () => void }) {
  return (
    <Focusable onPress={onPress} style={StyleSheet.flatten([st.chip, on && st.chipOn, color ? { borderLeftColor: color, borderLeftWidth: 3 } : null])}>
      <Text style={[st.chipText, on && st.chipTextOn]}>{label}</Text>
    </Focusable>
  )
}

/** The focused (or previewing) game, large: line score, where it's on and the standouts. */
function Featured({ game: g, league }: { game: SportsGame; league: string }) {
  const shown = scoreShown(g)
  const periods = Math.max(g.away.lines?.length ?? 0, g.home.lines?.length ?? 0)
  const cols = Array.from({ length: Math.min(periods, 9) }, (_, i) => i)
  const sideRow = (s: SportsSide) => (
    <View style={st.fRow}>
      <Logo team={s} size={44} />
      <Text style={st.fName} numberOfLines={1}>
        {s.name}
        {!!s.record && <Text style={st.fRecord}>  {s.record}</Text>}
      </Text>
      {shown &&
        cols.map((i) => (
          <Text key={i} style={st.fCell}>
            {s.lines?.[i] ?? ''}
          </Text>
        ))}
      {g.state !== 'pre' && <Text style={st.fTotal}>{shown ? s.score : '–'}</Text>}
    </View>
  )
  return (
    <View style={st.featured}>
      {shown && cols.length > 0 && (
        <View style={st.fRow}>
          <View style={st.flex} />
          {cols.map((i) => (
            <Text key={i} style={[st.fCell, st.fHead]}>
              {i + 1}
            </Text>
          ))}
          <Text style={[st.fTotal, st.fHead]}>T</Text>
        </View>
      )}
      {sideRow(g.away)}
      {sideRow(g.home)}
      <Text style={st.fMeta} numberOfLines={1}>
        {[league, g.state === 'pre' ? new Date(g.start).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) : g.detail, g.broadcasts?.[0]].filter(Boolean).join('  ·  ')}
      </Text>
      {(shown || g.state === 'pre') &&
        [g.away, g.home].map(
          (s) =>
            !!s.leader && (
              <Text key={s.id} style={st.fLeader} numberOfLines={1}>
                {s.abbr}  {leaderText(s.leader, true)}
              </Text>
            ),
        )}
      {!shown && g.state !== 'pre' && <Text style={st.fLeader}>Spoiler-free: hold OK on the game to show the score.</Text>}
    </View>
  )
}

/** Sports channels and the channels showing a game, OK to watch, PLAY or hold for Multi-View. */
function ChannelsView({
  active,
  channels,
  games,
  onWatch,
  onMultiView,
}: {
  active: boolean
  channels: LiveChannel[] | null
  games: SportsGame[]
  onWatch: (c: LiveChannel) => void
  onMultiView: (c: LiveChannel) => void
}) {
  const focused = useRef<LiveChannel | null>(null)
  useTVEventHandler((evt) => {
    if (active && evt?.eventType === 'playPause' && focused.current) onMultiView(focused.current)
  })
  const onChannel = useMemo(() => {
    const m = new Map<number, SportsGame>()
    for (const g of [...games].sort(byState)) for (const c of g.channels ?? []) if (!m.has(c.id)) m.set(c.id, g)
    return m
  }, [games])
  if (!channels) return <ActivityIndicator color={colors.accent} />
  const shown = channels.filter((c) => onChannel.has(c.id) || isSportsChannel(c))
  const withGames = shown.filter((c) => onChannel.has(c.id))
  const rest = shown.filter((c) => !onChannel.has(c.id))
  if (!shown.length) return <Text style={st.muted}>No sports channels in your Live TV lineup.</Text>
  const grid = (list: LiveChannel[], first: boolean) => (
    <View style={st.grid}>
      {list.map((c, i) => (
        <ChannelCard
          key={c.id}
          channel={c}
          game={onChannel.get(c.id)}
          preferFocus={first && i === 0}
          onPress={() => onWatch(c)}
          onLongPress={() => onMultiView(c)}
          onFocus={() => (focused.current = c)}
        />
      ))}
    </View>
  )
  return (
    <>
      {withGames.length > 0 && (
        <View style={st.section}>
          <Text style={st.sectionTitle}>Showing games</Text>
          {grid(withGames, true)}
        </View>
      )}
      {rest.length > 0 && (
        <View style={st.section}>
          <Text style={st.sectionTitle}>Sports channels</Text>
          {grid(rest, !withGames.length)}
        </View>
      )}
    </>
  )
}

/**
 * The hub's gear: spoiler-free mode, the score bug, your teams, and Game
 * Lights (start and stop it, and its settings) once it's on for this profile.
 */
function SportsSettings({
  prefs,
  lights,
  followed,
  onPrefs,
  onTeams,
  onSwitchLights,
  onToggleLights,
  onLightsSettings,
  onClose,
}: {
  prefs: SportsPrefs
  lights: GameLightsSetup | null
  followed: number
  onPrefs: (patch: Partial<SportsPrefs>) => void
  onTeams: () => void
  onSwitchLights: () => void
  onToggleLights: () => void
  onLightsSettings: () => void
  onClose: () => void
}) {
  const press = isPhone ? 'Tap' : 'OK'
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <ScrollView style={st.settings} contentContainerStyle={st.settingsIn}>
          <Text style={st.sectionTitle}>Sports Hub settings</Text>
          <Focusable hasTVPreferredFocus onPress={() => onPrefs({ spoilerFree: !prefs.spoilerFree })} style={st.option}>
            <Text style={st.optionName}>Spoiler-free mode: {prefs.spoilerFree ? 'On' : 'Off'}</Text>
            <Text style={st.muted}>Hides scores, stats and the score bug until you choose to see a game's score. {press} to switch.</Text>
          </Focusable>
          <Focusable onPress={() => onPrefs({ scoreBug: prefs.scoreBug === false })} style={st.option}>
            <Text style={st.optionName}>Score bug: {prefs.scoreBug === false ? 'Off' : 'On'}</Text>
            <Text style={st.muted}>Your teams' scores in a corner while you watch something else. {press} to switch.</Text>
          </Focusable>
          <Focusable onPress={onTeams} style={st.option}>
            <Text style={st.optionName}>Favorite teams: {followed}</Text>
            <Text style={st.muted}>Follow and unfollow teams in any league.</Text>
          </Focusable>
          {lights ? (
            <>
              <Focusable onPress={onSwitchLights} style={st.option}>
                <Text style={st.optionName}>Game Lights: {lights.person.enabled ? 'On' : 'Off'}</Text>
                <Text style={st.muted}>
                  Flash the Hue lights when the teams you follow score. The same switch as Settings → This TV. {press} to turn it {lights.person.enabled ? 'off' : 'on'}.
                </Text>
              </Focusable>
              {!!lights.person.enabled && (
                <Focusable onPress={onToggleLights} style={st.option}>
                  <Text style={st.optionName}>{lights.person.off ? 'Stopped' : 'Running'}: {press} to {lights.person.off ? 'start' : 'stop'}</Text>
                  <Text style={st.muted}>Stop the lights for a while, say for a game you're recording, without turning Game Lights off.</Text>
                </Focusable>
              )}
              {!!lights.person.enabled && (
                <Focusable onPress={onLightsSettings} style={st.option}>
                  <Text style={st.optionName}>Game Lights settings</Text>
                  <Text style={st.muted}>
                    {lights.paired || lights.person.local ? 'Teams, room, TV delay and what the other team does.' : "Pair this home's Hue hub in Settings → This TV, or ask an admin to pair one."}
                  </Text>
                </Focusable>
              )}
            </>
          ) : (
            <View style={st.option}>
              <Text style={st.optionName}>Game Lights</Text>
              <Text style={st.muted}>The Game Lights plugin isn't running on this server. An admin can turn it on under Plugins.</Text>
            </View>
          )}
          <View style={st.buttons}>
            <Button small label="Close" onPress={onClose} />
          </View>
        </ScrollView>
      </View>
    </Modal>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  screen: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: isPhone ? 160 : 110 },
  head: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', marginBottom: 20 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800', marginRight: 30 },
  viewTab: { paddingVertical: 10, paddingHorizontal: 22, borderBottomWidth: 4, borderBottomColor: 'transparent' },
  viewTabOn: { borderBottomColor: colors.accent },
  viewTabText: { color: colors.muted, fontSize: 22, fontWeight: '800', letterSpacing: 2 },
  viewTabTextOn: { color: colors.text },
  dayButton: { borderWidth: 2, borderColor: colors.accent, borderRadius: 14, paddingVertical: 10, paddingHorizontal: 22, marginRight: 14 },
  dayText: { color: colors.text, fontSize: font.small, fontWeight: '700' },
  iconButton: { width: 64, height: 64, borderRadius: 14, alignItems: 'center', justifyContent: 'center', marginLeft: 8 },
  iconText: { color: colors.text, fontSize: 28 },
  top: { flexDirection: isPhone ? 'column' : 'row', marginBottom: 20 },
  preview: { width: isPhone ? 686 : 640, height: isPhone ? 386 : 360, borderRadius: 18, overflow: 'hidden', backgroundColor: '#000', alignItems: 'center', justifyContent: 'center', marginRight: 40, marginBottom: isPhone ? 20 : 0 },
  previewIcon: { fontSize: 60 },
  previewText: { color: colors.muted, fontSize: font.small, marginTop: 16 },
  previewTag: { position: 'absolute', left: 14, bottom: 12, color: colors.text, fontSize: 18, fontWeight: '700', backgroundColor: 'rgba(0,0,0,0.6)', borderRadius: 6, paddingHorizontal: 10, paddingVertical: 4, overflow: 'hidden' },
  featured: { flex: 1, minHeight: 300, backgroundColor: colors.surface, borderRadius: 18, borderWidth: 1, borderColor: colors.border, paddingHorizontal: 32, paddingVertical: 24, justifyContent: 'center' },
  fRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 12 },
  fName: { flex: 1, color: colors.text, fontSize: font.body, fontWeight: '700', marginLeft: 16 },
  fRecord: { color: colors.muted, fontSize: font.small, fontWeight: '400' },
  fCell: { width: 44, color: colors.text, fontSize: font.small, textAlign: 'center' },
  fHead: { color: colors.muted, fontSize: 18 },
  fTotal: { width: 70, color: colors.accent, fontSize: font.heading, fontWeight: '800', textAlign: 'right' },
  fMeta: { color: colors.accent, fontSize: font.small, marginTop: 8 },
  fLeader: { color: colors.muted, fontSize: 20, marginTop: 8 },
  chips: { paddingVertical: 8, alignItems: 'center' },
  chip: { borderRadius: 10, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.surface, paddingVertical: 8, paddingHorizontal: 18, marginRight: 12 },
  chipOn: { backgroundColor: colors.accent, borderColor: colors.accent },
  chipText: { color: colors.text, fontSize: 20 },
  chipTextOn: { color: colors.accentText, fontWeight: '700' },
  chipGap: { width: 40 },
  section: { marginTop: 22 },
  rowHead: { flexDirection: 'row', alignItems: 'center', marginBottom: 6 },
  sectionTitle: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginLeft: 0, marginRight: 18 },
  sectionSub: { color: colors.muted, fontSize: 18, fontWeight: '700', letterSpacing: 1 },
  row: { paddingVertical: 10, paddingRight: 40 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 10 },
  muted: { color: colors.muted, fontSize: font.small, maxWidth: 1400, marginTop: 6 },
  error: { color: colors.danger, fontSize: font.small, marginVertical: 8 },
  hints: { position: 'absolute', left: 0, right: 0, bottom: 0, paddingVertical: 14, paddingHorizontal: gutter, backgroundColor: 'rgba(11,13,16,0.92)' },
  hint: { color: colors.muted, fontSize: 20, textAlign: 'right' },
  hintKey: { color: colors.text, fontWeight: '800' },
  notice: { position: 'absolute', top: 30, alignSelf: 'center', backgroundColor: colors.surface3, borderRadius: 14, paddingVertical: 14, paddingHorizontal: 28 },
  noticeText: { color: colors.text, fontSize: font.small },
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  menu: { backgroundColor: colors.surface, borderRadius: 20, padding: 30, flexDirection: 'row', flexWrap: 'wrap', maxWidth: 1200, justifyContent: 'center' },
  settings: { flexGrow: 0, width: 1100, maxHeight: '92%', backgroundColor: colors.surface, borderRadius: 22 },
  settingsIn: { padding: 40 },
  option: { backgroundColor: colors.surface2, borderRadius: 18, padding: 24, marginTop: 18 },
  optionName: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginTop: 14 },
}))
