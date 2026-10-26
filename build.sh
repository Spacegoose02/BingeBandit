#!/usr/bin/env sh
# Builds the web UI and the server into dist/bingebandit (Linux/macOS).
# Usage: ./build.sh [version]
set -eu
VERSION="${1:-dev}"
ROOT="$(cd "$(dirname "$0")" && pwd)"

(cd "$ROOT/web" && { [ -d node_modules ] || npm ci; } && npm run build)
(cd "$ROOT/server" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$ROOT/dist/bingebandit" ./cmd/bingebandit)
# Bundled plugins go in dist/plugins, which the server reads by default.
for p in watchlist-rss hue-sync cinemask-upgrades game-lights; do
  mkdir -p "$ROOT/dist/plugins/$p"
  (cd "$ROOT/server" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/plugins/$p/$p" "./plugins/$p")
  cp "$ROOT/server/plugins/$p/plugin.json" "$ROOT/dist/plugins/$p/"
done

echo "Built dist/bingebandit ($VERSION)"
