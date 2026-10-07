#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
#
# Render demo.jpg: the coloured output of a live run, drawn to an image by
# freeze (github.com/charmbracelet/freeze), scaled and converted to JPEG by
# sips and, when installed, recompressed by jpegoptim at quality 85. Needs
# network for the weather, aerosol and geocoding lookups. Lines wrap at 150
# columns, as in a real terminal; the legend already wraps to the table.
#
# The default run is the Markovac site with -filter and its measured SQM, not
# the README's Zagreb transcript. Any arguments replace that whole flag set,
# so give -lat and -lon again:
#
#   scripts/demo.sh
#   scripts/demo.sh -lat 45.8 -lon 16 -origin
set -euo pipefail

cd "$(dirname "$0")/.."
command -v freeze >/dev/null || { echo "install freeze: go install github.com/charmbracelet/freeze@latest" >&2; exit 1; }

args=(-filter -lat 45.58587022468967 -lon 17.312918864814147 -sqm 21.60)
[ $# -gt 0 ] && args=("$@")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/astro-recommender" .
{
	printf '\033[1;32m❯\033[0m ./astro-recommender %s\n' "${args[*]}"
	CLICOLOR_FORCE=1 "$tmp/astro-recommender" "${args[@]}"
} >"$tmp/demo.ansi"

freeze --execute "cat $tmp/demo.ansi" --background '#1d1f21' --padding 20 --window=false \
	--border.radius 0 --wrap 150 --font.size 14 --line-height 1.35 -o "$tmp/demo.png" </dev/null
sips -Z 2000 -s format jpeg -s formatOptions 95 "$tmp/demo.png" --out demo.jpg >/dev/null
command -v jpegoptim >/dev/null && jpegoptim -q -m 85 --strip-all demo.jpg
ls -l demo.jpg
