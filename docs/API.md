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
| GET / PUT | `/me/prefs` | Playback preferences (audio/subtitle language, skip intro, …), `hiddenTabs`: the app menu tabs turned off (`sports`, `live`, `more`), `pinnedTabs`: tabs that stay under More until pinned (`discover`), `notifyTogether` (others may invite you to watch together), `liveFollows` (Live TV show titles to be alerted about `liveAlertMinutes` before they air; 0 turns the alerts off) and `quietWhilePlaying` (no pop-ups over a movie or show). |
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
| GET | `/notifications` | `{notifications, unread}`: your newest 50 (`kind` is `episodes`, `recording`, `sports`, `live`, `request` or `server`; `link` is a web app path). Each also goes to your push devices. Kept for 60 days. |
| POST | `/notifications/read` | `{ids: [...]}` marks those read; `{}` marks all read. |
| DELETE | `/notifications` | Clears yours. |
| GET | `/push` | `{publicKey, devices, kinds}`: the VAPID key to subscribe with (`applicationServerKey`), your devices (`{id, endpoint, device, kindsOff, createdAt, lastUsed}`) and the kinds a device can turn off (`request`, `sports`, `together`, `live`, `episodes`, `recording`, `server`). |
| POST | `/push/devices` | `{endpoint, keys: {p256dh, auth}, device, kindsOff}` (a `PushSubscription.toJSON()` plus a name) turns push on for a browser; the endpoint must be https on a known push service (Google, Mozilla, Apple, Microsoft). The same endpoint again updates it and moves it to you. At most 30 per person. |
| PUT | `/push/devices/{id}` | `{kindsOff}` |
| DELETE | `/push/devices/{id}` | Forgets one of your devices. |
| POST | `/push/test` | Sends a test to each of your devices: `{results: {"<id>": "sent" or the error}}`. A device whose push service says it's gone is removed. |
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
| GET | `/libraries` | Libraries you can see. Music, audiobook and photo libraries (`kind` `music`, `audiobooks`, `photos`) are only listed for apps that send `X-BB-Media` with the kinds they can show, comma-separated (the web app sends `music,audiobooks,photos`); without it they're left out here, in `/home` and in `/search`, and their pages answer 404. |
| GET | `/libraries/{id}/items` | Query: `sort`, `offset`, `limit` (max 500) → `{library, items, total}`. Filters, any mix: `genre`, `yearFrom`, `yearTo`, `unwatched=1` (movies not watched, shows with an episode left), `minRating` (out of 10). Filtered pages send no collections and fold none away. |
| GET | `/libraries/{id}/items?view=artists` | In a music library the grid is albums; `view=artists` lists artists instead. Music is `artist` → `album` → `track`, audiobooks `audiobook` → `chapter`, photos `photoalbum` → `photo`. `studio` is an album's or track's artist and an audiobook's author; a track's `seasonNumber` is its disc and `episodeNumber` its number. Their `poster` is `/items/{id}/cover`. |
| GET | `/libraries/{id}/filters` | `{genres, decades}` for a filter bar; decades newest first (`2020`, `2010`…). |
| GET | `/people?name=` | A cast or crew member by name: `{name, photo, titles: [{item, role, kind}]}`, newest first, only titles you can see. `kind` is `cast` if they acted in it; `role` is their parts, else their jobs. 404 when nothing credits them. |
| GET | `/libraries/{id}/collections`, `/collections/{id}` | Collections. A smart one carries its rules (`labelRule`, `genreRule`, `studioRule`, `yearFrom`, `yearTo`); everything matching all the set rules is a member, plus anything added by hand. |
| GET | `/rows?key=&libraries=&genre=&offset=&limit=` | Every title of a built-in row (`recent`, `released`, `top-rated`, `unwatched`, `genres` with `genre`, `watched`, `continue`) from the listed libraries the user can see, a page at a time (limit up to 500, default 120): `{title, items}`. Rows on `/home` and `/libraries/{id}/hubs` give the query as `seeAll`. |
| POST | `/admin/libraries/{id}/kometa` | Admin. Body: a Kometa collection file (YAML). `?dryRun=1` only returns the plan; `?recommended=1` shows collections on Recommended unless the file sets `visible_library: false`. Returns `{collections: [{id, name, lists, sort, sortTitle, exists, visible, skipped}]}`; posters and list syncs continue in the background. |
| PUT, POST | `/admin/collections/{id}/artwork` | Admin. `PUT {type: "poster"\|"backdrop", url}` downloads an image (or a ThePosterDB poster page; an empty url resets it); `POST ?type=` takes a multipart `file`. |
| GET | `/items/{id}` | A movie, show, season or episode, with files, streams, cast and children. Movies and shows include `externalRatings` (`imdb`, `imdbVotes`, `imdbId`, `rottenTomatoes`, `rottenTomatoesAudience`, `metacritic`) once a ratings provider has scores for them. Movies, episodes and shows include `quality` (`resolution` 4k|1080|720|sd, `range` dv|hdr10|hlg|hdr, `audio` atmos|dtsx|truehd|dtshd|7.1|5.1) of the best copy; a show's from its best episode. |
| GET | `/items/{id}/recommend` | People this movie or show can be recommended to (opted in, and able to see it): `[{id, name}]`. |
| POST | `/items/{id}/recommend` | `{userIds, note}` → `{sent}`. Recommending again renews it. |
| DELETE | `/recommendations/{itemId}` | Hide a title from your "Recommended for You" row. |
| GET/PUT | `/me/recommendations` | `{accept}`: whether others may recommend titles to you (off by default). GET also has `enabled`, false when an admin turned recommendations off (setting `recommendations_enabled`); then recommending answers 403 and the row is left out. `/home` includes the row (`id: "recommended"`) and `recommendedBy` (item id → `{from, notes}`). |
| GET | `/items/{id}/next` | The next episode to play. |
| GET | `/items/{id}/tracks` | What playing an artist, album, audiobook or photo album plays or shows, in order: `[{item, file: {fileId, duration, container, audioCodec, width, height, size}, album}]`. `/items/{id}` for an album, audiobook or photo album (or one of their tracks, chapters or photos, with `parent`) includes the same as `tracks`, and `artist` for albums and tracks. |
| GET | `/items/{id}/cover?w=` | An album's, artist's, audiobook's or photo's artwork as JPEG: the folder's `cover.jpg`, else the picture inside a song, else (photos) the photo. `w` is `small` (300), `medium` (600, the default) or `large` (1200). 404 when there is none. |
| POST | `/items/{id}/watched`, `/items/{id}/unwatched` | Mark watched or unwatched. |
| GET | `/search?q=` | Search titles and people. |
| GET | `/requests` | Your title requests (admins: `?all=1` for everyone's) → `{requests, enabled, canRequest, reason, limits, fourK, watchlist, trakt, kidsWait, household}`. `limits` is `{movie, show}`, each `{limit, used, resetsAt}` (`limit` 0 = none) for the last 7 days; `fourK` says you may ask for 4K copies; `watchlist` is your Trakt watchlist switch and `trakt` whether an admin set Trakt up; `kidsWait` is true for a kid whose requests go to a parent; `household` lists your kids' requests waiting for you. Admins also get `handlers` (the plugins that fetch approved requests), `problems` (open problem reports) and `cleanupDays`. A request is `{id, kind, tmdbId, title, year, poster, overview, requestedBy, people, note, status, reason, link, seasons, fourK, stage, progress, eta, releaseDate, availableAt, mine}`; `status` is `parent` (a kid asked; waiting for a parent), `pending`, `approved` (waiting for the plugin), `sent`, `failed`, `declined` or `available`. `seasons` (shows) are the seasons asked for, none being the whole show. While `sent`, the plugin's `stage` (`searching`, `downloading`, `importing`, `waiting` when it isn't out yet), `progress` (0–1) and `eta` (Unix seconds) say how it's going. |
| GET | `/requests/search?q=&kind=` | TMDB movies and shows to request: `[{kind, tmdbId, title, year, overview, poster, releaseDate, itemId, onServer, request}]`. 403 for kids (unless an admin lets kids request, `request_kids`) or when an admin turned requests off (setting `requests_enabled`); 503 without a TMDB key. |
| GET | `/requests/discover` | Rows for the Requests page, leaving out what the server has: `[{key, title, items}]` with "Because you watched …" (TMDB's recommendations for your two latest finished titles), trending this week, upcoming movies, shows on the air, popular movies and shows. Items are like `/requests/search`'s. |
| GET | `/requests/title/{kind}/{tmdbId}` | One title from TMDB with what the server has: `{title, backdrop, status, has4K, fourK, limit, kidsWait, seasons}`; for shows each season is `{number, name, episodes, airDate, onServer, requested}` (`onServer` counts its episodes here). |
| GET | `/requests/similar/{kind}/{tmdbId}`, `/requests/person?name=`, `/requests/collection/{id}` | Titles the server doesn't have: like this one, with this person (cast or crew), or a collection's TMDB collection or lists. |
| POST | `/requests` | `{kind, tmdbId, title, year, poster, overview, note, seasons, fourK, releaseDate}` asks for a title (201), or joins an existing request for it (200). Asking for more seasons, or the 4K copy, of a request that came in or was declined starts it over. 409 when the server already has what you ask for; 429 with when the next one frees up once you've used your weekly limit (joining your own request doesn't count, nor do watchlist requests); 403 for 4K unless an admin allowed it (`request_4k`). Admins' requests, and those of people an admin set to auto-approve, are approved at once; a kid's waits for a parent. |
| DELETE | `/requests/{id}` | Take your name off a request that isn't approved (or was declined, or is in); admins delete any with `?all=1`. |
| POST | `/requests/{id}/parent` | A grown-up in a kid's household answers their request: yes sends it on (to the admins, or approved at once when the parent's requests are), `?answer=no` with `{reason}` declines it. |
| GET / PUT | `/requests/settings` | `{watchlist}`: request what's on your Trakt watchlist (checked every 6 hours; needs Trakt connected under Account). |
| POST | `/admin/requests/{id}/approve`, `/admin/requests/{id}/decline` | Admin. Decline takes `{reason}`, which the people who asked are told. Approving a failed request tries again; approving tells the people who asked. |
| GET | `/admin/requests/rules` | Admin. Each person with their request rules `{autoApprove, movieLimit, showLimit}` (limits per 7 days; -1 follows the server's `request_movie_limit` / `request_show_limit`, 0 is none) and their current `movies` and `shows` limit state. |
| PUT | `/admin/requests/rules/{userId}` | Admin. Sets `{autoApprove, movieLimit, showLimit}`. |
| GET | `/admin/requests/unwatched?days=` | Admin. Requested titles in at least `days` (default: setting `request_cleanup_days`) that none of the people who asked has played: `{days, requests}`, each request with the `items` it brought in. |
| POST | `/items/{id}/problems` | Report a problem with a movie or episode: `{kind, note, position}`, `kind` being `video`, `audio`, `subtitles`, `wrong` or `other`. Adds to your open report about the same thing. Admins are notified. |
| GET | `/problems` | Your reports (admins: `?all=1` for everyone's open ones, `&resolved=1` with fixed ones too). A report is `{id, itemId, user, kind, note, position, status, reply, title, showTitle, itemKind, seasonNumber, episodeNumber, poster, tmdbId}`; `status` is `open`, `fixing` or `resolved`. |
| POST | `/admin/problems/{id}` | Admin. `{action, reply}`: `resolve` (the reporter hears, with `reply`), `reopen`, or `refetch`, which sends `problem.refetch` to the plugin that fetches requests so it replaces the file (409 when none is running). |
| DELETE | `/problems/{id}` | Your own report, or any for admins. |
| GET | `/discover` | Discover rows: `[{id, name, videos: [{id, title, description, publishedAt}]}]`, newest first. `id` is the YouTube video id (thumbnail `https://i.ytimg.com/vi/{id}/mqdefault.jpg`). |
| GET | `/discover/videos/{id}` | Plays a Discover video through the server (TV app): yt-dlp finds its stream and the answer is `{url, type}`, an HLS playlist (`/discover/videos/{id}/index.m3u8`, its parts at `…/s/{n}`; `type` `m3u8`) or an MP4 file (`…/video.mp4`, with Range; `type` `mp4`). Only videos on Discover; 502 with the reason when YouTube won't give one. One lookup per video runs at a time and carries on if the caller goes away (it's kept for the next ask); 504 if it takes over 90 seconds. |
| POST | `/discover/videos/{id}/prepare` | Starts finding a Discover video's stream ahead of a press (the TV app asks when a tile stays highlighted). 202; at most 2 such lookups run at once, beyond that it does nothing. |
| GET / POST / DELETE | `/watchlist`, `/watchlist/{id}` | Your watchlist. |
| GET / POST / PUT / DELETE | `/playlists`, `/playlists/{id}`, `/playlists/{id}/items`, `/playlists/{id}/order` | Playlists. |
| GET | `/profiles`, `/profiles/{username}` | Public profiles and stats. |

Image URLs in responses are already paths on this server (e.g. `/api/images/tmdb/abc.jpg`); add `?w=w342` (TMDB sizes) to get a smaller one.

## Playing

| Method | Path | Notes |
|---|---|---|
| GET | `/files/{id}/direct` | The original file, with range requests. Use when the client can play the codec. Away from home, a file above the admin's quality limit answers 403 (convert it instead) and going over the admin's stream limit answers 429 with a message to show. |
| GET | `/files/{id}/audio?start=` | A song or chapter converted to MP3 from `start` seconds, for players that can't play the file as it is; seek by asking again. Photos from `/files/{id}/direct` don't count against the stream limits. |
| POST | `/files/{id}/transcode` | `{start, maxHeight, bitrateKbps, audioStream, burnSubtitle, replace, audioCodecs, maxAudioChannels, adaptive}` (all optional; `-1` = default track / none) → `{session, playlist}`: open `playlist` as HLS. Pass the old `session.id` as `replace` when seeking. `audioCodecs` lists what the client plays inside HLS (`aac`, `ac3`, `eac3`, `mp3`) and `maxAudioChannels` how many channels it outputs: a matching source track is copied untouched, other surround tracks become AAC 5.1 when `maxAudioChannels` ≥ 6, and everything else (or a client that sends neither) gets AAC stereo. `session.audioCodec`, `audioChannels` and `audioCopy` say what was chosen. With `adaptive: true`, when the admin has turned on adaptive streaming and the video has to be converted anyway, `playlist` is a master playlist with up to three qualities (`session.variants`, best first). Away from home the admin's quality limit caps the video (files above it are converted down) and a new session over the stream limit answers 429; `replace` sessions aren't new streams. |
| POST | `/files/{id}/offline` | Offline download for the phone app. `{height, audioStream, burnSubtitle}`: `height` 0 is the original file, otherwise a one-off MP4 copy (H.264, stereo AAC) no taller than that, made in the background with `audioStream` and an image subtitle `burnSubtitle` burned in (`-1` = default track / none). → `{status, progress, url, size, height, error}`: `status` is `ready` (download `url`, which takes range requests), `queued` or `preparing` (`progress` 0..1; ask again), or `failed` (asking again retries). Same rules as `/files/{id}/direct?download=1`: admins, or everyone when the admin allows downloads (403 otherwise), and only titles the profile can see. Copies nobody asks for in 7 days are deleted. |
| GET | `/offline/{key}` | A finished copy from `/files/{id}/offline` (its `url`). Checks downloads and access again; 404 when it has been cleared (ask for it again). |
| POST | `/offline/check` | `{items: [itemId…]}` → `{canDownload, allowed: [itemId…]}`: which downloaded titles the profile may keep. The phone app deletes the others (downloads turned off, a library taken away, a kid approval removed). |
| GET | `/transcode/{sid}/{name}` | HLS playlist and segments. `stream.ts` is the whole conversion as one MPEG-TS stream, following it as it goes (for DLNA TVs). |
| DELETE | `/transcode/{sid}` | Stop a transcode when the viewer leaves. |
| GET | `/files/{id}/subtitles/{stream}` | A subtitle track as WebVTT. |
| GET | `/files/{id}/subtitles/search?lang=` | Search OpenSubtitles for the file (`lang` is a language name or code, English by default) → `{language, results: [{fileId, name, language, downloads, hearingImpaired, hashMatch, trusted, ai}]}`, best first. 409 without an API key in Settings; 403 for kid profiles. |
| POST | `/files/{id}/subtitles/download` | `{fileId, language, hearingImpaired}` from a search result: saves it beside the video as `<video>.<lang>.srt` → `{streams, index}` (the file's streams and the new one's index). 429 when the day's OpenSubtitles downloads are used up. |
| GET | `/files/{id}/chapters` | Chapter marks → `{chapters: [{start, end, title?}]}`, seconds in the original file. Empty when the file has none (a single chapter covering the whole file counts as none). |
| GET | `/files/{id}/trickplay` | Scrub previews → `{interval, width, height, columns, rows, count, sheets}`: thumbnail `n` (at `n × interval` seconds) is in sheet `n / (columns × rows)`, column `n % columns`, row `(n / columns) % rows`. 404 until they're made (asking starts making them). |
| GET | `/files/{id}/trickplay/{n}.jpg` | Sheet `n` (from 0) of thumbnails. |
| GET | `/files/{id}/trickplay.bif?from=` | The thumbnails as a Roku BIF file; `from` is where a conversion started, in seconds. |
| POST | `/progress` | `{playSessionId, itemId, fileId, position, duration, paused, stopped, mode, client, played}`. Send every ~10 s while playing and once with `stopped: true`. `played` is the total seconds actually played in this play session (the server caps it at wall-clock time). Optional `lights: false` keeps Hue Sync and other light plugins off this player (left out means on). This drives resume points, watched state, Now Playing, history and webhooks. A new session's first report answers 429 when it puts the account over the stream limit away from home. |

### Casting to TVs

The server plays on Chromecasts, Google TVs and DLNA smart TVs on its network; the app that starts a cast is only its remote. TVs open the media with a `?token=` that only reaches that cast's file, its conversion and artwork, and stops working when the cast ends.

| Method | Path | Notes |
|---|---|---|
| GET | `/cast/devices?refresh=1` | `{devices: [{id, name, kind, model, host}]}`, `kind` `chromecast` or `dlna`. Found by mDNS and SSDP (about 2.5 s) plus the admin's `cast_devices` addresses, and kept for a minute; `refresh=1` looks again. |
| POST | `/cast/start` | `{deviceId, itemId, fileId?, start?, audioStream?}` (a movie or episode; `start` in seconds) → a cast: `{id, device, itemId, fileId, title, subtitle, poster, duration, position, state, volume, mode, error, started}`. `state` is `loading`, `playing`, `paused`, `idle` or `finished`; `volume` is 0–1 (Chromecast) or -1; `mode` is `direct` (MP4 with 8-bit H.264 and AAC/MP3 up to 1080p) or `transcode` (HLS for Chromecasts, one MPEG-TS stream for DLNA). A new cast to a TV ends the one already on it. 502 with the TV's reason when it won't play. |
| GET | `/cast/sessions` | `{sessions: [...]}`: your casts. The server follows each TV, saves the position like `/progress` (client "Cast to *TV name*") and ends the cast when the TV stops or finishes. |
| GET | `/cast/sessions/{id}` | One cast; 404 once it ended. |
| POST | `/cast/sessions/{id}` | `{action: "play" \| "pause" \| "seek" \| "volume" \| "audio", position?, level?, audioStream?}` → the cast. Seeking a conversion starts a new one there and reloads the TV. `volume` is Chromecast only (400 for DLNA). |
| DELETE | `/cast/sessions/{id}` | Stop playing on the TV. |

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
| POST | `/live/viewers/{viewer}/buffering` | `{seconds}`: the player has been buffering this long. Past `stallSecs` (from `/watch`) the server may move the channel to its next stream → `{switched, upstream, reason}`. `upstream` is true when the server sees the stream itself late or stalled; with nothing to switch to, players should keep waiting rather than restart. |
| GET | `/live/playlist.m3u`, `/live/epg.xml` | M3U and XMLTV for other IPTV apps. Use `?token=<API key>`. |
| GET | `/live/tuner/<API key>/…` | HDHomeRun-style tuner for Plex, Emby and Jellyfin: `discover.json`, `lineup.json`, `lineup_status.json`, `device.xml`, plus `epg.xml` and `playlist.m3u`. The key is in the path because Plex can't send one any other way. |
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
| GET | `/together/{id}` | The room (404 once it has ended, or when you can't see what it plays). `?watching=1` joins it by polling (the TV app): you count as a member while you look at least every 10 s. |
| GET | `/together/{id}/events` | Server-sent events: the room as above after every change and join or leave. |
| POST | `/together/{id}/state` | `{paused, position, itemId?}` play, pause, seek or move everyone to another movie or episode. |
| GET / POST | `/together/{id}/people`, `/together/{id}/invite` | Who can be invited (people who can see it and left `prefs.notifyTogether` on; a kid only sees other kids); `{userIds}` invites them. |
| GET | `/together/invites` | Your open invitations. `DELETE /together/{id}/invite` declines one. |

`state.position` is seconds at `state.at` (server ms); while playing, add the time since, using `now` to correct for clock differences.

### Sports

Scores and schedules come from ESPN's public (unofficial) site API, cached on the server.

| | | |
|---|---|---|
| GET | `/sports` | `{leagues, allLeagues, teams, games, error?}`: your league rows (see below), the teams you follow, and their games from two days ago to a week ahead (live first, then upcoming, then results). Each game has `mine` (your team ids in it) and, with Live TV, `channels` whose guide shows it. |
| GET | `/sports/live` | `{games}`: just your teams' games on now (today's and yesterday's scoreboards), with `channels`, for the player's score bug. |
| PUT | `/sports/teams` | `[{league, id}]` replaces your teams (at most 40); names, logos and colours come from ESPN. |
| GET | `/sports/scores?date=YYYYMMDD&leagues=` | The Sports Hub: `{date, leagues, allLeagues, teams, games, error?}` with the games of your league rows on the day (today by default), or only the comma-separated `leagues`. Games carry `mine` and, with Live TV, `channels`; `error` names leagues ESPN didn't answer for. |
| GET | `/sports/{league}/games/{id}` | One game: `{game, stats, players, leaders, standings}`. `game` has each side's `lines` (score by period); `stats` are team stats side by side (`{label, away, home}`); `players` are per-team tables (`{team, title, labels, rows}`); `leaders` are per category (`{title, away, home}`); `standings` are the teams' groups (`{title, labels, rows}`). |
| GET | `/sports/{league}/teams` | Every team in a league. `GET /sports` lists the 15 leagues (`id`, `name`, `sport`): NFL, college football, UFL, CFL, NBA, WNBA, men's and women's college basketball, MLB, college baseball, NHL, men's college hockey, MLS, UFC and NASCAR Cup. UFC fights are games whose sides are the fighters. A NASCAR race is a game with empty sides and `race`, its top ten (`{place, name, shortName, number, winner}`). Neither has teams to follow. |
| GET | `/sports/leagues` | Every league the server knows (`id`, `name`, `sport`) with `hidden`: whether an admin took it out of the Sports section. |
| GET | `/sports/{league}/scores?date=YYYYMMDD` | The league's games on an ESPN (US Eastern) day, today by default. |
| GET | `/sports/watch-links?network=ESPN2&network=ABC` | For a CFB Tracker (Stat Scraps), not signed in: send the **Sports data** key as `X-Api-Key`. `{links: [{network, channel: {id, number, name}, path}]}` for each network (up to 12) that has a channel: the first one tied to it under **Live TV → Networks**, else a channel named for it. `path` is the web page that plays it (`/live/{id}`). 404 while no key is set, 401 for a wrong key. |

Game alerts are notifications of kind `sports`, set by the `gameAlertMinutes` (0 for none) and `notifyGameScores` preferences. `scoreBug` (default on) shows live scores in a corner of the players. `spoilerFree` has the TV Sports Hub hide scores until asked, and turns the score bug off. `multiViewLayouts` (`[{name, channels}]`, at most 12, 1 to 4 channel ids each) are saved Sports Multi-View layouts.

**Which leagues show.** An admin can take leagues out of the Sports section for everyone with the `sports_hidden_leagues` setting (comma-separated ids, Server → Settings → Sports data); those leagues are left out of every list, their teams can't be followed (`PUT /sports/teams` answers 400), their per-league routes answer 404, and the tracker stops following them. Each profile then orders and hides the rest with the `sportsOrder` (league ids moved to the top, in order; the others follow in the usual order) and `sportsHidden` (rows turned off) preferences. `leagues` in `/sports` and `/sports/scores` is the profile's rows in its order, which the TV, Roku and web apps draw one row each; `allLeagues` is every league the server shows, in the profile's order, for picking teams and rows. A followed team's games still show under Your teams when its league's row is off.

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

`GET /api/admin/files?offset=&limit=` (up to 1000 a page) is the plugin API's `/files` for an admin's key: every movie and episode file with its item and quality, `{files, total}`. Cinemask reads quality this way instead of opening every title.

`GET /api/admin/health` (`?refresh=1` asks GitHub again) is the Health page: `{update: {current, checking, latest: {version, url, publishedAt, notes}, available, error}, folders: [{label, kind, path, online, error, total, free, low}], lowPercent, system, tasks, alerts, channels}`. `POST /api/admin/alerts/test` takes the `alert_*` settings (unsaved ones win over saved) and returns `{results: {email|discord|ntfy: "sent" or the error}}`. `GET /api/admin/system` has `update` when the daily check found a newer version.

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
| `request.created` / `request.approved` / `request.declined` / `request.available` | Title requests: someone asked, an admin approved or declined, or the title arrived in a library. `data` is the request. |
| `problem.created` / `problem.refetch` | Problem reports: someone reported one, or an admin asked for a new copy. `data` is the report. |
| `test` | The *Send test* button. |

Playback events also carry `userId`, `tmdbId`, and for episodes `showTmdbId`, `season`, `episode` and `episodeTitle`.

A webhook whose format is **Plex-style** instead sends what Plex sends: `multipart/form-data` with a `payload` field holding `{event: "media.play" | "media.stop" | "media.scrobble", Account: {title: <username>}, Player, Server, Metadata}`. Only playback events are sent in this format. Use it for apps that accept Plex webhooks, such as Yamtrack.

The `X-BingeBandit-Event` header names the event. If the webhook has a secret, `X-BingeBandit-Signature: sha256=<hex>` is the HMAC-SHA256 of the raw body with that secret. Compare it in constant time before trusting the request. Deliveries time out after 10 s and aren't retried.
