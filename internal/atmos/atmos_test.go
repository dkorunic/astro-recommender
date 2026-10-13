// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import (
	"math"
	"testing"
)

func TestSkyBrightness(t *testing.T) {
	// Pickering airmass and K&S sky brightness against textbook values.
	if math.Abs(Airmass(90)-1) > 1e-3 || math.Abs(Airmass(30)-2) > 0.01 {
		t.Errorf("airmass(90, 30) = %v, %v", Airmass(90), Airmass(30))
	}
	dark := NanoLamberts(22)
	if m := MagFromNL(SkyBrightness(dark, 0.172, 90, -10, 0, 0, meanMoonDist)); math.Abs(m-22) > 1e-6 {
		t.Errorf("moonless zenith = %v mag, want 22", m)
	}
	// Full Moon at 60°, target at 60° and 90° away: ~18.8 mag/arcsec² from K&S,
	// 0.33 mag brighter at exact full with the opposition surge.
	if m := MagFromNL(SkyBrightness(dark, 0.172, 60, 60, 90, 0, meanMoonDist)); math.Abs(m-18.45) > 0.05 {
		t.Errorf("full-moon sky = %v mag, want ~18.45", m)
	}
	// Moonlight alone: x1.35 at full, tapering to none at 7°; and as distance⁻².
	moon := func(alpha, dist float64) float64 {
		return SkyBrightness(dark, 0.172, 60, 60, 90, alpha, dist) - SkyBrightness(dark, 0.172, 60, -10, 90, alpha, dist)
	}
	near := func(a, b float64) bool { return math.Abs(a/b-1) < 1e-9 }
	if r := moon(6.9999999, meanMoonDist) / moon(7, meanMoonDist); math.Abs(r-1) > 1e-6 {
		t.Errorf("surge not continuous at 7°: ratio %v", r)
	}
	// I*(0)/I*(7) without the surge is 10^(0.4·0.026·7); with it, x1.35 more.
	if r, want := moon(0, meanMoonDist)/moon(7, meanMoonDist), 1.35*math.Pow(10, 0.4*(0.026*7+4e-9*7*7*7*7)); !near(r, want) {
		t.Errorf("surge at full: ratio %v, want %v", r, want)
	}
	if r, want := moon(30, 56)/moon(30, 63.8), math.Pow(63.8/56, 2); !near(r, want) {
		t.Errorf("distance: ratio %v, want %v", r, want)
	}
}

func TestExtinctionCoeff(t *testing.T) {
	// Extinction: sea level, no aerosols ~0.136; Zagreb (130 m, AOD 0.09) ~0.232.
	if k := ExtinctionCoeff(0, 0); math.Abs(k-0.1356) > 1e-3 {
		t.Errorf("extinctionCoeff(0, 0) = %v", k)
	}
	if k := ExtinctionCoeff(130, 0.09); math.Abs(k-0.232) > 2e-3 {
		t.Errorf("extinctionCoeff(130, 0.09) = %v", k)
	}
}

func TestBortleClass(t *testing.T) {
	// bortleClass inverts the SQM bands; each class's typical value maps back to it.
	for b := 1; b <= 9; b++ {
		if got := BortleClass(BortleMag[b]); got != b {
			t.Errorf("bortleClass(%v) = %d, want %d", BortleMag[b], got, b)
		}
	}
}

// SkyParts splits SkyBrightnessLit without changing it, and BandSky passes a
// band's share of the V width, Rayleigh moonlight going as λ⁻⁴: an [O III]
// band 2.9 times what an Hα one does.
func TestSkyPartsBandSky(t *testing.T) {
	dark := NanoLamberts(19)
	for _, c := range []struct{ alt, rho, light float64 }{{60, 90, 0}, {40, 20, 1e-6}, {75, 120, 3e-7}} {
		glow, rayleigh, mie := SkyParts(dark, 0.2, c.alt, c.rho, c.light)
		if total := SkyBrightnessLit(dark, 0.2, c.alt, c.rho, c.light); math.Abs(glow+rayleigh+mie-total) > 1e-12*total {
			t.Errorf("%+v: parts %v + %v + %v, total %v", c, glow, rayleigh, mie, total)
		}
		if c.light == 0 && (rayleigh != 0 || mie != 0) {
			t.Errorf("moonlight without a Moon: %v %v", rayleigh, mie)
		}
	}
	glow, rayleigh, mie := BandSky(vCenter, 8.8)
	if glow != 0.1 || math.Abs(rayleigh-0.1) > 1e-15 || math.Abs(mie-0.1) > 1e-15 {
		t.Errorf("BandSky at V = %v %v %v, want 0.1 each", glow, rayleigh, mie)
	}
	_, ha, haMie := BandSky(656.3, 7)
	_, o3, o3Mie := BandSky(500.7, 7)
	if r := o3 / ha; math.Abs(r-math.Pow(656.3/500.7, 4)) > 1e-12 || r < 2.9 || r > 3 {
		t.Errorf("Rayleigh [O III]/Hα = %v", r)
	}
	if r := o3Mie / haMie; math.Abs(r-math.Pow(656.3/500.7, angstrom)) > 1e-12 {
		t.Errorf("Mie [O III]/Hα = %v", r)
	}
}

// Zodiacal reads Leinert's table: the pole value past the grid, the
// reference direction's excess 0, the ecliptic 90° from the Sun half a
// magnitude brighter on a pristine sky, symmetric either side of the Sun and
// the ecliptic, and the solar vicinity's blanks filled from the nearest
// tabulated latitude.
func TestZodiacal(t *testing.T) {
	mag := func(nl float64) float64 { return MagFromNL(nl) }
	if got := mag(Zodiacal(0, 90)); math.Abs(got-(s10Mag-2.5*math.Log10(zodiacalPole/s10))) > 1e-9 || Zodiacal(0, 90) != ZodiacalPoleNL {
		t.Errorf("pole %v mag/arcsec²", got)
	}
	if got := mag(Zodiacal(90, 0)); math.Abs(got-22.02) > 0.01 {
		t.Errorf("ecliptic at 90° %v, want 22.02", got)
	}
	if ex := ZodiacalExcess(ZodiacalRefLon, ZodiacalRefLat); ex != 0 {
		t.Errorf("excess at the reference %v", ex)
	}
	sky := NanoLamberts(22)
	if got := mag(sky + ZodiacalExcess(90, 0)); math.Abs(got-21.47) > 0.02 {
		t.Errorf("pristine sky on the ecliptic 90° from the Sun %v, want 21.47", got)
	}
	if got := mag(sky + ZodiacalExcess(0, 90)); math.Abs(got-22.06) > 0.01 {
		t.Errorf("pristine sky at the pole %v, want 22.06", got)
	}
	// Mirror symmetry, and the longitude wrapped either way.
	for _, c := range [][2]float64{{90, 30}, {120, 60}, {170, 80}} {
		want := Zodiacal(c[0], c[1])
		for _, m := range [][2]float64{{-c[0], c[1]}, {c[0], -c[1]}, {360 - c[0], c[1]}, {c[0] + 360, c[1]}, {c[0], 180 - c[1]}} {
			if got := Zodiacal(m[0], m[1]); math.Abs(got-want) > 1e-12 {
				t.Errorf("Zodiacal%v = %v, want Zodiacal%v = %v", m, got, c, want)
			}
		}
	}
	// On the grid the table value comes back; between rows and columns the
	// value lies between the neighbours.
	if got := Zodiacal(60, 15); math.Abs(MagFromNL(got)-(s10Mag-2.5*math.Log10(292/s10))) > 1e-9 {
		t.Errorf("grid point (60, 15) = %v", MagFromNL(got))
	}
	if got, lo, hi := Zodiacal(67.5, 17.5), Zodiacal(75, 20), Zodiacal(60, 15); !(got > lo && got < hi) {
		t.Errorf("between the grid: %v not within %v..%v", got, lo, hi)
	}
	// The blanks near the Sun read as the row's first tabulated latitude.
	if a, b := Zodiacal(0, 0), Zodiacal(0, 15); a != b {
		t.Errorf("solar vicinity (0, 0) = %v, want the (0, 15) value %v", a, b)
	}
	// Brightness falls with latitude and, away from the Sun, rises towards
	// the antisolar point (the gegenschein).
	for lat := 5.0; lat <= 90; lat += 5 {
		if Zodiacal(90, lat) >= Zodiacal(90, lat-5) {
			t.Errorf("not falling with latitude at %v", lat)
		}
	}
	if Zodiacal(180, 0) <= Zodiacal(150, 0) {
		t.Error("no gegenschein")
	}
}

// ExtinctionCoeff is the sum of its three parts; SplitExtinction recovers
// the Rayleigh and aerosol parts of a fixed coefficient, ozone first, Rayleigh
// up to sea level, the rest aerosol; Transmission is Extinction at the airmass.
func TestExtinctionParts(t *testing.T) {
	for _, c := range [][2]float64{{0, 0}, {2400, 0.1}, {5000, 0.5}} {
		if got, want := ExtinctionCoeff(c[0], c[1]), RayleighExtinction(c[0])+OzoneExtinction+AerosolExtinction(c[1]); got != want {
			t.Errorf("ExtinctionCoeff%v = %v, want %v", c, got, want)
		}
	}
	for _, c := range []struct{ k, ray, aer float64 }{{0.2, 0.1066, 0.2 - 0.029 - 0.1066}, {0.1, 0.071, 0}, {0.01, 0, 0}, {0, 0, 0}} {
		ray, aer := SplitExtinction(c.k)
		if math.Abs(ray-c.ray) > 1e-9 || math.Abs(aer-c.aer) > 1e-9 {
			t.Errorf("SplitExtinction(%v) = %v, %v, want %v, %v", c.k, ray, aer, c.ray, c.aer)
		}
	}
	if got, want := Transmission(Airmass(35), 0.2), Extinction(35, 0.2); got != want {
		t.Errorf("Transmission %v, Extinction %v", got, want)
	}
}
