import React, { useEffect, useRef, useState } from 'react'
import { Image, StyleSheet, Text, View } from 'react-native'
import { api } from './api'
import { quietNow } from './quiet'
import { scaled, colors, font } from './theme'

interface Note {
  id: number
  kind: string
  title: string
  body?: string
  image?: string
  createdAt: number
  readAt?: number
}

/**
 * Game alerts and followed Live TV shows pop up in a corner for a few
 * seconds, over whatever is on screen (a movie included, unless the person
 * asked for quiet while watching: then they wait until playback ends). They
 * stay unread, so the web app's bell still shows them.
 */
export function Toasts({ playing }: { playing: boolean }) {
  const playingRef = useRef(playing)
  playingRef.current = playing
  const [shown, setShown] = useState<Note | null>(null)
  const seen = useRef<Set<number> | null>(null)
  useEffect(() => {
    let hide: ReturnType<typeof setTimeout> | undefined
    const look = () =>
      api.get<{ notifications: Note[] }>('/api/notifications').then(({ notifications }) => {
        const fresh = notifications.filter((n) => (n.kind === 'sports' || n.kind === 'live') && !n.readAt && Date.now() / 1000 - n.createdAt < 15 * 60)
        // The first look only learns what's already there.
        if (!seen.current) {
          seen.current = new Set(fresh.map((n) => n.id))
          return
        }
        const next = fresh.find((n) => !seen.current!.has(n.id))
        if (!next || quietNow(playingRef.current)) return
        seen.current.add(next.id)
        setShown(next)
        if (hide) clearTimeout(hide)
        hide = setTimeout(() => setShown(null), 10000)
      }, () => undefined)
    look()
    const t = setInterval(look, 60000)
    return () => {
      clearInterval(t)
      if (hide) clearTimeout(hide)
    }
  }, [])
  if (!shown) return null
  return (
    <View style={st.toast} pointerEvents="none">
      {!!shown.image && <Image source={{ uri: shown.image }} style={st.image} resizeMode="contain" />}
      <View style={st.text}>
        <Text style={st.title}>{shown.title}</Text>
        {!!shown.body && <Text style={st.body}>{shown.body}</Text>}
      </View>
    </View>
  )
}

const st = StyleSheet.create(scaled({
  toast: {
    position: 'absolute', top: 40, right: 40, maxWidth: 760, flexDirection: 'row', alignItems: 'center',
    backgroundColor: 'rgba(20,24,29,0.94)', borderRadius: 20, padding: 22, borderWidth: 2, borderColor: colors.accent,
  },
  image: { width: 64, height: 64, marginRight: 20 },
  text: { flexShrink: 1 },
  title: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  body: { color: colors.muted, fontSize: font.small, marginTop: 4 },
}))
