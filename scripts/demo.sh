#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
#
# Render demo.jpg: the interactive terminal view of a live run, as a
# terminal with Kitty graphics (Ghostty, Kitty, WezTerm) shows it. The view
# renders one frame without a terminal (ASTRO_RECOMMENDER_SNAPSHOT, at
# COLUMNS x LINES), once the night and the selected target's picture have
# loaded; freeze (github.com/charmbracelet/freeze) draws it, and
# scripts/demo_picture.py pastes the DSS2 picture in its own pixels over its
# half-blocks, which is all text can hold. sips scales and converts to JPEG
# and, when installed, jpegoptim recompresses at quality 85. Needs network
# for the forecasts, geocoding and the picture, and Pillow.
#
# The default run is the Markovac site with -filter dual and its measured SQM, not
# the README's Zagreb transcript. Any arguments replace that whole flag set,
# so give -lat and -lon again:
#
#   scripts/demo.sh
#   scripts/demo.sh -lat 45.8 -lon 16 -scope origin
set -euo pipefail

cd "$(dirname "$0")/.."
command -v freeze >/dev/null || { echo "install freeze: go install github.com/charmbracelet/freeze@latest" >&2; exit 1; }

args=(-scope origin -filter dual -lat 45.58587022468967 -lon 17.312918864814147 -sqm 21.60)
[ $# -gt 0 ] && args=("$@")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/astro-recommender" .
cols=180 lines=50 size=14 lh=1.35 pad=20
{
	printf '\033[1;32m❯\033[0m ./astro-recommender %s\n' "${args[*]}"
	COLUMNS=$cols LINES=$lines ASTRO_RECOMMENDER_SNAPSHOT="$tmp/frame.ansi" "$tmp/astro-recommender" "${args[@]}" </dev/null >/dev/null
	cat "$tmp/frame.ansi"
} >"$tmp/demo.ansi"

freeze --execute "cat $tmp/demo.ansi" --background '#111111' --padding $pad --window=false \
	--border.radius 0 --wrap $cols --font.size $size --line-height $lh -o "$tmp/demo.png" </dev/null
# One line, the prompt, above the frame.
python3 -I scripts/demo_picture.py "$tmp/demo.png" "$tmp/frame.ansi.json" 1 $size $lh $pad $cols
sips -Z 2000 -s format jpeg -s formatOptions 95 "$tmp/demo.png" --out demo.jpg >/dev/null
command -v jpegoptim >/dev/null && jpegoptim -q -m 85 --strip-all demo.jpg
ls -l demo.jpg
