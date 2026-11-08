import { NativeModules, Platform } from 'react-native'
import type { Item } from './types'

/**
 * Continue Watching on the Android TV / Google TV home screen (the system's
 * "Watch Next" row), kept in step with the app's own row each time Home
 * loads. Phones, Fire TV launchers without the row and older APKs without
 * the module do nothing.
 */
const WatchNext = NativeModules.WatchNext as
  | { sync(items: object[]): Promise<boolean>; clear(): Promise<boolean> }
  | undefined

/** TMDB artwork straight from TMDB: the launcher fetches it without our sign-in. */
function publicArt(url?: string): string | undefined {
  const m = url?.match(/\/api\/images\/tmdb\/([^?]+)/)
  return m ? `https://image.tmdb.org/t/p/w780/${m[1]}` : url?.startsWith('https://') ? url : undefined
}

let last = ''

export function syncWatchNext(items: Item[]) {
  if (!WatchNext || Platform.OS !== 'android' || !Platform.isTV) return
  const list = items
    .filter((it) => it.kind === 'movie' || it.kind === 'episode')
    .slice(0, 20)
    .map((it) => {
      const episode = it.kind === 'episode'
      return {
        id: String(it.id),
        title: episode ? it.showTitle || it.title : it.title,
        episode: episode ? it.title : null,
        seasonNumber: it.seasonNumber,
        episodeNumber: it.episodeNumber,
        description: it.overview?.slice(0, 300),
        image: publicArt(it.backdrop || it.showBackdrop || it.poster),
        position: Math.round((it.watch?.position > 15 ? it.watch.position : 0) * 1000),
        duration: Math.round((it.watch?.duration || (it.runtime ?? 0) * 60) * 1000),
        lastWatched: (it.watch?.updatedAt || Date.now() / 1000) * 1000,
      }
    })
  const key = JSON.stringify(list)
  if (key === last) return
  last = key
  WatchNext.sync(list).catch(() => undefined)
}

/** Signed out or switched profile: the next person shouldn't see this row. */
export function clearWatchNext() {
  last = ''
  WatchNext?.clear().catch(() => undefined)
}

/** bingebandit://play/<id>?t=<seconds> from the home screen: what to open. */
export function parseWatchNextLink(url: string | null): { id: number; start: number } | null {
  const m = url?.match(/^bingebandit:\/\/play\/(\d+)(?:\?t=(\d+))?/)
  return m ? { id: Number(m[1]), start: Number(m[2] ?? 0) } : null
}
