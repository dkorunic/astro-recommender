#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Add the minor axis to LBN.yaml from Lynds' catalogue (CDS VII/9), which
gives each nebula's largest (Diam1) and smallest (Diam2) dimension.

    curl -O https://cdsarc.cds.unistra.fr/ftp/cats/VII/9/catalog.dat
    scripts/lbnminor.py catalog.dat internal/catalog/targets/LBN.yaml > /tmp/lbn.yaml && mv /tmp/lbn.yaml internal/catalog/targets/LBN.yaml

Standard library only. Matched by the LBN number (VII/9's running number).
Five entries have the two swapped (Diam2 > Diam1) and uptonight's size is
the larger, so the minor axis is the smaller of the two, added before the
type line only when below the list's size; everything else is left byte for
byte.
"""

import re
import sys


def main():
    diams = {int(line[1:5]): (int(line[35:39]), int(line[40:43])) for line in open(sys.argv[1], encoding="ascii")}
    chunks = open(sys.argv[2], encoding="utf-8").read().split("\n- ")
    for i, chunk in enumerate(chunks):
        f = dict(re.findall(r"^\s*-?\s*(\w+): ?(.*)$", chunk, re.M))
        m = re.fullmatch(r"LBN (\d+)", f.get("name", ""))
        if not m or "minor" in f or int(m.group(1)) not in diams:
            continue
        minor = min(diams[int(m.group(1))])
        if 0 < minor < float(f.get("size", -9999)):
            chunks[i] = re.sub(r"^(\s*)(type: )", rf"\g<1>minor: {minor}\n\g<1>\g<2>", chunk, count=1, flags=re.M)
    sys.stdout.write("\n- ".join(chunks))


if __name__ == "__main__":
    main()
