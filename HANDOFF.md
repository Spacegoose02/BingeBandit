# Handoff

Snapshot of where BingeBandit stands, written so the next person (or AI session) can pick it up cold.

_Last updated: 2026-10-23, first half of Milestone 2._

## Latest: library admin tools (M2)

- **Edit dialog** (pencil on any item page, admins only): Details, Tags (genres, labels, collections), Artwork (TMDB picks, URL, upload) and Advanced (episode order, shows only). Editing a field locks it; the lock icon toggles it.
- **Fix match** (magnifier on movies and shows): searches TMDB, re-points the item and refreshes it.
- **Collections:** a Collections tab on each library and `/collection/:id` pages. Smart collections take a label rule.
- **Library access:** Users → "All libraries" or "N libraries" button per non-admin user.
- Schema migration 2 adds these; it was tested upgrading a v1 database. Store tests cover locks, visibility and collections.
- **Not tested yet:** the TMDB-backed parts (artwork alternatives, match search, TMDB collection linking) need a real key.

## Milestone 1 state

- **Milestone 1 (core media server) works end to end** and was tested in a browser with generated media:
  - setup → admin account → add Movies and TV libraries → scan → home rows → item pages
  - direct play (H.264/AAC MP4) with resume from a timestamp
  - transcode (HEVC/AC3 MKV → H.264/AAC HLS), quality switch to 720p, embedded SRT subtitles
  - auto-play of the next episode, watched marking, Continue Watching
  - Now Playing on the admin dashboard
- `go vet`, `go test` (filename parser) and `tsc` all pass.
- **Not tested yet:** TMDB metadata against the real API (needs a key: Settings → Metadata), hardware encoders, the Docker image build, the GitHub release workflow, Linux/macOS builds.

## Decisions made

| Decision | Why |
| --- | --- |
| Go server, single binary, web UI embedded | Simple installs on Windows, Unraid and Docker; strong at concurrent streaming |
| SQLite via `modernc.org/sqlite` (pure Go) | No CGO, so cross-compiling every OS from one CI runner just works |
| React + Vite web UI | Shares code and ideas with the planned React Native TV/phone apps |
| HLS via ffmpeg, 4-second segments, "event" playlists | Works in every browser (hls.js) and natively on Apple/Android TV |
| Accent colour teal `#2dd4bf` on a near-black UI | Fits the grey low-poly raccoon brand; distinct from Plex's amber |

## Dev environment on this machine

- Go, Node 22, MinGit and FFmpeg are portable installs in `%LOCALAPPDATA%\Programs` (on the user PATH). winget crashed when run non-interactively.
- `.claude/launch.json` runs `dist\bingebandit.exe` against a scratch data folder for previews.
- Build everything: `.\build.ps1`

## Known gaps and next steps

1. Add a TMDB key and run "Refresh metadata". Check movie/show matching on a real library and tune `library/names.go` with new test cases.
2. Rest of M2: scan on file changes, scheduled tasks (DB backup, cache cleanup), TheTVDB, OMDb ratings (see ROADMAP).
3. Transcoding: throttle ffmpeg so it doesn't run far ahead; add an adaptive bitrate ladder; burn in image subtitles; add external `.srt` sidecars.
4. Scanning probes files one at a time. Add a small worker pool for large first scans.
5. Transcode sessions don't re-check library access after they start (they're bound to the user who started them).
6. Audio is always transcoded to AAC stereo. Add 5.1 passthrough for capable clients.
7. Then start M4 Live TV, the biggest differentiator.
