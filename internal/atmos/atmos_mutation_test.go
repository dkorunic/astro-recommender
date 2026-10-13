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

// mutRSSMoon is Winkler's (2022) single-scattered moonlight written out from
// the paper, the line-of-sight integral (his eq. 6) summed numerically rather
// than in closed form: the Moon's V flux (Allen's magnitude law, the
// distance and the opposition surge) dimmed by the air above each height h,
// scattered by the normalised phase function (his eqs. 9, 10, 12), dimmed on
// the way down, in a uniform layer of optical depth 0.921k, K&S's scattering
// airmass standing for sec ζ; × 10^δ, per arcsec², in nanoLamberts.
func mutRSSMoon(k, kRay, kAer, alt, moonAlt, rho, alpha, dist float64) float64 {
	X := func(a float64) float64 {
		z := (90 - a) * math.Pi / 180
		return 1 / math.Sqrt(1-0.96*math.Sin(z)*math.Sin(z))
	}
	flux := NanoLamberts(-12.73) * math.Pow(10, -0.4*(0.026*alpha+4e-9*math.Pow(alpha, 4))) * (60.27 / dist) * (60.27 / dist)
	if alpha < 7 {
		flux *= 1.35 - 0.05*alpha
	}
	const chi, g = 0.0148, 0.8
	c := math.Cos(rho * math.Pi / 180)
	pR := 3 * (1 - chi) / (4 * (1 + 2*chi)) * ((1+3*chi)/(1-chi) + c*c) / (4 * math.Pi)
	pM := (1 - g*g) / math.Pow(1+g*g-2*g*c, 1.5) / (4 * math.Pi)
	p := (kRay*pR + kAer*pM) / (kRay + kAer)
	tau := 0.921034 * k
	z, zeta := X(moonAlt), X(alt)
	// Simpson's rule over h in [0, 1] vertical airmasses.
	const n = 2000
	sum := 0.0
	for i := 0; i <= n; i++ {
		h := float64(i) / n
		f := tau * zeta * math.Exp(-tau*((1-h)*z+h*zeta))
		switch {
		case i == 0 || i == n:
			sum += f
		case i%2 == 1:
			sum += 4 * f
		default:
			sum += 2 * f
		}
	}
	gamma := sum / (3 * n) / tau // the gradation function, his τs/τ taken into δ

	return flux * math.Pow(10, 0.078) * p * gamma * tau / (206264.80624709636 * 206264.80624709636)
}

func TestMutMoonLight(t *testing.T) {
	if got := MoonLight(0.2, 0.1, 0.07, 0, 10, 60); got != (Moon{}) {
		t.Errorf("Moon on the horizon gives %+v, want none", got)
	}
	if got := MoonLight(0.2, 0.1, 0.07, 0.5, 10, 60); got.NL <= 0 {
		t.Errorf("Moon just up gives %+v, want light", got)
	}
	// Nearer Moon is brighter as distance⁻².
	mutNear(t, "distance", MoonLight(0.2, 0.1, 0.07, 40, 30, 56).NL/MoonLight(0.2, 0.1, 0.07, 40, 30, 60.27).NL, (60.27/56)*(60.27/56), 1e-9)
	// Opposition surge: ×1.35 at full, ×1.1 at 5°, none at 10°.
	base := func(a float64) float64 { return math.Pow(10, -0.4*(0.026*a+4e-9*math.Pow(a, 4))) }
	mutNear(t, "surge 0/10", MoonLight(0.2, 0.1, 0.07, 40, 0, 60).NL/MoonLight(0.2, 0.1, 0.07, 40, 10, 60).NL, 1.35*base(0)/base(10), 1e-9)
	mutNear(t, "surge 5/10", MoonLight(0.2, 0.1, 0.07, 40, 5, 60).NL/MoonLight(0.2, 0.1, 0.07, 40, 10, 60).NL, 1.1*base(5)/base(10), 1e-9)
	// The beam's airmass, its transmission and the Rayleigh share.
	m := MoonLight(0.2, 0.1, 0.07, 30, 10, 60)
	X := 1 / math.Sqrt(1-0.96*0.75)
	mutNear(t, "moon airmass", m.X, X, 1e-12)
	mutNear(t, "moon fade", m.Fade, math.Pow(10, -0.4*0.2*X), 1e-12)
	mutNear(t, "Rayleigh share", m.Ray, 0.1/0.17, 1e-12)
	if m := MoonLight(0.03, 0, 0, 30, 10, 60); m.Ray != 1 {
		t.Errorf("no scattering split: Rayleigh share %v, want 1", m.Ray)
	}
}

func TestMutSkyBrightness(t *testing.T) {
	z := NanoLamberts(21.5)
	mutNear(t, "dark zenith", SkyBrightness(z, 0.2, 90, -10, 50, 0, 60), z, 1e-9)
	// K&S eq. 2: dark sky at zenith distance Z is B_zen 10^(−0.4k(X−1)) X.
	X := 1 / math.Sqrt(1-0.96*0.75)
	mutNear(t, "dark 30°", SkyBrightness(z, 0.2, 30, -10, 50, 0, 60), z*math.Pow(10, -0.4*0.2*(X-1))*X, 1e-6)
	// The moonlight against the numerical integral, the Moon above, below
	// and level with the target (the closed form's X = x limit).
	for _, c := range [][5]float64{{60, 30, 25, 20, 58}, {45, 50, 90, 60, 62}, {35, 20, 5, 3, 60.27}, {50, 50, 70, 10, 60}, {40, 40.0000001, 30, 0, 60}} {
		ray, aer := SplitExtinction(0.15)
		dark := SkyBrightness(z, 0.15, c[0], -10, c[2], c[3], c[4])
		got := SkyBrightness(z, 0.15, c[0], c[1], c[2], c[3], c[4]) - dark
		want := mutRSSMoon(0.15, ray, aer, c[0], c[1], c[2], c[3], c[4])
		if math.Abs(got-want) > 1e-6*want {
			t.Errorf("moonlight %v = %g, want %g", c, got, want)
		}
	}
}
