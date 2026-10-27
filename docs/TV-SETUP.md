# Putting BingeBandit on someone else's TV

This guide is for setting up a friend's or family member's TV so it watches from your server. You do steps 1 and 2 once, on your server. Steps 3 to 5 happen at their TV and take about ten minutes. Nobody types a password on the TV.

## What you need

- **A TV that runs Android apps:** Google TV, Android TV (including Sony, TCL, Hisense and Philips models with Google TV), Nvidia Shield, or an Amazon Fire TV / Fire TV Stick. It needs Android 7 or newer, which covers anything from the last several years.
- **Roku:** there's an early Roku channel that has to be sideloaded with the Roku in developer mode (see `apps/roku/README.md`). It isn't in the Roku Channel Store yet.
- **Other TVs** (Samsung, LG, Apple TV, Vizio) can't run the app. Plug in a Google TV Streamer, Chromecast with Google TV or Fire TV Stick and use that instead.
- **Your phone**, signed in to BingeBandit, to approve the TV.
- **An account or profile for them** on your server (see step 2).

## 1. Make your server reachable from outside your home

The TV is on someone else's internet, so it needs an address that works from anywhere. Pick one:

- **A domain with a reverse proxy (recommended).** Point a hostname such as `tv.example.com` at `http://<server-ip>:8484` with Nginx Proxy Manager, SWAG, Caddy, Traefik or Cloudflare Tunnel, with HTTPS turned on. Then in BingeBandit go to **Server → Settings → Network**:
  - Add the proxy's IP under **Trusted reverse proxies**.
  - Set **Public URL** to `https://tv.example.com`.
- **Tailscale.** Install Tailscale on the server and on their TV (it's in the Play Store and on Fire TV), and share your server with their Tailscale account. On the TV you use the server's Tailscale address, such as `100.101.102.103`. Nothing is exposed to the internet. In **Server → Settings → Network**, add `100.64.0.0/10` under **Extra local networks** so those devices count as home.
- **Port forward (simplest, least safe).** Forward TCP port 8484 on your router to the server, and use your home's public IP or a dynamic DNS name. This is plain HTTP with no encryption, so prefer one of the options above.

Also check that **Allow connections from outside the home network** is on (it is by default).

**Test it:** on your phone with Wi-Fi off, open the address. You should see the BingeBandit sign-in page.

## 2. Give them an account or a profile

- **Their own account:** under **Server → Users**, add a user for them. Their watch history, Continue Watching and watchlist stay separate from yours.
- **A profile under your account:** on your **Account** page, choose **Add profile**. Use this for family who don't need their own sign-in.
- You can limit what each one sees, such as libraries, Live TV or kid mode. For accounts, set this under **Server → Users**. For profiles, set it on the profile itself.

## 3. Install the app on their TV

The app isn't in the app stores. You install it with the free **Downloader** app.

1. **Install Downloader:**
   - Fire TV: search the Appstore for "Downloader" (by AFTVnews).
   - Google TV / Android TV: search the Play Store for "Downloader".
2. **Allow it to install apps:**
   - **Fire TV:** go to **Settings → My Fire TV → About** and click the device name 7 times until it says you're a developer. Then go to **Settings → My Fire TV → Developer options → Install unknown apps** and turn on **Downloader**.
   - **Google TV:** go to **Settings → System → About** and click **Android TV OS build** 7 times. Then go to **Settings → Apps → Security & restrictions → Unknown sources** and turn on **Downloader**.
3. **Download BingeBandit:** open Downloader, enter `github.com/Spacegoose02/BingeBandit/releases/latest`, scroll to **Assets** and open `BingeBandit-TV-<version>.apk`. Choose **Install**, then **Done** (or **Open**).
4. You can delete the downloaded file when Downloader offers to. Later updates appear in BingeBandit under **Settings**, so you never need Downloader again.

## 4. Point the app at your server

Open BingeBandit on the TV. It looks for servers on the local network first, which won't find yours from their house.

Choose **Type an address** and enter your address from step 1. You don't need `https://` or a port:

- `tv.example.com` for a domain (the app tries HTTPS first)
- `100.101.102.103` for Tailscale
- `203.0.113.7:8484` for a port forward

Not sure of the address? It's shown at the bottom of the **Link a device** page (`/link`) on your server. The app remembers the address, so this only happens once.

## 5. Sign them in with the code

The TV now shows a six-letter code and a QR code.

1. On your phone, scan the QR code, or go to `/link` on your server (for example `https://tv.example.com/link`) and type the code.
2. **Sign it in as:** if you're an admin, pick their account.
3. **Profile:**
   - **Always <name>** means the TV always opens as that profile, without asking who's watching. Use this for a single-person TV.
   - **Ask who's watching** means the TV shows the profile picker each time it opens.
4. Tap **Link device**. The TV signs in within a couple of seconds.

The code lasts 10 minutes. If it runs out, choose **Get a new code** on the TV.

## If something goes wrong

- **"No BingeBandit server answered":** check the address from step 1 on a phone that's off Wi-Fi. If it works there but not on the TV, look for typos, or try writing `https://` out in full.
- **The QR code opens an address that doesn't load:** the TV connected using its local address. Go to **Type an address** on the TV and use the public one.
- **Videos buffer:** your home upload speed limits how much can stream out. Pick a lower quality in the TV player's settings.
- **Sign them out later:** remove the TV under **Signed-in devices** on the **Account** page. Use their account's page if the TV is on their account. If it's on one of your profiles, switch to that profile first. As an admin you can also delete the user.
