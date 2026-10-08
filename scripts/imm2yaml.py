#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Convert Gary Imm's Deep Sky Compendium (XLSX) to an uptonight target list.

    scripts/imm2yaml.py IMM_Compendium_2026.xlsx > internal/catalog/targets/GaryImmFull.yaml

Standard library only: the XLSX is read as zipped XML (sheet "Main",
data from row 10; columns A name, D type, E subtype, F class, G size in
arcmin, J rating, T integrated magnitude, U surface brightness, L raw RA h/m/s, M RA deg, N raw Dec d/m/s, O Dec deg,
P constellation, Q nickname, R alternative IDs). L and N are used to
cross-check and correct M and O.
"""

import html
import json
import math
import re
import sys
import zipfile

TYPES = {
    ("Gal", "Cluster"): "Galaxy Cluster",
    ("Gal", "Group"): "Galaxy Group",
    ("Gal", "Trio"): "Galaxy Group",
    ("Gal", "Chain"): "Galaxy Group",
    ("Gal", "Pair"): "Galaxy Duo",
    ("Neb", "Em"): "Emission Nebula",
    ("Neb", "PN"): "Planetary Nebula",
    ("Neb", "PPN"): "Preplanetary Nebula",
    ("Neb", "SNR"): "Supernova Remnant",
    ("Neb", "Refl"): "Reflection Nebula",
    ("Neb", "Dark"): "Dark Nebula",
    ("Neb", "Mol Cld"): "Molecular Cloud",
    ("Stars", "GC"): "Globular Cluster",
    ("Stars", "OC"): "Open Cluster",
    ("Stars", "HH"): "Herbig-Haro Object",
    ("Stars", "Nova"): "Nova",
    ("Stars", "YSO"): "Young Stellar Object",
    ("Stars", "Star Cld"): "Star Cloud",
}
# Dec typed as DDMM without seconds (e.g. 3633 for +36 33), which the sheet
# reads as 0 36 33: these lie in Ursa Major, not on the celestial equator.
DDMM_TYPOS = {"NGC 3813", "NGC 3998", "NGC 5473"}
CELL = re.compile(r'<c r="([A-Z]+)\d+"((?:(?!/>)[^>])*)(?:/>|>(.*?)</c>)', re.S)


def sheet_path(z, name):
    """Resolve a sheet name to its XML part via workbook.xml and its rels."""
    wb = z.read("xl/workbook.xml").decode()
    rels = z.read("xl/_rels/workbook.xml.rels").decode()
    for tag in re.findall(r"<sheet\b[^>]*>", wb):
        if re.search(r'\bname="%s"' % re.escape(name), tag):
            rid = re.search(r'r:id="([^"]+)"', tag).group(1)
            for rel in re.findall(r"<Relationship\b[^>]*>", rels):
                if re.search(r'\bId="%s"' % re.escape(rid), rel):
                    target = re.search(r'Target="([^"]+)"', rel).group(1)
                    return target.lstrip("/") if target.startswith("/xl/") else "xl/" + target
    sys.exit(f"error: no sheet named {name!r} in the workbook")


def rows(path):
    z = zipfile.ZipFile(path)
    strings = [html.unescape(re.sub(r"<[^>]+>", "", m))
               for m in re.findall(r"<si>(.*?)</si>", z.read("xl/sharedStrings.xml").decode(), re.S)]
    sheet = z.read(sheet_path(z, "Main")).decode()
    for num, body in re.findall(r'<row r="(\d+)"[^>]*>(.*?)</row>', sheet, re.S):
        if int(num) < 10:
            continue
        r = {}
        for col, attrs, inner in CELL.findall(body):
            v = re.search(r"<v>(.*?)</v>", inner or "")
            if v:
                r[col] = strings[int(v.group(1))] if re.search(r'\bt="s"', attrs) else html.unescape(v.group(1))
        if not r.get("A", "").strip():
            continue
        if not r.get("M") or not r.get("O"):
            print(f"warning: row {num} ({r['A'].strip()}): no RA/Dec, skipped", file=sys.stderr)
            continue
        yield r


def sexagesimal(deg, hours=False):
    v = deg / 15 if hours else deg
    sign = "-" if v < 0 else ("" if hours else "+")
    s = round(abs(v) * 36000)  # tenths of a second
    if hours:
        s %= 24 * 36000  # 23 59 59.96 rounds to 24h, which the loader rejects: wrap to 0h
    d, rem = divmod(s, 36000)
    m, t = divmod(rem, 600)
    return f"{sign}{d:02d} {m:02d} {t / 10:04.1f}"


def raw_deg(v, hours=False):
    """Decode the sheet's raw [+-]DDMMSS / HHMMSS cell into degrees."""
    n = int(float(v))
    sign = -1 if n < 0 else 1
    n = abs(n)
    d, m, s = n // 10000, n // 100 % 100, n % 100
    return sign * (d + m / 60 + s / 3600) * (15 if hours else 1)


def position(r):
    """RA and Dec in degrees: the decimal columns, cross-checked against the
    raw h/m/s and d/m/s cells (the raw cell wins when they disagree)."""
    name = r["A"].strip()
    ra, dec = float(r["M"]), float(r["O"])
    if name in DDMM_TYPOS:
        n = abs(int(float(r["N"])))
        return ra, (n // 100 + n % 100 / 60) * (-1 if float(r["N"]) < 0 else 1)
    if r.get("L") and abs(raw_deg(r["L"], hours=True) - ra) * math.cos(math.radians(dec)) > 1 / 60:
        print(f"warning: {name}: RA column {ra:.4f} != raw {r['L']}, using raw", file=sys.stderr)
        ra = raw_deg(r["L"], hours=True)
    if r.get("N") and abs(raw_deg(r["N"]) - dec) > 1 / 60:
        print(f"warning: {name}: Dec column {dec:.4f} != raw {r['N']}, using raw", file=sys.stderr)
        dec = raw_deg(r["N"])
    return ra, dec


# Cross-identification columns -> designation as the other lists spell it.
# AU/AV hold Kohoutek/Minkowski "1-07" as 107; AP (Abell galaxy clusters)
# would collide with AS (Abell PNe) and is left out.
ALIASES = {"AI": "NGC {}", "AJ": "IC {}", "AL": "M {}", "AM": "Caldwell {}", "AN": "Arp {}", "AO": "HCG {}",
           "AQ": "UGC {}", "AR": "PGC {}", "AS": "Abell {}", "AW": "Barnard {}", "AX": "Gum {}", "AY": "LBN {}",
           "AZ": "LDN {}", "BA": "RCW {}", "BB": "Sh 2-{}", "BD": "vdB {}"}  # Sh 2-N as HASH spells it


def designation(s):
    """Normalize a designation as the names are written: single spaces,
    leading zeros stripped ("Abell 01" -> "Abell 1", "Minkowski 1-07" -> "Minkowski 1-7")."""
    return re.sub(r"(?<=[ \-])0+(?=\d)", "", " ".join(s.split()))


def aliases(path):
    """Every designation of every Compendium row (its name, the alternative
    IDs column and the cross-identification columns) -> the rows carrying it,
    normalized with designation(). Non-numeric cross-ID cells are skipped.
    For a row's magnitude read column T (num() it), for the type columns D/E."""
    out = {}
    for r in rows(path):
        ids = [r["A"]] + (r.get("R") or "").split(",")
        ids += [fmt.format(int(num(r.get(c)))) for c, fmt in ALIASES.items() if num(r.get(c)) is not None]
        for c, fmt in (("AU", "K {}-{}"), ("AV", "M {}-{}")):  # 107 means 1-07
            if num(r.get(c)) is not None:
                v = str(int(num(r[c])))
                ids.append(fmt.format(v[:-2], int(v[-2:])))
        for i in ids:
            if i.strip():
                out.setdefault(designation(i), []).append(r)
    return out


def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def kind(r):
    """A row's type as the lists spell it (columns D type, E subtype, F class)."""
    typ, sub, cls = r.get("D", "").strip(), r.get("E", "").strip().replace("CLd", "Cld"), r.get("F") or ""
    k = TYPES.get((typ, sub), {"Gal": "Galaxy", "Stars": "Star"}.get(typ, "Nebula"))
    return "Wolf-Rayet Nebula" if k == "Emission Nebula" and cls.startswith("WR") else k


def main():
    q = lambda s: json.dumps(s, ensure_ascii=False)  # JSON strings are valid YAML
    print("# Generated by scripts/imm2yaml.py from Gary Imm's Deep Sky Compendium")
    print("# (2026, 6th edition) - data (c) Gary Imm.")
    print("# rating is Gary Imm's 0 (low) - 5 (high) imaging rating; uptonight ignores it.")
    for r in rows(sys.argv[1]):
        name = designation(r["A"])
        desc = " ".join((r.get("Q") or "").split()) or " ".join((r.get("R") or "").split())
        size, rating, mag, surfbr = num(r.get("G")), num(r.get("J")), num(r.get("T")), num(r.get("U"))
        ra, dec = position(r)
        print(f"- constellation: {q((r.get('P') or '').strip())}")
        print(f"  dec: {q(sexagesimal(dec))}")
        print(f"  description: {q(desc)}")
        print(f"  mag: {mag if mag is not None else -9999}")
        print(f"  name: {q(name)}")
        print(f"  ra: {q(sexagesimal(ra, hours=True))}")
        print(f"  rating: {int(rating) if rating is not None else 0}")
        print(f"  size: {size if size is not None else -9999}")
        if surfbr is not None:
            print(f"  surfbr: {round(surfbr, 2)}")
        print(f"  type: {q(kind(r))}")


if __name__ == "__main__":
    main()
