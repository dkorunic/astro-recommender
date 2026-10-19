#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Build the Barnard, van den Bergh, Arp, Hickson, van den Bergh-Herbst,
Cederblad, Abell (ACO), Magakian (MCRN), DEM, Dias, Hartley (DCld) and
Arp-Madore (AM) catalogues as uptonight target lists from their CDS/VizieR
tables, Gum's H II regions, the cometary globules and Henize's Magellanic
nebulae from SIMBAD, and Harris' globular clusters from his own file,
cross-identified with Gary Imm's Deep Sky Compendium (vdBH has no Compendium
rows and is built from VizieR alone).

    V='https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-out.all&-out.max=10000&-out.add=_RAJ,_DEJ&-oc.form=dec&-source='
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
    curl -o mcrn.tsv "${V}J/A+A/399/141/table1"  # Magakian 2003, 913 reflection nebulae
    curl -o deml.tsv "${V}VII/232/lmc"           # Davies, Elliott & Meaburn 1976: 358 LMC ...
    curl -o dems.tsv "${V}VII/232/smc"           # ... and 168 SMC H II regions
    curl -o ocl.tsv "${V}B/ocl/clusters"         # Dias et al. 2002 (v3.5, 2016), 2167 open clusters
    curl -o dcld.tsv "${V}VII/191/table1"        # Hartley et al. 1986, 1101 southern dark clouds
    curl -o am.tsv "${V}VII/170/catalog"         # Arp & Madore 1987, 6445 southern peculiar galaxies
    curl -o mwgc.dat https://physics.mcmaster.ca/~harris/mwgc.dat  # Harris 1996, 2010 edition, 157 globulars
    curl -o henize.tsv --get https://simbad.cds.unistra.fr/simbad/sim-tap/sync -d REQUEST=doQuery -d LANG=ADQL \
        -d FORMAT=tsv --data-urlencode "QUERY=SELECT ident.id, basic.main_id, basic.otype, basic.ra, basic.dec,
        basic.galdim_majaxis, basic.galdim_minaxis FROM ident JOIN basic ON ident.oidref = basic.oid
        WHERE ident.id LIKE 'LHA 120-N %' OR ident.id LIKE 'LHA 115-N %'"   # 2026-10-13: 454 LMC and 130 SMC identifiers
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
    scripts/vizier2yaml.py MCRN mcrn.tsv $X > $T/MCRN.yaml
    scripts/vizier2yaml.py Henize henize.tsv deml.tsv dems.tsv $X > $T/Henize.yaml
    scripts/vizier2yaml.py DEM deml.tsv dems.tsv $X > $T/DEM.yaml
    scripts/vizier2yaml.py Harris mwgc.dat $X > $T/Harris.yaml
    scripts/vizier2yaml.py Dias ocl.tsv $X > $T/Dias.yaml
    scripts/vizier2yaml.py DCld dcld.tsv $X > $T/DCld.yaml
    scripts/vizier2yaml.py AM am.tsv $X > $T/AM.yaml

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
checked against SIMBAD: the Compendium has typos (vdB 156's Dec, ~1° off),
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

MCRN has no sizes: its cross-IDs (vdB, NGC/IC, Ced, Bernes...) find the
Compendium row that gives one, and fill the description. Henize keeps
SIMBAD's nebulae (HENIZE_TYPES), named "LMC N44"; DEM's axes fill the sizes
SIMBAD lacks through DEM's Henize cross-IDs. Harris' clusters always take the
Compendium's size, since Harris gives half-light radii (HALF_LIGHT times
r_h otherwise), and keep his integrated V. Dias drops the classes that are no
cluster (DIAS_DROP) and keeps its own diameters; a Dias cluster whose
Compendium row is a nebula (NGC 2264) is a cluster with nebulosity. DEM and
DCld carry minor axes: the catalogue's axis ratio on the size kept, only
while that is within 2x of the catalogue's major axis. Arp-Madore's type
comes from its description (am_type).
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
    # rev6b fixed the Compendium's Barnard 134/144/344, vdB 5/32/39, Arp 8/67/201/258
    # and Gum 30 typos, and dropped Gum 77a's alt-ID from Sh2-34.
    "HCG 48": "compendium",
    "MCRN 902": "vizier",  # vdB 156's row: RA fixed in rev6b, Dec still ~1° off omi And
    "MCRN 767": "none",  # the Serpens nebula; Sh2-68 is 1.3° away
    # Harris and Dias designations are names other lists use too (Arp 2 is
    # also Arp's galaxy): their entries are keyed by (list, designation).
    ("Harris", "Arp 2"): "none",  # the globular; the Compendium's Arp 2 is the galaxy
    ("Dias", "NGC 6664"): "compendium",  # Dias 23' off SIMBAD
}


def settled(name, des):
    """How POSITION settles catalogue name's des: "vizier", "compendium",
    "none", or None when it does not."""
    return POSITION.get((name, des), POSITION.get(des))


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


def mcrn_ids(t):
    """Magakian's cross-identifications as the lists spell them: DG, Racine
    (RK68), vdB, vdBH, Bernes ([B77]), NGC/IC, GN, Cederblad, Sh2 and the
    Other column (RNO, LkHa, GM, a star). A lettered range (vdB 1a-d) is the
    number."""
    out = []
    for col, fmt in (("DG", "DG {}"), ("[RK68]", "[RK68] {}"), ("VdB", "vdB {}"), ("VdBH", "vdBH {}"), ("[B77]", "Bernes {}"),
                     ("Ced", "Ced {}"), ("GN", "GN {}")):
        last = None
        for v in t[col].split(","):
            v = re.sub(r"(?<=\d)[a-z]-[a-z]$", "", v.strip())
            if re.fullmatch(r"[a-z]", v) and last:  # "4a,b": 4b
                v = re.sub(r"[a-z]?$", v, last, count=1)
            if v:
                out.append(designation(fmt.format(v)))
                last = v
    out += [designation(re.sub(r"^(NGC|IC)(?=\d)", r"\1 ", v)) for v in t["NGC/IC"].split(",") if v.strip()]
    out += [re.sub(r"^Sh(?=\d)", "Sh2-", v.strip()) for v in t["Other"].split(",") if v.strip()]
    return out


def henize_names(s, cloud):
    """DEM's Henize cross-IDs ("N77C,B", "N11") as "LMC N77C", "LMC N77B"."""
    out = []
    for part in filter(None, (p.strip() for p in s.split(","))):
        if m := re.fullmatch(r"N(\d+)([A-Z]?)", part):
            out.append(f"{cloud} N{m.group(1)}{m.group(2)}")
        elif out and re.fullmatch(r"[A-Z]", part):  # "N77C,B": B of the same number
            out.append(re.sub(r"[A-Z]?$", part, out[-1], count=1))
    return out


def dem_name(t, cloud):
    """DEM L 1, DEM S 4a, as SIMBAD spells them."""
    return f"DEM {cloud[0]} {t['DEM']}{t['m_DEM']}"


# SIMBAD's types of Henize's objects that are nebulae, as list types.
HENIZE_TYPES = {"EmO": "HII Region", "HII": "HII Region", "SFR": "HII Region", "reg": "HII Region",
                "SNR": "Supernova Remnant", "Cl*": "Cluster Nebulosity"}

# Dias's classes (note G1) that are no cluster: asterisms and dust holes,
# non-existent NGCs, objects not found on the DSS, dubious ones and possible
# remnants (POCRs).
DIAS_DROP = {"a", "n", "nf", "d", "p", "--"}
# Dias's coordinate typos, SIMBAD's positions instead (Sesame, 2026-10-13):
# NGC 7762's 23h59 for 23h49, Ruprecht 53's -26 for -27, NGC 2413's -13 24
# for -13 02.
DIAS_TYPOS = {"NGC 7762": (357.47204, 68.03500), "Ruprecht 53": (122.70000, -27.00000), "NGC 2413": (113.27500, -13.03331)}

# The visual diameter per arcminute of half-light radius, for the globulars
# the Compendium lacks: its median size over r_h for the 93 it has (2026-10-13;
# quartiles 5.6 and 10.6: Harris's r_h is a light profile, not an extent).
HALF_LIGHT = 7.7


def harris(path):
    """Harris (1996, 2010 edition) as (ID, other name, RA, Dec, V_t, r_h):
    Part I's positions joined by ID to Part II's integrated V and Part III's
    half-light radius (arcmin), fixed columns."""
    part, rows, v, rh = 0, [], {}, {}
    for line in open(path, encoding="latin-1"):
        if re.match(r"\s+Part I+:", line):
            part = line.split(":")[0].strip().count("I")
            continue
        i = line[:12].strip()
        if not i or i == "ID" or not re.match(r" \S", line) or line.startswith((" _", " (")):
            continue
        if part == 1 and (m := re.match(r"(\d\d \d\d [\d.]+)\s+([-+]\d\d \d\d [\d.]+)", line[25:].strip())):
            h, mi, se = (float(x) for x in m.group(1).split())
            d, dm, ds = (float(x) for x in m.group(2).lstrip("+-").split())
            sign = -1 if m.group(2).startswith("-") else 1
            rows.append((i, line[12:25].strip(), (h + mi / 60 + se / 3600) * 15, sign * (d + dm / 60 + ds / 3600)))
        elif part == 2:
            v[i] = num(line[40:46])
        elif part == 3:
            rh[i] = num(line[64:70])
    for i, ident, ra, dec in rows:
        yield designation(i), ident, ra, dec, v.get(i), rh.get(i)


# Arp & Madore's description codes (VII/170 note 4) by the type they make.
AM_PN, AM_NEBULA = {25}, {21}  # planetary nebulae; stars with nebulosity
AM_GROUP = {3, 4, 5, 17, 18, 19, 24}  # triples to quintets, chains, groups, clusters
AM_DUO = {2, 23}  # interacting doubles, close pairs


def galactic(ra, dec):
    """J2000 RA/Dec in degrees -> galactic (l, b) in degrees."""
    ra, dec = math.radians(ra), math.radians(dec)
    rap, decp, l0 = math.radians(192.85948), math.radians(27.12825), math.radians(122.93192)
    b = math.asin(math.sin(dec) * math.sin(decp) + math.cos(dec) * math.cos(decp) * math.cos(ra - rap))
    l = l0 - math.atan2(math.cos(dec) * math.sin(ra - rap),
                        math.sin(dec) * math.cos(decp) - math.cos(dec) * math.sin(decp) * math.cos(ra - rap))
    return math.degrees(l) % 360, math.degrees(b)


def dcld_name(t):
    """Hartley's designation, rebuilt from the position where VizieR's is off
    it by more than its rounding: 14 rows lost the latitude's sign or tens
    (266.0-04.3 is at b +4.3, 265.5-00.0 at b -10.0, 263.7-00.0 at -1.0)."""
    l, b = galactic(float(t["_RAJ2000"]), float(t["_DEJ2000"]))
    nl, nb = float(t["DCld"][:5]), float(t["DCld"][5:])
    if abs((l - nl + 180) % 360 - 180) > 0.15 or abs(b - nb) > 0.15:
        return f"{l:05.1f}{b:+05.1f}{t['n_DCld']}"
    return t["DCld"] + t["n_DCld"]


def am_type(codes, members):
    """An Arp-Madore entry's type from its description codes and the number
    of rows (members) it has."""
    if codes & AM_PN:
        return "Planetary Nebula"
    if codes & AM_NEBULA:
        return "Nebula"
    if codes & AM_GROUP or members > 2:
        return "Galaxy Group"
    return "Galaxy Duo" if codes & AM_DUO or members == 2 else "Galaxy"


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
    if name == "Henize":
        # Henize's numbers carry stars and planetaries too: only the nebulae
        # (EmO, HII, SFR, reg, SNR) and the clusters in them (Cl*) are kept.
        # SIMBAD gives few sizes; DEM's (args[1:]) fill them through its
        # Henize cross-IDs, with its minor axis.
        dem = {}
        for path, cloud in zip(args[1:], ("LMC", "SMC")):
            for t in table(path):
                for n in henize_names(t["Name"], cloud):
                    dem.setdefault(n, (num(t["Maj"]), num(t["Min"]), dem_name(t, cloud)))
        order = lambda t: (t["id"][4:7], int(m.group(1)), m.group(2)) if (m := re.search(r"N\s+(\d+)(\S*)$", t["id"])) else ("", 0, t["id"])
        seen = set()
        for t in sorted(simbad(args[0]), key=order):
            m = re.fullmatch(r"LHA 1(20|15)-N\s+(\d+)([A-Za-z]?\d?)", t["id"])
            typ = HENIZE_TYPES.get(t["otype"])
            if not m or not typ or t["id"] in seen:
                continue
            seen.add(t["id"])
            cloud = "LMC" if m.group(1) == "20" else "SMC"
            des = f"{cloud} N{m.group(2)}{m.group(3)}"
            maj, mnr, other = dem.get(des, (None, None, None))
            size = num(t["galdim_majaxis"]) or maj
            minor = num(t["galdim_minaxis"]) if num(t["galdim_majaxis"]) else mnr
            main_id = designation(t["main_id"])
            desc = [d for d in (main_id if not main_id.startswith(("LHA", "[")) else None, other) if d]
            yield des, [f"N{m.group(2)}{m.group(3)} ({cloud})"], num(t["ra"]), num(t["dec"]), size, typ, None, None, desc, minor
        return
    if name == "DEM":
        for path, cloud in zip(args, ("LMC", "SMC")):
            last = None
            for t in table(path):
                # VizieR repeats DEM L 120's record as a second DEM L 121
                # ahead of the real one (N33): a record equal to the last.
                rec = (t["RA1950"], t["DE1950"], t["Maj"], t["Min"])
                if rec == last:
                    continue
                last = rec
                yield (dem_name(t, cloud), [], float(t["_RAJ2000"]), float(t["_DEJ2000"]),
                       num(t["Maj"]), "HII Region", None, None, henize_names(t["Name"], cloud), num(t["Min"]))
        return
    if name == "AM":
        # An association's members are rows of one designation (n_AM points
        # to the previous member): one entry at their mean position, sized
        # by their extent, typed by the description codes and member count.
        groups = {}
        for t in table(args[0]):
            groups.setdefault(t["AM"], []).append(t)
        for des, rows in groups.items():
            ra = [float(t["_RAJ2000"]) for t in rows]
            ra = [r - 360 if r - ra[0] > 180 else r + 360 if ra[0] - r > 180 else r for r in ra]
            dec = [float(t["_DEJ2000"]) for t in rows]
            codes = {int(c) for t in rows for c in (t[f"CD{i}"] for i in range(1, 6)) if c}
            descs = list(dict.fromkeys(d for t in rows if (d := re.sub(r"\s*\(#\d+\)", "", " ".join(t["Desc"].split())))))
            members = [("", float(t["_RAJ2000"]), float(t["_DEJ2000"]), num(t["Diam"]), None) for t in rows]
            yield (f"AM {des}", [], sum(ra) / len(ra) % 360, sum(dec) / len(dec), extent(members), am_type(codes, len(rows)),
                   None, None, ["; ".join(descs)] if descs else [])
        return
    if name == "Harris":
        for i, ident, ra, dec, mag, rh in harris(args[0]):
            # Harris's short IDs (2MS-GC01, ESO-SC06, 1636-283) for the names
            # SIMBAD and the literature use, kept in the description.
            if re.match(r"2MS-|ESO-|\d", i):
                i, ident = ident, i
            # Palomar's are "Pal N" here, "Palomar N" in the Compendium.
            spellings = [re.sub(r"^Pal ", "Palomar ", i)] if i.startswith("Pal ") else []
            yield i, spellings, ra, dec, rh and round(HALF_LIGHT * rh, 1), "Globular Cluster", mag, None, [ident] if ident else []
        return
    rows = table(args[0])
    seen = set()
    dcld = {}
    for t in rows:
        if name == "DCld":
            dcld.setdefault(dcld_name(t), []).append(t)
    for t in rows:
        ra, dec = float(t["_RAJ2000"]), float(t["_DEJ2000"])
        if name == "MCRN":
            ids = mcrn_ids(t)
            typ = "Emission Nebula" if t["Class"].startswith("E") else "Reflection Nebula"
            yield (f"MCRN {t['Seq']}", [i for i in ids if not i.startswith(("DG ", "GN ", "[RK68]"))], ra, dec, None, typ, None, None,
                   ids + ([t["Comments"]] if t["Comments"] else []))
        elif name == "Dias":
            cls = t["Class"]
            # FSR 1496 is listed twice, identically.
            if cls in DIAS_DROP or t["Cluster"] in seen:
                continue
            seen.add(t["Cluster"])
            ra, dec = DIAS_TYPOS.get(t["Cluster"], (ra, dec))
            typ = "Cluster Nebulosity" if "e" in cls else "Globular Cluster" if cls == "g" else "Open Cluster"
            note = {"o": "OB association", "oe": "OB association", "m": "moving group", "cr": "remnant"}.get(cls)
            yield t["Cluster"], [], ra, dec, num(t["Diam"]), typ, None, None, [note] if note else []
        elif name == "DCld":
            des = dcld_name(t)
            # Hartley gives two clouds 2-4' apart each of DCld 315.8-27.5 and
            # 349.2+03.1: both kept, as he names them.
            note = ["one of two clouds of this designation"] if len(dcld[des]) > 1 else []
            yield f"DCld {des}", [], ra, dec, num(t["Size1"]), "Dark Nebula", None, None, note, num(t["Size2"])
        elif name == "Barnard":
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

# The Compendium's emission types (imm2yaml.TYPES, kind).
EMISSION = {"Emission Nebula", "Planetary Nebula", "Supernova Remnant", "Wolf-Rayet Nebula"}

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
              "MCRN": "Magakian (2003), CDS J/A+A/399/141", "DEM": "Davies, Elliott & Meaburn (1976), CDS VII/232",
              "Henize": "Henize (1956), positions from SIMBAD, sizes from DEM (CDS VII/232) where SIMBAD has none",
              "Harris": "Harris (1996, 2010 edition), https://physics.mcmaster.ca/~harris/mwgc.dat",
              "Dias": "Dias et al. (2002, version 3.5 2016), CDS B/ocl", "DCld": "Hartley et al. (1986), CDS VII/191",
              "AM": "Arp & Madore (1987), CDS VII/170",
              "Gum": "Gum (1955), positions from SIMBAD", "CG": "cometary globules (Hawarden & Brand 1976, Zealey et al. 1983, Reipurth 1983), positions from SIMBAD"}[name]
    print(f"# Generated by scripts/vizier2yaml.py: {source}" + (
        ",\n# cross-identified with Gary Imm's Deep Sky Compendium (2026) - data (c) Gary Imm." if xlsx else "."))
    unsettled = []
    for des, spellings, ra, dec, size, typ, mag, bmag, desc, *rest in catalog(name, args):
        minor = rest[0] if rest else None
        major = size  # the catalogue's own, which the minor axis goes with
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
                and settled(name, des) != "none"]
        surfbr, nick, other = None, [], []
        if mine:
            r = mine[0]
            # SIMBAD gives few nebula sizes: Gum's giant H II regions are then
            # judged by the Compendium's (Gum 55, 7° across, 3° off).
            ref = size or (num(r.get("G")) if name in ("Gum", "CG") else None)
            far = sep(*position(r), ra, dec) > max(20, (ref or 0) / 2)
            if far and settled(name, des) is None:
                unsettled.append(f"{des}: Compendium {designation(r['A'])} {sep(*position(r), ra, dec):.0f}' from VizieR")
            if not far or settled(name, des) == "compendium":
                ra, dec = position(r)
            rsize = num(r.get("G"))
            # A globular cluster's half-light diameter is no visual size: the
            # Compendium's always wins. Dias's diameters are the list's point.
            if rsize and (not size or name == "Harris" or (name != "Dias" and size / 4 <= rsize <= size * 4)) \
                    and not (far and settled(name, des) == "vizier"):
                size = rsize
            typ = kind(r)
            # A Dias cluster named like the nebula it lies in (NGC 2264, the
            # Trifid's NGC 6514): the Compendium row is the nebula, the object
            # both, typed as OpenNGC types them, so -filter still sees emission.
            if name == "Dias" and "Cluster" not in typ:
                typ = "Cl+N Emission Nebula" if typ in EMISSION else "Cluster Nebulosity"
            # The Compendium's Hickson magnitudes are Hickson's total B, and its
            # surface brightness that over the group's disc: bmag says it all.
            # Harris's integrated V is the cluster's own.
            if name != "HCG":
                mag, surfbr = (mag if name == "Harris" else None) or num(r.get("T")) or mag, num(r.get("U"))
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
        # MCRN's spellings are its cross-IDs, which the description lists.
        seen = {re.sub(r"\s", "", n).lower() for n in ([des] if name == "MCRN" else names)}
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
        # The catalogue's axis ratio on the size kept, when that is still its
        # own object (within 2x of its major axis, as rcw2yaml).
        if minor and major and size and size / 2 <= major <= size * 2 and (m := round(minor / major * size, 1)) < size:
            print(f"  minor: {m}")
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
