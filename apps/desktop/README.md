# BingeBandit for Windows

An Electron app for a living-room PC (an HTPC) or any Windows desktop. It shows the server's own web app full screen and hands videos to [mpv](https://mpv.io), so every file plays as it is: HEVC, AV1, 10-bit, HDR, DTS and TrueHD (passed through to a receiver if you turn that on), and image subtitles. The server never has to convert anything.

What it does:

- **Finds the server:** the first start looks for servers on this PC and the home network, or you type the address. Ctrl+Shift+S comes back to that screen later.
- **Full screen** from the start; F11 switches. Sign-in, profiles, kid mode and everything else are the web app's.
- **TV mode:** the web app is driven with a remote or the arrow keys (`web/src/tvNav.ts`). Arrows move to the nearest item that way: along a row, between rows, and across to the sidebar at the row's end. OK opens it, Back (or Escape, or Backspace) goes back and lands on the item you came from, and the menu key (or M) opens a card's menu. The page is drawn bigger so it reads from the sofa: about 1536 pixels wide whatever the screen. Ctrl+= and Ctrl+- change that, and Ctrl+0 goes back to automatic. To try TV mode in a browser, open any page with `?tv=1` (and `?tv=0` to turn it off).
- **Plays in mpv**, full screen over the app:
  - starts where you left off and reports progress, so Continue Watching, watched state, Trakt and Hue Sync work as in the other apps;
  - picks audio and subtitles from your preferences, including sidecar subtitle files;
  - skips intros and credits as your preferences say (OK skips when offered);
  - plays the next episode in the same window, or offers it (N or OK);
  - Back, Escape or Backspace stops, and the app goes back to where you were.
- **Multi-View** (`/multiview`, from the Sports page): up to four Live TV channels at once, like the TV app's. OK moves the sound to the focused screen, the menu key has change channel, full screen and remove, and the toolbar sets the number of screens, jumps to the next game of your teams and saves layouts. It works in any browser too.
- Watch Together and Live TV stay in the app's own browser player for now.

## Keys in the player

| Key | Does |
|---|---|
| OK / Enter | Skip the intro or credits when offered, otherwise pause |
| ◀ / ▶ | Back 10 s / forward 30 s |
| ▲ / ▼ | Volume |
| N | Next episode |
| Back / Esc / Backspace | Stop |

mpv's own keys work too (space pauses, `j` cycles subtitles, `#` cycles audio, `i` shows stats). Both settings files, `mpv.conf` and `input.conf`, are copied to `%APPDATA%\BingeBandit\mpv` on the first play and are yours to change after that. To send Dolby and DTS audio to a receiver untouched, uncomment the `audio-spdif` line in `mpv.conf`.

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

Releases build both .exe files on a Windows runner (`.github/workflows/release.yml`). They aren't code-signed yet, so Windows SmartScreen asks once before the first start ("More info", then "Run anyway").

## How it fits together

- `src/main.js` opens the window on the server's address (saved in `%APPDATA%\BingeBandit\settings.json`) and runs mpv for the page.
- `src/preload.js` gives the server's pages `window.bbDesktop` (and only them; the main process checks the sender too). The web app's player (`web/src/pages/DesktopPlayer.tsx`) uses it when it's there.
- `src/mpv.js` starts mpv with the session's token as a Bearer header and talks to it over its JSON IPC pipe: time, pause and end of file come back, and the next episode loads into the same window.
