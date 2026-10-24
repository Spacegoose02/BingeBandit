<p align="center">
  <img src="Brand/09_Horizontal_Lockup_LightBG.png" alt="BingeBandit" width="420">
</p>

**BingeBandit** is a self-hosted media server that aims to replace Plex, Jellyfin, Dispatcharr and ErsatzTV with one app: your movies and shows, live TV with a real guide and DVR, and custom TV channels built from your own library.

> Status: **Milestone 1 (core media server)** is working. See [docs/ROADMAP.md](docs/ROADMAP.md) for everything that's planned.

## What works today

- Movie and TV libraries from any folders, with automatic scanning (and periodic rescans)
- Filename parsing for `Movie (Year)`, scene-style names, `S01E02`, `1x02`, `Season 01/` folders and `{tmdb-12345}` hints
- TMDB metadata: posters, backdrops, logos, ratings, genres, cast & crew, trailers, collections, content ratings
- A Plex-style home: hero spotlight, Continue Watching (with next-episode "On Deck"), Recently Added and Recently Released
- Movie, show, season and episode pages; mark watched and unwatched; trailer playback
- A web player that plays directly when the browser can, otherwise transcodes to HLS with FFmpeg
  - quality switching, audio-track switching and text subtitles
  - resume, auto-play of the next episode, keyboard shortcuts
  - hardware encoding (NVENC, Quick Sync, VAAPI, AMF, VideoToolbox) and HDR→SDR tone mapping
- Users with admin and regular roles, an admin dashboard with Now Playing (local/remote, IP, direct vs transcode, source → target) and library stats
- A single binary with the web UI embedded, for Windows, Linux, macOS and Docker/Unraid

## Run it

### Windows (from source)

Requirements: [Go 1.26+](https://go.dev/dl/), [Node 22+](https://nodejs.org/), and [FFmpeg](https://www.gyan.dev/ffmpeg/builds/) on your PATH.

```powershell
.\build.ps1
.\dist\bingebandit.exe
```

Open http://localhost:8484. The first visit asks you to create the admin account.

### Docker / Unraid

```bash
docker compose up -d
```

See [docker-compose.yml](docker-compose.yml) for media volumes and GPU passthrough.

### Options

| Flag        | Env var       | Default                         |
| ----------- | ------------- | ------------------------------- |
| `-addr`     | `BB_ADDR`     | `:8484`                         |
| `-data`     | `BB_DATA_DIR` | `%AppData%\BingeBandit` / `~/.config/BingeBandit` |
| `-ffmpeg`   | `BB_FFMPEG`   | `ffmpeg` (from PATH)            |
| `-ffprobe`  | `BB_FFPROBE`  | `ffprobe` (from PATH)           |

Everything else (TMDB key, hardware encoder, transcode folder, scan interval…) is under **Settings** in the web UI.

## Develop

```powershell
# terminal 1: API server (rebuild after Go changes)
cd server; go run ./cmd/bingebandit -data ..\data

# terminal 2: web UI with hot reload, proxies /api to :8484
cd web; npm install; npm run dev
```

Then open http://localhost:5173. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before making big changes, and [HANDOFF.md](HANDOFF.md) for where things stand.

## Releasing

Push a tag such as `v0.1.0`. GitHub Actions then builds binaries for Windows, Linux and macOS, attaches them to a GitHub Release, and publishes a multi-arch Docker image to `ghcr.io/<owner>/bingebandit`.
