import React, { useCallback, useEffect, useState } from 'react'
import { Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { api, current } from './api'
import { Button } from './components'
import { ItemMenuContext, useNav } from './nav'
import { colors, font, scaled } from './theme'
import type { Item, ItemDetail } from './types'

type Page = 'main' | 'playlists' | 'recommend' | 'collections' | 'manage'

interface Choice {
  id: number
  name: string
}

/**
 * Holds the options menu that holding OK on a card brings up, and gives
 * every card a way to open it.
 */
export function ItemMenuHost({ children }: { children: React.ReactNode }) {
  const [item, setItem] = useState<Item | null>(null)
  const open = useCallback((it: Item) => setItem(it), [])
  return (
    <ItemMenuContext.Provider value={open}>
      {children}
      {item && <ItemMenu item={item} onClose={() => setItem(null)} />}
    </ItemMenuContext.Provider>
  )
}

/**
 * Plex's long-press menu: play, go to the show, season or movie, mark
 * watched, watchlist, playlists, recommend, trailer; admins also add to
 * collections and refresh. A Modal keeps focus in it, and Back closes it
 * (or goes back to the first page).
 */
function ItemMenu({ item, onClose }: { item: Item; onClose: () => void }) {
  const nav = useNav()
  const admin = !!current()?.user.isAdmin
  const [d, setD] = useState<ItemDetail | null>(null)
  const [page, setPage] = useState<Page>('main')
  const [choices, setChoices] = useState<Choice[] | null>(null)
  const [mine, setMine] = useState<string[]>([])
  const [newName, setNewName] = useState('')
  const [msg, setMsg] = useState('')
  const [recommendOn, setRecommendOn] = useState(true)
  useEffect(() => {
    api.get<ItemDetail>(`/api/items/${item.id}`).then(setD, (e) => setMsg(e.message))
    api.get<{ enabled?: boolean }>('/api/me/recommendations').then((r) => setRecommendOn(r.enabled !== false), () => undefined)
  }, [item.id])

  const it = d?.item ?? item
  const playable = it.kind === 'movie' || it.kind === 'episode'
  const target = playable ? it : d?.upNext
  const resume = target && target.watch?.position > 30 ? target.watch.position : 0
  const watched = playable ? !!it.watch?.watched : !!it.childCount && !it.unwatchedCount
  // The watchlist, recommendations and collections hold movies and shows.
  const top: Item | undefined = it.kind === 'movie' || it.kind === 'show' ? it : d?.show
  const go = (fn: () => void) => {
    onClose()
    fn()
  }
  const act = (p: Promise<unknown>, done: string) =>
    p.then(
      () => {
        setMsg(done)
        setPage('main')
        api.get<ItemDetail>(`/api/items/${item.id}`).then(setD, () => undefined)
      },
      (e) => setMsg(e.message),
    )
  const openPage = (p: Page) => {
    setMsg('')
    setChoices(null)
    setPage(p)
    if (p === 'playlists') api.get<Choice[]>('/api/playlists').then(setChoices, (e) => setMsg(e.message))
    if (p === 'recommend' && top) api.get<Choice[]>(`/api/items/${top.id}/recommend`).then(setChoices, (e) => setMsg(e.message))
    if (p === 'collections' && top)
      api.get<{ collections: string[]; library: string[] }>(`/api/admin/items/${top.id}/collections`).then(
        (c) => {
          setMine(c.collections)
          setChoices(c.library.map((name, i) => ({ id: i, name })))
        },
        (e) => setMsg(e.message),
      )
  }
  const back = () => (page === 'main' ? onClose() : setPage('main'))

  let buttons: React.ReactNode
  switch (page) {
    case 'main':
      buttons = (
        <>
          {target && (
            <Opt
              first
              label={resume ? 'Resume' : playable ? 'Play' : `Play S${target.seasonNumber} E${target.episodeNumber}`}
              onPress={() => go(() => nav.push({ name: 'player', id: target.id, start: resume }))}
            />
          )}
          {resume > 0 && <Opt label="Play from the start" onPress={() => go(() => nav.push({ name: 'player', id: target!.id, start: 0 }))} />}
          {!!d?.trailers?.length && <Opt label="Watch trailer" onPress={() => go(() => nav.push({ name: 'trailer', url: d.trailers![0].url, title: it.title }))} />}
          {it.kind === 'movie' && <Opt label="Go to movie" onPress={() => go(() => nav.push({ name: 'details', id: it.id }))} />}
          {(it.showId || d?.show) && <Opt label="Go to show" onPress={() => go(() => nav.push({ name: 'details', id: d?.show?.id ?? it.showId! }))} />}
          {it.kind === 'episode' && d?.season && <Opt label="Go to season" onPress={() => go(() => nav.push({ name: 'details', id: d.season!.id }))} />}
          {(it.kind === 'show' || it.kind === 'season') && <Opt label={it.kind === 'show' ? 'Go to show' : 'Go to season'} onPress={() => go(() => nav.push({ name: 'details', id: it.id }))} />}
          <Opt
            label={watched ? 'Mark unwatched' : 'Mark watched'}
            onPress={() => act(api.post(`/api/items/${it.id}/${watched ? 'unwatched' : 'watched'}`), watched ? 'Marked unwatched.' : 'Marked watched.')}
          />
          {top && (
            <Opt
              label={top.onWatchlist ? 'Remove from watchlist' : 'Add to watchlist'}
              onPress={() =>
                act(top.onWatchlist ? api.del(`/api/watchlist/${top.id}`) : api.post(`/api/watchlist/${top.id}`), top.onWatchlist ? 'Removed from your watchlist.' : 'Added to your watchlist.')
              }
            />
          )}
          <Opt label="Add to playlist…" onPress={() => openPage('playlists')} />
          {top && recommendOn && <Opt label="Recommend to…" onPress={() => openPage('recommend')} />}
          {admin && top && <Opt label="Add to collection…" onPress={() => openPage('collections')} />}
          {admin && <Opt label="Manage…" onPress={() => openPage('manage')} />}
          <Opt label="Close" onPress={onClose} />
        </>
      )
      break
    case 'playlists':
      buttons = (
        <>
          {choices?.map((p, i) => (
            <Opt key={p.id} first={i === 0} label={p.name} onPress={() => act(api.post(`/api/playlists/${p.id}/items`, { itemIds: [it.id] }), `Added to ${p.name}.`)} />
          ))}
          {choices && (
            <View style={st.newRow}>
              <TextInput
                style={st.input}
                value={newName}
                onChangeText={setNewName}
                placeholder="New playlist name"
                placeholderTextColor={colors.muted}
                disableFullscreenUI
              />
              <Button
                small
                label="Create"
                onPress={() => newName.trim() && act(api.post('/api/playlists', { name: newName.trim(), itemIds: [it.id] }), `Made ${newName.trim()}.`)}
              />
            </View>
          )}
          <Opt label="Back" first={!choices?.length} onPress={back} />
        </>
      )
      break
    case 'recommend':
      buttons = (
        <>
          {choices?.length === 0 && <Text style={st.hint}>Nobody here takes recommendations yet (they turn it on under Account → Sharing).</Text>}
          {choices?.map((p, i) => (
            <Opt key={p.id} first={i === 0} label={p.name} onPress={() => act(api.post(`/api/items/${top!.id}/recommend`, { userIds: [p.id], note: '' }), `Recommended to ${p.name}.`)} />
          ))}
          <Opt label="Back" first={!choices?.length} onPress={back} />
        </>
      )
      break
    case 'collections': {
      const save = (names: string[]) =>
        api.put(`/api/admin/items/${top!.id}/collections`, { collections: names }).then(
          () => setMine(names),
          (e) => setMsg(e.message),
        )
      buttons = (
        <>
          {choices?.length === 0 && <Text style={st.hint}>This library has no collections you can add to yet; make one on the web.</Text>}
          {choices?.map((c, i) => {
            const on = mine.includes(c.name)
            return <Opt key={c.name} first={i === 0} label={`${on ? '✓ ' : ''}${c.name}`} onPress={() => save(on ? mine.filter((n) => n !== c.name) : [...mine, c.name])} />
          })}
          <Opt label="Done" first={!choices?.length} onPress={back} />
        </>
      )
      break
    }
    case 'manage':
      buttons = (
        <>
          <Opt first label="Refresh metadata" onPress={() => act(api.post(`/api/admin/items/${(top ?? it).id}/refresh`), 'Refreshing the details…')} />
          {playable && <Opt label="Find intro and credits" onPress={() => act(api.post(`/api/admin/items/${it.id}/detect-markers`), 'Looking for the intro and credits…')} />}
          <Opt label="Back" onPress={back} />
        </>
      )
      break
  }

  const title = it.kind === 'episode' ? `${it.showTitle ?? ''} · S${it.seasonNumber} E${it.episodeNumber}` : it.title
  return (
    <Modal transparent animationType="fade" onRequestClose={back}>
      <Pressable style={st.scrim} onPress={onClose}>
        <Pressable style={st.box}>
          <Text style={st.title} numberOfLines={1}>{title}</Text>
          {it.kind === 'episode' && <Text style={st.hint} numberOfLines={1}>{it.title}</Text>}
          {!!msg && <Text style={st.msg}>{msg}</Text>}
          <ScrollView style={st.list}>{buttons}</ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  )
}

function Opt({ label, onPress, first }: { label: string; onPress: () => void; first?: boolean }) {
  return <Button label={label} onPress={onPress} preferFocus={first} style={st.opt} />
}

const st = StyleSheet.create(scaled({
  scrim: { flex: 1, backgroundColor: 'rgba(0,0,0,0.65)', alignItems: 'center', justifyContent: 'center' },
  box: { backgroundColor: colors.surface, borderRadius: 22, padding: 36, width: 820, maxHeight: '90%', borderWidth: 1, borderColor: colors.border },
  title: { color: colors.text, fontSize: font.heading, fontWeight: '800' },
  hint: { color: colors.muted, fontSize: font.small, marginTop: 6 },
  msg: { color: colors.accent, fontSize: font.small, marginTop: 10 },
  list: { marginTop: 20 },
  opt: { marginRight: 0, marginBottom: 12, alignItems: 'flex-start' },
  newRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 12 },
  input: { flex: 1, backgroundColor: colors.surface2, color: colors.text, fontSize: font.body, borderRadius: 14, paddingHorizontal: 20, paddingVertical: 12, marginRight: 16 },
}))
