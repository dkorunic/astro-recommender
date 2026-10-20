#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Calibrate internal/scoring's vContinuum against Finkbeiner's Hα map.

    scripts/calibrate_lines.py [--cache DIR]

Standard library only. The sample is every emission-line target of at least
30' (the map's beam is 6') with a surface brightness as scoring derives it,
which TestCalibrationSample prints under CALIBRATE_LINES=1. For each,
hamap.measure takes Finkbeiner's map (2003, ApJS 146, 407, in Rayleighs;
the full-sky HEALPix file, cached under --cache, which NASA SkyView's
"H-Alpha Comp" cutouts this was first fitted with resample within 3%)
over a box twice the object's size: the mean over the object's disc less
the median of a ring from 1.25 to 2 radii (the Milky Way's diffuse Hα),
none where the ring is hamap.RING_MAX of the disc or more.
scripts/hamap.py measures the nebulae without a surface brightness the
same way, so refit after changing either. Its lines derived from V are
    lines = V / fV,   fV = sum of share x Bessell V at the line (lineV),
whose Hα share is what the map measures. vContinuum is the median over the
sample of the derived Hα against the measured, in magnitudes: the part of
a nebula's catalogued V light that is stars and continuum. Exact duplicates
(one nebula under two names, same size and surface brightness) count once;
objects without a measurement are listed and skipped. Prints the table,
the median and interquartile range; bake vContinuum into score.go by hand.
"""

import argparse
import math
import os
import statistics
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from hamap import Map, measure  # noqa: E402

# Bessell (1990) V, 10 nm steps from 470 nm (SVO Generic/Bessell.V).
BESSELL_V = [0, 0.030, 0.163, 0.458, 0.780, 0.967, 1.000, 0.973, 0.898, 0.792, 0.684, 0.574,
             0.461, 0.359, 0.270, 0.197, 0.135, 0.081, 0.045, 0.025, 0.017, 0.013, 0.009, 0]


def bessell(nm):
    x = (nm - 470) / 10
    if x <= 0 or x >= len(BESSELL_V) - 1:
        return 0.0
    i = int(x)
    return BESSELL_V[i] + (x - i) * (BESSELL_V[i + 1] - BESSELL_V[i])


# score.go's lineV: Hα, [O III] 500.7, [S II] doublet, Hβ, [N II] 654.8 and
# 658.3, [O III] 495.9.
LINE_V = (bessell(656.3), bessell(500.7), (bessell(671.6) + bessell(673.1)) / 2, bessell(486.1),
          bessell(654.8), bessell(658.3), bessell(495.9))
PHOTONS_V0 = 1005 * 10 * 88                     # V = 0, photons s^-1 cm^-2 over the band
RAYLEIGH = 1e6 / (4 * math.pi) / 4.2545e10      # photons s^-1 cm^-2 arcsec^-2


def sample():
    """The rows TestCalibrationSample prints."""
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
    out = subprocess.run(["go", "test", "./internal/scoring", "-run", "^TestCalibrationSample$", "-v", "-count", "1"],
                         cwd=root, env=dict(os.environ, CALIBRATE_LINES="1"), capture_output=True, text=True, check=True).stdout
    for line in out.splitlines():
        if line.startswith("ROW|"):
            _, lst, name, typ, ra, dec, size, sb, *shares = line.split("|")
            yield lst, name, typ, float(ra), float(dec), float(size), float(sb), [float(s) for s in shares]


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/hamap"))
    args = ap.parse_args()
    os.makedirs(args.cache, exist_ok=True)
    hmap = Map(args.cache)
    seen, offsets = set(), []
    print(f"{'list':9} {'name':13} {'type':20} {'size':>5} {'SB':>5} {'fV':>5} {'Ha R':>6} {'V/fV':>6} {'mag':>6}")
    for lst, name, typ, ra, dec, size, sb, shares in sample():
        if (round(size), sb) in seen:
            continue
        seen.add((round(size), sb))
        meas = measure(hmap, ra, dec, size)
        if meas is None:
            print(f"{lst:9} {name:13} no map data, or ring-dominated", file=sys.stderr)
            continue
        fv = sum(s * t for s, t in zip(shares, LINE_V))
        derived = shares[0] * PHOTONS_V0 * 10 ** (-0.4 * sb) / fv / RAYLEIGH
        mag = -2.5 * math.log10(meas / derived) if meas > 0 else math.nan
        if meas > 0:
            offsets.append(mag)
        print(f"{lst:9} {name:13} {typ[:20]:20} {size:5.0f} {sb:5.2f} {fv:5.3f} {meas:6.0f} {derived:6.0f} {mag:6.2f}")
    q = statistics.quantiles(offsets, n=4)
    print(f"\nvContinuum = {statistics.median(offsets):.2f} mag over {len(offsets)} nebulae (interquartile {q[0]:.2f}-{q[2]:.2f})")


if __name__ == "__main__":
    main()
