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
| `server/internal/stream` | Transcode sessions (ffmpeg → HLS segments), Now Playing tracker |
| `server/internal/api` | HTTP handlers. `auth.go`, `browse.go` (home/items/search), `playback.go`, `admin.go` |
| `server/internal/web` | `go:embed` of `dist/`, which `npm run build` in `web/` fills |
| `web/src` | React 18 + TypeScript + Vite. `pages/`, `components/`, `api.ts`, `types.ts` (mirrors Go JSON) |

## Data model

- **items** is one table for `movie`, `show`, `season` and `episode`. Seasons and episodes have `parent_id` (season → show, episode → season) and `show_id`, which is denormalised so show-level queries stay simple.
- **match_key** (unique per library) is how a rescan finds the same item again: `movie:<normalised title>:<year>`, `show:<title>:<year>`, `…:s1`, `…:s1:e2`. Multiple files with the same key become *versions* of one item (e.g. 4K + 1080p).
- **media_files** hold ffprobe results, including all streams as JSON. `scan_gen` marks which scan last saw a file; after a scan where every root folder was reachable, unseen files are pruned. A missing drive never wipes a library.
- **watch_state** is per user and item. Crossing 90% marks the item watched and resets the position.
- Images are stored as references (`tmdb:/abc.jpg`) and turned into `/api/images/tmdb/abc.jpg?w=w500` URLs on the way out, served from a disk cache.

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
5. Sessions with no segment or playlist request for 90s are killed by a janitor. The player also deletes its session when it unmounts or on `pagehide`.
6. The player posts `/api/progress` every 10s and on pause. That saves the resume point and keeps `stream.Activity` (Now Playing) alive; entries expire after 45s without a ping.

**Windows gotcha:** ffmpeg replaces `index.m3u8` by renaming a temp file over it. Go's `os.Open` doesn't grant `FILE_SHARE_DELETE`, so reading the playlist while ffmpeg renames freezes it. Always read transcode output with `stream.OpenShared`.

## Conventions

- Add a schema change as a **new** entry in `migrations`; never edit a shipped one.
- All SQL stays in `store`. Handlers do validation, auth and response shaping.
- Item queries bind `@uid` (named parameter) because the select includes per-user watch state. Don't mix `?` and named parameters in one query.
- `web/src/types.ts` mirrors the Go JSON tags. Update both together.
- Settings that admins change at runtime go in `store.SettingDefaults` and the Settings page, not in flags.
