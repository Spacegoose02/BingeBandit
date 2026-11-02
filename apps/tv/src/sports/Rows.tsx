import React, { useState } from 'react'
import { Modal, ScrollView, StyleSheet, Text, View } from 'react-native'
import { Button, Focusable } from '../components'
import { scaled, colors, font, isPhone } from '../theme'
import type { SportsLeague } from '../types'
import { sportColors } from './hub'

/**
 * The profile's Sports rows: which leagues get a row in the Scores tab and in
 * what order. OK on a league turns its row on or off; ▲ moves it up. Leagues
 * an admin hid for the whole server aren't listed. Your teams' games still
 * show under Your teams when their league's row is off.
 */
export default function SportsRows({
  leagues,
  hidden: hiddenAtStart,
  onSave,
  onClose,
}: {
  /** Every league the server shows, in this profile's order. */
  leagues: SportsLeague[]
  hidden: string[]
  onSave: (order: string[], hidden: string[]) => Promise<void>
  onClose: () => void
}) {
  const [order, setOrder] = useState(leagues)
  const [hidden, setHidden] = useState(() => new Set(hiddenAtStart))
  const [error, setError] = useState('')
  const press = isPhone ? 'Tap' : 'OK'
  const up = (i: number) => {
    if (i === 0) return
    const next = [...order]
    ;[next[i - 1], next[i]] = [next[i], next[i - 1]]
    setOrder(next)
  }
  const toggle = (id: string) => {
    const next = new Set(hidden)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setHidden(next)
  }
  const save = (o: string[], h: string[]) => onSave(o, h).then(onClose, (e: Error) => setError(e.message))
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <View style={st.scrim}>
        <ScrollView style={st.box} contentContainerStyle={st.boxIn}>
          <Text style={st.title}>Sports rows</Text>
          <Text style={st.muted}>
            {press} on a league turns its row on or off; ▲ moves it up. Your teams' games always show under Your teams. An admin can take leagues out for everyone
            in Server → Settings → Sports data.
          </Text>
          {!!error && <Text style={st.error}>{error}</Text>}
          {order.map((l, i) => {
            const on = !hidden.has(l.id)
            return (
              <View key={l.id} style={st.row}>
                <Focusable hasTVPreferredFocus={i === 0} onPress={() => toggle(l.id)} style={StyleSheet.flatten([st.league, { borderLeftColor: sportColors[l.sport] ?? colors.border }])}>
                  <Text style={[st.name, !on && st.off]}>
                    {on ? '✓ ' : '    '}
                    {l.name}
                    {on ? '' : '  (off)'}
                  </Text>
                </Focusable>
                <Focusable onPress={() => up(i)} style={StyleSheet.flatten([st.up, i === 0 && st.upNone])}>
                  <Text style={st.upText}>▲</Text>
                </Focusable>
              </View>
            )
          })}
          <View style={st.buttons}>
            <Button small primary label="Save" onPress={() => save(order.map((l) => l.id), order.filter((l) => hidden.has(l.id)).map((l) => l.id))} />
            <Button small label="Usual order, all on" onPress={() => save([], [])} />
            <Button small label="Cancel" onPress={onClose} />
          </View>
        </ScrollView>
      </View>
    </Modal>
  )
}

const st = StyleSheet.create(scaled({
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', alignItems: 'center', justifyContent: 'center' },
  box: { flexGrow: 0, width: 1000, maxHeight: '92%', backgroundColor: colors.surface, borderRadius: 22 },
  boxIn: { padding: 40 },
  title: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  muted: { color: colors.muted, fontSize: font.small, marginTop: 6, marginBottom: 12 },
  error: { color: colors.danger, fontSize: font.small, marginVertical: 8 },
  row: { flexDirection: 'row', alignItems: 'center', marginTop: 10 },
  league: { flex: 1, backgroundColor: colors.surface2, borderRadius: 14, borderLeftWidth: 4, paddingVertical: 14, paddingHorizontal: 22 },
  name: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  off: { color: colors.muted, fontWeight: '400' },
  up: { width: 70, height: 60, borderRadius: 14, backgroundColor: colors.surface2, alignItems: 'center', justifyContent: 'center', marginLeft: 12 },
  upNone: { opacity: 0.3 },
  upText: { color: colors.text, fontSize: 24 },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', marginTop: 24 },
}))
