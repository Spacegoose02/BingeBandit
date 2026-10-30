# Architecture

```
┌──────────────────────────────── bingebandit (one Go binary) ─────────────────────────────────┐
│                                                                                               │
│  HTTP (chi) ── /api/*  ──► api ──► store (SQLite, modernc — pure Go, no CGO)                  │
│       │                     ├────► library.Scanner ──► media.Probe (ffprobe)                 │
│       │                     │                     └──► metadata.Service ──► TMDB             │
│       │                     ├────► metadata.ImageCache (disk cache of TMDB art)              │
│       │                     └────► stream.Manager (ffmpeg → HLS) / stream.Activity           │
│       └─ /*  ──► web (embedded React build, SPA fallback to index.html)                       │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Layout

| Path | What lives there |
| --- | --- |
| `server/cmd/bingebandit` | `main`: wires the packages together, handles shutdown |
| `server/internal/config` | Flags/env for things needed before the DB opens (port, data dir, ffmpeg path) |
| `server/internal/store` | **All SQL.** Schema migrations (`schema.go`, append-only), users/sessions, settings, libraries, items, files, watch state |
| `server/internal/library` | Folder walking, filename parsing (`names.go`, with tests), scan status, scheduled rescans |
| `server/internal/media` | ffprobe wrapper, Windows "no console window" helper |
| `server/internal/metadata` | TMDB client, metadata refresh for movies/shows/seasons/episodes, image cache |
| `server/internal/tasks` | Scheduler (1-minute tick, maintenance window, Run now) and the built-in tasks: scan, metadata refresh, backup, optimize, cache cleanup, media analysis |
| `server/internal/logs` | `log` output tee: console, rotating `<data>/logs/bingebandit.log` (10 MB × 3) and a 3000-line ring buffer for the Logs page |
| `server/internal/markers` | Intro/credits detection: ffmpeg decodes mono 5512 Hz audio from each episode's start (≤10 min / 50%) and end (≤7 min / 30%); `Fingerprint` makes a 32-bit hash per ~0.124s frame (33 log-spaced bands, Haitsma–Kalker style); `FindCommon` scores every alignment against a neighbouring episode, then takes the longest run of near-matching frames (≤10 bit errors) and trims sparse edges. Fingerprints are cached in `<data>/cache/fingerprints` |
| `server/internal/stream` | Transcode sessions (ffmpeg → HLS segments), Now Playing tracker |
| `server/internal/api` | HTTP handlers, one file per area: `auth.go`, `browse.go` (home/items/search), `playback.go`, `admin.go`, `live.go`, `household.go` (profiles), `kids.go`, `sports.go`, `together.go`, `notifications.go`, `plugins.go` (plugin API), `overlays.go`, `hlstoken.go` (Roku playlists)… `api.go` has the router |
| `server/internal/livetv` | Live TV: M3U/XMLTV import, the channel relay and failover (`relay.go`, `pace.go`), DVR, custom channels, Dispatcharr import, logos, YouTube links through yt-dlp |
| `server/internal/importer` | Plex/Tautulli import, list and chart sources for collections (MDBList, TMDB, Trakt, IMDb, Yamtrack), Kometa import, default collections |
| `server/internal/trakt` | Trakt device sign-in and history sync |
| `server/internal/plugins` | Plugin host: runs, pairs and proxies out-of-process plugins, and the plugin catalogue |
| `server/internal/sports` | ESPN scoreboards and the tracker that sends game events |
| `server/internal/discover` | YouTube playlists and channels for the Discover page |
| `server/internal/overlay` | Draws Kometa-style badges onto posters |
| `server/internal/s3` | S3-compatible backup upload and download |
| `server/internal/avatars` | The built-in animal profile pictures |
| `server/internal/web` | `go:embed` of `dist/`, which `npm run build` in `web/` fills |
| `server/plugins/*` | Bundled plugins: Watchlist RSS, Hue Sync, Game Lights, Cinemask Upgrades, YouTube Live |
| `server/pkg/bbplugin` | Go kit for writing plugins |
| `web/src` | React 18 + TypeScript + Vite + react-router 6. `pages/` (admin pages in `pages/admin/`), `components/`, `api.ts`, `types.ts` (mirrors Go JSON) |
| `apps/tv` | Android TV / Fire TV / phone app (react-native-tvos) |
| `apps/roku` | Roku channel (BrightScript / SceneGraph) |

## Data model

- **items** is one table for `movie`, `show`, `season` and `episode`. Seasons and episodes have `parent_id` (season → show, episode → season) and `show_id`, which is denormalised so show-level queries stay simple.
- **match_key** (unique per library) is how a rescan finds the same item again: `movie:<normalised title>:<year>`, `show:<title>:<year>`, `…:s1`, `…:s1:e2`. Multiple files with the same key become *versions* of one item (e.g. 4K + 1080p).
- **media_files** hold ffprobe results, including all streams as JSON. `scan_gen` marks which scan last saw a file; after a scan where every root folder was reachable, unseen files are pruned. A missing drive never wipes a library.
- **watch_state** is per user and item. Crossing the `watched_percent` setting (default 90%) marks the item watched and resets the position. A file holding several episodes is stored on the first and linked to the rest in `media_file_episodes`; finishing it finishes them all.
- Images are stored as references and turned into URLs on the way out. `tmdb:/abc.jpg` becomes `/api/images/tmdb/abc.jpg?w=w500`, served from a disk cache. `local:/<sha>.png` is admin-uploaded or URL-downloaded art in `<data>/artwork/`, content-addressed.
- **Locked fields:** `items.locked` is a JSON array of field names (`title`, `poster`, `collections`…; see `store.LockableFields`). `UpdateMetadata` wraps every lockable column in `CASE WHEN <locked> THEN <current> ELSE ? END`, so provider refreshes skip locked fields without a read-modify-write. Admin edits replace the lock list; choosing artwork adds its lock.
- **Collections** (`collections`, `collection_items`) belong to one library. Members are explicit links (TMDB `belongs_to_collection`, or the admin's Edit → Tags) **plus**, for smart collections, every item whose `labels` contains `label_rule` (case-insensitive). The membership test is one SQL fragment, `store.memberOf`.
- **Library access:** `users.all_libraries` (default on), or the `user_libraries` rows. Every user-facing item query includes `store.visible`, which binds `@uid`. `uid 0` means the server itself and sees everything. Handlers that start from a library or file id check `CanSeeLibrary`, or load the item as the user, and answer 404 so hidden content isn't revealed.

## Live TV

- **Sources** (`live_sources`): M3U playlists import into `live_streams` (upserted by URL, with stale entries removed). XMLTV guides import into `epg_channels` and `epg_programs` (12h behind to 8 days ahead). Both are refreshed by the "Refresh Live TV" task according to each source's interval.
- **Channels** (`channels` and `channel_streams`): a channel is a number, name and guide id plus an ordered list of streams, possibly from different providers.
- **Relay** (`livetv.Manager`, one `Relay` per watched channel): a single upstream HTTP connection fans out to every viewer.
  - **Fallback:** streams are tried in priority order, skipping providers at `max_streams` (counted across all running relays). A watchdog moves on to the next stream after a 12s stall.
  - **Raw TS viewers** (`/api/live/channels/{id}/stream.ts`, used by external apps) get chunks through a buffered channel. A viewer that falls 512 chunks behind is dropped.
  - **Web viewers** share one ffmpeg per channel that turns the relayed TS into a rolling HLS playlist (`hls_list_size 8`, `delete_segments`). The first ~3 MB of the stream are probed: H.264 is copied, anything else is converted with yadif deinterlacing.
  - **Viewer lifetime:** web viewers stay counted while their player polls the playlist with `?viewer=`, and expire after 30s without a poll. A relay with no viewers for 15s stops.
- **HLS upstreams:** if the upstream answers with an HLS playlist (by Content-Type, or a `.m3u8` path after redirects), `openStream` hands the URL to ffmpeg (`-c copy -f mpegts`). The relay then reads ffmpeg's stdout exactly like a TS stream.
- **Logos:** `/api/live/channels/{id}/logo` serves the channel's logo from `<data>/cache/logos`. It only fetches URLs already stored on a channel.
- **DVR** (`livetv.Recorder`, tables `recordings` and `recording_rules`): every 10s the recorder starts due recordings and fails ones whose window passed while the server was down. Every 5 minutes it expands active series rules over the next 72h, matching by title.
  - A recording is a `ts` viewer of the shared relay writing to disk, so it rides fail-over and shares the provider connection. If the stream drops it rejoins until `stop_at`.
  - Files go to `dvr_folder`, in either "library" naming (`Show/Season NN/Show - SxxEyy - Subtitle.ts`, or `Show/Show - YYYY-MM-DD HH.MM.ts` without episode numbers) or a flat folder.
  - The TV scanner understands date names (season = year, episode = MMDDhhmm, titled with the air date).
- **Custom channels** (`channels.custom = 1`, `custom_blocks`, `playout_items`): `BuildPlayout` keeps 48h scheduled.
  - **Content:** each block's sources resolve to movies and episodes with a known duration. The block covering a time of day picks the next item; shuffle uses a seeded permutation per cycle, and order uses a persisted cursor.
  - **Guide:** the guide id is `custom.<id>`, and `Programs()` serves `playout_items` for it with negative ids (so `ProgramByID` and the DVR can tell them from XMLTV entries).
  - **Playback:** `Relay.pullCustom` starts one real-time ffmpeg per item at the right offset, normalized to 720p30 H.264 + AAC. `-output_ts_offset` keeps timestamps continuous across items, so the shared HLS output just copies.
- **Testing without a provider:** `go run ./cmd/fakeiptv` serves six ffmpeg test channels (one MPEG-2, one "flaky" that dies after 20s and has a backup feed), an M3U playlist and an XMLTV guide on `:8585`.

## Scanning

- **Full scan** (`Scanner.Scan`, `ScanWait`): walks every library folder, then processes files with 4 workers (`probeWorkers`). Each file becomes an item upsert, plus an ffprobe run if its size or mtime changed. Then it prunes, then fetches metadata for new items.
- **Folder watching** (`library/watch.go`, fsnotify): watches every folder under each library root and re-reads the library list every 2 minutes. An event queues its folder for that library. After 30 quiet seconds (copies in progress keep resetting the timer), `ScanFolders` walks just those folders and `PruneUnder` prunes only below them. Network shares may not emit events, so the scheduled full scan is the backstop.
- Only one scan job runs per library at a time (`ErrRunning`). The watcher re-queues if it hits a running scan.

## Apps

`apps/tv` is the Android TV / Fire TV / phone app (react-native-tvos), and `apps/roku` is the Roku channel (BrightScript). Both use the same `/api` as the web UI, with a Bearer token from the TV code flow (`/api/link/*`). They find servers by probing `/api/health` (which returns `product: "BingeBandit"`, the server name and the server's `id`) across the local /24. Playback is direct when the device can decode the file, else through the transcoder's HLS, and progress goes to `/api/progress` like the web player. See `apps/tv/README.md` and `apps/roku/README.md` for building.

- **Server id and other addresses:** the apps remember the server's `id` and its other addresses (LAN and public). When a request can't connect they try the others, then scan the LAN for the same id, so a TV keeps working when the internet or the public address is down (`apps/tv/src/addresses.ts`, Roku `ApiTask.brs`).
- **Roku and tokens:** Roku's video node doesn't reliably send headers, so it adds `?token=` to URLs, and `api/hlstoken.go` writes the token into every URI of a playlist requested that way.
- **TV app structure:** `App.tsx` keeps a small stack of screens (screens below the top stay mounted and hidden, so Back keeps their scroll and focus). Styles are authored on a 1920×1080 canvas and scaled (`src/theme.ts`); `isPhone` switches to phone layouts.

## People: profiles and kid mode

- A **profile** is a `users` row with `owner_id` set. Everything per person (watch state, prefs, watchlist, Trakt, team picks) keys on the user id, so profiles need nothing special elsewhere. Profiles have no password; the account switches to them (`api/household.go`), with a PIN when one is set.
- **Prefs** (`users.prefs`, JSON, `store/prefs.go`) hold per-person choices such as audio language, alerts and pinned tabs; device-only choices (Hue room, menu position) stay on the device.
- **Kid mode** (`users.kid`, `store/kids.go`): a kid sees only libraries they're given, plus titles and collections a parent approved one by one (`kid_titles`, `kid_collections`, less `kid_blocked`), and only Live TV channels or channel groups approved for kids. All of it is part of `store.visible`.
- The **settings passcode** (`api/passcode.go`) locks admin and account changes per session (423 until unlocked); watching is never locked.

## Background loops

Besides the scheduler's tasks (`tasks/builtin.go`: scans, metadata, ratings, Live TV refresh, custom channel schedules, Discover, artwork caching, backups, database upkeep, intro/credits detection, default collections, list collections), `main.go` starts: folder watching, the DVR recorder, the plugin host, `RunNotifications` (new episodes, live show alerts, every minute) and `RunSports` (the ESPN tracker, every 30 s while a followed game is on, else every 5 min). Events from all of them go through `Server.Emit` to webhooks and plugins.

## Import

`internal/importer` turns a source (Plex API, Tautulli API, Tautulli DB file) into `Record`s. `Matcher` maps each one to a library item (file name → TMDB id → title + year; episodes via their show), and `Runner` writes plays (`plays.import_key`), watched marks, resume points and collections through the store's `Import*` methods. These never remove or un-watch anything and skip duplicates, so any import can be re-run. The API (`api/importing.go`) creates mapped users and starts the runner in the background; the admin page polls `/api/admin/import/status`.

`internal/trakt` wraps Trakt's API (device-code OAuth, history read/write) and a `Syncer` that keeps per-user tokens, imports a user's Trakt history through `importer.RunOnce`, and pushes finished plays (`plays.trakt_at` marks what Trakt already has). The progress handler calls `Syncer.PushNew` when a play finishes.

## Plugins

`internal/plugins.Host` runs out-of-process plugins (protocol in `docs/PLUGINS.md`). Rows live in `plugins` (id, kind, URL, key, settings, last manifest).

- **Managed** plugins are folders with a `plugin.json` in the bundled plugins folder or `<data>/plugins`. `discover` (every minute) adds new ones disabled. `supervise` runs each enabled one on a free loopback port with its key in the environment, logs its output, and restarts it with backoff.
- **Remote** plugins are added by URL: `AddRemote` reads the manifest, sends a new key to `/bingebandit/pair` (with the old key when re-adding), and stores it.
- `Sync` makes running instances match the table; the manifest fetch is the health check. `Server.Emit` fans events out to webhooks and to plugins subscribed to them.
- `Proxy` serves `/api/plugins/{id}/*` from the plugin, swapping the user's credentials for the plugin key and user headers, refusing `/bingebandit/*` paths and dropping `Set-Cookie`.
- The plugin API (`api/plugins.go`, `/api/plugin/v1`) authenticates by key (`PluginByKey`) and reuses the user API's handlers: as the server (uid 0, sees everything) by default, or as a user under `/users/{uid}/…` (`asUser`).
- `pkg/bbplugin` is the Go kit plugins use; `plugins/watchlist-rss` is the bundled reference plugin.

## Auth

- The first run (no users) exposes `POST /api/setup` to create the admin; after that it returns 409.
- Login returns a random 256-bit token. Only its SHA-256 is stored. The web UI uses an HttpOnly cookie; apps send `Authorization: Bearer <token>`. Players that can't set headers can use `?token=` on GET.
- Cookie-authenticated writes must send `X-Requested-With: BingeBandit` (CSRF guard).
- Failed logins are rate-limited per IP (10 per 15 minutes).
- API keys (`bb_` + random, `api_keys` table) are stored as SHA-256 like sessions. `requireAuth` tries a session first, then an API key, so a key works anywhere a token does.
- Two-step sign-in: RFC 6238 TOTP (`api/totp.go`, SHA-1, 30s, 6 digits, ±1 step). Recovery codes are stored hashed and deleted on use. Login answers 401 with `needsCode: true` until a valid code is sent.
- TV linking (`api/link.go`): an in-memory map of short codes → poll tokens (10-minute TTL, start rate-limited per IP). Approving sets the user; the next poll creates the session.
- Single sign-on (`api/oidc.go`, `go-oidc` + `x/oauth2`): providers in `oidc_providers`, links in `user_identities` (provider + subject). Auth code + PKCE + nonce; the pending state is in memory. The callback creates an ordinary session cookie.
- Network (`api/network.go`): the outermost middleware rewrites `RemoteAddr`, the scheme and the host from trusted proxies' forwarded headers, then blocks non-local clients when remote access is off. Use `clientIP`, `isHTTPS`, `baseURL` and `externalURL` rather than reading the request directly.
- Webhooks (`api/webhooks.go`): `Server.Emit(event, data)` posts to each enabled, subscribed hook in a goroutine (10s timeout), signing with HMAC-SHA256 when a secret is set. The DVR reports through `Recorder.OnEvent`, wired in `main.go`.

## Playback

1. The client fetches `/api/items/{id}`, which includes the files and their streams.
2. **Direct play:** if the browser can decode the container, video and audio (`canDirectPlay` in `Player.tsx`), it streams `/api/files/{id}/direct` with HTTP range support.
3. **Transcode:** otherwise it calls `POST /api/files/{id}/transcode {start, maxHeight, audioStream, replace}`. The server starts ffmpeg writing 4-second HLS segments into `<transcode dir>/bb-<id>/`:
   - H.264 8-bit SDR within the requested height is copied (`-c:v copy`, cheap remux); otherwise it's re-encoded with the configured hardware encoder.
   - HDR is tone mapped with zscale + hable when enabled.
   - `encodeVideo` forces a re-encode of H.264 that would otherwise be copied, for slow devices that stutter on direct play.
   - Audio follows what the client says it can play (`audioCodecs`, `maxAudioChannels`): a track it can take is copied; a surround track it can't is turned into `surroundCodec` (AC-3 or E-AC-3) or AAC 5.1; anything else becomes AAC stereo (`audioArgs` in `stream/transcode.go`).
4. Seeking inside the transcoded range is a normal seek. Seeking past it starts a new session at that offset (`replace` kills the old one). The player tracks `offset + video.currentTime` as file time.
   - Image subtitles (`burnSubtitle`) switch the video chain to a `-filter_complex` graph: `[0:v:0]<tonemap>[base];[base][0:N]overlay[ov];[ov]<scale,format>[vout]`.
   - With ffmpeg ≥ 6.1 (`readrate_initial_burst`, detected at startup) and the throttle setting on, input is read at 2× real time after a 60-second burst.
   - Text subtitles (embedded or sidecar files, which get stream indexes ≥ 1000 with an `external` file name) are never burned in. The player fetches them as WebVTT from `/api/files/{id}/subtitles/{index}.vtt` and draws them itself.
   - The player chooses audio and subtitle tracks from `/api/me/prefs` (`pickTracks` in `Player.tsx`). Three non-seek stalls within 60s step the quality down one rung when the user allows it.
5. Sessions with no segment or playlist request for 90s are killed by a janitor. The player also deletes its session when it unmounts or on `pagehide`.
6. The player posts `/api/progress` every 10s and on pause. That saves the resume point and keeps `stream.Activity` (Now Playing) alive; entries expire after 45s without a ping.

**Windows gotcha:** ffmpeg replaces `index.m3u8` by renaming a temp file over it. Go's `os.Open` doesn't grant `FILE_SHARE_DELETE`, so reading the playlist while ffmpeg renames freezes it. Always read transcode output with `stream.OpenShared`.

## Conventions

- Add a schema change as a **new** entry in `migrations`; never edit a shipped one. `migrate_test.go` keeps the hash of every shipped migration and fails if one changes.
- More rules, the build, checks and releases: [DEVELOPMENT.md](DEVELOPMENT.md).
- All SQL stays in `store`. Handlers do validation, auth and response shaping.
- Item queries bind `@uid` (named parameter) because the select includes per-user watch state. Don't mix `?` and named parameters in one query.
- `web/src/types.ts` mirrors the Go JSON tags. Update both together.
- Settings that admins change at runtime go in `store.SettingDefaults` and the Settings page, not in flags.
