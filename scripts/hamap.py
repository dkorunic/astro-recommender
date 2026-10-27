#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Add Hα fluxes from Finkbeiner's full-sky map to the embedded lists' large
emission nebulae.

    scripts/hamap.py [--cache DIR]

Standard library only. Finkbeiner (2003, ApJS 146, 407) combined WHAM,
VTSS and SHASSA into one Hα map at 6' resolution, in Rayleighs; LAMBDA
serves it as a NESTED HEALPix FITS table in galactic coordinates
(Nside 512, 12.6 MB, cached under --cache). TestHaMapSample (HAMAP=1)
lists the nebulae it applies to: emission-line, at least 30', with no
surface brightness of their own (the Heart, the California, the Rosette
and the Veil carry a cluster's or star's magnitude, LBN none). For each,
measure() takes the mean over the object's disc less the median of a ring
from 1.25 to 2 radii (the Milky Way's diffuse Hα), the geometry
scripts/calibrate_lines.py fitted vContinuum with, so a map flux and a
catalogued surface brightness rank on one footing. Where the ring's median
is RING_MAX (0.8) of the disc's mean or more, the ring lies on the same
complex (IC 1795 on W3 and the Heart, the Pelican beside the North America)
and the difference would read far too faint: the flux stays unknown, as
in calibrate_lines. The mean intensity over the ellipse of its
axes is its integrated flux, log10 erg cm⁻² s⁻¹, written as `haflux:`
before the type line, and catalog.Target.FluxSB turns it back into the
lines' surface brightness. A nebula's map value is the narrowband surveys':
WHAM, which resolves [N II] from Hα, sets only the zero point, on 1°
scales (Finkbeiner 2003, §3.2). SHASSA's 32 Å filter passes both [N II]
lines (Gaustad et al. 2001), VTSS's 17 Å one part of 654.8 nm, so south of
Dec +15° a flux can hold the H II regions' typical 0.36 of Hα in [N II],
up to 0.33 mag too bright; vContinuum was fitted on the same map.

A planetary's haflux is Frew's (scripts/haflux.py); every other entry's is
this script's, removed and rewritten on each run, so it is idempotent. Run
after haflux.py, then `task lists`.
"""

import argparse
import math
import os
import statistics
import struct
import subprocess
import sys
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from haflux import put  # noqa: E402
from mwsc2yaml import TARGETS  # noqa: E402

URL = "https://lambda.gsfc.nasa.gov/data/foregrounds/halpha/lambda_halpha_fwhm06_0512.fits"
# J2000 equatorial to galactic unit vectors (the IAU rotation, as SOFA's Icrs2g).
EQ2GAL = ((-0.0548755604, -0.8734370902, -0.4838350155),
          (0.4941094279, -0.4448296300, 0.7469822445),
          (-0.8676661490, -0.1980763734, 0.4559837762))
PIXELS = 48  # the measurement grid's side, as calibrate_lines' SkyView cutouts were
HC_ERG_NM = 1.98644586e-9  # catalog.hcErgNM
HA_NM = 656.3  # catalog.LineNM[Ha]
ARCMIN = math.pi / 180 / 60
# A ring this bright against the disc sits on the same complex (IC 1795's on
# W3 and the Heart: 89 of 103 R), and the difference is mostly its noise.
RING_MAX = 0.8


class Map:
    """Finkbeiner's map, R, looked up by J2000 RA and Dec in degrees."""

    def __init__(self, cache):
        path = os.path.join(cache, os.path.basename(URL))
        if not os.path.exists(path):
            with urllib.request.urlopen(URL, timeout=300) as r:
                data = r.read()
            with open(path + ".tmp", "wb") as f:
                f.write(data)
            os.replace(path + ".tmp", path)
        data = open(path, "rb").read()
        i, cards = 0, {}
        for _ in range(2):  # the primary header, then the table's
            while True:
                block = data[i:i + 2880]
                i += 2880
                for k in range(0, 2880, 80):
                    card = block[k:k + 80].decode("ascii", "replace")
                    if "=" in card[:10]:
                        cards[card[:8].strip()] = card[10:].split("/")[0].strip().strip("'").strip()
                if any(block[k:k + 3] == b"END" and not block[k + 3:k + 80].strip() for k in range(0, 2880, 80)):
                    break
        self.nside = int(cards["NSIDE"])
        if cards["ORDERING"] != "NESTED" or cards["TFORM1"] != "E" or int(cards["NAXIS2"]) != 12 * self.nside ** 2:
            sys.exit(f"error: {path}: not the NESTED float HEALPix map expected")
        self.values = struct.unpack(f">{12 * self.nside ** 2}f", data[i:i + 48 * self.nside ** 2])

    def pixel(self, ra, dec):
        """The NESTED pixel holding J2000 ra, dec (HEALPix's ang2pix_nest)."""
        n, r, d = self.nside, math.radians(ra), math.radians(dec)
        eq = (math.cos(d) * math.cos(r), math.cos(d) * math.sin(r), math.sin(d))
        x, y, z = (sum(m * c for m, c in zip(row, eq)) for row in EQ2GAL)
        za, tt = abs(z), (math.atan2(y, x) % (2 * math.pi)) * 2 / math.pi
        if za <= 2 / 3:  # the equatorial zone
            t1, t2 = n * (0.5 + tt), n * z * 0.75
            jp, jm = int(t1 - t2), int(t1 + t2)
            ifp, ifm = jp // n, jm // n
            face = ifp | 4 if ifp == ifm else ifp if ifp < ifm else ifm + 8
            ix, iy = jm % n, n - jp % n - 1
        else:  # the polar caps
            ntt = min(3, int(tt))
            tp, tmp = tt - ntt, n * math.sqrt(3 * (1 - za))
            jp, jm = min(int(tp * tmp), n - 1), min(int((1 - tp) * tmp), n - 1)
            face, ix, iy = (ntt, n - jm - 1, n - jp - 1) if z >= 0 else (ntt + 8, jp, jm)
        spread = lambda v: sum(((v >> b) & 1) << (2 * b) for b in range(n.bit_length()))  # noqa: E731
        return face * n * n + (spread(ix) | spread(iy) << 1)

    def __call__(self, ra, dec):
        return self.values[self.pixel(ra, dec)]


def measure(hmap, ra, dec, size):
    """Mean Hα over the disc of the major axis less the ring's median, R:
    calibrate_lines' SkyView geometry, a PIXELS-square gnomonic grid over
    twice the size; None where the map has no finite value, the disc's mean
    is not positive (noise: no flux to take a log of) or the ring's median
    is RING_MAX of the disc's mean or more."""
    side, c = 2 * size / 60, (PIXELS - 1) / 2
    d0, a0 = math.radians(dec), math.radians(ra)
    disc, ring = [], []
    for y in range(PIXELS):
        for x in range(PIXELS):
            xi, eta = math.radians((x - c) * side / PIXELS), math.radians((y - c) * side / PIXELS)
            den = math.cos(d0) - eta * math.sin(d0)
            v = hmap(math.degrees(a0 + math.atan2(xi, den)) % 360,
                     math.degrees(math.atan2(math.sin(d0) + eta * math.cos(d0), math.hypot(xi, den))))
            r = math.hypot(x - c, y - c) / (PIXELS / 2) * 2  # in object radii
            if not math.isfinite(v):
                return None
            if r <= 1:
                disc.append(v)
            elif 1.25 <= r <= 2:
                ring.append(v)
    mean, background = statistics.mean(disc), statistics.median(ring)
    if mean <= 0 or background >= RING_MAX * mean:
        return None
    return mean - background


def flux(rayleighs, major, minor):
    """log10 of the integrated Hα flux, erg cm⁻² s⁻¹, of a mean intensity
    over the ellipse of the axes in arcmin (FluxSB's own area)."""
    per_sr = rayleighs * 1e6 / (4 * math.pi) * HC_ERG_NM / HA_NM
    return math.log10(per_sr * math.pi / 4 * major * minor * ARCMIN ** 2)


def sample():
    """(list, name, ra text, dec text, ra, dec, major, minor) per TestHaMapSample row."""
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
    out = subprocess.run(["go", "test", "./internal/catalog", "-run", "^TestHaMapSample$", "-v", "-count", "1"],
                         cwd=root, env=dict(os.environ, HAMAP="1"), capture_output=True, text=True, check=True).stdout
    for line in out.splitlines():
        if line.startswith("ROW|"):
            _, lst, name, ra_s, dec_s, ra, dec, major, minor = line.split("|")
            yield lst, name, ra_s, dec_s, float(ra), float(dec), float(major), float(minor)


def selftest():
    """measure on flat maps, offline: a negative disc (the noise floor) is
    None, never a non-positive difference for flux's log."""
    assert measure(lambda ra, dec: -0.5, 10, 10, 30) is None
    assert measure(lambda ra, dec: float("nan"), 10, 10, 30) is None
    assert measure(lambda ra, dec: 5.0, 10, 10, 30) is None  # the ring equals the disc
    bump = measure(lambda ra, dec: 10.0 if abs(dec - 10) < 0.2 and abs(ra - 10) < 0.2 else 1.0, 10, 10, 30)
    assert bump is not None and bump > 0 and math.isfinite(flux(bump, 30, 30)), bump
    print("selftest ok")


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/hamap"))
    ap.add_argument("--selftest", action="store_true", help="check measure offline and exit")
    args = ap.parse_args()
    if args.selftest:
        return selftest()
    os.makedirs(args.cache, exist_ok=True)
    hmap = Map(args.cache)
    found, cache = {}, {}
    for lst, name, ra_s, dec_s, ra, dec, major, minor in sample():
        k = (ra, dec, major)  # one nebula in several lists is measured once
        if k not in cache:
            cache[k] = measure(hmap, ra, dec, major)
        r = cache[k]
        if r is None:
            print(f"warning: {lst}: {name}: no map value, or its ring is {RING_MAX:.0%} of its disc", file=sys.stderr)
            continue
        found[(lst, name, ra_s, dec_s)] = flux(r, major, minor)
    for fname in sorted(os.listdir(TARGETS)):
        if not fname.endswith(".yaml"):
            continue
        lst, path = fname[:-5], os.path.join(TARGETS, fname)
        text = open(path, encoding="utf-8").read()
        parts, n = text.split("\n- "), 0
        for i, p in enumerate(parts):
            f = {k: v.strip('"') for k, v in (line.strip().lstrip("- ").split(": ", 1) for line in p.splitlines() if ": " in line)}
            if "planetary" in f.get("type", "").lower():
                continue
            logf = found.get((lst, f.get("name"), f.get("ra"), f.get("dec")))
            parts[i] = put(p, logf)
            n += logf is not None
        if "\n- ".join(parts) != text:
            open(path, "w", encoding="utf-8").write("\n- ".join(parts))
        if n:
            print(f"{fname}: {n} nebulae with a map flux", file=sys.stderr)


if __name__ == "__main__":
    main()
