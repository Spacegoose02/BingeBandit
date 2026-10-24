import React, { useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { current } from '../api'
import { Button, Focusable } from '../components'
import { scaled, colors, font, gutter } from '../theme'
import { checkForUpdate, installed, installUpdate, type AppRelease } from '../updates'

export default function Settings({ onSignOut, update }: { onSignOut: () => void; update: AppRelease | null }) {
  const s = current()
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
    <View style={st.screen}>
      <View style={st.facts}>
        <Text style={st.label}>Signed in as</Text>
        <Text style={st.value}>{s?.user.username}</Text>
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
          <Text style={st.hint}>{Math.round(found.size / 1e6)} MB · press OK to download, then confirm the install on the TV.</Text>
        </Focusable>
      ) : (
        <View style={st.buttons}>
          <Button label="Check for updates" onPress={check} />
        </View>
      )}
      {!!msg && <Text style={st.msg}>{msg}</Text>}
      <View style={st.signOut}>
        <Button label="Sign out" onPress={onSignOut} />
      </View>
      <Text style={st.hint}>Signing out also lets you pick a different server.</Text>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, paddingHorizontal: gutter, paddingTop: 30 },
  facts: { backgroundColor: colors.surface, borderRadius: 22, padding: 40, maxWidth: 1100 },
  label: { color: colors.muted, fontSize: font.small, marginTop: 14 },
  value: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginTop: 4 },
  update: { backgroundColor: colors.surface2, borderRadius: 22, padding: 30, maxWidth: 1100, marginTop: 24, borderWidth: 2, borderColor: colors.accent },
  buttons: { flexDirection: 'row', marginTop: 28 },
  signOut: { flexDirection: 'row', marginTop: 48 },
  msg: { color: colors.text, fontSize: font.body, marginTop: 18 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 14 },
}))
