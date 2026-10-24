# BingeBandit HTTP API

The web app uses this same JSON API, so anything it does, a script or app can do too.
Everything lives under `/api` on your server, e.g. `http://192.168.1.10:8484/api/health`.

The API is still pre-1.0. Endpoints listed here are expected to stay; fields may be added at any time, so ignore ones you don't know.

## Authenticating

Create an **API key** under *Account → API keys* (keys start with `bb_`). A key acts as you, with your library access and admin rights if you have them. Delete it there to revoke it.

Send it either way:

```
Authorization: Bearer bb_xxxxxxxxxxxxxxxx
```

or, for players that can't set headers, as `?token=bb_xxxx` on GET requests.

```bash
curl -H "Authorization: Bearer $BB_KEY" http://localhost:8484/api/me
```

Apps that sign people in themselves can use `POST /api/auth/login` (below) or the TV code flow, and send the returned session token the same way.

Rules:

- Requests and responses are JSON (`Content-Type: application/json`). Times are Unix seconds; durations and positions are seconds.
- Errors look like `{"error": "message"}` with a 4xx/5xx status. `401` means the token is missing or wrong, `403` means you aren't allowed.
- Requests authenticated by the browser **cookie** must send `X-Requested-With: BingeBandit` on anything but GET. Bearer tokens and API keys don't need it.
- Failed password sign-ins are limited to 10 per IP per 15 minutes.

## Signing in (apps)

| Method | Path | Notes |
|---|---|---|
| POST | `/auth/login` | `{username, password, device, code?}` → `{token, user}`. If two-step sign-in is on and no `code` was sent: `401 {"error", "needsCode": true}`; ask for the 6-digit code (or a recovery code) and send it again with `code`. |
| POST | `/auth/logout` | Ends the session used for this request. |
| POST | `/link/start` | No auth. `{device}` → `{code, pollToken, expiresIn, linkUrl}`. Show `code` on screen. |
| GET | `/link/poll?token=<pollToken>` | No auth. `202` while waiting, `200 {token, user}` once someone approves, `410` when expired. Poll every 2–5 s. |
| POST | `/link/approve` | `{code, userId?}`: what `/link` in the web app calls. Admins may pass another user's `userId`. |
| GET | `/auth/oidc` | No auth. Enabled single sign-on providers: `[{id, name}]`. Browser-only flow: send the user to `/api/auth/oidc/{id}/login?next=/`. |

**TV code flow:** call `/link/start`, show the code and "go to `linkUrl`" (a QR code of `linkUrl` works well), poll `/link/poll` until you get a token, then store the token.

## You

| Method | Path | Notes |
|---|---|---|
| GET | `/me` | The signed-in user. |
| GET / PUT | `/me/prefs` | Playback preferences (audio/subtitle language, skip intro, …). |
| PUT | `/me/password` | `{current, new}`. |
| GET | `/me/history` | Your recent plays. |
| GET / DELETE | `/me/sessions`, `/me/sessions/{id}` | Signed-in devices; sign one out. |
| GET / POST / DELETE | `/me/keys`, `/me/keys/{id}` | API keys. POST `{name}` returns `{key, info}`; the key is only shown then. |
| GET / DELETE | `/me/identities`, `/me/identities/{id}` | Accounts linked for single sign-on. |
| PUT | `/me/share-activity` | `{share: true}` lets others see what you're watching. |
| GET | `/me/export/history.csv`, `/me/export/letterboxd.csv` | Your watch history as CSV (TMDB ids), or finished movies in Letterboxd's import format. |
| GET | `/me/trakt` | `{configured, account, pendingNew, pendingAll}`. |
| POST | `/me/trakt/connect`, `/me/trakt/poll` | Connect Trakt: show `userCode` and `verificationUrl`, then poll (202 while waiting, 200 when connected). |
| POST | `/me/trakt/import`, `/me/trakt/push` | Import your Trakt history; send plays to Trakt (`{all: true}` includes plays from before connecting). |
| PUT / DELETE | `/me/trakt` | `{autoSync}`; disconnect. |
| GET | `/activity/friends` | What people who share are watching now. |

## Browsing

| Method | Path | Notes |
|---|---|---|
| GET | `/home` | Hero, Continue Watching and the Home rows. |
| GET | `/libraries` | Libraries you can see. |
| GET | `/libraries/{id}/items` | Query: `sort`, `offset`, `limit` (max 500) → `{library, items, total}`. |
| GET | `/libraries/{id}/collections`, `/collections/{id}` | Collections. |
| GET | `/items/{id}` | A movie, show, season or episode, with files, streams, cast and children. |
| GET | `/items/{id}/next` | The next episode to play. |
| POST | `/items/{id}/watched`, `/items/{id}/unwatched` | Mark watched or unwatched. |
| GET | `/search?q=` | Search titles and people. |
| GET / POST / DELETE | `/watchlist`, `/watchlist/{id}` | Your watchlist. |
| GET / POST / PUT / DELETE | `/playlists`, `/playlists/{id}`, `/playlists/{id}/items`, `/playlists/{id}/order` | Playlists. |
| GET | `/profiles`, `/profiles/{username}` | Public profiles and stats. |

Image URLs in responses are already paths on this server (e.g. `/api/images/tmdb/abc.jpg`); add `?w=w342` (TMDB sizes) to get a smaller one.

## Playing

| Method | Path | Notes |
|---|---|---|
| GET | `/files/{id}/direct` | The original file, with range requests. Use when the client can play the codec. |
| POST | `/files/{id}/transcode` | `{start, maxHeight, bitrateKbps, audioStream, burnSubtitle, replace}` (all optional; `-1` = default track / none) → `{session, playlist}`: open `playlist` as HLS. Pass the old `session.id` as `replace` when seeking. |
| GET | `/transcode/{sid}/{name}` | HLS playlist and segments. |
| DELETE | `/transcode/{sid}` | Stop a transcode when the viewer leaves. |
| GET | `/files/{id}/subtitles/{stream}` | A subtitle track as WebVTT. |
| POST | `/progress` | `{playSessionId, itemId, fileId, position, duration, paused, stopped, mode, client, played}`. Send every ~10 s while playing and once with `stopped: true`. `played` is the total seconds actually played in this play session (the server caps it at wall-clock time). This drives resume points, watched state, Now Playing, history and webhooks. |

## Live TV and DVR

| Method | Path | Notes |
|---|---|---|
| GET | `/live/channels` | Channels with what's on now and next. |
| POST / DELETE | `/live/channels/{id}/favorite` | Favorite a channel. |
| GET | `/live/guide?from=&to=` | Channels, programs and your recordings for a window (default: from 30 min ago, 6 h, at most 24 h). |
| POST | `/live/channels/{id}/watch` | Start watching → `{viewerId, playlist}` (HLS). |
| GET | `/live/channels/{id}/stream.ts` | Raw MPEG-TS for IPTV players. |
| DELETE | `/live/viewers/{viewer}` | Stop watching. |
| GET | `/live/playlist.m3u`, `/live/epg.xml` | M3U and XMLTV for other IPTV apps. Use `?token=<API key>`. |
| GET | `/dvr` | Upcoming, series rules and recorded. |
| POST | `/dvr/recordings` | `{channelId, programId, series}`. |
| DELETE | `/dvr/recordings/{id}`, `/dvr/rules/{id}` | Cancel or delete. |

## Admin

Admin-only endpoints are under `/api/admin/…`: libraries and scans, item editing and matching, users and library access, settings, Live TV sources and channels, tasks, logs, statistics, webhooks and single sign-on. They change often, so read the web app's source (`web/src/pages/admin`) to see how they're used.

## Webhooks

*Settings → Webhooks* sends a POST to your URL when something happens:

```json
{
  "event": "playback.start",
  "timestamp": "2026-10-23T20:15:00Z",
  "server": "BingeBandit",
  "data": { "user": "alice", "itemId": 42, "kind": "episode", "title": "…", "subtitle": "S01E02 · …", "client": "Web", "mode": "direct", "position": 31, "duration": 1440, "local": true }
}
```

| Event | When |
|---|---|
| `playback.start` | A play has lasted 30 s (the same rule as watch history, so accidental clicks don't count). |
| `playback.stop` | A play that started stops. |
| `playback.finished` | Something is marked watched by playing it. |
| `recording.started` / `recording.completed` / `recording.failed` | DVR recordings. `data` is the recording, without its file path. |
| `test` | The *Send test* button. |

Playback events also carry `tmdbId`, and for episodes `showTmdbId`, `season`, `episode` and `episodeTitle`.

A webhook whose format is **Plex-style** instead sends what Plex sends: `multipart/form-data` with a `payload` field holding `{event: "media.play" | "media.stop" | "media.scrobble", Account: {title: <username>}, Player, Server, Metadata}`. Only playback events are sent in this format. Use it for apps that accept Plex webhooks, such as Yamtrack.

The `X-BingeBandit-Event` header names the event. If the webhook has a secret, `X-BingeBandit-Signature: sha256=<hex>` is the HMAC-SHA256 of the raw body with that secret. Compare it in constant time before trusting the request. Deliveries time out after 10 s and aren't retried.
