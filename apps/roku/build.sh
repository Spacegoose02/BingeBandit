#!/usr/bin/env sh
# Builds the sideloadable channel zip: build/BingeBandit-Roku.zip
set -e
cd "$(dirname "$0")"
mkdir -p build
rm -f build/BingeBandit-Roku.zip
zip -qr build/BingeBandit-Roku.zip manifest source components images fonts -x '*.DS_Store'
echo "build/BingeBandit-Roku.zip"
