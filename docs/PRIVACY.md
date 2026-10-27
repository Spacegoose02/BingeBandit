# BingeBandit privacy policy

_Last updated: 27 October 2026_

BingeBandit is a self-hosted media server. The BingeBandit apps (web, Android TV, Fire TV and Android phones) are clients for a BingeBandit server that you, or someone in your household, runs on your own hardware.

## What the app sends, and where

The app only talks to the BingeBandit server whose address you enter or that it finds on your home network. That server, not the BingeBandit developers, holds:

- your account (username, display name, optional email and PIN),
- what you watch and how far you got (watch history, Continue Watching, watchlist),
- your preferences (audio and subtitle languages, menu layout).

The BingeBandit developers don't run that server and receive none of this data. There are no ads, analytics or tracking SDKs in the app, and nothing is sold or shared.

Your server may connect to other services that its administrator turns on, such as TMDB for artwork, Trakt for watch history sync, or Plex for imports. Those are set up on the server and follow that service's own privacy policy.

## Connections on your network

Home servers are often reached over plain `http` on the local network. Traffic is encrypted only when your server is set up with `https`.

## Children

The app isn't directed at children. Households can create kids profiles on their server, where an adult approves which titles and channels the profile can see.

## Deleting your data

Your data lives on your server. Its administrator can delete your account and history from Admin → Users. Signing out of the app removes the sign-in from the device; uninstalling removes everything the app stored on it.

## Contact

Questions: open an issue at https://github.com/Spacegoose02/BingeBandit/issues.
