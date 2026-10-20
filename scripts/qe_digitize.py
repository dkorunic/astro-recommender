#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Digitize a colour sensor's QE chart into internal/config's Response.

    scripts/qe_digitize.py CHART.png --x FIRST LAST [--xpx PX0 PX1] [--box X0 Y0 X1 Y1] [--dark] [--mono COLOUR [--black-max V] [--sat-min S]]

Needs Pillow and NumPy. CHART is a vendor's colour QE chart (red, green and
blue curves against wavelength), rendered from its PDF page, e.g. with
`gs -sDEVICE=png16m -r400`. The plot's gridlines are found as the grey rows
and columns spanning most of --box (the plot area in pixels; default the
bounding box of the coloured curves, widened); --x gives the wavelengths of the first and last vertical
gridline, the horizontal ones need no labels, since the values are taken
relative to the chart's highest point, the peak QE that the preset gives
separately (its highest point over the chart's whole range, which for a
sensor peaking in the near infrared lies past 700 nm). Each curve's pixels are told apart by hue (red, green, blue,
saturated), so the line colours need not be pure. Prints, relative to the
peak, each channel at every catalog.Line ([O III] with 495.9 nm at a third
of 500.7's flux, [S II] the doublet's mean) and each channel's mean over
400-700 nm, as the Go literal; a line where a curve is hidden under another
prints the curve's nearest visible value with a warning. --mono reads a
mono sensor's single curve instead (black, or one of the hues: red, green,
blue), printed as MonoResponse's fields; a black curve is told from the
plot's frame by leaving out the rows and columns that are mostly dark, a
coloured one by its hue at any brightness (neutral labels and gridlines
left out). A chart with tick marks instead of vertical gridlines takes
--xpx, the image columns of --x's two wavelengths.
"""

import argparse
import colorsys
import sys

import numpy as np
from PIL import Image

# catalog.Lines in order, as wavelengths (nm) and weights.
LINES = [("Ha", [(656.3, 1)]), ("OIII", [(500.7, 3), (495.9, 1)]), ("SII", [(671.6, 1), (673.1, 1)]),
         ("Hb", [(486.1, 1)]), ("NII6548", [(654.8, 1)]), ("NII6583", [(658.3, 1)])]
HUES = {"red": (330, 25), "green": (85, 160), "blue": (200, 260)}  # degrees, wrapping for red


def gridlines(grey, axis, span):
    """Centres of runs of rows (axis=1) or columns (axis=0) mostly grey."""
    counts = grey.sum(axis=axis)
    hits = [i for i, c in enumerate(counts) if c > 0.5 * span]
    groups = []
    for i in hits:
        if groups and i - groups[-1][-1] <= 2:
            groups[-1].append(i)
        else:
            groups.append([i])
    return [sum(g) / len(g) for g in groups]


def regular(lines):
    """The evenly spaced grid from the first line to the last at the median
    spacing: a gridline hidden under the curves (IMX462's 800 nm) or a stray
    one would otherwise skew the scale."""
    step = float(np.median(np.diff(lines)))
    n = int(round((lines[-1] - lines[0]) / step))
    return list(np.linspace(lines[0], lines[-1], n + 1))


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("chart")
    ap.add_argument("--x", nargs=2, type=float, required=True, metavar=("FIRST", "LAST"))
    ap.add_argument("--box", nargs=4, type=int, metavar=("X0", "Y0", "X1", "Y1"))
    ap.add_argument("--dark", action="store_true", help="grey gridlines on a black background (Player One)")
    ap.add_argument("--light-grid", action="store_true", help="white gridlines on a tinted background (Sony's CCD charts); give --box")
    ap.add_argument("--mono", choices=["black", *HUES], help="a mono chart's single curve, by its colour")
    ap.add_argument("--grid-max", type=float, default=0.86, help="gridlines' brightness limit; raise it for pale ones")
    ap.add_argument("--sat-min", type=float, default=0.3, help="--mono hue's saturation limit; lower it for a pale curve (QHY's pink)")
    ap.add_argument("--black-max", type=float, default=0.45, help="--mono black's brightness limit; lower it for dark grey gridlines")
    ap.add_argument("--xpx", nargs=2, type=float, metavar=("PX0", "PX1"), help="image columns of --x's wavelengths, for a chart without vertical gridlines")
    args = ap.parse_args()
    img = np.asarray(Image.open(args.chart).convert("RGB")).astype(float) / 255
    if args.box:
        x0, y0, x1, y1 = args.box
    elif args.mono:
        # No coloured curve to bound the plot: the whole chart, whose
        # gridlines still span most of it.
        x0, y0, x1, y1 = 0, 0, img.shape[1], img.shape[0]
    else:
        # The plot area: the saturated (curve) pixels' bounding box, widened
        # to take in the gridlines at the plot's edges.
        mxa, mna = img.max(axis=2), img.min(axis=2)
        ys, xs = np.where((mxa - mna > 0.4) & (mxa > 0.35))
        if not len(xs):
            sys.exit("no coloured curves found; give --box")
        pad_x, pad_y = int(0.06 * (xs.max() - xs.min())), int(0.15 * (ys.max() - ys.min()))
        x0, x1 = max(xs.min() - pad_x, 0), min(xs.max() + pad_x, img.shape[1])
        y0, y1 = max(ys.min() - pad_y, 0), min(ys.max() + pad_y, img.shape[0])
        print(f"# box {x0} {y0} {x1} {y1}", file=sys.stderr)
    plot = img[y0:y1, x0:x1]
    r, g, b = plot[..., 0], plot[..., 1], plot[..., 2]
    mx, mn = plot.max(axis=2), plot.min(axis=2)
    grey = (mx - mn < 0.06) & (mx < args.grid_max)  # grey gridlines and a black axis
    if args.dark:
        grey &= mx > 0.2  # lighter than the background
    if args.light_grid:
        grey = (mx - mn < 0.06) & (mx > 0.93)  # white on the tint
    vx = gridlines(grey, 0, y1 - y0)
    hy = gridlines(grey, 1, x1 - x0)
    if len(vx) < 3 <= len(hy) and not args.xpx:
        # A box much taller than the plot (the whole chart, for --mono
        # black): narrow it to the horizontal gridlines' extent and look again.
        cols = np.where(grey[int(min(hy)) - 1:int(min(hy)) + 2].any(axis=0))[0]
        x0, x1 = x0 + cols.min(), x0 + cols.max() + 1
        y0, y1 = y0 + int(min(hy)) - 2, y0 + int(max(hy)) + 3
        plot = img[y0:y1, x0:x1]
        r, g, b = plot[..., 0], plot[..., 1], plot[..., 2]
        mx, mn = plot.max(axis=2), plot.min(axis=2)
        grey = (mx - mn < 0.06) & (mx < args.grid_max)
        if args.dark:
            grey &= mx > 0.2
        vx = gridlines(grey, 0, y1 - y0)
        hy = gridlines(grey, 1, x1 - x0)
        print(f"# box narrowed to {x0} {y0} {x1} {y1}", file=sys.stderr)
    if args.xpx:
        vx = [args.xpx[0] - x0, args.xpx[1] - x0]
    if len(vx) < 2 or len(vx) < 3 and not args.xpx or len(hy) < 3:
        sys.exit(f"found {len(vx)} vertical and {len(hy)} horizontal gridlines; give --box around the plot")
    vx, hy = (vx if args.xpx else regular(vx)), regular(hy)
    print(f"# {len(vx)} vertical gridlines {args.x[0]:g}-{args.x[1]:g} nm, {len(hy)} horizontal", file=sys.stderr)
    nm_at = lambda nm: np.interp(nm, np.linspace(args.x[0], args.x[1], len(vx)), vx)
    # Hue per pixel; saturated, not too dark.
    hsv = np.vectorize(colorsys.rgb_to_hsv)(r, g, b)
    hue, sat, val = hsv[0] * 360, hsv[1], hsv[2]
    masks = {}
    for name, (lo, hi) in HUES.items():
        inside = (hue >= lo) | (hue <= hi) if lo > hi else (hue >= lo) & (hue <= hi)
        masks[name] = inside & (sat > 0.45) & (val > 0.35)
    if args.mono == "black":
        dark = mx < args.black_max  # black or dark grey, below the gridlines
        # The frame and axes: rows and columns mostly dark.
        dark[dark.mean(axis=1) > 0.5, :] = False
        dark[:, dark.mean(axis=0) > 0.5] = False
        masks = {"black": dark}
    elif args.mono:
        lo, hi = HUES[args.mono]
        inside = (hue >= lo) | (hue <= hi) if lo > hi else (hue >= lo) & (hue <= hi)
        masks = {args.mono: inside & (sat > args.sat_min) & (val > 0.08)}
    names = list(masks)
    # Height above the lowest gridline, in the chart's units of gridline steps.
    base, step = max(hy), (max(hy) - min(hy)) / (len(hy) - 1)
    top, bottom = max(int(min(hy)) - 3, 0), int(max(hy)) + 4

    def height(name, nm, warn=False):
        x = int(round(nm_at(nm)))
        for dx in range(0, 12):  # the nearest column where the curve is visible
            for c in (x - dx, x + dx):
                # Only within the plot: the legend's swatches above it are
                # curve-coloured too.
                ys = np.where(masks[name][top:bottom, max(c - 1, 0):c + 2].any(axis=1))[0] + top
                if len(ys) and name == "black" and ys.max() - ys.min() > 0.1 * (bottom - top):
                    continue  # a dark vertical gridline, not the thin curve
                if len(ys):
                    if dx and warn:
                        print(f"# warning: {name} hidden at {nm} nm, taken {dx} px away", file=sys.stderr)
                    return (base - ys.mean()) / step
        return 0.0

    curves = {n: np.array([height(n, nm) for nm in range(400, 701)]) for n in names}
    # The peak over the whole chart: a sensor whose response peaks in the
    # near infrared (IMX462, IMX464) has its vendor peak QE there.
    span = range(int(np.ceil(args.x[0])), int(args.x[1]) + 1, 2)
    peak = max(max(height(n, nm) for nm in span) for n in names)
    rel = lambda n, nm: height(n, nm, warn=True) / peak
    print(f"# peak at {peak:.2f} gridline steps", file=sys.stderr)
    if args.mono:
        n = names[0]
        vals = [sum(rel(n, nm) * k for nm, k in parts) / sum(k for _, k in parts) for _, parts in LINES]
        print("Lines: [catalog.NumLines]float64{" + ", ".join(f"catalog.{line}: {v:.3f}" for (line, _), v in zip(LINES, vals)) + "},")
        print(f"Broad: {curves[n].mean() / peak:.3f},")
        return
    print("Lines: [catalog.NumLines][3]float64{")
    for line, parts in LINES:
        w = sum(k for _, k in parts)
        vals = [sum(rel(n, nm) * k for nm, k in parts) / w for n in HUES]
        print(f"\tcatalog.{line}: {{{vals[0]:.3f}, {vals[1]:.3f}, {vals[2]:.3f}}},")
    print("},")
    broad = [curves[n].mean() / peak for n in HUES]
    print(f"Broad: [3]float64{{{broad[0]:.3f}, {broad[1]:.3f}, {broad[2]:.3f}}},")


if __name__ == "__main__":
    main()
