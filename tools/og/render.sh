#!/usr/bin/env bash
# Renders the OpenGraph cards into assets/gfx/og/*.jpg.
# Needs a Chromium build with WebGL (e.g. Playwright's chrome-headless-shell) and ImageMagick.
set -euo pipefail
cd "$(dirname "$0")/../.."
CHROME=${CHROME:-$(ls ~/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell | tail -1)}
tmp=$(mktemp -d)
for v in feed pulse me golang experts blogs podcasts community forums all; do
	"$CHROME" --no-sandbox --hide-scrollbars --allow-file-access-from-files \
		--use-angle=swiftshader --enable-unsafe-swiftshader \
		--virtual-time-budget=8000 --window-size=1200,630 \
		--screenshot="$tmp/$v.png" "file://$PWD/tools/og/og.html?v=$v" >/dev/null 2>&1
	convert "$tmp/$v.png" -strip -quality 86 "assets/gfx/og/$v.jpg"
	echo "assets/gfx/og/$v.jpg"
done
rm -rf "$tmp"
