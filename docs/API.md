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

| Method | Path | Notes |
|---|---|---|
| GET | `/apps/android` | The newest Android TV APK on the server's update repository (GitHub releases, checked at most hourly): `{version, versionCode, url, size, publishedAt, notes}`, or `204` when there's none or updates are off (`-update-repo ""`). Install it when `versionCode` is greater than the app's own. |

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
| GET | `/notifications` | `{notifications, unread}`: your newest 50 (`kind` is `episodes` or `recording`; `link` is a web app path). Kept for 60 days. |
| POST | `/notifications/read` | `{ids: [...]}` marks those read; `{}` marks all read. |
| DELETE | `/notifications` | Clears yours. |
| GET | `/activity/friends` | What people who share are watching now. |
| GET / PUT / DELETE | `/me/passcode` | Settings passcode: `{set, unlocked, unlockedUntil}`; PUT `{passcode}` (4–8 digits); DELETE removes it. |
| POST | `/me/passcode/unlock`, `/me/passcode/lock` | `{passcode}` or `{password}` unlocks this session's settings for 15 minutes of use. While locked, `/admin/*`, account changes under `/me` and profile management answer **423**. |

### Profiles

A profile is its own user under an account (`ownerId` and `profileName` on the user), with separate history, watchlist and preferences. Profiles have no password; a device reaches one by switching.

| Method | Path | Notes |
|---|---|---|
| GET | `/household` | The account and its profiles: `[{id, name, isOwner, hasPin, current, avatar, auto, allLibraries, libraryIds}]`. `avatar` is the picture's URL; `auto` marks the member the account opens as without asking who's watching (switch to it unless it has a PIN). |
| POST | `/household` | Account only. `{name}` adds a profile (up to 10). |
| PUT / DELETE | `/household/{id}` | Account only. `{name, allLibraries, libraryIds}` (never more than the account sees); delete removes the profile and its history. |
| PUT | `/household/{id}/pin` | Account only. `{pin}` sets a profile's PIN; `""` removes it. The account's own PIN is its settings passcode. |
| PUT / DELETE | `/household/{id}/avatar` | The account for anyone in it, or a profile for itself. PUT a JPEG, PNG or WebP of up to 5 MB as the multipart `file` field → `{avatar}`. |
| PUT | `/household/autoselect` | Account only. `{memberId}` opens every device as that member without asking; `0` asks who's watching. |
| POST | `/household/{id}/switch` | `{pin}` when the member has one. Ends this session and returns `{token, user}` for the member (and sets the cookie for the web UI). A wrong PIN answers 403 `{needsPin: true}`. |

## Browsing

| Method | Path | Notes |
|---|---|---|
| GET | `/home` | Hero, Continue Watching and the Home rows. |
| GET | `/libraries` | Libraries you can see. |
| GET | `/libraries/{id}/items` | Query: `sort`, `offset`, `limit` (max 500) → `{library, items, total}`. |
| GET | `/libraries/{id}/collections`, `/collections/{id}` | Collections. A smart one carries its rules (`labelRule`, `genreRule`, `studioRule`, `yearFrom`, `yearTo`); everything matching all the set rules is a member, plus anything added by hand. |
| POST | `/admin/libraries/{id}/kometa` | Admin. Body: a Kometa collection file (YAML). `?dryRun=1` only returns the plan; `?recommended=1` shows collections on Recommended unless the file sets `visible_library: false`. Returns `{collections: [{id, name, lists, sort, sortTitle, exists, visible, skipped}]}`; posters and list syncs continue in the background. |
| PUT, POST | `/admin/collections/{id}/artwork` | Admin. `PUT {type: "poster"\|"backdrop", url}` downloads an image (or a ThePosterDB poster page; an empty url resets it); `POST ?type=` takes a multipart `file`. |
| GET | `/items/{id}` | A movie, show, season or episode, with files, streams, cast and children. Movies and shows include `externalRatings` (`imdb`, `imdbVotes`, `imdbId`, `rottenTomatoes`, `rottenTomatoesAudience`, `metacritic`) once a ratings provider has scores for them. |
| GET | `/items/{id}/recommend` | People this movie or show can be recommended to (opted in, and able to see it): `[{id, name}]`. |
| POST | `/items/{id}/recommend` | `{userIds, note}` → `{sent}`. Recommending again renews it. |
| DELETE | `/recommendations/{itemId}` | Hide a title from your "Recommended for You" row. |
| GET/PUT | `/me/recommendations` | `{accept}`: whether others may recommend titles to you (off by default). `/home` includes the row (`id: "recommended"`) and `recommendedBy` (item id → `{from, notes}`). |
| GET | `/items/{id}/next` | The next episode to play. |
| POST | `/items/{id}/watched`, `/items/{id}/unwatched` | Mark watched or unwatched. |
| GET | `/search?q=` | Search titles and people. |
| GET | `/discover` | Discover rows: `[{id, name, videos: [{id, title, description, publishedAt}]}]`, newest first. `id` is the YouTube video id (thumbnail `https://i.ytimg.com/vi/{id}/mqdefault.jpg`). |
| GET / POST / DELETE | `/watchlist`, `/watchlist/{id}` | Your watchlist. |
| GET / POST / PUT / DELETE | `/playlists`, `/playlists/{id}`, `/playlists/{id}/items`, `/playlists/{id}/order` | Playlists. |
| GET | `/profiles`, `/profiles/{username}` | Public profiles and stats. |

Image URLs in responses are already paths on this server (e.g. `/api/images/tmdb/abc.jpg`); add `?w=w342` (TMDB sizes) to get a smaller one.

## Playing

| Method | Path | Notes |
|---|---|---|
| GET | `/files/{id}/direct` | The original file, with range requests. Use when the client can play the codec. |
| POST | `/files/{id}/transcode` | `{start, maxHeight, bitrateKbps, audioStream, burnSubtitle, replace, audioCodecs, maxAudioChannels, adaptive}` (all optional; `-1` = default track / none) → `{session, playlist}`: open `playlist` as HLS. Pass the old `session.id` as `replace` when seeking. `audioCodecs` lists what the client plays inside HLS (`aac`, `ac3`, `eac3`, `mp3`) and `maxAudioChannels` how many channels it outputs: a matching source track is copied untouched, other surround tracks become AAC 5.1 when `maxAudioChannels` ≥ 6, and everything else (or a client that sends neither) gets AAC stereo. `session.audioCodec`, `audioChannels` and `audioCopy` say what was chosen. With `adaptive: true`, when the admin has turned on adaptive streaming and the video has to be converted anyway, `playlist` is a master playlist with up to three qualities (`session.variants`, best first). |
| GET | `/transcode/{sid}/{name}` | HLS playlist and segments. |
| DELETE | `/transcode/{sid}` | Stop a transcode when the viewer leaves. |
| GET | `/files/{id}/subtitles/{stream}` | A subtitle track as WebVTT. |
| POST | `/progress` | `{playSessionId, itemId, fileId, position, duration, paused, stopped, mode, client, played}`. Send every ~10 s while playing and once with `stopped: true`. `played` is the total seconds actually played in this play session (the server caps it at wall-clock time). Optional `lights: false` keeps Hue Sync and other light plugins off this player (left out means on). This drives resume points, watched state, Now Playing, history and webhooks. |

## Live TV and DVR

An admin can turn Live TV off for a user (`PUT /api/admin/users/{id}/live-tv {on}`); `GET /me` then has `liveTV: false` and everything in this section answers **403**.

| Method | Path | Notes |
|---|---|---|
| GET | `/live/channels` | Channels with what's on now and next. |
| POST / DELETE | `/live/channels/{id}/favorite` | Favorite a channel. |
| GET | `/live/guide?from=&to=` | Channels, programs and your recordings for a window (default: from 30 min ago, 6 h, at most 24 h). |
| POST | `/live/channels/{id}/watch` | Start watching → `{viewerId, playlist, bufferSecs, stallSecs}` (HLS; `bufferSecs` is how far behind live to play, `stallSecs` how long buffering lasts before reporting it). Answers once the first segments exist (up to ~25 s), or 503 with the reason. |
| GET | `/live/channels/{id}/stream.ts` | Raw MPEG-TS for IPTV players. |
| DELETE | `/live/viewers/{viewer}` | Stop watching. |
| POST | `/live/viewers/{viewer}/ping` | Keep a paused viewer counted (every 10 s). 410 once the viewer is gone. |
| POST | `/live/viewers/{viewer}/buffering` | `{seconds}`: the player has been buffering this long. Past `stallSecs` (from `/watch`) the server may move the channel to its next stream → `{switched, reason}`. |
| GET | `/live/playlist.m3u`, `/live/epg.xml` | M3U and XMLTV for other IPTV apps. Use `?token=<API key>`. |
| GET | `/items/{id}/preroll` | What to play before a movie, from the viewer's settings: `{trailers: [{itemId, title, year, youtube}], intro?: {url, name}}`. Empty for anything but a movie. |
| GET | `/preroll/intro/{n}` | An intro video (range requests work). |
| GET | `/dvr` | Upcoming, series rules and recorded. Upcoming recordings carry `conflict` when a provider's connection limit may stop them. |
| POST | `/dvr/recordings` | `{channelId, programId, series, keepLast}` (`keepLast`: series only, keep the newest N recordings, 0 = all). |
| PATCH | `/dvr/rules/{id}` | `{keepLast}`; older recordings and their files are deleted right away. |
| DELETE | `/dvr/recordings/{id}`, `/dvr/rules/{id}` | Cancel or delete. |

### Watch Together

| | | |
|---|---|---|
| POST | `/together` | `{itemId, position, paused}` starts a room → `{id, host, state, members, now}`. |
| GET | `/together/{id}` | The room (404 once it has ended, or when you can't see what it plays). |
| GET | `/together/{id}/events` | Server-sent events: the room as above after every change and join or leave. |
| POST | `/together/{id}/state` | `{paused, position, itemId?}` play, pause, seek or move everyone to another movie or episode. |
| GET / POST | `/together/{id}/people`, `/together/{id}/invite` | Who can be invited (people who accept recommendations and can see it); `{userIds}` invites them. |
| GET | `/together/invites` | Your open invitations. `DELETE /together/{id}/invite` declines one. |

`state.position` is seconds at `state.at` (server ms); while playing, add the time since, using `now` to correct for clock differences.

### Sports

Scores and schedules come from ESPN's public (unofficial) site API, cached on the server.

| | | |
|---|---|---|
| GET | `/sports` | `{leagues, teams, games, error?}`: the leagues, the teams you follow, and their games from two days ago to a week ahead (live first, then upcoming, then results). Each game has `mine` (your team ids in it) and, with Live TV, `channels` whose guide shows it. |
| PUT | `/sports/teams` | `[{league, id}]` replaces your teams (at most 40); names, logos and colours come from ESPN. |
| GET | `/sports/{league}/teams` | Every team in a league: `nfl`, `college-football`, `nba`, `mens-college-basketball`, `womens-college-basketball`, `mlb`, `nhl`, `mls`. |
| GET | `/sports/{league}/scores?date=YYYYMMDD` | The league's games on an ESPN (US Eastern) day, today by default. |

Game alerts are notifications of kind `sports`, set by the `gameAlertMinutes` (0 for none) and `notifyGameScores` preferences.

### Passkeys

| | | |
|---|---|---|
| POST | `/auth/passkey/begin` | No sign-in needed. → `{flow, options}` (WebAuthn request options, base64url). |
| POST | `/auth/passkey/finish` | `{flow, credential, device}` with the browser's credential as JSON → `{token, user}` like `/auth/login` (no 2FA code asked). |
| GET | `/me/passkeys` | Your passkeys: `[{id, name, createdAt, lastUsedAt}]`. |
| POST | `/me/passkeys/begin` | → `{flow, options}` (WebAuthn creation options). |
| POST | `/me/passkeys` | `{flow, credential, name}` adds the passkey. |
| PUT / DELETE | `/me/passkeys/{id}` | Rename (`{name}`) or remove. |

Passkeys belong to the address they were added on (the page's host), so add them on the address people sign in with, over https://.

## Admin

Admin-only endpoints are under `/api/admin/…`: libraries and scans, item editing and matching, users and library access, settings, Live TV sources and channels, tasks, logs, statistics, webhooks and single sign-on. They change often, so read the web app's source (`web/src/pages/admin`) to see how they're used.

## Plugins

| Method | Path | Notes |
|---|---|---|
| GET | `/plugins` | Running plugins and the pages of theirs you can open: `[{id, name, pages: [{title, path}]}]` (`pages` is empty when none are for you). Apps use it to tell whether, say, `hue-sync` runs. |
| any | `/plugins/{id}/{path}` | Proxied to the plugin, with you as the user. |

Admins manage plugins under `/api/admin/plugins`: `GET /admin/plugins/catalog` (`?refresh=1` skips the cache) lists catalogue plugins with `installed`, `current` and `update`; `POST /admin/plugins/catalog/install` `{catalog, id}` installs or updates one; `POST /admin/plugins/{id}/uninstall` removes one installed that way. Plugins themselves call `/api/plugin/v1` with their key; see [PLUGINS.md](PLUGINS.md).

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
| `player.play` / `player.pause` / `player.resume` / `player.stop` | The player itself, at once: opened, paused, resumed, closed (`reason` is `stopped`, or `timeout` when it stopped pinging for 45 s). For lights and automations. Extra fields: `sessionId`, `fileId`, `paused`, `lights` (false when Hue Sync is switched off on that device). |
| `recording.started` / `recording.completed` / `recording.failed` | DVR recordings. `data` is the recording, without its file path. |
| `sports.game.start` / `sports.score` / `sports.game.end` | A game of a team someone follows on the Sports page started, someone scored, or it ended. `data` has the `game` (from ESPN), its `sport`, the `scorer` team id, and the `fans` who follow each team. |
| `test` | The *Send test* button. |

Playback events also carry `userId`, `tmdbId`, and for episodes `showTmdbId`, `season`, `episode` and `episodeTitle`.

A webhook whose format is **Plex-style** instead sends what Plex sends: `multipart/form-data` with a `payload` field holding `{event: "media.play" | "media.stop" | "media.scrobble", Account: {title: <username>}, Player, Server, Metadata}`. Only playback events are sent in this format. Use it for apps that accept Plex webhooks, such as Yamtrack.

The `X-BingeBandit-Event` header names the event. If the webhook has a secret, `X-BingeBandit-Signature: sha256=<hex>` is the HMAC-SHA256 of the raw body with that secret. Compare it in constant time before trusting the request. Deliveries time out after 10 s and aren't retried.
