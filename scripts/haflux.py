#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Add integrated Hα fluxes to the planetary nebulae of the embedded lists.

    scripts/haflux.py [--cache DIR]

Standard library only. Frew, Bojičić & Parker (2013, MNRAS 431, 2; CDS
J/MNRAS/431/2) measured the integrated Hα flux of 1258 Galactic planetaries
on the SHASSA and VTSS surveys (table1) and compiled 49 more (table4):
log10 F(Hα) in erg cm⁻² s⁻¹, observed, [N II] removed, not dereddened, what
a camera collects. Every list entry typed as a planetary nebula is matched
by position to the nearest of them within max(1', a quarter of its size),
else by name (spaces aside) within NAMED, since uptonight's lists give
positions to the minute (Herschel400's NGC 246, 1.1' off); a namesake
farther is a warning, a position to check. The match gets `haflux:`
inserted before its `type:` line (replaced if present);
a lower limit (">") is no measurement and is skipped. Idempotent; rerun
after regenerating a list (fillmag-style, nothing else touched), then
`task lists`. catalog.Target.SurfaceBrightness turns the flux and the
list's size into the lines' V surface brightness, which a central star's
catalogued magnitude cannot fake.
"""

import argparse
import math
import os
import re
import sys
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from crosscheck import degrees, sep  # noqa: E402
from mwsc2yaml import TARGETS  # noqa: E402

CDS = "https://cdsarc.cds.unistra.fr/ftp/J/MNRAS/431/2/"
# table -> (logF(Hα) columns, limit flag column), 0-based slices of the ReadMe's bytes.
TABLES = {"table1.dat": ((65, 71), 64), "table4.dat": ((51, 57), None)}
# How far a name match may be, arcmin: a whole minute of RA at the equator
# is 15', but the lists' coarse positions found so far are within 5' of
# Frew's, and the GaryImmFull namesakes beyond it are 5.6-19' off.
NAMED = 5.0


def latitude(ra, dec):
    """Galactic latitude, degrees, of J2000 ra, dec in degrees."""
    r, (ra_p, dec_p) = math.radians, (192.85948, 27.12825)  # the north galactic pole
    return math.degrees(math.asin(math.sin(r(dec)) * math.sin(r(dec_p))
                                  + math.cos(r(dec)) * math.cos(r(dec_p)) * math.cos(r(ra - ra_p))))


def key(name):
    """A designation spaces aside, lower case: Frew's "Sh 2-78" is "sh2-78"."""
    return re.sub(r"\s", "", name).lower()


def fluxes(cache):
    """[(ra, dec, logF, name)] from the two tables, limits left out."""
    out = []
    for name, ((a, b), flag) in TABLES.items():
        path = os.path.join(cache, name)
        if not os.path.exists(path):
            with urllib.request.urlopen(CDS + name, timeout=60) as r:
                data = r.read()
            with open(path + ".tmp", "wb") as f:
                f.write(data)
            os.replace(path + ".tmp", path)
        for line in open(path, encoding="utf-8"):
            if flag is not None and line[flag:flag + 1] in "<>":
                continue
            try:
                logf = float(line[a:b])
            except ValueError:
                continue
            ra = (int(line[30:32]) + int(line[33:35]) / 60 + float(line[36:40]) / 3600) * 15
            dec = int(line[42:44]) + int(line[45:47]) / 60 + int(line[48:50]) / 3600
            if line[41] == "-":
                dec = -dec
            elif line[42:44] == "00":
                # The sign byte is blank for every -00 and +00 Dec (NGC 2346's
                # -00 48 read as +00 48 was 97' off): the sign whose galactic
                # latitude is the PN G designation's.
                glat = float(line[5:10])
                dec = min((dec, -dec), key=lambda d: abs(latitude(ra, d) - glat))
            out.append((ra, dec, logf, line[12:30].strip()))
    return out


def put(entry, logf):
    """entry (a "\n- "-split chunk of a list) with its haflux line removed and,
    unless logf is None, haflux: logf inserted before its type line."""
    entry = re.sub(r"^ *haflux: .*\n", "", entry, flags=re.M)
    if not entry.startswith("#"):
        entry = entry.lstrip(" ")  # a removed first key leaves the next indented
    if logf is None:
        return entry
    m = re.search(r"^( *)type: ", entry, re.M)
    if m.start() == 0 or entry[:m.start()].endswith("- "):
        return entry[:m.start()] + f"haflux: {logf:.2f}\n  " + entry[m.start():]
    return entry[:m.start()] + f"{m.group(1)}haflux: {logf:.2f}\n" + entry[m.start():]


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/haflux"))
    args = ap.parse_args()
    os.makedirs(args.cache, exist_ok=True)
    ref = fluxes(args.cache)
    named = {}
    for r in ref:  # table1's own measurement over table4's compiled one, as by position
        named.setdefault(key(r[3]), r)
    for fname in sorted(os.listdir(TARGETS)):
        if not fname.endswith(".yaml"):
            continue
        path = os.path.join(TARGETS, fname)
        parts = open(path, encoding="utf-8").read().split("\n- ")
        n = 0
        for i, p in enumerate(parts):
            t = re.search(r'^\s*-?\s*type: "?([^"\n]*)"?$', p, re.M)
            if not t or "planetary" not in t.group(1).lower():
                continue
            f = dict(re.findall(r'^\s*-?\s*(\w+): "?([^"\n]*)"?$', p, re.M))
            ra, dec = degrees(f.get("ra", ""), True), degrees(f.get("dec", ""), False)
            if ra is None or dec is None:
                continue
            size = float(f.get("size", -9999) or -9999)
            limit = max(1.0, size / 4)
            best = min(ref, key=lambda r: sep((ra, dec), r[:2]))
            found = sep((ra, dec), best[:2]) <= limit
            if not found and key(f.get("name", "")) in named:
                namesake = named[key(f["name"])]
                if sep((ra, dec), namesake[:2]) <= NAMED:
                    best, found = namesake, True
                else:
                    print(f"warning: {fname}: {f['name']} is {sep((ra, dec), namesake[:2]):.1f}' from Frew's", file=sys.stderr)
            p = put(p, best[2] if found else None)
            n += found
            parts[i] = p
        open(path, "w", encoding="utf-8").write("\n- ".join(parts))
        if n:
            print(f"{fname}: {n} planetaries with a flux", file=sys.stderr)


if __name__ == "__main__":
    main()
