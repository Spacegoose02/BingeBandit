import React, { useEffect, useState } from 'react'
import { Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { Button } from '../components'
import {
  downloadPrefs, formatBytes, pauseDownload, removeDownload, resumeDownload, setDownloadPrefs, syncDownloads, titleOf, useDownloads, usedBytes,
  type Download,
} from '../downloads'
import { useNav, useScreen } from '../nav'
import { colors, font, gutter, isPhone, quietPress, scaled } from '../theme'

const LIMITS = [5, 10, 20, 50, 0] // GB; 0 = no limit

/**
 * Titles saved on this phone, playable with no connection: progress while
 * they download, pause/resume, remove, and the Wi-Fi-only and storage limit
 * settings. Opens from the Downloads tab, or from Home when the server
 * can't be reached.
 */
export default function Downloads({ withBack }: { withBack?: boolean }) {
  const nav = useNav()
  const all = useDownloads()
  const prefs = downloadPrefs()
  const { returns } = useScreen()
  // Coming here online sends positions watched offline and drops downloads
  // this profile may no longer keep.
  useEffect(() => {
    syncDownloads().catch(() => undefined)
  }, [returns])
  const [confirm, setConfirm] = useState<string | null>(null)

  const done = all.filter((d) => d.state === 'done')
  const queue = all.filter((d) => d.state !== 'done')
  const play = (d: Download) => nav.push({ name: 'player', id: d.itemId, start: d.watched ? 0 : d.position, offline: true })

  return (
    <ScrollView contentContainerStyle={st.body}>
      {withBack && (
        <Pressable android_disableSound={quietPress} onPress={nav.back} hitSlop={20} style={st.back}>
          <Text style={st.backText}>‹ Back</Text>
        </Pressable>
      )}
      <Text style={st.title}>Downloads</Text>
      <Text style={st.muted}>
        {formatBytes(usedBytes())} used{prefs.limitGB ? ` of ${prefs.limitGB} GB` : ''} · {prefs.wifiOnly ? 'Downloads on Wi-Fi only' : 'Downloads on Wi-Fi and mobile data'}
      </Text>

      {all.length === 0 && (
        <Text style={[st.muted, st.empty]}>
          Nothing downloaded yet. Open a movie or an episode and tap Download to watch it without a connection.
        </Text>
      )}

      {queue.length > 0 && <Text style={st.section}>Downloading</Text>}
      {queue.map((d) => (
        <Row key={d.key} d={d} confirm={confirm === d.key} setConfirm={setConfirm}>
          {d.state === 'paused' || d.state === 'failed' ? (
            <Button small label={d.state === 'failed' ? 'Try again' : 'Resume'} onPress={() => resumeDownload(d.key)} />
          ) : (
            <Button small label="Pause" onPress={() => pauseDownload(d.key)} />
          )}
        </Row>
      ))}

      {done.length > 0 && <Text style={st.section}>On this phone</Text>}
      {done.map((d) => (
        <Row key={d.key} d={d} confirm={confirm === d.key} setConfirm={setConfirm} onPress={() => play(d)}>
          <Button small primary label={d.position > 15 && !d.watched ? 'Resume' : 'Play'} onPress={() => play(d)} />
        </Row>
      ))}

      <Text style={st.section}>Settings</Text>
      <Text style={st.label}>Download over</Text>
      <View style={st.row}>
        <Button small primary={prefs.wifiOnly} label="Wi-Fi only" onPress={() => setDownloadPrefs({ wifiOnly: true })} />
        <Button small primary={!prefs.wifiOnly} label="Wi-Fi and mobile data" onPress={() => setDownloadPrefs({ wifiOnly: false })} />
      </View>
      <Text style={st.label}>Storage limit</Text>
      <View style={st.row}>
        {LIMITS.map((g) => (
          <Button key={g} small primary={prefs.limitGB === g} label={g ? `${g} GB` : 'No limit'} onPress={() => setDownloadPrefs({ limitGB: g })} />
        ))}
      </View>
      <Text style={st.hint}>
        Downloads belong to this profile. They're removed if the server stops allowing downloads or this profile can no longer see the title. What you
        watch offline is sent to the server when it can be reached again.
      </Text>
    </ScrollView>
  )
}

function Row({ d, confirm, setConfirm, onPress, children }: {
  d: Download
  confirm: boolean
  setConfirm: (k: string | null) => void
  onPress?: () => void
  children: React.ReactNode
}) {
  const pct = d.state === 'preparing' ? (d.prepare ?? 0) : d.total > 0 ? d.done / d.total : 0
  const quality = d.height ? `${d.height}p` : 'Original'
  const status =
    d.state === 'done'
      ? [quality, formatBytes(d.total), d.watched ? 'Watched' : d.position > 15 ? `${Math.round((d.position / Math.max(1, d.duration)) * 100)}% watched` : ''].filter(Boolean).join(' · ')
      : d.state === 'preparing'
        ? `${d.note ?? 'The server is preparing it'}${d.prepare ? ` · ${Math.round(d.prepare * 100)}%` : ''}`
        : d.state === 'downloading'
          ? `${formatBytes(d.done)} of ${d.total ? formatBytes(d.total) : '…'}`
          : d.state === 'paused'
            ? `Paused · ${formatBytes(d.done)}${d.total ? ` of ${formatBytes(d.total)}` : ''}`
            : d.state === 'failed'
              ? d.note ?? 'Failed'
              : d.note ?? 'Waiting'
  return (
    <Pressable android_disableSound={quietPress} onPress={onPress} style={st.item}>
      {d.poster ? <Image source={{ uri: 'file://' + d.poster }} style={st.poster} /> : <View style={[st.poster, st.posterFallback]} />}
      <View style={st.flex}>
        <Text style={st.itemTitle} numberOfLines={2}>{titleOf(d.item)}</Text>
        {d.item.kind === 'episode' && <Text style={st.itemSub} numberOfLines={1}>{d.item.title}</Text>}
        <Text style={[st.itemSub, d.state === 'failed' && st.error]} numberOfLines={3}>{status}</Text>
        {(d.state === 'downloading' || d.state === 'preparing' || d.state === 'paused') && (
          <View style={st.track}>
            <View style={[st.fill, { width: `${Math.round(Math.min(1, pct) * 100)}%` }]} />
          </View>
        )}
        <View style={st.buttons}>
          {children}
          {confirm ? (
            <>
              <Button small label="Remove" onPress={() => removeDownload(d.key)} />
              <Button small label="Keep" onPress={() => setConfirm(null)} />
            </>
          ) : (
            <Button small label={d.state === 'done' ? 'Remove' : 'Cancel'} onPress={() => setConfirm(d.key)} />
          )}
        </View>
      </View>
    </Pressable>
  )
}

const st = StyleSheet.create(scaled({
  body: { paddingHorizontal: gutter, paddingTop: isPhone ? 30 : 60, paddingBottom: 80 },
  back: { alignSelf: 'flex-start', marginBottom: 24 },
  backText: { color: colors.accent, fontSize: font.body, fontWeight: '700' },
  title: { color: colors.text, fontSize: font.title, fontWeight: '900', marginBottom: 8 },
  muted: { color: colors.muted, fontSize: font.small },
  empty: { marginTop: 40, fontSize: font.body, lineHeight: 40 },
  section: { color: colors.text, fontSize: font.heading, fontWeight: '800', marginTop: 40, marginBottom: 16 },
  label: { color: colors.muted, fontSize: font.small, marginBottom: 10, marginTop: 6 },
  row: { flexDirection: 'row', flexWrap: 'wrap', marginBottom: 10 },
  hint: { color: colors.muted, fontSize: font.small, lineHeight: 34, marginTop: 10 },
  item: { flexDirection: 'row', backgroundColor: colors.surface, borderRadius: 16, padding: 20, marginBottom: 20 },
  poster: { width: 120, height: 180, borderRadius: 10, marginRight: 24 },
  posterFallback: { backgroundColor: colors.surface3 },
  flex: { flex: 1 },
  itemTitle: { color: colors.text, fontSize: font.body, fontWeight: '700' },
  itemSub: { color: colors.muted, fontSize: font.small, marginTop: 6 },
  error: { color: colors.danger },
  track: { height: 8, borderRadius: 4, backgroundColor: colors.surface3, marginTop: 14, overflow: 'hidden' },
  fill: { height: 8, borderRadius: 4, backgroundColor: colors.accent },
  buttons: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 16 },
}))
