// Mirrors the JSON shapes in server/internal/store and server/internal/api.

export interface User {
  id: number
  username: string
  isAdmin: boolean
  createdAt: number
  allLibraries: boolean
  libraryIds?: number[]
}

export interface WatchState {
  position: number
  duration: number
  watched: boolean
  updatedAt: number
}

export interface Credit {
  name: string
  role: string
  kind: 'cast' | 'crew'
  photo?: string
}

export type ItemKind = 'movie' | 'show' | 'season' | 'episode'

export interface Item {
  id: number
  libraryId: number
  kind: ItemKind
  parentId?: number
  showId?: number
  title: string
  sortTitle: string
  originalTitle?: string
  year?: number
  overview?: string
  tagline?: string
  poster?: string
  backdrop?: string
  logo?: string
  rating?: number
  contentRating?: string
  genres?: string[]
  studio?: string
  runtime?: number
  releaseDate?: string
  tmdbId?: number
  collectionId?: number
  collectionName?: string
  trailer?: string
  credits?: Credit[]
  edition?: string
  labels?: string[]
  locked?: string[]
  episodeOrder?: '' | 'desc'
  seasonNumber?: number
  episodeNumber?: number
  addedAt: number
  watch: WatchState
  childCount?: number
  unwatchedCount?: number
  showTitle?: string
  showPoster?: string
  showBackdrop?: string
}

export interface MediaStream {
  index: number
  type: 'video' | 'audio' | 'subtitle'
  codec: string
  language?: string
  title?: string
  channels?: number
  width?: number
  height?: number
  default?: boolean
  forced?: boolean
}

export interface MediaFile {
  id: number
  itemId: number
  path: string
  size: number
  container: string
  duration: number
  bitrate: number
  videoCodec: string
  width: number
  height: number
  bitDepth: number
  hdr: boolean
  audioCodec: string
  audioChannels: number
  streams: MediaStream[]
}

export interface Library {
  id: number
  name: string
  kind: 'movies' | 'shows'
  paths: string[] | null
  createdAt: number
  lastScan: number
  itemCount: number
}

export interface ItemDetail {
  item: Item
  files?: MediaFile[]
  children?: Item[]
  show?: Item
  season?: Item
  upNext?: Item
  collections?: { collection: Collection; items: Item[] }[]
  collectionNames?: string[] // admin only: explicit collection links
}

export interface Collection {
  id: number
  libraryId: number
  name: string
  overview?: string
  poster?: string
  backdrop?: string
  tmdbId?: number
  labelRule?: string
  itemCount: number
}

export interface HomeData {
  hero: Item[]
  continueWatching: Item[]
  rows: { id: string; title: string; items: Item[] }[]
}

export interface ScanStatus {
  libraryId: number
  running: boolean
  phase: 'scanning' | 'metadata' | 'done'
  found: number
  processed: number
  started: number
  finished: number
  error?: string
}

export interface NowPlaying {
  id: string
  userId: number
  username: string
  itemId: number
  title: string
  subtitle?: string
  poster?: string
  fileId: number
  mode: 'direct' | 'transcode'
  position: number
  duration: number
  paused: boolean
  ip: string
  local: boolean
  client: string
  source: string
  target: string
  startedAt: number
  updatedAt: number
  transcodeId?: string
}

export interface Stats {
  movies: number
  shows: number
  seasons: number
  episodes: number
  files: number
  totalSize: number
  totalHours: number
  users: number
}

export interface SystemInfo {
  version: string
  os: string
  dataDir: string
  ffmpeg: string
  ffprobe: string
  metadataActive: boolean
}
