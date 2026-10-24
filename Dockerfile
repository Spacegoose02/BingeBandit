# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY server/internal/web/dist/.gitkeep /src/server/internal/web/dist/.gitkeep
RUN npm run build

FROM golang:1.27-alpine AS server
ARG VERSION=dev
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /bingebandit ./cmd/bingebandit
# Bundled plugins: each gets its own folder with its plugin.json. They're off until an admin turns them on.
RUN for p in watchlist-rss hue-sync cinemask-upgrades; do \
      mkdir -p /plugins/$p \
      && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /plugins/$p/$p ./plugins/$p \
      && cp plugins/$p/plugin.json /plugins/$p/ || exit 1; \
    done

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
ENV BB_DATA_DIR=/config BB_ADDR=:8484 BB_PLUGINS_DIR=/usr/local/lib/bingebandit/plugins
VOLUME ["/config"]
EXPOSE 8484
ENTRYPOINT ["/usr/local/bin/bingebandit"]
