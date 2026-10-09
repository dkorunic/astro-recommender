// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import (
	"math"
	"testing"
)

func mutNear(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %.6g, want %.6g ± %g", name, got, want, tol)
	}
}

// Band edges are inclusive: an SQM of exactly 21.75 is Bortle 1.
func TestMutBortleClassEdges(t *testing.T) {
	for _, c := range []struct {
		mag  float64
		want int
	}{{22.0, 1}, {21.75, 1}, {21.74, 2}, {21.5, 2}, {17.5, 8}, {17.49, 9}, {15, 9}} {
		if got := BortleClass(c.mag); got != c.want {
			t.Errorf("BortleClass(%v) = %d, want %d", c.mag, got, c.want)
		}
	}
}

// Rayleigh 0.1066 + ozone 0.029 at sea level; AOD τ adds 2.5·log10(e)·τ =
// 1.0857τ mag; altitude thins the Rayleigh part.
func TestMutExtinctionCoeff(t *testing.T) {
	mutNear(t, "k(0,0)", ExtinctionCoeff(0, 0), 0.1356, 1e-4)
	mutNear(t, "k(0,0.2)-k(0,0)", ExtinctionCoeff(0, 0.2)-ExtinctionCoeff(0, 0), 0.2*2.5*math.Log10(math.E), 1e-3)
	mutNear(t, "k(7996,0)", ExtinctionCoeff(7996, 0), 0.1066/math.E+0.029, 1e-4)
}

// Pickering 2002: X(90°) ≈ 1, X(30°) ≈ 1.995; below the horizon it is X(0)
// (≈ 38), never NaN.
func TestMutAirmass(t *testing.T) {
	mutNear(t, "X(90)", Airmass(90), 1, 1e-3)
	mutNear(t, "X(30)", Airmass(30), 1.995, 0.01)
	mutNear(t, "X(0)", Airmass(0), 38, 2)
	mutNear(t, "X(-5)", Airmass(-5), Airmass(0), 1e-9)
}

func TestMutExtinction(t *testing.T) {
	mutNear(t, "zenith", Extinction(90, 0.3), 1, 1e-3)
	want := math.Pow(10, -0.4*0.2*(Airmass(30)-1))
	mutNear(t, "alt30", Extinction(30, 0.2), want, 1e-9)
	mutNear(t, "alt30 approx", Extinction(30, 0.2), 0.8325, 2e-3)
}

// K&S eq. 1: B = 34.08 exp(20.7233 − 0.92104 V); V = 22 gives ≈ 54 nL.
func TestMutNanoLamberts(t *testing.T) {
	mutNear(t, "NL(22)", NanoLamberts(22), 54.0, 0.2)
	for _, v := range []float64{17, 20.5, 22, 25} {
		mutNear(t, "roundtrip", MagFromNL(NanoLamberts(v)), v, 1e-9)
	}
	// 5 mag is a factor 100.
	mutNear(t, "5 mag", NanoLamberts(17)/NanoLamberts(22), 100, 0.01)
}

// mutKSMoon is K&S (1991) eq. 8-21 written out from the paper.
func mutKSMoon(k, alt, moonAlt, rho, alpha, dist float64) float64 {
	X := func(a float64) float64 {
		z := (90 - a) * math.Pi / 180
		return 1 / math.Sqrt(1-0.96*math.Sin(z)*math.Sin(z))
	}
	istar := math.Pow(10, -0.4*(3.84+0.026*alpha+4e-9*math.Pow(alpha, 4)))
	istar *= (60.27 / dist) * (60.27 / dist)
	if alpha < 7 {
		istar *= 1.35 - 0.05*alpha
	}
	r := rho * math.Pi / 180
	f := math.Pow(10, 5.36)*(1.06+math.Cos(r)*math.Cos(r)) + math.Pow(10, 6.15-rho/40)

	return f * istar * math.Pow(10, -0.4*k*X(moonAlt)) * (1 - math.Pow(10, -0.4*k*X(alt)))
}

func TestMutMoonLight(t *testing.T) {
	if got := MoonLight(0.2, 0, 10, 60); got != 0 {
		t.Errorf("Moon on the horizon gives %g, want 0", got)
	}
	if got := MoonLight(0.2, 0.5, 10, 60); got <= 0 {
		t.Errorf("Moon just up gives %g, want > 0", got)
	}
	// Nearer Moon is brighter as distance⁻².
	mutNear(t, "distance", MoonLight(0.2, 40, 30, 56)/MoonLight(0.2, 40, 30, 60.27), (60.27/56)*(60.27/56), 1e-9)
	// Opposition surge: ×1.35 at full, ×1.1 at 5°, none at 10°.
	base := func(a float64) float64 { return math.Pow(10, -0.4*(0.026*a+4e-9*math.Pow(a, 4))) }
	mutNear(t, "surge 0/10", MoonLight(0.2, 40, 0, 60)/MoonLight(0.2, 40, 10, 60), 1.35*base(0)/base(10), 1e-9)
	mutNear(t, "surge 5/10", MoonLight(0.2, 40, 5, 60)/MoonLight(0.2, 40, 10, 60), 1.1*base(5)/base(10), 1e-9)
}

func TestMutSkyBrightness(t *testing.T) {
	z := NanoLamberts(21.5)
	mutNear(t, "dark zenith", SkyBrightness(z, 0.2, 90, -10, 50, 0, 60), z, 1e-9)
	// K&S eq. 2: dark sky at zenith distance Z is B_zen 10^(−0.4k(X−1)) X.
	X := 1 / math.Sqrt(1-0.96*0.75)
	mutNear(t, "dark 30°", SkyBrightness(z, 0.2, 30, -10, 50, 0, 60), z*math.Pow(10, -0.4*0.2*(X-1))*X, 1e-6)
	for _, c := range [][5]float64{{60, 30, 25, 20, 58}, {45, 50, 90, 60, 62}, {35, 20, 5, 3, 60.27}} {
		dark := SkyBrightness(z, 0.15, c[0], -10, c[2], c[3], c[4])
		got := SkyBrightness(z, 0.15, c[0], c[1], c[2], c[3], c[4]) - dark
		want := mutKSMoon(0.15, c[0], c[1], c[2], c[3], c[4])
		if math.Abs(got-want) > 1e-6*want {
			t.Errorf("moonlight %v = %g, want %g", c, got, want)
		}
	}
}
