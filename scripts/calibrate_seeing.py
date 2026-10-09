#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Calibrate internal/weather's Seeing against an ESO DIMM archive.

    scripts/calibrate_seeing.py [--site paranal|lasilla] [--from 2024-05] [--to 2026-09]

Standard library only. For each month it fetches Open-Meteo's archived
ECMWF IFS 0.25 forecast (historical-forecast-api, the same 48 series Profile
requests, complete from May 2024) and the site's DIMM seeing (ESO ambient
conditions, public CSV), both cached under --cache, then rebuilds every
hour's levels exactly as weather.levels() does (dewan.py supplies cn2,
tropopause and the FWHM), splits Dewan's integral into the ground layer
(surface to the first level) and the free atmosphere above, and fits
    J_obs = a_g G + a_f F
by least squares, J_obs being the Cn² integral the hour's median DIMM seeing
implies (ε^(5/3) ∝ J, Cuevas et al. 2024 eq. 13-15 collapsed to the two
regimes a global model can tell apart). Fits on even months, evaluates on
odd ones, then fits on all. Prints the factors and seeing statistics raw,
one-factor and two-factor; bake the result into weather.go by hand.
"""

import argparse
import calendar
import csv
import datetime as dt
import json
import math
import os
import statistics
import sys
import time
import urllib.parse
import urllib.request

from dewan import PRESSURES, cn2, tropopause

SITES = {  # lat, lon (rounded to 2 decimals like the Go code), ESO form
    "paranal": (-24.63, -70.40, "dimm_paranal"),
    "lasilla": (-29.26, -70.73, "ambient_lasilla"),
}
SURFACE = ["surface_pressure", "temperature_2m", "wind_speed_10m", "wind_direction_10m"]
LEVEL = ["geopotential_height_{}hPa", "temperature_{}hPa", "wind_speed_{}hPa", "wind_direction_{}hPa"]
MIN_LAYER, SCALE_HEIGHT_PER_K, MIN_READINGS = 300, 29.3, 10
LAMBDA = 500e-9
K2 = (2 * math.pi / LAMBDA) ** 2


def fetch(url, path):
    """GET url into path unless cached; returns the text. One request a second."""
    if not os.path.exists(path):
        time.sleep(1)
        req = urllib.request.Request(url, headers={"User-Agent": "astro-recommender calibrate_seeing"})
        with urllib.request.urlopen(req, timeout=120) as r, open(path, "wb") as f:
            f.write(r.read())
    with open(path, encoding="utf-8") as f:
        return f.read()


def months(first, last):
    y, m = map(int, first.split("-"))
    while f"{y:04d}-{m:02d}" <= last:
        yield y, m
        y, m = (y + 1, 1) if m == 12 else (y, m + 1)


def forecast(site, y, m, cache):
    lat, lon, _ = SITES[site]
    hourly = ",".join(SURFACE + [v.format(p) for p in PRESSURES for v in LEVEL])
    url = ("https://historical-forecast-api.open-meteo.com/v1/forecast?"
           f"latitude={lat:.2f}&longitude={lon:.2f}&models=ecmwf_ifs025&start_date={y:04d}-{m:02d}-01"
           f"&end_date={y:04d}-{m:02d}-{calendar.monthrange(y, m)[1]:02d}&hourly={hourly}&timeformat=unixtime&timezone=UTC")
    body = json.loads(fetch(url, os.path.join(cache, f"{site}-{y:04d}-{m:02d}.json")))
    if "hourly" not in body:
        sys.exit(f"open-meteo {y}-{m:02d}: {body.get('reason', body)}")
    return body


def dimm(site, y, m, cache):
    """Median DIMM seeing per UTC hour (unix) with at least MIN_READINGS readings."""
    form = SITES[site][2]
    rng = f"{y:04d}-{m:02d}-01T00:00:00..{y:04d}-{m:02d}-{calendar.monthrange(y, m)[1]:02d}T23:59:59"
    url = (f"https://archive.eso.org/wdb/wdb/asm/{form}/query?"
           + urllib.parse.urlencode({"wdbo": "csv/download", "start_date": rng, "tab_fwhm": "on", "top": 200000}))
    text = fetch(url, os.path.join(cache, f"{site}-{y:04d}-{m:02d}.csv"))
    rows = csv.reader(line for line in text.splitlines() if line and not line.startswith("#"))
    header = next(rows, None)
    if header is None and "No data returned" in text:
        return {}  # the DIMM has gaps of months (La Silla, 2024)
    if not header or not header[0].startswith("Date"):
        sys.exit(f"eso {form} {y}-{m:02d}: unexpected response\n{text[:300]}")
    col = next(i for i, h in enumerate(header) if h.startswith("DIMM Seeing"))
    per_hour = {}
    for row in rows:
        if len(row) <= col or not row[col]:
            continue
        t = dt.datetime.fromisoformat(row[0]).replace(tzinfo=dt.timezone.utc)
        hour = int(t.timestamp()) // 3600 * 3600
        eps = float(row[col])
        if 0.1 <= eps <= 5:
            per_hour.setdefault(hour, []).append(eps)
    return {h: statistics.median(v) for h, v in per_hour.items() if len(v) >= MIN_READINGS}


# --- weather.levels() in Python; a level is (P, Z, T, wind km/h, dir) as dewan.py reads it.

def plausible(l):
    return -1000 <= l[1] <= 30000 and -150 <= l[2] <= 60 and 0 <= l[3] <= 1000 and 0 <= l[4] <= 360


def barometric(p, elev, t):
    want = 1013.25 * math.exp(-elev / (SCALE_HEIGHT_PER_K * (t + 273.15 + 0.00325 * elev)))
    return 0.88 * want <= p <= 1.12 * want


def hypsometric(a, b):
    want = SCALE_HEIGHT_PER_K * ((a[2] + b[2]) / 2 + 273.15) * math.log(a[0] / b[0])
    return 0.7 * want <= b[1] - a[1] <= 1.3 * want


def levels(h, i, elev):
    """The hour's levels as weather.levels() keeps them, or None without a complete profile."""
    def val(key):
        v = h.get(key)
        return v[i] if v is not None and i < len(v) and v[i] is not None and math.isfinite(v[i]) else None

    s = [val(k) for k in SURFACE]
    if None in s:
        return None
    surf = (s[0], elev + 2, s[1], s[2], s[3])
    if not (plausible(surf) and barometric(surf[0], elev, surf[2])):
        return None
    lv = [surf]
    for p in PRESSURES:
        v = [val(k.format(p)) for k in LEVEL]
        if None in v:
            continue
        l, below = (float(p), v[0], v[1], v[2], v[3]), lv[-1]
        if plausible(l) and l[1] >= below[1] + MIN_LAYER and hypsometric(below, l):
            lv.append(l)
    return lv if len(lv) > 4 and lv[-1][0] <= 300 else None  # the surface and minPressureLevels = 3, up to the jet


def integrals(lv):
    """(ground, free): Dewan's ∫Cn² dz over the first layer and over the rest."""
    trop = tropopause(lv)
    parts = [cn2(a, b, i >= trop) * (b[1] - a[1]) if b[1] - a[1] >= MIN_LAYER else 0 for i, (a, b) in enumerate(zip(lv, lv[1:]))]
    return parts[0], sum(parts[1:])


def seeing_of(j):
    """FWHM in arcseconds at the zenith and 500 nm of the Cn² integral j."""
    return 0.98 * LAMBDA * (0.423 * K2 * j) ** 0.6 * 180 / math.pi * 3600


def integral_of(eps):
    """The inverse of seeing_of: the Cn² integral a FWHM (arcseconds) implies."""
    return (eps / 3600 / 180 * math.pi / (0.98 * LAMBDA)) ** (5 / 3) / (0.423 * K2)


# --- the fit

def fit(rows, two):
    """Least squares J = a_g G + a_f F over (G, F, J) rows; one factor shared when not two.
    A negative factor is clamped to 0 and the other refit."""
    if not two:
        a = sum(j * (g + f) for g, f, j in rows) / sum((g + f) ** 2 for g, f, j in rows)
        return a, a
    sgg = sum(g * g for g, f, j in rows)
    sff = sum(f * f for g, f, j in rows)
    sgf = sum(g * f for g, f, j in rows)
    sgj = sum(g * j for g, f, j in rows)
    sfj = sum(f * j for g, f, j in rows)
    det = sgg * sff - sgf * sgf
    ag, af = (sgj * sff - sfj * sgf) / det, (sfj * sgg - sgj * sgf) / det
    if ag < 0:
        ag, af = 0, sfj / sff
    if af < 0:
        ag, af = sgj / sgg, 0
    return ag, af


def stats(rows, ag, af):
    obs = [seeing_of(j) for g, f, j in rows]
    mod = [seeing_of(ag * g + af * f) for g, f, j in rows]
    diff = [m - o for m, o in zip(mod, obs)]
    logd = [math.log(m / o) for m, o in zip(mod, obs)]
    return (len(rows), statistics.fmean(diff), math.sqrt(statistics.fmean(d * d for d in diff)),
            statistics.correlation(mod, obs), math.sqrt(statistics.fmean(d * d for d in logd)), statistics.median(mod), statistics.median(obs))


def report(label, rows, ag, af):
    n, bias, rmse, r, logrmse, med_m, med_o = stats(rows, ag, af)
    print(f"  {label:<34} a_g {ag:6.3f}  a_f {af:6.3f}  bias {bias:+.2f}\"  rmse {rmse:.2f}\"  r {r:.2f}  "
          f"log-rmse {logrmse:.2f}  median {med_m:.2f}\" vs {med_o:.2f}\" observed  (n={n})")


def selftest():
    # Known factors are recovered from exact data, with and without the clamp.
    rows = [(g, f, 0.3 * g + 1.7 * f) for g in (1e-13, 3e-13, 9e-13) for f in (2e-13, 5e-13)]
    ag, af = fit(rows, True)
    assert abs(ag - 0.3) < 1e-9 and abs(af - 1.7) < 1e-9, (ag, af)
    ag, af = fit([(g, f, 1.7 * f - 0.1 * g) for g, f, j in rows], True)
    assert ag == 0 and af > 0, (ag, af)
    assert abs(integral_of(seeing_of(5e-13)) - 5e-13) < 1e-22
    # A standard-atmosphere hour builds the surface and eleven levels.
    h = {"surface_pressure": [1013.25], "temperature_2m": [15.0], "wind_speed_10m": [10.0], "wind_direction_10m": [270.0]}
    for p, z in zip(PRESSURES, [800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800]):
        h.update({LEVEL[0].format(p): [float(z)], LEVEL[1].format(p): [15 - 6.5 * min(z, 11000) / 1000],
                  LEVEL[2].format(p): [20.0], LEVEL[3].format(p): [270.0]})
    lv = levels(h, 0, 0)
    assert lv is not None and len(lv) == 12, lv
    g, f = integrals(lv)
    assert g > 0 and f > 0 and 0.5 <= seeing_of(g + f) <= 1.5, (g, f)
    h["geopotential_height_850hPa"] = [700.0]  # under 925 hPa: dropped, the rest kept
    assert len(levels(h, 0, 0)) == 11
    print("selftest ok")


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--site", choices=SITES, default="paranal")
    ap.add_argument("--from", dest="first", default="2024-05", help="first month, YYYY-MM (archive complete from 2024-05)")
    last = dt.date.today().replace(day=1) - dt.timedelta(days=1)
    ap.add_argument("--to", dest="last", default=f"{last:%Y-%m}", help="last month, YYYY-MM (default: last full month)")
    ap.add_argument("--cache", default=os.path.expanduser("~/.cache/astro-recommender/calibrate"))
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        return selftest()
    os.makedirs(args.cache, exist_ok=True)

    rows, by_month, dropped, elev = [], {}, 0, None
    for y, m in months(args.first, args.last):
        fc = forecast(args.site, y, m, args.cache)
        elev = fc["elevation"]
        obs = dimm(args.site, y, m, args.cache)
        h, n = fc["hourly"], 0
        for i, t in enumerate(h["time"]):
            if t not in obs:
                continue
            lv = levels(h, i, elev)
            if lv is None:
                dropped += 1
                continue
            g, f = integrals(lv)
            rows.append((g, f, integral_of(obs[t])))
            by_month.setdefault(m % 2, []).append(rows[-1])
            n += 1
        print(f"{y}-{m:02d}: {n} hours matched", file=sys.stderr)
    if len(rows) < 20:
        sys.exit(f"only {len(rows)} matched hours")

    print(f"{args.site}: {len(rows)} hours, {dropped} forecast hours without a complete profile, model elevation {elev:.0f} m")
    print(f"  ground share of the raw integral: median {statistics.median(g / (g + f) for g, f, j in rows):.0%}")
    print("Held out (fit on even months, evaluated on odd):")
    train, test = by_month.get(0, []), by_month.get(1, [])
    if len(train) >= 20 and len(test) >= 20:
        report("raw Dewan", test, 1, 1)
        report("one factor", test, *fit(train, False))
        report("ground + free factors", test, *fit(train, True))
    else:
        print("  not enough months")
    print("All hours:")
    report("raw Dewan", rows, 1, 1)
    report("one factor", rows, *fit(rows, False))
    report("ground + free factors", rows, *fit(rows, True))


if __name__ == "__main__":
    main()
