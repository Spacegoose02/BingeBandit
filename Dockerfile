# syntax=docker/dockerfile:1

# The web UI and Go binaries are built on the runner's own platform and
# cross-compiled, not under QEMU: npm ci under arm64 emulation hangs.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY server/internal/web/dist/.gitkeep /src/server/internal/web/dist/.gitkeep
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS server
ARG VERSION=dev
ARG TARGETOS TARGETARCH
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /bingebandit ./cmd/bingebandit
# Bundled plugins: each gets its own folder with its plugin.json. They're off until an admin turns them on.
RUN for p in watchlist-rss hue-sync cinemask-upgrades game-lights youtube-live; do \
      mkdir -p /plugins/$p \
      && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /plugins/$p/$p ./plugins/$p \
      && { echo '{'; echo "  \"version\": \"${VERSION#v}\","; tail -n +2 plugins/$p/plugin.json; } > /plugins/$p/plugin.json || exit 1; \
    done

# yt-dlp finds the HLS address of YouTube live streams (the YouTube Live
# plugin's channels). The standalone build needs no Python; the plugin runs
# "yt-dlp -U" daily, since YouTube changes often. YouTube only hands out
# stream addresses after a JavaScript challenge, which yt-dlp solves with
# Deno (its default runtime).
FROM alpine:3.20 AS ytdlp
ARG TARGETARCH
RUN case "$TARGETARCH" in arm64) f=yt-dlp_linux_aarch64; d=aarch64 ;; *) f=yt-dlp_linux; d=x86_64 ;; esac \
 && wget -qO /yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/latest/download/$f" \
 && chmod +x /yt-dlp \
 && wget -qO /deno.zip "https://github.com/denoland/deno/releases/latest/download/deno-$d-unknown-linux-gnu.zip" \
 && unzip -q /deno.zip -d / && chmod +x /deno && rm /deno.zip

FROM debian:bookworm-slim
ARG TARGETARCH
# ffmpeg for probing/transcoding; VA-API drivers for AMD (mesa) and, on x86
# only since the package doesn't exist for arm64, Intel hardware transcoding.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata mesa-va-drivers \
 && if [ "$TARGETARCH" = "amd64" ]; then apt-get install -y --no-install-recommends intel-media-va-driver; fi \
 && rm -rf /var/lib/apt/lists/*
COPY --from=server /bingebandit /usr/local/bin/bingebandit
COPY --from=server /plugins /usr/local/lib/bingebandit/plugins
COPY --from=ytdlp /yt-dlp /deno /usr/local/bin/
ENV BB_DATA_DIR=/config BB_ADDR=:8484 BB_PLUGINS_DIR=/usr/local/lib/bingebandit/plugins
VOLUME ["/config"]
EXPOSE 8484
ENTRYPOINT ["/usr/local/bin/bingebandit"]
