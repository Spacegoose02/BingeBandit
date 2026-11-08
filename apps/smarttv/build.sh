#!/bin/sh
# Puts together the Samsung (Tizen) and LG (webOS) apps in dist/: the
# shared page in shell/ plus each TV's manifest and icons. Packaging and
# installing need each maker's tools; see README.md.
set -e
cd "$(dirname "$0")"
rm -rf dist
for tv in tizen webos; do
  mkdir -p dist/$tv
  cp shell/* dist/$tv/
  cp $tv/* dist/$tv/
done
echo "Built dist/tizen and dist/webos."
