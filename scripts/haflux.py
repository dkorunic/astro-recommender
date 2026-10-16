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
and gets `haflux:` inserted before its `type:` line (replaced if present);
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


def fluxes(cache):
    """[(ra, dec, logF)] from the two tables, limits left out."""
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
            out.append((ra, -dec if line[41] == "-" else dec, logf))
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/haflux"))
    args = ap.parse_args()
    os.makedirs(args.cache, exist_ok=True)
    ref = fluxes(args.cache)
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
            p = re.sub(r"^ *haflux: .*\n", "", p, flags=re.M)
            if not p.startswith("#"):
                p = p.lstrip(" ")  # a removed first key leaves the next indented
            if sep((ra, dec), best[:2]) <= limit:
                m = re.search(r"^( *)type: ", p, re.M)
                if m.start() == 0 or p[:m.start()].endswith("- "):
                    p = p[:m.start()] + f"haflux: {best[2]:.2f}\n  " + p[m.start():]
                else:
                    p = p[:m.start()] + f"{m.group(1)}haflux: {best[2]:.2f}\n" + p[m.start():]
                n += 1
            parts[i] = p
        open(path, "w", encoding="utf-8").write("\n- ".join(parts))
        if n:
            print(f"{fname}: {n} planetaries with a flux", file=sys.stderr)


if __name__ == "__main__":
    main()
