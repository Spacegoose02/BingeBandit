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
  allLibraries: boolean
  libraryIds?: number[]
}

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
  liveFollows: string[] | null
  liveAlertMinutes: number
  quietWhilePlaying: boolean
}

export interface SportsLeague {
  id: string
  name: string
  sport: 'football' | 'basketball' | 'baseball' | 'hockey' | 'soccer'
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
  /** The person's teams playing in it. */
  mine?: string[]
  /** Live TV channels whose guide shows it. */
  channels?: { id: number; number: number; name: string; title: string }[]
}

export interface SportsHome {
  leagues: SportsLeague[]
  teams: SportsTeam[]
  games: SportsGame[]
  error?: string
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
  kind: 'intro' | 'credits'
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

export interface Library {
  id: number
  name: string
  kind: 'movies' | 'shows'
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
  tvdbId?: number // shows that take their metadata from TheTVDB
  tvdbOrder?: string // '' (aired) | dvd | absolute
  trailers?: LocalTrailer[] // movies: trailer files from the movie's folder
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
