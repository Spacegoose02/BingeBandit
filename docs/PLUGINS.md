# Plugins

A BingeBandit plugin is a separate program that speaks HTTP. It never runs inside the server, so it can be written in any language, crash without taking the server down, and be updated on its own.

There are two ways to run one:

| | Plugins folder ("managed") | Own service ("remote") |
|---|---|---|
| Where it runs | BingeBandit starts it, on the same machine | Anywhere it can reach BingeBandit, such as its own Docker container |
| Install | **Admin → Plugins → Catalogue → Install**, or put its folder in a plugins folder; then turn it on | Start it, then **Admin → Plugins → Add by address** |
| How it's paired | BingeBandit passes its key in the environment | BingeBandit sends a key to `POST /bingebandit/pair` |
| Restarts | BingeBandit restarts it if it exits (5 s, doubling up to 5 min) | Its own (Docker's restart policy) |

The plugins folders are `<data>/plugins` and the bundled folder (`-plugins` / `BB_PLUGINS_DIR`, by default `plugins` next to the server; `/usr/local/lib/bingebandit/plugins` in the Docker image). New folders are picked up within a minute. A plugin in `<data>/plugins` replaces a bundled one with the same id.

> **Trust.** A plugin's key can read every user's library data and change watchlists, and its pages open on BingeBandit's own address while you're signed in. Only install plugins you trust, as you would a Jellyfin or Plex plugin.

## Bundled plugins

| Plugin | What it does |
|---|---|
| **Cinemask Upgrades** (`cinemask-upgrades`) | Lists movies and episodes below the quality you want, judged from the files (resolution, and optionally old codecs, low bitrate, 4K without HDR), on **Admin → Plugins → Upgrades**. For each, [Cinemask](https://github.com/Spacegoose02/cinemask) can search its indexers and grab a better copy, show the releases it found to pick one, move the title to a profile with a higher cutoff, or start monitoring it. **Search all** goes through the list one title at a time. Needs Cinemask's address and an API key with full access from a Cinemask manager or admin. |
| **Hue Sync** (`hue-sync`) | Philips Hue lights follow the colours on screen. It reads the playing file's picture a little ahead with ffmpeg (4 frames a second, 48×27 pixels) and keeps in step through the `player.*` events. Each light follows a zone (whole screen, left, centre, right, top, bottom). Set up from **Admin → Plugins → Hue Sync setup** (pair the bridge, assign lights); filters for people and devices, brightness, delay and pause/stop behaviour are its settings. Movies and shows only, not Live TV. |
| **Watchlist RSS** (`watchlist-rss`) | Each person's watchlist as an RSS feed (`feed.xml?token=<API key>`), and RSS feeds people subscribe to whose movies and shows go on their watchlist when they're in the library (matched by TMDB id, or by title and year). Optionally takes finished movies off the watchlist. People open it from **Account → Plugins**. |

## Catalogues

**Admin → Plugins → Catalogue** lists plugins that can be installed with one click. BingeBandit's own catalogue is always read: each release publishes `plugin-catalog.json` with the bundled plugins built for every platform, so a server without them (or with older copies) can install or update them. Admins can add other catalogues (one address per line under **Catalogues**; setting `plugin_catalogs`). When two catalogues list the same id, the first one wins.

Installing downloads the zip for the server's platform, checks its SHA-256, unpacks it into `<data>/plugins/<id>` and leaves the plugin off. Updating replaces that folder (the plugin is stopped while it's swapped, and started again if it was on). Plugins installed this way can be uninstalled from their row; bundled ones can only be turned off. A catalogue is cached for 10 minutes (the refresh button reads it again).

A catalogue is JSON:

```json
{
  "name": "My plugins",
  "plugins": [
    {
      "id": "my-plugin",
      "name": "My Plugin",
      "version": "1.2.0",
      "description": "What it does.",
      "author": "you",
      "homepage": "https://…",
      "downloads": {
        "linux-amd64": { "url": "https://…/my-plugin-1.2.0-linux-amd64.zip", "sha256": "…" },
        "windows-amd64": { "url": "https://…", "sha256": "…" }
      }
    }
  ]
}
```

Download keys are Go's `<os>-<arch>` (`linux-amd64`, `linux-arm64`, `windows-amd64`, `darwin-amd64`, `darwin-arm64`; the Docker image is `linux-amd64` or `linux-arm64`). `sha256` is required. Each zip holds the plugin's folder (or its files at the top level) with a `plugin.json` whose `id` matches the entry; links and paths outside the folder are refused. An update is offered when the catalogue's `version` is newer than the `version` in the installed `plugin.json`.

## The protocol

Everything is JSON over HTTP. One key, made by BingeBandit (`bbp_` + 64 hex characters), authenticates both directions as `Authorization: Bearer <key>`.

### What the plugin serves

| Route | Auth | |
|---|---|---|
| `GET /bingebandit/manifest` | none | The manifest (below). Also the health check, every minute. |
| `POST /bingebandit/pair` | see below | Remote plugins only. Body `{"key", "serverUrl", "id"}`. Store both and answer 204. If already paired, accept only when the request carries the current key (re-pairing rotates it); otherwise answer **409**. |
| `POST /bingebandit/unpair` | key | Forget the pairing (sent when an admin removes the plugin). 204. |
| `POST /bingebandit/events` | key | An event (below). Answer quickly (204) and do the work in the background; delivery times out after 10 s and isn't retried. |
| anything else | key | The plugin's own pages and endpoints, reached by users through BingeBandit's proxy. |

Plugins must refuse requests without the key: only BingeBandit may call them, and the user headers below are only trustworthy because of it.

### The manifest

```json
{
  "id": "hue-sync",
  "name": "Hue Sync",
  "version": "1.0.0",
  "description": "Lights follow the colours of what's playing.",
  "author": "you",
  "homepage": "https://…",
  "events": ["playback.start", "playback.stop"],
  "settings": [
    { "key": "bridge", "label": "Bridge address", "type": "text", "help": "Find it in the Hue app." },
    { "key": "token", "label": "Bridge user", "type": "password" },
    { "key": "brightness", "label": "Brightness", "type": "number", "default": "80" },
    { "key": "mode", "label": "Mode", "type": "select", "options": ["poster", "backdrop"], "default": "backdrop" },
    { "key": "dim", "label": "Dim when paused", "type": "bool", "default": "true" }
  ],
  "pages": [{ "title": "Hue Sync", "path": "/", "admin": true }]
}
```

- `id`: lowercase letters, digits and dashes, up to 48 characters. It's the plugin's address (`/api/plugins/{id}/`).
- `events`: event names, or `"*"` for all. `plugin.settings` is always sent.
- `settings` become a form in **Admin → Plugins**. Values are strings. `password` values are never shown again in the web app; leaving one empty keeps it.
- `pages` are links in **Admin → Plugins** and, unless `admin`, **Account → Plugins**. A page marked `admin` is only listed for admins; the plugin must still check `X-BingeBandit-Admin`.
- A managed plugin's folder holds a `plugin.json` with at least `id`, `name` and `command`, e.g. `"command": ["watchlist-rss"]` or `["python3", "main.py"]`. The program runs in its folder; a first element that names a file in the folder (or that file plus `.exe` on Windows) is run from there. What the plugin serves at `/bingebandit/manifest` wins over `plugin.json`.

### Managed plugins' environment

| Variable | |
|---|---|
| `BINGEBANDIT_PLUGIN_ADDR` | Listen here (`127.0.0.1:<free port>`). |
| `BINGEBANDIT_URL` | The server, on loopback. |
| `BINGEBANDIT_PLUGIN_KEY` | The key. |
| `BINGEBANDIT_PLUGIN_ID` | The id. |
| `BINGEBANDIT_PLUGIN_DATA` | A folder for the plugin's files (`<data>/plugin-data/<id>`). |
| `BINGEBANDIT_FFMPEG`, `BINGEBANDIT_FFPROBE` | The server's ffmpeg and ffprobe. |

Output on stdout and stderr goes to the server log, prefixed `plugin <id>:`. On shutdown or when turned off, the process gets an interrupt (killed on Windows) and is killed 5 s later.

### Events

```json
{ "event": "playback.finished", "timestamp": "2026-10-24T20:15:00Z", "server": "BingeBandit", "data": { … } }
```

The events and their `data` are the same as [webhooks](API.md#webhooks): `playback.*`, `player.*` and `recording.*`. Playback and player events include `userId` and `itemId`; player events also have `sessionId`, `fileId`, `position` and `paused`. Plugins can also subscribe to `player.progress`, sent on every player ping (about every 10 s, and at once on pause, resume and seek in the web player), which webhooks don't offer. `plugin.settings` carries the plugin's settings (with defaults) after an admin saves them.

### Pages and endpoints for users

`/api/plugins/{id}/{path}` on BingeBandit is proxied to `{path}` on the plugin, for any signed-in user (cookie, `Authorization`, or `?token=` on GET, so feed readers can use an API key). BingeBandit removes the user's cookie, `Authorization` header and `token` parameter, and adds:

| Header | |
|---|---|
| `Authorization` | `Bearer <plugin key>` |
| `X-BingeBandit-User-Id`, `X-BingeBandit-User` | Who's asking. |
| `X-BingeBandit-Admin` | `true` or `false`. |
| `X-BingeBandit-Prefix` | `/api/plugins/{id}`, for building links. |
| `X-BingeBandit-External-URL` | BingeBandit's public address, for absolute links (feeds). |

Paths under `/bingebandit/` can't be reached through the proxy, and `Set-Cookie` is dropped from responses. Pages that call back with `fetch` and the user's cookie must send `X-Requested-With: BingeBandit` on anything but GET, like the web app does.

### The plugin API

`/api/plugin/v1` on BingeBandit, with `Authorization: Bearer <plugin key>`. It answers 401 once the plugin is turned off or removed. Without a user in the path, calls see every library, as the server does.

| Method | Path | |
|---|---|---|
| GET | `/info` | `{id, server, version, settings}` |
| GET | `/settings` | Settings with defaults filled in. |
| POST | `/log` | `{level: "info"\|"warn"\|"error", message}` into the server log. |
| GET | `/users` | `[{id, username, isAdmin}]` |
| GET | `/libraries` | Libraries, with their folders. |
| GET | `/libraries/{id}/items?offset=&limit=&sort=` | Like the user API. |
| GET | `/items/{id}` | Item, files (with paths and streams) or children. |
| GET | `/lookup?tmdb=&kind=` or `?title=&year=&kind=` | Movies and shows by TMDB id, or by title (case-insensitive) and year (±1). `kind` is `movie` or `show`. |
| GET | `/search?q=` | Title search. |
| GET | `/files?offset=&limit=` | Every movie and episode file with its item (title, year, TMDB id; for episodes the show, its TMDB id, season and episode) and quality (resolution, codec, HDR, bitrate, size), by file id. `{files, total}`; up to 1000 a page. |
| GET | `/files/{id}/direct` | The file itself, with range requests (for reading the picture or sound). |
| GET | `/images/tmdb/{file}?w=`, `/images/local/{file}` | Artwork. Items' image URLs start with `/api/images/`; swap that for `/api/plugin/v1/images/`. |
| GET | `/users/{uid}/items/{id}` | An item with that user's watch state and library access. |
| GET | `/users/{uid}/lookup?…` | `/lookup` as that user (includes `onWatchlist`). |
| GET | `/users/{uid}/watchlist` | Their watchlist. |
| POST / DELETE | `/users/{uid}/watchlist/{id}` | Add or remove. |
| GET | `/users/{uid}/history` | Their play history. |

The API is versioned: v1 only grows. Breaking changes would come as `/api/plugin/v2`.

## Writing one in Go

`server/pkg/bbplugin` does the protocol for you; `server/plugins/watchlist-rss` is a complete example.

```go
p := &bbplugin.Plugin{
	Manifest: bbplugin.Manifest{ID: "hello", Name: "Hello", Version: "1.0.0", Events: []string{"playback.finished"}},
	OnEvent: func(ctx context.Context, e bbplugin.Event) { log.Printf("%s: %s", e.Event, e.Data) },
	Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := bbplugin.UserOf(r)
		fmt.Fprintf(w, "Hello, %s", u.Username)
	}),
}
log.Fatal(p.Run())
```

`p.Call(ctx, "GET", "/users", nil, &out)` calls the plugin API. `Run` reads the environment above; run as its own service, it listens on `BINGEBANDIT_PLUGIN_ADDR` (default `:8490`) and keeps its pairing in `BINGEBANDIT_PLUGIN_DATA` (default `./data`).

## Writing one in another language

Serve the four `/bingebandit/` routes, check the key on everything except the manifest, and call the plugin API with the same key. For a remote plugin, persist the key and server URL from `/pair` so it survives restarts.
