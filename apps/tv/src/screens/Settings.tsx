import React, { useCallback, useEffect, useState } from 'react'
import { Modal, ScrollView, StyleSheet, Text, View } from 'react-native'
import { ApiError, api, current, saveSession } from '../api'
import { Button, Focusable } from '../components'
import { hueArea, hueSyncOn, setHueArea, setHueSync, type MenuPosition } from '../devicePrefs'
import { fetchGameLightsSetup, fetchHueMode, hueModeNames, nextHueMode, saveGameLights, saveHueMode, type GameLightsSetup, type HueMode } from '../lights'
import { Icon } from '../icons'
import { scaled, colors, font, gutter, isPhone, px } from '../theme'
import type { Library, Member } from '../types'
import { checkForUpdate, installed, installUpdate, selfUpdates, type AppRelease } from '../updates'
import { fetchHousehold, PinPad } from './Profiles'

// The tabs a person can turn off (prefs.hiddenTabs). Home, Search and Settings
// always show, so there's always a way back to turn them on.
export type HideableTab = 'sports' | 'live' | 'discover' | 'favorites' | 'more'
export const hideableNames: Record<HideableTab, string> = { sports: 'Sports', live: 'Live TV', discover: 'Discover', favorites: 'Favorites', more: 'More' }

/** What the menu shows, and how to change it; Home owns both. */
export interface MenuChoices {
  libraries: Library[]
  hidden: HideableTab[]
  tabs: HideableTab[] // the ones this person has at all (Live TV can be off for them)
  togglePin: (l: Library) => void
  toggleTab: (t: HideableTab) => void
  position: MenuPosition
  setPosition: (p: MenuPosition) => void
}

type Section = 'device' | 'menu' | 'alerts' | 'profiles' | 'admin'

/**
 * Runs a settings change, asking for the settings passcode first when the
 * server says this session is locked (423), then trying again.
 */
function useGuard() {
  const [ask, setAsk] = useState<{ retry: () => Promise<unknown>; error: string } | null>(null)
  const guard = useCallback(async (change: () => Promise<unknown>): Promise<boolean> => {
    try {
      await change()
      return true
    } catch (e) {
      if (e instanceof ApiError && e.status === 423) {
        setAsk({ retry: change, error: '' })
        return false
      }
      throw e
    }
  }, [])
  const prompt = ask && (
    <Modal transparent animationType="fade" onRequestClose={() => setAsk(null)}>
      <View style={st.modal}>
        <PinPad
          title="Enter your settings passcode"
          error={ask.error}
          onEnter={async (passcode) => {
            try {
              await api.post('/api/me/passcode/unlock', { passcode })
              await ask.retry()
              setAsk(null)
            } catch (e) {
              setAsk({ ...ask, error: (e as Error).message })
            }
          }}
          onCancel={() => setAsk(null)}
        />
      </View>
    </Modal>
  )
  return { guard, prompt }
}

export default function Settings({ onSignOut, onSwitchProfile, update, menu }: { onSignOut: () => void; onSwitchProfile: () => void; update: AppRelease | null; menu: MenuChoices }) {
  const admin = !!current()?.user.isAdmin
  const [section, setSection] = useState<Section>('device')
  const names: [Section, string][] = [
    ['device', isPhone ? 'This phone' : 'This TV'],
    ['menu', 'Menu'],
    ['alerts', 'Alerts'],
    ['profiles', 'Profiles & PINs'],
    ...(admin ? ([['admin', 'Server']] as [Section, string][]) : []),
  ]
  const { guard, prompt } = useGuard()
  return (
    <ScrollView contentContainerStyle={st.screen}>
      {/* Only OK switches, so moving down from the menu doesn't change the section. */}
      <View style={st.sections}>
        {names.map(([s, name]) => (
          <Focusable key={s} onPress={() => setSection(s)} style={StyleSheet.flatten([st.chip, section === s && st.chipActive])}>
            <Text style={[st.chipText, section === s && st.chipTextActive]}>{name}</Text>
          </Focusable>
        ))}
      </View>
      {section === 'device' && <Device update={update} menu={menu} onSignOut={onSignOut} onSwitchProfile={onSwitchProfile} />}
      {section === 'menu' && <MenuSection menu={menu} />}
      {section === 'alerts' && <AlertsSection />}
      {section === 'profiles' && <ProfilesSection guard={guard} />}
      {section === 'admin' && admin && <AdminSection guard={guard} />}
      {prompt}
    </ScrollView>
  )
}

function Device({ update, menu, onSignOut, onSwitchProfile }: { update: AppRelease | null; menu: MenuChoices; onSignOut: () => void; onSwitchProfile: () => void }) {
  const s = current()
  const [hasProfiles, setHasProfiles] = useState(false)
  useEffect(() => {
    fetchHousehold().then((m) => setHasProfiles(m.length > 1), () => undefined)
  }, [])
  // Hue Sync only shows once this profile picked Sync or Theater (Account
  // on the web), and only when the server runs the plugin.
  const [hueMode, setHueMode] = useState<HueMode | null>(null)
  const hue = hueMode !== null && hueMode !== 'off'
  const [hueOn, setHueOn] = useState(hueSyncOn)
  // Once shown, the mode row stays while this screen is open, so cycling past Off doesn't make it vanish.
  const [hueSeen, setHueSeen] = useState(false)
  useEffect(() => {
    fetchHueMode().then((m) => {
      setHueMode(m)
      setHueSeen(m !== null && m !== 'off')
    })
  }, [])
  const cycleHueMode = () => {
    if (!hueMode) return
    const m = nextHueMode(hueMode)
    setHueMode(m)
    saveHueMode(m).catch(() => setHueMode(hueMode))
  }
  const toggleHue = () => {
    setHueSync(!hueOn)
    setHueOn(!hueOn)
  }
  // The Hue room or zone this TV is in, from the Hue app. OK steps through them.
  const [areas, setAreas] = useState<{ id: string; name: string; type?: string }[]>([])
  const [area, setAreaState] = useState(hueArea)
  useEffect(() => {
    if (hue)
      api.get<{ id: string; name: string; type: string }[]>('/api/plugins/hue-sync/areas').then(
        (list) =>
          setAreas(
            list.map((a) => ({
              ...a,
              name: `${a.name} (${a.type === 'Zone' ? 'zone' : a.type === 'Entertainment' ? 'entertainment area' : 'room'})`,
            })),
          ),
        () => undefined,
      )
  }, [hue])
  const options = [{ id: '', name: 'Every light' }, ...areas]
  const areaName = options.find((a) => a.id === area)?.name ?? 'Every light'
  const nextArea = () => {
    const i = options.findIndex((a) => a.id === area)
    const id = options[(i + 1) % options.length].id
    setHueArea(id)
    setAreaState(id)
  }
  // Game Lights is off for each profile until it's turned on, here or in
  // Account on the web; then OK picks the room its lights flash in.
  const [lights, setLights] = useState<GameLightsSetup | null>(null)
  useEffect(() => {
    fetchGameLightsSetup().then(setLights)
  }, [])
  const saveLights = (person: GameLightsSetup['person']) => {
    if (!lights) return
    setLights({ ...lights, person })
    saveGameLights(person).catch(() => setLights(lights))
  }
  const lightsAreas = [{ id: '', name: "The house's game lights" }, ...(lights?.areas ?? []).map((a) => ({ id: a.id, name: `${a.name} (${a.type === 'Zone' ? 'zone' : 'room'})` }))]
  const lightsArea = lights?.person.area ?? ''
  const lightsAreaName = lightsAreas.find((a) => a.id === lightsArea)?.name ?? lightsAreas[0].name
  const nextLightsArea = () => {
    if (!lights) return
    const i = lightsAreas.findIndex((a) => a.id === lightsArea)
    saveLights({ ...lights.person, area: lightsAreas[(i + 1) % lightsAreas.length].id })
  }
  // Recommendations from other people, unless an admin turned them off.
  const [recs, setRecs] = useState<{ accept: boolean; enabled?: boolean } | null>(null)
  useEffect(() => {
    api.get<{ accept: boolean; enabled?: boolean }>('/api/me/recommendations').then(setRecs, () => undefined)
  }, [])
  const toggleRecs = () => {
    if (!recs) return
    const accept = !recs.accept
    setRecs({ ...recs, accept })
    api.put('/api/me/recommendations', { accept }).catch(() => setRecs(recs))
  }
  const [found, setFound] = useState<AppRelease | null>(update)
  const [msg, setMsg] = useState('')
  const [progress, setProgress] = useState<number | null>(null)

  const check = async () => {
    setMsg('Checking…')
    try {
      const rel = await checkForUpdate()
      setFound(rel)
      setMsg(rel ? '' : 'You have the newest version.')
    } catch (e) {
      setMsg((e as Error).message)
    }
  }
  const install = async () => {
    if (!found || progress !== null) return
    setMsg('')
    setProgress(0)
    try {
      await installUpdate(found, setProgress)
      setMsg('Follow the prompt on screen to finish updating.')
    } catch (e) {
      setMsg((e as Error).message)
    } finally {
      setProgress(null)
    }
  }

  return (
    <>
      <View style={st.facts}>
        <Text style={st.label}>Signed in as</Text>
        <Text style={st.value}>{s?.user.profileName ?? s?.user.username}</Text>
        <Text style={st.label}>Server</Text>
        <Text style={st.value}>{s?.server.replace(/^https?:\/\//, '')}</Text>
        <Text style={st.label}>App version</Text>
        <Text style={st.value}>{installed.name}</Text>
      </View>
      {found ? (
        <Focusable onPress={install} style={st.update}>
          <Text style={st.value}>
            {progress !== null ? `Downloading… ${Math.round(progress * 100)}%` : `Update to version ${found.version}`}
          </Text>
          <Text style={st.hint}>
            {Math.round(found.size / 1e6)} MB · {isPhone ? 'tap to download, then confirm the install.' : 'press OK to download, then confirm the install on the TV.'}
          </Text>
        </Focusable>
      ) : selfUpdates ? (
        <View style={st.buttons}>
          <Button label="Check for updates" onPress={check} />
        </View>
      ) : (
        <Text style={st.hint}>Google Play keeps this app up to date.</Text>
      )}
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      {!isPhone && (
        <Focusable onPress={() => menu.setPosition(menu.position === 'side' ? 'top' : 'side')} style={st.option}>
          <Text style={st.value}>Menu: {menu.position === 'side' ? 'Down the side' : 'Across the top'}</Text>
          <Text style={st.hint}>
            {menu.position === 'side'
              ? 'Shows just icons until you press Left into it, like Plex.'
              : 'Always shows every name across the top.'}{' '}
            Press OK to switch.
          </Text>
        </Focusable>
      )}
      {hueMode !== null && hueSeen && (
        <Focusable onPress={cycleHueMode} style={st.option}>
          <Text style={st.value}>Hue Sync: {hueModeNames[hueMode]}</Text>
          <Text style={st.hint}>
            {hueMode === 'sync'
              ? 'The lights follow the colours on screen.'
              : hueMode === 'theater'
                ? 'The lights dim while something plays.'
                : 'Off for this profile. Turn it back on here now, or later in Account on the web.'}{' '}
            {isPhone ? 'Tap' : 'Press OK'} for the next one.
          </Text>
        </Focusable>
      )}
      {hue && (
        <Focusable onPress={toggleHue} style={st.option}>
          <Text style={st.value}>Hue Sync on this {isPhone ? 'phone' : 'TV'}: {hueOn ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {hueOn ? 'The Hue lights follow what plays on this device.' : 'The Hue lights leave this device alone.'} {isPhone ? 'Tap' : 'Press OK'} to turn it {hueOn ? 'off' : 'on'}.
          </Text>
        </Focusable>
      )}
      {hue && hueOn && areas.length > 0 && (
        <Focusable onPress={nextArea} style={st.option}>
          <Text style={st.value}>Hue lights: {areaName}</Text>
          <Text style={st.hint}>
            The room or zone this {isPhone ? 'phone' : 'TV'} is in: all its lights follow the picture, and other rooms can sync their own. {isPhone ? 'Tap' : 'Press OK'} for the next one.
          </Text>
        </Focusable>
      )}
      {lights && (
        <Focusable onPress={() => saveLights({ ...lights.person, enabled: !lights.person.enabled, off: false })} style={st.option}>
          <Text style={st.value}>Game Lights for {s?.user.profileName ?? s?.user.username}: {lights.person.enabled ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {lights.person.enabled
              ? 'The Hue lights flash when your teams score. More choices are under Game Lights in Sports.'
              : 'Flash the Hue lights when the teams you follow in Sports score. Off until you turn it on.'}{' '}
            {isPhone ? 'Tap' : 'Press OK'} to turn it {lights.person.enabled ? 'off' : 'on'}.
          </Text>
          {!!lights.person.enabled && !lights.paired && <Text style={st.error}>An admin still needs to pair the Hue bridge on the Game Lights setup page.</Text>}
        </Focusable>
      )}
      {lights?.person.enabled && lights.paired && (
        <Focusable onPress={nextLightsArea} style={st.option}>
          <Text style={st.value}>Game Lights room: {lightsAreaName}</Text>
          <Text style={st.hint}>The room you watch games in; its lights flash for your profile. {isPhone ? 'Tap' : 'Press OK'} for the next one.</Text>
        </Focusable>
      )}
      {recs && recs.enabled !== false && (
        <Focusable onPress={toggleRecs} style={st.option}>
          <Text style={st.value}>Recommendations to me: {recs.accept ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {recs.accept
              ? 'Other people here can recommend movies and shows to you; they show up in a row on Home.'
              : 'Nobody can recommend titles to you.'}{' '}
            {isPhone ? 'Tap' : 'Press OK'} to turn it {recs.accept ? 'off' : 'on'}.
          </Text>
        </Focusable>
      )}
      <View style={st.signOut}>
        {hasProfiles && <Button label="Switch profile" onPress={onSwitchProfile} style={st.gap} />}
        <Button label="Sign out" onPress={onSignOut} />
      </View>
      <Text style={st.hint}>Signing out also lets you pick a different server or sign in as someone else.</Text>
    </>
  )
}

function MenuSection({ menu }: { menu: MenuChoices }) {
  return (
    <>
      <Text style={st.hint}>Home, Search and Settings always show. {isPhone ? 'Tap' : 'Press OK on'} a library or tab to pin or unpin it.</Text>
      {menu.libraries.map((l) => (
        <Focusable key={l.id} onPress={() => menu.togglePin(l)} style={StyleSheet.flatten([st.option, st.optionRow])}>
          <Icon name={l.pinned ? 'pinned' : 'pin'} size={px(44)} color={l.pinned ? colors.accent : colors.muted} />
          <View style={st.optionText}>
            <Text style={st.value}>{l.name}</Text>
            <Text style={st.hint}>{l.pinned ? 'Has a tab in the menu and fills your Home.' : 'Only under More, and left out of Home.'}</Text>
          </View>
        </Focusable>
      ))}
      {menu.tabs.map((t) => (
        <Focusable key={t} onPress={() => menu.toggleTab(t)} style={StyleSheet.flatten([st.option, st.optionRow])}>
          <Icon name={menu.hidden.includes(t) ? 'pin' : 'pinned'} size={px(44)} color={menu.hidden.includes(t) ? colors.muted : colors.accent} />
          <View style={st.optionText}>
            <Text style={st.value}>{hideableNames[t]}</Text>
            <Text style={st.hint}>{t === 'more' ? 'Lists every library and tab to open, pin or unpin.' : menu.hidden.includes(t) ? 'Only under More.' : 'Has a tab in the menu.'}</Text>
          </View>
        </Focusable>
      ))}
    </>
  )
}

interface AlertPrefs {
  gameAlertMinutes: number
  notifyGameScores: boolean
  scoreBug: boolean
  liveAlertMinutes: number
  liveFollows?: string[] | null
  notifyTogether: boolean
  quietWhilePlaying: boolean
}

const minutesLabel = (m: number) => (m === 0 ? 'Off' : m < 60 ? `${m} minutes before` : m === 60 ? 'An hour before' : '2 hours before')

/** This profile's pop-ups and alerts, saved with its other preferences. */
function AlertsSection() {
  const [p, setP] = useState<AlertPrefs | null>(null)
  const [msg, setMsg] = useState('')
  useEffect(() => {
    api.get<AlertPrefs>('/api/me/prefs').then(setP, (e) => setMsg(e.message))
  }, [])
  if (!p) return <Text style={st.hint}>{msg || 'Loading…'}</Text>
  const save = (patch: Partial<AlertPrefs>) => {
    const before = p
    setP({ ...p, ...patch })
    // The rest of the preferences go back as they are.
    api
      .get<AlertPrefs>('/api/me/prefs')
      .then((cur) => api.put<AlertPrefs>('/api/me/prefs', { ...cur, ...patch }))
      .then(() => api.get('/api/me/prefs'))
      .catch((e) => {
        setP(before)
        setMsg(e.message)
      })
  }
  const cycle = (list: number[], now: number) => list[(list.indexOf(now) + 1) % list.length]
  const press = isPhone ? 'Tap' : 'Press OK'
  return (
    <>
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      <Focusable onPress={() => save({ gameAlertMinutes: cycle([0, 5, 15, 30, 60, 120], p.gameAlertMinutes) })} style={st.option}>
        <Text style={st.value}>Before my teams' games: {minutesLabel(p.gameAlertMinutes)}</Text>
        <Text style={st.hint}>A pop-up before games of the teams you follow in Sports. {press} for the next choice.</Text>
      </Focusable>
      <Focusable onPress={() => save({ notifyGameScores: !p.notifyGameScores })} style={st.option}>
        <Text style={st.value}>Final scores: {p.notifyGameScores ? 'On' : 'Off'}</Text>
        <Text style={st.hint}>The score when your teams' games end. {press} to turn it {p.notifyGameScores ? 'off' : 'on'}.</Text>
      </Focusable>
      <Focusable onPress={() => save({ scoreBug: !p.scoreBug })} style={st.option}>
        <Text style={st.value}>Score bug: {p.scoreBug ? 'On' : 'Off'}</Text>
        <Text style={st.hint}>Your teams' live score in a corner of the player while you watch something else. {press} to turn it {p.scoreBug ? 'off' : 'on'}.</Text>
      </Focusable>
      <Focusable onPress={() => save({ liveAlertMinutes: cycle([0, 5, 10, 15, 30, 60], p.liveAlertMinutes) })} style={st.option}>
        <Text style={st.value}>Before Live TV shows I follow: {minutesLabel(p.liveAlertMinutes)}</Text>
        <Text style={st.hint}>
          {p.liveFollows?.length ? `Following ${p.liveFollows.join(', ')}.` : 'Not following any shows yet.'} Hold OK on a show in the guide to follow it. {press} for the next choice.
        </Text>
      </Focusable>
      <Focusable onPress={() => save({ notifyTogether: !p.notifyTogether })} style={st.option}>
        <Text style={st.value}>Watch together invitations: {p.notifyTogether ? 'On' : 'Off'}</Text>
        <Text style={st.hint}>{p.notifyTogether ? 'People can invite you to watch with them.' : "Nobody can invite you, and you aren't listed when they try."} {press} to turn it {p.notifyTogether ? 'off' : 'on'}.</Text>
      </Focusable>
      <Focusable onPress={() => save({ quietWhilePlaying: !p.quietWhilePlaying })} style={st.option}>
        <Text style={st.value}>Pop-ups while watching: {p.quietWhilePlaying ? 'Wait until I stop' : 'Show them'}</Text>
        <Text style={st.hint}>Alerts and invitations over a movie, show or channel. {press} to switch.</Text>
      </Focusable>
    </>
  )
}

type Guard = (change: () => Promise<unknown>) => Promise<boolean>

/**
 * Who's watching, PINs and the settings passcode. The account manages its
 * profiles' PINs and which member the app opens as; a profile sees only its
 * own PIN. Anyone can have this device open straight as them.
 */
function ProfilesSection({ guard }: { guard: Guard }) {
  const [members, setMembers] = useState<Member[] | null>(null)
  const [passcode, setPasscode] = useState<{ set: boolean; unlocked: boolean } | null>(null)
  const [msg, setMsg] = useState('')
  const [entering, setEntering] = useState<{ title: string; save: (pin: string) => Promise<unknown> } | null>(null)
  const load = useCallback(() => {
    fetchHousehold().then(setMembers, (e) => setMsg(e.message))
    api.get<{ set: boolean; unlocked: boolean }>('/api/me/passcode').then(setPasscode, () => undefined)
  }, [])
  useEffect(load, [load])
  const run = (change: () => Promise<unknown>, done: string) =>
    guard(change).then(
      (ok) => {
        if (ok) setMsg(done)
        load()
      },
      (e) => setMsg((e as Error).message),
    )
  const me = members?.find((m) => m.current)
  const owner = !!me?.isOwner
  const household = !!members && members.length > 1
  // This device opens as whoever is signed in, skipping "Who's watching?".
  // Switching profile here starts a new session without it, so the next
  // person to open the app is asked (and for the PIN) again.
  const [stay, setStay] = useState(() => !!current()?.profileChosen)
  const toggleStay = async () => {
    const s = current()
    if (!s) return
    await saveSession({ ...s, profileChosen: !stay })
    setStay(!stay)
    setMsg(!stay ? `${isPhone ? 'This phone' : 'This TV'} opens as ${me?.name ?? 'you'} now.` : "Asks who's watching when the app opens.")
  }
  const auto = members?.find((m) => m.auto)
  // OK steps through "Ask who's watching" and each member.
  const nextAuto = () => {
    if (!members) return
    const order = [null, ...members]
    const i = order.findIndex((m) => (m?.id ?? 0) === (auto?.id ?? 0))
    const next = order[(i + 1) % order.length]
    run(() => api.put('/api/household/autoselect', { memberId: next?.id ?? 0 }), next ? `Opens as ${next.name} now.` : "Asks who's watching now.")
  }
  if (entering)
    return (
      <PinPad
        title={entering.title}
        error=""
        onEnter={(pin) => {
          const save = entering.save
          setEntering(null)
          run(() => save(pin), 'Saved.')
        }}
        onCancel={() => setEntering(null)}
      />
    )
  return (
    <>
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      {household && me && (
        <Focusable onPress={toggleStay} style={st.option}>
          <Text style={st.value}>
            Sign in as {me.name} on {isPhone ? 'this phone' : 'this TV'}: {stay ? 'On' : 'Off'}
          </Text>
          <Text style={st.hint}>
            {stay
              ? `Opens straight as ${me.name}, without "Who's watching?" or a PIN. Switching profile turns it off.`
              : `Skip "Who's watching?" here and open as ${me.name} every time.`}{' '}
            {isPhone ? 'Tap' : 'Press OK'} to turn it {stay ? 'off' : 'on'}.
          </Text>
        </Focusable>
      )}
      {owner && household && (
        <Focusable onPress={nextAuto} style={st.option}>
          <Text style={st.value}>When the app opens: {auto ? `sign in as ${auto.name}` : "ask who's watching"}</Text>
          <Text style={st.hint}>
            Signs straight in as one profile on every device{auto?.hasPin ? ' (it still asks for its PIN)' : ''}. {isPhone ? 'Tap' : 'Press OK'} for the next choice.
          </Text>
        </Focusable>
      )}
      {owner &&
        members
          ?.filter((m) => !m.isOwner)
          .map((m) => (
            <View key={m.id} style={st.option}>
              <Text style={st.value}>{m.name}</Text>
              <Text style={st.hint}>{m.hasPin ? 'Has a PIN to switch to it.' : 'No PIN: anyone can switch to it.'}</Text>
              <View style={st.buttons}>
                <Button small label={m.hasPin ? 'Change PIN' : 'Add a PIN'} onPress={() => setEntering({ title: `A 4 to 8 digit PIN for ${m.name}`, save: (pin) => api.put(`/api/household/${m.id}/pin`, { pin }) })} />
                {m.hasPin && <Button small label="Remove PIN" onPress={() => run(() => api.put(`/api/household/${m.id}/pin`, { pin: '' }), `${m.name} has no PIN now.`)} />}
              </View>
            </View>
          ))}
      {passcode && household && me && (
        <View style={st.option}>
          <Text style={st.value}>Your PIN ({me.name}): {passcode.set ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {passcode.set
              ? `Switching to ${me.name} asks for it, and so does changing ${owner ? 'settings, profiles and the server' : 'your settings'}.`
              : `Anyone can switch to ${me.name} now. Add a PIN so only you can.`}
          </Text>
          <View style={st.buttons}>
            <Button small label={passcode.set ? 'Change PIN' : 'Add a PIN'} onPress={() => setEntering({ title: 'Choose a 4 to 8 digit PIN', save: (code) => api.put('/api/me/passcode', { passcode: code }) })} />
            {passcode.set && <Button small label="Remove PIN" onPress={() => run(() => api.del('/api/me/passcode'), `${me.name} has no PIN now.`)} />}
            {passcode.set && passcode.unlocked && <Button small label="Lock now" onPress={() => run(() => api.post('/api/me/passcode/lock'), 'Locked.')} />}
          </View>
        </View>
      )}
      {passcode && !household && (
        <View style={st.option}>
          <Text style={st.value}>Settings passcode: {passcode.set ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {passcode.set
              ? 'Changing settings, profiles and the server asks for it first.'
              : 'Ask for a code before anyone changes settings on this account, so kids can\'t.'}
          </Text>
          <View style={st.buttons}>
            <Button small label={passcode.set ? 'Change passcode' : 'Turn on'} onPress={() => setEntering({ title: 'Choose a 4 to 8 digit passcode', save: (code) => api.put('/api/me/passcode', { passcode: code }) })} />
            {passcode.set && <Button small label="Turn off" onPress={() => run(() => api.del('/api/me/passcode'), 'The passcode is off.')} />}
            {passcode.set && passcode.unlocked && <Button small label="Lock now" onPress={() => run(() => api.post('/api/me/passcode/lock'), 'Locked.')} />}
          </View>
        </View>
      )}
      {!owner && household && <Text style={st.hint}>The account that owns this profile can also change or remove your PIN.</Text>}
    </>
  )
}

interface LiveSource {
  id: number
  kind: string
  name: string
  lastRefresh: number
  lastError?: string
  entryCount: number
}

function ago(t: number) {
  if (!t) return 'never'
  const m = Math.round((Date.now() / 1000 - t) / 60)
  if (m < 2) return 'just now'
  if (m < 120) return `${m} min ago`
  return `${Math.round(m / 60)} h ago`
}

/** A few server settings worth changing from the couch; the rest are on the web. */
function AdminSection({ guard }: { guard: Guard }) {
  const [settings, setSettings] = useState<Record<string, string> | null>(null)
  const [sources, setSources] = useState<LiveSource[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [msg, setMsg] = useState('')
  const load = useCallback(() => {
    guard(() => api.get<Record<string, string>>('/api/admin/settings').then(setSettings)).catch((e) => setMsg(e.message))
    api.get<LiveSource[]>('/api/admin/live/sources').then(setSources, () => undefined)
    api.get<Library[]>('/api/libraries').then(setLibraries, () => undefined)
  }, [guard])
  useEffect(load, [load])
  const run = (change: () => Promise<unknown>, done: string) =>
    guard(change).then(
      (ok) => {
        if (ok) setMsg(done)
        load()
      },
      (e) => setMsg((e as Error).message),
    )
  const num = (key: string, def: number) => Number(settings?.[key] ?? def) || 0
  const step = (key: string, value: number) => run(() => api.put('/api/admin/settings', { [key]: String(value) }), 'Saved.')
  const days = num('continue_watching_days', 0)
  const pct = num('watched_percent', 90)
  return (
    <>
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      <Text style={st.hint}>Everything else is under Admin on the web.</Text>
      {settings && (
        <>
          <View style={st.option}>
            <Text style={st.value}>Keep titles in Continue Watching: {days ? `${days} days` : 'until finished'}</Text>
            <Text style={st.hint}>Titles nobody has played for this long drop out of Continue Watching and On Deck.</Text>
            <View style={st.buttons}>
              {[0, 14, 30, 60, 90, 180].map((d) => (
                <Button small key={d} primary={d === days} label={d ? `${d} days` : 'Never'} onPress={() => step('continue_watching_days', d)} />
              ))}
            </View>
          </View>
          <View style={st.option}>
            <Text style={st.value}>Count as watched after {pct}%</Text>
            <View style={st.buttons}>
              <Button small label="− 5" onPress={() => step('watched_percent', Math.max(50, pct - 5))} />
              <Button small label="+ 5" onPress={() => step('watched_percent', Math.min(100, pct + 5))} />
            </View>
          </View>
        </>
      )}
      {sources.length > 0 && <Text style={st.section}>Live TV sources</Text>}
      {sources.map((src) => (
        <View key={src.id} style={StyleSheet.flatten([st.option, st.optionRow])}>
          <View style={st.optionText}>
            <Text style={st.value}>{src.name}</Text>
            <Text style={st.hint}>
              {src.kind === 'epg' ? 'Guide' : 'Playlist'} · {src.entryCount} {src.kind === 'epg' ? 'programmes' : 'streams'} · refreshed {ago(src.lastRefresh)}
            </Text>
            {!!src.lastError && <Text style={st.error}>Last refresh failed: {src.lastError}</Text>}
          </View>
          <Button small label="Refresh" onPress={() => run(() => api.post(`/api/admin/live/sources/${src.id}/refresh`), `Refreshing ${src.name}…`)} />
        </View>
      ))}
      {libraries.length > 0 && <Text style={st.section}>Libraries</Text>}
      {libraries.map((l) => (
        <View key={l.id} style={StyleSheet.flatten([st.option, st.optionRow])}>
          <View style={st.optionText}>
            <Text style={st.value}>{l.name}</Text>
          </View>
          <Button small label="Scan for new files" onPress={() => run(() => api.post(`/api/admin/libraries/${l.id}/scan`), `Scanning ${l.name}…`)} />
        </View>
      ))}
    </>
  )
}

const st = StyleSheet.create(scaled({
  screen: { paddingHorizontal: gutter, paddingTop: 30, paddingBottom: 40 },
  facts: { backgroundColor: colors.surface, borderRadius: 22, padding: 40, maxWidth: 1100 },
  label: { color: colors.muted, fontSize: font.small, marginTop: 14 },
  value: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginTop: 4 },
  update: { backgroundColor: colors.surface2, borderRadius: 22, padding: 30, maxWidth: 1100, marginTop: 24, borderWidth: 2, borderColor: colors.accent },
  option: { backgroundColor: colors.surface, borderRadius: 22, padding: 30, maxWidth: 1100, marginTop: 24 },
  buttons: { flexDirection: 'row', marginTop: 28 },
  signOut: { flexDirection: 'row', marginTop: 48 },
  gap: { marginRight: 24 },
  section: { color: colors.text, fontSize: font.title, fontWeight: '800', marginTop: 48 },
  msg: { color: colors.text, fontSize: font.body, marginTop: 18 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 14 },
  error: { color: colors.danger, fontSize: font.small, marginTop: 8 },
  sections: { flexDirection: 'row', flexWrap: 'wrap', marginBottom: 24 },
  chip: { paddingHorizontal: 30, paddingVertical: 12, borderRadius: 40, marginRight: 16, marginBottom: 8, backgroundColor: colors.surface2 },
  chipActive: { backgroundColor: colors.surface3 },
  chipText: { color: colors.muted, fontSize: font.body, fontWeight: '700' },
  chipTextActive: { color: colors.text },
  optionRow: { flexDirection: 'row', alignItems: 'center' },
  optionText: { flex: 1, marginLeft: 24, marginRight: 24 },
  modal: { flex: 1, backgroundColor: colors.bg },
}))
