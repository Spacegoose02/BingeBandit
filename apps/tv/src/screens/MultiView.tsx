import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ActivityIndicator, Modal, ScrollView, StyleSheet, Text, View } from 'react-native'
import Video from 'react-native-video'
import { api, authHeaders } from '../api'
import { Button, Focusable } from '../components'
import { useNav } from '../nav'
import { useLandscape } from '../screen'
import { scaled, colors, font, isPhone } from '../theme'
import type { LiveChannel, SportsDay, SportsGame, SportsHome } from '../types'
import { MAX_SCREENS, byState, dayKey, isSportsChannel, loadSportsPrefs, multiViewPicks, patchPrefs, scoreShown, type MultiViewLayout } from '../sports/hub'
import { ChannelCard, matchup, scoreLine } from '../sports/parts'
import { joinChannel, leaveChannel, liveBuffer, reportBuffering } from '../live'
import { NO_LIVE_BADGE } from './Player'

type Tile = number | null // a channel id, or an empty screen

/**
 * Sports Multi-View: up to four channels at once. The screen with the sound
 * has a speaker; OK moves the sound to the focused screen, holding OK has
 * its options (change channel, full screen, remove). Up from the top row
 * reaches the toolbar: how many screens, one press for the next game of
 * your teams, and saved layouts, which follow the profile.
 */
export default function MultiView({ channels: initial }: { channels: number[] }) {
  useLandscape()
  const nav = useNav()
  const [tiles, setTiles] = useState<Tile[]>(() => (initial.length ? initial.slice(0, MAX_SCREENS) : [null, null]))
  const [audio, setAudio] = useState(0)
  const [focused, setFocused] = useState(0)
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [games, setGames] = useState<SportsGame[]>([])
  const [mine, setMine] = useState<SportsGame[]>([])
  const [layouts, setLayouts] = useState<MultiViewLayout[]>([])
  const [picker, setPicker] = useState<number | null>(null)
  const [options, setOptions] = useState<number | null>(null)
  const [layoutsMenu, setLayoutsMenu] = useState(false)
  const [notice, setNotice] = useState('')
  const say = useCallback((t: string) => {
    setNotice(t)
    setTimeout(() => setNotice((n) => (n === t ? '' : n)), 4000)
  }, [])

  useEffect(() => {
    api.get<LiveChannel[]>('/api/live/channels').then(setChannels, () => undefined)
    loadSportsPrefs().then((p) => setLayouts(p.multiViewLayouts ?? []), () => undefined)
    const load = () => {
      api.get<SportsDay>(`/api/sports/scores?date=${dayKey(0)}`).then((d) => setGames(d.games), () => undefined)
      api.get<SportsHome>('/api/sports').then((h) => setMine(h.games), () => undefined)
    }
    load()
    const t = setInterval(load, 30000)
    return () => clearInterval(t)
  }, [])
  // The hub's Multi-View button shows what's here.
  useEffect(() => {
    multiViewPicks.splice(0, multiViewPicks.length, ...tiles.filter((t): t is number => t !== null))
  }, [tiles])

  const channel = (id: Tile) => channels.find((c) => c.id === id)
  const gameOn = useMemo(() => {
    const m = new Map<number, SportsGame>()
    for (const g of [...games, ...mine].sort(byState)) for (const c of g.channels ?? []) if (!m.has(c.id)) m.set(c.id, g)
    return m
  }, [games, mine])

  const put = (i: number, id: number) => {
    setTiles((ts) => ts.map((t, j) => (j === i ? id : t === id ? null : t)))
    setAudio(i)
  }
  const setCount = (n: number) => {
    setTiles((ts) => (n > ts.length ? [...ts, ...Array<Tile>(n - ts.length).fill(null)] : ts.slice(0, n)))
    setAudio((a) => Math.min(a, n - 1))
    setFocused((f) => Math.min(f, n - 1))
  }
  // Each screen keeps its own key as others are removed around it, so the
  // screens after a removed one keep playing instead of all rejoining.
  const keys = useRef<number[]>([])
  const nextKey = useRef(0)
  while (keys.current.length < tiles.length) keys.current.push(nextKey.current++)
  if (keys.current.length > tiles.length) keys.current.length = tiles.length
  const remove = (i: number) => {
    if (tiles.length === 1) return setTiles([null])
    keys.current.splice(i, 1)
    setTiles((ts) => ts.filter((_, j) => j !== i))
    setFocused((f) => (f > i ? f - 1 : Math.min(f, tiles.length - 2)))
    setAudio((a) => (a === i ? 0 : a > i ? a - 1 : a))
  }

  // One press: the next game of your teams that's on a channel and not up yet.
  const nextGame = () => {
    const g = mine
      .filter((x) => x.state !== 'post' && x.channels?.length && !x.channels.some((c) => tiles.includes(c.id)))
      .sort(byState)[0]
    if (!g) return say("None of your teams' games is on your channels right now.")
    const id = g.channels![0].id
    let i = tiles.indexOf(null)
    if (i < 0 && tiles.length < MAX_SCREENS) {
      i = tiles.length
      setTiles((ts) => [...ts, id])
      setAudio(i)
    } else {
      if (i < 0) i = focused
      put(i, id)
    }
    say(`${matchup(g)} on ${g.channels![0].name}`)
  }

  const saveLayout = async () => {
    const ids = tiles.filter((t): t is number => !!t)
    if (!ids.length) return say('Pick a channel first.')
    const name = ids.map((id) => channel(id)?.name ?? `${id}`).join(' · ').slice(0, 60)
    const next = [...layouts.filter((l) => l.name !== name), { name, channels: ids }].slice(-12)
    try {
      const p = await patchPrefs({ multiViewLayouts: next })
      setLayouts(p.multiViewLayouts ?? next)
      say(`Saved "${name}".`)
    } catch (e) {
      say((e as Error).message)
    }
  }
  const deleteLayout = async (name: string) => {
    const next = layouts.filter((l) => l.name !== name)
    try {
      const p = await patchPrefs({ multiViewLayouts: next })
      setLayouts(p.multiViewLayouts ?? next)
    } catch (e) {
      say((e as Error).message)
    }
  }

  const n = tiles.length
  // 1: full; 2: side by side; 3: one large and two stacked; 4: a grid.
  // Every screen sits in its own slot under the same parent, so changing the
  // layout moves the slots instead of rebuilding (and reloading) the players.
  const slots: [number, number, number, number][] =
    n === 1
      ? [[0, 0, 100, 100]]
      : n === 2
        ? [[0, 0, 50, 100], [50, 0, 50, 100]]
        : n === 3
          ? [[0, 0, 200 / 3, 100], [200 / 3, 0, 100 / 3, 50], [200 / 3, 50, 100 / 3, 50]]
          : [[0, 0, 50, 50], [50, 0, 50, 50], [0, 50, 50, 50], [50, 50, 50, 50]]
  const grid = tiles.map((_, i) => {
    const [left, top, width, height] = slots[i]
    return (
      <View key={keys.current[i]} style={[st.slot, { left: `${left}%`, top: `${top}%`, width: `${width}%`, height: `${height}%` }]}>
        <Screen
          index={i}
          channelId={tiles[i]}
          channel={channel(tiles[i])}
          game={tiles[i] ? gameOn.get(tiles[i]!) : undefined}
          audio={audio === i}
          preferFocus={i === 0}
          style={n === 2 ? st.half : st.full}
          onFocus={() => setFocused(i)}
          onPress={() => (tiles[i] ? setAudio(i) : setPicker(i))}
          onLongPress={() => (tiles[i] ? setOptions(i) : setPicker(i))}
        />
      </View>
    )
  })

  return (
    <View style={st.screen}>
      <View style={st.toolbar}>
        {[1, 2, 3, 4].map((k) => (
          <Button key={k} small primary={k === n} label={`${k} ${k === 1 ? 'screen' : 'screens'}`} onPress={() => setCount(k)} />
        ))}
        <Button small label="▶ Next game of my team" onPress={nextGame} />
        <Button small label="Save layout" onPress={saveLayout} />
        <Button small label={`Layouts (${layouts.length})`} onPress={() => setLayoutsMenu(true)} />
        <Button small label="Exit" onPress={() => nav.back()} />
      </View>
      <View style={st.flex}>{grid}</View>
      {!isPhone && (
        <Text style={st.hint}>
          <Text style={st.hintKey}>OK</Text> Audio  ·  <Text style={st.hintKey}>Hold OK</Text> Options  ·  <Text style={st.hintKey}>▲</Text> Toolbar  ·  <Text style={st.hintKey}>BACK</Text> Exit
        </Text>
      )}
      {!!notice && (
        <View style={st.notice} pointerEvents="none">
          <Text style={st.noticeText}>{notice}</Text>
        </View>
      )}

      {options !== null && (
        <Modal transparent animationType="fade" onRequestClose={() => setOptions(null)}>
          <View style={st.scrim}>
            <View style={st.menu}>
              <Text style={st.menuTitle}>{channel(tiles[options])?.name ?? 'Screen'}</Text>
              <Button preferFocus label="Change channel" onPress={() => {
                  setPicker(options)
                  setOptions(null)
                }} />
              <Button label="Listen to this one" onPress={() => {
                  setAudio(options)
                  setOptions(null)
                }} />
              <Button
                label="Full screen"
                onPress={() => {
                  const id = tiles[options]
                  setOptions(null)
                  // Replaced, not pushed, so the other screens stop streaming.
                  if (id) nav.replace({ name: 'live', channelId: id })
                }}
              />
              <Button label="Remove this screen" onPress={() => {
                  remove(options)
                  setOptions(null)
                }} />
              <Button label="Cancel" onPress={() => setOptions(null)} />
            </View>
          </View>
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
      {layoutsMenu && (
        <Modal transparent animationType="fade" onRequestClose={() => setLayoutsMenu(false)}>
          <View style={st.scrim}>
            <ScrollView style={st.box} contentContainerStyle={st.boxIn}>
              <Text style={st.menuTitle}>Saved layouts</Text>
              {layouts.length === 0 && <Text style={st.muted}>Nothing saved yet. Set up the screens, then Save layout in the toolbar.</Text>}
              {layouts.map((l, i) => (
                <View key={l.name} style={st.layoutRow}>
                  <Button
                    preferFocus={i === 0}
                    label={`${l.name}  (${l.channels.length})`}
                    onPress={() => {
                      setTiles(l.channels.slice(0, MAX_SCREENS))
                      setAudio(0)
                      setLayoutsMenu(false)
                    }}
                  />
                  <Button small label="Delete" onPress={() => deleteLayout(l.name)} />
                </View>
              ))}
              <Button small label="Close" preferFocus={!layouts.length} onPress={() => setLayoutsMenu(false)} />
            </ScrollView>
          </View>
        </Modal>
      )}
    </View>
  )
}

/** One screen: its channel's stream, muted unless it has the sound, labelled with the channel and the game on it. */
/**
 * One screen's stream, kept going the way the full-screen player keeps a
 * channel going: it plays the server's buffer behind live (so a slow
 * segment eats into the cushion instead of freezing the picture), tells the
 * server when it has been stuck long enough to move to the channel's next
 * stream, and rejoins after an error or when the picture stops moving.
 */
function useTileStream(channelId: Tile) {
  const [stream, setStream] = useState<{ src: string; buffer: number } | null>(null)
  const [attempt, setAttempt] = useState(0)
  const viewer = useRef('')
  const stall = useRef(12)
  useEffect(() => {
    setStream(null)
    if (!channelId) return
    let cancelled = false
    let retry: ReturnType<typeof setTimeout> | undefined
    joinChannel(channelId).then(
      (r) => {
        // A join answered after the screen moved on is left at once; it
        // mustn't take the place of the newer one, which would then never be left.
        if (cancelled) return leaveChannel(r.viewerId)
        viewer.current = r.viewerId
        stall.current = r.stallSecs
        setStream({ src: r.playlist, buffer: r.bufferSecs })
      },
      () => {
        if (!cancelled) retry = setTimeout(() => setAttempt((a) => a + 1), 5000)
      },
    )
    return () => {
      cancelled = true
      if (retry) clearTimeout(retry)
      leaveChannel(viewer.current)
      viewer.current = ''
    }
  }, [channelId, attempt])
  const rejoin = useCallback(() => setAttempt((a) => a + 1), [])

  // Stuck: buffering, or "playing" with the position standing still (a
  // decoder that gave up). Past the server's stall time, report it, and
  // rejoin unless the server moved the channel to another stream.
  const since = useRef(0)
  const lastPos = useRef(-1)
  const check = useCallback(
    (stuck: boolean) => {
      const now = Date.now()
      if (!stuck) {
        since.current = 0
        return
      }
      if (!since.current) since.current = now
      const secs = (now - since.current) / 1000
      if (secs >= stall.current) {
        since.current = now
        reportBuffering(viewer.current, secs).then((switched) => {
          if (!switched) rejoin()
        })
      }
    },
    [rejoin],
  )
  useEffect(() => {
    since.current = 0
    lastPos.current = -1
  }, [stream])
  return {
    src: stream?.src ?? null,
    key: `${stream?.src}#${attempt}`,
    bufferConfig: liveBuffer(stream?.buffer ?? 10),
    onBuffer: (e: { isBuffering: boolean }) => check(e.isBuffering),
    onProgress: (e: { currentTime: number }) => {
      check(Math.abs(e.currentTime - lastPos.current) < 0.1)
      lastPos.current = e.currentTime
    },
    // Waits longer after each failure, so a box out of video decoders isn't hammered.
    onError: () => setTimeout(rejoin, Math.min(30000, 2000 * (attempt + 1))),
  }
}

function Screen({
  channelId,
  channel,
  game,
  audio,
  preferFocus,
  style,
  onFocus,
  onPress,
  onLongPress,
}: {
  index: number
  channelId: Tile
  channel?: LiveChannel
  game?: SportsGame
  audio: boolean
  preferFocus: boolean
  style: object
  onFocus: () => void
  onPress: () => void
  onLongPress: () => void
}) {
  const stream = useTileStream(channelId)
  const src = stream.src
  const [focused, setFocused] = useState(false)
  const score = game && scoreShown(game) ? scoreLine(game) : ''
  return (
    <Focusable
      onPress={onPress}
      onLongPress={onLongPress}
      hasTVPreferredFocus={preferFocus}
      onFocus={() => {
        setFocused(true)
        onFocus()
      }}
      onBlur={() => setFocused(false)}
      style={StyleSheet.flatten([st.tile, style, focused && st.tileFocused])}
    >
      {channelId && src ? (
        <View style={StyleSheet.absoluteFill} pointerEvents="none">
          <Video
            key={stream.key}
            source={{ uri: src, type: 'm3u8', headers: authHeaders(), bufferConfig: stream.bufferConfig }}
            style={StyleSheet.absoluteFill}
            onBuffer={stream.onBuffer}
            onProgress={stream.onProgress}
            progressUpdateInterval={1000}
            onError={stream.onError}
            resizeMode="contain"
            muted={!audio}
            // Each player would take Android's audio focus as it starts, and
            // losing it pauses the others, so only the newest screen played.
            disableFocus
            controlsStyles={NO_LIVE_BADGE}
            focusable={false}
          />
        </View>
      ) : channelId ? (
        <ActivityIndicator color={colors.accent} />
      ) : (
        <Text style={st.empty}>{isPhone ? 'Tap' : 'OK'} to pick a channel</Text>
      )}
      {channel && (
        <Text style={st.label} numberOfLines={1}>
          {channel.number}  {channel.name}
        </Text>
      )}
      {(!!score || !!channel?.now?.title) && (
        <Text style={st.bottom} numberOfLines={1}>
          {score || channel?.now?.title}
        </Text>
      )}
      {audio && channelId && <Text style={st.speaker}>🔊</Text>}
    </Focusable>
  )
}

/** Your teams' games on your channels, then other games on now, then the sports channels. */
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
  const mineIds = new Set(mine.filter((g) => g.state !== 'post').flatMap((g) => g.channels?.map((c) => c.id) ?? []))
  const sections: [string, LiveChannel[]][] = [
    ['Your teams', channels.filter((c) => mineIds.has(c.id))],
    ['Games on now', channels.filter((c) => !mineIds.has(c.id) && gameOn.get(c.id)?.state === 'in')],
    ['Sports channels', channels.filter((c) => !mineIds.has(c.id) && gameOn.get(c.id)?.state !== 'in' && isSportsChannel(c))],
  ]
  let first = true
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <ScrollView style={st.pickerBox} contentContainerStyle={st.boxIn}>
          <Text style={st.menuTitle}>Pick a channel</Text>
          {sections.map(
            ([title, list]) =>
              list.length > 0 && (
                <View key={title}>
                  <Text style={st.muted}>{title}</Text>
                  <View style={st.wrap}>
                    {list.map((c) => {
                      const f = first
                      first = false
                      return <ChannelCard key={c.id} channel={c} game={gameOn.get(c.id)} preferFocus={f} onPress={() => onPick(c.id)} />
                    })}
                  </View>
                </View>
              ),
          )}
          {sections.every(([, l]) => !l.length) && <Text style={st.muted}>No sports channels in your Live TV lineup.</Text>}
          <Button small label="Cancel" preferFocus={sections.every(([, l]) => !l.length)} onPress={onClose} />
        </ScrollView>
      </View>
    </Modal>
  )
}

const st = StyleSheet.create(scaled({
  flex: { flex: 1 },
  screen: { flex: 1, backgroundColor: '#05070a', padding: 14 },
  toolbar: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginBottom: 6 },
  slot: { position: 'absolute', justifyContent: 'center' },
  full: { flex: 1 },
  half: { width: '100%', aspectRatio: 16 / 9 },
  tile: { margin: 6, borderRadius: 14, overflow: 'hidden', backgroundColor: '#000', alignItems: 'center', justifyContent: 'center', borderWidth: 3, borderColor: 'transparent' },
  tileFocused: { borderColor: colors.text },
  empty: { color: colors.muted, fontSize: font.small },
  label: { position: 'absolute', top: 12, left: 12, color: colors.text, fontSize: 22, fontWeight: '700', backgroundColor: 'rgba(11,13,16,0.75)', borderRadius: 8, paddingHorizontal: 12, paddingVertical: 4, overflow: 'hidden', maxWidth: '80%' },
  bottom: { position: 'absolute', bottom: 12, left: 12, right: 60, color: colors.text, fontSize: 20, textShadowColor: '#000', textShadowRadius: 4 },
  speaker: { position: 'absolute', bottom: 10, right: 14, fontSize: 24 },
  hint: { color: colors.muted, fontSize: 20, textAlign: 'right', paddingTop: 6, paddingRight: 10 },
  hintKey: { color: colors.text, fontWeight: '800' },
  notice: { position: 'absolute', top: 90, alignSelf: 'center', backgroundColor: colors.surface3, borderRadius: 14, paddingVertical: 14, paddingHorizontal: 28 },
  noticeText: { color: colors.text, fontSize: font.small },
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  menu: { backgroundColor: colors.surface, borderRadius: 20, padding: 30, minWidth: 600, alignItems: 'stretch' },
  menuTitle: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginBottom: 16 },
  muted: { color: colors.muted, fontSize: font.small, marginVertical: 10 },
  box: { flexGrow: 0, width: 1100, maxHeight: '90%', backgroundColor: colors.surface, borderRadius: 22 },
  pickerBox: { flexGrow: 0, width: 1500, maxHeight: '90%', backgroundColor: colors.surface, borderRadius: 22 },
  boxIn: { padding: 36 },
  layoutRow: { flexDirection: 'row', alignItems: 'center' },
  wrap: { flexDirection: 'row', flexWrap: 'wrap' },
}))
