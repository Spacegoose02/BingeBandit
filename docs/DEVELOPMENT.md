# Developing BingeBandit

How to build, run, test and release BingeBandit, and the rules that keep the code base in one piece. Read [ARCHITECTURE.md](ARCHITECTURE.md) for how the parts fit together and [HANDOFF.md](../HANDOFF.md) for where the work stands.

## What's in the repository

| Path | What it is | Language |
| --- | --- | --- |
| `server/` | The server: one Go binary with the web app embedded | Go 1.26+ |
| `server/cmd/bingebandit` | `main`, which wires the packages together | |
| `server/cmd/fake*` | Fake services for development and tests (see below) | |
| `server/internal/*` | The server's packages; [ARCHITECTURE.md](ARCHITECTURE.md) has a table | |
| `server/plugins/*` | The bundled plugins, each its own binary with a `plugin.json` | |
| `server/pkg/bbplugin` | The Go kit for writing plugins ([PLUGINS.md](PLUGINS.md)) | |
| `web/` | The web app: React 18, TypeScript, Vite, react-router 6 | TypeScript |
| `apps/tv/` | Android TV, Google TV, Fire TV and Android phone app ([README](../apps/tv/README.md)) | react-native-tvos 0.83, React 19 |
| `apps/roku/` | Roku channel ([README](../apps/roku/README.md)) | BrightScript / SceneGraph |
| `unraid/` | The Unraid container template ([UNRAID.md](UNRAID.md)) | |
| `Brand/` | Logo and artwork (not covered by the AGPL) | |
| `docs/` | User guide, API, plugins, architecture, roadmap, this file | |

## Building

You need Go 1.26+, Node 22+ and FFmpeg (with ffprobe) on your PATH.

```sh
./build.sh            # Linux / macOS: dist/bingebandit and dist/plugins/*
.\build.ps1           # Windows: dist\bingebandit.exe and dist\plugins\*
```

Both build the web app into `server/internal/web/dist`, then the server with the web app embedded, then each bundled plugin into `dist/plugins/<id>/`, which the server reads by default. Run `dist/bingebandit` and open http://localhost:8484; the first visit asks for the admin account.

## Running while you work

```sh
# terminal 1: the server (re-run after Go changes)
cd server && go run ./cmd/bingebandit -data ../data

# terminal 2: the web app with hot reload, proxying /api to :8484
cd web && npm install && npm run dev
```

Open http://localhost:5173. The `data` folder holds the database, caches, logs and transcodes; delete it to start over.

### Server options

| Flag | Env var | Default | What it's for |
| --- | --- | --- | --- |
| `-addr` | `BB_ADDR` | `:8484` | Listen address |
| `-data` | `BB_DATA_DIR` | `%AppData%\BingeBandit` / `~/.config/BingeBandit` | Database, caches, artwork, logs, transcodes, plugin data |
| `-ffmpeg` | `BB_FFMPEG` | `ffmpeg` | ffmpeg binary |
| `-ffprobe` | `BB_FFPROBE` | `ffprobe` | ffprobe binary |
| `-plugins` | `BB_PLUGINS_DIR` | `plugins` next to the binary (`/usr/local/lib/bingebandit/plugins` in Docker) | Bundled plugins |
| `-update-repo` | `BB_UPDATE_REPO` | `Spacegoose02/BingeBandit` | GitHub repo the TV app's self-updater reads; empty turns it off |
| `-trakt-api` | `BB_TRAKT_API` | `https://api.trakt.tv` | Point at `fakeplex` for testing |
| `-tpdb-url` | `BB_TPDB_URL` | `https://theposterdb.com` | Point at a stand-in for testing |

Everything else (TMDB key, encoders, network, Live TV…) is a setting in the web app, stored in the database.

### Fake services

Most outside services have a fake under `server/cmd`, so you can try a feature without accounts or hardware. Run each with `go run ./cmd/<name>` from `server/`.

| Fake | Address | What it pretends to be | How to use it |
| --- | --- | --- | --- |
| `fakeiptv` | `:8585` | An IPTV provider: ffmpeg test channels (MPEG-2, an HLS one, and a flaky one that dies after 20 s with a backup feed), an M3U playlist and an XMLTV guide | Add `http://localhost:8585/playlist.m3u` and `/epg.xml` under Admin → Live TV → Sources |
| `fakedispatcharr` | `:9191` | Dispatcharr in front of fakeiptv | Admin → Live TV → Sources → Import from Dispatcharr, `admin` / `admin` or key `fake-dispatcharr-key` |
| `fakeoidc` | `:8686` | An OpenID Connect provider (alice, bob, carol) | Settings → Single sign-on: issuer `http://localhost:8686`, client `bingebandit`, secret `dev-secret` |
| `fakeplex` | `:32400`, `:8181`, `:8282` | Plex (token `fake-plex-token`), Tautulli (key `fake-tautulli-key`) and Trakt (`fake-trakt-client` / `fake-trakt-secret`) | Admin → Import; for Trakt start the server with `-trakt-api http://localhost:8282`. `-makedb path` writes a `tautulli.db` |

Hue bridges, Cinemask, ESPN, TMDB, TheTVDB, Wikidata, YouTube and the chart sites are faked inside the Go tests (`httptest` servers) rather than as commands.

## Checks

CI (`.github/workflows/ci.yml`) runs these on every push. Run the ones for what you changed before pushing.

| Part | Command | Where |
| --- | --- | --- |
| Web app | `npm ci && npm run build` (type checks with `tsc`, then builds) | `web/` |
| Go formatting | `gofmt -l .` must print nothing | `server/` |
| Go | `go vet ./... && go test ./...` | `server/` |
| TV app | `npm ci && npx tsc --noEmit && npx eslint App.tsx src` | `apps/tv/` |
| Roku | `npx brighterscript --project bsconfig.json`, then `./build.sh` | `apps/roku/` |

`tv-android.yml` also builds both Android flavours on pull requests that touch `apps/tv/android` or the TV app's packages.

A few Go tests generate small test videos with `ffmpeg` and skip themselves when it isn't on the PATH (CI doesn't install it), so run them locally when you touch transcoding, markers or Hue Sync.

## Rules of the code base

- **Migrations are append-only.** Schema changes are a new string at the end of `migrations` in `server/internal/store/schema.go`. `migrate_test.go` keeps the SHA-256 of every migration that has shipped (`shipped`) and fails if one changes, because a server that already ran a migration never runs it again: v0.22.0 moved an index between migrations and broke every upgrade from v0.21.0. Add the new hash to `shipped` when a release carries it. Many features need no migration at all: prefs (`users.prefs` JSON) and settings rows cover most new options.
- **All SQL lives in `store`.** Handlers in `api` validate, check access and shape responses.
- **Library access is in every item query.** User-facing queries include `store.visible`, bound to `@uid`; uid 0 is the server itself and sees everything. Answer 404, not 403, for something the user can't see. Kid mode (`store/kids.go`) is part of the same check.
- **Don't mix `?` and named (`@uid`) parameters** in one query.
- **`web/src/types.ts` and `apps/tv/src/types.ts` mirror the Go JSON tags.** Change them with the Go struct. When a field is new, older apps won't send it: make the server keep the stored value when it's missing.
- **Settings admins change at runtime** go in `store.SettingDefaults` and the Settings page, not flags. **Per-person choices** go in prefs (`store/prefs.go`), so they follow the profile.
- **Every new outgoing request** goes through a timeout, and anything that talks to a provider should cope with it being down: the server is meant to keep working without the internet (GUIDE section 10).
- **The TV app runs on old, slow boxes.** Lay screens out on the 1920×1080 canvas with `scaled()` (`src/theme.ts`), use `FlatList` for long lists, and remember Android only sends key-up events to JS.
- **Read transcode output with `stream.OpenShared`** on Windows (see ARCHITECTURE.md, Playback).

## Testing on real devices

Most features are tested against fakes, and HANDOFF.md says for each one what was never tried for real. When you do try something on a real device, write down the result there.

- **Android TV / phone:** `adb connect <ip>:5555`, a debug APK and Metro with `adb reverse tcp:8081 tcp:8081` ([apps/tv/README.md](../apps/tv/README.md#developing)). `adb screencap` can't capture HEVC video drawn on a hardware layer, so a black screenshot isn't a bug.
- **Roku:** sideload `build/BingeBandit-Roku.zip` in developer mode ([apps/roku/README.md](../apps/roku/README.md)), and watch `telnet <roku-ip> 8085`. The `brs-node` simulator runs the UI but can't play video.
- **Browser:** the web app works in any Chromium; Playwright is handy for passkeys (virtual authenticator) and phone widths.

## Releasing

Push a tag like `v0.35.0` on `main`. `.github/workflows/release.yml` then:

1. Builds the server for Windows (amd64), Linux and macOS (amd64 and arm64) with the bundled plugins, and zips the plugins for the plugin catalogue (`plugin-catalog.json`, with each zip's SHA-256).
2. Builds the Android APK (sideload flavour, signed with the `ANDROID_KEYSTORE*` secrets when they exist) and, with the release key, the Google Play bundle.
3. Builds the Roku zip with the manifest version from the tag.
4. Publishes the multi-arch Docker image to `ghcr.io/spacegoose02/bingebandit` (with yt-dlp and Deno for YouTube Live).

All files are attached to the GitHub Release. The TV app's self-updater and the plugin catalogue read the newest release, so they see the new version as soon as it's published.

## Writing documentation

- **README.md:** what BingeBandit is and how to start it. Keep it short.
- **docs/GUIDE.md:** for people running a server. Plain language, no code names, screenshots from `docs/screenshots` (made-up titles only).
- **docs/API.md, docs/PLUGINS.md:** for people writing scripts, apps and plugins. Update them with any endpoint, event or plugin API change.
- **docs/ARCHITECTURE.md:** how the server works. Update it when a package's job changes.
- **docs/ROADMAP.md:** every board card and whether it's done.
- **HANDOFF.md:** a dated section for each piece of work at the top of the log: what changed, the migration number if any, and what was and wasn't tested.
