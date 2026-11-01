# BingeBandit for Windows

An Electron app for a living-room PC (an HTPC) or any Windows desktop. It shows the server's own web app full screen and hands videos to [mpv](https://mpv.io), so every file plays as it is: HEVC, AV1, 10-bit, HDR, DTS and TrueHD (passed through to a receiver if you turn that on), and image subtitles. The server never has to convert anything.

What it does:

- **Finds the server:** the first start looks for servers on this PC and the home network, or you type the address. Ctrl+Shift+S comes back to that screen later.
- **Full screen** from the start; F11 switches. Sign-in, profiles, kid mode and everything else are the web app's.
- **TV mode:** the web app is driven with a remote or the arrow keys (`web/src/tvNav.ts`). Arrows move to the nearest item that way: along a row, between rows, and across to the sidebar at the row's end. OK opens it, Back (or Escape, or Backspace) goes back and lands on the item you came from, and the menu key (or M) opens a card's menu. The page is drawn bigger so it reads from the sofa: about 1536 pixels wide whatever the screen. Ctrl+= and Ctrl+- change that, and Ctrl+0 goes back to automatic. To try TV mode in a browser, open any page with `?tv=1` (and `?tv=0` to turn it off).
- **Plays in mpv**, full screen over the app, under BingeBandit's own controls (not mpv's): the title, a progress bar with the intro and credits marked, the time and when it ends, and an options menu. They're sized to read from the sofa and work with a remote, the keyboard or a mouse.
  - starts where you left off and reports progress, so Continue Watching, watched state, Trakt and Hue Sync work as in the other apps;
  - picks audio and subtitles from your preferences, including sidecar subtitle files; the menu changes them;
  - skips intros and credits as your preferences say, or offers a Skip intro button (OK);
  - plays the next episode in the same window, or offers it in an Up next card (OK);
  - Back, Escape or Backspace stops, and the app goes back to where you were.
- **Live TV in mpv:** channels play in mpv too, so any channel plays as it comes. Up and down change channel, the menu lists every channel with what's on now, left and right rewind and catch up, and the badge shows LIVE or how far behind you are (L or clicking the badge jumps back to live). The app holds the Live TV seat, rejoins on its own when a stream drops and asks the server for the channel's next source when a stream stalls, like the web player.
- **Watch Together in mpv:** join a watch party from an invitation or a link and it plays in mpv, kept in step with everyone else. Play, pause and skipping go both ways, and a message says who did what. The menu starts a party, invites people, copies the invite link and leaves.
- **Updates itself:** the installed app downloads a new version in the background and installs it the next time it closes; a notice in the app offers to restart now. The portable .exe can't update itself, so it says when a new version is out and links to the download.
- **Multi-View** (`/multiview`, from the Sports page): up to four Live TV channels at once, like the TV app's. OK moves the sound to the focused screen, the menu key has change channel, full screen and remove, and the toolbar sets the number of screens, jumps to the next game of your teams and saves layouts. It works in any browser too (it stays in the app's browser player).

## Keys in the player

| Key | Does |
|---|---|
| OK / Enter | Skip the intro or credits, or play what's up next, when offered; otherwise pause |
| ◀ / ▶ | Back 10 s / forward 30 s |
| ▲ / ▼ | Volume; on Live TV, the channel (Page Up / Page Down too) |
| Menu key, M or Tab | The menu: audio, subtitles, channels, Watch Together |
| N | Next episode |
| L | Back to live |
| Back / Esc / Backspace | Close the menu, or stop |

mpv's own keys work too (`j` cycles subtitles, `#` cycles audio, `i` shows stats). Both settings files, `mpv.conf` and `input.conf`, are copied to `%APPDATA%\BingeBandit\mpv` on the first play and are yours to change after that (a file still exactly as an older version wrote it is replaced with the new default). To send Dolby and DTS audio to a receiver untouched, uncomment the `audio-spdif` line in `mpv.conf`.

A TV remote drives the app through HDMI-CEC with a USB-CEC adapter (Pulse-Eight), or with a FLIRC receiver or an air-mouse remote: anything that sends arrow keys, Enter and Back.

## Building

Needs Node 22. On Windows:

```powershell
cd apps/desktop
npm install
npm run fetch-mpv   # downloads mpv into .\mpv (needs 7-Zip's 7z on PATH)
npm start           # runs the app
npm run dist        # dist\BingeBandit-Windows-Setup-<version>.exe and a portable .exe
```

Without `fetch-mpv`, the app uses an `mpv` on PATH. `npm test` runs the tests; the playback test needs mpv and ffmpeg and is skipped without them.

Releases build both .exe files on a Windows runner (`.github/workflows/release.yml`), with `latest.yml` and the installer's `.blockmap`, which installed apps read to update themselves (`src/updates.js`, electron-updater, GitHub releases). The app checks at start and every six hours; set `BB_NO_UPDATES=1` to turn that off.

They aren't code-signed, so Windows SmartScreen asks once before the first install ("More info", then "Run anyway"); updates install without asking. To sign them, add a code signing certificate as two repository secrets: `WIN_CSC_LINK` (the .pfx, base64-encoded) and `WIN_CSC_KEY_PASSWORD`. The release build signs when they're there.

## How it fits together

- `src/main.js` opens the window on the server's address (saved in `%APPDATA%\BingeBandit\settings.json`) and runs mpv for the page.
- `src/preload.js` gives the server's pages `window.bbDesktop` (and only them; the main process checks the sender too). The web app's player (`web/src/pages/DesktopPlayer.tsx`) uses it when it's there.
- `src/mpv.js` starts mpv with the session's token as a Bearer header and talks to it over its JSON IPC pipe: time, pause and end of file come back, and the next episode (or channel) loads into the same window.
- `mpv-scripts/bingebandit.lua` draws BingeBandit's controls over mpv (with the Inter font in `mpv-scripts/fonts`). The page tells it what to show (`bb-state`: title, prompts, the party badge; `bb-menu`: the page's menu entries; `bb-toast`) and it sends keys and menu picks back as `bb-enter`, `bb-next`, `bb-pick <id>` and `bb-channel up|down`. Audio and subtitle choices it makes itself. `web/src/pages/DesktopPlayer.tsx` and `DesktopLivePlayer.tsx` are the pages.
- `window.bbDesktop.features` says what the app can do (`live`, `together`, `ui`), so a newer server still works with an older app: it keeps Live TV and watch parties in the browser player there.
