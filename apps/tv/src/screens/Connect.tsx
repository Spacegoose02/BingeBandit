import React, { useEffect, useState } from 'react'
import { ActivityIndicator, Image, StyleSheet, Text, TextInput, View } from 'react-native'
import { normalizeServer } from '../api'
import { Button, Focusable } from '../components'
import { probe, scan } from '../discovery'
import { scaled, colors, font, gutter } from '../theme'
import type { ServerInfo } from '../types'

/** First screen: find the server on the home network, or type its address. */
export default function Connect({ onChosen }: { onChosen: (s: ServerInfo) => void }) {
  const [found, setFound] = useState<ServerInfo[]>([])
  const [scanning, setScanning] = useState(true)
  const [typing, setTyping] = useState(false)
  const [address, setAddress] = useState('')
  const [error, setError] = useState('')
  const [checking, setChecking] = useState(false)

  const runScan = () => {
    setScanning(true)
    setFound([])
    scan((s) => setFound((list) => (list.some((x) => x.url === s.url) ? list : [...list, s]))).finally(() => setScanning(false))
  }
  useEffect(runScan, [])

  const tryAddress = async () => {
    if (!address.trim()) return
    setChecking(true)
    setError('')
    const url = normalizeServer(address)
    const s = await probe(url, 4000)
    setChecking(false)
    if (s) onChosen(s)
    else setError(`No BingeBandit server answered at ${url.replace(/^https?:\/\//, '')}. Check the address, and that the server is running.`)
  }

  return (
    <View style={st.screen}>
      <Image source={require('../assets/lockup.png')} style={st.logo} resizeMode="contain" />
      <Text style={st.title}>Choose your server</Text>
      {typing ? (
        <View style={st.form}>
          <Text style={st.label}>Server address (shown in BingeBandit under Settings, or ask whoever runs it)</Text>
          <TextInput
            style={st.input}
            value={address}
            onChangeText={setAddress}
            placeholder="192.168.1.10"
            placeholderTextColor={colors.muted}
            autoFocus
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
            onSubmitEditing={tryAddress}
          />
          {!!error && <Text style={st.error}>{error}</Text>}
          <View style={st.buttons}>
            <Button label={checking ? 'Checking…' : 'Connect'} primary onPress={tryAddress} />
            <Button label="Back" onPress={() => setTyping(false)} />
          </View>
        </View>
      ) : (
        <View style={st.form}>
          {found.map((s, i) => (
            <Focusable key={s.url} onPress={() => onChosen(s)} hasTVPreferredFocus={i === 0} style={st.server}>
              <Text style={st.serverName}>{s.name}</Text>
              <Text style={st.serverUrl}>{s.url.replace(/^https?:\/\//, '')}</Text>
            </Focusable>
          ))}
          {scanning && (
            <View style={st.scanning}>
              <ActivityIndicator color={colors.accent} size="large" />
              <Text style={st.muted}>Looking for servers on your network…</Text>
            </View>
          )}
          {!scanning && found.length === 0 && (
            <Text style={st.muted}>
              No servers found nearby. If your server is on a different part of the network (or reached through the internet), type its address.
            </Text>
          )}
          <View style={st.buttons}>
            <Button label="Type an address" onPress={() => setTyping(true)} preferFocus={!scanning && found.length === 0} />
            {!scanning && <Button label="Search again" onPress={runScan} />}
          </View>
        </View>
      )}
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg, alignItems: 'center', paddingTop: 90, paddingHorizontal: gutter },
  logo: { width: 640, height: 160, marginBottom: 30 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800', marginBottom: 30 },
  form: { width: 1200, maxWidth: '100%' },
  server: { backgroundColor: colors.surface2, padding: 32, marginBottom: 20 },
  serverName: { color: colors.text, fontSize: font.heading, fontWeight: '700' },
  serverUrl: { color: colors.muted, fontSize: font.small, marginTop: 4 },
  scanning: { flexDirection: 'row', alignItems: 'center', gap: 16, marginVertical: 20 },
  muted: { color: colors.muted, fontSize: font.body },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 24 },
  label: { color: colors.muted, fontSize: font.small, marginBottom: 10 },
  input: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.heading, borderRadius: 16, paddingHorizontal: 28, paddingVertical: 20, borderWidth: 2, borderColor: colors.border },
  error: { color: colors.danger, fontSize: font.body, marginTop: 12 },
}))
