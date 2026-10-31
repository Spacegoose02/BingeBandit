# BingeBandit for Roku

A Roku channel written in BrightScript and SceneGraph, Roku's own app framework. It shares no code with the Android TV app in `apps/tv`, but talks to the same server API.

**Status:** never tried on a real Roku. It has been checked with the BrighterScript compiler and run in the [brs-engine](https://github.com/lvcabral/brs-engine) simulator against a local server (sign-in, Who's watching? with PINs, Home, libraries, show and season pages, search, resume position, the Live TV guide and its options, Live TV join, flip and leave, the player's Options and drawn subtitles). The simulator can't play video, so playback itself is untested.

## What it does

- **Finding the server:** scans the Roku's home network on port 8484, or you type an address (a bare name like `tv.example.com` tries https first, an IP tries :8484).
- **Signing in:** shows a pairing code to enter at `/link` on a phone or computer that's signed in, the same as the Android TV app. Nobody types a password on the remote.
- **Who's watching?** with 4 to 8 digit PINs. It's skipped when the TV was linked straight to a profile, when there's only one person, or when the account opens as someone without a PIN.
- **Home:** Continue Watching and the server's rows. The menu on the left has Search, each library, Approved Movies and Approved Shows for kid profiles, Live TV (when it's on for the account), Settings, Switch profile and Sign out.
- **Settings:** the settings passcode, the same one as the web and the Android TV app: turn it on, change it, turn it off, or lock now. A new code is typed twice. In a household it's each person's PIN instead, and the account owner can add, change or remove its profiles' PINs. A change made while settings are locked asks for the passcode first, then goes through.
- **Libraries** as a poster grid with collections mixed in by title, and collections open as their own grid.
- **Libraries** filter with `*`: genre, decade, rating and unwatched only.
- **Details** for movies, shows, seasons and episodes: Play / Resume / Start over, Trailer (a movie's trailer file from its folder), Mark watched, watchlist, a row of seasons or episodes, and the cast. Picking someone in the cast opens a grid of their titles.
- **Playback:** direct play when the Roku says it can decode the file (MP4 or MKV with H.264, HEVC, VP9 or AV1 and a supported audio track, checked with `roDeviceInfo.CanDecodeVideo/Audio`); otherwise the server converts it to HLS. Direct play that fails or stalls for 12 seconds falls back to converting. HDR files on a TV without HDR10, and files taller than the TV's output, are converted. Progress is reported every 10 seconds and on exit, so Continue Watching and resume work across devices. Episodes offer the next one with a 10 second countdown.
- **Audio and subtitles** follow the viewer's preferences from the web (audio language, subtitles off / auto / forced only / always, size and background), like the Android TV app. Text subtitles are drawn by the channel from the server's WebVTT, so they stay in step in direct play and conversion; image subtitles (PGS, DVD) are burned in by the server. Another audio track in direct play is switched on the Roku itself when its track list matches the file's, otherwise the server converts with that track.
- **Skip intro and credits** from the server's markers: a button (OK skips) or automatic, as the viewer's preferences say. Skipping an episode's credits goes to Up next; a movie with a scene after the credits jumps to it. Up next waits for OK when the countdown is turned off.
- **Player remote:** OK or Play/Pause pauses, Left/Right jump 10 s back / 30 s forward, Rewind/Fast forward jump a minute, presses add up and the jump happens when you stop. `*` opens Options: audio track, subtitles and quality (direct play, convert, or convert at 1080p / 720p / 480p).
- **Live TV guide:** a cable-box grid (Roku's TimeGrid) with six hours of programs, the focused program described up top and the last channel watched previewing in the corner while the guide has focus. OK watches the channel; `*` adds or removes it from favourites, follows the show (a pop-up before it airs), records the program or series (or asks an admin to), and picks the channels shown (all, favourites, your categories, the provider's groups).
- **Watching Live TV:** Up/Down change channel. Long buffering is reported so the server can move a channel to its backup stream.
- **Kid profiles:** the server already filters what a kid can see; the channel just shows what it's given, plus the Approved tabs.

Not there yet: Watch Together, the Sports Hub and Multi-View, trailers and the intro video before a movie (pre-roll), Discover, Hue Sync and Game Lights on the TV, long-press menus (playlists, recommend, admin tools) and settings beyond the passcode and PINs. Channel logos that are SVG files don't show (Roku can't draw SVG); PNG and JPEG logos do, in the player.

**To check on a real Roku** (none of this ran outside the simulator): direct play and conversion at all; switching audio tracks in direct play (`availableAudioTracks` order against the file's); subtitle timing and how the drawn subtitles look over video; skip intro in direct play and in conversion; the guide's preview Video next to the grid, the TimeGrid's look (focus and program backgrounds, the now bar) and scrolling through six hours; trailer files in MKV.

## Server side

Roku's video player and image loader can't send an `Authorization` header reliably, so the channel adds `?token=` to image, file and playlist URLs (the server already accepted it on GETs). Playlists asked for with `?token=` now come back with the token on every segment URI too (`server/internal/api/hlstoken.go`), and `&fromStart=1` adds `EXT-X-START:TIME-OFFSET=0` so a conversion's growing EVENT playlist plays from its start instead of its live edge.

## Build

```sh
./build.sh          # writes build/BingeBandit-Roku.zip
npx brighterscript --project bsconfig.json   # type and syntax check
```

CI checks the channel and uploads the zip as the `BingeBandit-Roku` artifact on every push (unzip GitHub's download once to get `BingeBandit-Roku.zip`). Tagged releases attach `BingeBandit-Roku-vX.Y.Z.zip`, with the manifest version set from the tag.

## Sideloading onto a Roku (developer mode)

1. On the Roku remote press **Home 3×, Up 2×, Right, Left, Right, Left, Right**. The Developer Settings screen appears.
2. Note the IP address it shows, choose **Enable installer and restart**, agree to the license, and set a web server password. The Roku restarts.
3. On a computer on the same network, open `http://<roku-ip>` in a browser and sign in as `rokudev` with that password.
4. Click **Upload**, pick `BingeBandit-Roku.zip`, and click **Install with zip**. The channel opens straight away and stays in the Home screen's channel list.

Only one sideloaded channel can be installed at a time; installing another replaces it. Updating is the same upload with a newer zip. To watch the channel's log while testing, `telnet <roku-ip> 8085`.

## Publishing on the Roku Channel Store

A Channel Store listing needs a free Roku developer account at [developer.roku.com](https://developer.roku.com), linked to a Roku account. The channel is packaged on a Roku in developer mode (Utilities → Packager at `http://<roku-ip>`, after running `genkey` over telnet to make a signing key), then uploaded in the developer dashboard with artwork, a description and a privacy policy, and goes through Roku's certification. A **beta channel** is the quickest way to share it with a few people without certification: up to 20 Roku accounts install it from an invite link, and it expires after 120 days. Without any of this, each Roku can still run the sideloaded zip in developer mode.
