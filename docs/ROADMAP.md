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

## M2: Library polish and admin tools

- Admin UI: **Edit details** with tabs (Title, Sort title, Original title, Edition, Originally available, Content rating, Rating, Studio, Tagline, Summary), Genres, Collections, Labels, Artwork (poster/background/logo), Advanced (episode sort order, metadata provider TMDB/TheTVDB, credits detection on/off). Locked fields that survive refreshes.
- Change poster: TMDB alternatives, ThePosterDB scraper, by URL, upload a file. Change banners/backgrounds the same way.
- Tags/labels used for collections ("Lion King Collection") and automatic smart collections ("Original Disney films").
- Fix match / unmatch (search TMDB and pick).
- Metadata providers: TheTVDB, IMDb + Rotten Tomatoes ratings (OMDb or MDBList), user reviews.
- Custom library types: TV, Movies, Kids, Kids TV, Christmas, 4K, YouTube. Admin picks which libraries each user can see.
- Libraries pinned to the sidebar drive the merged Recently Added on Home; admins add collections to Home by right-clicking.
- Scan settings: watch folders for changes (partial scan), scan on a schedule, empty trash toggle.
- Scheduled tasks (with run window): DB backup (+ remote backup), optimize DB, clear old cache, refresh metadata periodically, media analysis, extensive analysis during maintenance.
- Logging and detailed info page.
- Tone mapping algorithm choice and hardware decoding device selection.
- Image-based subtitles (PGS/VobSub) burned in, external `.srt` sidecar files, subtitle settings (size/style), auto-pick English audio track.
- Retry failed playback automatically. Auto quality when bandwidth drops (adaptive bitrate ladder).
- Transcode throttling (don't run far ahead of the player).

## M3: User features

- User settings: skip intro, skip outro, auto-adjust quality, passcode-protected settings.
- **Intro and credits detection** (audio fingerprinting across episodes, black-frame detection).
- Trailers or a custom intro video before a movie (user setting).
- Watchlist ("add to my watchlist or library" from Discover).
- Playlists, plus user-made lists for TV ("what I want to watch next").
- User profile with stats, shareable profile (off by default), recommendations from other users on Home (opt-in).
- Discover: new shows and movie trailers (admin adds YouTube playlists).
- Recently Added and Recently Released per library.
- Watch Together: invite a user and sync playback.
- Admin: watch statistics graphs, top played per library, play history (Tautulli replacement).
- Notifications: new episodes, live sports starting, etc.

## M4: Live TV (Dispatcharr replacement)

- M3U and EPG manager: multiple M3U accounts, multiple EPGs, channel mapping (like Dispatcharr).
- Logo manager, categories, favourites, favourite channels → custom categories, sort through categories.
- Stream proxy: users watching the same channel share one upstream connection; fallback M3U streams per channel; buffering/proxy settings; reconnect logic tuned for remote viewers.
- Live TV stats like Dispatcharr: channel, IP, user, average speed, data, duration, kill stream. Live TV logs.
- TV Guide like the Rogers/Xfinity guide screenshot; mini guide while watching (picture top ¾, channel + summary, guide at the bottom).
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
