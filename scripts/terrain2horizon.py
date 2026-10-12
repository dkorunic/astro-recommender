#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Write a -horizon file from the terrain around a site.

    scripts/terrain2horizon.py LAT LON [--radius 20] [--zoom 12] [--step 2] > horizon.txt

Standard library only. Reads the free AWS Terrain Tiles (Terrarium PNGs,
elevation = R*256 + G + B/256 - 32768 m; SRTM, GMTED, ETOPO1 and national
DEMs, ~30 m at zoom 12 in mid latitudes), cached under --cache, and marches
a ray every --step degrees of azimuth out to --radius km, keeping the
steepest elevation angle (Earth's curvature dropping a point d²(1-k)/2R, k =
0.13 for refraction). Bare ground only: trees and buildings are not in the
model, so add them to the file by hand. A ridge must rise ~580 m per km to
reach the default -alt-min of 30°, so the file matters mostly with a lower
-alt-min at a mountain site.
"""

import argparse
import array
import math
import os
import struct
import sys
import urllib.request
import zlib

TILE = 256
URL = "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"
R_EARTH = 6371000.0
REFRACTION = 0.13  # standard terrestrial refraction coefficient


def paeth(a, b, c):
    p = a + b - c
    pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
    if pa <= pb and pa <= pc:
        return a
    return b if pb <= pc else c


def decode(data):
    """Elevations in metres of a Terrarium PNG (8-bit RGB, not interlaced)."""
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError("not a PNG")
    pos, idat = 8, []
    while pos < len(data):
        n, kind = struct.unpack(">I4s", data[pos : pos + 8])
        body = data[pos + 8 : pos + 8 + n]
        if kind == b"IHDR":
            w, h, depth, colour, _, _, interlace = struct.unpack(">IIBBBBB", body)
            if (w, h, depth, colour, interlace) != (TILE, TILE, 8, 2, 0):
                raise ValueError(f"unexpected PNG {w}x{h} depth {depth} colour {colour} interlace {interlace}")
        elif kind == b"IDAT":
            idat.append(body)
        pos += 12 + n
    raw, stride = zlib.decompress(b"".join(idat)), TILE * 3
    prev, out = bytearray(stride), array.array("f")
    for row in range(TILE):
        f, line = raw[row * (stride + 1)], bytearray(raw[row * (stride + 1) + 1 : (row + 1) * (stride + 1)])
        for i in range(stride):
            a = line[i - 3] if i >= 3 else 0
            c = prev[i - 3] if i >= 3 else 0
            line[i] = (line[i] + (0, a, prev[i], (a + prev[i]) // 2, paeth(a, prev[i], c))[f]) & 0xFF
        out.extend(line[i] * 256 + line[i + 1] + line[i + 2] / 256 - 32768 for i in range(0, stride, 3))
        prev = line
    return out


class Terrain:
    """Bilinear elevation from Terrarium tiles at one zoom, fetched on demand."""

    def __init__(self, zoom, cache):
        self.zoom, self.cache, self.tiles = zoom, cache, {}

    def tile(self, x, y):
        x %= 1 << self.zoom
        if (x, y) not in self.tiles:
            path = os.path.join(self.cache, str(self.zoom), str(x), f"{y}.png")
            if not os.path.exists(path):
                os.makedirs(os.path.dirname(path), exist_ok=True)
                req = urllib.request.Request(URL.format(z=self.zoom, x=x, y=y), headers={"User-Agent": "astro-recommender terrain2horizon"})
                with urllib.request.urlopen(req, timeout=60) as r:
                    data = r.read()
                with open(path + ".tmp", "wb") as f:
                    f.write(data)
                os.replace(path + ".tmp", path)
            with open(path, "rb") as f:
                self.tiles[x, y] = decode(f.read())
        return self.tiles[x, y]

    def pixel(self, px, py):
        t = self.tile(px // TILE, py // TILE)
        return t[(py % TILE) * TILE + px % TILE]

    def __call__(self, lat, lon):
        n = TILE << self.zoom
        fx = (lon + 180) / 360 * n - 0.5
        s = math.sin(math.radians(lat))
        fy = (0.5 - math.log((1 + s) / (1 - s)) / (4 * math.pi)) * n - 0.5
        x0, y0 = math.floor(fx), min(max(math.floor(fy), 0), n - 2)
        dx, dy = fx - x0, fy - y0
        top = self.pixel(x0, y0) * (1 - dx) + self.pixel(x0 + 1, y0) * dx
        bottom = self.pixel(x0, y0 + 1) * (1 - dx) + self.pixel(x0 + 1, y0 + 1) * dx
        return top * (1 - dy) + bottom * dy


def destination(lat, lon, az, d):
    """The point d metres from (lat, lon) along azimuth az, on a sphere."""
    p, l, a, g = math.radians(lat), math.radians(lon), math.radians(az), d / R_EARTH
    p2 = math.asin(math.sin(p) * math.cos(g) + math.cos(p) * math.sin(g) * math.cos(a))
    l2 = l + math.atan2(math.sin(a) * math.sin(g) * math.cos(p), math.cos(g) - math.sin(p) * math.sin(p2))
    return math.degrees(p2), (math.degrees(l2) + 540) % 360 - 180


def horizon(elev, lat, lon, eye, step, near, radius, spacing):
    """(azimuth, altitude) pairs: the steepest terrain angle seen from eye metres."""
    out = []
    for i in range(round(360 / step)):
        az, best, d = i * step, -90.0, near
        while d <= radius:
            drop = d * d * (1 - REFRACTION) / (2 * R_EARTH)
            best = max(best, math.degrees(math.atan2(elev(*destination(lat, lon, az, d)) - eye - drop, d)))
            d += spacing
        out.append((az, best))
    return out


def selftest():
    # Flat ground from 2 m: the dip peaks where 2/d = d(1-k)/2R, about 7.7 km out.
    flat = horizon(lambda la, lo: 0.0, 45, 16, 2, 90, 100, 20000, 50)
    want = -math.degrees(2 * math.sqrt(2 * (1 - REFRACTION) / (2 * R_EARTH)))
    assert all(abs(alt - want) < 0.005 for _, alt in flat), (flat[0], want)
    # A 1000 m wall 5 km east: atan((1000 - 2 - drop) / 5000) only at 90°.
    wall = horizon(lambda la, lo: 1000.0 if lo > destination(45, 16, 90, 5000)[1] and abs(la - 45) < 0.01 else 0.0, 45, 16, 2, 90, 100, 6000, 25)
    want = math.degrees(math.atan2(998 - 5000**2 * (1 - REFRACTION) / (2 * R_EARTH), 5000))
    assert abs(dict(wall)[90] - want) < 0.3 and dict(wall)[270] < 0, wall
    # A hand-built 256x256 RGB PNG with Sub and Paeth rows round-trips.
    rows = b""
    for y in range(TILE):
        px = bytes(v for x in range(TILE) for v in ((32768 + x + y) >> 8, (x + y) & 0xFF, 128))
        rows += b"\x00" + px if y % 2 else b"\x01" + bytes((px[i] - (px[i - 3] if i >= 3 else 0)) & 0xFF for i in range(len(px)))
    chunk = lambda k, b: struct.pack(">I", len(b)) + k + b + struct.pack(">I", zlib.crc32(k + b))
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", TILE, TILE, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")
    e = decode(png)
    assert e[0] == 0.5 and e[TILE * 3 + 7] == 10.5, (e[0], e[TILE * 3 + 7])
    print("selftest ok")


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("lat", type=float, nargs="?")
    ap.add_argument("lon", type=float, nargs="?")
    ap.add_argument("--radius", type=float, default=20, help="km to march out to (default 20)")
    ap.add_argument("--zoom", type=int, default=12, help="tile zoom, 10-15 (default 12, ~30 m in mid latitudes)")
    ap.add_argument("--step", type=float, default=2, help="degrees of azimuth between rays (default 2)")
    ap.add_argument("--height", type=float, default=2, help="eye height above the ground, m (default 2)")
    ap.add_argument("--elevation", type=float, help="site ground elevation in m, if the model's is off (a valley floor)")
    ap.add_argument("--near", type=float, default=100, help="m from the site where the march starts (default 100)")
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/terrain"))
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        return selftest()
    if args.lat is None or args.lon is None:
        ap.error("LAT and LON are required")
    if not (abs(args.lat) <= 85 and abs(args.lon) <= 180 and 10 <= args.zoom <= 15 and 0 < args.step <= 90 and 0 < args.near < args.radius * 1000):
        ap.error("want |LAT| <= 85, |LON| <= 180, zoom 10-15, step 0-90, near below radius")

    elev = Terrain(args.zoom, args.cache)
    ground = elev(args.lat, args.lon) if args.elevation is None else args.elevation
    spacing = 2 * math.pi * R_EARTH * math.cos(math.radians(args.lat)) / (TILE << args.zoom)  # one pixel
    points = horizon(elev, args.lat, args.lon, ground + args.height, args.step, args.near, args.radius * 1000, spacing)
    print(f"# terrain horizon, {args.radius:g} km at zoom {args.zoom}, ground {ground:.0f} m + {args.height:g} m")
    print("# AWS Terrain Tiles (Mapzen; SRTM, GMTED, ETOPO1 and others); bare ground, add trees and buildings by hand")
    print("# azimuth altitude")
    for az, alt in points:
        print(f"{az:g} {alt:.1f}")
    return None


if __name__ == "__main__":
    sys.exit(main())
