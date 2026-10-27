# BingeBandit for Android TV, Fire TV and Android phones

A React Native app ([react-native-tvos](https://github.com/react-native-tvos/react-native-tvos)), driven by the remote's D-pad on TVs and by touch on phones. The same APK installs on both and switches layouts. It talks to the same `/api` as the web UI.

On phones:
- **Navigation:** tabs sit along the bottom.
- **Player:** tap for the controls, drag the bar to seek, and video turns sideways.
- **Live TV:** swipe up or down to change channel.
- **Guide:** swipe through time; tap a program to see it, and tap again to watch.
- **Kid lock:** the **Lock** button in either player asks for a 2-digit PIN you choose. Until that PIN is entered, taps and Back do nothing.

What it does:

- **Finds the server** on the home network by itself, or you type its address.
- **Signs in without typing a password:** it shows a code and a QR code. Approve it from a phone at `/link`, where an admin can pick whose account it is. A password works too, including two-step codes.
- Home rows, Continue Watching, movie/show/season/episode pages, mark watched.
- **Plays** directly when the TV can decode the file, otherwise through the server's transcoder. It resumes where you left off, plays the next episode, and reports progress (history, Continue Watching, Trakt sync).
  - ◀/▶ seek.
  - OK opens a control bar: back 10 s, pause/play, forward 30 s, next episode, and audio & subtitles.
  - Audio and subtitles follow your preferences from the web app, and can be changed while watching.
- **Live TV:** a guide grid like a cable box, with the focused channel previewing live. OK tunes in; ▲/▼ changes channel while watching.
- **Search**, and adding to or removing from your watchlist.
- **Updates itself:** when a newer APK is on GitHub, Settings offers it. The first time, Android asks you to allow BingeBandit to install apps.

## Building

Needs Node 22, JDK 17 and the Android SDK (platform 36, build-tools 36, NDK 27.1).

```powershell
cd apps/tv
npm install
cd android
.\gradlew assembleSideloadRelease -PbbVersion=0.4.0
```

The APK lands in `android/app/build/outputs/apk/sideload/release/`. Install it on a Fire TV or Android TV with `adb install`, or with the Downloader app.

`-PbbVersion` sets the version name, and the version code is derived from it (`major*10000 + minor*100 + patch`). Releases pass the git tag.

On Windows inside a sandboxed or packaged environment, Gradle can fail with "Unable to establish loopback connection". Set `JAVA_TOOL_OPTIONS=-Djdk.net.unixdomain.tmpdir=C:\Users\<you>\.jdk-uds` (a folder outside AppData).

There are two flavors. **sideload** is the APK above, which updates itself from GitHub. **play** (`bundlePlayRelease`) is the bundle for Google Play, without the self-updater: see [docs/GOOGLE_PLAY.md](../../docs/GOOGLE_PLAY.md).

## Signing

Release builds are signed with the key in `BB_KEYSTORE` (path), plus `BB_KEYSTORE_PASSWORD`, `BB_KEY_ALIAS` and `BB_KEY_PASSWORD`. Without these they're signed with the debug key, which is fine for testing.

For GitHub releases, add these repository secrets:

- `ANDROID_KEYSTORE_BASE64` (`base64 -w0 release.keystore`)
- `ANDROID_KEYSTORE_PASSWORD`
- `ANDROID_KEY_ALIAS`
- `ANDROID_KEY_PASSWORD`

Keep the keystore safe: Android only installs an update if it's signed with the same key as the version already installed.

## Installing on a TV

Turn on developer options and USB/ADB debugging on the TV, then:

```powershell
adb connect <tv-ip>:5555        # approve the prompt on the TV
adb install -r BingeBandit-TV-0.4.0.apk
```

If the server isn't found automatically (for example the TV and server are on different subnets), choose **Type an address** and enter the server's IP.

## Developing

For quick iteration on a real TV:

1. Build and install a debug APK: `.\gradlew assembleDebug -PreactNativeArchitectures=armeabi-v7a`, then `adb install`.
2. Run `npm start` (Metro) and `adb reverse tcp:8081 tcp:8081`.
3. Edits now reload on the TV in seconds. `adb logcat | findstr ReactNativeJS` shows the app's logs.

The emulator needs the Windows Hypervisor Platform feature turned on.

Lay screens out on a 1920×1080 canvas and wrap style sheets in `scaled()` (`src/theme.ts`). Android TV reports a 1080p screen as about 960×540 dp, so unscaled sizes come out twice as large.
