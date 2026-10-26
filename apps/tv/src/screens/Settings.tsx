import React, { useEffect, useState } from 'react'
import { ScrollView, StyleSheet, Text, View } from 'react-native'
import { api, current } from '../api'
import { Button, Focusable } from '../components'
import { hueArea, hueSyncOn, setHueArea, setHueSync } from '../devicePrefs'
import { scaled, colors, font, gutter, isPhone } from '../theme'
import type { Library } from '../types'
import { checkForUpdate, installed, installUpdate, type AppRelease } from '../updates'
import { fetchHousehold } from './Profiles'

// The tabs a person can turn off (prefs.hiddenTabs). Home, Search and Settings
// always show, so there's always a way back to turn them on.
export type HideableTab = 'sports' | 'live' | 'more'
export const hideableNames: Record<HideableTab, string> = { sports: 'Sports', live: 'Live TV', more: 'More' }

/** What the menu shows, and how to change it; Home owns both. */
export interface MenuChoices {
  libraries: Library[]
  hidden: HideableTab[]
  tabs: HideableTab[] // the ones this person has at all (Live TV can be off for them)
  togglePin: (l: Library) => void
  toggleTab: (t: HideableTab) => void
}

export default function Settings({ onSignOut, onSwitchProfile, update, menu }: { onSignOut: () => void; onSwitchProfile: () => void; update: AppRelease | null; menu: MenuChoices }) {
  const s = current()
  const [hasProfiles, setHasProfiles] = useState(false)
  useEffect(() => {
    fetchHousehold().then((m) => setHasProfiles(m.length > 1), () => undefined)
  }, [])
  // The Hue Sync switch only shows when the server runs the plugin.
  const [hue, setHue] = useState(false)
  const [hueOn, setHueOn] = useState(hueSyncOn)
  useEffect(() => {
    api.get<{ id: string }[]>('/api/plugins').then((ps) => setHue(ps.some((p) => p.id === 'hue-sync')), () => undefined)
  }, [])
  const toggleHue = () => {
    setHueSync(!hueOn)
    setHueOn(!hueOn)
  }
  // The Hue room or zone this TV is in, from the Hue app. OK steps through them.
  const [areas, setAreas] = useState<{ id: string; name: string }[]>([])
  const [area, setAreaState] = useState(hueArea)
  useEffect(() => {
    if (hue) api.get<{ id: string; name: string }[]>('/api/plugins/hue-sync/areas').then(setAreas, () => undefined)
  }, [hue])
  const options = [{ id: '', name: 'Every light' }, ...areas]
  const areaName = options.find((a) => a.id === area)?.name ?? 'Every light'
  const nextArea = () => {
    const i = options.findIndex((a) => a.id === area)
    const id = options[(i + 1) % options.length].id
    setHueArea(id)
    setAreaState(id)
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
    <ScrollView contentContainerStyle={st.screen}>
      <View style={st.facts}>
        <Text style={st.label}>Signed in as</Text>
        <Text style={st.value}>{s?.user.profileName ?? s?.user.username}</Text>
        <Text style={st.label}>Server</Text>
        <Text style={st.value}>{s?.server.replace(/^https?:\/\//, '')}</Text>
        <Text style={st.label}>App version</Text>
        <Text style={st.value}>{installed.name}</Text>
      </View>
      {/* The update comes first and full width, so Down from the tabs lands on it rather than Sign out. */}
      {found ? (
        <Focusable onPress={install} style={st.update}>
          <Text style={st.value}>
            {progress !== null ? `Downloading… ${Math.round(progress * 100)}%` : `Update to version ${found.version}`}
          </Text>
          <Text style={st.hint}>
            {Math.round(found.size / 1e6)} MB · {isPhone ? 'tap to download, then confirm the install.' : 'press OK to download, then confirm the install on the TV.'}
          </Text>
        </Focusable>
      ) : (
        <View style={st.buttons}>
          <Button label="Check for updates" onPress={check} />
        </View>
      )}
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      {hue && (
        <Focusable onPress={toggleHue} style={st.option}>
          <Text style={st.value}>Hue Sync: {hueOn ? 'On' : 'Off'}</Text>
          <Text style={st.hint}>
            {hueOn ? 'The Hue lights follow what plays on this device.' : 'The Hue lights leave this device alone.'} {isPhone ? 'Tap' : 'Press OK'} to turn it {hueOn ? 'off' : 'on'}.
          </Text>
        </Focusable>
      )}
      {hue && hueOn && areas.length > 0 && (
        <Focusable onPress={nextArea} style={st.option}>
          <Text style={st.value}>Hue lights: {areaName}</Text>
          <Text style={st.hint}>
            The room or zone this {isPhone ? 'phone' : 'TV'} is in, so other rooms can sync their own lights. {isPhone ? 'Tap' : 'Press OK'} for the next one.
          </Text>
        </Focusable>
      )}
      <Text style={st.section}>Menu</Text>
      <Text style={st.hint}>Home, Search and Settings always show. {isPhone ? 'Tap' : 'Press OK on'} a library or tab to change it.</Text>
      {menu.libraries.map((l) => (
        <Focusable key={l.id} onPress={() => menu.togglePin(l)} style={st.option}>
          <Text style={st.value}>{l.name}: {l.pinned ? 'Pinned' : 'Not pinned'}</Text>
          <Text style={st.hint}>{l.pinned ? 'Has a tab in the menu and fills your Home.' : 'Only under More, and left out of Home.'}</Text>
        </Focusable>
      ))}
      {menu.tabs.map((t) => (
        <Focusable key={t} onPress={() => menu.toggleTab(t)} style={st.option}>
          <Text style={st.value}>{hideableNames[t]} tab: {menu.hidden.includes(t) ? 'Hidden' : 'Shown'}</Text>
          {t === 'more' && <Text style={st.hint}>Lists every library to open, pin or unpin.</Text>}
        </Focusable>
      ))}
      <View style={st.signOut}>
        {hasProfiles && <Button label="Switch profile" onPress={onSwitchProfile} style={st.gap} />}
        <Button label="Sign out" onPress={onSignOut} />
      </View>
      <Text style={st.hint}>Signing out also lets you pick a different server.</Text>
    </ScrollView>
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
}))
