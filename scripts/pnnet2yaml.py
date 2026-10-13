#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Convert the Planetary Nebulae.net catalogue (French amateur PN discoveries,
https://planetarynebulae.net/) into an uptonight target list.

    curl -o pnnet.csv https://planetarynebulae.net/datas/TABLE_NP_CSV_converti.csv
    scripts/pnnet2yaml.py pnnet.csv > internal/catalog/targets/PNnet.yaml

PN.net is "all rights reserved"; its authors gave written permission to fetch
it and embed the result (October 2026), and maintain the CSV export for it
(id, name, png, ra, dec in decimal degrees, status). Its status codes T, L, P
and c are kept (true, likely, possible PN, new candidate: the status in the
description); the rest (HII, Em*, star, G...) are other objects. The CSV has
no sizes, so the site's search is still fetched for them, joined by name
(two requests: the search page for the discoverer checkboxes, then one search
with all of them ticked); an object it lacks gets -9999. Standard library only.
"""

import csv
import html
import json
import math
import re
import sys
import urllib.parse
import urllib.request

URL = "https://planetarynebulae.net/EN/page_recherche.php"
UA = "astro-recommender (https://github.com/dkorunic/astro-recommender)"
KEEP = {"T": "True PN", "L": "Likely PN", "P": "Possible PN", "c": "New candidate"}
# Positions the export gets wrong, as decimal degrees, until it is fixed:
# CopMart 8's RA is "18:24:2886" on the site, which the CSV read as 2886
# seconds (19h12m06s); 18 24 28.86 is HASH's and fits its PN G 010.9-03.9.
FIXES = {"CopMart 8": ("276.12025", "-21.34200")}


def get(data=None):
    body = urllib.parse.urlencode(data).encode() if data else None
    req = urllib.request.Request(URL, data=body, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read().decode("utf-8", errors="replace")


def cells(row):
    return [" ".join(html.unescape(re.sub(r"<[^>]*>", " ", c)).split())
            for c in re.findall(r"<td[^>]*>(.*?)</td>", row, re.S)]


def sexagesimal(v, hours):
    """Decimal degrees "322.68800" -> "21 30 45.12" (hours) or "+52 41 48.10",
    to 0.01 s, as fine as the CSV's 1e-5 degrees; None when malformed or out
    of range."""
    try:
        deg = float(v)
    except ValueError:
        return None
    if not math.isfinite(deg) or not (0 <= deg < 360 if hours else abs(deg) <= 90):
        return None
    x = deg / 15 if hours else abs(deg)
    t = round(x * 360000)  # hundredths of a second
    if hours:
        t %= 24 * 360000  # 23 59 59.996 rounds to 24h: wrap to 0h
    d, rem = divmod(t, 360000)
    mi, cs = divmod(rem, 6000)
    sign = "" if hours else ("-" if deg < 0 else "+")
    return f"{sign}{d:02d} {mi:02d} {cs / 100:05.2f}"


PNG = re.compile(r"PN[ -]?G\s*([+-]?\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)")


def png(v):
    """Normalize a PN G designation ("PN G" optional) to "PN G lll.l+bb.b";
    the data sometimes drops the longitude's zero padding or swaps l and b
    (a negative l)."""
    v = v.strip()
    m = PNG.fullmatch(v if v.startswith("PN") else "PN G " + v)
    if not m:
        return None
    l, b = float(m[1]), float(m[2])
    if l < 0 or abs(b) > 90:
        l, b = b, l
    if not (0 <= l < 360 and abs(b) <= 90):
        return None
    return f"PN G {l:05.1f}{b:+05.1f}"


def png_offset(g, ra, dec):
    """How far (degrees) the galactic position of RA/Dec is from the one
    designation g encodes, when more than its 0.1 degree rounding allows;
    else 0."""
    m = PNG.fullmatch(g)
    r, d = math.radians(ra), math.radians(dec)
    rap, decp, l0 = math.radians(192.85948), math.radians(27.12825), math.radians(122.93192)
    b = math.asin(math.sin(d) * math.sin(decp) + math.cos(d) * math.cos(decp) * math.cos(r - rap))
    l = l0 - math.atan2(math.cos(d) * math.sin(r - rap), math.sin(d) * math.cos(decp) - math.cos(d) * math.sin(decp) * math.cos(r - rap))
    off = math.hypot((math.degrees(l) - float(m[1]) + 180) % 360 - 180, math.degrees(b) - float(m[2]))
    return off if off > 0.2 else 0


def size(v):
    """Major axis in arcmin from "4,8 min d'arc" or "2 x 1 min d'arc"; -9999 if none."""
    nums = [float(n) for n in re.findall(r"\d+(?:\.\d+)?", v.replace(",", "."))]
    return max(nums) if nums else -9999


def sizes():
    """Name -> major axis from the site's search, which the CSV lacks."""
    boxes = sorted(set(re.findall(r'name="(chk_[^"]+)"', get())))
    if not boxes:
        sys.exit("error: no checkboxes on the search page, has the site changed?")
    page = get({**{b: "on" for b in boxes}, "AD_debut": "-2", "AD_fin": "-2"})
    rows = [cells(r) for r in re.split(r"<tr>", page) if "page_np.php?id=" in r]
    out = {c[0]: size(c[4]) for c in rows if len(c) >= 6}
    if not out:
        sys.exit("error: no sizes parsed, has the site changed?")
    return out


def main():
    rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
    if not rows or not {"name", "png", "ra", "dec", "status"} <= rows[0].keys():
        sys.exit("error: not a PN.net CSV (id,name,png,ra,dec,status), has the export changed?")
    sz = sizes()
    q = lambda s: json.dumps(s, ensure_ascii=False)  # JSON strings are valid YAML
    print("# Generated by scripts/pnnet2yaml.py from Planetary Nebulae.net")
    print("# (https://planetarynebulae.net/, (c) Planetary Nebulae net, all rights reserved), used with permission.")
    kept = 0
    for r in rows:
        status = KEEP.get(r["status"].strip())
        if not status:
            continue
        name = " ".join(r["name"].split())
        if name in FIXES:
            r["ra"], r["dec"] = FIXES[name]
        ra, dec = sexagesimal(r["ra"], True), sexagesimal(r["dec"], False)
        if not ra or not dec:
            print(f"warning: {name}: bad coordinates {r['ra']!r} {r['dec']!r}, skipped", file=sys.stderr)
            continue
        g = png(r["png"])
        if not g:
            g = r["png"].strip()
            print(f"warning: {name}: odd designation {g!r}, kept as is", file=sys.stderr)
        elif off := png_offset(g, float(r["ra"]), float(r["dec"])):
            # Su Objet 8 and PaSter 1 (217.2 for 271.3) sit where HASH puts
            # them, so it is their designation; Mul 19 and RebZva 1 are in
            # neither HASH nor SIMBAD. Report them to PN.net.
            print(f"warning: {name}: {g} is {off:.1f} deg from its position", file=sys.stderr)
        if name not in sz:
            print(f"warning: {name}: not in the site's search, no size", file=sys.stderr)
        kept += 1
        print('- constellation: ""')  # the loader computes it from RA/Dec
        print(f"  dec: {q(dec)}")
        print(f"  description: {q(f'{status}, {g}')}")
        print(f"  name: {q(name)}")
        print(f"  ra: {q(ra)}")
        print(f"  size: {sz.get(name, -9999)}")
        print('  type: "Planetary Nebula"')
    print(f"{len(rows)} objects, {kept} kept", file=sys.stderr)
    if not kept:
        sys.exit("error: nothing kept, has the export changed?")


if __name__ == "__main__":
    main()
