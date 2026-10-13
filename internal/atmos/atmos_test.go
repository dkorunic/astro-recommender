// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import (
	"cmp"
	"math"
	"slices"
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
	// Full Moon (with the opposition surge) at 60°, target at 60° and 90°
	// away: 18.60 mag/arcsec² from Winkler's model, where K&S gave 18.45.
	if m := MagFromNL(SkyBrightness(dark, 0.172, 60, 60, 90, 0, meanMoonDist)); math.Abs(m-18.60) > 0.01 {
		t.Errorf("full-moon sky = %v mag, want 18.60", m)
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
	for _, c := range []struct{ alt, rho, moonAlt float64 }{{60, 90, -5}, {40, 20, 30}, {75, 120, 60}} {
		m := MoonLight(0.2, 0.1, 0.071, c.moonAlt, 20, 60)
		glow, rayleigh, mie := SkyParts(dark, 0.2, c.alt, c.rho, m)
		if total := SkyBrightnessLit(dark, 0.2, c.alt, c.rho, m); math.Abs(glow+rayleigh+mie-total) > 1e-12*total {
			t.Errorf("%+v: parts %v + %v + %v, total %v", c, glow, rayleigh, mie, total)
		}
		if c.moonAlt < 0 && (rayleigh != 0 || mie != 0) {
			t.Errorf("moonlight without a Moon: %v %v", rayleigh, mie)
		}
		if c.moonAlt > 0 && (rayleigh <= 0 || mie <= 0) {
			t.Errorf("%+v: no moonlight %v %v", c, rayleigh, mie)
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

func TestBroadSky(t *testing.T) {
	// The camera band is the unfiltered sky; halves add up to it.
	g, r, m := BroadSky(CameraLo, CameraHi)
	if math.Abs(g-1) > 1e-12 || math.Abs(r-1) > 1e-12 || math.Abs(m-1) > 1e-12 {
		t.Errorf("camera band: %v %v %v, want 1 1 1", g, r, m)
	}
	g1, r1, m1 := BroadSky(CameraLo, 550)
	g2, r2, m2 := BroadSky(550, CameraHi)
	if math.Abs(g1+g2-1) > 1e-12 || math.Abs(r1+r2-1) > 1e-12 || math.Abs(m1+m2-1) > 1e-12 {
		t.Errorf("halves: %v %v %v", g1+g2, r1+r2, m1+m2)
	}
	// The blue half holds more of the scattered moonlight than of the
	// glow, Rayleigh's λ⁻⁴ more than Mie's.
	if !(r1 > m1 && m1 > g1) {
		t.Errorf("blue half: glow %v, Mie %v, Rayleigh %v", g1, m1, r1)
	}
}

// Galactic reads Pioneer 10's tables: a grid cell is V between the blue and
// red values, Pioneer's gap is filled log-linearly along the row, the poles
// take the ±80° rows' mean, longitude wraps, and the reference is the sky's
// median by solid angle.
func TestGalactic(t *testing.T) {
	// Transcription spot checks (Leinert et al. 1998, Tables 35 and 36):
	// the Sagittarius peak, the LMC cell, and the gaps in the same cells of
	// both tables.
	if galacticBlue[0][11] != 569 || galacticRed[0][11] != 1306 || galacticBlue[28][5] != 213 || galacticRed[28][5] != 230 {
		t.Error("transcription spot checks")
	}
	gaps := 0
	for i := range galacticBlue {
		for j := range galacticBlue[i] {
			if (galacticBlue[i][j] == 0) != (galacticRed[i][j] == 0) {
				t.Errorf("gap at l %d, b %v in one table only", i*10, galacticB[j])
			}
			if galacticBlue[i][j] == 0 {
				gaps++
			}
		}
	}
	if gaps != 28 {
		t.Errorf("%d gaps, want 28", gaps)
	}
	// On the grid: V from blue and red.
	want := s10NL(math.Pow(569, 0.45) * math.Pow(1306, 0.55))
	if got := Galactic(0, -4); math.Abs(got-want) > 1e-9*want {
		t.Errorf("Galactic(0, -4) = %v, want %v", got, want)
	}
	// A gap (l 210°, b 30°) between its row's measured 16° and 60°.
	lo, hi := galacticV[21][16], galacticV[21][22]
	f := (30.0 - 16) / (60 - 16)
	if got := galacticV[21][19]; math.Abs(got-math.Exp(math.Log(lo)*(1-f)+math.Log(hi)*f)) > 1e-9 || !(got < lo && got > hi) {
		t.Errorf("gap fill %v between %v and %v", got, lo, hi)
	}
	// Poles: one value whatever the longitude; latitude clamps; longitude wraps.
	for _, l := range []float64{0, 90, 255} {
		if Galactic(l, 90) != s10NL(galacticPole[1]) || Galactic(l, -90) != s10NL(galacticPole[0]) || Galactic(l, 95) != Galactic(l, 90) {
			t.Errorf("poles at l %v", l)
		}
		if a, b := Galactic(l+360, 12), Galactic(l, 12); math.Abs(a-b) > 1e-9*b {
			t.Errorf("l %v + 360: %v, want %v", l, a, b)
		}
	}
	if a, b := Galactic(-5, 3), Galactic(355, 3); math.Abs(a-b) > 1e-9*b {
		t.Errorf("l -5: %v, want l 355's %v", a, b)
	}
	// The reference is the solid-angle median of the V table (the ±80° rows
	// standing for the caps).
	type cell struct{ v, w float64 }
	var cells []cell
	total := 0.0
	for i := range galacticV {
		for j, b := range galacticB {
			lo, hi := -90.0, 90.0
			if j > 0 {
				lo = (galacticB[j-1] + b) / 2
			}
			if j < len(galacticB)-1 {
				hi = (galacticB[j+1] + b) / 2
			}
			w := math.Sin(hi*math.Pi/180) - math.Sin(lo*math.Pi/180)
			cells = append(cells, cell{galacticV[i][j], w})
			total += w
		}
	}
	slices.SortFunc(cells, func(a, b cell) int { return cmp.Compare(a.v, b.v) })
	acc, median := 0.0, 0.0
	for _, c := range cells {
		if acc += c.w; acc >= total/2 {
			median = c.v

			break
		}
	}
	if math.Abs(median-galacticRefS10) > 1 {
		t.Errorf("sky median %v S10, reference %v", median, galacticRefS10)
	}
	// On a pristine sky: the galactic poles 0.3 mag/arcsec² darker, the
	// Sagittarius clouds 1.7 brighter; nothing below the minimum.
	sky := NanoLamberts(22)
	for _, c := range []struct{ l, b, want float64 }{{0, 90, 22.29}, {0, -4, 20.26}, {180, 0, 21.50}} {
		if got := MagFromNL(sky + GalacticExcess(c.l, c.b)); math.Abs(got-c.want) > 0.02 {
			t.Errorf("pristine sky at (%v, %v): %.2f, want %.2f", c.l, c.b, got, c.want)
		}
	}
	for _, d := range [][2]float64{{0, 0}, {1e300, -1e300}, {-725, 95}, {359.9999999, 7}} {
		if v := Galactic(d[0], d[1]); math.IsNaN(v) || v < GalacticMinNL*(1-1e-12) {
			t.Errorf("Galactic%v = %v, below the minimum %v", d, v, GalacticMinNL)
		}
	}
}

// Winkler's (2022) moonlight: the phase functions are normalised over the
// sphere; against K&S, whose beam is dimmed by the whole column before it
// scatters, the sky is brighter under a low Moon (Winkler's finding) and
// near the Moon (the aerosol forward peak), a little darker 90° away; the
// gradation function is continuous where the Moon and the target share an
// airmass.
func TestMoonScatter(t *testing.T) {
	m := Moon{NL: 1, X: 2, Fade: math.Exp(-tauPerMag * 0.2 * 2)}
	sum := func(ray float64) float64 {
		m.Ray = ray
		s := 0.0
		const n = 20000
		for i := range n {
			rho := (float64(i) + 0.5) * 180 / n
			r, a := m.scatter(0.2, 2, rho)
			s += (r + a) * 2 * math.Pi * math.Sin(rho*deg) * math.Pi / n
		}

		return s
	}
	gamma := 2 * tauPerMag * 0.2 * math.Exp(-tauPerMag*0.2*2)
	for _, ray := range []float64{1, 0} {
		if s := sum(ray) / gamma; math.Abs(s-1) > 1e-6 {
			t.Errorf("phase function (Rayleigh share %v) integrates to %v, want 1", ray, s)
		}
	}
	// Against K&S (1991, eqs. 15-21) for full Moon at extinction 0.172.
	ks91 := func(moonAlt, alt, rho float64) float64 {
		istar := math.Pow(10, -0.4*3.84) * 1.35 * math.Pow(10, -0.4*0.172*ks(moonAlt))
		c := math.Cos(rho * deg)

		return (math.Pow(10, 5.36)*(1.06+c*c) + math.Pow(10, 6.15-rho/40)) * istar * (1 - math.Pow(10, -0.4*0.172*ks(alt)))
	}
	ray, aer := SplitExtinction(0.172)
	rss := func(moonAlt, alt, rho float64) float64 {
		r, a := MoonLight(0.172, ray, aer, moonAlt, 0, meanMoonDist).scatter(0.172, ks(alt), rho)

		return r + a
	}
	for _, c := range []struct{ moonAlt, alt, rho, lo, hi float64 }{
		{5, 60, 80, 1.05, 1.3},  // low Moon: brighter
		{60, 60, 10, 1.5, 2.5},  // near the Moon: the forward peak
		{60, 60, 90, 0.8, 0.95}, // 90° away: a little darker
	} {
		if r := rss(c.moonAlt, c.alt, c.rho) / ks91(c.moonAlt, c.alt, c.rho); r < c.lo || r > c.hi {
			t.Errorf("%+v: Winkler/K&S %v, want %v..%v", c, r, c.lo, c.hi)
		}
	}
	// No extinction, no scattering.
	if r, a := MoonLight(0, 0, 0, 40, 0, meanMoonDist).scatter(0, 1.5, 50); r != 0 || a != 0 {
		t.Errorf("moonlight without extinction %v %v", r, a)
	}
	// Continuity across X = x.
	moon := MoonLight(0.172, ray, aer, 40, 0, meanMoonDist)
	x := moon.X
	at, _ := moon.scatter(0.172, x, 50)
	for _, d := range []float64{1e-7, -1e-7, 2e-6, -2e-6} {
		if near, _ := moon.scatter(0.172, x+d, 50); math.Abs(near/at-1) > 1e-5 {
			t.Errorf("gradation at X%+g: %v against %v at X", d, near, at)
		}
	}
}
