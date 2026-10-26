import React, { useEffect, useState } from 'react'
import { Platform, StyleSheet, Text, TextInput, View } from 'react-native'
import QRCode from 'react-native-qrcode-svg'
import { ApiError, request, type Session } from '../api'
import { Button } from '../components'
import { px, scaled, colors, font, gutter, isPhone } from '../theme'
import type { ServerInfo, User } from '../types'

function deviceName(): string {
  const c = Platform.constants as { Model?: string; Brand?: string }
  const model = c.Model ? `${c.Brand ? c.Brand + ' ' : ''}${c.Model}` : 'Android TV'
  return model.length > 50 ? model.slice(0, 50) : model
}

/**
 * Signing in on a TV: show a code (and a QR code of the link) to approve from
 * a phone that's already signed in. A password works too.
 */
export default function SignIn({ server, onSignedIn, onBack }: { server: ServerInfo; onSignedIn: (s: Session) => void; onBack: () => void }) {
  const [link, setLink] = useState<{ code: string; pollToken: string; linkUrl: string } | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [usePassword, setUsePassword] = useState(isPhone) // phones have a keyboard; TVs show a code

  useEffect(() => {
    if (usePassword) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    setError('')
    setLink(null)
    request<{ code: string; pollToken: string; linkUrl: string }>('POST', '/api/link/start', { device: deviceName() }, server.url)
      .then((l) => {
        if (stopped) return
        setLink(l)
        const poll = async () => {
          try {
            const res = await request<{ token: string; user: User; profileChosen?: boolean } | undefined>(
              'GET',
              `/api/link/poll?token=${l.pollToken}`,
              undefined,
              server.url,
            )
            if (stopped) return
            if (res) onSignedIn({ server: server.url, token: res.token, user: res.user, profileChosen: res.profileChosen })
            else timer = setTimeout(poll, 2000)
          } catch (err) {
            if (!stopped) setError((err as Error).message)
          }
        }
        timer = setTimeout(poll, 2000)
      })
      .catch((err) => !stopped && setError((err as Error).message))
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [attempt, usePassword, server.url, onSignedIn])

  if (usePassword) return <PasswordSignIn server={server} onSignedIn={onSignedIn} onBack={() => setUsePassword(false)} onServer={onBack} />

  const shortUrl = link?.linkUrl.replace(/^https?:\/\//, '').replace(/\?code=.*$/, '')
  return (
    <View style={st.screen}>
      <Text style={st.title}>Sign in to {server.name}</Text>
      <View style={st.columns}>
        <View style={st.left}>
          <Text style={st.step}>{isPhone ? '1. On another device that’s signed in, go to' : '1. On your phone or computer, go to'}</Text>
          <Text style={st.url}>{shortUrl ?? '…'}</Text>
          <Text style={st.step}>2. Enter this code</Text>
          <Text style={st.code}>{link ? `${link.code.slice(0, 3)}-${link.code.slice(3)}` : '······'}</Text>
          {error ? (
            <>
              <Text style={st.error}>{error}</Text>
              <View style={st.buttons}>
                <Button label="Get a new code" primary preferFocus onPress={() => setAttempt((a) => a + 1)} />
              </View>
            </>
          ) : (
            <Text style={st.muted}>Waiting for you to approve it. The code works for 10 minutes.</Text>
          )}
        </View>
        {link && !isPhone && ( // nobody scans their own screen
          <View style={st.qr}>
            <QRCode value={link.linkUrl} size={px(380)} backgroundColor="#ffffff" color="#0b0d10" />
            <Text style={st.qrHint}>Or scan this with your phone's camera</Text>
          </View>
        )}
      </View>
      <View style={st.buttons}>
        <Button label="Use a password instead" preferFocus={!error} onPress={() => setUsePassword(true)} />
        <Button label="Different server" onPress={onBack} />
      </View>
    </View>
  )
}

function PasswordSignIn({
  server,
  onSignedIn,
  onBack,
  onServer,
}: {
  server: ServerInfo
  onSignedIn: (s: Session) => void
  onBack: () => void
  onServer: () => void
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [needsCode, setNeedsCode] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    setError('')
    try {
      const res = await request<{ token: string; user: User }>(
        'POST',
        '/api/auth/login',
        { username, password, code: needsCode ? code : undefined, device: deviceName() },
        server.url,
      )
      onSignedIn({ server: server.url, token: res.token, user: res.user })
    } catch (err) {
      if (err instanceof ApiError && err.data?.needsCode) {
        if (needsCode) setError(err.message)
        setNeedsCode(true)
      } else setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <View style={st.screen}>
      <Text style={st.title}>Sign in to {server.name}</Text>
      <View style={st.form}>
        {needsCode ? (
          <>
            <Text style={st.label}>Code from your authenticator app</Text>
            <TextInput style={st.input} value={code} onChangeText={setCode} keyboardType="number-pad" autoFocus onSubmitEditing={submit} />
          </>
        ) : (
          <>
            <Text style={st.label}>Username</Text>
            <TextInput style={st.input} value={username} onChangeText={setUsername} autoCapitalize="none" autoCorrect={false} autoFocus />
            <Text style={st.label}>Password</Text>
            <TextInput style={st.input} value={password} onChangeText={setPassword} secureTextEntry onSubmitEditing={submit} />
          </>
        )}
        {!!error && <Text style={st.error}>{error}</Text>}
        <View style={st.buttons}>
          <Button label={busy ? 'Signing in…' : 'Sign in'} primary onPress={submit} />
          <Button label={isPhone ? 'Use a code instead' : 'Back'} onPress={onBack} />
          {isPhone && <Button label="Different server" onPress={onServer} />}
        </View>
      </View>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flex: 1, backgroundColor: colors.bg, paddingHorizontal: isPhone ? gutter : gutter * 1.5, paddingTop: isPhone ? 40 : 70 },
  title: { color: colors.text, fontSize: font.title, fontWeight: '800', marginBottom: 40 },
  columns: { flexDirection: isPhone ? 'column' : 'row', alignItems: isPhone ? 'flex-start' : 'center' },
  left: { flex: isPhone ? 0 : 1, paddingRight: 40 },
  step: { color: colors.muted, fontSize: font.body, marginBottom: 6 },
  url: { color: colors.text, fontSize: font.title, fontWeight: '700', marginBottom: 40 },
  code: { color: colors.accent, fontSize: isPhone ? 96 : 150, fontWeight: '900', letterSpacing: isPhone ? 8 : 14, marginBottom: 24 },
  qr: { alignItems: 'center', padding: 30, backgroundColor: '#ffffff', borderRadius: 16 },
  qrHint: { color: '#333', fontSize: font.small, marginTop: 12 },
  muted: { color: colors.muted, fontSize: font.body },
  error: { color: colors.danger, fontSize: font.body, marginTop: 8 },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 36 },
  form: { width: 1100, maxWidth: '100%' },
  label: { color: colors.muted, fontSize: font.small, marginBottom: 8, marginTop: 14 },
  input: { backgroundColor: colors.surface2, color: colors.text, fontSize: font.heading, borderRadius: 16, paddingHorizontal: 28, paddingVertical: 18, borderWidth: 2, borderColor: colors.border },
}))
