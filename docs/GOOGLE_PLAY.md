# Publishing the Android app on Google Play

One Play listing covers Android phones, tablets, Android TV and Google TV. Fire TV doesn't use Google Play, so it keeps the APK from GitHub Releases.

## What the repo builds

The Android app has two flavors:

- **sideload**: the APK on GitHub Releases (`BingeBandit-TV-vX.Y.Z.apk`). Same as before: it checks GitHub for updates and installs them itself.
- **play**: the bundle for Google Play (`BingeBandit-TV-GooglePlay-vX.Y.Z.aab`). Play updates it, so it has no in-app updater and no "install unknown apps" permission (Play rejects apps that update themselves).

Each release builds both. The `.aab` is only built when the signing secrets are set, because Play won't accept a bundle signed with the debug key. Its version code comes from the tag (`v0.25.0` → 2500), so every release is a higher number than the last, as Play requires.

Build it locally with:

```powershell
cd apps/tv/android
.\gradlew bundlePlayRelease -PbbVersion=0.25.0
```

## Signing

Use **Play App Signing** (Play Console's default). Google keeps the key that signs what users install; the key in the `ANDROID_KEYSTORE*` secrets becomes the **upload key**, which only proves the upload came from you. If the upload key is ever lost, Play support can reset it.

If the secrets are already set (the GitHub APKs are signed with them), that key can be the upload key. When Play asks, choose "Use Google-generated key". Devices that sideloaded the APK then can't update to the Play version in place, since Play's key is different: uninstall the APK first. (To avoid that, choose "Use a different app signing key" and upload the existing release key with Google's PEPK tool instead; it's more steps and that key then can't be rotated easily.)

If they aren't set yet, make a key once (keep the file and passwords somewhere safe, never in the repo):

```powershell
keytool -genkeypair -v -keystore bingebandit-upload.keystore -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

Then add the four repository secrets (GitHub → Settings → Secrets and variables → Actions): `ANDROID_KEYSTORE_BASE64` (the file, base64-encoded), `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`.

## Play Console checklist

Done by hand in [Play Console](https://play.google.com/console):

1. **Create app**: name "BingeBandit", app (not game), free. Package name is fixed forever on first upload: `com.bingebandit.tv`.
2. **Upload the first bundle** to the **Internal testing** track: download the `.aab` from the GitHub release and drag it in. Accept Play App Signing.
3. **Set up your app** (Dashboard → "Set up your app"):
   - **Privacy policy**: `https://github.com/Spacegoose02/BingeBandit/blob/main/docs/PRIVACY.md`
   - **App access**: "All or some functionality is restricted". Reviewers need a server they can reach from the internet and a login. Give them the server address, a username and password for a test account (not an admin), and one line on how to sign in (Settings → enter the server address, or "Sign in with a password"). Without this the review is rejected.
   - **Ads**: No ads.
   - **Content rating**: fill in the questionnaire. Category "All other app types". Answer honestly that users can stream media from their own server; the app itself has no user-to-user chat, purchases or location sharing.
   - **Target audience**: pick **13–15, 16–17 and 18+** only. Don't tick any under-13 age group (see "Kids profiles" below).
   - **News app**: No. **Government app**: No. **Financial features**: none. **Health**: none.
   - **Data safety**: Data is sent to the server the user chooses, so declare it as collected:
     - Personal info → Name, Email address (optional) – App functionality, Account management.
     - App activity → App interactions (watch history), Other user-generated content (watchlist) – App functionality.
     - Not shared with third parties. Not encrypted in transit (home servers often use plain `http`). Users can request deletion (from their server's admin).
4. **Store listing**:
   - Short description (80 characters), full description, app icon 512×512 PNG, feature graphic 1024×500.
   - Phone screenshots: at least 2.
   - **Android TV**: open Advanced settings → Form factors → add Android TV and opt in. That needs a TV banner 1280×720 and at least 1 TV screenshot (1920×1080). The TV version goes through its own quality review (D-pad works everywhere, Back behaves, no touch-only screens), which can take a few extra days.
5. **Countries/regions**: pick where it's available.
6. **Closed testing** (required for new personal developer accounts): before production you must run a closed test with at least **12 testers** opted in for **14 days in a row**, then apply for production access from the Dashboard. Testers join with a Google account via an opt-in link. Family members and friends count. Internal testing (up to 100 people, no review wait) is fine for trying it yourself first, but it doesn't count toward the 14 days.
7. **Production**: after Google approves production access, promote the tested release.

Each later release: download the new `.aab` from GitHub Releases and upload it to the track. (Uploading automatically from the release workflow with a Play service account is possible later.)

## Kids profiles on Google TV

Google TV kids profiles only show apps a parent picks from Google Play, so getting on Play is what makes this possible. As far as Google's help pages say, the parent can choose apps without the app being in the Families program; check this early by installing from the internal or closed test track and looking for BingeBandit in the kids profile's app list.

Don't opt into **Designed for Families** or pick under-13 age groups. That brings the Families policy: Google expects all content in the app to be suitable for children, which a client for your own media library can't promise, and it adds stricter reviews. BingeBandit's own kids profiles (approved titles and channels) are the right way to keep kids to approved content, and they work whatever Play thinks the audience is.
