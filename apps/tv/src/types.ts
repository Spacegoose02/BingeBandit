// The parts of the server's types the TV app uses (see web/src/types.ts).

export interface User {
  id: number
  username: string
  isAdmin: boolean
  profileName?: string // set for a profile under an account
  liveTV?: boolean // false when an admin turned Live TV off for this user
}

/** A person in the account's household: the account or one of its profiles. */
export interface Member {
  id: number
  name: string
  isOwner: boolean
  hasPin: boolean
  current: boolean
  avatar?: string
  /** The account opens as this member without asking who's watching. */
  auto?: boolean
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
  onWatchlist?: boolean
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
  streams: import('./tracks').MediaStream[]
}

export interface ItemDetail {
  item: Item
  files?: MediaFile[]
  children?: Item[]
  show?: Item
  season?: Item
  upNext?: Item
  trailers?: { url: string; name: string }[] // movies: trailer files from the movie's folder
}

/** A row of titles on Home or a library's Recommended tab. */
export interface HubRow {
  id: string
  title: string
  items: Item[]
  collectionId?: number
}

export interface HomeData {
  hero: Item[]
  continueWatching: Item[]
  rows: HubRow[]
}

export interface Library {
  id: number
  name: string
  kind: 'movies' | 'shows'
  pinned: boolean // in this user's menu and on their Home
  itemCount: number
}

export interface Collection {
  id: number
  name: string
  overview?: string
  poster?: string
  backdrop?: string
  itemCount: number
}

export interface GuideProgram {
  id?: number
  title: string
  subtitle?: string
  description?: string
  episode?: string
  category?: string
  start: number
  stop: number
}

export interface LiveChannel {
  id: number
  number: number
  name: string
  logo?: string
  group?: string
  epgId?: string
  favorite: boolean
  now?: GuideProgram
  next?: GuideProgram
}

/** A viewer's own channel category (made on the web). */
export interface ChannelCategory {
  id: number
  name: string
  channelIds: number[]
}

export interface ServerInfo {
  url: string
  name: string
  version: string
}

export interface SportsLeague {
  id: string
  name: string
  sport: string
}

export interface SportsTeam {
  league: string
  id: string
  name: string
  shortName: string
  abbr: string
  logo?: string
  color?: string
}

export interface SportsSide extends SportsTeam {
  home: boolean
  score?: string
  winner?: boolean
  record?: string
}

export interface SportsGame {
  id: string
  league: string
  start: string
  state: 'pre' | 'in' | 'post'
  detail: string
  home: SportsSide
  away: SportsSide
  broadcasts?: string[]
  mine?: string[]
  /** Live TV channels whose guide shows it. */
  channels?: { id: number; number: number; name: string }[]
}

export interface SportsHome {
  leagues: SportsLeague[]
  teams: SportsTeam[]
  games: SportsGame[]
  error?: string
}
