#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Calibrate internal/scoring's vContinuum against Finkbeiner's Hα map.

    scripts/calibrate_lines.py [--cache DIR]

Standard library only. The sample is every emission-line target of at least
30' (the map's beam is 6') with a surface brightness as scoring derives it,
which TestCalibrationSample prints under CALIBRATE_LINES=1. For each it
fetches NASA SkyView's "H-Alpha Comp" (Finkbeiner 2003, ApJS 146, 407, in
Rayleighs) over a box twice the object's size, cached under --cache, and
takes the mean over the object's disc less the median of a ring from 1.25
to 2 radii (the Milky Way's diffuse Hα). Its lines derived from V are
    lines = V / fV,   fV = sum of share x Bessell V at the line (lineV),
whose Hα share is what the map measures. vContinuum is the median over the
sample of the derived Hα against the measured, in magnitudes: the part of
a nebula's catalogued V light that is stars and continuum. Exact duplicates
(one nebula under two names, same size and surface brightness) count once;
objects SkyView has no data for are listed and skipped. Prints the table,
the median and interquartile range; bake vContinuum into score.go by hand.
"""

import argparse
import math
import os
import statistics
import struct
import subprocess
import sys
import time
import urllib.request

# Bessell (1990) V, 10 nm steps from 470 nm (SVO Generic/Bessell.V).
BESSELL_V = [0, 0.030, 0.163, 0.458, 0.780, 0.967, 1.000, 0.973, 0.898, 0.792, 0.684, 0.574,
             0.461, 0.359, 0.270, 0.197, 0.135, 0.081, 0.045, 0.025, 0.017, 0.013, 0.009, 0]


def bessell(nm):
    x = (nm - 470) / 10
    if x <= 0 or x >= len(BESSELL_V) - 1:
        return 0.0
    i = int(x)
    return BESSELL_V[i] + (x - i) * (BESSELL_V[i + 1] - BESSELL_V[i])


# score.go's lineV: Hα, [O III] (495.9 at a third of 500.7), [S II] doublet, Hβ.
LINE_V = (bessell(656.3), bessell(500.7) + bessell(495.9) / 3,
          (bessell(671.6) + bessell(673.1)) / 2, bessell(486.1))
PHOTONS_V0 = 1005 * 10 * 88                     # V = 0, photons s^-1 cm^-2 over the band
RAYLEIGH = 1e6 / (4 * math.pi) / 4.2545e10      # photons s^-1 cm^-2 arcsec^-2
PIXELS = 48


def sample():
    """The rows TestCalibrationSample prints."""
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
    out = subprocess.run(["go", "test", "./internal/scoring", "-run", "^TestCalibrationSample$", "-v", "-count", "1"],
                         cwd=root, env=dict(os.environ, CALIBRATE_LINES="1"), capture_output=True, text=True, check=True).stdout
    for line in out.splitlines():
        if line.startswith("ROW|"):
            _, lst, name, typ, ra, dec, size, sb, *shares = line.split("|")
            yield lst, name, typ, float(ra), float(dec), float(size), float(sb), [float(s) for s in shares]


def image(ra, dec, side, cache):
    """SkyView's Hα map, PIXELS square over side degrees; None without data."""
    path = os.path.join(cache, f"{ra:.4f}_{dec:.4f}_{side:.4f}_{PIXELS}.fits")
    if not os.path.exists(path):
        url = ("https://skyview.gsfc.nasa.gov/current/cgi/runquery.pl?Survey=H-Alpha+Comp"
               f"&Position={ra},{dec}&Coordinates=J2000&Size={side}&Pixels={PIXELS}&Return=FITS")
        with urllib.request.urlopen(url, timeout=120) as r:
            data = r.read()
        with open(path + ".tmp", "wb") as f:
            f.write(data)
        os.replace(path + ".tmp", path)
        time.sleep(1)
    data = open(path, "rb").read()
    if data[:6] != b"SIMPLE" or b"No data found" in data[:2880]:
        return None
    i = 0
    while True:  # 2880-byte header blocks up to END
        block = data[i:i + 2880]
        i += 2880
        if any(block[k:k + 3] == b"END" and not block[k + 3:k + 80].strip() for k in range(0, 2880, 80)):
            break
    return struct.unpack(f">{PIXELS * PIXELS}f", data[i:i + 4 * PIXELS * PIXELS])


def measure(ra, dec, size, cache):
    """Mean Hα over the disc less the ring's median, R; None without data."""
    px = image(ra, dec, 2 * size / 60, cache)
    if px is None:
        return None
    disc, ring = [], []
    c = (PIXELS - 1) / 2
    for y in range(PIXELS):
        for x in range(PIXELS):
            r = math.hypot(x - c, y - c) / (PIXELS / 2) * 2  # in object radii
            v = px[y * PIXELS + x]
            if math.isfinite(v):
                if r <= 1:
                    disc.append(v)
                elif 1.25 <= r <= 2:
                    ring.append(v)
    return statistics.mean(disc) - statistics.median(ring)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/calibrate_lines"))
    args = ap.parse_args()
    os.makedirs(args.cache, exist_ok=True)
    seen, offsets = set(), []
    print(f"{'list':9} {'name':13} {'type':20} {'size':>5} {'SB':>5} {'fV':>5} {'Ha R':>6} {'V/fV':>6} {'mag':>6}")
    for lst, name, typ, ra, dec, size, sb, shares in sample():
        if (round(size), sb) in seen:
            continue
        seen.add((round(size), sb))
        meas = measure(ra, dec, size, args.cache)
        if meas is None:
            print(f"{lst:9} {name:13} no SkyView data", file=sys.stderr)
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
