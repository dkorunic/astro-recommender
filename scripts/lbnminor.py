#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Add the minor axis to LBN.yaml from Lynds' catalogue (CDS VII/9), which
gives each nebula's largest (Diam1) and smallest (Diam2) dimension, and type
the nebulae by Lynds' colour: 1 (brighter on the blue Palomar plate) a
reflection nebula, 2 (equal on both) a nebula, which is not emission-line,
3 and 4 (brighter on, or only on, the red plate: Halpha) an H II region.
uptonight typed 1043 of the 1116 "HII region", and some by an object
cross-identified in them (the Orion and Eagle Nebulae "Open cluster", the
Gamma Cygni Nebula "Star", the Witch Head "Duplicate"); every LBN entry is a
nebula, so those are retyped too, and only its explicit reflection, nebula
and supernova remnant types are kept.

    curl -O https://cdsarc.cds.unistra.fr/ftp/cats/VII/9/catalog.dat
    scripts/lbnminor.py catalog.dat internal/catalog/targets/LBN.yaml > /tmp/lbn.yaml && mv /tmp/lbn.yaml internal/catalog/targets/LBN.yaml

Standard library only. Matched by the LBN number (VII/9's running number).
Five entries have the two swapped (Diam2 > Diam1) and uptonight's size is
the larger, so the minor axis is the smaller of the two, added before the
type line only when below the list's size; everything else but those types
is left byte for byte.
"""

import re
import sys


# uptonight's types the colour replaces, and the type each colour makes.
RETYPE = {"HII region", "Open cluster", "Star", "Duplicate"}
COLOUR = {"1": "Reflection nebula", "2": "Nebula", "3": "HII region", "4": "HII region"}


def main():
    lines = open(sys.argv[1], encoding="ascii").readlines()
    diams = {int(line[1:5]): (int(line[35:39]), int(line[40:43])) for line in lines}
    colour = {int(line[1:5]): line[52] for line in lines}
    chunks = open(sys.argv[2], encoding="utf-8").read().split("\n- ")
    for i, chunk in enumerate(chunks):
        f = dict(re.findall(r"^\s*-?\s*(\w+): ?(.*)$", chunk, re.M))
        m = re.fullmatch(r"LBN (\d+)", f.get("name", ""))
        if not m or int(m.group(1)) not in diams:
            continue
        if f.get("type") in RETYPE and (t := COLOUR.get(colour[int(m.group(1))])) and t != f["type"]:
            chunk = chunks[i] = re.sub(r"^(\s*type: ).*$", rf"\g<1>{t}", chunk, count=1, flags=re.M)
        if "minor" in f:
            continue
        minor = min(diams[int(m.group(1))])
        if 0 < minor < float(f.get("size", -9999)):
            chunks[i] = re.sub(r"^(\s*)(type: )", rf"\g<1>minor: {minor}\n\g<1>\g<2>", chunk, count=1, flags=re.M)
    sys.stdout.write("\n- ".join(chunks))


if __name__ == "__main__":
    main()
