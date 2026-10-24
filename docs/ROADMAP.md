# Roadmap

Every card from the planning board, grouped into milestones in build order. ✅ = done, 🟡 = partly done, blank = not started.

## M1: Core media server ✅ (current)

| | Item |
|---|---|
| ✅ | Server for Windows / Linux / Docker / Unraid (single binary, Dockerfile, compose, GitHub Release workflow) |
| ✅ | Movies and TV libraries, scan on add, periodic rescans, empty trash for missing files (safe when drives are offline) |
| ✅ | TMDB metadata: posters, backdrops, logos, ratings, genres, studio/network, content rating |
| ✅ | Cast & crew, trailers, "Other movies in this collection" |
| ✅ | Shows → seasons (with season covers) → episodes |
| ✅ | Home like Plex: hero, Continue Watching (+ next episode), Recently Added (merged per media type), Recently Released |
| ✅ | Mark as watched / unwatched (movie, episode, season, whole show) |
| ✅ | Continue watching with progress per user |
| ✅ | Video player: direct play, HLS transcode, quality, audio tracks, text subtitles, auto next episode |
| ✅ | GPU support / hardware encoder selection (NVENC, QSV, VAAPI, AMF, VideoToolbox), tone mapping toggle |
| ✅ | Admin setting: transcode temp folder |
| ✅ | Admin dashboard: Now Playing with local/remote, IP, direct vs transcode, source → target; library stats |
| ✅ | Show file quality (resolution, codec, HDR, audio, size) |
| ✅ | Mobile-friendly web UI |
| ✅ | Users and admins, password login, rate-limited sign-in |
| ✅ | Download original files: a button on movie and episode pages, shown to admins and, when the admin allows downloads, to everyone |

## M2: Library polish and admin tools 🟡 (in progress)

| | Item |
|---|---|
| ✅ | **Edit details** dialog: Title, Sort title, Original title, Edition, Originally available, Content rating, Rating, Studio/Network, Tagline, Summary; Tags tab (Genres, Labels, Collections); Artwork tab; Advanced tab (episode order). Edited fields lock and survive refreshes |
| ✅ | Change poster, background and logo: TMDB alternatives, by URL (paste from ThePosterDB, Fanart.tv…), upload a file |
| ✅ | Collections: TMDB collections automatically, hand-picked collections, and **smart collections** that include everything with a label ("Original Disney films") |
| ✅ | Fix match (search TMDB and pick) |
| ✅ | Admin picks which libraries each user can see |
| ✅ | Scan settings: watch folders for changes (partial scan of just the changed folders), scheduled full scans, empty trash for removed files |
| ✅ | Scheduled tasks with a maintenance window: database backup (to any folder, e.g. a NAS, with retention), optimize database, clean up cache, refresh metadata, media analysis |
| ✅ | Logging and detailed info: Logs page with live tail, filters and download; rotating log file |
| ✅ | Parallel probing for faster first scans |
| ✅ | Subtitles: external `.srt/.ass/.vtt` sidecars (language, forced, SDH from the file name), image subtitles (PGS/VobSub) burned in, per-user size and background |
| ✅ | Subtitles in a `Subs/` folder (release downloads): `Subs/2_English.srt` for a movie, `Subs/<episode>/…` and `Subs/<episode>.en.srt` for TV |
| ✅ | Per-user playback preferences: auto-pick audio language (English by default) and subtitles (off / auto for foreign audio / forced only / always), auto-play next episode |
| ✅ | Retry failed playback automatically; fall back from direct play to transcoding |
| ✅ | Lower quality automatically when playback keeps buffering (user setting) |
| ✅ | Transcode throttling (stays ~2× ahead of playback after a 60s head start) |
| ✅ | Restore a database backup from Admin → Tasks (the current database is backed up first) |
| ✅ | Download a backup, and restore from an uploaded file: in Admin → Tasks, or on a new server's first screen instead of creating an admin |
| | True adaptive bitrate (multi-rendition HLS ladder) |
| ✅ | ThePosterDB search in Change poster (reads their pages; no public API) |
| | Metadata provider choice per show (TheTVDB), credits detection toggle |
| ✅ | Smart collections by genre, studio, year range or label |

- Metadata providers: TheTVDB, IMDb + Rotten Tomatoes ratings (OMDb or MDBList), user reviews.
- Custom library types: TV, Movies, Kids, Kids TV, Christmas, 4K, YouTube.
- Libraries pinned to the sidebar drive the merged Recently Added on Home; admins add collections to Home by right-clicking.
- Remote backup targets beyond a folder path (S3/Backblaze).
- Tone mapping algorithm choice and hardware decoding device selection.

## M3: User features 🟡 (in progress)

| | Item |
|---|---|
| ✅ | Watchlist (bookmark on movies and shows, Home row, finished movies drop off) |
| ✅ | Playlists, which double as "my own lists for TV": add movies, episodes, seasons or whole shows; reorder; Play all with continuous playback |
| ✅ | Watch history per user, plus a server-wide history with IP, client and direct/transcode |
| ✅ | User profile with stats (hours, movies, episodes, shows, top genres and shows, monthly chart, favorite time); shareable with other users, off by default |
| ✅ | Admin statistics: plays per day, top movies, shows and viewers, direct play vs. transcode, clients (Tautulli replacement) |
| ✅ | Auto-adjust quality user setting (done in M2) |
| ✅ | **Intro and credits detection** (audio fingerprints compared across episodes) with Skip Intro / Next buttons; per-user skip settings (button / automatic / off); admins can set or fix times by hand |
| | Credits detection for movies (no sibling episodes to compare; needs black-frame/text detection) |
| | Trailers or a custom intro video before a movie (user setting) |
| ✅ | **Settings passcode**: a per-user 4–8 digit passcode locks Account changes and the Admin pages in the web UI until it (or the password) is entered; unlocks last 15 minutes of use; apps and API keys aren't affected; admins can clear a forgotten one |
| | Recommendations from other users on Home (opt-in) |
| | Discover: new shows and movie trailers (admin adds YouTube playlists) |
| | Watch Together: invite a user and sync playback |
| | Notifications: new episodes, live sports starting, etc. |

## M4: Live TV (Dispatcharr replacement) 🟡 (phase 1 done)

| | Item |
|---|---|
| ✅ | M3U and EPG manager: multiple M3U accounts (with connection limits and user agent), multiple XMLTV guides (gzip OK), scheduled refresh |
| ✅ | Channels from playlist streams (by group or hand-picked), numbering, categories, logos, guide matching by tvg-id with manual override |
| ✅ | **Shared stream proxy**: everyone on a channel shares one upstream connection; fallback streams per channel switch automatically on errors or stalls; provider connection limits respected |
| ✅ | Automatic browser compatibility: H.264 passed through, MPEG-2/HEVC broadcast converted (with deinterlacing) |
| ✅ | Live TV stats like Dispatcharr: channel, now playing, stream/provider, current and average Mbps, data, uptime, fail-overs, viewers with IP/user/player, stop channel or kick viewer |
| ✅ | TV guide like the Rogers screenshot, favourites, category filter |
| ✅ | Live player with channel up/down, now/next, and a mini guide (picture on top, guide at the bottom) |
| ✅ | M3U + XMLTV output so other IPTV apps (TiviMate, VLC, Plex) can use BingeBandit's channels |
| ✅ | HLS (`.m3u8`) upstreams, remuxed by ffmpeg without re-encoding |
| ✅ | Channel logos cached by the server |
| ✅ | **DVR**: record a program or a whole series from the guide, padding, recordings named for a TV library (or one folder), admin approval switch, let-users-record switch, Recordings page; date-named recordings show up as episodes |
| ✅ | Logo library (upload your own logos, match them to channels by name), your own channel categories (start from your favourites) on the web and TV guide |
| | DVR: conflict handling when providers are at their connection limit, keep-last-N episodes per series |
| ✅ | Buffering/proxy settings, live TV logs view, per-user API keys for the exported playlist |
- Guides from local providers by ZIP (DirecTV, Comcast…) via Schedules Direct / similar.
- Sports tracker with scores and alerts (ESPN API: college football/basketball, NBA, MLB, MLS, NHL, NFL); notifications when a game or show is about to start.
- YouTube live channels plugin: an admin-managed list of channels that turn on and off automatically.

## M5: Custom TV channels (ErsatzTV replacement) 🟡 (phase 1 done)

| | Item |
|---|---|
| ✅ | Channels built from server content: libraries, shows, collections (including smart label collections), playlists, labels |
| ✅ | Schedules: time-of-day blocks over an all-day default; shuffle (each item once per cycle) or in order (continues where it left off) |
| ✅ | 48-hour playout in the guide; plays like any live channel (shared, DVR-able, in Activity and the M3U export) |
| | Channels from RSS / API lists (e.g. Yamtrack pending list), named after the source |
| | Filler, commercials/bumpers, watermarks, flex time, hard block boundaries |

## M6: Users and security 🟡 (phase 2 done)

| | Item |
|---|---|
| ✅ | 2FA (TOTP) with one-time recovery codes; admins can turn it off for a user who lost their phone |
| ✅ | Link a TV with a code (`/link`); admins can link a TV as another user; the login page can sign in by code |
| ✅ | Signed-in devices list with remote sign-out |
| ✅ | Show others what you're watching (opt-in), as a "Friends are watching" row on Home |
| ✅ | API keys (`bb_…`, hashed, last-used time), also for IPTV app playlist/guide URLs |
| ✅ | Webhooks for playback start/stop/finished and recording started/completed/failed, HMAC-signed |
| ✅ | Single sign-on with any OpenID Connect provider (Google, TinyAuth, Authelia, Authentik, Keycloak), linking accounts, optional account creation |
| ✅ | A documented public API ([API.md](API.md)) |
| ✅ | Network settings: public URL, extra LAN networks, trusted reverse proxies, remote access on/off |
| ✅ | **Profiles** under one account (Netflix / Plex Home style): "Who's watching?" after sign-in on the web and the TV app, separate history, Continue Watching, watchlist and preferences, a PIN per profile, and library limits for kids |
| | Passkeys |
| | Plex/Tautulli account linking (with M7 import) |

## M7: Import ✅ (phases 1–2 done)

| | Item |
|---|---|
| ✅ | Plex watch history for every account (Plex API), the owner's watched marks and resume points |
| ✅ | Tautulli history through its API, or straight from a copy of `tautulli.db` (works after Plex is gone) |
| ✅ | Map source users to BingeBandit users, or create them |
| ✅ | Collections from Plex, by library |
| ✅ | Preview (dry run), a list of what didn't match, and safe re-runs (no duplicates, even across Plex and Tautulli) |
| ✅ | Trakt: connect per user with a code, import Trakt history, send history, automatic sync of new watches |
| ✅ | Yamtrack and other trackers: through Trakt, or Plex-style webhooks |
| ✅ | Download your history as CSV (with TMDB ids) and as a Letterboxd import file |
| | Watched state for managed/shared Plex users (needs each user's token, via plex.tv) |
| | Simkl and other trackers with their own APIs |

## M8: Apps 🟡 (phases 1–3: Android TV and phone app)

| | Item |
|---|---|
| ✅ | **Android TV / Fire TV APK** (`apps/tv`, react-native-tvos): home rows, details, seasons/episodes, player with resume/next episode/progress, Live TV with channel up/down |
| ✅ | Easy setup: finds the server on the home network, signs in with a code or QR code (or password + 2FA), big remote-friendly screens |
| ✅ | APK built and attached to GitHub releases (signed when the key secrets are set) |
| ✅ | TV player control bar (⟲ 10 s / play-pause / 30 s ⟳ / next episode / audio & subtitles); audio and subtitle choice from the viewer's preferences, text subtitles drawn by the app, image subtitles burned in |
| ✅ | Cable-box style guide grid: timeline, programs sized by length, details and a live preview of the focused channel |
| ✅ | Search; watchlist add/remove on details |
| ✅ | In-app updates: the server checks GitHub for the newest TV APK (`/api/apps/android`); the app downloads it and hands it to Android's installer |
| ✅ | **Android phones** (same APK): touch layouts, bottom tabs, tap controls and a draggable seek bar, landscape video, swipeable guide, swipe to change channel |
| ✅ | Kid lock in the phone players: lock with a 2-digit PIN; taps and Back do nothing until it's entered |
| ✅ | Unraid container template and install guide (`unraid/`, `docs/UNRAID.md`) |
| | iPhone / iPad and Apple TV (react-native-tvos supports tvOS; needs a Mac to build) |
| | **Windows EXE** client (desktop wrapper around the web UI with mpv for full codec support) |
| | Roku app (BrightScript; separate codebase) |
| | Casting to devices (Chromecast, DLNA) |
| | Push notifications |

## M9: Plugins 🟡 (phases 1–2: plugin system, Hue Sync)

| | Feature |
|---|---|
| ✅ | **Plugin system** (out-of-process, API-based): plugins are HTTP services, started by BingeBandit from a plugins folder or added by address (own container). Manifest, settings form, events, user pages through a proxy, versioned plugin API (`/api/plugin/v1`), Go kit (`pkg/bbplugin`). See [PLUGINS.md](PLUGINS.md) |
| ✅ | **Watchlist RSS** (bundled): each person's watchlist as an RSS feed; subscribed RSS feeds add matching library items to the watchlist; optionally remove finished movies |
| | Yamtrack plugin (Yamtrack already works today through a Plex-style webhook or Trakt; a plugin could add history import and per-user setup) |
| | Cinemask plugin: search Cinemask for upgrades of library files |
| ✅ | **Hue Sync** (bundled): Hue lights follow the colours on screen, per zone (left, right, centre…), in step with the player; pause and stop behaviour, filters for people and devices |
| | Hue Entertainment API streaming (25 updates a second, like the Hue Sync box) |
| | Plugin catalogue: install plugins from a list instead of copying folders |

## Docs

- Good documentation site (install guides per platform, hardware transcoding guide, naming guide).
