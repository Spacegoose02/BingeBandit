# BingeBandit for Samsung and LG TVs

A small app for Samsung (Tizen) and LG (webOS) TVs. It asks for your server's address once, then opens the server's own web app in TV mode, which is driven by the remote: the arrows move, OK picks, Back goes back, and Play/Pause and the other media keys work in the player. Everything after the first screen comes from your server, so the app never needs updating for new features.

On the next start it opens the last server by itself after a couple of seconds; choose **Use another server** before then to change it.

Neither store has it yet, so it's installed with the TV in developer mode. It hasn't been tried on a real Samsung or LG TV yet; please report what works and what doesn't.

## Build

```sh
./build.sh
```

This puts the two apps together in `dist/tizen` and `dist/webos`.

## Samsung (Tizen, 2018 and later)

1. Install [Tizen Studio](https://developer.tizen.org/development/tizen-studio/download) with the **TV Extensions** and the **Samsung Certificate Extension** (Package Manager → Extension SDK).
2. On the TV, open **Apps**, press **1 2 3 4 5** on the remote, turn **Developer mode** on and enter your computer's IP address. Restart the TV.
3. In Tizen Studio's **Device Manager**, connect to the TV's IP address.
4. In **Certificate Manager**, make a **Samsung** certificate profile (it asks you to sign in with a Samsung account and adds the TV's DUID).
5. Package and install:

   ```sh
   tizen package -t wgt -s <certificate profile> -- dist/tizen
   tizen install -n dist/tizen/BingeBandit.wgt -t <TV name from sdb devices>
   ```

## LG (webOS 4 and later)

1. Install the [webOS TV CLI](https://webostv.developer.lge.com/develop/tools/cli-installation) (`npm install -g @webos-tools/cli`).
2. Install the **Developer Mode** app from the LG Content Store on the TV, sign in with an LG developer account, and turn on **Dev Mode Status** and **Key Server**. Developer mode lasts 50 hours at a time; the app can extend it.
3. Add the TV and install:

   ```sh
   ares-setup-device            # add the TV: its IP address, port 9922, user prisoner
   ares-novacom --device tv --getkey   # the passphrase the Developer Mode app shows
   ares-package dist/webos
   ares-install --device tv app.bingebandit.tv_1.0.0_all.ipk
   ```

## Notes

- The server has to be reachable from the TV at the address you type, and HTTPS needs a certificate the TV trusts (a Cloudflare tunnel's or a public one; not a self-signed one).
- Video plays in the TV's browser engine, so a file it can't play is converted by the server, as on any browser. Surround sound and HDR depend on the TV.
- The web app turns TV mode on by itself on these TVs, so opening the server's address in the TV's own browser works much the same, without the app.
