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
	if m := MagFromNL(SkyBrightness(dark, 0.172, 90, -10, 0, 0)); math.Abs(m-22) > 1e-6 {
		t.Errorf("moonless zenith = %v mag, want 22", m)
	}
	// Full Moon at 60°, target at 60° and 90° away: ~18.8 mag/arcsec².
	if m := MagFromNL(SkyBrightness(dark, 0.172, 60, 60, 90, 0)); math.Abs(m-18.8) > 0.2 {
		t.Errorf("full-moon sky = %v mag, want ~18.8", m)
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
