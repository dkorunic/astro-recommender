#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Paste the selected target's DSS2 picture, in its own pixels, over the
half-block picture of a terminal-view snapshot that freeze has drawn, as
Kitty graphics show it in a terminal that speaks them (freeze draws text
only).

    demo_picture.py FRAME.png SNAPSHOT.json ROW_OFFSET FONT_SIZE LINE_HEIGHT PADDING COLUMNS

SNAPSHOT.json is the TUI's (ASTRO_RECOMMENDER_SNAPSHOT): the picture's cell
rectangle and hips2fits URL; ROW_OFFSET the lines freeze drew above the
snapshot (the prompt). The cell geometry is freeze v0.2.2's: a cell is
FONT_SIZE / 1.68 wide and FONT_SIZE x LINE_HEIGHT tall from PADDING, and the
PNG is the SVG scaled by a whole factor, read from the image's width
against the SVG's (2 x PADDING + (COLUMNS + 1) cells, COLUMNS the widest
line). The picture is fetched
again at the rectangle's pixel size, fitted inside it and centred, as the
view fits it. Pillow; network.
"""

import io
import json
import re
import sys
import urllib.parse
import urllib.request

from PIL import Image

FONT_RATIO = 1.68  # freeze's fontHeightToWidthRatio


def main():
    frame, rect_file, row_off, size, line_height, pad, cols = sys.argv[1:8]
    row_off, size, line_height, pad, cols = int(row_off), float(size), float(line_height), float(pad), int(cols)
    with open(rect_file) as f:
        rect = json.load(f)
    if not rect.get("url"):
        print("demo_picture: no picture in the snapshot, left as it is", file=sys.stderr)
        return
    img = Image.open(frame).convert("RGB")
    cw, lh = size / FONT_RATIO, size * line_height
    scale = img.width / (2 * pad + (cols + 1) * cw)
    x0 = round((pad + rect["col"] * cw) * scale)
    y0 = round((pad + (rect["row"] + row_off) * lh) * scale)
    w, h = round(rect["cols"] * cw * scale), round(rect["rows"] * lh * scale)
    url = re.sub(r"width=\d+&height=\d+", f"width={w}&height={h}", rect["url"])
    if urllib.parse.urlparse(url).netloc != "alaskybis.cds.unistra.fr":
        sys.exit(f"demo_picture: unexpected picture host in {url}")
    req = urllib.request.Request(url, headers={"User-Agent": "astro-recommender demo"})
    with urllib.request.urlopen(req, timeout=60) as r:
        pic = Image.open(io.BytesIO(r.read())).convert("RGB")
    pic.thumbnail((w, h), Image.Resampling.LANCZOS)
    # The half-blocks go: the rectangle takes the background beside it,
    # a quarter line lower too, where freeze's glyphs reach past their cell.
    img.paste(img.getpixel((max(0, x0 - 2), y0)), (x0, y0, x0 + w, y0 + h + round(lh * scale / 4)))
    img.paste(pic, (x0 + (w - pic.width) // 2, y0 + (h - pic.height) // 2))
    img.save(frame)
    print(f"demo_picture: {pic.width}x{pic.height} px pasted at {x0},{y0}", file=sys.stderr)


if __name__ == "__main__":
    main()
