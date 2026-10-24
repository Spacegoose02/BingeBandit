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
| `server/internal/stream` | Transcode sessions (ffmpeg → HLS segments), Now Playing tracker |
| `server/internal/api` | HTTP handlers. `auth.go`, `browse.go` (home/items/search), `playback.go`, `admin.go` |
| `server/internal/web` | `go:embed` of `dist/`, which `npm run build` in `web/` fills |
| `web/src` | React 18 + TypeScript + Vite. `pages/`, `components/`, `api.ts`, `types.ts` (mirrors Go JSON) |

## Data model

- **items** is one table for `movie`, `show`, `season` and `episode`. Seasons and episodes have `parent_id` (season → show, episode → season) and `show_id`, which is denormalised so show-level queries stay simple.
- **match_key** (unique per library) is how a rescan finds the same item again: `movie:<normalised title>:<year>`, `show:<title>:<year>`, `…:s1`, `…:s1:e2`. Multiple files with the same key become *versions* of one item (e.g. 4K + 1080p).
- **media_files** hold ffprobe results, including all streams as JSON. `scan_gen` marks which scan last saw a file; after a scan where every root folder was reachable, unseen files are pruned. A missing drive never wipes a library.
- **watch_state** is per user and item. Crossing 90% marks the item watched and resets the position.
- Images are stored as references and turned into URLs on the way out. `tmdb:/abc.jpg` becomes `/api/images/tmdb/abc.jpg?w=w500`, served from a disk cache. `local:/<sha>.png` is admin-uploaded or URL-downloaded art in `<data>/artwork/`, content-addressed.
- **Locked fields:** `items.locked` is a JSON array of field names (`title`, `poster`, `collections`…; see `store.LockableFields`). `UpdateMetadata` wraps every lockable column in `CASE WHEN <locked> THEN <current> ELSE ? END`, so provider refreshes skip locked fields without a read-modify-write. Admin edits replace the lock list; choosing artwork adds its lock.
- **Collections** (`collections`, `collection_items`) belong to one library. Members are explicit links (TMDB `belongs_to_collection`, or the admin's Edit → Tags) **plus**, for smart collections, every item whose `labels` contains `label_rule` (case-insensitive). The membership test is one SQL fragment, `store.memberOf`.
- **Library access:** `users.all_libraries` (default on), or the `user_libraries` rows. Every user-facing item query includes `store.visible`, which binds `@uid`. `uid 0` means the server itself and sees everything. Handlers that start from a library or file id check `CanSeeLibrary`, or load the item as the user, and answer 404 so hidden content isn't revealed.

## Scanning

- **Full scan** (`Scanner.Scan`, `ScanWait`): walks every library folder, then processes files with 4 workers (`probeWorkers`). Each file becomes an item upsert, plus an ffprobe run if its size or mtime changed. Then it prunes, then fetches metadata for new items.
- **Folder watching** (`library/watch.go`, fsnotify): watches every folder under each library root and re-reads the library list every 2 minutes. An event queues its folder for that library. After 30 quiet seconds (copies in progress keep resetting the timer), `ScanFolders` walks just those folders and `PruneUnder` prunes only below them. Network shares may not emit events, so the scheduled full scan is the backstop.
- Only one scan job runs per library at a time (`ErrRunning`). The watcher re-queues if it hits a running scan.

## Auth

- The first run (no users) exposes `POST /api/setup` to create the admin; after that it returns 409.
- Login returns a random 256-bit token. Only its SHA-256 is stored. The web UI uses an HttpOnly cookie; apps send `Authorization: Bearer <token>`. Players that can't set headers can use `?token=` on GET.
- Cookie-authenticated writes must send `X-Requested-With: BingeBandit` (CSRF guard).
- Failed logins are rate-limited per IP (10 per 15 minutes).

## Playback

1. The client fetches `/api/items/{id}`, which includes the files and their streams.
2. **Direct play:** if the browser can decode the container, video and audio (`canDirectPlay` in `Player.tsx`), it streams `/api/files/{id}/direct` with HTTP range support.
3. **Transcode:** otherwise it calls `POST /api/files/{id}/transcode {start, maxHeight, audioStream, replace}`. The server starts ffmpeg writing 4-second HLS segments into `<transcode dir>/bb-<id>/`:
   - H.264 8-bit SDR within the requested height is copied (`-c:v copy`, cheap remux); otherwise it's re-encoded with the configured hardware encoder.
   - HDR is tone mapped with zscale + hable when enabled.
   - Audio is always AAC stereo for now.
4. Seeking inside the transcoded range is a normal seek. Seeking past it starts a new session at that offset (`replace` kills the old one). The player tracks `offset + video.currentTime` as file time.
   - Image subtitles (`burnSubtitle`) switch the video chain to a `-filter_complex` graph: `[0:v:0]<tonemap>[base];[base][0:N]overlay[ov];[ov]<scale,format>[vout]`.
   - With ffmpeg ≥ 6.1 (`readrate_initial_burst`, detected at startup) and the throttle setting on, input is read at 2× real time after a 60-second burst.
   - Text subtitles (embedded or sidecar files, which get stream indexes ≥ 1000 with an `external` file name) are never burned in. The player fetches them as WebVTT from `/api/files/{id}/subtitles/{index}.vtt` and draws them itself.
   - The player chooses audio and subtitle tracks from `/api/me/prefs` (`pickTracks` in `Player.tsx`). Three non-seek stalls within 60s step the quality down one rung when the user allows it.
5. Sessions with no segment or playlist request for 90s are killed by a janitor. The player also deletes its session when it unmounts or on `pagehide`.
6. The player posts `/api/progress` every 10s and on pause. That saves the resume point and keeps `stream.Activity` (Now Playing) alive; entries expire after 45s without a ping.

**Windows gotcha:** ffmpeg replaces `index.m3u8` by renaming a temp file over it. Go's `os.Open` doesn't grant `FILE_SHARE_DELETE`, so reading the playlist while ffmpeg renames freezes it. Always read transcode output with `stream.OpenShared`.

## Conventions

- Add a schema change as a **new** entry in `migrations`; never edit a shipped one.
- All SQL stays in `store`. Handlers do validation, auth and response shaping.
- Item queries bind `@uid` (named parameter) because the select includes per-user watch state. Don't mix `?` and named parameters in one query.
- `web/src/types.ts` mirrors the Go JSON tags. Update both together.
- Settings that admins change at runtime go in `store.SettingDefaults` and the Settings page, not in flags.
