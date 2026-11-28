import React, { useState } from 'react'
import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { api } from './api'
import { Button } from './components'
import { colors, font, quietPress, scaled } from './theme'
import type { TitleRequest } from './types'

/** The libraries a request can land in, from /api/requests/title (older servers leave them out). */
export interface RequestLibraries {
  title?: { request?: TitleRequest }
  libraries?: { id: number; name: string }[]
  library?: number
}

type Pick = { title: string; libraries: { id: number; name: string }[]; first: number; send: (libraryId?: number) => void }

/**
 * Asking for a title, with a stop to pick its library (Movies, Kids Movies,
 * Christmas) when there's more than one to pick from. ask looks the
 * libraries up, then calls send with the one picked (or none, when there's
 * nothing to pick, the server is older, or someone already picked);
 * picker is the menu to render while it's open.
 */
export function useLibraryPick(hint?: number) {
  const [pick, setPick] = useState<Pick | null>(null)
  const ask = (t: { kind: 'movie' | 'show'; tmdbId: number; title: string }, send: (libraryId?: number) => void) => {
    api.get<RequestLibraries>(`/api/requests/title/${t.kind}/${t.tmdbId}${hint ? `?library=${hint}` : ''}`).then(
      (info) => {
        const libraries = info.libraries ?? []
        const req = info.title?.request
        if (libraries.length <= 1 || (req && req.status !== 'declined' && req.libraryId)) send(info.library || undefined)
        else setPick({ title: t.title, libraries, first: info.library || libraries[0].id, send })
      },
      () => send(),
    )
  }
  const picker = pick && (
    <LibraryMenu
      pick={pick}
      onPick={(id) => {
        setPick(null)
        pick.send(id)
      }}
      onClose={() => setPick(null)}
    />
  )
  return { ask, picker }
}

function LibraryMenu({ pick, onPick, onClose }: { pick: Pick; onPick: (id: number) => void; onClose: () => void }) {
  // The one to offer first leads, so OK right away keeps today's choice.
  const list = [...pick.libraries].sort((a, b) => (a.id === pick.first ? -1 : b.id === pick.first ? 1 : 0))
  return (
    <Modal transparent animationType="fade" onRequestClose={onClose}>
      <Pressable android_disableSound={quietPress} style={st.scrim} onPress={onClose}>
        <View style={st.menu}>
          <Text style={st.title} numberOfLines={1}>Request {pick.title}</Text>
          <Text style={st.hint}>Which library should it go in?</Text>
          <ScrollView style={st.list}>
            {list.map((l, i) => (
              <Button key={l.id} primary={i === 0} preferFocus={i === 0} label={l.name} onPress={() => onPick(l.id)} style={st.option} />
            ))}
          </ScrollView>
          <Button label="Cancel" onPress={onClose} style={st.option} />
        </View>
      </Pressable>
    </Modal>
  )
}

const st = StyleSheet.create(
  scaled({
    scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.6)', alignItems: 'center', justifyContent: 'center' },
    menu: { backgroundColor: colors.surface, borderRadius: 22, padding: 40, minWidth: 700, maxWidth: 1000, maxHeight: 900, borderWidth: 1, borderColor: colors.border },
    title: { color: colors.text, fontSize: font.heading, fontWeight: '700' },
    hint: { color: colors.muted, fontSize: font.small, marginTop: 6, marginBottom: 24 },
    list: { flexGrow: 0 },
    option: { marginBottom: 16 },
  }),
)
