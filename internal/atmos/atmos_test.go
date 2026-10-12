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
