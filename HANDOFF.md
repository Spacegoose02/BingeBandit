# Handoff

Where BingeBandit stands, written so the next person (or AI session) can pick it up cold. Read **Start here** first; everything under **Log** is the history of each piece of work, newest first, with what was and wasn't tested.

_Last updated: 2026-10-30. Latest release v0.36.0, 50 schema migrations._



## Sports Hub fixes: NHL scoreboard, Multi-View freezes (2026-10-30, no migration)

- **NHL "Couldn't reach ESPN":** the NHL's probable goalies come with `athlete.position` as a plain string ("G") where every other feed sends `{abbreviation}`, so decoding failed and the whole NHL scoreboard was dropped. `espnPosition` reads either (checked against ESPN's live NHL scoreboard; `TestPositionAsString`).
- **Multi-View secondary screens freezing:** screens played at the live edge with no recovery. Each screen now plays the server's buffer behind live (`liveBuffer`, as the full-screen player does), reports a stall past the server's stall time (so the server can move the channel to its next stream), and rejoins when buffering or a standing-still picture lasts that long, or after an error (backing off up to 30 s). Still not tried on a device; a box with fewer hardware video decoders than screens can still struggle.

## Start here

### What this is

BingeBandit is a self-hosted media server meant to replace Plex, Jellyfin, Dispatcharr and ErsatzTV in one app: libraries, live TV with a guide and DVR, custom channels, and apps for browsers, Android TV / Fire TV / phones and Roku. It's pre-1.0 and runs at home and on family members' TVs. Michael (GitHub `Spacegoose02`) owns it and makes the product calls; when this log says "Michael chose", that's a decision to keep unless he changes it.

### Where things are

| Need | Look in |
| --- | --- |
| What it does, for people running it | [docs/GUIDE.md](docs/GUIDE.md) |
| How the server works, package by package | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Build, run, fakes, checks, code rules, releasing | [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) |
| HTTP API and plugin protocol | [docs/API.md](docs/API.md), [docs/PLUGINS.md](docs/PLUGINS.md) |
| Every board card and whether it's done | [docs/ROADMAP.md](docs/ROADMAP.md) |
| The apps | [apps/tv/README.md](apps/tv/README.md), [apps/roku/README.md](apps/roku/README.md) |
| Why Go, SQLite, React and HLS | **Decisions made**, near the end of this file |

### The five-minute version

```sh
./build.sh && ./dist/bingebandit          # or .\build.ps1 on Windows; http://localhost:8484
cd server && go vet ./... && go test ./... # what CI runs, plus gofmt -l . and the web build
cd server && go run ./cmd/fakeiptv         # a fake IPTV provider; more fakes in DEVELOPMENT.md
```

Before changing code, know these:

- **Migrations are append-only and hashed.** Add a new one at the end of `store/schema.go`; `migrate_test.go` fails if a shipped one changes (v0.22.0 broke upgrades that way). Prefer prefs or settings rows when they'll do.
- **Every user-facing item query goes through `store.visible`** (library access, profiles and kid mode). Hidden things answer 404.
- **Three clients share the API:** web (`web/src/types.ts`), TV app (`apps/tv/src/types.ts`) and Roku. Older installed apps don't send new fields, so the server must keep stored values when a field is missing.
- **The TV app is mostly tested by typechecking.** Most TV work since the phone layouts was never run on a device; say so in the log when that's the case, and say what you did try.
- **Plugins are separate binaries** under `server/plugins/`, listed in `build.sh`, `build.ps1`, the Dockerfile and `release.yml`. A new bundled plugin goes in all four.

### Open work (task board, 2026-10-30)

Several of these are being worked on in parallel; check open pull requests before starting one. Done in v0.35.0 and now in the log: Hue Sync and Game Lights per profile and per device on the TV, a profile's own Hue hub, the Sports Hub, the single OK click, the Roku settings passcode and the /tv install link.

- **Sports settings on the web** from the board's screenshots: Sports rows, Sports channels, Multi-View, favorite teams by division.
- **An admin switch for recommendations on the web.** (`recommendations_enabled` already exists in admin Settings, from the third feature board; check what the card still wants.)
- **Bugs and security:** a review pass is in progress.

### Never tried for real

Most outside services and all hardware were only tested against fakes. These are the ones most likely to need fixes the first time they meet the real thing:

- **Devices:** the TV app on the onn box since the phone work (profile picker onward), the last step of the in-app APK update, any Roku (playback and the LAN scan never ran even in the simulator), surround passthrough to a receiver, a TV with the internet unplugged, installing from `/tv` (the stamped APK and auto-connect), the Sports Hub against live ESPN (NASCAR's fields especially), Multi-View with four streams (each now ignores Android audio focus) and Game Lights rooms per TV.
- **Hue:** Hue Sync and Game Lights on a real bridge, pairing a hub from the TV and the TV driving its own lights (Sync colours, Theater, Game Lights cues), Hue Entertainment streaming, CPU use of Hue Sync on 4K HEVC.
- **Live TV:** a real IPTV provider or HDHomeRun, a real Dispatcharr, YouTube Live and Discover against real YouTube.
- **Services:** TMDB and TheTVDB matching on a real library, Trakt, Plex and Tautulli import, chart lists (TMDB, Trakt, IMDb, Yamtrack, MDBList), Wikidata award collections, ThePosterDB scraping, real OIDC providers (Google, Authelia), passkeys on a phone, S3 on AWS or Backblaze, Cinemask with real indexers.
- **Media:** intro and credits detection on real seasons (thresholds may need tuning), image subtitle burn-in from a Blu-ray rip, Dolby Vision and Atmos detection, Quick Sync on Intel and NVENC session limits with adaptive streaming.

### Keeping this file useful

Add a dated `##` section at the top of the log (right under `# Log`, not above **Start here**) for each piece of work: what changed (with file and function names), the migration number if any, and what was and wasn't tested. Update **Open work** and **Never tried for real** when they change, and the date and release line above.

# Log

## Game Lights per TV (2026-10-30, no migration)

- Game Lights now works like Hue Sync: the profile switch (web Account or TV Settings) allows it, and each TV turns it on for itself and picks its own room. TV Settings → This TV has "Game Lights on this TV" (off until turned on) and "Game Lights room"; with the TV's own Hue hub paired, the hub's room is used and the TV plays the cues itself.
- **Plugin** (`game-lights/config.go`, `me.go`): `Person.Devices`, keyed by an id each app makes for itself (`bb.deviceId`, `devicePrefs.ts`), set with `PUT /me/devices/{id}`. Once any TV has set one, effects play only where a TV is on: in that TV's room on the server's bridge, or as cues for that TV (`/me/cues?device=`) when it has its own hub. People with no TVs set keep the profile's room. `PUT /me/settings` (web My Game Lights, the Sports Hub panel) keeps the TVs; Test plays wherever the person's TVs are on.
- The Sports Hub's Game Lights panel still starts and stops with the profile flag; its room row points to TV Settings once TVs have rooms.
- **Tested:** `go test ./plugins/game-lights/` (`TestDevices`), `tsc` and `eslint` for apps/tv. **Not tried on a device.**

## Sports Hub trimmed, NASCAR, Multi-View plays every screen (2026-10-30, no migration)

- Leagues cut from 43 to 15 (football, basketball, baseball, hockey, MLS, UFC) plus **NASCAR Cup** (`racing/nascar-premier`): a race is a game with empty sides and `Race`, its top ten, from the weekend's Race session (practice and qualifying are skipped). Races find Live TV channels whose guide names NASCAR. Teams saved in a dropped league stay in the database but are left out (`myTeams`, and the tracker skips unknown leagues).
- Multi-View passes `disableFocus` to react-native-video: each player took Android's audio focus as it started, and losing focus pauses a player, so only the newest screen played.
- **Tested:** `go test ./internal/...` (`TestRace`, the dropped-league case in `TestSports`), `tsc` and `eslint` for apps/tv, `tsc` for web. NASCAR's ESPN shape (competition `type.abbreviation`, competitor `order`, `vehicle.number`) is from memory, not checked against live ESPN. Nothing ran on a device.

## Sports Hub on the TV (2026-10-30, no migration)

- **Server** (`internal/sports`): 43 leagues (was 8), including WNBA, the big European soccer leagues and cups, rugby, AFL, cricket (IPL, Big Bash), lacrosse and UFC (each fight on a card is a game, the fighters its sides; `Teams` is empty for UFC). Scoreboard games now carry each side's `lines` (score by period) and `leader` (or probable starter). `Client.Summary` reads ESPN's game summary into box score, team stats, player tables, leaders and standings (cached 15 s live, 10 min otherwise). New routes `GET /api/sports/scores` (every league's day, 8 ESPN requests at a time) and `GET /api/sports/{league}/games/{id}`. Prefs `spoilerFree` and `multiViewLayouts`. The new league paths, the cricket/rugby ids and the leaders/probables fields weren't checked against live ESPN (no network here); a league ESPN rejects just shows in the hub's error line.
- **TV** (`src/screens/Sports.tsx`, `src/sports/*`, `src/screens/MultiView.tsx`): Scores / My Teams / Channels tabs, a day picker, a corner preview (OK previews a game on your channels, OK again is full screen), the focused game's line score beside it, sport chips and "With a channel", Live now, Your teams and a row per league. Holding OK opens box score, stats, leaders and standings with Follow buttons. PLAY adds a game's channel to Multi-View (route `multiview`): 1 to 4 screens, OK moves the sound, hold OK for options, a toolbar with layouts, saved layouts (profile prefs) and one press for the next game of your teams. Spoiler-free hides scores, stats and the score bug (web too) until "Show the score". The gear has spoiler-free, score bug, favorite teams and Game Lights: the same On/Off switch as Settings → This TV (`person.enabled`), Running/Stopped (`person.off`) and the Game Lights panel. **Typechecked and linted only; not tried on a device. Four streams at once depend on the box's decoders and the provider's connection limits.**
- Not done: the web's Sports settings page from the screenshots (Sports rows, Sports channels, Multi-View, favorite teams by division).

## Easy TV install with a /tv link (2026-10-30, no migration)

- **Server** (`api/tvapp.go`, `tvAppDownload`): `GET /tv` fetches the newest `BingeBandit-TV-*.apk` release (cached under `<data>/cache/tv-app`) and serves it with the server's address written into the APK Signing Block as an extra ID-value pair (`apkStampID`, "BBsv"). Signature schemes v2/v3 don't cover that block, so the signature still checks out; apksigner's padding shrinks to make room, so the file keeps its size. The address typed to get there goes first, the public URL second. If stamping fails, `/tv` redirects to the plain GitHub APK.
- **TV** (`ApkStamp.kt`, `AppUpdaterModule.kt`, `src/updates.ts`, `screens/Connect.tsx`): on first launch the app reads the stamp from its installed APK, checks the server answers and goes straight to the sign-in code; anything wrong falls back to "choose your server".
- **Web:** `/tv-setup` (`pages/TVSetupSheet.tsx`) is a one-page, large-print setup sheet for Fire TV or Google TV with the address and the helper's name filled in, linked from Link a device. TV-SETUP.md and GUIDE.md follow the new steps.
- **Tested:** `go test ./internal/api` (stamping, re-stamping, padding, unsigned zips rejected); a local `/tv` download of the real v0.34.0 APK kept its size, passed `unzip -t` and verified as v2 with apksigtool, and a Java port of the reader got the address back. The Kotlin was only compiled by the TV Android CI build. **Not tried on a real TV.** APKs before v0.35.0 have no reader, so auto-connect starts with v0.35.0.

## Hue per device and a profile's own hub (2026-10-30, no migration)

- **Sync or Theater per device:** the TV keeps its own type (`bb.hueType`, `devicePrefs.ts`) and sends it as `lightsMode` with progress reports (`api/playback.go`, `stream/activity.go`); the Hue Sync plugin uses it over the profile's mode (`playerEvent.deviceMode`), unless the profile has Hue Sync off. Settings → This TV has "Hue Sync for <profile>" (the same flag as Account on the web, off by default), then this TV's on/off, type and room. The player's Options change this device and report at once. The web and older apps send no type and keep the profile's mode.
- **A profile pairs its own hub from the TV** (`apps/tv/src/hue.ts`): the Hue local API only answers on the hub's own network, so households elsewhere couldn't use Hue. Settings → This TV → "Hue hub for this profile" (shown once Hue Sync or Game Lights is on) finds the hub through Philips' discovery service or a typed address, waits up to 30 s for its button and keeps the pairing on this device for the profile, then a "Hue room" row picks the room or zone; OK twice forgets it. While paired, the server-side room rows are hidden.
  - Theater dims the room while playing and restores it on pause and stop.
  - Sync: `GET /api/plugins/hue-sync/me/colours?fileId=&from=&seconds=` (`hue-sync/colours.go`) reads up to 60 s of zone colours with ffmpeg (4 at once at most); the TV fetches 30 s chunks ahead of the playhead and reports `lights: false` so the server's bridge leaves the device alone.
  - Game Lights: pairing sets `Person.local`; the plugin queues effects (`GET /api/plugins/game-lights/me/cues?after=`, `game-lights/cues.go`) instead of flashing its bridge, and the TV polls every 3 s from Home and plays each cue after the person's delay.
- **Tested:** `go test` for hue-sync (`TestDeviceMode`, `TestColoursForTV`), game-lights (`TestLocalCues`) and internal/api; `tsc` and `eslint` for apps/tv. **Not tried on a device or against a real hub.**

## TV app: one click per OK, Game Lights in Settings (2026-10-30, no migration)

- **Double click:** on Android TV a focusable Pressable gets a native `OnClickListener`, so Android already plays the click, and Pressability played `SoundManager.playTouchSound()` on top. Every Pressable now passes `android_disableSound={quietPress}` (`!isPhone`, `theme.ts`): TVs get Android's one click, phones keep React Native's.
- **Game Lights:** Settings → This TV has "Game Lights for <profile>" (off until turned on, the same flag as Account on the web) and, once on, the room its lights flash in.
- **Tested:** `tsc` and `eslint` for apps/tv. **Not tried on a device.**

## Roku settings passcode (2026-10-30, no migration)

- The Roku menu has **Settings** (`SettingsScreen`), which matches the Android TV app's Profiles & PINs. On its own, the account gets the settings passcode (turn on, change, turn off, lock now). In a household it's "Your PIN", and the owner also adds, changes or removes each profile's PIN (`PUT /api/household/{id}/pin`). A new code is typed twice. A change that gets 423 opens the passcode pad, `POST /api/me/passcode/unlock`, then sends the change again; a wrong code shows the server's "That didn't match".
- The 4 to 8 digit pad moved out of `ProfilesScreen` into `components/common/PinEntry` (fields `title`, `error`, `reset`, `pin`, `cancel`). It can't be called `PinPad`: that's a built-in SceneGraph node and silently wins.
- Tested in the brs-engine simulator against a local server: turn on, lock, change while locked (wrong code then right one), add a profile PIN while locked, Who's watching? wrong PIN and Back. Never run on a real Roku.

## Cinemask folders follow BingeBandit libraries (2026-10-30, no migration)

- Michael adds kids titles in Cinemask and they always landed in Movies, so he moved them to Kids Movies here afterwards. Cinemask already asks which root folder when it has more than one; the fix is mostly in Cinemask (its own PR): **Add BingeBandit libraries** in Settings → Media management reads `/api/libraries` (paths need an admin key) and finds each folder on Cinemask's side, and a title whose folder was moved here (`moveItem`) is found again in its new root after library sync and before an import.
- Here: plugin API `/collections/{id}/missing` also returns the collection's `library` name, and Cinemask Upgrades 1.2.0 monitors a collection's missing titles in the Cinemask folder of that name (`RootFolders`, `rootFor`).
- Not tried against a real Cinemask.

## Admin files for Cinemask (2026-10-28, no migration)

- `GET /api/admin/files` is the plugin API's `/files` (`pluginFiles`, `Store.FileSummaries`) for an admin's key, so Cinemask's library sync gets every movie and episode file's quality in a few requests instead of opening each title. Tested in `adminfiles_test.go`.

## Working without the internet (2026-10-28, no migration)

- **Server id:** `/api/health` now also returns `id` (`Store.ServerID`, setting row `server_id`, made on first use, not listed with the admin settings, carried by backups). Apps use it so another server on a network is never taken for theirs.
- **TV/phone app** (`apps/tv/src/addresses.ts`): the session keeps `alternates` (the same server's other addresses) and `serverId`. `learnAddresses()` runs after the app opens or signs in: it notes the id, adds `/api/link/info`'s public URL, and when no known address is a private IP or `.local` it scans the /24 for the same id (once a day per server, Wi-Fi or Ethernet only). When a signed-in request can't connect, `request()` calls `reconnect()` (registered with `onUnreachable`): probe the other addresses (3 s, id must match), else scan; the one that answers becomes `server` (saved with `patchSession`, which keeps the cache) and the request is sent again. A request that timed out is only repeated for GETs. Requests failing together share one round; after a miss, failures for 15 s don't start another.
- **Roku** (`ApiTask.brs`): the same in BrightScript. Globals `alternates`, `serverId`, `reconnecting`; `httpRequest` retries once through `reconnect()`; build `"learn"` runs `learnAddresses()` from Home after `/api/me`. Registry `lanScanAt.<id>` limits scans to once a day.
- **Cache artwork task** (`cache_artwork`, every 6 h, and after each Scan libraries task): downloads TMDB posters at w342, backgrounds at w1280 and w780, and logos at w500 that aren't on disk yet, newest titles first, at most 1,500 a run, four at a time; stops after 20 failures with none fetched (offline). `ImageCache.Has` checks the disk; `api.Server.Images` and the task share one `ImageCache`.
- **Docs:** GUIDE.md section 10 "When the internet is down" (local DNS for the public name, what still needs the internet).
- **Tested:** Go tests for the server id, `TMDBArtwork` and `ImageCache.Has`. TV app typechecked and linted; Roku checked with brighterscript. Never tried on a real device with the internet unplugged.

## Smaller roadmap gaps (2026-10-27, migration 46)

- **Multi-episode files:** `library/names.go` `lastEpisode` reads the extra episodes after the first marker (`S01E01E02`, `-E02`, `-02` as a range, `.S01E02` in the same season; the number must end at a separator, at most 30 per file). `Scanner.episodeItems` makes a row for each; the file stays on the first (`media_files.item_id`) and later ones are linked in `media_file_episodes` (migration 46, `Store.SetFileEpisodes` replaces a file's links each scan). `FilesForItem`, custom channel playables and `deleteOrphans` follow the links; merges move them. Finishing one finishes the others (`SaveProgress` → `SharedFileEpisodes`) and `NextEpisode`/`UpNext` skip the rest of the file just played. Extra episodes get "Episode N" until metadata names them.
- **Hue Entertainment streaming (Hue Sync 1.4.0):** pairing asks for `generateclientkey` (stored as `ClientKey`; bridges paired earlier must forget and pair again, the setup page says so). `/areas` lists v1 groups of type `Entertainment` too, and the web Account and TV Settings pickers show them. A session whose device picked one streams (`streamsync.go`): CLIP v2 over HTTPS (bridge cert not checked) to start/stop the entertainment configuration, DTLS 1.2 PSK (`pion/dtls`, identity from `/auth/v1` `hue-application-id`) to port 2100, HueStream v2 XY packets at 25 Hz, sampler at 12 fps with easing. Channels follow left/right/top/bottom/all by their position. One stream at a time; anything else falls back to light commands. Light commands for pause/theater are replaced by stream colours while streaming; `on_stop` waits for the stream to stop.
- **TV surround:** the TV app sends `audioCodecs` with `ac3`/`eac3` when `AudioSupport` says the box can decode or pass them through, and the new transcode option `surroundCodec` (`ac3` first, else `eac3`) turns DTS/TrueHD and other 6+ channel tracks into Dolby Digital (640k) or Dolby Digital Plus (768k) instead of AAC 5.1. Direct play already used the same check.
- **Tested:** Go tests for names, a scan of a temp library (links, rename back to single, next episode, watched), stream packets and a fake bridge (HTTPS v2 + a pion DTLS PSK server) end to end, `audioArgs`. TV app typechecked and linted only. Never run against a real Hue bridge, a real receiver or the onn box.

## Third feature board (2026-10-27, no migrations)

- **PR #100 (merged):** score pills shrink to fit (`overlay/draw.go` `drawRatings`, cache key v3); custom channel editor Previous/Next, X to cancel a scheduled item (`store.CancelPlayout`), Restart channel (`livetv.RestartPlayout`); Hue Sync per profile (Off default / Sync / Theater, plugin `GET/PUT /me`, `People` in its config) and Game Lights opt-in per profile (`Person.Enabled`, `Off` = stopped, `Skip` teams, config version 2).
- **Player:** Summary button and an Options menu (Watch together, Hue Sync mode, Quality, Audio & Subtitles) on web and TV; the TV seek bar is focusable and scrubs with left/right (10/30/60 s steps, commits after 900 ms or OK).
- **Recommendations:** admin setting `recommendations_enabled` (default on); `GET /api/me/recommendations` says `enabled`. TV app has a Discover tab (YouTube rows) that is opt-in through `prefs.pinnedTabs` (`OptInTabs`), and a "Recommendations to me" toggle.
- **Live TV on TV:** CH+/CH− step channels; holding OK on a guide cell opens Watch / favourite / Record or Ask to record / Follow show (the logo column isn't focusable, so favouriting is from the programme cell).
- **Alerts:** prefs `notifyTogether`, `liveFollows` + `liveAlertMinutes` (kind `live`, `notifyLiveShows`, alert ids `live:epg:start` in `sports_alerts`), `quietWhilePlaying`, `scoreBug`. Notifications tick every minute now. Watch together invitees: everyone who can see the title and allows invites; kids only see kids.
- **Cinemask collections:** Cinemask Upgrades 1.1.0 setting "Let collections add their missing titles" (off). Then the collection editor shows Monitor/Search switches (stored in the plugin's `collections.json`, with `added` so a title is handed over once). Plugin API `GET /collections/{id}/missing` (`importer.Lists.Missing`: TMDB collection parts for movie libraries plus followed lists, minus anything in any library).
- **Score bug:** `GET /api/sports/live` (today's and yesterday's scoreboards only); web `components/ScoreBug.tsx`, TV `src/scorebug.tsx` over any player, hidden on the game's own channel.
- Already existed, so nothing built: trailers / custom intro before movies (prefs `preRollTrailers`, `preRollIntro`), TV passcode settings (Settings → Profiles & PINs). Local guides by zip: dropped by Michael.
- Nothing here was tried on a device, a real Hue bridge, or a real Cinemask.
- **YouTube Live** (Michael chose real channels via yt-dlp): plugin `server/plugins/youtube-live` (bundled, off by default) lists YouTubers (setting `youtubers`), checks `/channel/<UC>/live` every 3 min (`isLiveNow` + canonical /watch link; upcoming streams don't count) and sends the whole list to the new plugin API `PUT /live/channels`. `livetv.SyncPluginChannels` keeps them in an m3u source at `plugin:<id>` (no migration: the sources table only allows m3u/epg, so `IsPluginSource` checks the URL and refresh skips it), creates one channel per stream (kept, just disabled while not live, so numbers and favourites stay) and writes a guide entry per live stream (start → now+2 h, pushed on each report). The relay resolves youtube.com links with `yt-dlp -g` each time it opens them (`livetv/youtube.go`). Dockerfile adds the standalone yt-dlp; the plugin runs `yt-dlp -U` daily. Never tried against real YouTube (the dev proxy blocks it).

## Roku channel (2026-10-27)

- **`apps/roku`**: a BrightScript/SceneGraph channel, separate from the React Native TV app. `MainScene` keeps a stack of screens (`BaseScreen`: `params` in, `navigate` out, `resumed` when the screen above closes, `closing` as it's removed). `ApiTask` does every request off the render thread and builds ContentNodes there (`build`: home, items, library, details, channels); `apiSendDetached` parks a task on the scene so the player's last progress report outlives the player. Fonts are bundled Inter (subset, OFL) because system font URIs differ between firmware and the simulator.
- **Flow:** Connect (LAN /24 scan on :8484 or typed address, same candidates as the TV app) → SignIn (pairing code via `/api/link/start` + poll) → household check like the TV app's `needsPicker` → Profiles (own 4–8 digit PIN pad) → Home (menu + RowsView/GridView/SearchView/LiveView) → Details → Player / LivePlayer.
- **Playback:** direct play when `roDeviceInfo.CanDecodeVideo/Audio` accept the container, codecs and audio (no HDR without HDR10, nothing taller than the TV mode), else `/api/files/{id}/transcode` (AAC, plus AC-3/E-AC-3 copy when the Roku can pass them through). Own controls so a seek past what's converted restarts the transcode with `replace`. Progress every 10 s, `client: "Roku"`.
- **Server:** `hlstoken.go` rewrites transcode and live playlists requested with `?token=` so every URI carries the token (Roku's Video node doesn't reliably send headers on segments), and `fromStart=1` adds `EXT-X-START:TIME-OFFSET=0`.
- **CI:** `roku-app` job runs BrighterScript and uploads `BingeBandit-Roku.zip`; releases attach `BingeBandit-Roku-vX.Y.Z.zip` with the manifest version from the tag.
- **Tested:** in the brs-engine simulator (`brs-node`, `brs-cli app.zip -e` with ECP keypresses and Ctrl+S screenshots) against a local server and fakeiptv: sign-in, PIN pad, Home, libraries, show/season pages, search, resume position saved on exit, Live TV join/flip/leave, sign out. The simulator can't decode video and its roUrlTransfer is synchronous, so playback and the network scan are untested. **Never run on a real Roku.**

## Second feature board (2026-10-26)

- **"database is locked" (SQLITE_BUSY / 517):** the DSN now has `_txlock=immediate`, so every `db.Begin()` takes the write lock up front; a deferred transaction that read first used to fail at once on its first write if anything else had written meanwhile (busy_timeout can't help that). Live TV sources are downloaded to a temp file (`livetv.spool`) before the import transaction starts, so the lock lasts as long as the parsing, not the download. `TestStreamImportWithConcurrentWrite` reproduces the 517.
- **Continue Watching:** one card per show (`cwKey`: by show name, so the same show in two libraries counts once); setting `continue_watching_days` (Settings → Library, 0 = until finished) drops titles not played for that long from Continue Watching and On Deck.
- **Hue Sync 1.2.0:** `/areas` lists every room and zone (rooms first); picking one syncs all its lights, ticked ones keep their picture zone, the rest follow the whole picture. Web groups rooms and zones.
- **Game Lights 1.1.0:** per-person choices in `config.json` `people` (by user id): off, Hue room or zone (each area gets its own `Runner`, so rooms run at once), a delay per way of watching (`Sources`, or their own), the other team's scores (red, their colours as `opp_colour`, or nothing) and their own team colours. Fans of both teams in a game each get effects from their side. Calibration: `POST /me/calibrate` sets the current source's delay to the time since the newest play of their games (within 5 min). User page `My Game Lights` (`/me`, listed for everyone) and `GET /me/state`, `PUT /me/settings`, `POST /me/test`. The TV Sports tab has a Game Lights button with the same choices (not colours).
- **TV app menu:** `devicePrefs.menuPosition` (`side` default, or `top`, Settings → This TV). The side rail is absolute over the screen, icons only (`src/icons.tsx`, react-native-svg) until focus enters, then names. The logo is focusable in both layouts and opens `AccountMenu` (Settings, Switch profile, Switch user = sign out).
- **TV long-press options:** `ItemMenuContext` (nav.tsx) / `ItemMenuHost` (itemMenu.tsx). Poster and wide cards open it on long press: play/resume, from the start, trailer, go to movie/show/season, mark watched, watchlist, add to playlist (or a new one), recommend; admins add to collections (new `GET`/`PUT /api/admin/items/{id}/collections`, names) and Manage (refresh metadata, find intro and credits).
- **TV More:** libraries plus the Sports and Live TV tabs with pin icons (pinning a tab = not in `prefs.hiddenTabs`), beside Continue Watching (started) and On Deck (next episodes) from `/api/home`. A hidden tab opened from More stays in the menu while open, like an unpinned library.
- **TV Settings tabs:** This TV, Menu, Profiles & PINs (profile PINs and "When the app opens" for the account; the settings passcode on, off, change, lock now), Server for admins (Continue Watching days, watched %, Live TV sources with their last error and Refresh, library scans). A 423 asks for the passcode (`useGuard`) and retries.
- **Watch Together on TV** (`src/together.tsx`): polls `GET /api/together/{id}?watching=1` every 1.5 s (no EventSource in RN); the server keeps polling viewers as members for 10 s after each look (`polled`). The player's Watch together button starts a room and invites; play, pause, seeks and next episode go out through `userPause`/`userSeek`/`playNext`. `TogetherInvites` pops invitations up every 10 s; Join opens the player with `together` and skips pre-roll.
- **Tested:** Go tests for the 517, Continue Watching dedupe and days, item collections, polled Watch Together members, Hue Sync areas, Game Lights people/rooms/calibration. **All TV app changes are typechecked and linted only; not tried on a device.** Game Lights and Hue Sync not tried on real Hue hardware.

## Sports tracker and Game Lights (2026-10-26)

- **ESPN** (`server/internal/sports`): the unofficial site API, no key. Leagues: NFL, college football (`groups=80`, every FBS game), NBA, men's and women's college basketball (`groups=50`), MLB, NHL, MLS. One scoreboard request per US Eastern day (ESPN rejects date ranges), cached 20 s while a game is on or about to start, 5 min for nearby days, 1 h otherwise; team lists for 12 h.
- **Team picks** (migration 39, `sports_teams`): per user or profile, copied from ESPN when picked. API: `GET /api/sports`, `PUT /api/sports/teams`, `GET /api/sports/{league}/teams`, `GET /api/sports/{league}/scores?date=` (docs/API.md → Sports). Live and upcoming games link to Live TV channels whose guide entry around kick-off names both teams (nickname or place, whole words).
- **Tracker** (`RunSports`): looks at the leagues people follow every 30 s while one of their games is on or within 30 min, else every 5 min, and right after someone changes teams. It sends `sports.game.start`, `sports.score`, `sports.game.end` (also webhooks) and `sports.game.live` (plugins only, every look) with the game, sport, scorer and fans. A game seen for the first time announces nothing, so restarts don't replay scores.
- **Alerts:** prefs `gameAlertMinutes` (default 15, 0 = off) and `notifyGameScores` (default off), under Account → Playback. Notifications of kind `sports`; `sports_alerts` keeps each one from being sent twice. The TV app pops up new sports notifications in a corner for 10 s over anything, polling every minute (`apps/tv/src/toasts.tsx`); they stay unread for the web bell.
- **Web:** `/sports` (nav: Sports) with live, coming up and results for your teams, a team picker by league with search, and every league's scores by day. **TV app:** a Sports tab with the same sections (OK on a game with a channel tunes to it) and a team picker. TV typechecked only.
- **Game Lights plugin** (`server/plugins/game-lights`, bundled, off by default), ported from Michael's GameLights app (C:\code\gamelights-src): pairs its own Hue bridge (v1 API, like Hue Sync; the bridge code is copied from hue-sync), flashes the ticked lights and restores them, checking they took it. Football reads ESPN's play feed every 4 s (core API, then the site summary) and classifies plays as in the original; other sports use `sports.score` (goals, runs; not basketball baskets) and every sport gets a win effect. After 3 failed play reads football falls back to score changes. Settings: whose teams, TV delay (default 25 s), brightness, reduced flashing; the page has effect toggles and tests, team colour overrides (VT's official colours built in), a sample game and Reset lights. Tested end to end against a fake ESPN and a fake bridge; never tried on real Hue lights or a live game.

## Chart lists and Kometa import (2026-10-26)

- **More list sources** (`server/internal/importer/charts.go`): a collection's `list_url` may now hold several links, one per line, and their titles are taken in turn without repeats. Besides MDBList and RSS: TMDB charts (`themoviedb.org/movie`, `/movie/top-rated`, `/trending/movie/week`, `/tv/…`, `/list/<id>`; uses the TMDB key), Trakt charts and lists (`trakt.tv/movies/trending`, `/shows/anticipated`, `/users/<u>/lists/<l>`; uses `trakt_client_id`), IMDb charts and lists (`imdb.com/chart/top`, `/chart/moviemeter`, `/list/ls…`; IMDb's GraphQL API like Kometa, falling back to the title links on the page), and Yamtrack lists (the Yamtrack host in Settings → Metadata, or any `/list/<x>` link; scraped like Kometa's `yamtrack_list`, signing in with `yamtrack_username`/`yamtrack_password` when the list is private). `?limit=N` keeps the first N. **None of these were reachable from the sandbox, so they're tested only against fakes of their responses.**
- **Matching:** `IndexItem`, `Record` and the `Matcher` now use IMDb ids (`items.imdb_id`) and shows' TVDB ids too.
- **List order:** `collection_items.from_list` now holds each title's place on the list (1 = first; 0 = added by hand), and the new `list` collection sort orders by it. No migration.
- **Plex-style sort titles:** `PLEXSORT` (`store/plexsort.go`) is a SQLite collation in the Unicode collation order Plex uses: punctuation with `_` before `+`, then digits, then letters ignoring case. `collectionOrder` uses it, so Kometa's `+_` / `++_` / `+++_` tiers come out as on Plex.
- **Kometa import** (`importer/kometa.go`, `POST /api/admin/libraries/{id}/kometa`, YAML body, `?dryRun=1`, `?recommended=1`): maps `mdblist_list`, `trakt_*`, `tmdb_*`, `imdb_chart`, `imdb_list`, `yamtrack_list`, `summary`, `sort_title`, `url_poster`/`url_background` (downloaded in the background, ThePosterDB links too), `collection_order`/`smart_label`/`sort_by` (to a collection sort), `collection_mode` and `visible_*`. Radarr/Sonarr and Kometa-only settings are ignored; other builders and `file_poster` are reported as skipped. Collections match by name, so importing again updates them. Imported rows are placed in sort-title order. Posters and list syncs run in the background.
- **Web:** *Add chart rows* (Recommended and Collections tabs) makes chart collections that show on Recommended and, optionally, Home. *Import Kometa file* on the Collections tab previews, then imports. The collection editor takes several lists with an *Add a chart* picker, a *List order* sort, and a poster link or upload (`PUT`/`POST /api/admin/collections/{id}/artwork`).

## Poster overlays and default collections (migration 38, Kometa-style)

- **Switches** (Admin → Overlays, all off by default): `overlays_enabled` plus one per built-in badge (`overlay_resolution`, `overlay_hdr`, `overlay_audio`, `overlay_ratings`); `default_collections_studios|decades|genres|awards` and `default_collections_minimum` (5).
- **Overlays are drawn at serve time**, never written to artwork. While on, movie and show `poster` URLs become `/api/images/poster/<id>-<tag>.jpg` (`store.overlayPosterURL`; the tag hashes the overlay settings and the poster ref, so clients refetch after a change, and the apps still add `?w=`). `api/overlays.go` works out the title's facts (`overlay.FactsFrom`: best file's resolution, dynamic range and best audio track across a movie's versions or all of a show's episodes; IMDb/RT from `external_ratings`; TMDB score), draws with `internal/overlay` (Go Bold from `golang.org/x/image`), and caches the JPEG in `<data>/cache/overlays/<width>/<id>-<key>.jpg`. Nothing to draw → 302 to the plain poster.
- **Your own overlays** (`poster_overlays` table): uploaded image, position (9 spots or `full` for frames), width %, margin %, condition (`always`, `res:4k|1080|720|sd`, `hdr`, `dv`, `hdr10`, `atmos`, `dtsx`, `truehd`, `dtshd`, `surround`, `label:<x>`, `imdb:<n>`, `rt:<n>`, `new:<days>`), movies/shows, one library or all, on/off, drawing order. To replace a built-in badge, turn it off and upload your own. `GET /api/admin/overlays/preview` draws a sample with unsaved badge choices.
- **Dolby Vision / Atmos:** `media.Probe` now records each video stream's `range` (sdr, hdr10, hlg, dv from the DOVI record or dvh1/dvhe tag) and each audio stream's ffprobe `profile` (names Atmos, DTS:X, DTS-HD MA) in the streams JSON. Older files show plain "HDR" until re-probed: movies are re-probed on first draw, and the "Detect Dolby Vision and Atmos" task does 1,000 files a run while overlays are on.
- **Default collections** (`importer/defaults.go`, task "Build default collections", daily and right after the switches change): smart collections per studio/network, genre (top 40 with ≥ minimum titles) and decade, plus award winners from Wikidata SPARQL (P166 "award received", matched by IMDb then TMDB id; Oscars, Golden Globes, BAFTA, Palme d'Or, Golden Lion, Golden Bear, Emmys). Marked with `collections.builtin` (`studio:<name>`, `decade:1980`, `genre:<name>`, `award:<id>`), created with Collection Mode `hide`, never replacing an admin's collection of the same name; turning a group off deletes only its own. If Wikidata fails, existing award collections stay. Franchises were already automatic (TMDB collections).
- **Tested:** overlay facts/conditions/drawing, API (URL rewrite, render + cache, upload, validation, edit, preview, plain fallback), default collections against a fake Wikidata, and in Chromium a library with an HDR10 4K and a 1080p 5.1 file plus an uploaded "new" ribbon. **Not tested** against the real Wikidata (award labels are guesses at its English labels) or with real Dolby Vision / Atmos files. The TV app gets the overlaid posters through the same URLs but wasn't tried.

## Board bug fixes (2026-10-26)

- **Hue Sync rooms per device (plugin 1.1.0):** each device can pick a Hue room or zone (web: Account → Hue Sync; TV: Settings → Hue lights), stored on the device and sent as `lightsArea` with progress, passed to plugins in `player.*` events. The plugin's `GET /areas` (any signed-in user, via `/api/plugins/hue-sync/areas`) lists rooms and zones holding chosen lights. The engine now runs one session per player: each drives the chosen lights in its area, so rooms sync at the same time; a new `player.play` takes over lights another player was using. Saved light states and pause/stop handling are per session. Tested with a fake bridge (two rooms at once); not tried on real Hue hardware.

- **Mark watched on seasons/shows:** `unwatchedCount` had `omitempty`, so a fully watched season never said 0 and every client kept offering *Mark watched* (and the web's watched tick on show cards never showed). The server now always sends it, and the web and TV clients treat a missing count as 0 for older servers.
- **Cinemask 429s:** the Cinemask Upgrades plugin retries a 429 after `Retry-After` (or 15 s, 30 s, 1 min, 2 min), then gives a clear error. *Search all* doubles its pause between titles (up to 2 min) while Cinemask or its indexers report rate limits, and eases back after.
- **TV app Back position:** screens under the top one stay mounted, hidden with `display: none` (Android makes them invisible, so they can't take focus), instead of unmounting. Back returns to the same scroll position, and `StackScreen` in `App.tsx` puts focus back on the last focused `Button`/`Focusable` (`FocusMemoryContext`, `requestTVFocus`). `useScreen()` gives `active` and `returns`: Home rows and Details reload on each return, and the Live TV guide's preview stream stops while a channel plays over it. **Typechecked and linted only; not tried on a device.**
- **Built-in profile pictures:** 134 animal pictures from Michael (`server/internal/avatars/images`, 256 px WebP, ~1.5 MB embedded), served without sign-in at `/api/avatars/<name>.webp`. Someone with no upload gets one picked from their user id (`avatars.ForUser`), so no migration. `POST /api/household/{id}/avatar/roll` stores another at random as `avatar:/<name>`; `DELETE …/avatar` now answers with the built-in picture. `User.avatarCustom` / `member.avatarCustom` tell uploads apart. The web picker shows *Another one* for built-in pictures and *Remove* for uploads.
- **Count as watched after:** `watched_percent` (Settings → Library, 50–100, default 90) replaces the fixed 90% in `SaveProgress`.
- **Collection sort and sort title (migration 36):** `collections.sort` (release date oldest/newest, title, added, rating) orders a collection's titles in `CollectionPage`; `collections.sort_title` orders collections among each other (`collectionOrder`), like Plex's "+_1A…" trick. Both are in the collection editor; the API leaves them alone when an older web app doesn't send them.
- **Shared playlists (migration 37):** `playlist_shares`. The owner picks people under *Share* on the playlist page, from their household and from people who take recommendations (Account → Sharing), so names aren't shown to anyone who didn't opt in. Shared people see it under *Shared with you* (`GET /api/playlists/shared`), open and play it (`ViewPlaylist` for `GET /playlists/{id}` and `/next`), but every change still goes through `ownPlaylist`. Items are filtered by the viewer's own library access. Checked in Chromium.
- **Local trailer files:** `library/extras.go`. Plex-style extras (`-trailer`, `-featurette`, `-behindthescenes`… and `.trailer`/`trailer.*`) no longer become movie versions or episodes. `LocalTrailers` finds a movie's `-trailer` files and its `Trailers/` folder; `/api/items/{id}` lists them as `trailers` and `/api/items/{id}/trailers/{n}` streams them to anyone who can see the movie. The web Trailer button plays the file first (YouTube as fallback); the TV app has a Trailer button (`trailer` route). Pre-roll trailers now also come from files (`PreRollTrailer.url`), so the TV app plays trailers before movies too. Web checked in Chromium; TV typechecked only.
- **TV app, unpinned libraries:** a *More* tab lists libraries the person can see but hasn't pinned; one opened from there joins the menu while it's open. Typechecked only.
- **TV app, pinning and hiding tabs:** *More* now lists every library with a Pin/Unpin button, and holding OK on a library (in the menu or under More; a long tap on phones) brings up Pin/Unpin in a `Modal`. A library tab has a *Pin to menu*/*Unpin from menu* chip. Pins save through `PUT /api/me/pins` with the whole order, as the web's editor does. Settings → Menu pins libraries and shows or hides the Sports, Live TV and More tabs, kept as `prefs.hiddenTabs` so they follow the profile; Home, Search and Settings can't be hidden, so there's always a way back. Typechecked and linted only; not tried on a device.

## Quality choice and laggy direct play

- **Server:** `Options.EncodeVideo` (`encodeVideo` in the transcode request) encodes H.264 that would otherwise be copied at its own size, so "Convert" gives a slow device something lighter. With it, the adaptive ladder is offered at original quality too.
- **Web player:** the settings menu lists *Original · Direct Play* (when the browser can play the file), *Original · Convert*, and each smaller size · Convert. With Auto quality on, direct play (or a copied stream) that drops over 20% of frames in two 5-second checks in a row switches to *Original · Convert* with a toast. Buffering three times a minute still steps the quality down.
- **TV app:** the player menu is now *Quality, audio & subtitles*, with the same choices. Before playing, `canDirectPlay` sends files ExoPlayer can't handle straight to the server: containers outside mp4/m4v/mov/mkv/webm/ts/m2ts/mts, video other than H.264/HEVC/VP9/AV1, and DTS/TrueHD audio (plus AC-3/E-AC-3 on phones). Picking an audio track it can't decode does the same. Direct play that buffers three times in a minute (not right after starting or seeking) switches to converting, with a notice.
- **Tested:** transcode arg test for `encodeVideo`; in Chromium the menu shows both originals and *Convert* sends `encodeVideo`. The TV app is typechecked and linted only.

## Faster libraries and collections

- **Why:** each page of a library ran one query that worked out every column, including episode and unwatched counts per show, for every title in the library before sorting and cutting to the page (SQLite fills result columns before it sorts). Every poster also carried the full cast and crew.
- **Server:** `Store.pagedItems` picks the page's ids first (filter, sort, `LIMIT/OFFSET` on bare rows, `ROW_NUMBER` keeps the order), then loads full rows for those ids only. `LibraryItems`, `RowItems` and the new `CollectionPage` use it, with `listSelect` (no `credits`; only `/api/items/{id}` sends them). `GET /api/collections/{id}` takes `offset`/`limit` and returns `total` (no limit: every title, for older apps). Collection rows on Recommended/Home and the collection shelves on a title's page load 24 titles instead of the whole collection. A page of a 1,500-show library went from ~115 ms to ~26 ms in a local benchmark.
- **Web:** the collection page loads 120 titles and the rest as you scroll. Libraries already paged and opened on Recommended.
- **TV app:** the library, collections and collection grids are `FlatList`s of fixed-height rows (`Grid` in `screens/Library.tsx`), so only rows near the screen are drawn; the A–Z bar scrolls with `scrollToIndex`. Opening a library from the menu always starts on Recommended (`forgetSection`; Back from details keeps the section), and the Recommended / Library / Collections chips switch on OK only, since moving down from the menu used to land on, and switch to, whichever chip was below. **Typechecked only; not tried on a device.**
- **Tested:** store test (slim, ordered library and collection pages), all Go tests, web build, TV typecheck and lint, and in Chromium a 300-title collection loading 120 then all 300 on scroll, and a library opening on Recommended.

## Matching across sources, merging copies (migration 35)

- **Ids:** movies and shows keep `tmdb_id`, `imdb_id` (new) and, for shows, `tvdb_id` from whichever source matched them (TMDB `/movie` `imdb_id`, `/tv` `external_ids`, TheTVDB `remoteIds`). `UpdateMetadata` keeps an id a source didn't report. The migration copies IMDb ids already fetched for ratings.
- **Where details come from:** `items.metadata_source`: `""` = TMDB, else TheTVDB; `"tvdb"` = TheTVDB (Fix Match, or picked automatically when TMDB has no match; `ShowTVDB` now reads this instead of `tvdb_id > 0`); `"none"` = left unmatched on purpose (Fix Match → *Leave unmatched*, `POST /api/admin/items/{id}/unmatch`), which refreshes skip.
- **Match order** (`metadata.Service.refresh`, `findTMDB`): TMDB id (Fix Match, earlier match, `{tmdb-…}` hint) → `{tvdb-…}` / `{imdb-tt…}` folder hints looked up with TMDB `/find` → TMDB search → for shows, TheTVDB (hint or top search hit). No match: the title keeps its folder name and `Item.unmatched` is true (admins see an Unmatched pill that opens Fix Match).
- **Merging** (`Store.MergeDuplicates`, after every metadata pass and after Fix Match): movies and shows of a library that share any id, with no id disagreeing (union-find), become one: movie files become versions, show episodes join (see the duplicate-shows section below). Movies merge only within one edition; unmatched titles never merge. `show_aliases` became `item_aliases`, and movie folders get aliases too. Fix Match answers `{id}` with the id that now holds the title, and the web app follows it.
- **Tested:** store tests (cross-id merge, disagreeing ids, editions, unmatched, aliases), a metadata test against fake TMDB/TheTVDB (IMDb hint, TheTVDB fallback, TheTVDB hint found on TMDB, merge, left unmatched), hint parsing, and Leave unmatched in Chromium. **Not tried** against the real TMDB/TheTVDB APIs.

## Audio-only playback, missing posters, duplicate shows

- **Picture missing (sound only):** the web player's direct-play check now asks the browser about the file's bit depth (10-bit H.264 never direct plays; 10-bit HEVC/VP9/AV1 ask with Main10 codec strings). Both players also check after starting: web, 2 s of direct play with no decoded frame (`getVideoPlaybackQuality`) or no video size; TV app, `onLoad` with no video size (ExoPlayer drops a video track it can't decode and plays the sound alone). Either way the server converts instead.
- **Posters:** TMDB image downloads (`metadata/images.go`) no longer use the request's context, so a browser cancelling a poster it scrolled past doesn't fail the others waiting on that image. Cards (`Art` in `Cards.tsx`) retry a failed image once after 3 s, then show the title.
- **Duplicate shows:** migration 32 adds `show_aliases`. After each metadata pass on a TV library, `Store.MergeDuplicateShows` folds shows that matched the same TMDB (then TVDB) id into the one with the most episodes: episodes join its seasons (two copies of an episode become two versions), and watch state, watchlist, collections, playlists, markers and history follow (`UPDATE OR IGNORE`). The duplicate folder's match key becomes an alias, so `episodeItem` adds later scans of it to the kept show. Library pages also sort with `i.id` as a tie-break, and the web Library tab drops repeats and stale pages.
- **Tested:** store tests for the merge (overlapping episodes, watch state, watchlist, alias, idempotent) and paging; in Chromium a 10-bit H.264 episode converts and an 8-bit one direct plays. The no-picture checks themselves weren't reproduced (Chromium here decodes what it says it can); the TV app change is typechecked only.

## Calmer Live TV failover, per-user Live TV (after v0.6.1)

- **Why:** fallback streams of a channel are often seconds apart in what they show, so every switch replays (or skips) a moment, and the slow-stream switching from #32 switched far too often. Jellyfin, which Michael found steady, never switches streams mid-play: it shares one tuner stream per channel and only fails when it ends.
- **Pace** (`livetv/pace.go`): a PCR discontinuity counts the wall time since the last PCR as played instead of zero, so a remuxed HLS upstream skipping ahead no longer reads as falling behind.
- **When to leave a slow stream** (`relay.go`): only once it's `buffer + switch time` behind real time (when viewers would really have buffered for the switch time, not just used up their cushion); never within the switch time of the last switch; and only if there's another stream that wasn't itself left for being slow in the last 10 minutes (`bench`, `canLeaveSlow`). Benched streams go to the back of each pass (`passOrder`). A stream that played 2 minutes resets the pass count, so a channel that fails now and then doesn't stop after three failures in a day. A player's buffering report switches only if the server sees it too (stream ≥ max(3, buffer/2) s behind, or no new segment for 8 s); an unmeasurable stream no longer trusts the player alone. Setting `live_switch_slow` (default true) turns slowness switching off entirely; failing or silent streams always switch.
- **HLS across a switch:** each new upstream gets a fresh HLS ffmpeg writing into the same folder with `append_list`, so segment numbers carry on and players keep the same URLs. The relay records the first segment of each output (`hlsBreaks`), and `Manager.Playlist` rewrites the served playlist with `EXT-X-DISCONTINUITY` before it and a correct `EXT-X-DISCONTINUITY-SEQUENCE` (ffmpeg's own tags on appended lists are wrong). Before, the new stream's bytes went into the same ffmpeg, whose timestamps then went backwards. Priming a new output with the stream's head skips bytes already queued for it (`tsChunk` offsets), so the start isn't fed twice, and a stale head (output started long after the stream) isn't used at all.
- **Per-user Live TV:** migration 29 adds `users.live_tv` (default 1; admins always have it). `PUT /api/admin/users/{id}/live-tv {on}` sets it for the account and its profiles, and turning it off disconnects their viewers (`Manager.KickUser`, not the DVR). All `/live/*` and `/dvr*` user routes sit behind `requireLiveTV` (403). Web hides Live TV and Recordings; the TV app checks `/api/me` on launch and hides the Live TV tab. Admin → Users has the toggle.
- **Tested:** unit tests for pace discontinuities, slow failover + benching, slowness switching off, playlist discontinuity rewriting; Chromium playing the fake flaky channel through its failover (0.3 s hiccup, no jump back); the toggle with a second account (403s, nav hidden, back on).


## Import from Dispatcharr (M4)

- **Server** (`livetv/dispatcharr.go`): signs in to Dispatcharr's REST API (`POST /api/accounts/token/` with an admin's login, or an `X-API-Key`) and reads M3U accounts, user agents, EPG sources, channel groups, logos, channels (`?include_streams=true`, so each channel's streams come in fallback order; older versions that list stream ids are read through `streams/by-ids/`) and EPG data. Handles plain lists and DRF pages (following `next` by path only). Channel overrides are applied through the `effective_*` fields when present.
- **Import** (background job, `DispatcharrJob`): each account becomes an M3U source (Xtream accounts become a `get.php?…&type=m3u_plus&output=ts` URL; connection limit, user agent string and refresh interval carry over). Its group filter is seeded before the first refresh (`SeedSourceGroups`) from Dispatcharr's enabled groups plus any group a channel uses; "new groups" follows `auto_enable_new_groups_live`. XMLTV sources with a URL become guides; uploaded files, Schedules Direct and dummy guides are skipped with a note. Then channels are created (`CreateChannel`) with number, name, group, logo, guide id (the matched EPG entry's tvg_id, else the channel's) and enabled = not hidden from output. Streams are matched in the new source by URL, then the Xtream stream id, then name within group, then a unique name. Channels without a found stream are skipped and listed. Taken numbers go to the end. Re-running skips sources with the same URL and channels with the same number and name. Logos uploaded to Dispatcharr point at its public logo cache.
- **Web:** Admin → Live TV → Sources → Import from Dispatcharr: address, login or API key, a preview of what's imported or skipped, then progress and notes. The login isn't stored.
- **`cmd/fakedispatcharr`** (`:9191`, admin / admin or key `fake-dispatcharr-key`) fronts `cmd/fakeiptv` with renumbered channels and a fallback stream.
- **Tested:** unit test with a fake Dispatcharr (Xtream account whose playlist URLs differ, plain account, paging, overrides, uploaded logo, hidden channel, missing stream, group seeding, re-import), and the whole flow in Chromium against fakedispatcharr + fakeiptv. **Not tested against a real Dispatcharr**: the field names come from its source (main branch, Oct 2026).

## Watch Together (M3)

- **Server** (`api/together.go`): rooms live in memory (`togetherRooms`), keyed by a random id that doubles as the invite link (`/play/<item>?together=<id>`). State is item, paused, position at a server time, a revision and who changed it. Every change and every join/leave is pushed to everyone over SSE (`/together/{id}/events`, 20 s keep-alive, `X-Accel-Buffering: no`). Anyone in the room can control it. Rooms with nobody connected for 10 minutes are dropped; invites expire after 6 hours. Only people who can see what is playing can open a room.
- **Invites:** to people who accept recommendations (the opt-in from the recommendations feature) and can see the item; they get a banner at the top of every page (`TogetherInvites`, polls every 20 s) with Join / Not now. Anyone else can be sent the link.
- **Web player:** people icon → start a watch party / invite / copy link / leave. Joining fetches the room first and starts at the room's position (or follows it to what it plays now). Local play/pause/seek (buttons, keys, seek bar, skip intro) are sent; remote ones are applied with a toast ("Sam paused"). Every 5 s a playing viewer more than 3 s off the room jumps back in step. Next episode / playlist item moves everyone (`state: {togetherMove}` on the navigation, announced on arrival). Pre-roll is skipped in watch parties.
- **Not in the TV app yet.** Tested: API test (access, invites, SSE updates, item change) and two Chromium browsers in one party (pause, seek while paused, the guest pressing play).

## Plugin catalogue (M9)

- **Server** (`plugins/catalog.go`): `Host.Catalogs` reads each catalogue URL (cached 10 min) and marks entries `installed` (in `<data>/plugins`, `Host.InstallDir`) or `bundled`, with `update` when the catalogue's version is newer than the folder's `plugin.json`. `Host.Install` downloads the zip for `GOOS-GOARCH`, requires and checks its SHA-256, unpacks it (no links, nothing outside the folder), checks the `plugin.json` id, then swaps it into `<data>/plugins/<id>` with the plugin stopped and calls `Sync`. `Host.Uninstall` only removes folders in `InstallDir`.
- **Catalogues:** BingeBandit's own is `releases/latest/download/plugin-catalog.json`, which the release workflow now builds from the plugin zips (and it stamps the tag into each zip's `plugin.json` as `version`; the Docker image does the same for the bundled copies). Until the next release there is none, and the page says so. Admins add others in setting `plugin_catalogs` (one URL per line, validated); installs only come from listed catalogues.
- **Web:** Admin → Plugins has a Catalogue section (Install / Update to x, Not built for this platform, Catalogues dialog, refresh) and Uninstall on installed rows.
- **Tested:** host test (install, bad checksum, unsafe zip paths, wrong id, other platform, enable, update while running, uninstall, bundled not uninstallable), API test (admin only, listed catalogues only, address validation, missing official catalogue), the release script's catalogue step on fake zips, and install → turn on → uninstall of Watchlist RSS in Chromium. The workflow itself only runs on a tag, so it hasn't run in CI.

## Notifications (M3)

- **Table** `notifications` (migration 25): one row per user, with `kind`, `title`, `body`, `link` (a web app path), `image` (poster), `read_at`. Pruned after 60 days. API: `GET /api/notifications`, `POST /api/notifications/read`, `DELETE /api/notifications`.
- **New episodes** (`api/notifications.go`, `Server.RunNotifications`, every 5 minutes from `main.go`): episodes with `added_at` past the watermark (internal setting `notify_episodes_since`; the first run only sets it to now, so an existing library doesn't notify) are grouped per show. Followers are users who started or finished any episode of the show or have it on their watchlist, who can still see it, haven't watched the new episode, and have pref `notifyEpisodes` on. One notification per show and user ("3 new episodes of …"; a single episode links to the episode).
- **Recordings:** `Server.Emit` turns `recording.completed` / `recording.failed` into a notification for the recording's owner when pref `notifyRecordings` is on.
- **Web:** a bell next to the user chip with an unread badge (polls every minute), list, mark all read, clear. Account → Playback has the two switches. The TV app doesn't show them yet.
- **Tested:** API test (followers, hidden library, pref off, watched episode skipped, no repeats, recordings, read/clear) and the bell in Chromium at desktop and phone widths.

## Passkeys (M6)

- **Library:** `github.com/go-webauthn/webauthn`. Table `passkeys` (migration 23): the library's credential JSON per row, credential id unique. User handle = the user id as 8 big-endian bytes; sign-in checks the handle matches the credential's owner.
- **Flows** (`api/passkeys.go`): begin/finish pairs keep the challenge in memory (`passkeyFlows`, 5 min, single use). The RP ID comes from the page's `Origin` only when that is the request's Host or the public URL, else from the request address; a flow begun on one address can't be finished on another.
- **Sign-in** is discoverable (no username): Login → *Sign in with a passkey*. It skips the 2FA code (the passkey already checks the device and the person) and counts failures in the login rate limiter. Profiles sign in through their account and can't add passkeys.
- **Account → Passkeys:** add (named), rename, remove; guarded by the settings passcode like other account changes. **Admin → Users:** *Remove passkeys* for a lost device.
- Browsers only offer passkeys on secure pages: the server's https:// address (or localhost). On plain http:// LAN addresses the button is hidden and the Account section says why.
- **Tested:** endpoint test (RP ID choice, single-use flows, own-only delete, admin remove) and the full add → sign in → removed-passkey-fails flow in Chromium with a virtual authenticator. **Not tried** with a real phone or password manager.
- **TV app:** unchanged (signs in by code).

## Trailers and intro video before movies (M3)

- **Admin:** Settings → Library → *Intro videos before movies* (`preroll_paths`): files or folders (searched two levels deep for mp4/m4v/mov/webm/mkv), one per line. One is picked at random, served by `GET /api/preroll/intro/{n}` (index into the sorted list).
- **Per user** (prefs `preRollTrailers` 0–5, default 0; `preRollIntro`, default on): `GET /api/items/{id}/preroll` returns trailers (YouTube keys from TMDB) of other movies the viewer can see and hasn't watched, a random pick of the 12 newest, never rated above the movie when it has a known US rating; plus the intro.
- **Web player** (`components/PreRoll.tsx`): only for movies started from the beginning and not from a playlist. Trailers use YouTube's iframe API (skipped if it can't load in 8 s or the video errors), then the intro; Skip / Skip all / Esc. Intros fall back to muted autoplay when the browser blocks sound.
- **TV app:** plays the intro only (no web view for YouTube), with a Start movie button. Typechecked only; **not tried on a device**.
- **Tested:** API test (ratings, watched, hidden libraries, episodes, intro file serving, pref off) and the intro → movie flow in Chromium. YouTube trailers were **not** tried (no YouTube access from the build machine).

## DVR keep-last-N and connection-limit conflicts (M4)

- **Keep newest N** per series rule (`recording_rules.keep_last`, migration 20; 0 = keep all). Set from the Recordings page (Series → Keep) or `PATCH /api/dvr/rules/{id}`. `Recorder.Prune` deletes the oldest *completed* recordings beyond N (row and file, then empty Show/Season folders) after each recording completes, when the setting changes, and every 5 minutes with the rules. Files outside the recordings folder (`Recorder.InFolder`) are never deleted, only the row.
- **Conflicts** (`livetv/conflicts.go`): upcoming recordings are walked in start order and each gets a connection from the first of its channel's providers with one free (same channel shares, custom channels need none). Ones that can't are returned with `conflict` in `/api/dvr` and the schedule response; the Recordings page shows a Conflict pill and the reason, the guide adds it to its note. Live viewers aren't counted.
- **Recordings come first** (admin setting `dvr_priority`, off by default): when a relay that the DVR is watching finds every provider at its limit, `Manager.preemptForRecording` stops the live-only relay on that provider with the fewest viewers. Relays with a recording are never bumped. Recording failures now say why (the relay's last error, e.g. connection limit), and empty failed files are removed.
- **Tested:** conflict rules, pruning (outside-folder files kept, keep 0), and preemption on and off against a fake provider. **Not tried** with a real provider.

## Logo library, your own channel categories, ThePosterDB search (after v0.6.0)

- **Logo library** (Admin → Live TV → **Logos**): upload PNG/JPEG/SVG/WebP/GIF logos (2 MB each, many at once), stored content-addressed in `<data>/logos/` and listed in `channel_logos` (migration 18).
  - A channel that uses one has `logo = "library:<file>"`; provider URLs work as before. Playlist refreshes never touch channel logos, so library logos stick.
  - "Give each to channels with the same name" and **Match all to channels** use `livetv.MatchName` (ignores case, HD/FHD/4K, `US:`/`CA |` prefixes and bracketed text; "TSN 1 FHD" = "TSN1.svg").
  - **Channels** on a logo picks exactly which channels use it; unticked ones and those of a deleted logo go back to their first stream's provider logo.
  - The channel and custom-channel editors have a logo field with a **Library** picker (and upload). `/api/admin/live/channels` adds `logoUrl` for display.
  - Logo URLs carry `?v=<hash of the logo>` so browsers show a new logo at once. The exported M3U points library logos at the server (`…/logo?v=…&token=…`).
- **My categories** (per viewer, so each profile has its own; `channel_categories` + `channel_category_members`): the guide's **My categories** button creates (optionally from your favourites), renames, orders, fills and deletes them; the hero has **Add to category…**. The filter lists Favorites, then your categories, then provider categories. `/api/live/guide` includes `categories`; `/api/live/categories` is the CRUD.
  - **TV app:** the guide now has a row of filter chips (All, ★ Favourites, your categories, provider categories) above the timeline. Typechecked only; **not tried on a device**.
- **ThePosterDB** (Edit → Artwork → Poster): **Search** reads ThePosterDB's search page and the top three title pages (`metadata/tpdb.go`), shows thumbnails (downscaled and cached in `<data>/cache/tpdb/`), and a click downloads `https://theposterdb.com/api/assets/<id>`. You can also paste a title/set/poster link into the search box, and "Use URL" accepts poster page links. **Open site** opens their search in a new tab.
  - Michael chose scraping knowing ThePosterDB's terms forbid it and that it can break or be blocked by Cloudflare. Their markup was never seen raw from here (the build machine can't reach them): the parser looks for any `/posters/<id>` link and any poster id (`data-poster-id`, `/api/assets/<id>`, `/poster/<id>`), and was tested only against a stand-in (`-tpdb-url` / `BB_TPDB_URL`). If real searches come back empty, the fix is in `parseSearch`/`parsePosters`.
  - Anonymous downloads of `/api/assets/<id>` are assumed to work (Kometa uses them); if ThePosterDB starts requiring a login, downloads will fail with the HTTP status.

## Cinemask Upgrades (M9 phase 3)

- **Plugin** `server/plugins/cinemask-upgrades`, bundled, off by default. [Cinemask](https://github.com/Spacegoose02/cinemask) is Michael's Sonarr/Radarr-style manager; "search Cinemask for upgrades" was read as: BingeBandit decides what's low quality from the probed files, and Cinemask does the searching and downloading.
  - `quality.go`: per movie/episode, the best file (resolution from width *or* height, so 1920×800 is 1080p; then bitrate) is listed when below the target resolution (setting, default 1080p), or optionally an old codec (MPEG-2/4, VC-1, WMV…), below a bitrate, or 4K without HDR. Unprobed files are skipped. Shows are grouped with their episodes.
  - `cinemask.go`: Cinemask's API with `X-Api-Key` (no CSRF needed for keys): `/api/auth/me`, `/api/monitored/lookup/{kind}/{tmdb}`, `/api/profiles`, `/api/monitored/{id}/search|releases|grab`, `PATCH /api/monitored/{id}` (profile), `POST /api/monitored` (with `search:false`). The key's owner must be a Cinemask manager/admin and the key full-access.
  - Cinemask only grabs what beats *its* view of the file and stays within the title's profile cutoff, so the page offers moving the title to a higher profile before searching. Manual search lists Cinemask's verdicts (accepted / rejections) with Grab.
  - `bulk.go`: **Search all** searches every listed title Cinemask monitors, one at a time, 5 s apart; titles not in Cinemask are skipped and counted. It can be stopped.
- **Server:** plugin API `GET /files?offset=&limit=` (`Store.FileSummaries`): every movie/episode file with item, show and quality fields.
- **Tested:** unit tests for the rules (4K copy next to a 720p one, widescreen 1080p, Xvid, unprobed) and the routes against a fake Cinemask (admin-only, at-cutoff message, profile change then grab, releases, grab, adding without searching, bad input, search all, bad key); the round-trip test covers `/files`. **Not tested:** a real Cinemask with real indexers.

## Hue Sync (M9 phase 2)

- **Plugin** `server/plugins/hue-sync`, bundled, off by default. Lights follow the picture, not a poster palette (Michael's call).
  - `sampler.go`: ffmpeg reads `/api/plugin/v1/files/{id}/direct` from the player's position at 4 fps, 48×27 px (`-skip_frame noref`, 2 threads), at most 45 s ahead of the playhead, keeping 30 s behind.
  - `colour.go`: zones (all, left, centre, right, top, bottom); saturation-weighted average → CIE xy clamped to gamut C; brightness from luminance, never fully off, capped by the setting.
  - `sync.go`: one followed player at a time. Position = last ping + elapsed; a jump > 2.5 s outside what's read restarts ffmpeg. Commands are skipped when nothing visibly changed, and the rate is capped at ~10 light commands a second. Light states are saved on start and restored on stop (or off/leave).
  - `bridge.go`: Hue v1 local API (pair with the link button, lights, rooms, state). Pairing and light→zone choices live in `bridge.json` in the plugin's data, set from its admin page.
- **Server:** new `player.play/pause/resume/stop` events (also for webhooks) and plugin-only `player.progress` on every ping (`emitPlayer` in `api/playback.go`). `Activity.OnExpire` plus a 15 s sweep send `player.stop` with `reason: timeout`. The plugin API gained `/files/{id}/direct`; managed plugins get `BINGEBANDIT_FFMPEG`/`_FFPROBE`.
- **Players:** web reports at once on seek, on first play and on resume; the TV app reports at once on pause and resume (seeks were already reported).
- **Tested:** unit tests for colours, the sampler on a red/green/blue video, and the engine against a fake bridge (filters, seek, restore). By hand: the real server and plugin with a fake bridge: red 0.3 s after play, green within 0.3 s of the cut, warm white on pause, the right colour on resume, yellow 0.2 s after a seek, and the lamp restored on stop. **Not tested:** a real Hue bridge, and CPU use on 4K HEVC (it decodes the whole picture; `-skip_frame noref` helps).

## Plugins (M9 phase 1)

- **Out-of-process plugins** (`internal/plugins`, protocol in `docs/PLUGINS.md`). A plugin is an HTTP service. Managed plugins sit in a plugins folder (bundled: `BB_PLUGINS_DIR`, `/usr/local/lib/bingebandit/plugins` in Docker; user: `<data>/plugins`) and BingeBandit runs them on loopback with their key in the environment. Remote plugins (own container) are added by address and paired over `POST /bingebandit/pair`.
- **Admin → Plugins** (`web/src/pages/admin/Plugins.tsx`): on/off, state and errors, a settings form from the manifest (password values never come back), Restart/Check, Add by address. Running plugins' pages show under **Account → Plugins**.
- **Plugin API** `/api/plugin/v1` (`api/plugins.go`): reuses the user API handlers as the server, or as a user under `/users/{uid}/…`. Events from `Server.Emit` go to webhooks and subscribed plugins; playback events now carry `userId`.
- **Proxy** `/api/plugins/{id}/*`: strips the user's cookie/Authorization/`?token=`, adds `X-BingeBandit-User-*`, blocks `/bingebandit/*`, drops `Set-Cookie`.
- **Watchlist RSS** (`server/plugins/watchlist-rss`, bundled, off by default): `feed.xml?token=<API key>` per person, subscribed feeds fill the watchlist (TMDB id from Letterboxd-style tags, else "Title (Year)"), optional removal of finished movies. Non-admins can't subscribe to loopback/private addresses. Built by `build.sh`/`build.ps1`, the Dockerfile and release zips.
- **Go kit** `server/pkg/bbplugin` for writing plugins.
- **Tested:** `go test` covers pairing, re-pairing and unpairing, the proxy (credential stripping, `/bingebandit` blocking), events, settings, the plugin API as a user, disabling, and a managed plugin's lifecycle. By hand: the real server with the bundled plugin, subscribing to a local feed (2 of 3 matched), the feed with an API key, the non-admin private-address block, and the admin page, settings modal and plugin page in Chromium. **Not tested:** the Docker image build and release zips (no Docker here), Windows.
- **Trust model:** plugin pages are served on BingeBandit's origin, so a plugin's JavaScript runs with the viewer's session. Fine for trusted plugins; a sandboxed origin for plugin pages would be the fix if third-party plugins appear.

## Profiles (after v0.6.0)

- **Model:** a profile is a `users` row with `owner_id` (and `profile_name`), migration 16. All per-user data (watch state, plays, watchlist, playlists, prefs, Trakt) already keys on the user id, so it stays apart with no other changes.
  - The stored username is `Name (owner)` (`store.ProfileUsername`), which stays unique and reads well in admin stats.
  - Profiles have no password, so they can't sign in directly. Password change and 2FA refuse them, and admins can't set one for them.
- **API:** `api/household.go`. `GET/POST /api/household`, and `PUT/DELETE /api/household/{id}`, `PUT …/pin` and `POST …/switch`. Only the account manages profiles, and it's behind the settings passcode.
  - Switching ends the current session and starts one for the member on the same device name. A member with a PIN needs it, rate limited, and a wrong PIN answers 403.
  - A profile's PIN is its `passcode_hash`. The account's PIN is its settings passcode.
  - Library limits for a profile are capped at what the account sees.
  - **Pictures** (migration 27, `users.avatar`, an artwork ref stored by `metadata.Artwork`): `PUT/DELETE /api/household/{id}/avatar`. The account can set anyone's picture, and a profile can set its own. The web crops pictures to a 512 px square JPEG before uploading (`components/Avatar.tsx`).
  - **Opening as a member** (`users.auto_profile_id` on the account, 0 = ask): `PUT /api/household/autoselect {memberId}`. `GET /api/household` marks that member `auto`. Clients then skip the picker, switching to that member unless it has a PIN, in which case the picker shows.
- **Web:** `pages/WhoIsWatching.tsx` at `/who` appears whenever the app opens in a new tab or after signing in (the `sessionStorage` "picked" flag in `profilePick.ts`; a reload doesn't ask again), keeping the link that was opened. It also appears from the user menu → Switch profile, and that never auto-selects. Account → Profiles adds, edits (name, libraries, PIN) and deletes profiles. Admin → Users marks profiles and hides Reset password for them.
- **TV app:** `screens/Profiles.tsx` with a remote-friendly PIN pad. It shows on every app start and after sign-in when the account has profiles (`needsPicker`, which also applies the account's "open as"), and from Settings → Switch profile. It shows the profiles' pictures. **Not yet tried on a device**: the onn box and the S24+ still need a run-through. It type-checks and lints.

## Live TV: switch streams when viewers would be buffering

- The old stall timeout only caught a stream that sent **nothing**. A stream that trickles in slower than real time never tripped it, but every player drained its buffer.
- `livetv/pace.go` reads the PCR clock in the upstream's MPEG-TS. It reports how far the stream has fallen behind the furthest ahead it has been, so a provider's initial burst doesn't count. It handles wraps of the 33-bit clock and splices.
- `pull`'s watchdog moves to the next stream when the stream falls behind by more than the switch time (`live_stall_seconds`, default 12, now labelled "Switch streams after buffering for"). This only applies when the channel has a fallback stream; a channel with one stream keeps playing it.
- Players (web and TV) that buffer for the switch time call `POST /live/viewers/{id}/buffering`. The relay switches only if the server sees the problem too:
  - the stream is ≥3 s behind real time
  - or the HLS output has made no segment for over 8 s
  - or the stream can't be measured (no PCR)

  Otherwise it logs that the viewer's own network is the likely cause. There's at most one switch per switch time.
- `/watch` returns `stallSecs`. Activity shows "N s behind real time" for a stream that is falling behind.
- Tests: `TestPace`, `TestSlowStreamFailsOver` (a 30%-speed stream fails over in about 6 s with a 3 s setting), `TestBufferingReport`.

## Live TV settings, logs and playlist links

- **Settings → Live TV playback** also has:
  - **Buffer** (`live_buffer_seconds`, default 10). `/watch` returns it as `bufferSecs`; the web player sets `liveSyncDurationCount` from it and the TV app sets ExoPlayer's `targetOffsetMs`.
  - **Switch streams after no data for** (`live_stall_seconds`, default 12). This replaced the `stallTimeout` constant.
  - **Proxy for IPTV providers** (`live_proxy`): http, https or socks5. `livetv/proxy.go` applies it to streams, M3U/XMLTV refreshes and logos, and passes `-http_proxy` to ffmpeg for HLS upstreams (http proxies only). Empty falls back to `HTTP(S)_PROXY`.
- **Live TV logs:** Admin → Logs has an Everything / Live TV switch (`/admin/logs?only=live`). It matches `live tv:`, `custom channel` and `dvr:` lines. Live TV → Activity links to it. Failed joins are now logged with the user and channel.
- **Playlist URLs for everyone:** `components/IPTVLinks.tsx` makes a key and shows the M3U and XMLTV URLs. It's on Account → API keys for anyone who has channels, and on Admin → Live TV → Sources. The playlist only lists that user's channels.

## Settings passcode (after v0.6.0)

- **Account → Settings passcode** sets a 4–8 digit passcode (bcrypt in `users.passcode_hash`, migration 15).
- **What it locks:** `settingsLock` middleware (`api/passcode.go`). For a user with a passcode, a session answers 423 on `/api/admin/*`, non-GET `/api/me/*` and `/api/link/approve` until unlocked. Watching, reading prefs, progress and the watchlist are never locked, so the TV app is unaffected. API keys are never locked.
- **Unlocking:** `POST /api/me/passcode/unlock` with the passcode, or the account password as a fallback. It's rate limited like logins, and a wrong one answers 403, not 401, so the web UI doesn't sign out. The unlock is per session, in memory, and slides to 15 minutes after the last guarded request. A restart or "Lock now" locks it again.
- **Web:** `PasscodeGate` wraps `/account`, `/link` and every `/admin` page. `PasscodePrompt` (in `Layout`) pops up when a locked action happens elsewhere, such as editing an item. `api.ts` fires `bb:locked` on any 423.
- **Admin → Users → Clear passcode** is for someone who forgot theirs.
- **Next:** profiles under one account, with this passcode as the profile PIN.

## Live TV playback reliability (after v0.6.0)

- **Flipping channels on a provider with a connection limit:** a relay with no viewers left (someone flipped away, or a guide preview moved on) used to hold its upstream for 15 s, so the next channel on that provider failed with "all providers busy". `reclaimIdle` now stops such a relay when another channel needs the slot, and an all-busy channel waits two passes (~6 s) before giving up.
- **`POST /watch` waits for the first segments** (`HLSFile` on `live.m3u8`, up to 25 s) before answering. ExoPlayer used to time out on a playlist request that hung while the upstream connected, which showed up on the TV as timeouts when switching fast. Clients give the join 40 s (`apps/tv/src/live.ts`).
- **Codec probing** tries at 256 KB, then 1 MB, so low-bitrate channels start in under a second instead of waiting 8 s.
- **Pause and rewind:** Settings → Live TV playback → "Pause and rewind window" (`live_timeshift_minutes`, default 15) sets `-hls_list_size`. A paused player stops polling, so clients ping `POST /live/viewers/{id}/ping` every 10 s.
  - Web: Space/K pause, ←/→ −10 s/+30 s, L or the badge jumps to live. `maxLiveSyncPlaybackRate` was removed so a rewound player doesn't speed back to live.
  - TV: OK/play-pause pauses, ◀/▶ −10 s/+30 s, the info bar shows "−m:ss" behind live. Phones get −10s / Pause / +30s / Live buttons.
- **TV buffer:** ExoPlayer plays ~10 s behind live, waits for 5 s after a stall, at 1× speed (`LIVE_BUFFER`). Channel up/down updates the info bar at once and tunes 700 ms after the last press.
- **Errors retry on their own** (web and TV): 1.5 s, 3 s, 4.5 s, then every 15 s with the message shown; the message clears when playback resumes. The web box also has Dismiss.
- **Hardware encoding for live TV** (`livetv/encoder.go`, shared by relays and custom channels):
  - Quick Sync on Linux now sets up the device explicitly (`-init_hw_device vaapi=va:/dev/dri/renderD128 -init_hw_device qsv=qs@va`, `hwupload`). VAAPI and VideoToolbox are now supported for live TV (they fell through to libx264 before).
  - An encoder that fails within 20 s is skipped for 30 minutes (CPU instead) and Live TV → Activity shows why. Settings → Transcoding → **Test encoder** runs a 2 s 1080i encode the way live TV does and shows ffmpeg's error.
  - H.264 channels are passed through without converting, so they never use the GPU; Activity says so.
- **Tested** against fakeiptv with a 1-connection source: flip to another channel reclaimed the idle connection (0.4 s join), 28 segments kept with a 1-minute window, Quick Sync failure here (no GPU) fell back to the CPU, web pause/go-live in Chromium. **Not tested:** the TV app on a device, and Quick Sync on real Intel hardware.
- **Not changed:** the on-demand transcoder (`stream/transcode.go`) still starts `h264_qsv` without explicit device setup.

## Latest: phones, kid lock, Unraid (M8 phase 3)

- **One APK for TVs and phones.** `isPhone = !Platform.isTV` in `theme.ts` switches layouts.
  - Phones size from their short side: about 750 canvas units across, so text keeps its size when the phone turns.
  - Phones use smaller gutters and their own font sizes. `units()` turns dp back into canvas units (the guide uses it).
- **Edge-to-edge:** Android 15 draws behind the system bars. `react-native-safe-area-context` (5.10) pads the app on phones (`Insets` in `App.tsx`). The players stay full-screen and pad only their controls.
- **Bold text:** Android's "Bold text" setting (`fontWeightAdjustment`) draws text bolder than RN measured it, which cut the last word off many labels. `MainActivity.attachBaseContext` sets it to 0 so measuring and drawing agree; the font size setting still applies. Found on a Galaxy S24+ with bold text on.
- **Native:** `ScreenModule` (`Screen.setLandscape`) turns the phone sideways while a player is open (`useLandscape` in `src/screen.ts`). `BingeBanditPackage` (renamed from `AppUpdaterPackage`) registers it alongside `AppUpdater`.
- **Screens on phones:**
  - **Home:** a bottom tab bar.
  - **Details:** a ‹ Back link and wrapping buttons.
  - **Sign-in:** password first ("Use a code instead"); no QR code.
  - **Player:** tap to show or hide the controls, a back arrow, and `SeekBar` (drag, seek on release; measured in page coordinates because Android ignores `pointerEvents` on Text).
  - **Live player:** swipe up or down to change channel.
  - **Guide:** geometry from the window width (two hours on a TV or a sideways phone, one hour upright). Drag sideways to move through time. Tap a block to select it; tap again, or tap the preview, to watch. A compact header when sideways.
  - **Search:** `disableFullscreenUI`, so the landscape keyboard doesn't hide results.
  - **Settings:** scrolls.
  - **Touch feedback:** buttons dim when pressed and don't show the TV focus ring.
- **Kid lock** (`src/lock.tsx`, `useLock`), in both players on phones:
  - **Lock** asks for a 2-digit PIN chosen on the spot. While locked, taps only show "Locked · tap here to unlock" and Back is swallowed.
  - The same PIN unlocks; a wrong one clears with a message. Nothing is stored.
- **Unraid:** `unraid/bingebandit.xml` is a dockerMan template. Add the repo under Docker → Template repositories, or use it from the template URL. It has appdata, read-only media, and optional recordings, RAM transcode and `/dev/dri`. `docs/UNRAID.md` walks through install, first run, Intel/AMD/NVIDIA transcoding, apps, remote access, updates, backups and troubleshooting.
- **Tested on a Galaxy S24+** (Android 15, 3-button navigation, font scale 1.15, bold text) over wireless debugging:
  - discovery, code sign-in, Home, Details, playback with landscape
  - controls, seek drag, the kid lock (wrong and right PIN)
  - guide in both orientations: swipe, tap to select, preview tap to watch
  - live channel swipe and the info bar, Search, Settings
- **Not tested on a device:** the TV after these changes. TV code paths are unchanged apart from shared components; the guide geometry gives the same 2-hour/1500-unit grid on a 1080p TV.
- **Dev gotcha:** Fast Refresh while the lock keypad is open can leave touches dead until the app restarts. This is a development-only effect.

## TV app phase 2 (M8 phase 2)

- **Player** (`screens/Player.tsx`):
  - Controls have three states: hidden, `seek` (◀/▶ jumps, a progress bar and hint) and `full`. OK, ▲ or ▼ opens the full bar: ⟲ 10s, Pause/Play, 30s ⟳, Next episode (episodes only, from `/api/items/{id}/next`), Audio & subtitles. The bar hides after 6 s unless paused.
  - Back closes the menu, then the bar, then leaves. Screens take Back first through `backInterceptor` in `nav.tsx`.
  - Tracks: `src/tracks.ts` ports the web player's `pickTracks` and the viewer's prefs (`/api/me/prefs`).
    - Direct play picks audio with `selectedAudioTrack` (ExoPlayer's track position).
    - Text subtitles are fetched as VTT and drawn by the app.
    - Image subtitles (PGS/VobSub) force a transcode with burn-in.
- **Guide** (`screens/LiveGuide.tsx`), the Live TV tab:
  - A header shows the focused program's details and a muted live preview. The preview joins the channel's stream 1.2 s after focus settles and leaves on change.
  - Then a half-hour ruler with a teal "now" line, and channel rows whose program blocks are sized by duration. Each row uses a FlatList, so big channel lists stay light.
  - Android only moves focus to blocks it can see. So when the focused block touches an edge of the visible two hours, the timeline slides to show what's next. `TVFocusGuideView trapFocusLeft/Right` keeps Left/Right inside the grid.
  - Channel logos that are SVG go through react-native-svg, because Android's Image can't draw them.
- **Search** tab (`/api/search`, 350 ms debounce) and **watchlist** add/remove on Details.
- **In-app updates:**
  - Server: `GET /api/apps/android` (`api/apps.go`) reads the newest GitHub release of `-update-repo` (default `Spacegoose02/BingeBandit`; empty disables it), caches it for an hour, and returns the `BingeBandit-TV-*.apk` asset with a versionCode computed like Gradle's.
  - App: the native `AppUpdater` module downloads the APK to the cache and opens Android's installer via a FileProvider. The first time, it sends the viewer to "Install unknown apps" for BingeBandit.
  - Settings shows the version, and a dot appears on the tab when an update exists.
- **Home** remembers the last tab, so Back from the player or details returns to Live TV or Search, not Home.
- **Tested on the onn box:** the control bar, pause and seek, the audio/subtitle menu, subtitle cues, Search, the guide (focus, sliding, preview, tune and Back), and the update offer up to the "Install unknown apps" permission page. The final install step wasn't run, because that permission is the TV owner's to grant.

## The Android TV / Fire TV app (M8 phase 1)

- **`apps/tv`:** react-native-tvos 0.83 (RN 0.83, New Architecture, Hermes), React 19, TypeScript. The libraries are react-native-video 6 (ExoPlayer), AsyncStorage 3, NetInfo, react-native-svg and react-native-qrcode-svg. `.npmrc` sets `legacy-peer-deps`, because the tvos fork's pre-release version doesn't satisfy plain `react-native` peer ranges.
- **Screens:**
  - **Connect:** scans the device's /24 on port 8484 for `/api/health` with `product: BingeBandit` (the server now names itself there), or takes a typed address.
  - **SignIn:** the link code plus a QR code of `linkUrl`, polling `/api/link/poll`. A password option handles 2FA codes.
  - **Home:** a top menu (Home / Live TV / Settings), a hero that follows focus, Continue Watching and the home rows.
  - **Details:** movie, show, season or episode. Play, resume and mark watched; seasons and episodes.
  - **Player:** direct play first, falling back to `/transcode` HLS on error. Seeking in transcode mode restarts the transcode at the target. ◀ 10 s, ▶ 30 s. Reports `/api/progress` every 10 s and on exit, with `played` counted like the web player. Plays the next episode at the end.
  - **LivePlayer:** joins `/live/channels/{id}/watch`; ▲/▼ changes channel; leaves the viewer on exit.
- **Navigation:** a tiny stack in `App.tsx`; the remote's Back pops it. The session (server, token, user) is kept in AsyncStorage and checked against `/api/me` at start.
- **Android:**
  - Package `com.bingebandit.tv`. Cleartext http is allowed, since home servers are usually plain http.
  - Icons and the TV banner are generated from `Brand/`.
  - Version from `-PbbVersion` (versionCode = major·10000 + minor·100 + patch).
  - Release signing via `BB_KEYSTORE*` env vars, falling back to the debug key.
- **CI:** type-check and lint on every push; the release workflow builds a signed APK (when the `ANDROID_KEYSTORE*` secrets exist) and attaches `BingeBandit-TV-<tag>.apk`.
- **Toolchain on this PC:**
  - JDK 17 at `%LOCALAPPDATA%\Programs\jdk-17`. Android SDK at `C:\Users\spacegoose\Android\Sdk`: cmdline-tools 19, because the newest `android.exe` needs a missing VC++ DLL; platform 36, build-tools 35/36, NDK 27.1, CMake 3.22.
  - The SDK is outside AppData on purpose. The Claude desktop app is a packaged (MSIX) app, so new folders it creates under `AppData\Local` are redirected into its private storage and invisible to the user's own terminal.
  - `JAVA_HOME`, `ANDROID_HOME` and `JAVA_TOOL_OPTIONS=-Djdk.net.unixdomain.tmpdir=C:\Users\spacegoose\.jdk-uds` are set for the user. Without the last one, Gradle fails with "Unable to establish loopback connection", because AppData paths are redirected for the packaged app this runs in.
- **Tested on a real TV** (onn. 4K Streaming Box, Google TV 14, Amlogic, 32-bit ARM only) over network adb (`adb connect <ip>:5555`):
  - **What works:**
    - sign-in by code (approved via `/api/link/approve`)
    - Home rows with artwork
    - details, season and episode lists, remote navigation and Back
    - direct play of H.264 and of HEVC Main 4:2:0 MKV, with AC3 5.1 passed through to the TV's Dolby decoder
    - converted HLS playback
    - pause, seek and the overlay
    - Live TV list and channel up/down
    - Trakt auto-sync triggered from TV playback
  - **Fixed during device testing:**
    - Release builds blocked cleartext. RN's Gradle plugin forces the `usesCleartextTraffic` placeholder to false for release, so the manifest now says `true` literally.
    - The layout was designed in px but RN uses dp (1080p TV = 960×540 dp). `theme.ts` now has `px()`/`scaled()`, and style sheets are authored on a 1920×1080 canvas.
    - The TV event handler only ever gets key-*up* events on Android (key-down drives native focus), so players act on `eventKeyAction !== 0`.
    - Players need a focused view, or keys never reach JS: there's a full-screen `hasTVPreferredFocus` Pressable.
    - Amlogic rejects 4:4:4 HEVC frame by frame without an error, so direct play now falls back to transcoding after 8 s without progress.
    - ExoPlayer's LIVE badge on event-type HLS is hidden via `controlsStyles.hideLiveBadge`.
  - **Gotchas:**
    - `adb screencap` doesn't capture HEVC video, which the Amlogic chip draws on a hardware layer. A blank screenshot there isn't a bug; look at the TV.
    - The test server PC (192.168.86.x) and the TV (192.168.80.x) are on different subnets, so auto-discovery can't find it. Typing the address works, and the app says so.
    - Unconfirmed: the converted output of the odd 4:4:4 test file showed nothing in screenshots, probably the same capture limit.
  - **Development loop:**
    - Debug APK (`assembleDebug -PreactNativeArchitectures=armeabi-v7a`), `npx react-native start`, then `adb reverse tcp:8081 tcp:8081`. Changes reload on the TV in seconds; JS logs show in `adb logcat` under `ReactNativeJS`.
    - Fast Refresh while the player is open can raise "Rendered more hooks…". Restart the app; it's not a real bug.
- **Not done yet:** phone layouts.

## Trakt sync, history downloads, Plex-style webhooks (M7 phase 2)

- **Trakt** (`internal/trakt`):
  - An admin adds the server's Trakt app keys under Settings → Trakt (redirect URI `urn:ietf:wg:oauth:2.0:oob`).
  - Each person uses Account → Trakt → **Connect**, which uses Trakt's device-code flow: they enter the code at trakt.tv/activate. Tokens are kept per user in `trakt_accounts` and refreshed within an hour of expiry.
  - **Import my Trakt history** runs the M7 importer synchronously for that one user, with the source user "trakt". Imported plays are marked as already on Trakt. A Trakt viewing that matches a local play (±15 min) marks the local one as sent.
  - **Send N plays to Trakt** pushes every finished play not yet sent (`plays.trakt_at = 0`), in batches of 100. Plays without a TMDB id go by title and year. Plays Trakt doesn't recognise are still marked, so they aren't retried forever.
  - **Automatic sync** (on by default) pushes plays started after connecting, in the background, when a play finishes. Earlier history is only sent on request.
  - Disconnect revokes the token. Trakt errors never answer 401, so they can't sign the person out of BingeBandit.
- **Downloads** (Account → Download my history):
  - `/api/me/export/history.csv`: every play, with type, title, year, TMDB id, season/episode, episode title and completed.
  - `/api/me/export/letterboxd.csv`: finished movies in Letterboxd's import format, with repeat viewings marked as rewatches.
- **Plex-style webhooks:** a webhook's Format can be "Plex-style". It sends multipart `payload` JSON with `media.play` / `media.stop` / `media.scrobble`, plus Account, Player and Metadata (movie `tmdb://` Guid; for episodes the show title, season and episode, and the show's TMDB id as `grandparentGuid`). This is for Yamtrack and other apps that accept Plex webhooks; recording events are skipped. Playback event data now includes `tmdbId`, `showTmdbId`, `season`, `episode` and `episodeTitle`.
- **Dev:**
  - `cmd/fakeplex` now also fakes Trakt on `:8282` (client `fake-trakt-client` / `fake-trakt-secret`; `/activate` approves the code; `/_received` shows what was sent).
  - Run BingeBandit with `-trakt-api http://localhost:8282` (or `BB_TRAKT_API`). `.claude/launch.json` does.
- **Tested in the browser against the fake:**
  - connecting with a code
  - import (2 of 3 matched; re-import added nothing)
  - sending 6 plays (correct grouping by show and season)
  - automatic sync after a real 35-second play, sent once
  - both CSV downloads
  - a Plex-style test webhook
  - Unit tests cover the Trakt body grouping and the Plex payload.
- **Not tested:**
  - Real Trakt. Check token refresh: Trakt's token lifetime has changed before.
  - Whether real Yamtrack accepts the Plex-style payload. It identifies the user by Plex username (Account.title, which is the BingeBandit username here), and its episode matching may expect episode-level GUIDs, which we don't have. Yamtrack can also import from Trakt, which is the safer route.
- Migration 13 (`trakt_accounts`, `plays.trakt_at`, `webhooks.format`).

## Importing from Plex and Tautulli (M7 phase 1)

- **Server → Import** (`/admin/import`) has three steps:
  1. Pick a source: Plex (address + token), Tautulli (address + API key) or a Tautulli database file path. Credentials are used for the run only, never saved.
  2. Map each source user to a BingeBandit user, "Create a new user" (passwordless; link a TV for them or set a password), or Skip. Same-name users are suggested.
  3. Choose history / watched marks (+ resume points for Plex) / collections (Plex), then **Preview** (dry run) or **Import**.
  - It runs in the background with progress, Cancel, result tiles, and the most-watched titles that didn't match.
- **`internal/importer`:**
  - **Sources:** `Plex` reads `/status/sessions/history/all` (all accounts), item details for TMDB guids and file names (cached per rating key), `/library/sections/*/all` for the owner's viewCount and viewOffset, and collections. `TautulliAPI` uses `get_users` and `get_history` (grouping off). `TautulliDB` reads `session_history` + `_metadata` + `users` read-only after checking the columns exist.
  - **Matcher:** file base name, then TMDB id, then title + year (±1, accents and articles folded, trailing "(US)" dropped). Episodes match by show, then season/episode.
  - **Runner:** one import at a time.
- **No duplicates:**
  - Plays carry `import_key` (migration 12, unique per user).
  - `ImportPlay` also skips a play of the same item by the same user that ended within 15 minutes, so Plex + Tautulli + BingeBandit's own history don't double up.
  - Watched marks never un-watch. Resume points only replace older local progress.
  - Collections are found or created by name (case-insensitive) and only gain items.
  - History rows use this library's titles, with mode "imported".
- **`cmd/fakeplex`:** a fake Plex (`:32400`, token `fake-plex-token`) and Tautulli (`:8181`, key `fake-tautulli-key`) with history matching the test media. `-makedb path` writes a `tautulli.db`.
- **Tested in the browser:**
  - Preview, then a Plex import creating a user, collections and a resume point.
  - Re-running Plex added nothing new.
  - Tautulli API added only its one extra (partly watched) play and skipped a music track.
  - The database file added nothing new.
  - Errors for a bad token, bad key and missing file.
  - Unit tests cover the matcher and the store's duplicate and state rules.
- **Not tested** against a real Plex or Tautulli. Watch for: Plex servers that need `https://…plex.direct` addresses; very large histories (Plex history is paged at 200, and items are looked up one at a time, so expect minutes); Tautulli database schema differences in older versions. Plex only reports watched marks for the token's owner; other users' marks come only from their history.
- Migration 12.

## Single sign-on, network settings, API docs (M6 phase 2)

- **Single sign-on (OIDC):** Settings → Single sign-on adds providers (name, issuer, client ID/secret, extra scopes). Saving checks the issuer's discovery document. The panel shows the redirect URL to register (`<public URL>/api/auth/oidc/callback`). The login page gets a "Continue with …" button per provider. Account → Linked accounts links and unlinks.
  - Auth code flow with PKCE and nonce. Pending sign-ins are kept in memory for 10 minutes and each state works once. Discovery is cached for an hour.
  - An unknown provider account is refused with "link it under Account", unless the provider has **Create an account for anyone** turned on. That makes a passwordless, non-admin user named from `preferred_username` or the email (all libraries). Such users can **Set a password** under Account, and can't unlink their only sign-in method until they do.
  - Signing in through a provider skips BingeBandit's own 2FA (the provider authenticates). The 2FA panel is hidden for passwordless accounts.
  - Removing a provider deletes its links. Users who only had that provider need an admin to set a password.
- **Network settings** (Settings → Network): public URL, remote access on/off (outside clients get a 403 page), extra LAN networks, and trusted reverse proxies.
  - Trusted proxies' `X-Forwarded-For` (rightmost untrusted hop), `X-Real-IP`, `-Proto` and `-Host` are honoured. This feeds the client IP (rate limits, sessions, "local" on Now Playing), https cookies and generated URLs.
  - Turning remote access off is refused when the admin is themselves outside.
  - The policy is cached and reloaded when settings are saved.
- **`docs/API.md`:** authentication, the TV code flow, the user-facing endpoints, playback reporting, Live TV, and the webhook format and signature.
- **`cmd/fakeoidc`:** a click-to-sign-in OIDC provider for development (`dist\fakeoidc.exe -addr :8686`, client `bingebandit` / `dev-secret`, people alice, bob, carol).
- **Tested in the browser against fakeoidc:**
  - linking, signing in through the provider, and an unlinked account refused
  - account creation, the cancelled-at-provider error, a bogus or replayed state, the unlink guard, and setting a password then signing in with it
  - network settings validation, forwarded headers from a trusted proxy, the remote-access block, and the public URL in the redirect
  - Unit tests cover forwarded headers, the remote-access block, CIDR parsing and the `next` redirect guard.
- **Not tested:** real Google / TinyAuth / Authelia. Things to check: Google needs an https redirect URL unless it's localhost, and some providers send `email_verified` as a string. **Passkeys are still to do.** The browser pane here has no authenticator to test WebAuthn with.
- Migration 11.

## Users and security, phase 1 (M6)

- **Two-step sign-in (TOTP):** Account → Two-step sign-in shows a key and an `otpauth://` link, confirms a code, then shows 8 one-time recovery codes (stored hashed). The login page asks for a code when needed; recovery codes work there too. Turning it off needs the password; Admin → Users has "Turn off 2-step" for locked-out users.
- **Link a TV with a code:** `POST /api/link/start` (no auth, rate-limited) returns a 6-character code and a poll token. Someone signed in enters it at `/link` (admins can pick which user). `GET /api/link/poll` returns 202 until approved, then the session once (`web=1` also sets the cookie). Codes live in memory for 10 minutes. The web login page has "Sign in with a code instead".
- **Devices and API keys:** Account lists signed-in devices (sign out any but this one) and API keys (`bb_…`, shown once, stored hashed). Keys work anywhere a session token does. Admin → Live TV → Sources can make a key for an IPTV app and fills in the playlist and guide URLs, so they no longer embed a browser session.
- **Sharing:** an opt-in toggle in Account; Home shows "Friends are watching" from users who opted in, filtered to items the viewer can access, with no IPs or devices.
- **Webhooks:** Settings → Webhooks. Events: playback.start (once 30s have played, same as history), playback.stop (only for plays that started), playback.finished, recording.started/completed/failed. With a secret, the body is signed: `X-BingeBandit-Signature: sha256=<hex HMAC>`. Recording payloads drop the file path.
- **Tested in the browser:** the whole 2FA cycle including wrong codes and single-use recovery codes, code linking (browser and app style, admin-for-user, expired/used codes), API key use, `last_used` and revocation, session revoke, friends activity, and every webhook event against a local receiver that checked signatures.
- **Not done yet:** passkeys, OIDC/Google, a documented public API, network/remote access settings. A TOTP code can be reused within its 30-second window (no replay tracking). There is no QR code on the setup or link screens yet.
- Migration 10.

## Custom channels (M5)

- Admin → Live TV → Channels → **New custom channel**: number, name, an all-day block, optional time blocks, shuffle or order, and sources from libraries, shows, collections, playlists and labels. The editor shows the next 12 airings.
- **Tested:** a channel with Movies (shuffle, all day) plus Test Show (in order, now to +30 min) built 2,644 entries over 48h.
  - The guide showed the right "now".
  - Watching through an item boundary kept the stream continuous, with no fail-overs.
  - The web player played it at 720p.
  - A builder unit test covers contiguity, shuffle-without-repeats, timed blocks and refill.
- **Known limits:** an item that runs past a block's start delays that block (no hard cut). An item with no audio track is streamed without audio rather than with generated silence, which some players dislike. The custom source picker's display-name fix was not re-verified in the browser.
- Migration 9.

## Live TV phase 2 — DVR, HLS upstreams, logos (M4)

- **HLS upstreams:** `.m3u8` provider streams now relay. Tested with fakeiptv channel 7: video passed through at ~260 kbps.
- **DVR:**
  - The guide has **Record** and **Record series** buttons, and red dots mark programs set to record.
  - The Recordings page lists Upcoming, Series and Recorded, with admin Approve buttons.
  - Settings → Live TV recording has allow users, require approval, folder, naming and padding.
- **Tested against fakeiptv:**
  - recording, stopping early (23s valid H.264/AAC kept) and de-duplication
  - a series rule scheduled 3 upcoming airings
  - the approval flow (user → pending → admin approves → scheduled)
  - the recordings folder added as a TV library: shows up as show / Season 2026 / dated episode, and the Watch link works
- **Not tested:** recordings that span hours, recordings while a provider is at its connection limit (they fail with "connection limit" and retry every 5s), and real providers.
- Migration 8.

## Live TV phase 1 (M4)

- **Admin → Live TV:** Sources (M3U and XMLTV), Channels (add from streams, edit, fallback order, guide match), and Activity (per-channel stats and viewers).
- **Viewer pages:** Live TV guide at `/live`, and the player at `/live/:id` (↑/↓ change channel, G toggles the mini guide).
- **Tested against `cmd/fakeiptv`:**
  - import, channel creation and guide matching
  - two web viewers on one upstream connection (confirmed in the provider's log)
  - MPEG-2 converted automatically
  - flaky stream → backup within the same second
  - M3U, XMLTV and raw TS output
  - channel flipping cleans up viewers
- **Not yet tested with a real IPTV provider or HDHomeRun.** Things to watch: provider-specific user agents, HTTPS redirects, and `.m3u8` (HLS) upstreams. Only MPEG-TS over HTTP is relayed today; HLS upstream support is the most likely next need.
- The exported playlist embeds the token it was fetched with. Use an API key (M6) for apps.

## Discover (M3)

- `internal/discover`: admins add YouTube playlists or channels on the Discover page (Manage). Links can be a playlist URL (`list=`), `/channel/UC…`, an `@handle` (looked up on the channel page), or a bare `PL…`/`UC…` id.
  - Videos come from YouTube's public Atom feeds (`/feeds/videos.xml?playlist_id=` / `channel_id=`), so no API key is needed. Each feed holds the newest 15 videos; up to 50 per source are kept.
  - Adding a source fetches it first, so a bad link is reported right away.
  - The **Refresh Discover** task runs every 3 hours. A failed fetch keeps the old videos and shows the error in Manage.
- Tables `discover_sources` and `discover_videos`. Videos play in a youtube-nocookie embed, and thumbnails load from i.ytimg.com.
- Tested against a fake YouTube (`TestDiscover`). YouTube was blocked in the build sandbox, so it **still needs a check against real YouTube feeds**.

## Intro and credits detection (M3)

- Pure Go (no Chromaprint dependency): `internal/markers`. The **Detect intros and credits** scheduled task (nightly, maintenance window) analyzes seasons that have new episodes. A show or season page has a "Detect now" button (admins).
- Tested with generated episodes that share theme and credits music at different offsets and bitrates. All markers landed within about 1s, with no false positives on unrelated audio. **Not yet tried on real TV episodes**: the thresholds `maxBitErrors`, `maxGapFrames` and `edgeFrames` may need tuning against a real season.
- `markers` table (migration 6): `source` is `detected` or `manual`. Manual markers (Edit → Advanced on an episode or movie) are never overwritten.
- **Movies** (`markers/credits.go`): no sibling episodes to compare, so the same task samples the picture in the last 15 minutes (or last fifth). Keyframes only (`-skip_frame nokey`); a file with too few keyframes falls back to one frame every 2s. ffmpeg `signalstats` gives each frame's average brightness and saturation, and dark, colourless frames count as credits. The longest run of 60s or more wins, bridging stretches of normal picture shorter than 8s. If it ends within 45s of the end, it runs to the end. Otherwise what follows is a post-credits scene: Skip Credits (and the automatic setting) jumps to it instead of ending the movie. Credits over footage or on coloured backgrounds aren't caught; admins can set those by hand or press Detect on the movie page. Tested with generated films only, not yet on real movies.
- Player: Skip Intro, plus Next / Skip Credits. Skipping the credits counts as finishing the episode. User settings for each: button, automatic or off.
- Movies get no automatic markers (there's nothing to compare against), but manual ones work.

## Watchlist, playlists, history, stats (M3)

- **Watchlist** (bookmark button; sidebar page; "On Your Watchlist" Home row). Finished movies leave it automatically.
- **Playlists** (+ button on any item, sidebar page, `/playlist/:id`). Shows and seasons expand to episodes in order. Reorder and remove rows; "Play all" plays continuously through `?playlist=` on the player.
- **Play history:** a `plays` row is written once a session has played 30s. The player reports the seconds it actually played; the server caps that at wall-clock time. Rows keep title and subtitle, so history survives library removals.
- **Profiles** (`/profile`, `/profile/:username`): stats plus recent history. Private unless the owner turns sharing on; admins can always view.
- **Admin Statistics** (`/admin/statistics`): daily plays, top movies, shows and viewers, streaming mode, clients, and paged history. Chart marks use `--chart-mark` (#0d9488), validated with the dataviz palette checker for the dark surface.
- Migration 5 adds these tables plus `users.profile_public`.

## Playback polish (M2)

- **Sidecar subtitles:** `Movie.en.srt`, `Movie.eng.forced.srt`, `Movie.English.SDH.srt` and similar are found during scans and by folder watching. Only `Movie.<tags>.ext` matches, like Plex. A `Subs/` (or `Sub/`, `Subtitles/`) folder beside the video is searched too: files named after the video, any file when the video is alone in its folder (`Subs/2_English.srt`), and everything in `Subs/<video name>/` (TV packs). Leading track numbers are ignored; the external name is stored relative to the video's folder (`Subs/2_English.srt`).
- **Image subtitles** (PGS/VobSub/DVB) are burned in by the transcoder when picked. ffmpeg can't create bitmap subtitles, so this is only unit-tested against the generated command line; **check it with a real Blu-ray rip.**
- **Account → Playback:** audio language, subtitle mode/language/size/box, auto quality, auto-play next. The player applies them; English audio is the default.
- Playback retries twice on failure; direct play falls back to transcoding; repeated buffering steps quality down with a notice.
- Transcodes are throttled (Settings → Transcoding toggle) when ffmpeg supports `-readrate_initial_burst`.
- Migration 4 adds `users.prefs`.

## Server maintenance (M2)

- **Folder watching:** new or deleted files show up about 30s after the copy finishes, and only that folder is rescanned. Toggle it in Settings → Library.
- **Scheduled Tasks page** (`/admin/tasks`): scan, metadata refresh, DB backup (folder and retention configurable), optimize, cache cleanup, media analysis. Each task has an enable switch, an interval and Run now. Maintenance tasks wait for the window (default 2–5 AM).
- **Logs page** (`/admin/logs`): live tail with filter and level views; the log file is in `<data>/logs/`.
- Scans probe 4 files in parallel.
- Migration 3 moves the old `scan_interval_hours` setting into the scan task.
- **Docker fix:** the arm64 image build failed on `intel-media-va-driver`, which is x86-only; it's now installed on amd64 only. The v0.1.0 and v0.2.0 Docker images did not publish; the next tag will.

## Library admin tools (M2)

- **Edit dialog** (pencil on any item page, admins only): Details, Tags (genres, labels, collections), Artwork (TMDB picks, URL, upload) and Advanced (episode order, shows only). Editing a field locks it; the lock icon toggles it.
- **Fix match** (magnifier on movies and shows): searches TMDB, re-points the item and refreshes it.
- **TheTVDB per show** (`metadata/tvdb.go`, `tvdb_show.go`): with a TheTVDB v4 key (and subscriber PIN for user-supported keys) in Settings → Metadata, Fix match on a show can switch to TheTVDB and pick aired, DVD or absolute order (`items.tvdb_id`, `tvdb_order`).
  - The show's text, poster, backdrop, cast, season posters and every episode come from TheTVDB, matched to files by season and episode number in that order.
  - When TMDB is set up and knows the show (TheTVDB's remote id), TMDB adds the trailer, tagline, rating, and a logo, backdrop or cast if TheTVDB has none.
  - Matching a TMDB result switches the show back. The login token is kept for a month and renewed on a 401.
  - TheTVDB images are linked straight from artworks.thetvdb.com, not cached.
  - Tested against a fake TheTVDB only (`TestTVDBShow`), so it needs a check with a real key.
- **Detect intros and credits** can be turned off per library (`libraries.skip_markers`, the checkbox in the library editor). The nightly task skips those libraries; the Detect button still works.
- **Collections:** a Collections tab on each library and `/collection/:id` pages. Smart collections take rules (genre, studio, a year range and/or a label; every set rule must match), and `/api/admin/libraries/{id}/facets` lists the values to pick from.
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

_Refreshed 2026-10-24 and kept for history; the current lists are **Open work** and **Never tried for real** at the top. The full backlog is in `docs/ROADMAP.md`._

Needs real hardware or real media (can't be checked with generated test files):

1. **Retest the TV app on v0.6.0, including the new profile picker.** The onn box wasn't retested after the phone work (M8 phase 3), and the last step of the in-app update (installing the APK) has never been run.
2. **Metadata matching on a real library.** Add a TMDB key, run "Refresh metadata", and tune `library/names.go` with new test cases.
3. **Live TV with a real IPTV provider or HDHomeRun.** Watch for provider-specific user agents and HTTPS redirects.
4. **Intro and credits detection on real episodes.** `maxBitErrors`, `maxGapFrames` and `edgeFrames` may need tuning against a real season.
5. **Custom channel source picker.** Its display-name fix was never re-checked in the browser.

Code gaps:

6. ~~Audio is always transcoded to AAC stereo.~~ Clients now say which audio they can play: the web player copies AC-3 (and E-AC-3 in Safari) or keeps 5.1 as AAC when the computer has surround output; the TV app copies AC-3/E-AC-3 its box can decode or pass through, and turns DTS/TrueHD into Dolby Digital for it (2026-10-27; still to be tried on the onn box with a receiver).
7. ~~Transcode sessions don't re-check library access after they start.~~ Done: running transcodes stop as soon as an admin takes the library away or deletes the user, profile or library, and re-check every 30 s otherwise.
8. ~~True adaptive bitrate.~~ Done as an opt-in admin setting (Settings → Transcoding → Adaptive streaming). It only applies when the video is converted anyway; a copied stream stays single quality. Not yet tried with hardware encoders on real GPUs, where three encodes per viewer may hit NVENC session limits.
9. ~~Backups only go to a folder.~~ Backups can also go to S3-compatible storage (tested against a local MinIO, not yet against Backblaze or AWS). They can be downloaded and restored from an uploaded file, including on a new server's setup screen.
