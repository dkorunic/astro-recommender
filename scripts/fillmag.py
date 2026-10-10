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
theirs); everything else is left byte for byte.

    scripts/fillmag.py internal/catalog/targets/Messier.yaml > /tmp/m.yaml && mv /tmp/m.yaml internal/catalog/targets/Messier.yaml

Standard library only. Run after regenerating OpenNGC/OpenIC, and on
Messier.yaml before the lists that borrow from it.
"""

import re
import sys

from mwsc2yaml import entries, lookup


def main():
    tables = {field: lookup(field) for field in ("mag", "surfbr", "bsurfbr")}
    ratio = {}
    for f in (f for name in ("OpenIC.yaml", "OpenNGC.yaml") for f in entries(name) if "minor" in f):
        # OpenNGC's description starts with the Messier number, which GaryImm's M 31 goes by.
        for k in [f["name"]] + re.findall(r"^M \d+\b", f.get("description", "")):
            ratio[k] = float(f["minor"]) / float(f["size"])
    chunks = open(sys.argv[1], encoding="utf-8").read().split("\n- ")
    for i, chunk in enumerate(chunks):
        f = dict(re.findall(r"^\s*-?\s*(\w+): ?(.*)$", chunk, re.M))
        keys = [f.get("name", "")] + re.findall(r"(?:NGC|IC) \d+", f.get("description", ""))[:1]
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
        r = next((ratio[k] for k in keys if k in ratio), None)
        if r is not None and "minor" not in f and float(f.get("size", -9999)) > 0:
            chunk = re.sub(r"^(\s*)(type: )", rf"\g<1>minor: {round(float(f['size']) * r, 2)}\n\g<1>\g<2>", chunk, count=1, flags=re.M)
        chunks[i] = chunk
    sys.stdout.write("\n- ".join(chunks))


if __name__ == "__main__":
    main()
