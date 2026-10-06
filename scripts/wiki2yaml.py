#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Build the Melotte (1915, 245 clusters) or Collinder (1931, 471 clusters)
catalogue as an uptonight target list.

    curl -o mel.wiki 'https://en.wikipedia.org/w/index.php?title=Melotte_catalogue&action=raw'
    scripts/wiki2yaml.py Melotte mel.wiki > internal/catalog/targets/Melotte.yaml
    curl -o cr.wiki 'https://en.wikipedia.org/w/index.php?title=Collinder_catalogue&action=raw'
    scripts/wiki2yaml.py Collinder cr.wiki > internal/catalog/targets/Collinder.yaml

Standard library only. The Wikipedia table gives each number its NGC/IC
designation and common name; position, size and type come from MWSC.yaml (by
NGC/IC number, else "Melotte N"/"Collinder N"), then OpenNGC/OpenIC.yaml, then
GaryImmFull.yaml (the Hyades, which MWSC lacks); the few in none of them come
from SIMBAD (EXTRA). The magnitude comes from GaryImmFull, OpenNGC or OpenIC,
whichever has one, since MWSC has none. Regenerate after MWSC.yaml.
"""

import json
import re
import sys

from imm2yaml import sexagesimal
from mwsc2yaml import entries, lookup

LINK = re.compile(r"\[\[(?:[^|\]]*\|)?([^\]]*)\]\]")  # [[target|text]] -> text
REF = re.compile(r"<ref.*?(?:</ref>|/>)|\{\{[^}]*\}\}|<[^>]*>")  # footnotes, {{notetag|...}}, <br/>
# The source list's type, unless it isn't a cluster (OpenNGC "Nebula" for NGC
# 7023, "Duplicate"): then the Wikipedia table's, which describes the cluster.
CLUSTER_TYPES = {"Open Cluster", "Globular Cluster", "Cluster Nebulosity", "Asterism"}
WIKI_TYPES = {"Open cluster": "Open Cluster", "Globular cluster": "Globular Cluster", "Asterism": "Asterism",
              "Galaxy": "Galaxy", "Moving group": "Open Cluster", "Stellar association": "Open Cluster"}
# Catalogue -> (table abbreviation, size).
CATALOGUES = {"Melotte": ("Mel", 245), "Collinder": ("Cr", 471)}
# In no embedded list (J2000 deg, arcmin): Dias et al. (2002, CDS B/ocl) where
# it has the cluster, else SIMBAD.
EXTRA = {
    "Melotte 31": (79.54166, 33.37361, -9999),  # SIMBAD
    "Melotte 186": (270.59792, 3.26, 187.4),  # SIMBAD
    "Collinder 173": (120.70417, -46.38333, 370),
    "Collinder 198": (131.325, -31.76667, 5),
    "Collinder 232": (161.1625, -59.56, 4),
    "Collinder 234": (161.3, -59.75, -9999),  # SIMBAD; southern part of Trumpler 16
    "Collinder 240": (167.91667, -60.30972, 32),
    "Collinder 257": (186.19167, -60.88667, 5),
    "Collinder 285": (220.275, 69.56667, 1400),  # Ursa Major Moving Group
    "Collinder 302": (246.53333, -26.25, 500),  # Upper Scorpius association
    "Collinder 316": (253.875, -40.83333, 100),
    "Collinder 318": (254.25, -40.66667, 60),  # Trumpler 24
    "Collinder 332": (262.7, -37.08333, -9999),  # SIMBAD
    "Collinder 345": (266.14583, -33.86667, 5),
    "Collinder 401": (294.5875, 0.34528, 1),
    "Collinder 427": (315.15417, 68.15972, 4),
    "Collinder 464": (78.1625, 73.97472, 120),
    "Collinder 467": (114.825, -10.55, 2),
}
# Known only by a common name in the Wikipedia table.
ALIAS = {"Collinder 39": "Melotte 20", "Collinder 42": "Melotte 22", "Collinder 50": "Melotte 25",
         "Collinder 256": "Melotte 111"}


def rows(abbr, path):
    """(number, NGC/IC cell, other names, object type) per table row."""
    for chunk in open(path, encoding="utf-8").read().split("\n|-"):
        line = chunk.strip().splitlines()[0] if chunk.strip() else ""
        cells = [" ".join(LINK.sub(r"\1", REF.sub(" ", c)).split()) for c in line.lstrip("| ").split("||")]
        m = re.fullmatch(abbr + r" (\d+)", cells[0])
        if m and len(cells) > 4:
            yield int(m.group(1)), cells[1].strip("- "), cells[2], WIKI_TYPES[cells[4]]


def main():
    cat, path = sys.argv[1], sys.argv[2]
    abbr, count = CATALOGUES[cat]
    q = lambda s: json.dumps(s, ensure_ascii=False)  # JSON strings are valid YAML
    known = {n: {"name": n, "ra": sexagesimal(ra, hours=True), "dec": sexagesimal(dec), "size": size,
                 "type": "Open Cluster"} for n, (ra, dec, size) in EXTRA.items()}
    for name in ("GaryImmFull.yaml", "OpenIC.yaml", "OpenNGC.yaml", "MWSC.yaml"):  # later lists win
        known.update((f["name"], f) for f in entries(name))
    known.update((a, known[b]) for a, b in ALIAS.items())
    mags = lookup("mag")  # MWSC has none of its own
    mags.update((a, mags[b]) for a, b in ALIAS.items() if b in mags)
    out = list(rows(abbr, path))
    if sorted(r[0] for r in out) != list(range(1, count + 1)):
        sys.exit(f"error: want {abbr} 1-{count} once each, got {len(out)} rows")
    found, missing = [], []
    for n, ids, other, wtype in out:
        # NGC/IC number, then "Collinder N", then the other names ("Melotte 20", "Stock 2").
        keys = re.findall(r"(?:NGC|IC) \d+", ids)[:1] + [f"{cat} {n}"] + [k.strip() for k in other.split(",")]
        f = next((known[k] for k in keys if k in known), None)
        mag = next((mags[k] for k in keys if k in mags), -9999)
        if f is None:
            missing.append(f"{abbr} {n} ({ids or 'no NGC/IC'} {other})")
        found.append((n, ids, other, wtype, f, mag))
    if missing:
        sys.exit("error: not found in any list: " + "; ".join(missing))
    print(f"# Generated by scripts/wiki2yaml.py: the {cat} catalogue, cross-identified")
    print("# with NGC/IC via Wikipedia; positions, sizes and types from MWSC, OpenNGC and GaryImmFull.")
    for n, ids, other, wtype, f, mag in found:
        typ = f["type"]
        if typ not in CLUSTER_TYPES:
            typ = "Cluster Nebulosity" if "nebula" in typ.lower() and wtype == "Open Cluster" else wtype
        messier = re.match(r"M \d+", f.get("description", ""))
        desc = " ".join(filter(None, (messier and messier.group(0), ids, other)))
        print('- constellation: ""')  # the loader computes it from RA/Dec
        print(f"  dec: {q(f['dec'])}")
        print(f"  description: {q(desc)}")
        print(f"  mag: {mag}")
        print(f"  name: {q(f'{abbr} {n}')}")
        print(f"  ra: {q(f['ra'])}")
        print(f"  size: {f['size']}")
        print(f"  type: {q(typ)}")


if __name__ == "__main__":
    main()
