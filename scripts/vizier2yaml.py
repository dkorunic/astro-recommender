#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Build the Barnard, van den Bergh, Arp, Hickson and van den Bergh-Herbst
catalogues as uptonight target lists from their CDS/VizieR tables,
cross-identified with Gary Imm's Deep Sky Compendium (vdBH has no
Compendium rows and is built from VizieR alone).

    V='https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-out.all&-out.max=5000&-out.add=_RAJ,_DEJ&-oc.form=dec&-source='
    curl -o barnard.tsv "${V}VII/220A/barnard"   # Barnard 1927, 349 dark nebulae
    curl -o vdb.tsv "${V}VII/21/catalog"         # van den Bergh 1966, 158 reflection nebulae
    curl -o arpord.tsv "${V}VII/192/arpord"      # Arp 1966 (Webb 1996 index), 338 objects
    curl -o arplist.tsv "${V}VII/192/arplist"    # ... and their 592 member galaxies
    curl -o hcg.tsv "${V}VII/213/groups"         # Hickson 1982, 100 compact groups
    curl -o vdbh.tsv "${V}VII/218/catalog"       # van den Bergh & Herbst 1975, 136 southern reflection nebulae
    X=IMM_Compendium_2026.xlsx T=internal/catalog/targets
    scripts/vizier2yaml.py Barnard barnard.tsv $X > $T/Barnard.yaml
    scripts/vizier2yaml.py vdB vdb.tsv $X > $T/vdB.yaml
    scripts/vizier2yaml.py Arp arpord.tsv arplist.tsv $X > $T/Arp.yaml
    scripts/fillmag.py --minor-only $T/Arp.yaml > /tmp/a.yaml && mv /tmp/a.yaml $T/Arp.yaml
    scripts/vizier2yaml.py HCG hcg.tsv $X > $T/HCG.yaml
    scripts/vizier2yaml.py vdBH vdbh.tsv > $T/vdBH.yaml

Standard library only. VizieR gives every object's J2000 position
(precessed by VizieR from Barnard's 1875 and the 1950 ones), its size
(Barnard's diameter, vdB's largest radius doubled, Hickson's group
diameter, vdBH's largest diameter; for Arp the extent of its member
galaxies, since arpord's Size is Arp's photograph) and the type the
catalogue is of. The Compendium row that is the object itself (named, or
alternative-ID'd, with its designation, nearest first) then gives the
position, type, magnitude, surface brightness and common name, and its size
unless more than 4x off VizieR's (Barnard 344's 60' for 7'). An own row
farther from VizieR than max(20', half the size) is settled by POSITION,
checked against SIMBAD: the Compendium has typos (Barnard 144 is 12° off),
VizieR one (HCG 48, 4° off); an unsettled one exits. The other rows
carrying the designation (a cross-ID column: IC 434 for Barnard 33) within
that distance are named in the description, with Arp's member galaxies,
vdB's illuminating star and vdBH's own cross-IDs. Without an own row,
Arp's type is its member count (Galaxy, Galaxy Duo, Galaxy Group) and its
magnitude their combined V, Hickson's the group's total B (bmag). vdB's
and vdBH's magnitudes are the illuminating stars', not the nebulae's, and
are left out; minor axes come from fillmag.py --minor-only (OpenNGC's axis
ratio), which only Arp's single galaxies get.
"""

import json
import math
import re
import sys

from imm2yaml import aliases, designation, kind, num, position, sexagesimal
from sharpless2yaml import sep

# Own rows far from VizieR, settled against SIMBAD (Sesame, 2026-10-12):
# "vizier" where the Compendium's coordinates are typos, "compendium" where
# VizieR's are.
POSITION = {
    "Barnard 134": "vizier", "Barnard 144": "vizier", "Barnard 344": "vizier",
    "vdB 5": "vizier", "vdB 32": "vizier", "vdB 39": "vizier",
    "Arp 8": "vizier", "Arp 67": "vizier", "Arp 201": "vizier", "Arp 258": "vizier",
    "HCG 18": "vizier",  # its own row is Arp 258's
    "HCG 48": "compendium",
}


def table(path):
    """A VizieR asu-tsv file as dicts of stripped strings (the header, the
    units and the dashes are its first three non-comment lines)."""
    lines = [line.rstrip("\n").split("\t") for line in open(path, encoding="utf-8")
             if line.strip() and not line.startswith("#")]
    return [dict(zip(lines[0], (c.strip() for c in row))) for row in lines[3:]]


def arp_members(path):
    """Arp number -> its member galaxies as (name, RA, Dec, major arcmin, V)."""
    out = {}
    for t in table(path):
        out.setdefault(int(t["Arp"]), []).append(
            (designation(re.sub(r"^MESSIER\b", "M", t["Name"])), float(t["_RAJ2000"]), float(t["_DEJ2000"]), num(t["dim1"]), num(t["VT"])))
    return out


def extent(members):
    """The arcminutes the members span: the widest pair's separation plus
    their half sizes, or the one galaxy's size; None without sizes."""
    sized = [m for m in members if m[3]]
    if not sized:
        return None
    return round(max([m[3] for m in sized] + [sep(a[1], a[2], b[1], b[2]) + (a[3] + b[3]) / 2
                                              for a in sized for b in sized if a is not b]), 1)


def combined(mags):
    """The combined magnitude of distinct members' magnitudes, None without any."""
    flux = sum(10 ** (-0.4 * m) for m in mags if m)
    return round(-2.5 * math.log10(flux), 1) if flux else None


def distinct(members):
    """The members once per name (arplist repeats a galaxy for its parts)."""
    return list({m[0]: m for m in members}.values())


def catalog(name, args):
    """(designation, other spellings, RA, Dec, size, type, mag, bmag,
    description) per VizieR row of catalogue name, in catalogue order."""
    rows = table(args[0])
    for t in rows:
        ra, dec = float(t["_RAJ2000"]), float(t["_DEJ2000"])
        if name == "Barnard":
            yield f"Barnard {t['Barn']}", [], ra, dec, num(t["Diam"]), "Dark Nebula", None, None, []
        elif name == "vdB":
            radius = max(filter(None, (num(t["BRadMax"]), num(t["RRadMax"]))), default=None)
            star = f"HD {t['HD']}" if t["HD"] else " ".join(t["DM"].split())
            yield (f"vdB {t['VdB']}", [], ra, dec, radius and round(2 * radius, 1), "Reflection Nebula", None, None,
                   [f"illuminated by {star}"] if star else [])
        elif name == "Arp":
            members = distinct(MEMBERS.get(int(t["Arp"]), []))
            typ = {1: "Galaxy", 2: "Galaxy Duo"}.get(len(members), "Galaxy Group")
            # Webb's view name ("NGC 4038 + 39") only when no member is listed.
            yield (f"Arp {t['Arp']}", [], ra, dec, extent(MEMBERS.get(int(t["Arp"]), [])), typ,
                   combined(m[4] for m in members), None, [m[0] for m in members] or [designation(t["Name"])])
        elif name == "HCG":
            yield f"HCG {t['HCG']}", [f"Hickson {t['HCG']}"], ra, dec, num(t["AngSize"]), "Galaxy Group", None, num(t["Totmag"]), []
        elif name == "vdBH":
            diam = max(filter(None, (num(t["BDiam"]), num(t["RDiam"]))), default=None)
            yield (f"vdBH {t['BHe']}{t['m_BHe'].replace(',', '')}", [], ra, dec, diam, "Reflection Nebula", None, None,
                   [designation(t["IDs"])] if t["IDs"] else [])


def own(r, names):
    """Whether Compendium row r is the object itself: named, or
    alternative-ID'd, with one of its designations (spaces aside)."""
    keys = {re.sub(r"\s", "", n).lower() for n in names}
    return any(re.sub(r"\s", "", designation(i)).lower() in keys for i in [r["A"]] + (r.get("R") or "").split(","))


MEMBERS = {}

# The Compendium's group types (imm2yaml.TYPES): a Hickson group's own row is one.
GROUPS = {"Galaxy Group", "Galaxy Duo", "Galaxy Cluster"}


def main():
    name, args = sys.argv[1], sys.argv[2:]
    xlsx = args.pop() if args[-1].endswith((".xlsx", ".xlsm")) else None
    if name == "Arp":
        MEMBERS.update(arp_members(args[1]))
    index = aliases(xlsx) if xlsx else {}
    q = lambda s: json.dumps(s, ensure_ascii=False)  # JSON strings are valid YAML
    source = {"Barnard": "Barnard (1927), CDS VII/220A", "vdB": "van den Bergh (1966), CDS VII/21",
              "Arp": "Arp (1966) in Webb's index (1996), CDS VII/192", "HCG": "Hickson (1982), CDS VII/213",
              "vdBH": "van den Bergh & Herbst (1975), CDS VII/218"}[name]
    print(f"# Generated by scripts/vizier2yaml.py: {source}" + (
        ",\n# cross-identified with Gary Imm's Deep Sky Compendium (2026) - data (c) Gary Imm." if xlsx else "."))
    unsettled = []
    for des, spellings, ra, dec, size, typ, mag, bmag, desc in catalog(name, args):
        names = [des] + spellings
        rows = list({id(r): r for n in names for r in index.get(n, []) + index.get(n.replace(" ", ""), [])}.values())
        rows.sort(key=lambda r: sep(*position(r), ra, dec))
        # A group's own row is a group's: HCG 10's NGC 536 row is a member's.
        mine = [r for r in rows if own(r, names) and (typ != "Galaxy Group" or kind(r) in GROUPS)]
        surfbr, nick, other = None, [], []
        if mine:
            r = mine[0]
            far = sep(*position(r), ra, dec) > max(20, (size or 0) / 2)
            if far and des not in POSITION:
                unsettled.append(f"{des}: Compendium {designation(r['A'])} {sep(*position(r), ra, dec):.0f}' from VizieR")
            if not far or POSITION.get(des) == "compendium":
                ra, dec = position(r)
            rsize = num(r.get("G"))
            if rsize and (not size or size / 4 <= rsize <= size * 4) and not (far and POSITION.get(des) == "vizier"):
                size = rsize
            typ = kind(r)
            # The Compendium's Hickson magnitudes are Hickson's total B, and its
            # surface brightness that over the group's disc: bmag says it all.
            if name != "HCG":
                mag, surfbr = num(r.get("T")) or mag, num(r.get("U"))
            # "Stephan's Quintet Member", "Butterfly Nebula (3 of 3)": notes, not names.
            nick = re.split(r"\s*,\s*", re.sub(r"\s*\([^)]*\)|\s+Member$", "", " ".join((r.get("Q") or "").split())))
            if not own(r, [des]):  # HCG 18's row is Arp 258
                other.append(designation(r["A"]))
        for r in rows:
            # Barnard 44a's row carries 44 in its cross-ID column: another object.
            if r in mine or re.fullmatch(re.escape(des) + r"[a-z]", designation(r["A"])):
                continue
            if (d := sep(*position(r), ra, dec)) <= max(20, (size or 0) / 2):
                other.append(designation(r["A"]))
            else:
                print(f"warning: {des}: Compendium {designation(r['A'])} is {d:.0f}' away, ignored", file=sys.stderr)
        # Common names, other designations, then members or star; once each,
        # spacing aside (Webb spells MCG-02-05-50 +A both ways).
        seen = {re.sub(r"\s", "", n).lower() for n in names}
        desc = [d for d in nick + other + desc if d and not (k := re.sub(r"\s", "", d).lower()) in seen and not seen.add(k)]
        if bmag:
            print(f"- bmag: {bmag}")
            print('  constellation: ""')
        else:
            print('- constellation: ""')  # the loader computes it from RA/Dec
        print(f"  dec: {q(sexagesimal(dec))}")
        print(f"  description: {q(', '.join(desc))}")
        print(f"  mag: {mag if mag else -9999}")
        print(f"  name: {q(des)}")
        print(f"  ra: {q(sexagesimal(ra, hours=True))}")
        print(f"  size: {size if size else -9999}")
        if surfbr:
            print(f"  surfbr: {round(surfbr, 2)}")
        print(f"  type: {q(typ)}")
    if unsettled:
        sys.exit("error: own rows far from VizieR, check against SIMBAD and add to POSITION:\n  " + "\n  ".join(unsettled))


if __name__ == "__main__":
    main()
