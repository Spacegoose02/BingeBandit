import React, { useEffect, useRef, useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { colors, font, scaled } from './theme'

const PIN_LENGTH = 2

/**
 * A child lock for the phone players: lock with a 2-digit PIN you choose on
 * the spot; while locked, taps and Back do nothing except show a small badge,
 * and the same PIN unlocks.
 */
export function useLock() {
  const [pin, setPin] = useState<string | null>(null)
  const [prompt, setPrompt] = useState<'set' | 'unlock' | null>(null)
  const [badge, setBadge] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current)
    },
    [],
  )

  const locked = pin !== null
  /** Called for taps (and Back) while locked: show the unlock badge for a moment. */
  const poke = () => {
    setBadge(true)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => setBadge(false), 3000)
  }

  const layer = (
    <>
      {locked && badge && !prompt && (
        <Pressable style={st.badge} onPress={() => setPrompt('unlock')} hitSlop={20}>
          <Text style={st.badgeText}>Locked · tap here to unlock</Text>
        </Pressable>
      )}
      {prompt && (
        <Keypad
          title={prompt === 'set' ? 'Choose a 2-digit PIN to unlock with' : 'Enter the PIN to unlock'}
          check={prompt === 'unlock' ? (p) => p === pin : undefined}
          onDone={(p) => {
            setPrompt(null)
            setBadge(false)
            setPin(prompt === 'set' ? p : null)
          }}
          onCancel={() => setPrompt(null)}
        />
      )}
    </>
  )

  return {
    locked,
    /** Whether Back should be swallowed: while locked, or to close the keypad. */
    handleBack: () => {
      if (prompt) setPrompt(null)
      else if (locked) poke()
      else return false
      return true
    },
    startLock: () => setPrompt('set'),
    poke,
    layer,
  }
}

function Keypad({ title, check, onDone, onCancel }: { title: string; check?: (pin: string) => boolean; onDone: (pin: string) => void; onCancel: () => void }) {
  const [digits, setDigits] = useState('')
  const [wrong, setWrong] = useState(false)
  const press = (d: string) => {
    const next = digits + d
    setWrong(false)
    if (next.length < PIN_LENGTH) return setDigits(next)
    if (check && !check(next)) {
      setWrong(true)
      setDigits('')
      return
    }
    onDone(next)
  }
  const keys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '', '0', '⌫']
  return (
    <Pressable style={st.shade} onPress={() => undefined}>
      <View style={st.pad}>
        <Text style={st.title}>{title}</Text>
        <View style={st.dots}>
          {Array.from({ length: PIN_LENGTH }, (_, i) => (
            <View key={i} style={[st.dot, i < digits.length && st.dotOn]} />
          ))}
        </View>
        <Text style={st.wrong}>{wrong ? 'Wrong PIN, try again' : ' '}</Text>
        <View style={st.grid}>
          {keys.map((k, i) =>
            k ? (
              <Pressable
                key={i}
                style={({ pressed }) => [st.key, pressed && st.keyPressed]}
                onPress={() => (k === '⌫' ? setDigits(digits.slice(0, -1)) : press(k))}
              >
                <Text style={st.keyText}>{k}</Text>
              </Pressable>
            ) : (
              <View key={i} style={st.keyBlank} />
            ),
          )}
        </View>
        <Pressable onPress={onCancel} hitSlop={16}>
          <Text style={st.cancel}>Cancel</Text>
        </Pressable>
      </View>
    </Pressable>
  )
}

const st = StyleSheet.create(scaled({
  badge: { position: 'absolute', bottom: 60, alignSelf: 'center', backgroundColor: 'rgba(11,13,16,0.85)', borderRadius: 40, paddingHorizontal: 36, paddingVertical: 18 },
  badgeText: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  shade: { ...StyleSheet.absoluteFillObject, backgroundColor: 'rgba(0,0,0,0.75)', alignItems: 'center', justifyContent: 'center' },
  // Sized to fit a sideways phone (~750 canvas units tall).
  pad: { backgroundColor: colors.surface, borderRadius: 24, paddingHorizontal: 36, paddingVertical: 28, alignItems: 'center', width: 520 },
  title: { color: colors.text, fontSize: font.body, fontWeight: '700', textAlign: 'center' },
  dots: { flexDirection: 'row', marginTop: 18 },
  dot: { width: 26, height: 26, borderRadius: 13, borderWidth: 3, borderColor: colors.muted, marginHorizontal: 12 },
  dotOn: { backgroundColor: colors.accent, borderColor: colors.accent },
  // Full width, so the message never wraps onto a hidden second line.
  wrong: { color: colors.danger, fontSize: font.small, marginTop: 8, alignSelf: 'stretch', textAlign: 'center' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', width: 400, marginTop: 4 }, // 3 keys of 116 + 2×6 margin, with room for rounding
  key: { width: 116, height: 72, margin: 6, borderRadius: 16, backgroundColor: colors.surface3, alignItems: 'center', justifyContent: 'center' },
  keyPressed: { backgroundColor: colors.accent },
  keyBlank: { width: 116, height: 72, margin: 6 },
  keyText: { color: colors.text, fontSize: font.title, fontWeight: '700' },
  cancel: { color: colors.muted, fontSize: font.body, marginTop: 14 },
}))
