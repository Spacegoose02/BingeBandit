<p align="center">
  <img src="Brand/09_Horizontal_Lockup_LightBG.png" alt="BingeBandit" width="420">
</p>

**BingeBandit** is a self-hosted media server that aims to replace Plex, Jellyfin, Dispatcharr and ErsatzTV with one app: your movies and shows, live TV with a real guide and DVR, and custom TV channels built from your own library.

![BingeBandit Home](docs/screenshots/home.webp)

**New here? Read the [user guide](docs/GUIDE.md).** It walks through installing, adding your media, Live TV, profiles, kid mode and the TV apps, with screenshots.

> BingeBandit is under active development and pre-1.0. See [docs/ROADMAP.md](docs/ROADMAP.md) for what's done and what's planned.

## Features

**Your library**
- Movie and TV libraries from any folders, picked up automatically as files change
- Posters, backdrops, ratings, cast, trailers and collections from TMDB, with optional TheTVDB, MDBList, IMDb and Rotten Tomatoes scores
- A Plex-style Home with Continue Watching, pinned libraries and recommendation rows you choose
- Collections that follow smart rules or TMDB, Trakt, IMDb and MDBList lists, plus Kometa-style poster overlays
- Watchlist, shared playlists, watch history, Discover and Sports pages

**Watching**
- Plays directly when the device can, otherwise transcodes with FFmpeg, including hardware encoding (NVENC, Quick Sync, VAAPI, AMF, VideoToolbox) and HDR to SDR tone mapping
- Audio and subtitle choice, resume across devices, skip intro and credits, auto-play next episode, Watch Together
- Apps for Android TV, Google TV, Fire TV and Android phones, an early Roku channel, and a web app that works on phones

**Live TV**
- M3U and XMLTV sources, channels with automatic backup streams, a cable-box style guide and a DVR
- Custom 24/7 channels from your own library, one-click import from Dispatcharr, and M3U, XMLTV and Plex tuner output for other apps

**Your household**
- Users and profiles with "Who's watching?", PINs, passkeys, two-step sign-in and single sign-on
- Kid mode: kids see only the titles, collections and channels you approve
- TVs sign in with a code from your phone, so nobody types a password on a remote

**Running it**
- A single binary with the web app built in, for Windows, Linux, macOS and Docker/Unraid
- Admin dashboard with Now Playing, statistics, scheduled tasks, backups (optionally to S3), logs and Plex/Tautulli/Trakt import
- [Plugins](docs/PLUGINS.md) that run as their own processes: Watchlist RSS, Hue Sync, Game Lights and Cinemask Upgrades are bundled
- An [HTTP API](docs/API.md) for scripts and your own apps

## Screenshots

| | |
|---|---|
| ![Movie page](docs/screenshots/movie.webp) | ![Live TV guide](docs/screenshots/live-guide.webp) |
| **Movie page** | **Live TV guide** |
| ![Library](docs/screenshots/library.webp) | ![Season page](docs/screenshots/season.webp) |
| **Library** | **Season page** |
| ![Kid profile](docs/screenshots/kids-home.webp) | ![Dashboard](docs/screenshots/admin-dashboard.webp) |
| **Kid profile** | **Admin dashboard** |

The screenshots use made-up titles with generated artwork. More are in the [user guide](docs/GUIDE.md).

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

See [docker-compose.yml](docker-compose.yml) for media volumes and GPU passthrough. On Unraid, follow [docs/UNRAID.md](docs/UNRAID.md): it covers the container template, paths, hardware transcoding and the apps.

To put the TV app on a friend's or family member's TV in another home, see [docs/TV-SETUP.md](docs/TV-SETUP.md).

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

Then open http://localhost:5173.

To try Live TV without an IPTV subscription, run the bundled fake provider and add `http://localhost:8585/playlist.m3u` and `http://localhost:8585/epg.xml` as sources under Server → Live TV:

```powershell
cd server; go run ./cmd/fakeiptv
```

To try single sign-on, run the fake OpenID Connect provider and add it under Server → Settings → Single sign-on with issuer `http://localhost:8686`, client ID `bingebandit` and secret `dev-secret`:

```powershell
cd server; go run ./cmd/fakeoidc
```

To try importing, run the fake Plex + Tautulli + Trakt server and use Server → Import with `http://localhost:32400` and token `fake-plex-token`, or `http://localhost:8181` and API key `fake-tautulli-key`. For Trakt, start BingeBandit with `-trakt-api http://localhost:8282` and use client ID `fake-trakt-client` and secret `fake-trakt-secret` under Settings → Trakt:

```powershell
cd server; go run ./cmd/fakeplex
```

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before making big changes, and [HANDOFF.md](HANDOFF.md) for where things stand. The HTTP API for scripts and apps is described in [docs/API.md](docs/API.md).

## Releasing

Push a tag such as `v0.1.0`. GitHub Actions then builds binaries for Windows, Linux and macOS, attaches them to a GitHub Release, and publishes a multi-arch Docker image to `ghcr.io/<owner>/bingebandit`.

## License

BingeBandit is licensed under the [GNU Affero General Public License v3.0](LICENSE). You can use, modify and share it. If you distribute a modified version, or run one as a service for others, you must publish your changes under the same license.

The BingeBandit name, raccoon logo and the artwork in [`Brand/`](Brand) and `web/public/brand/` are not covered by the AGPL. Please don't use them for your own forks or products without permission.
