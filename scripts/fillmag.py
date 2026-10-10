#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Fill the unknown (-9999) magnitudes of an uptonight list from the lists
that have them (Messier, OpenNGC, OpenIC, GaryImmFull), matched by name
(a galaxy cluster "Abell N" also as the Compendium's "AbellG N") or by the
first NGC/IC number in the description (the object's own cross-ID; a later
one is a neighbour mentioned in passing), and add the surfbr (V, GaryImmFull) and
bsurfbr (B, OpenNGC/OpenIC) surface brightness keys where those have one,
and minor, the list's own size times OpenNGC/OpenIC's axis ratio (the
lists' sizes are not always OpenNGC's, so its minor axis could exceed
theirs; a cross-ID's ratio only when it leads the description alone and
the sizes agree within 2x, since a pair, complex or differently delimited
object has a shape of its own); everything else is left byte for byte.

    scripts/fillmag.py internal/catalog/targets/Messier.yaml > /tmp/m.yaml && mv /tmp/m.yaml internal/catalog/targets/Messier.yaml
    scripts/fillmag.py --minor-only internal/catalog/targets/Caldwell.yaml > /tmp/c.yaml && mv /tmp/c.yaml internal/catalog/targets/Caldwell.yaml

--minor-only adds just the minor axis and leaves every other key as the
list's generator wrote it (Caldwell's come from caldwell2yaml.py).

Standard library only. Run after regenerating OpenNGC/OpenIC, and on
Messier.yaml before the lists that borrow from it.
"""

import re
import sys

from mwsc2yaml import entries, lookup


def main():
    minor_only = sys.argv[1] == "--minor-only"
    tables = {} if minor_only else {field: lookup(field) for field in ("mag", "surfbr", "bsurfbr")}
    axes = {}
    for f in (f for name in ("OpenIC.yaml", "OpenNGC.yaml") for f in entries(name) if "minor" in f):
        # OpenNGC's description starts with the Messier number, which GaryImm's M 31 goes by.
        for k in [f["name"]] + re.findall(r"^M \d+\b", f.get("description", "")):
            axes[k] = (float(f["size"]), float(f["minor"]))
    chunks = open(sys.argv[-1], encoding="utf-8").read().split("\n- ")
    for i, chunk in enumerate(chunks):
        f = dict(re.findall(r"^\s*-?\s*(\w+): ?(.*)$", chunk, re.M))
        desc = f.get("description", "").strip('"')
        ids = re.findall(r"(?:NGC|IC) \d+", desc)
        keys = [f.get("name", "")] + ids[:1]
        if f.get("type") == "Galaxy Cluster":  # not the "Abell N" planetaries and remnants
            keys.append(keys[0].replace("Abell ", "AbellG "))
        for field, table in tables.items():
            v = next((table[k] for k in keys if k in table), None)
            if v is None:
                continue
            if field == "mag" and f.get("mag") == "-9999":
                chunk = re.sub(r"^(\s*mag: )-9999$", rf"\g<1>{v}", chunk, count=1, flags=re.M)
            elif field != "mag" and field not in f:  # before the type line, the entry's last
                chunk = re.sub(r"^(\s*)(type: )", rf"\g<1>{field}: {v}\n\g<1>\g<2>", chunk, count=1, flags=re.M)
        # A cross-ID's shape is the row's own only when it is the description's
        # one and leading designation (a second NGC/IC, or another name ahead
        # of it, makes the row a pair or complex: Caldwell's Arp 244, R CrA
        # and Dragons of Ara) and the sizes agree within 2x (else the row is
        # delimited differently: the 75' Western Veil arc of the 210' NGC 6960).
        size = float(f.get("size", -9999))
        ax = axes.get(keys[0])
        if ax is None and len(ids) == 1 and desc.startswith(ids[0]):
            ax = axes.get(ids[0])
            if ax and not ax[0] / 2 <= size <= ax[0] * 2:
                ax = None
        if ax and "minor" not in f and size > 0:
            chunk = re.sub(r"^(\s*)(type: )", rf"\g<1>minor: {round(size * ax[1] / ax[0], 2)}\n\g<1>\g<2>", chunk, count=1, flags=re.M)
        chunks[i] = chunk
    sys.stdout.write("\n- ".join(chunks))


if __name__ == "__main__":
    main()
