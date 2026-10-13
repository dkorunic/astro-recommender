#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Build the Barnard, van den Bergh, Arp, Hickson, van den Bergh-Herbst,
Cederblad and Abell (ACO) catalogues as uptonight target lists from their
CDS/VizieR tables, and Gum's H II regions and the cometary globules from
SIMBAD, cross-identified with Gary Imm's Deep Sky Compendium (vdBH has no
Compendium rows and is built from VizieR alone).

    V='https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-out.all&-out.max=5000&-out.add=_RAJ,_DEJ&-oc.form=dec&-source='
    curl -o barnard.tsv "${V}VII/220A/barnard"   # Barnard 1927, 349 dark nebulae
    curl -o vdb.tsv "${V}VII/21/catalog"         # van den Bergh 1966, 158 reflection nebulae
    curl -o arpord.tsv "${V}VII/192/arpord"      # Arp 1966 (Webb 1996 index), 338 objects
    curl -o arplist.tsv "${V}VII/192/arplist"    # ... and their 592 member galaxies
    curl -o hcg.tsv "${V}VII/213/groups"         # Hickson 1982, 100 compact groups
    curl -o vdbh.tsv "${V}VII/218/catalog"       # van den Bergh & Herbst 1975, 136 southern reflection nebulae
    curl -o ced.tsv "${V}VII/231/catalog"        # Cederblad 1946, 330 bright diffuse nebulae
    for t in 3 4 5; do curl -o aco$t.tsv "${V}VII/110A/table$t"; done  # ACO 1989: northern, southern, supplementary
    curl -o simbad.tsv --get https://simbad.cds.unistra.fr/simbad/sim-tap/sync -d REQUEST=doQuery -d LANG=ADQL \
        -d FORMAT=tsv --data-urlencode "QUERY=SELECT ident.id, basic.main_id, basic.otype, basic.ra, basic.dec,
        basic.galdim_majaxis, basic.galdim_minaxis FROM ident JOIN basic ON ident.oidref = basic.oid
        WHERE ident.id LIKE 'GUM %' OR ident.id LIKE 'CG %'"   # 2026-10-12: 97 Gum and 56 CG identifiers
    X=IMM_Compendium_2026.xlsx T=internal/catalog/targets
    scripts/vizier2yaml.py Barnard barnard.tsv $X > $T/Barnard.yaml
    scripts/vizier2yaml.py vdB vdb.tsv $X > $T/vdB.yaml
    scripts/vizier2yaml.py Arp arpord.tsv arplist.tsv $X > $T/Arp.yaml
    scripts/fillmag.py --minor-only $T/Arp.yaml > /tmp/a.yaml && mv /tmp/a.yaml $T/Arp.yaml
    scripts/vizier2yaml.py HCG hcg.tsv $X > $T/HCG.yaml
    scripts/vizier2yaml.py vdBH vdbh.tsv > $T/vdBH.yaml
    scripts/vizier2yaml.py Ced ced.tsv $X > $T/Ced.yaml
    scripts/vizier2yaml.py ACO aco3.tsv aco4.tsv aco5.tsv $X > $T/ACO.yaml
    scripts/vizier2yaml.py Gum simbad.tsv $X > $T/Gum.yaml
    scripts/vizier2yaml.py CG simbad.tsv $X > $T/CG.yaml

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

Cederblad's type follows his nebular spectrum (emission makes an H II region,
whatever the class) and class (A, a cluster); his size is the larger
dimension. ACO keeps the clusters with a redshift, sized by the Abell radius
1.72'/z (which matches the Compendium's sizes), plus any the Compendium has
(as "AbellG N"; "ACO", since "Abell N" is a planetary nebula in the lists).
Gum and CG take SIMBAD's position, size where it has one (few) and main
identifier (not a bracketed survey name); "far" is then judged by the
Compendium's size, since Gum's H II regions span degrees. CG keeps
numbers to 99 (CG 348 and the like are galaxies), and not a lettered part
that is a young star (CG 30N) or 5 degrees from its globule (CG 7S). SIMBAD
rows come in no set order, so both are sorted by number.
"""

import csv
import json
import math
import re
import sys

from imm2yaml import aliases, designation, kind, num, position, sexagesimal
from sharpless2yaml import sep

# Own rows far from VizieR, settled against SIMBAD (Sesame, 2026-10-12):
# "vizier" where the Compendium's coordinates are typos, "compendium" where
# VizieR's are, "none" where the row is another object (SIMBAD keeps them
# apart), which is then ignored.
POSITION = {
    "Barnard 134": "vizier", "Barnard 144": "vizier", "Barnard 344": "vizier",
    "vdB 5": "vizier", "vdB 32": "vizier", "vdB 39": "vizier",
    "Arp 8": "vizier", "Arp 67": "vizier", "Arp 201": "vizier", "Arp 258": "vizier",
    "HCG 18": "vizier",  # its own row is Arp 258's
    "HCG 48": "compendium",
    "Gum 30": "vizier",  # the Compendium's declination 10° off NGC 3293's
    "Gum 77a": "none",   # the Compendium's alt-ID on Sh2-34, 2.1° away
}


def table(path):
    """A VizieR asu-tsv file as dicts of stripped strings (the header, the
    units and the dashes are its first three non-comment lines)."""
    lines = [line.rstrip("\n").split("\t") for line in open(path, encoding="utf-8")
             if line.strip() and not line.startswith("#")]
    return [dict(zip(lines[0], (c.strip() for c in row))) for row in lines[3:]]


def simbad(path):
    """A SIMBAD TAP TSV (header line, quoted strings) as dicts."""
    return [{k: v.strip().strip('"') for k, v in row.items()} for row in csv.DictReader(open(path, encoding="utf-8"), delimiter="\t")]


# Cederblad's nebular spectrum (E/e emission, C/c continuous) and class (A:
# nebulous clusters) as list types: an emission spectrum makes an H II region
# whatever its class (Ced 7 is IC 1805, the Heart), so that -filter sees it;
# a continuous one around a cluster is the Pleiades' kind.
def ced_type(t):
    if t["SpNeb"][:1] in "Ee":
        return "Emission Nebula"
    return "Cluster Nebulosity" if t["Class"].startswith("A") else "Reflection Nebula"


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
    description) per VizieR (SIMBAD for Gum and CG) row of catalogue name,
    in catalogue order."""
    if name == "ACO":
        # Rich (table 3, 4) and supplementary southern (table 5) clusters;
        # only those with a redshift have a size: the Abell radius 1.72'/z,
        # taken as the diameter, which matches the Compendium's sizes
        # (Coma 74' for 64', Hercules 47' for 60'); without one a cluster is
        # kept only when the Compendium has it (main).
        for path in args:
            for t in table(path):
                z = num(t.get("z"))
                num_ = f"S {t['ACOS']}" if "ACOS" in t else t["ACO"]
                yield (f"ACO {num_}", [f"AbellG {num_.replace(' ', '')}"], float(t["_RAJ2000"]), float(t["_DEJ2000"]),
                       z and round(1.72 / z, 1), "Galaxy Cluster", None, None, [f"z = {z}"] if z else [])
        return
    if name in ("Gum", "CG"):
        prefix = {"Gum": "GUM", "CG": "CG"}[name]
        seen = set()
        # TAP returns rows in no set order: catalogue order keeps the output stable.
        order = lambda t: (int(m.group(1)), m.group(2)) if (m := re.search(r"(\d+)(\S*)$", t["id"])) else (0, t["id"])
        rows = sorted(simbad(args[0]), key=order)
        base = {re.sub(r"\s+", " ", t["id"]): t for t in rows}
        for t in rows:
            m = re.fullmatch(prefix + r"\s+(\d+)([A-Za-z]?\d?)", t["id"])
            # CG 348 and the like are galaxies of another list (Zealey and
            # Reipurth number to 38); a lettered CG that is a young star (CG
            # 30N) or 5° from its globule (CG 7S, at +61°) is not a globule.
            if not m or t["id"] in seen:
                continue
            if name == "CG":
                b = base.get(f"CG {m.group(1)}")
                # Both positions are needed for the distance: SIMBAD may lack either.
                if int(m.group(1)) > 99 or (m.group(2) and (t["otype"] == "Y*O" or (
                        b and None not in (num(b["ra"]), num(b["dec"]), num(t["ra"]), num(t["dec"])) and sep(num(b["ra"]), num(b["dec"]), num(t["ra"]), num(t["dec"])) > 300))):
                    continue
            seen.add(t["id"])
            # SIMBAD's bracketed survey names ([DB2002b] G259.43-12.72) are no help.
            main_id = "" if t["main_id"].startswith("[") else designation(t["main_id"])
            des = f"{name} {m.group(1)}{m.group(2)}"
            # A SIMBAD object without a position takes the Compendium row's (main).
            yield (des, [], num(t["ra"]), num(t["dec"]), num(t["galdim_majaxis"]),
                   "HII Region" if name == "Gum" else "Dark Nebula", None, None,
                   [main_id] if main_id and main_id.replace(" ", "").lower() != des.replace(" ", "").lower() else [])
        return
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
        elif name == "Ced":
            dims = [d for d in (num(t["Dim1"]), num(t["Dim2"])) if d]
            other = designation(t["Name"]) if re.match(r"[A-Za-z]", t["Name"]) else None
            yield (f"Ced {t['Ced']}{t['m_Ced']}", [], ra, dec, max(dims, default=None), ced_type(t), None, None,
                   [other] if other else [])
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
              "vdBH": "van den Bergh & Herbst (1975), CDS VII/218", "Ced": "Cederblad (1946), CDS VII/231",
              "ACO": "Abell, Corwin & Olowin (1989), CDS VII/110A",
              "Gum": "Gum (1955), positions from SIMBAD", "CG": "cometary globules (Hawarden & Brand 1976, Zealey et al. 1983, Reipurth 1983), positions from SIMBAD"}[name]
    print(f"# Generated by scripts/vizier2yaml.py: {source}" + (
        ",\n# cross-identified with Gary Imm's Deep Sky Compendium (2026) - data (c) Gary Imm." if xlsx else "."))
    unsettled = []
    for des, spellings, ra, dec, size, typ, mag, bmag, desc in catalog(name, args):
        names = [des] + spellings
        rows = list({id(r): r for n in names for r in index.get(n, []) + index.get(n.replace(" ", ""), [])}.values())
        if ra is None:
            own_rows = [r for r in rows if own(r, names)]
            if not own_rows:
                print(f"warning: {des}: no position in SIMBAD nor a Compendium row, skipped", file=sys.stderr)
                continue
            ra, dec = position(own_rows[0])
        rows.sort(key=lambda r: sep(*position(r), ra, dec))
        # A group's or cluster's own row is a group's: HCG 10's NGC 536 row is
        # a member's, and ACO 21 must not take Abell 21's planetary nebula.
        mine = [r for r in rows if own(r, names) and (typ not in ("Galaxy Group", "Galaxy Cluster") or kind(r) in GROUPS)
                and POSITION.get(des) != "none"]
        surfbr, nick, other = None, [], []
        if mine:
            r = mine[0]
            # SIMBAD gives few nebula sizes: Gum's giant H II regions are then
            # judged by the Compendium's (Gum 55, 7° across, 3° off).
            ref = size or (num(r.get("G")) if name in ("Gum", "CG") else None)
            far = sep(*position(r), ra, dec) > max(20, (ref or 0) / 2)
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
        if name == "ACO" and not size:
            continue  # no redshift and no Compendium row: no size to frame or limit by
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
