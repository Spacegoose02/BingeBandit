import React from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { current } from '../api'
import { Button } from '../components'
import { scaled, colors, font, gutter } from '../theme'

// Matches android/app/build.gradle versionName via package.json at build time.
const appVersion: string = require('../../package.json').version

export default function Settings({ onSignOut }: { onSignOut: () => void }) {
  const s = current()
  return (
    <View style={st.screen}>
      <View style={st.facts}>
        <Text style={st.label}>Signed in as</Text>
        <Text style={st.value}>{s?.user.username}</Text>
        <Text style={st.label}>Server</Text>
        <Text style={st.value}>{s?.server.replace(/^https?:\/\//, '')}</Text>
        <Text style={st.label}>App version</Text>
        <Text style={st.value}>{appVersion}</Text>
      </View>
      <View style={st.buttons}>
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
  buttons: { flexDirection: 'row', marginTop: 28 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 14 },
}))
