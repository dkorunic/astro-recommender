#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Cross-check the embedded lists against each other.

    scripts/crosscheck.py [--sep ARCMIN] [--ratio X] [--lists NAME ...] [--simbad]

Standard library only. Every object whose name (case and spaces aside, as
catalog.NameKey) appears in more than one list is compared across them:
positions more than --sep arcminutes apart (default 2) and sizes more than
--ratio times apart (default 3) are printed, worst first. Disagreements are
leads, not errors: lists measure sizes differently (a halo, a core). With
--simbad each flagged object's SIMBAD position and major axis (none for
most nebulae) are printed beside, from its batch script interface, and an
entry far off it while another list agrees is marked WRONG; fix it where the list comes from (a generator's
override table, or by hand for the uptonight lists, see CLAUDE.md).
"""

import argparse
import json
import math
import os
import sys
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from mwsc2yaml import TARGETS, entries  # noqa: E402

SCRIPT = "https://simbad.cds.unistra.fr/simbad/sim-script"


def degrees(text, hours):
    """'hh mm ss' or '[+-]dd mm ss' as degrees; None when unparsable."""
    parts = text.split()
    try:
        v = [abs(float(p)) for p in parts]
    except ValueError:
        return None
    if not v:
        return None
    x = v[0] + (v[1] if len(v) > 1 else 0) / 60 + (v[2] if len(v) > 2 else 0) / 3600
    if text.strip().startswith("-"):
        x = -x
    return x * 15 if hours else x


def sep(a, b):
    """Angular separation of (ra, dec) degree pairs in arcminutes."""
    r = math.radians
    c = (math.sin(r(a[1])) * math.sin(r(b[1])) +
         math.cos(r(a[1])) * math.cos(r(b[1])) * math.cos(r(a[0] - b[0])))
    return math.degrees(math.acos(max(-1, min(1, c)))) * 60


def load():
    """NameKey -> [(list, name, (ra, dec), size, type)] for names in two lists or more."""
    groups = {}
    for fname in sorted(os.listdir(TARGETS)):
        if not fname.endswith(".yaml"):
            continue
        for f in entries(fname):
            ra, dec = degrees(f.get("ra", ""), True), degrees(f.get("dec", ""), False)
            if ra is None or dec is None:
                continue
            size = float(f.get("size", -9999) or -9999)
            key = f["name"].lower().replace(" ", "")
            groups.setdefault(key, []).append((fname[:-5], f["name"], (ra, dec), size if size > 0 else None, f.get("type", "")))
    return {k: v for k, v in groups.items() if len({e[0] for e in v}) > 1}


def simbad(names):
    """Name -> (ra, dec, major axis arcmin or None) as SIMBAD resolves it, in
    batches through its script interface (TAP would want SIMBAD's own
    spacing of each identifier)."""
    out = {}
    names = sorted(names)
    for i in range(0, len(names), 400):
        script = 'output console=off script=off\nformat object "%OBJECT|%COO(d;A D)|%DIM(X)"\n'
        script += "".join(f"query id {n}\n" for n in names[i:i + 400])
        data = urllib.parse.urlencode({"script": script}).encode()
        with urllib.request.urlopen(SCRIPT, data, timeout=300) as r:
            text = r.read().decode("utf-8", "replace")
        for line in text.split("::data::")[-1].splitlines():
            parts = line.split("|")
            if len(parts) != 3:
                continue
            try:
                ra, dec = map(float, parts[1].split())
            except ValueError:
                continue
            dim = parts[2].strip()
            out[parts[0].strip()] = (ra, dec, float(dim) if dim.replace(".", "", 1).isdigit() else None)
    return out


def verdict(rows, ref, max_sep, ratio):
    """The entries SIMBAD shows wrong: farther from its position than max_sep
    and a quarter of the object's size (a cluster's centre is a matter of
    definition) while another entry is within half of that; or, for a
    galaxy or a nebula other than a planetary (SIMBAD gives clusters'
    membership radius and planetaries' bright shell, where the lists give
    the visual core and the halo), more than ratio times off its major axis
    while another is within its square root."""
    ra, dec, dim = ref
    sizes = [r[3] for r in rows if r[3]] + ([dim] if dim else [])
    limit = max(max_sep, max(sizes) / 4 if sizes else 0)
    bad = []
    near = [sep(r[2], (ra, dec)) for r in rows]
    for r, d in zip(rows, near):
        if d > limit and min(near) < limit / 2:
            bad.append((r[0], f"{d:.1f}' from SIMBAD"))
    kinds = " ".join(r[4] for r in rows).lower()
    if dim and "cluster" not in kinds and "planetary" not in kinds and "cl+n" not in kinds:
        offs = [max(r[3], dim) / min(r[3], dim) if r[3] else None for r in rows]
        for r, o in zip(rows, offs):
            if o and o > ratio and min(x for x in offs if x) < math.sqrt(ratio):
                bad.append((r[0], f"size {r[3]}' against SIMBAD's {dim}'"))
    return bad


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--sep", type=float, default=2.0)
    ap.add_argument("--ratio", type=float, default=3.0)
    ap.add_argument("--simbad", action="store_true")
    ap.add_argument("--lists", nargs="*", help="only names one of these lists carries")
    args = ap.parse_args()
    flags = []
    for key, rows in load().items():
        if args.lists and not any(r[0] in args.lists for r in rows):
            continue
        worst_sep = max(sep(a[2], b[2]) for a in rows for b in rows)
        sizes = [r[3] for r in rows if r[3]]
        ratio = max(sizes) / min(sizes) if len(sizes) > 1 else 1
        if worst_sep > args.sep or ratio > args.ratio:
            flags.append((max(worst_sep / args.sep, ratio / args.ratio), key, worst_sep, ratio, rows))
    flags.sort(reverse=True)
    ref = simbad({r[1] for f in flags for r in f[4]}) if args.simbad else {}
    for _, key, s, ratio, rows in flags:
        print(f"{rows[0][1]}: {s:.1f}' apart, sizes {ratio:.1f}x")
        for lst, name, (ra, dec), size, _ in rows:
            print(f"  {lst:12} {ra:9.4f} {dec:+9.4f} {size or '-'}")
        if rows[0][1] in ref:
            sra, sdec, sdim = ref[rows[0][1]]
            print(f"  {'SIMBAD':12} {sra:9.4f} {sdec:+9.4f} {sdim or '-'}")
            for lst, why in verdict(rows, ref[rows[0][1]], args.sep, args.ratio):
                print(f"  WRONG {lst}: {why}")
    print(f"{len(flags)} names flagged", file=sys.stderr)


if __name__ == "__main__":
    main()
