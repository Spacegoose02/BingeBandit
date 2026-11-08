// Mirrors the JSON shapes in server/internal/store and server/internal/api.

export interface Passkey {
  id: number
  name: string
  createdAt: number
  lastUsedAt: number
}

export interface User {
  id: number
  username: string
  isAdmin: boolean
  createdAt: number
  allLibraries: boolean
  libraryIds?: number[]
  /** How many passkeys (admin user list only). */
  passkeys?: number
  profilePublic: boolean
  totpEnabled: boolean
  shareActivity: boolean
  liveTV: boolean // may watch Live TV and use the DVR
  kid: boolean // only sees Live TV channels approved for kids, and gets Approved Movies and Approved Shows
  maxRating: number // highest rating level shown (ratingLevels); 0 is no limit
  hasPassword: boolean
  hasPasscode: boolean
  ownerId?: number // set for a profile: the account it belongs to
  profileName?: string
  avatar?: string // URL of the profile picture
  autoProfileId?: number // an account's member to open as without asking who's watching
  email?: string // a single sign-on with this email signs in as this user
}

/** A person in the signed-in account's household: the account or one of its profiles. */
export interface Member {
  id: number
  name: string
  isOwner: boolean
  hasPin: boolean
  current: boolean
  avatar?: string
  /** The account opens as this member without asking who's watching. */
  auto?: boolean
  /** Only sees Live TV channels approved for kids, and gets Approved Movies and Approved Shows. */
  kid?: boolean
  /** Highest rating level shown (ratingLevels); missing is no limit. */
  maxRating?: number
  allLibraries: boolean
  libraryIds?: number[]
}

/**
 * Rating limits, lowest first; a level's number is its index + 1 (the server
 * keeps the same list in store/rating_limits.go). Under a limit, unrated
 * movies and shows are hidden unless a parent approved them for a kid.
 */
export const ratingLevels = ['G / TV-Y', 'TV-Y7', 'PG / TV-PG', 'PG-13 / TV-14', 'R / TV-MA', 'NC-17']

/** Describes a rating limit level for people. */
export const ratingLimitName = (level?: number) => (level ? `Up to ${ratingLevels[level - 1]}` : 'Any rating')

export interface OIDCProvider {
  id: number
  name: string
  issuer: string
  clientId: string
  clientSecret?: string
  hasSecret: boolean
  scopes: string
  autoCreate: boolean
  enabled: boolean
}

export interface Identity {
  id: number
  providerId: number
  providerName: string
  email: string
  createdAt: number
  lastUsed: number
}

export interface DeviceSession {
  id: string
  device: string
  ip: string
  createdAt: number
  lastSeen: number
  current: boolean
}

export interface APIKey {
  id: number
  name: string
  prefix: string
  createdAt: number
  lastUsed: number
}

export interface Webhook {
  id: number
  name: string
  url: string
  events: string[]
  format: '' | 'plex' | string
  secret: string
  enabled: boolean
  lastStatus: string
  lastAt: number
}

export interface FriendActivity {
  username: string
  name?: string // the display name others see
  itemId: number
  title: string
  subtitle?: string
  poster?: string
  position: number
  duration: number
  paused: boolean
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

/** A cast or crew member's page: their titles the viewer can see, newest first. */
export interface Person {
  name: string
  photo?: string
  titles: { item: Item; role: string; kind: 'cast' | 'crew' }[]
}

export type ItemKind =
  | 'movie'
  | 'show'
  | 'season'
  | 'episode'
  // Music: artist > album > track. Audiobooks: audiobook > chapter. Photos: photoalbum > photo.
  | 'artist'
  | 'album'
  | 'track'
  | 'audiobook'
  | 'chapter'
  | 'photoalbum'
  | 'photo'

/** Item kinds from music, audiobook and photo libraries. */
export const MEDIA_ITEM_KINDS: ItemKind[] = ['artist', 'album', 'track', 'audiobook', 'chapter', 'photoalbum', 'photo']
export const isMediaKind = (k: ItemKind) => MEDIA_ITEM_KINDS.includes(k)

/** A track, chapter or photo with the file it plays or shows. */
export interface Track {
  item: Item
  file: {
    fileId: number
    duration: number
    container: string
    audioCodec?: string
    width?: number
    height?: number
    size: number
  }
  /** A track's album, a chapter's book or a photo's album. */
  album?: string
}

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
  /** Albums and tracks: the artist; audiobooks and chapters: the author. */
  studio?: string
  runtime?: number
  releaseDate?: string
  tmdbId?: number
  tvdbId?: number
  imdbId?: string
  /** "" (TMDB, else TheTVDB), "tvdb" (chosen in Fix Match) or "none" (left unmatched). */
  metadataSource?: string
  /** A movie or show no source knows, or left unmatched on purpose. */
  unmatched?: boolean
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
  onWatchlist?: boolean
  favorite?: boolean
}

export interface Playlist {
  id: number
  userId: number
  name: string
  itemCount: number
  duration: number
  poster?: string
  createdAt: number
  updatedAt: number
  /** Who made it, for a playlist shared with you; empty for your own. */
  owner?: string
  /** How many people you shared it with. */
  shares?: number
}

export interface Play {
  id: number
  userId: number
  username?: string
  itemId?: number
  title: string
  subtitle?: string
  kind: ItemKind
  libraryId: number
  showId?: number
  startedAt: number
  stoppedAt: number
  watched: number
  completed: boolean
  mode: string
  client: string
  ip?: string
  poster?: string
}

export interface Count {
  key: string
  itemId?: number
  poster?: string
  plays: number
  hours: number
}

export interface UserStats {
  hours: number
  plays: number
  movies: number
  episodes: number
  shows: number
  topGenres: Count[]
  topShows: Count[]
  monthlyHours: Count[]
  favoriteHour: number
}

export interface ServerStats {
  days: number
  daily: Count[]
  topMovies: Count[]
  topShows: Count[]
  topUsers: Count[]
  modes: Count[]
  clients: Count[]
  totalPlays: number
  totalHours: number
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
  hearingImpaired?: boolean
  external?: string // sidecar file name
}

export interface Prefs {
  audioLanguage: string
  subtitleMode: 'off' | 'auto' | 'forced' | 'always'
  subtitleLanguage: string
  subtitleSize: 'small' | 'medium' | 'large'
  subtitleBackground: boolean
  autoQuality: boolean
  autoPlayNext: boolean
  skipIntro: 'button' | 'auto' | 'off'
  skipCredits: 'button' | 'auto' | 'off'
  /** Older servers leave these out. */
  skipRecap?: 'button' | 'auto' | 'off'
  skipAds?: 'button' | 'auto' | 'off'
  /** Trailers of other movies before a movie (0 = none). */
  preRollTrailers: number
  /** The server's intro video before a movie, when an admin has set one. */
  preRollIntro: boolean
  notifyEpisodes: boolean
  notifyRecordings: boolean
  /** Minutes before a followed team's game to send an alert (0 = none). */
  gameAlertMinutes: number
  /** The final score when a followed team's game ends. */
  notifyGameScores: boolean
  notifyTogether: boolean
  /** Your teams' live scores in a corner of the player. */
  scoreBug: boolean
  spoilerFree?: boolean // the TV Sports Hub hides scores, the score bug too
  multiViewLayouts?: { name: string; channels: number[] }[] | null
  /** Leagues moved to the top of Sports, in order, and leagues whose rows are off. */
  sportsOrder?: string[] | null
  sportsHidden?: string[] | null
  liveFollows: string[] | null
  liveAlertMinutes: number
  quietWhilePlaying: boolean
}

export interface SportsLeague {
  id: string
  name: string
  sport: 'football' | 'basketball' | 'baseball' | 'hockey' | 'soccer' | 'mma' | 'racing'
}

export interface SportsTeam {
  league: string
  id: string
  name: string
  shortName: string
  abbr: string
  logo?: string
  color?: string
  altColor?: string
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
  name: string
  shortName: string
  start: string
  state: 'pre' | 'in' | 'post'
  detail: string
  period?: number
  clock?: string
  home: SportsSide
  away: SportsSide
  broadcasts?: string[]
  venue?: string
  /** A NASCAR race's running order, leader first; races have empty home and away. */
  race?: { place: number; name: string; shortName: string; number?: string; winner?: boolean }[]
  /** The person's teams playing in it. */
  mine?: string[]
  /** Live TV channels whose guide shows it. */
  channels?: { id: number; number: number; name: string; title: string }[]
}

export interface SportsHome {
  /** This profile's league rows, in its order (rows it turned off are left out). */
  leagues: SportsLeague[]
  /** Every league the server shows, in this profile's order: for picking teams and rows. */
  allLeagues?: SportsLeague[]
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

export interface PreRoll {
  /** url: the movie's own trailer file, preferred over the YouTube one. */
  trailers: { itemId: number; title: string; year?: number; youtube?: string; url?: string }[]
  intro?: { url: string; name: string }
}

export interface GuideProgram {
  id: number
  epgId: string
  start: number
  stop: number
  title: string
  subtitle?: string
  description?: string
  category?: string
  icon?: string
  episode?: string
  /** Every category, comma separated. */
  genres?: string
  /** The year it was made. */
  year?: number
  /** Age rating, such as "TV-14" or "R". */
  rating?: string
  /** Star rating, such as "7.5/10". */
  stars?: string
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

/** A viewer's own channel category. */
export interface ChannelCategory {
  id: number
  name: string
  channelIds: number[]
}

/** An image in the admin's channel logo library. */
export interface ChannelLogo {
  id: number
  name: string
  file: string
  url: string
  channels: number
  createdAt: number
}

export interface ChannelStream {
  streamId: number
  name: string
  sourceId: number
  sourceName: string
  url?: string
}

/** Live TV channels approved for kids: whole groups, and channels one by one. */
export interface KidApprovals {
  groups: { name: string; channels: number; approved: boolean }[]
  channelIds: number[]
}

export interface AdminChannel {
  id: number
  number: number
  name: string
  logo?: string // saved value: a provider URL or "library:<file>"
  logoUrl?: string // where to load it from
  group?: string
  epgId?: string
  enabled: boolean
  favorite: boolean
  custom: boolean
  streams?: ChannelStream[]
  lost?: string[] // streams the provider stopped listing since the streams were last saved
}

export interface ContentSource {
  type: 'library' | 'show' | 'collection' | 'playlist' | 'label'
  id?: number
  label?: string
  name?: string // display only
}

export interface Block {
  id?: number
  startMinute: number // -1 = all day
  endMinute: number
  mode: 'shuffle' | 'order'
  sources: ContentSource[]
}

export interface LiveSource {
  id: number
  kind: 'm3u' | 'epg'
  name: string
  url: string
  userAgent: string
  maxStreams: number
  refreshHours: number
  enabled: boolean
  lastRefresh: number
  lastError?: string
  entryCount: number
  newGroupsEnabled: boolean
}

export interface LiveStream {
  id: number
  sourceId: number
  sourceName: string
  name: string
  url: string
  logo?: string
  group?: string
  tvgId?: string
  tvgChno?: string
  channelIds?: number[]
}

export interface RelayStat {
  channelId: number
  number: number
  name: string
  logo?: string
  stream: string
  source: string
  started: number
  bytes: number
  avgBps: number
  currentBps: number
  video?: string
  encoder?: string
  hwError?: string
  behind?: number
  failovers: number
  lastError?: string
  now?: GuideProgram
  viewers: { id: string; username: string; ip: string; kind: string; connected: number }[]
}

export type RecordingStatus = 'pending' | 'scheduled' | 'recording' | 'completed' | 'failed' | 'cancelled'

export interface Recording {
  id: number
  userId: number
  username?: string
  ruleId?: number
  channelId?: number
  channelName: string
  title: string
  subtitle?: string
  description?: string
  episode?: string
  programStart: number
  programStop: number
  startAt: number
  stopAt: number
  status: RecordingStatus
  path?: string
  size: number
  error?: string
  itemId?: number
  /** Upcoming only: why a provider connection may not be free then. */
  conflict?: string
}

export interface RecordingRule {
  id: number
  userId: number
  username?: string
  channelId?: number
  channelName?: string
  title: string
  status: 'pending' | 'active'
  upcoming: number
  /** Keep only the newest N recordings of the series; 0 keeps all. */
  keepLast: number
}

export interface Marker {
  /** recap: "Previously on"; commercial: an ad break in a recording (there can be several). */
  kind: 'intro' | 'credits' | 'recap' | 'commercial'
  start: number
  end: number
  source: 'detected' | 'manual'
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

/** A chapter mark in a file; seconds from the start of the file. */
export interface Chapter {
  start: number
  end: number
  title?: string
}

/** Scrub previews: thumbnail n (at n × interval seconds) is in sheet n / (columns × rows). */
export interface TrickplayInfo {
  interval: number
  width: number
  height: number
  columns: number
  rows: number
  count: number
  sheets: number
}

/** A subtitle found on OpenSubtitles. */
export interface SubtitleResult {
  fileId: number
  name: string
  language: string
  downloads: number
  hearingImpaired: boolean
  hashMatch: boolean
  trusted: boolean
  ai: boolean
  uploader?: string
}

export type LibraryKind = 'movies' | 'shows' | 'music' | 'audiobooks' | 'photos'

export interface Library {
  id: number
  name: string
  kind: LibraryKind
  paths: string[] | null
  createdAt: number
  lastScan: number
  itemCount: number
  skipMarkers: boolean // intro and credits detection is off
  pinned: boolean // in this user's sidebar and on their Home
  collectionMode?: CollectionMode
}

/** How a library's Library tab shows collections (Plex's setting). */
export type CollectionMode = 'show' | 'hide' | 'collapse'

export interface ItemDetail {
  item: Item
  files?: MediaFile[]
  canDownload?: boolean // movies and episodes: the viewer may save the original file
  children?: Item[]
  show?: Item
  season?: Item
  upNext?: Item
  collections?: { collection: Collection; items: Item[] }[]
  collectionNames?: string[] // admin only: explicit collection links
  markers?: Marker[]
  externalRatings?: ExternalRatings // movies and shows, when a ratings provider is set up
  /** Movies and shows: the viewer's thumbs (-1 not for me, 1 like, 2 love, 0 none). */
  myRating?: number
  tvdbId?: number // shows that take their metadata from TheTVDB
  tvdbOrder?: string // '' (aired) | dvd | absolute
  trailers?: LocalTrailer[] // movies: trailer files from the movie's folder
  /** Albums, audiobooks and photo albums (and their tracks, chapters, photos): what they play or show, in order. */
  tracks?: Track[]
  /** Albums and tracks: their artist. */
  artist?: Item
  /** Tracks, chapters and photos: their album, audiobook or photo album. */
  parent?: Item
}

export interface LocalTrailer {
  url: string
  name: string
}

export interface ExternalRatings {
  imdbId?: string
  imdb?: number
  imdbVotes?: number
  rottenTomatoes?: number
  rottenTomatoesAudience?: number
  metacritic?: number
}

export type CollectionSort = '' | 'release_desc' | 'title' | 'added' | 'rating' | 'list'

export interface Collection {
  id: number
  libraryId: number
  name: string
  overview?: string
  poster?: string
  backdrop?: string
  tmdbId?: number
  mode?: CollectionMode | ''
  /** '' is release date, oldest first. */
  sort?: CollectionSort
  /** Orders the collection among the library's others; empty uses its name. */
  sortTitle?: string
  listUrl?: string // the lists it follows, one link per line (MDBList, TMDB, Trakt, IMDb, Yamtrack or RSS)
  listSyncedAt?: number
  listError?: string
  labelRule?: string
  genreRule?: string
  studioRule?: string
  yearFrom?: number
  yearTo?: number
  /** Tags on the collection itself (not its titles). */
  labels?: string[]
  itemCount: number
  visible?: Visibility // admins only: where it shows as a row
}

/** Where a library row shows (Plex's "Visible on"). */
export interface Visibility {
  library: boolean // the library's Recommended tab
  home: boolean
  friends: boolean // Home for people outside the admin's household
}

/** One row in a library's Manage Recommendations table. */
export interface LibraryRow extends Visibility {
  key: string
  title: string
  libraryOnly?: boolean
  collectionId?: number
}

/** A row of titles on Home or a library's Recommended tab. */
export interface HubRow {
  id: string
  title: string
  items: Item[]
  collectionId?: number
  libraryId?: number
  /** A built-in row's query for every title of it: /rows?<seeAll> here, GET /api/rows?<seeAll>. */
  seeAll?: string
}

export interface HomeData {
  hero: Item[]
  continueWatching: Item[]
  rows: HubRow[]
  recommendedBy?: Record<string, Recommendation> // item id → who recommended it
}

export interface Recommendation {
  itemId: number
  from: string[]
  notes?: string[] // "Name: note"
  at: number
}

export interface ScanStatus {
  libraryId: number
  running: boolean
  phase: 'scanning' | 'metadata' | 'moving' | 'done'
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
  name?: string // the display name others see
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
  update?: ServerRelease // set when the daily check found a newer version
}

export interface ServerRelease {
  version: string
  url: string
  publishedAt: string
  notes?: string
}

export interface PluginSetting {
  key: string
  label: string
  type: 'text' | 'password' | 'number' | 'bool' | 'select'
  default?: string
  help?: string
  options?: string[]
}

export interface PluginPage {
  title: string
  path: string
  admin?: boolean
}

export interface PluginManifest {
  id: string
  name: string
  version?: string
  description?: string
  author?: string
  homepage?: string
  events?: string[]
  settings?: PluginSetting[]
  pages?: PluginPage[]
}

/** A plugin in Admin → Plugins (mirrors plugins.Status). */
export interface Plugin {
  id: string
  kind: 'managed' | 'remote'
  url?: string
  serverUrl?: string
  enabled: boolean
  addedAt: number
  manifest: PluginManifest
  state: 'stopped' | 'starting' | 'running' | 'error' | 'missing'
  error?: string
  secretsSet: string[]
  values: Record<string, string>
  /** Installed from a catalogue, so it can be uninstalled. */
  installed?: boolean
}

/** A plugin in a catalogue (mirrors plugins.CatalogPlugin). */
export interface CatalogPlugin {
  id: string
  name: string
  version: string
  description?: string
  author?: string
  homepage?: string
  catalog: string
  available: boolean
  installed?: '' | 'bundled' | 'installed'
  current?: string
  update: boolean
}

export interface PluginCatalog {
  plugins: CatalogPlugin[]
  errors: { url: string; error: string }[]
  catalogs: string[]
  official: string
  platform: string
  installDir: string
}

/** Pages of running plugins a user can open (GET /api/plugins). */
export interface PluginLinks {
  id: string
  name: string
  pages: PluginPage[]
}

export interface DiscoverVideo {
  id: string // YouTube video id
  title: string
  description?: string
  publishedAt: number
}

export interface DiscoverRow {
  id: number
  name: string
  videos: DiscoverVideo[]
}

export interface DiscoverSource {
  id: number
  name: string
  kind: 'playlist' | 'channel'
  youtubeId: string
  position: number
  fetchedAt: number
  error?: string
}

/** A movie or show someone asked for (one per title; later askers join it). */
export interface TitleRequest {
  id: number
  kind: 'movie' | 'show'
  tmdbId: number
  title: string
  year?: number
  poster?: string
  overview?: string
  userId: number
  requestedBy: string
  people: number
  note?: string
  /** parent: a kid asked, waiting for a parent; approved: waiting for the fetching app; sent: it has it; available: in a library. */
  status: 'parent' | 'pending' | 'approved' | 'sent' | 'failed' | 'declined' | 'available'
  reason?: string
  link?: string
  /** Seasons of a show asked for; none is the whole show. */
  seasons?: number[]
  fourK?: boolean
  /** While it's fetched: searching, downloading, importing, or waiting (not out yet). */
  stage?: 'searching' | 'downloading' | 'importing' | 'waiting'
  /** 0-1 */
  progress?: number
  /** Unix seconds. */
  eta?: number
  releaseDate?: string
  availableAt?: number
  createdAt: number
  updatedAt: number
  mine: boolean
}

/** A TMDB search result on the Requests page. */
export interface RequestableTitle {
  kind: 'movie' | 'show'
  tmdbId: number
  title: string
  year?: number
  overview?: string
  poster?: string
  releaseDate?: string
  /** On a person's list: who they played, or their job. */
  role?: string
  itemId?: number // in a library the viewer can see
  onServer?: boolean
  request?: TitleRequest
}

/** A movie or show from TMDB with what the server has of it, for picking seasons. */
export interface RequestTitleInfo {
  title: RequestableTitle
  backdrop?: string
  status?: string
  has4K: boolean
  /** The viewer may ask for the 4K copy. */
  fourK: boolean
  limit: RequestLimit
  /** A kid whose requests go to a parent first. */
  kidsWait: boolean
  seasons?: { number: number; name: string; episodes: number; airDate?: string; onServer: number; requested: boolean }[]
}

export interface RequestLimit {
  /** 0: none */
  limit: number
  used: number
  resetsAt?: number
}

/** Someone saying a movie or episode has a problem. */
export interface ProblemReport {
  id: number
  itemId: number
  userId: number
  user: string
  kind: 'video' | 'audio' | 'subtitles' | 'wrong' | 'other'
  note?: string
  position?: number
  status: 'open' | 'fixing' | 'resolved'
  reply?: string
  createdAt: number
  updatedAt: number
  title: string
  showTitle?: string
  itemKind: string
  seasonNumber?: number
  episodeNumber?: number
  poster?: string
  tmdbId?: number
}

/** Channels an admin says carry a TV network (Admin → Live TV → Networks). */
export interface NetworkChannels {
  network: string
  channels: number[]
}

/** A Chromecast, Google TV or DLNA TV the server can play on. */
export interface CastDevice {
  id: string
  name: string
  kind: 'chromecast' | 'dlna'
  model?: string
  host: string
  manual?: boolean
}

/** Something the server is playing on a TV, with the app as its remote. */
export interface CastSession {
  id: string
  device: CastDevice
  itemId: number
  fileId: number
  title: string
  subtitle?: string
  poster?: string
  duration: number
  position: number
  state: 'loading' | 'playing' | 'paused' | 'idle' | 'finished'
  volume: number // 0-1, -1 when unknown
  mode: 'direct' | 'transcode'
  error?: string
  started: number
}
