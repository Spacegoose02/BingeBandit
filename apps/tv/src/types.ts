// The parts of the server's types the TV app uses (see web/src/types.ts).

export interface User {
  id: number
  username: string
  isAdmin: boolean
  profileName?: string // set for a profile under an account
  liveTV?: boolean // false when an admin turned Live TV off for this user
  kid?: boolean // gets Approved Movies and Approved Shows: titles a parent approved
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
  sortTitle?: string
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
  favorite?: boolean
  credits?: Credit[] // movies and shows, on the details endpoint
  watch: WatchState
}

export interface Credit {
  name: string
  role: string // the character for cast, the job for crew
  kind: 'cast' | 'crew'
  photo?: string
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
  collections?: { collection: Collection; items: Item[] }[] // movies and shows: the collections they're in
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
  sortTitle?: string
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
  /** Every category, comma separated. */
  genres?: string
  /** The year it was made. */
  year?: number
  /** Age rating, such as "TV-14" or "R". */
  rating?: string
  /** Star rating, such as "7.5/10". */
  stars?: string
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
  id?: string // older servers don't send one
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
  /** The score in each period (quarter, inning, half). */
  lines?: string[]
  /** The side's standout player so far, or its probable starter. */
  leader?: SportsLeader
}

export interface SportsLeader {
  name: string
  shortName: string
  position?: string
  /** What they've done: "1 REC, 16 YDS". */
  value?: string
}

export interface SportsGame {
  id: string
  league: string
  /** "Miami Dolphins at Minnesota Vikings", or a race's name. */
  name?: string
  start: string
  state: 'pre' | 'in' | 'post'
  detail: string
  home: SportsSide
  away: SportsSide
  period?: number
  venue?: string
  /** A race's running order (NASCAR), leader first; races have empty home and away. */
  race?: { place: number; name: string; shortName: string; number?: string; winner?: boolean }[]
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

/** /api/sports/scores: every league's games on a day. */
export interface SportsDay extends SportsHome {
  date: string
}

/** /api/sports/{league}/games/{id}: one game's box score and the rest. */
export interface SportsSummary {
  game: SportsGame
  stats: { label: string; away: string; home: string }[]
  players: { team: string; title: string; labels: string[]; rows: { name: string; position?: string; stats: string[] }[] }[]
  leaders: { title: string; away?: SportsLeader; home?: SportsLeader }[]
  standings: { title: string; labels: string[]; rows: { id: string; name: string; logo?: string; stats: string[] }[] }[]
}

/** A movie or show someone asked for (one per title; later askers join it). */
export interface TitleRequest {
  id: number
  kind: 'movie' | 'show'
  tmdbId: number
  title: string
  year?: number
  poster?: string
  requestedBy: string
  people: number
  status: 'pending' | 'approved' | 'sent' | 'failed' | 'declined' | 'available'
  mine: boolean
}

/** A TMDB search result the viewer can request. */
export interface RequestableTitle {
  kind: 'movie' | 'show'
  tmdbId: number
  title: string
  year?: number
  overview?: string
  poster?: string
  itemId?: number
  onServer?: boolean
  request?: TitleRequest
}
