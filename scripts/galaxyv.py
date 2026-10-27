#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Replace the aperture V magnitudes of the embedded lists' galaxies.

    scripts/galaxyv.py

Standard library only. Many lists carry a galaxy's V from a small aperture
around its nucleus (SIMBAD's, which uptonight's lists and the Compendium
copied: M 88 13.2 for a total near 9.6, NGC 4565 12.4), which derives a
surface brightness some 3 mag too faint. Such a V is fainter than the
galaxy's total B, a colour no galaxy has (catalog.minBV: B−V of -0.3); and a
V more than 1.5 brighter than B (catalog.maxBV) is no galaxy's colour either
(IC 127's 9.20 against B 14.83: a star's, or another object's). So every
list entry typed as a single galaxy (not a group, pair or cluster) whose V
is more than 0.3 fainter, or 1.5 brighter, than its B (its own bmag, else
OpenNGC/OpenIC's by name, spaces aside like catalog.NameKey (Pensack's
IC10), or by the description's leading NGC/IC number, as fillmag.py
matches) gets -9999 and the B as bmag (inserted before its
type line when the entry has none), so SurfaceBrightness derives from B
and the MAG column shows it as B. OpenNGC's V is no substitute: where it
passes this test it can still be partly an aperture one (M 88's 10.33
against B 10.24, where a spiral's B−V is near 0.6). Idempotent,
everything else left byte for byte; run after regenerating a list and after
fillmag.py (which fills unknown magnitudes from these lists), then
`task lists`.
"""

import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from haflux import key  # noqa: E402
from mwsc2yaml import TARGETS, lookup  # noqa: E402

# catalog.minBV, catalog.maxBV: no galaxy's total B−V is outside them, so a
# V fainter than B by more, or brighter by more, is not the galaxy's total.
MIN_BV, MAX_BV = -0.3, 1.5
GROUP = ("group", "cluster", "pair", "duo", "trio", "triplet")  # catalog.groupWords


def galaxy(typ):
    """Whether typ is a single galaxy's, as catalog.Target.kind reads it."""
    t = typ.lower()
    return "galax" in t and not any(w in t for w in GROUP)


def main():
    refs = ("OpenIC.yaml", "OpenNGC.yaml")
    bmag = {key(k): v for k, v in lookup("bmag", refs).items()}
    for fname in sorted(os.listdir(TARGETS)):
        if not fname.endswith(".yaml"):
            continue
        path = os.path.join(TARGETS, fname)
        parts = open(path, encoding="utf-8").read().split("\n- ")
        fixed = []
        for i, p in enumerate(parts):
            f = {k: v.strip('"') for k, v in re.findall(r"^\s*-?\s*(\w+): ?(.*)$", p, re.M)}
            if not galaxy(f.get("type", "")) or float(f.get("mag", -9999)) <= 0:
                continue
            # The leading designation only: a later one is a neighbour's
            # (Arp 78's NGC 770 beside NGC 772).
            ids = re.match(r"(?:NGC|IC) \d+\b", f.get("description", ""))
            keys = [key(k) for k in [f["name"]] + ([ids.group()] if ids else [])]
            b = f["bmag"] if float(f.get("bmag", -9999)) > 0 else next((bmag[k] for k in keys if k in bmag), None)
            v = float(f["mag"])
            if b is None or MIN_BV <= float(b) - v <= MAX_BV:
                continue
            p = re.sub(r"^(\s*-?\s*mag: ).*$", r"\g<1>-9999", p, count=1, flags=re.M)
            if "bmag" not in f:
                p = re.sub(r"^(\s*)(type: )", rf"\g<1>bmag: {b}\n\g<1>\g<2>", p, count=1, flags=re.M)
            fixed.append(f"{f['name']} V {v} B {b}")
            parts[i] = p
        if fixed:
            open(path, "w", encoding="utf-8").write("\n- ".join(parts))
            print(f"{fname}: {len(fixed)} V not the total: {', '.join(fixed[:6])}", file=sys.stderr)


if __name__ == "__main__":
    main()
