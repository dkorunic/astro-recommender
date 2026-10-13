#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Replace LDN.yaml's sizes by the diameter of the circle of each cloud's
area, from Lynds' catalogue (CDS VII/7A).

    curl -o ldn.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/7A/ldn&-out.all&-out.max=3000&-oc.form=dec'
    scripts/ldnsize.py ldn.tsv internal/catalog/targets/LDN.yaml > /tmp/ldn.yaml && mv /tmp/ldn.yaml internal/catalog/targets/LDN.yaml

Standard library only. Lynds gives only an area (square degrees), which
uptonight's list carried as Area x 60, as if it were degrees: LDN 621's
45 deg2 became 2700' and LDN 1622's 0.122 deg2 7.3'. 2 sqrt(Area/pi) x 60'
is the diameter of a disc of that area, a lower bound for the major axis of
an elongated cloud (the Compendium's sizes for the 16 it shares run a median
2.4x larger). Matched by LDN number; an area of 0 leaves the size unknown as
it was, and everything but the size lines is left byte for byte.
"""

import math
import re
import sys

from vizier2yaml import num, table


def main():
    areas = {int(t["LDN"]): num(t["Area"]) for t in table(sys.argv[1]) if t.get("LDN", "").isdigit()}
    chunks = open(sys.argv[2], encoding="utf-8").read().split("\n- ")
    for i, chunk in enumerate(chunks):
        m = re.search(r"^\s*name: LDN (\d+)$", chunk, re.M)
        area = areas.get(int(m.group(1))) if m else None
        if area:
            size = round(2 * math.sqrt(area / math.pi) * 60, 1)
            chunks[i] = re.sub(r"^(\s*size: ).*$", rf"\g<1>{size}", chunk, count=1, flags=re.M)
    sys.stdout.write("\n- ".join(chunks))


if __name__ == "__main__":
    main()
