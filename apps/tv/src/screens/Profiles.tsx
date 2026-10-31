import React, { useEffect, useState } from 'react'
import { ActivityIndicator, Image, ScrollView, StyleSheet, Text, View } from 'react-native'
import { ApiError, api, current, image, saveSession, type Session } from '../api'
import { Button, Focusable } from '../components'
import { backInterceptor } from '../nav'
import { colors, font, scaled } from '../theme'
import type { Member, User } from '../types'

/** The household's members: the account and its profiles. */
export function fetchHousehold(): Promise<Member[]> {
  return api.get<Member[]>('/api/household')
}

/**
 * Decides whether opening the app needs "Who's watching?". It doesn't for a
 * single member, or when the account chose a member to open as (switching to
 * it here unless it has a PIN). Resolves true when the picker should show.
 */
export async function needsPicker(): Promise<boolean> {
  const members = await fetchHousehold().catch(() => [] as Member[])
  if (members.length < 2) return false
  const auto = members.find((m) => m.auto)
  if (auto?.current) return false
  if (!auto || auto.hasPin) return true
  const s = current()
  if (!s) return true
  try {
    const res = await api.post<{ token: string; user: User }>(`/api/household/${auto.id}/switch`, { pin: '' })
    await saveSession({ server: s.server, alternates: s.alternates, serverId: s.serverId, token: res.token, user: res.user })
    return false
  } catch {
    return true
  }
}

/**
 * "Who's watching?": switches this device to another member of the
 * household, asking for that member's PIN when it has one.
 */
export default function Profiles({ onDone, onCancel }: { onDone: (s: Session) => void; onCancel?: () => void }) {
  const [members, setMembers] = useState<Member[] | null>(null)
  const [asking, setAsking] = useState<Member | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    fetchHousehold().then(setMembers, (e) => setError((e as Error).message))
  }, [])

  // Back on a PIN pad goes back to the profiles (not out of the app, or out of the picker).
  useEffect(() => {
    if (!asking) return
    const back = () => {
      setAsking(null)
      setError('')
      return true
    }
    backInterceptor.current = back
    return () => {
      if (backInterceptor.current === back) backInterceptor.current = null
    }
  }, [asking])

  const choose = async (m: Member, pin = '') => {
    const s = current()
    if (!s) return
    if (m.current) return onDone(s)
    if (m.hasPin && !pin) return setAsking(m)
    try {
      const res = await api.post<{ token: string; user: User }>(`/api/household/${m.id}/switch`, { pin })
      const next = { server: s.server, alternates: s.alternates, serverId: s.serverId, token: res.token, user: res.user }
      await saveSession(next)
      onDone(next)
    } catch (e) {
      if (e instanceof ApiError && e.data?.needsPin) {
        setError('Wrong PIN, try again')
        return
      }
      setAsking(null)
      setError((e as Error).message)
    }
  }

  if (asking) {
    return (
      <PinPad
        title={`Enter ${asking.name}'s PIN`}
        error={error}
        onEnter={(pin) => {
          setError('')
          choose(asking, pin)
        }}
        onCancel={() => {
          setAsking(null)
          setError('')
        }}
      />
    )
  }

  return (
    <ScrollView contentContainerStyle={st.screen}>
      <Text style={st.title}>Who's watching?</Text>
      {!members && !error && <ActivityIndicator color={colors.accent} size="large" />}
      <View style={st.grid}>
        {members?.map((m, i) => (
          <Focusable key={m.id} onPress={() => choose(m)} hasTVPreferredFocus={m.current || (i === 0 && !members.some((x) => x.current))} style={st.card}>
            <View style={[st.avatar, m.current && st.avatarCurrent]}>
              {m.avatar ? (
                <Image source={image(m.avatar, 'w342')} style={st.avatarImg} />
              ) : (
                <Text style={st.initial}>{m.name.slice(0, 1).toUpperCase()}</Text>
              )}
            </View>
            <Text style={st.name} numberOfLines={1}>
              {m.name}
            </Text>
            <Text style={st.hint}>{m.hasPin && !m.current ? 'PIN' : ' '}</Text>
          </Focusable>
        ))}
      </View>
      {!!error && <Text style={st.error}>{error}</Text>}
      {onCancel && (
        <View style={st.buttons}>
          <Button label="Back" small onPress={onCancel} />
        </View>
      )}
    </ScrollView>
  )
}

/** A remote-friendly number pad for a 4 to 8 digit PIN. */
export function PinPad({ title, error, onEnter, onCancel }: { title: string; error: string; onEnter: (pin: string) => void; onCancel: () => void }) {
  const [digits, setDigits] = useState('')
  useEffect(() => {
    if (error) setDigits('')
  }, [error])
  const keys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '⌫', '0', 'OK']
  const press = (k: string) => {
    if (k === '⌫') return setDigits(digits.slice(0, -1))
    if (k === 'OK') return digits.length >= 4 && onEnter(digits)
    if (digits.length < 8) setDigits(digits + k)
  }
  return (
    <View style={st.center}>
      <Text style={st.title}>{title}</Text>
      <Text style={st.dots}>{digits ? '●'.repeat(digits.length) : ' '}</Text>
      <Text style={st.error}>{error || ' '}</Text>
      <View style={st.pad}>
        {keys.map((k) => (
          <Focusable key={k} onPress={() => press(k)} hasTVPreferredFocus={k === '1'} style={StyleSheet.flatten([st.key, k === 'OK' && st.keyOk])}>
            <Text style={[st.keyText, k === 'OK' && st.keyOkText]}>{k}</Text>
          </Focusable>
        ))}
      </View>
      <View style={st.buttons}>
        <Button label="Cancel" small onPress={onCancel} />
      </View>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  screen: { flexGrow: 1, alignItems: 'center', justifyContent: 'center', padding: 60 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 40 },
  title: { color: colors.text, fontSize: font.hero, fontWeight: '800', marginBottom: 40, textAlign: 'center' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'center' },
  card: { alignItems: 'center', width: 260, padding: 20, margin: 16, borderRadius: 24 },
  avatar: { width: 180, height: 180, borderRadius: 40, backgroundColor: colors.accent, alignItems: 'center', justifyContent: 'center', overflow: 'hidden' },
  avatarImg: { width: '100%', height: '100%' },
  avatarCurrent: { borderWidth: 6, borderColor: colors.text },
  initial: { color: colors.accentText, fontSize: 90, fontWeight: '800' },
  name: { color: colors.text, fontSize: font.heading, fontWeight: '700', marginTop: 18, maxWidth: 240 },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 4 },
  error: { color: colors.danger, fontSize: font.body, marginTop: 12, textAlign: 'center' },
  buttons: { flexDirection: 'row', marginTop: 30 },
  dots: { color: colors.text, fontSize: font.title, letterSpacing: 12, minHeight: 60 },
  pad: { flexDirection: 'row', flexWrap: 'wrap', width: 3 * 160, marginTop: 10 },
  key: { width: 136, height: 96, margin: 12, borderRadius: 20, backgroundColor: colors.surface3, alignItems: 'center', justifyContent: 'center' },
  keyOk: { backgroundColor: colors.accent },
  keyText: { color: colors.text, fontSize: font.title, fontWeight: '700' },
  keyOkText: { color: colors.accentText },
}))
