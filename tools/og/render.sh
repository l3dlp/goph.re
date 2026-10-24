#!/usr/bin/env bash
# Renders the OpenGraph cards into assets/gfx/og/*.jpg.
# Needs a Chromium build with WebGL (e.g. Playwright's chrome-headless-shell) and ImageMagick.
set -euo pipefail
cd "$(dirname "$0")/../.."
CHROME=${CHROME:-$(ls ~/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell | tail -1)}
tmp=$(mktemp -d)
shot() { # url out
	"$CHROME" --no-sandbox --hide-scrollbars --allow-file-access-from-files \
		--use-angle=swiftshader --enable-unsafe-swiftshader \
		--virtual-time-budget=8000 --window-size=1200,630 \
		--screenshot="$tmp/card.png" "$1" >/dev/null 2>&1
	convert "$tmp/card.png" -strip -quality 86 "$2"
	echo "$2"
}
for v in feed pulse me picks golang experts blogs podcasts community forums all; do
	"$CHROME" --no-sandbox --hide-scrollbars --allow-file-access-from-files \
		--use-angle=swiftshader --enable-unsafe-swiftshader \
		--virtual-time-budget=8000 --window-size=1200,630 \
		--screenshot="$tmp/$v.png" "file://$PWD/tools/og/og.html?v=$v" >/dev/null 2>&1
	convert "$tmp/$v.png" -strip -quality 86 "assets/gfx/og/$v.jpg"
	echo "assets/gfx/og/$v.jpg"
done
# One card per pick: front matter keys og_title, og_sub, og_chips (comma separated).
mkdir -p assets/gfx/og/picks
for f in picks/*.md picks/drafts/*.md; do
	slug=$(basename "$f" .md)
	get() { sed -n "/^---$/,/^---$/{s/^$1: *//p}" "$f" | head -1; }
	title=$(get og_title); [ -z "$title" ] && title=$(get title)
	q=$(python3 -c 'import sys,urllib.parse as u;print(u.urlencode({"v":"custom","kicker":sys.argv[1],"title":sys.argv[2],"sub":sys.argv[3],"chips":sys.argv[4],"stamp":"1"}))' \
		"goph.re/picks · $(get og_kicker)" "$title" "$(get og_sub)" "$(get og_chips)")
	shot "file://$PWD/tools/og/og.html?$q" "assets/gfx/og/picks/$slug.jpg"
done
rm -rf "$tmp"
