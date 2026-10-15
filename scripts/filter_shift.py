#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Fit internal/scoring's filterN against Chroma's narrowband f/# curves.

    scripts/filter_shift.py [--cache DIR]

Standard library only. Chroma publishes measured transmission curves of its
3, 5 and 8 nm Hα, Hβ, [O III] and [S II] filters collimated and at f/2,
f/2.5, f/3, f/4 and f/5 (cached under --cache). A filter tilted by θ shifts
its band to λ·√(1 − sin²θ/n²); in a cone of focal ratio N each ray's angle
is tan θ = r/2N across the pupil, uniform in r². scoring's beam.through
models the filter as a top-hat of its nominal width centred on its line;
this prints, per effective index n, the rms and mean of that model's
transmission at the line less Chroma's measured one (relative to the
collimated curve's), over every curve whose collimated transmission at the
line is at least 0.5. Chroma's [N II], He II and "f/3 optimized" filters are
left out: they are centred off their line, pre-shifted for a fast beam.
Bake the best n into score.go's filterN by hand.
"""

import argparse
import math
import os
import re
import urllib.request

PAGE = "https://www.chroma.com/resources/technical-library/narrowband-astronomy-filter-f-number-data/"
LINES = {"ha": 656.3, "hb": 486.1, "oiii": 500.7, "sii": 672.4}


def get(url, path):
    if not os.path.exists(path):
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=60) as r, open(path + ".tmp", "wb") as f:
            f.write(r.read())
        os.replace(path + ".tmp", path)
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def curve(text):
    """(nm, transmission 0-1) pairs; the files mix commas and tabs, % and fractions."""
    pts = []
    for line in text.splitlines():
        f = re.split(r"[,\t]", line.strip())
        try:
            pts.append((float(f[0]), float(f[1])))
        except (ValueError, IndexError):
            pass
    scale = 100 if max(t for _, t in pts) > 1.5 else 1
    return [(nm, t / scale) for nm, t in pts]


def at(pts, nm):
    for (a, ta), (b, tb) in zip(pts, pts[1:]):
        if a <= nm <= b:
            return ta + (tb - ta) * (nm - a) / (b - a)
    return 0.0


def hat(nm, width, x):
    return 1.0 if nm - width / 2 <= x <= nm + width / 2 else 0.0


def cone(nm, width, ratio, n, steps=400):
    """A top-hat band's transmission at its own line in an unobstructed cone."""
    t = 0.0
    for i in range(steps):
        th = math.atan(math.sqrt((i + 0.5) / steps) / (2 * ratio))
        t += hat(nm, width, nm / math.sqrt(1 - math.sin(th) ** 2 / n ** 2))
    return t / steps


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/filter_shift"))
    args = ap.parse_args()
    os.makedirs(args.cache, exist_ok=True)
    page = get(PAGE, os.path.join(args.cache, "page.html"))
    paths = sorted(set(re.findall(r'href="(/assets/documents/astronomy/[^"]+\.txt)"', page)))
    cases = []
    for path in paths:
        m = re.search(r"\d+-(ha|hb|oiii|sii)-(\d)nm-(?:f-?)?([\d.]+)\.txt$", path)
        if not m or "optimized" in path:
            continue
        line, width, ratio = m.group(1), int(m.group(2)), float(m.group(3))
        coll = re.sub(r"(?:f-?)?[\d.]+\.txt$", "collimated.txt", path)
        meas = curve(get("https://www.chroma.com" + path, os.path.join(args.cache, os.path.basename(path))))
        col = curve(get("https://www.chroma.com" + coll, os.path.join(args.cache, os.path.basename(coll))))
        nm = LINES[line]
        if at(col, nm) >= 0.5:
            cases.append((line, width, ratio, at(meas, nm) / at(col, nm)))
    print(f"{len(cases)} curves")
    for n in (1.8, 1.9, 2.0, 2.05, 2.1, 2.2):
        err = [cone(LINES[line], width, ratio, n) - t for line, width, ratio, t in cases]
        rms = math.sqrt(sum(e * e for e in err) / len(err))
        print(f"n = {n:.2f}: rms {rms:.3f}, mean {sum(err) / len(err):+.3f}")


if __name__ == "__main__":
    main()
