#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Write a -horizon file from the terrain around a site.

    scripts/terrain2horizon.py LAT LON [--radius 20] [--zoom 12] [--step 2] [--no-osm] [--no-canopy] > horizon.txt

Standard library only. Reads the free AWS Terrain Tiles (Terrarium PNGs,
elevation = R*256 + G + B/256 - 32768 m; SRTM, GMTED, ETOPO1 and national
DEMs, ~30 m at zoom 12 in mid latitudes), cached under --cache, and marches
a ray every --step degrees of azimuth out to --radius km, keeping the
steepest elevation angle (Earth's curvature dropping a point d²(1-k)/2R, k =
0.13 for refraction). The terrain model is bare ground, so the script
also adds the buildings OpenStreetMap maps within --osm-radius (Overpass
API, cached under --cache), at their tagged height or else
--building-height, and the trees from the Meta/WRI 1 m canopy height map
(Tolan et al. 2024; CC BY 4.0; Cloud-Optimized GeoTIFFs on AWS, read by
range requests, cached), which measures every tree, garden ones too, as of
its 2009-2020 imagery. --no-canopy takes the trees and woods from OSM
instead, at --tree-height and --wood-height unless tagged (OSM seldom tags
heights and rarely maps garden trees); --no-osm --no-canopy is bare ground.
Check the result and edit the file by hand. A ridge must rise ~580 m per km
to reach the default -alt-min of 30°, so the terrain alone matters mostly
with a lower -alt-min at a mountain site; a building or tree edge within a
few tens of metres can block more than that.
"""

import argparse
import array
import itertools
import json
import math
import os
import re
import struct
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib

TILE = 256
URL = "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"
R_EARTH = 6371000.0
REFRACTION = 0.13  # standard terrestrial refraction coefficient
OVERPASS = "https://overpass-api.de/api/interpreter"
OSM_MAX_AGE = 30 * 86400  # s a cached Overpass answer is reused
TREE_RADIUS = 3.0  # m, a single tree's crown
USER_AGENT = "astro-recommender terrain2horizon"
CHM_URL = "https://dataforgood-fb-data.s3.amazonaws.com/forests/v1/alsgedi_global_v6_float/chm/{}.tif"
CHM_ZOOM, CHM_TILE = 9, 65536  # quadkey zoom of the canopy tiles, and their width in px (1.19 m at the equator)


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
                req = urllib.request.Request(URL.format(z=self.zoom, x=x, y=y), headers={"User-Agent": USER_AGENT})
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


def get_range(url, start, n):
    """n bytes of url from start, by an HTTP range request."""
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT, "Range": f"bytes={start}-{start + n - 1}"})
    with urllib.request.urlopen(req, timeout=120) as r:
        data = r.read()
    if len(data) != n:
        raise ValueError(f"{url}: asked for {n} bytes at {start}, got {len(data)}")
    return data


class Strips:
    """Rows of a striped one-band 8-bit TIFF or BigTIFF read through get(start, n).

    Enough TIFF for the canopy map's Cloud-Optimized GeoTIFFs: classic or
    BigTIFF, either byte order, strips (not tiles), no or Deflate
    compression, no or horizontal-difference predictor; anything else is a
    ValueError. Only the strips asked for are fetched.
    """

    TYPES = {3: "H", 4: "I", 16: "Q"}  # SHORT, LONG, LONG8: all an offset or count can be

    def __init__(self, get):
        self.get = get
        head = get(0, 16)
        self.bo = {b"II": "<", b"MM": ">"}.get(head[:2])
        if self.bo is None:
            raise ValueError("not a TIFF")
        magic = struct.unpack(self.bo + "H", head[2:4])[0]
        if magic == 42:
            self.off, cnt, entry, ifd = "I", "H", 12, struct.unpack(self.bo + "I", head[4:8])[0]
        elif magic == 43:
            self.off, cnt, entry, ifd = "Q", "Q", 20, struct.unpack(self.bo + "Q", head[8:16])[0]
        else:
            raise ValueError(f"TIFF magic {magic}")
        size = struct.calcsize(cnt)
        n = struct.unpack(self.bo + cnt, get(ifd, size))[0]
        body, self.tags = get(ifd + size, n * entry), {}
        for i in range(n):
            e = body[i * entry : (i + 1) * entry]
            tag, typ = struct.unpack(self.bo + "HH", e[:4])
            count = struct.unpack(self.bo + self.off, e[4 : 4 + struct.calcsize(self.off)])[0]
            self.tags[tag] = (typ, count, e[4 + struct.calcsize(self.off) :])
        self.width, self.height = self.scalar(256), self.scalar(257)
        self.rps = self.scalar(278, self.height)
        fields = {"bits": (258, 8), "samples": (277, 1), "compression": (259, 1), "predictor": (317, 1), "planar": (284, 1)}
        got = {k: self.scalar(t, d) for k, (t, d) in fields.items()}
        if 322 in self.tags or got["bits"] != 8 or got["samples"] != 1 or got["compression"] not in (1, 8, 32946) or got["predictor"] not in (1, 2):
            raise ValueError(f"unsupported TIFF layout {got}{' (tiled)' if 322 in self.tags else ''}")
        self.deflate, self.predictor = got["compression"] != 1, got["predictor"] == 2

    def array(self, tag, first, n):
        """Values first..first+n of an offset or count array tag."""
        typ, count, value = self.tags[tag]
        fmt = self.TYPES[typ]
        size = struct.calcsize(fmt)
        if count * size <= len(value):
            return struct.unpack(f"{self.bo}{count}{fmt}", value[: count * size])[first : first + n]
        at = struct.unpack(self.bo + self.off, value[: struct.calcsize(self.off)])[0]
        return struct.unpack(f"{self.bo}{n}{fmt}", self.get(at + first * size, n * size))

    def scalar(self, tag, default=None):
        if tag not in self.tags:
            if default is None:
                raise ValueError(f"TIFF tag {tag} missing")
            return default
        return self.array(tag, 0, 1)[0]

    def rows(self, r0, r1, c0, c1):
        """Rows r0..r1-1, each cut to columns c0..c1-1, as bytes."""
        s0, s1 = r0 // self.rps, (r1 - 1) // self.rps + 1
        offsets, counts = self.array(273, s0, s1 - s0), self.array(279, s0, s1 - s0)
        lo = min(offsets)
        blob = self.get(lo, max(o + c for o, c in zip(offsets, counts)) - lo)  # COG strips lie in order: one request
        out = []
        for i, (o, c) in enumerate(zip(offsets, counts)):
            data = blob[o - lo : o - lo + c]
            if self.deflate:
                data = zlib.decompress(data)
            for j in range(len(data) // self.width):
                row = (s0 + i) * self.rps + j
                if r0 <= row < r1:
                    line = data[j * self.width : (j + 1) * self.width]
                    if self.predictor:  # each byte the difference from its left neighbour, mod 256
                        line = bytes(v & 0xFF for v in itertools.accumulate(line[:c1]))
                    out.append(bytes(line[c0:c1]))
        return out


def quadkey(x, y, zoom):
    return "".join(str(((x >> i) & 1) | (((y >> i) & 1) << 1)) for i in range(zoom - 1, -1, -1))


def canopy(lat, lon, radius, cache):
    """Canopy height in m at (x, y) m east and north of the site, from the
    Meta/WRI map's window of radius m around it (0 off the map)."""
    n = CHM_TILE << CHM_ZOOM
    s = math.sin(math.radians(lat))
    gx = (lon + 180) / 360 * n
    gy = (0.5 - math.log((1 + s) / (1 - s)) / (4 * math.pi)) * n
    mpp = 2 * math.pi * R_EARTH * math.cos(math.radians(lat)) / n  # ground m per px
    half = math.ceil(radius / mpp) + 1
    x0, y0, size = int(gx) - half, int(gy) - half, 2 * half + 1
    path = os.path.join(cache, "canopy", f"{x0}_{y0}_{size}.bin")
    if os.path.exists(path):
        with open(path, "rb") as f:
            grid = f.read()
    else:
        grid = bytearray(size * size)
        for ty in range(y0 // CHM_TILE, (y0 + size - 1) // CHM_TILE + 1):
            for tx in range(x0 // CHM_TILE, (x0 + size - 1) // CHM_TILE + 1):
                url = CHM_URL.format(quadkey(tx, ty, CHM_ZOOM))
                r0, r1 = max(y0, ty * CHM_TILE), min(y0 + size, (ty + 1) * CHM_TILE)
                c0, c1 = max(x0, tx * CHM_TILE), min(x0 + size, (tx + 1) * CHM_TILE)
                try:
                    tif = Strips(lambda a, k, url=url: get_range(url, a, k))
                except urllib.error.HTTPError as e:
                    if e.code not in (403, 404):
                        raise
                    print(f"canopy: no tile {url.rsplit('/', 1)[1]} (sea or outside the map), taken as bare", file=sys.stderr)
                    continue
                if (tif.width, tif.height) != (CHM_TILE, CHM_TILE):
                    raise ValueError(f"canopy tile {tif.width}x{tif.height}, want {CHM_TILE}")
                for i, line in enumerate(tif.rows(r0 - ty * CHM_TILE, r1 - ty * CHM_TILE, c0 - tx * CHM_TILE, c1 - tx * CHM_TILE)):
                    at = (r0 - y0 + i) * size + c0 - x0
                    grid[at : at + len(line)] = line
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path + ".tmp", "wb") as f:
            f.write(grid)
        os.replace(path + ".tmp", path)

    def height(x, y):
        col, row = round(gx + x / mpp) - x0, round(gy - y / mpp) - y0
        return grid[row * size + col] if 0 <= col < size and 0 <= row < size else 0

    return height, mpp


def canopy_horizon(height, azimuths, rise, near, radius, spacing):
    """The steepest treetop along each azimuth from near to radius m, -90 where none is."""
    out = []
    for az in azimuths:
        dx, dy = math.sin(math.radians(az)), math.cos(math.radians(az))
        best, d = -90.0, near
        while d <= radius:
            h = height(d * dx, d * dy)
            if h >= 1:  # below a metre is the map's noise over open ground
                best = max(best, math.degrees(math.atan2(rise(az, d) + h, d)))
            d += spacing
        out.append(best)
    return out


def overpass(lat, lon, radius, cache):
    """Overpass JSON (out geom) of the buildings, trees and woods within radius m."""
    path = os.path.join(cache, "osm", f"{lat:.5f}_{lon:.5f}_{radius:g}.json")
    if os.path.exists(path) and time.time() - os.path.getmtime(path) < OSM_MAX_AGE:
        with open(path, "rb") as f:
            return json.load(f)
    around = f"(around:{radius:g},{lat:.6f},{lon:.6f})"
    # Small limits: a busy Overpass refuses (504) a query whose declared time
    # or memory it cannot reserve, and the defaults (180 s, 512 MiB) were
    # refused where 25 s and 64 MiB ran in 1-2 s (city centre: 0.5 MB).
    query = f"""[out:json][timeout:25][maxsize:67108864];
(
  way["building"]{around};
  relation["building"]{around};
  node["natural"="tree"]{around};
  way["natural"="tree_row"]{around};
  way["natural"="wood"]{around};
  relation["natural"="wood"]{around};
  way["landuse"="forest"]{around};
  relation["landuse"="forest"]{around};
);
out geom;"""
    req = urllib.request.Request(OVERPASS, data=urllib.parse.urlencode({"data": query}).encode(), headers={"User-Agent": USER_AGENT})
    # A busy server refuses with 429 or 504 (no free slot); it asks clients
    # to wait and try again rather than give up or hammer it.
    for wait in (10, 20, 40, None):
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                data = r.read()
            break
        except urllib.error.HTTPError as e:
            if e.code not in (429, 504) or wait is None:
                raise
            print(f"overpass: HTTP {e.code}, busy; retrying in {wait} s", file=sys.stderr)
            time.sleep(wait)
    osm = json.loads(data)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path + ".tmp", "wb") as f:
        f.write(data)
    os.replace(path + ".tmp", path)
    return osm


def metres(v):
    """A height tag in metres ("12", "12 m", "40'", "40 ft"), None when unreadable or not positive."""
    m = re.match(r"\s*(\d+(?:\.\d+)?)\s*(m|ft|')?\s*$", v or "")
    if not m:
        return None
    h = float(m[1]) * (0.3048 if m[2] in ("ft", "'") else 1)
    return h if 0 < h < 1000 else None


def tag_height(tags):
    """The height a feature's tags give: height, else building:levels at 3 m plus 2 m of roof; None without either."""
    if h := metres(tags.get("height")):
        return h
    levels = metres(tags.get("building:levels"))
    return levels * 3 + 2 if levels else None


def kind_of(tags):
    if "building" in tags:
        return "building"
    if tags.get("natural") in ("tree", "tree_row"):
        return "tree"
    return "wood"


def inside(x, y, segments):
    """Ray-casting parity of (x, y) against the segments of closed rings."""
    n = 0
    for (x1, y1), (x2, y2) in segments:
        if (y1 > y) != (y2 > y) and x < x1 + (y - y1) * (x2 - x1) / (y2 - y1):
            n += 1
    return n % 2 == 1


def obstacles(osm, lat, lon, heights):
    """Outlines and trees in metres east and north of the site, with their heights.

    Returns (outlines, trees, stats): outlines as (height, segments), trees
    as (x, y, radius, height) (a tree row as trees along its line, one per
    crown), and per kind the count and how many had a tag height. An outline
    holding the site (your own house, or a wood you stand in) is left out.
    Inner rings are left out too: a ray's steepest crossing is the outer
    edge it meets first.
    """
    ky = math.pi / 180 * R_EARTH
    kx = ky * math.cos(math.radians(lat))
    xy = lambda p: ((p["lon"] - lon) * kx, (p["lat"] - lat) * ky)
    outlines, trees, stats = [], [], {k: [0, 0, 0] for k in ("building", "tree", "wood")}  # found, tagged, holding the site
    for e in osm.get("elements", []):
        tags = e.get("tags", {})
        kind = kind_of(tags)
        tagged = tag_height(tags)
        h = tagged or heights[kind]
        st = stats[kind]
        if e["type"] == "node":
            trees.append((*xy(e), TREE_RADIUS, h))
        elif kind == "tree":  # tree_row
            pts = [xy(p) for p in e.get("geometry", [])]
            for (x1, y1), (x2, y2) in zip(pts, pts[1:]):
                n = max(1, round(math.hypot(x2 - x1, y2 - y1) / (2 * TREE_RADIUS)))
                trees.extend((x1 + (x2 - x1) * i / n, y1 + (y2 - y1) * i / n, TREE_RADIUS, h) for i in range(n + 1))
        else:
            ways = [e.get("geometry", [])] if e["type"] == "way" else [m.get("geometry", []) for m in e.get("members", []) if m.get("role") == "outer"]
            segments = []
            for way in ways:
                pts = [xy(p) for p in way]
                segments += zip(pts, pts[1:])
            if not segments:
                continue
            if inside(0, 0, segments):
                st[2] += 1
                continue
            outlines.append((h, segments))
        st[0] += 1
        st[1] += tagged is not None
    return outlines, trees, stats


def obstacle_horizon(outlines, trees, azimuths, rise):
    """The steepest obstacle top seen along each azimuth, -90 where none is.

    rise(az, d) is the ground d m out along az, in m above the eye (negative
    below it), so an obstacle stands on the terrain.
    """
    out = []
    for az in azimuths:
        dx, dy = math.sin(math.radians(az)), math.cos(math.radians(az))
        hits = []  # (distance, height)
        for h, segments in outlines:
            for (x1, y1), (x2, y2) in segments:
                ex, ey = x2 - x1, y2 - y1
                den = dx * ey - dy * ex
                if den == 0:
                    continue
                t = (x1 * ey - y1 * ex) / den  # along the ray
                u = (x1 * dy - y1 * dx) / den  # along the segment
                if t > 0 and 0 <= u <= 1:
                    hits.append((t, h))
        for x, y, r, h in trees:
            along = x * dx + y * dy
            off2 = r * r - (x * dy - y * dx) ** 2
            if along > 0 and off2 >= 0 and along - math.sqrt(off2) > 0:
                hits.append((along - math.sqrt(off2), h))
        out.append(max((math.degrees(math.atan2(rise(az, d) + h, d)) for d, h in hits), default=-90.0))
    return out


LEGEND = {"T": "terrain", "B": "OSM building", "O": "OSM building, tree or wood", "t": "canopy-map tree"}


def plot(points, sources, floor=30.0, width=72):
    """A panorama of the horizon as text: altitude against azimuth from north
    through east, 5° a row (10° above 50°) in eighth-row blocks, the -alt-min
    floor dotted, and under each column the source that sets it (LEGEND)
    where the horizon is above 0°, then the legend of those shown."""
    cols = [-90.0] * width
    src = [" "] * width
    for (az, alt), s in zip(points, sources):
        c = int(az % 360 / 360 * width)
        if alt > cols[c]:
            cols[c], src[c] = alt, s
    top = min(90, math.ceil(max(floor, max(cols)) / 10) * 10)
    step = 5 if top <= 50 else 10
    rows = top // step
    lines = []
    for r in range(rows, 0, -1):
        lo = (r - 1) * step  # the row spans lo..lo+step
        cells = []
        for alt in cols:
            eighths = round((alt - lo) / step * 8)
            if eighths >= 8:
                cells.append("█")
            elif eighths > 0:
                cells.append(" ▁▂▃▄▅▆▇"[eighths])
            else:
                cells.append("·" if lo < floor <= lo + step and r > 1 else " ")
        label = f"{lo + step:3.0f}°" if (lo + step) % 10 == 0 else "    "
        lines.append(f"{label}│{''.join(cells)}")
    lines.append("    └" + "─" * width)
    axis = [" "] * width
    for i, name in enumerate("NESW"):
        axis[i * width // 4] = name
    lines.append("     " + "".join(axis))
    shown = "".join(s if c > 0 else " " for s, c in zip(src, cols))
    lines.append("     " + shown)
    used = ", ".join(f"{k} {v}" for k, v in LEGEND.items() if k in shown)
    lines.append(f"     {used}{'; ' if used else ''}dots: -alt-min {floor:g}°")
    return "\n".join(lines)


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
    # OSM: a 9 m (3-level) house 10-20 m east, a 12 m tree 30 m north, and a
    # building around the site itself (left out), on flat ground 2 m below the eye.
    ky = math.pi / 180 * R_EARTH
    kx = ky * math.cos(math.radians(45))
    ll = lambda x, y: {"lat": 45 + y / ky, "lon": 16 + x / kx}
    square = lambda x0, y0, x1, y1: [ll(x0, y0), ll(x1, y0), ll(x1, y1), ll(x0, y1), ll(x0, y0)]
    osm = {"elements": [
        {"type": "way", "tags": {"building": "house", "building:levels": "3"}, "geometry": square(10, -5, 20, 5)},
        {"type": "node", "tags": {"natural": "tree"}, **ll(0, 30)},
        {"type": "way", "tags": {"building": "yes"}, "geometry": square(-4, -4, 4, 4)},
    ]}
    outlines, trees, stats = obstacles(osm, 45, 16, {"building": 8, "tree": 12, "wood": 20})
    assert stats == {"building": [1, 1, 1], "tree": [1, 0, 0], "wood": [0, 0, 0]}, stats
    alts = dict(zip((0, 90, 180), obstacle_horizon(outlines, trees, (0, 90, 180), lambda az, d: -2.0)))
    assert abs(alts[90] - math.degrees(math.atan2(9, 10))) < 0.01, alts
    assert abs(alts[0] - math.degrees(math.atan2(10, 30 - TREE_RADIUS))) < 0.01, alts
    assert alts[180] == -90.0, alts
    assert metres("40'") == 40 * 0.3048 and metres("12 m") == 12 and metres("tall") is None and metres("0") is None
    # A BigTIFF of 3 rows a strip, Deflate with the horizontal predictor,
    # and a classic one uncompressed: both give back the rows asked for.
    w, h = 300, 7
    rows = [bytes((x * 7 + y * 13) & 0xFF for x in range(w)) for y in range(h)]
    for big in (True, False):
        strips = []
        for y in range(0, h, 3):
            raw = b"".join(bytes((r[x] - (r[x - 1] if x else 0)) & 0xFF for x in range(w)) if big else r for r in rows[y : y + 3])
            strips.append(zlib.compress(raw) if big else raw)
        ns = len(strips)
        off, cnt, entry, typ, size = ("Q", "Q", 20, 16, 8) if big else ("I", "H", 12, 4, 4)
        tags = [(256, 3, w), (257, 3, h), (258, 3, 8), (259, 3, 8 if big else 1), (277, 3, 1), (278, 3, 3), (317, 3, 2 if big else 1)]
        hdr = b"II" + (struct.pack("<HHHQ", 43, 8, 0, 16) if big else struct.pack("<HI", 42, 8))
        ifd = len(hdr)
        arrays = ifd + struct.calcsize(cnt) + entry * (len(tags) + 2) + struct.calcsize(off)
        data = arrays + 2 * ns * size
        offsets, at = [], data
        for st in strips:
            offsets.append(at)
            at += len(st)
        pad = lambda v, f: struct.pack("<" + f, v).ljust(entry - 4 - struct.calcsize(off), b"\0")
        body = b"".join(struct.pack("<HH" + off, t, ty, 1) + pad(v, "H") for t, ty, v in tags)
        body += struct.pack("<HH" + off, 273, typ, ns) + pad(arrays, off) + struct.pack("<HH" + off, 279, typ, ns) + pad(arrays + ns * size, off)
        blob = hdr + struct.pack("<" + cnt, len(tags) + 2) + body + struct.pack("<" + off, 0)
        blob += struct.pack(f"<{ns}{off}", *offsets) + struct.pack(f"<{ns}{off}", *map(len, strips)) + b"".join(strips)
        tif = Strips(lambda a, k: blob[a : a + k])
        assert tif.rows(2, 6, 100, 250) == [r[100:250] for r in rows[2:6]], big
    # A 15 m tree 20-24 m east on flat ground 2 m below the eye; nothing west.
    alts = canopy_horizon(lambda x, y: 15 if 20 <= x <= 24 and abs(y) < 2 else 0, (90, 270), lambda az, d: -2.0, 2, 300, 0.5)
    assert abs(alts[0] - math.degrees(math.atan2(13, 20))) < 0.01 and alts[1] == -90.0, alts
    assert quadkey(3, 5, 3) == "213", quadkey(3, 5, 3)
    # The plot: a 40° building wall east, 10° terrain elsewhere.
    pts = [(az, 40.0 if 80 <= az <= 100 else 10.0) for az in range(0, 360, 2)]
    text = plot(pts, ["B" if alt == 40 else "T" for _, alt in pts]).split("\n")
    east = 90 * 72 // 360 + 5
    assert len(text) == 12 and text[0].startswith(" 40°") and text[0][east] == "█" and text[0][5] == " " and text[-3][5:].startswith("N"), text
    assert text[-2][east] == "B" and text[-2][5] == "T" and text[2].startswith(" 30°│··"), text
    assert text[-1] == "     T terrain, B OSM building; dots: -alt-min 30°", text[-1]
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
    ap.add_argument("--osm", action=argparse.BooleanOptionalAction, default=True, help="add OpenStreetMap buildings, and with --no-canopy its trees and woods (Overpass API; default on)")
    ap.add_argument("--osm-radius", type=float, default=300, help="m around the site to fetch them from (default 300)")
    ap.add_argument("--building-height", type=float, default=8, help="m, a building without height or building:levels tags (default 8)")
    ap.add_argument("--tree-height", type=float, default=12, help="m, a tree or tree row without a height tag (default 12)")
    ap.add_argument("--wood-height", type=float, default=20, help="m, a wood or forest without a height tag (default 20)")
    ap.add_argument("--canopy", action=argparse.BooleanOptionalAction, default=True, help="add trees from the Meta/WRI 1 m canopy height map instead of OSM's (default on)")
    ap.add_argument("--canopy-radius", type=float, default=300, help="m around the site to read the canopy map for (default 300)")
    ap.add_argument("--canopy-near", type=float, default=3, help="m from the site where the canopy march starts, past a tree you stand under (default 3)")
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/terrain"))
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        return selftest()
    if args.lat is None or args.lon is None:
        ap.error("LAT and LON are required")
    if not (abs(args.lat) <= 85 and abs(args.lon) <= 180 and 10 <= args.zoom <= 15 and 0 < args.step <= 90 and 0 < args.near < args.radius * 1000):
        ap.error("want |LAT| <= 85, |LON| <= 180, zoom 10-15, step 0-90, near below radius")
    if args.osm and not (0 < args.osm_radius <= 2000 and min(args.building_height, args.tree_height, args.wood_height) > 0):
        ap.error("want --osm-radius 0-2000 m and positive heights")
    if args.canopy and not (0 < args.canopy_near < args.canopy_radius <= 2000):
        ap.error("want 0 < --canopy-near < --canopy-radius <= 2000 m")

    elev = Terrain(args.zoom, args.cache)
    ground = elev(args.lat, args.lon) if args.elevation is None else args.elevation
    spacing = 2 * math.pi * R_EARTH * math.cos(math.radians(args.lat)) / (TILE << args.zoom)  # one pixel
    points = horizon(elev, args.lat, args.lon, ground + args.height, args.step, args.near, args.radius * 1000, spacing)
    sources = ["T"] * len(points)
    notes = []
    # Obstacles stand on the terrain, relative to the site's model ground,
    # so an --elevation override shifts the eye, not their footing.
    site = elev(args.lat, args.lon)
    rise = lambda az, d: elev(*destination(args.lat, args.lon, az, d)) - site - args.height
    if args.osm:
        heights = {"building": args.building_height, "tree": args.tree_height, "wood": args.wood_height}
        try:
            osm = overpass(args.lat, args.lon, args.osm_radius, args.cache)
        except (OSError, ValueError) as e:
            sys.exit(f"overpass: {e}")
        if args.canopy:  # the map measures the trees OSM would guess
            osm = {"elements": [e for e in osm.get("elements", []) if kind_of(e.get("tags", {})) == "building"]}
        outlines, trees, stats = obstacles(osm, args.lat, args.lon, heights)
        for kind, (n, tagged, held) in stats.items():
            extra = f", {held} around the site left out" if held else ""
            if n or held:
                print(f"osm: {n} {kind} features, {tagged} with a tag height, the others {heights[kind]:g} m{extra}", file=sys.stderr)
        tops = obstacle_horizon(outlines, trees, [az for az, _ in points], rise)
        raised = sum(top > alt for top, (_, alt) in zip(tops, points))
        print(f"osm: obstacles above the terrain at {raised} of {len(points)} azimuths", file=sys.stderr)
        # Which obstacle won is not tracked: with the canopy on, OSM gives buildings only.
        mark = "B" if args.canopy else "O"
        sources = [mark if top > alt else src for top, (_, alt), src in zip(tops, points, sources)]
        points = [(az, max(alt, top)) for (az, alt), top in zip(points, tops)]
        guessed = "" if args.canopy else f", trees {args.tree_height:g} m, woods {args.wood_height:g} m"
        notes.append(f"OpenStreetMap buildings ({args.building_height:g} m{guessed} unless tagged) within {args.osm_radius:g} m, "
                     "© OpenStreetMap contributors, ODbL (openstreetmap.org/copyright)")
    if args.canopy:
        try:
            height, mpp = canopy(args.lat, args.lon, args.canopy_radius, args.cache)
        except (OSError, ValueError, zlib.error) as e:
            sys.exit(f"canopy: {e}")
        tops = canopy_horizon(height, [az for az, _ in points], rise, args.canopy_near, args.canopy_radius, mpp / 2)
        raised = sum(top > alt for top, (_, alt) in zip(tops, points))
        print(f"canopy: trees above the rest at {raised} of {len(points)} azimuths", file=sys.stderr)
        sources = ["t" if top > alt else src for top, (_, alt), src in zip(tops, points, sources)]
        points = [(az, max(alt, top)) for (az, alt), top in zip(points, tops)]
        notes.append(f"trees within {args.canopy_radius:g} m from the Meta/WRI canopy height map (Tolan et al. 2024; 2009-2020 imagery), CC BY 4.0")
    print(f"# terrain horizon, {args.radius:g} km at zoom {args.zoom}, ground {ground:.0f} m + {args.height:g} m")
    print("# AWS Terrain Tiles (Mapzen; SRTM, GMTED, ETOPO1 and others)" + ("" if notes else "; bare ground, add trees and buildings by hand"))
    for note in notes:
        print(f"# {note}; check by hand")
    print("# azimuth altitude")
    for az, alt in points:
        print(f"{az:g} {alt:.1f}")
    print(plot(points, sources), file=sys.stderr)
    return None


if __name__ == "__main__":
    sys.exit(main())
