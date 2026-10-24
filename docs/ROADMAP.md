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
| 🟡 | Ability to download files — admin toggle and endpoint exist; no download button yet |

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
| ✅ | Per-user playback preferences: auto-pick audio language (English by default) and subtitles (off / auto for foreign audio / forced only / always), auto-play next episode |
| ✅ | Retry failed playback automatically; fall back from direct play to transcoding |
| ✅ | Lower quality automatically when playback keeps buffering (user setting) |
| ✅ | Transcode throttling (stays ~2× ahead of playback after a 60s head start) |
| | True adaptive bitrate (multi-rendition HLS ladder) |
| | ThePosterDB built-in search (no public API; needs a scraper) |
| | Metadata provider choice per show (TheTVDB), credits detection toggle |
| | Smart collections by genre, studio or year (today they work by label) |

- Metadata providers: TheTVDB, IMDb + Rotten Tomatoes ratings (OMDb or MDBList), user reviews.
- Custom library types: TV, Movies, Kids, Kids TV, Christmas, 4K, YouTube.
- Libraries pinned to the sidebar drive the merged Recently Added on Home; admins add collections to Home by right-clicking.
- Remote backup targets beyond a folder path (S3/Backblaze), and restoring a backup from the UI.
- Tone mapping algorithm choice and hardware decoding device selection.
- Subtitles in a `Subs/` folder (common in release downloads).

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
| | Passcode-protected settings |
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
| | Logo manager (upload and cache logos), favourite channels → custom categories |
| | Buffering/proxy settings, live TV logs view, per-user API keys for the exported playlist |
- Guides from local providers by ZIP (DirecTV, Comcast…) via Schedules Direct / similar.
- DVR: schedule recordings, admin-approval switch, DVR settings, admin chooses whether recordings go into the show's library or a temporary folder.
- Sports tracker with scores and alerts (ESPN API: college football/basketball, NBA, MLB, MLS, NHL, NFL); notifications when a game or show is about to start.
- YouTube live channels plugin: an admin-managed list of channels that turn on and off automatically.

## M5: Custom TV channels (ErsatzTV replacement)

- Channels built from server content, with schedules.
- Channels from RSS / API lists (e.g. Yamtrack pending list), named after the source.
- Everything else ErsatzTV does: filler, commercials/bumpers, watermarks, flex time.

## M6: Users and security

- 2FA (TOTP), strong sign-in methods, passkeys.
- OIDC with TinyAuth, Google and similar.
- Account linking: built-in account, Google, Plex/Tautulli (to import data until Plex shutdown).
- Link a TV with a code (`/link`), and admins can link TVs for users.
- Show others what you're watching (opt-in).
- API keys, a public API, webhooks for playback events.
- Network access settings, LAN networks, remote access settings.

## M7: Import

- Import users' Plex watch history (Plex API / Tautulli DB).
- Import/export progress for trackers such as Yamtrack and Trakt.
- Collections from Plex by library.

## M8: Apps

- **Android TV / Fire TV APK** and phones: React Native (react-native-tvos), talking to the same `/api`.
- **Windows EXE** client (desktop wrapper around the web UI with mpv for full codec support).
- Roku app (BrightScript; separate codebase).
- Casting to devices (Chromecast, DLNA).
- In-app updates, with TV and device updates delivered from the server.
- Push notifications.
- **Must:** an easy way for older, non-technical people to add this to their device (QR / link code, server auto-discovery, big friendly onboarding).

## M9: Plugins

- Plugin system (out-of-process, API-based).
- Yamtrack plugin, Cinemask plugin (search Cinemask for upgrades), Watchlist RSS feeds, Philips Hue sync to movie colours.

## Docs

- Good documentation site (install guides per platform, hardware transcoding guide, naming guide).
