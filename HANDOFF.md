# Handoff

Snapshot of where BingeBandit stands, written so the next person (or AI session) can pick it up cold.

_Last updated: 2026-10-24, Hue Sync (M9 phase 2)._

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
- **Web:** `pages/WhoIsWatching.tsx` at `/who` appears after a password sign-in (a `sessionStorage` flag in `profilePick.ts`) and from the user menu → Switch profile. Account → Profiles adds, edits (name, libraries, PIN) and deletes profiles. Admin → Users marks profiles and hides Reset password for them.
- **TV app:** `screens/Profiles.tsx` with a remote-friendly PIN pad. It shows after sign-in when the account has profiles, and from Settings → Switch profile. **Not yet tried on a device**: the onn box and the S24+ still need a run-through. It type-checks and lints.

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

## Intro and credits detection (M3)

- Pure Go (no Chromaprint dependency): `internal/markers`. The **Detect intros and credits** scheduled task (nightly, maintenance window) analyzes seasons that have new episodes. A show or season page has a "Detect now" button (admins).
- Tested with generated episodes that share theme and credits music at different offsets and bitrates. All markers landed within about 1s, with no false positives on unrelated audio. **Not yet tried on real TV episodes**: the thresholds `maxBitErrors`, `maxGapFrames` and `edgeFrames` may need tuning against a real season.
- `markers` table (migration 6): `source` is `detected` or `manual`. Manual markers (Edit → Advanced on an episode or movie) are never overwritten.
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

- **Sidecar subtitles:** `Movie.en.srt`, `Movie.eng.forced.srt`, `Movie.English.SDH.srt` and similar are found during scans and by folder watching. Only `Movie.<tags>.ext` matches, like Plex.
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

_Refreshed 2026-10-24. The full backlog is in `docs/ROADMAP.md`._

Needs real hardware or real media (can't be checked with generated test files):

1. **Retest the TV app on v0.6.0, including the new profile picker.** The onn box wasn't retested after the phone work (M8 phase 3), and the last step of the in-app update (installing the APK) has never been run.
2. **Metadata matching on a real library.** Add a TMDB key, run "Refresh metadata", and tune `library/names.go` with new test cases.
3. **Live TV with a real IPTV provider or HDHomeRun.** Watch for provider-specific user agents and HTTPS redirects.
4. **Intro and credits detection on real episodes.** `maxBitErrors`, `maxGapFrames` and `edgeFrames` may need tuning against a real season.
5. **Custom channel source picker.** Its display-name fix was never re-checked in the browser.

Code gaps:

6. Audio is always transcoded to AAC stereo. Add 5.1 passthrough for clients that can play it.
7. Transcode sessions don't re-check library access after they start (they're bound to the user who started them).
8. True adaptive bitrate (a multi-rendition HLS ladder).
9. Backups only go to a folder (no remote targets such as S3). Backups can be downloaded and restored from an uploaded file, including on a new server's setup screen.
