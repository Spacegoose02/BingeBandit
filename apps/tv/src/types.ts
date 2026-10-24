// The parts of the server's types the TV app uses (see web/src/types.ts).

export interface User {
  id: number
  username: string
  isAdmin: boolean
}

export interface WatchState {
  position: number
  duration: number
  watched: boolean
  updatedAt: number
}

export interface Item {
  id: number
  kind: 'movie' | 'show' | 'season' | 'episode'
  title: string
  year?: number
  overview?: string
  tagline?: string
  poster?: string
  backdrop?: string
  rating?: number
  contentRating?: string
  genres?: string[]
  runtime?: number
  seasonNumber?: number
  episodeNumber?: number
  showId?: number
  showTitle?: string
  showPoster?: string
  showBackdrop?: string
  childCount?: number
  unwatchedCount?: number
  watch: WatchState
}

export interface MediaFile {
  id: number
  container: string
  duration: number
  videoCodec: string
  width: number
  height: number
  hdr: boolean
  audioCodec: string
  audioChannels: number
}

export interface ItemDetail {
  item: Item
  files?: MediaFile[]
  children?: Item[]
  show?: Item
  season?: Item
  upNext?: Item
}

export interface HomeData {
  hero: Item[]
  continueWatching: Item[]
  rows: { id: string; title: string; items: Item[] }[]
}

export interface GuideProgram {
  title: string
  subtitle?: string
  start: number
  stop: number
}

export interface LiveChannel {
  id: number
  number: number
  name: string
  logo?: string
  group?: string
  favorite: boolean
  now?: GuideProgram
  next?: GuideProgram
}

export interface ServerInfo {
  url: string
  name: string
  version: string
}
