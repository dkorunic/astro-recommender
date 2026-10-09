#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
# SPDX-License-Identifier: MIT
"""Oracle for internal/weather's Seeing: Dewan et al. (1993) Cn² per layer,
the WMO tropopause on the forecast levels, Fried's r0 and the FWHM at 500 nm.
Prints the numbers the tests pin (TestSeeing, TestCn2Stratospheric,
TestMutCn2); change the Go code and this together."""

import math

PRESSURES = [1000, 925, 850, 700, 600, 500, 400, 300, 250, 200, 150, 100]
HEIGHTS = [110, 800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800]


def wind(l):
    """East and north components in m/s of (P, Z, T, speed km/h, direction)."""
    s, c = math.sin(math.radians(l[4])), math.cos(math.radians(l[4]))
    ms = l[3] / 3.6
    return -ms * s, -ms * c


def cn2(a, b, stratospheric):
    dz = b[1] - a[1]
    p, t = (a[0] + b[0]) / 2, (a[2] + b[2]) / 2 + 273.15
    au, av = wind(a)
    bu, bv = wind(b)
    s = math.hypot(bu - au, bv - av) / dz
    m = -79e-6 * p / (t * t) * ((b[2] - a[2]) / dz + 9.8e-3)
    y = 0.506 + 50 * s if stratospheric else 1.64 + 42 * s
    return 2.8 * m * m * 0.1 ** (4 / 3) * 10**y


def tropopause(lv):
    def qualifies(i):
        dz = lv[i + 1][1] - lv[i][1]
        return dz >= 50 and (lv[i + 1][2] - lv[i][2]) / dz >= -2e-3

    for i in range(len(lv) - 1):
        if (lv[i][0] + lv[i + 1][0]) / 2 <= 500 and qualifies(i) and (i + 2 >= len(lv) or qualifies(i + 1)):
            return i
    return len(lv) - 1


def seeing(lv, trop=None):
    if len(lv) < 3 or lv[-1][0] > 300:
        return 0
    trop = tropopause(lv) if trop is None else trop
    integral = sum(cn2(a, b, i >= trop) * (b[1] - a[1]) for i, (a, b) in enumerate(zip(lv, lv[1:])) if b[1] - a[1] >= 50)
    k = 2 * math.pi / 500e-9
    r0 = (0.423 * k * k * integral) ** -0.6
    return 0.98 * 500e-9 / r0 * 180 / math.pi * 3600


def profile(sea_t, lapse, winds, dirs):
    """The standard levels: lapse K/km, isothermal above 11 km."""
    return [(p, z, sea_t - lapse * min(z, 11000) / 1000, w, d) for p, z, w, d in zip(PRESSURES, HEIGHTS, winds, dirs)]


if __name__ == "__main__":
    west = [270] * 12
    std = profile(15, 6.5, [20] * 12, west)
    print(f"standard atmosphere      {seeing(std):.3f}\"  (TestSeeing 0.5-0.9)")
    jet = profile(15, 6.5, [20, 20, 30, 50, 80, 120, 180, 250, 250, 200, 120, 60], west)
    print(f"jet                      {seeing(jet):.3f}\"  (0.9-1.5)")
    veer = profile(15, 6.5, [60] * 12, [270 + 90 * (i % 2) for i in range(12)])
    print(f"veering every level      {seeing(veer):.3f}\"  (2-4)")
    above = [l for l in profile(12, 6.5, [20] * 12, west) if l[1] >= 120]
    above[0] = (above[0][0], above[0][1], 12, above[0][3], above[0][4])
    surface = (1000, 120, 10, 10, 270)
    neutral = [l for l in profile(10, 6.5, [20] * 12, west) if l[1] >= 120]
    print(f"inversion ground layer   {seeing([surface] + above):.3f}\"  (1.2-1.6)")
    print(f"neutral ground layer     {seeing([surface] + neutral):.3f}\"  vs none {seeing(neutral):.3f}\"")
    polar = profile(0, 6.5, [20] * 12, west)
    polar = [(p, z, max(t, polar[6][2]), w, d) for p, z, t, w, d in polar]
    print(f"polar, tropopause at 400 {seeing(polar):.3f}\"  (0.6-0.75); at 200 {seeing(polar, 9):.3f}\"; all troposphere {seeing(polar, 99):.3f}\"")
    a, b = (850, 1500, 8, 20, 270), (700, 3000, -4.5, 56, 270)
    print(f"cn2 850-700 hPa          {cn2(a, b, False):.3g} m^-2/3  (TestMutCn2 1.53e-17)")
    a, b = (250, 10400, -56.5, 100, 270), (200, 11800, -56.5, 136, 270)
    print(f"cn2 strat/trop           {cn2(a, b, True) / cn2(a, b, False):.3f}  (TestCn2Stratospheric < 0.1)")
