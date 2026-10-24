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

FROM debian:bookworm-slim
# ffmpeg for probing/transcoding; VA-API drivers for Intel/AMD hardware transcoding.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata mesa-va-drivers intel-media-va-driver \
 && rm -rf /var/lib/apt/lists/*
COPY --from=server /bingebandit /usr/local/bin/bingebandit
ENV BB_DATA_DIR=/config BB_ADDR=:8484
VOLUME ["/config"]
EXPOSE 8484
ENTRYPOINT ["/usr/local/bin/bingebandit"]
