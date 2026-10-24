# BingeBandit on Unraid

BingeBandit runs as one Docker container. The image is `ghcr.io/spacegoose02/bingebandit`. It has ffmpeg and the Intel and AMD hardware drivers built in, and works on x86 and ARM.

## 1. Install the container

### Option A: from the template (recommended)

1. In Unraid, go to **Docker**, scroll to the bottom and open **Template repositories**.
2. Add this line, then press **Save**:

   ```
   https://github.com/Spacegoose02/BingeBandit
   ```

3. Press **Add Container**, open the **Template** drop-down and choose **BingeBandit** under the user templates.
4. Check the paths described below, then press **Apply**.

### Option B: by hand

**Docker → Add Container**, then fill in:

| Field | Value |
|---|---|
| Name | `BingeBandit` |
| Repository | `ghcr.io/spacegoose02/bingebandit:latest` |
| Network Type | `Bridge` |
| WebUI | `http://[IP]:[PORT:8484]/` |
| Icon URL | `https://raw.githubusercontent.com/Spacegoose02/BingeBandit/main/Brand/13_App_Icon_Tile_1024x1024.png` |

Then use **Add another Path, Port, Variable, Label or Device** for each row below.

### Paths and port

| What | Container | Host (example) | Notes |
|---|---|---|---|
| Port | `8484` | `8484` | Keep 8484. The TV and phone apps search for servers on this port. |
| Appdata | `/config` | `/mnt/user/appdata/bingebandit` | Holds the database, artwork, caches, logs and backups. |
| Media | `/media` | `/mnt/user/media` | Read-only is fine. Libraries use paths under `/media`. |
| DVR recordings *(optional)* | `/config/recordings` | `/mnt/user/media/recordings` | Read/write. Puts recordings on the array instead of in appdata. |
| Transcode folder *(optional)* | `/config/transcode` | `/tmp/bingebandit-transcode` | `/tmp` is RAM on Unraid. That's fast, but it needs a few GB free. |

Unraid passes your timezone to the container automatically, so the guide and DVR times match your clock.

## 2. First run

1. When the container is running, click its icon and choose **WebUI**, or open `http://<your-unraid-ip>:8484`.
2. The first visit asks you to create the **admin account**.
3. Go to **Server → Libraries → Add library** and pick the folders using the container paths, such as `/media/movies` for Movies and `/media/tv` for TV.
4. Add a free TMDB API key under **Server → Settings** for posters, ratings, cast and trailers.

## 3. Hardware transcoding (optional)

Most TVs play files directly. Hardware transcoding helps phones, browsers and remote streams.

### Intel Quick Sync or AMD

1. **Edit** the container. Under **Show more settings**, set **Intel/AMD GPU** to `/dev/dri`.
   - If you added the container by hand, add a **Device** with value `/dev/dri`.
2. In BingeBandit, go to **Server → Settings**, and under hardware acceleration choose **VAAPI (Linux / Docker / Unraid)**.
3. Some older Intel chips need the **Intel GPU TOP** plugin (from Community Applications) installed on Unraid first.

### NVIDIA

1. Install the **Nvidia Driver** plugin from Community Applications and reboot.
2. **Edit** the container and turn on **Advanced View** (top right):
   - **Extra Parameters:** `--runtime=nvidia`
   - Add a **Variable** `NVIDIA_VISIBLE_DEVICES` set to your GPU's UUID, shown on the Nvidia Driver plugin page (or `all`).
   - Add a **Variable** `NVIDIA_DRIVER_CAPABILITIES` set to `all`.
3. In BingeBandit, go to **Server → Settings**, and under hardware acceleration choose **NVIDIA NVENC**.

## 4. TV and phone apps

1. Download `BingeBandit-TV-<version>.apk` from the [latest release](https://github.com/Spacegoose02/BingeBandit/releases/latest). The same file works on Android TV, Fire TV and Android phones.
2. Install it with the Downloader app (TVs), or open the APK on a phone.
3. Open BingeBandit:
   - **Same network as Unraid:** it finds the server by itself.
   - **Otherwise:** choose **Type an address** and enter `<your-unraid-ip>:8484`.
4. Sign in:
   - **On a TV:** a code appears. Approve it from a phone or computer at `http://<your-unraid-ip>:8484/link`.
   - **On a phone:** sign in with your username and password.
5. **Updates:** later updates appear in the app under **Settings**.

## 5. Watching away from home (optional)

- **Reverse proxy** (Nginx Proxy Manager, SWAG, Traefik and so on): point a hostname at `http://<unraid-ip>:8484`. Then in BingeBandit go to **Server → Settings → Network**:
  - Add the proxy's IP under **Trusted reverse proxies**.
  - Set the **Public URL**.
- **VPN** (Tailscale, WireGuard): use the server's VPN address in the apps. For Tailscale, add `100.64.0.0/10` under **Extra local networks** so those devices count as home.
- **Remote access switch:** if **Allow connections from outside the home network** is off, only devices on your home network can use the server.

## 6. Updating

- **Docker** tab → **Check for Updates**, then **apply update** on BingeBandit. Your appdata is kept.
- To stay on one version instead of `latest`, set the Repository to a version tag, such as `ghcr.io/spacegoose02/bingebandit:0.5.0`.

## 7. Backups

- BingeBandit backs up its database daily, during the maintenance window, to `/config/backups` in your appdata. You can change the folder and how many copies to keep under **Server → Tasks**.
- To go back to a backup, open **Server → Tasks**, scroll to **Restore a backup** and click **Restore** next to it. BingeBandit backs up the current database first, so a restore can be undone the same way. Restart the container afterwards.
- To move to a new server, click **Download** next to a backup, then on the new server's first screen choose **Moving from another server? Restore a backup** and pick the file. Everyone signs in with their old accounts. If the media folders have different paths on the new server, fix them under **Server → Libraries** before scanning. You can also restore a downloaded file later with **Restore from a file**.
- The **Appdata Backup** plugin also covers `/mnt/user/appdata/bingebandit`. Stop the container during its backup, as with any database app.

## Troubleshooting

- **Logs:**
  - Click the container icon and choose **Logs**.
  - Or in BingeBandit, go to **Server → Logs**. The files are also in `appdata/bingebandit/logs`.
- **Libraries show nothing:** check that the library paths start with `/media/...`, the container path, not `/mnt/user/...`.
- **Apps can't find the server:** check that port 8484 is mapped to 8484 and the device is on the same subnet. If not, type the address.
- **Recordings owned by root:** the container runs as root, so new recordings are owned by `root`. Run **Tools → New Permissions** on the recordings share, or this in the Unraid terminal:

  ```bash
  chown -R nobody:users /mnt/user/media/recordings
  ```
