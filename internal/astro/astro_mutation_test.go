// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"testing"
	"time"
)

const (
	mutZgLat = 45.81
	mutZgLon = 15.98
)

func mutLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skip(err)
	}

	return loc
}

func mutAngle(a, b float64) float64 { return math.Abs(math.Remainder(a-b, 360)) }

func TestMutDaysAndSidereal(t *testing.T) {
	if d := DaysJ2000(time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)); d != 0 {
		t.Errorf("DaysJ2000(J2000.0) = %v", d)
	}
	// GMST at J2000.0 (2000-01-01 12h UT) is 18.697374558h (IAU 1982).
	want := 18.697374558 * 15
	if g := LST(time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC), 0); mutAngle(g, want) > 0.0001 {
		t.Errorf("GMST = %v, want %v", math.Mod(g, 360), want)
	}
	// A sidereal day is 23h56m04.09s: after one, GMST is back.
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(23*time.Hour + 56*time.Minute + 4090*time.Millisecond)
	if d := mutAngle(LST(t1, 0), LST(t0, 0)); d > 0.001 {
		t.Errorf("sidereal day drift %v°", d)
	}
	// East longitude adds.
	if d := LST(t0, 15) - LST(t0, 0); math.Abs(d-15) > 1e-9 {
		t.Errorf("LST(15E) - GMST = %v", d)
	}
}

// Equinox and solstice instants of 2026 (USNO): 20 Mar 14:46 and 21 Jun 08:24 UTC.
func TestMutSun(t *testing.T) {
	ra, dec := SunRADec(time.Date(2026, 3, 20, 14, 46, 0, 0, time.UTC))
	if mutAngle(ra, 0) > 0.05 || math.Abs(dec) > 0.05 {
		t.Errorf("equinox Sun %v %v", ra, dec)
	}
	ra, dec = SunRADec(time.Date(2026, 6, 21, 8, 24, 0, 0, time.UTC))
	if mutAngle(ra, 90) > 0.05 || math.Abs(dec-23.436) > 0.02 {
		t.Errorf("solstice Sun %v %v", ra, dec)
	}
	ra, dec = SunRADec(time.Date(2026, 12, 21, 20, 50, 0, 0, time.UTC))
	if mutAngle(ra, 270) > 0.05 || math.Abs(dec+23.436) > 0.02 {
		t.Errorf("December solstice Sun %v %v", ra, dec)
	}
}

// Total lunar eclipse 2025-09-07 18:12 UTC: the Moon opposite the Sun.
// Total solar eclipse 2026-08-12 17:46 UTC: new Moon on the Sun.
func TestMutMoonPhase(t *testing.T) {
	full := time.Date(2025, 9, 7, 18, 12, 0, 0, time.UTC)
	if a := MoonPhaseAngle(full); a > 1.5 {
		t.Errorf("phase angle at lunar eclipse = %v", a)
	}
	if f := MoonIllumination(full); f < 0.999 {
		t.Errorf("illumination at lunar eclipse = %v", f)
	}
	sra, sdec := SunRADec(full)
	mra, mdec := MoonRADec(full)
	if mutAngle(mra, sra+180) > 1.5 || math.Abs(mdec+sdec) > 1.5 {
		t.Errorf("eclipsed Moon %v %v, Sun %v %v", mra, mdec, sra, sdec)
	}
	newm := time.Date(2026, 8, 12, 17, 46, 0, 0, time.UTC)
	if a := MoonPhaseAngle(newm); a < 178.5 {
		t.Errorf("phase angle at solar eclipse = %v", a)
	}
	if f := MoonIllumination(newm); f > 0.001 {
		t.Errorf("illumination at solar eclipse = %v", f)
	}
	// First quarter 2026-01-26 04:47 UTC: half lit.
	if f := MoonIllumination(time.Date(2026, 1, 26, 4, 47, 0, 0, time.UTC)); math.Abs(f-0.5) > 0.03 {
		t.Errorf("illumination at first quarter = %v", f)
	}
	// The Moon moves east ~13°/day.
	r0, _ := MoonRADec(newm)
	r1, _ := MoonRADec(newm.Add(24 * time.Hour))
	if d := math.Remainder(r1-r0, 360); d < 11 || d > 16 {
		t.Errorf("Moon daily motion %v°", d)
	}
}

// Perigee 55.9-57.5 and apogee 63.2-63.8 Earth radii; mean 60.27.
func TestMutMoonDistance(t *testing.T) {
	lo, hi, sum := 100.0, 0.0, 0.0
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	for h := 0; h < 24*365; h += 3 {
		d := MoonDistance(start.Add(time.Duration(h) * time.Hour))
		lo, hi, sum = min(lo, d), max(hi, d), sum+d
		n++
	}
	if lo < 55.5 || lo > 57 || hi < 63 || hi > 64.2 || math.Abs(sum/float64(n)-60.3) > 0.3 {
		t.Errorf("Moon distance range %v-%v mean %v", lo, hi, sum/float64(n))
	}
}

// Parallax lowers the Moon by asin(sin P cos alt) and leaves it near the
// zenith in place.
func TestMutMoonTopo(t *testing.T) {
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	checked := 0
	for h := range 24 * 30 {
		tm := base.Add(time.Duration(h) * time.Hour)
		gra, gdec := MoonRADec(tm)
		tra, tdec := MoonTopo(tm, mutZgLat, mutZgLon)
		ga := Altitude(gra, gdec, tm, mutZgLat, mutZgLon)
		ta := Altitude(tra, tdec, tm, mutZgLat, mutZgLon)
		if ga < 5 {
			continue
		}
		p := math.Asin(1/MoonDistance(tm)) / deg
		want := math.Asin(math.Sin(p*deg)*math.Cos(ga*deg)) / deg
		if d := (ga - ta) - want; math.Abs(d) > 0.01 {
			t.Fatalf("%v: parallax %v°, want %v°", tm, ga-ta, want)
		}
		checked++
	}
	if checked < 100 {
		t.Fatalf("only %d samples", checked)
	}
}

func TestMutAltAz(t *testing.T) {
	tm := time.Date(2026, 1, 15, 21, 0, 0, 0, time.UTC)
	lst := LST(tm, mutZgLon)
	// On the meridian: alt = 90 − |lat − dec|, south of the zenith az 180.
	for _, dec := range []float64{-20, 0, 30} {
		alt, az := AltAz(lst, dec, tm, mutZgLat, mutZgLon)
		if math.Abs(alt-(90-math.Abs(mutZgLat-dec))) > 1e-6 || mutAngle(az, 180) > 1e-6 {
			t.Errorf("meridian dec %v: %v %v", dec, alt, az)
		}
	}
	// Celestial pole at altitude = latitude, due north.
	if alt, az := AltAz(123, 90, tm, mutZgLat, mutZgLon); math.Abs(alt-mutZgLat) > 1e-6 || mutAngle(az, 0) > 1e-3 {
		t.Errorf("pole %v %v", alt, az)
	}
	// Equator six hours east of the meridian: rising due east.
	alt, az := AltAz(lst+90, 0, tm, mutZgLat, mutZgLon)
	if math.Abs(alt) > 1e-6 || mutAngle(az, 90) > 1e-6 {
		t.Errorf("rising %v %v", alt, az)
	}
	alt, az = AltAz(lst-90, 0, tm, mutZgLat, mutZgLon)
	if math.Abs(alt) > 1e-6 || mutAngle(az, 270) > 1e-6 {
		t.Errorf("setting %v %v", alt, az)
	}
	// SinAlt is clamped at the zenith.
	hit := false
	for i := range 2000 {
		d := float64(i) * 0.0451
		h := NewHorizontal(d, d)
		raw := math.Sin(d*deg)*math.Sin(d*deg) + math.Cos(d*deg)*math.Cos(d*deg)
		if raw > 1 {
			hit = true
		}
		if s := h.SinAlt(0); s > 1 || math.IsNaN(math.Asin(s)) {
			t.Fatalf("SinAlt at zenith %v = %v", d, s)
		}
	}
	if !hit {
		t.Log("no rounding past 1 found")
	}
	// WithDec keeps the site.
	h := NewHorizontal(mutZgLat, 10).WithDec(-30)
	if s := h.SinAlt(0); math.Abs(math.Asin(s)/deg-(90-(mutZgLat+30))) > 1e-9 {
		t.Errorf("WithDec altitude %v", math.Asin(s)/deg)
	}
}

func TestMutSeparationUnit(t *testing.T) {
	for _, c := range [][5]float64{{0, 0, 90, 0, 90}, {10, 89, 190, 89, 2}, {350, 0, 10, 0, 20}, {0, -30, 0, 60, 90}, {45, 10, 225, -10, 180}} {
		if s := Separation(c[0], c[1], c[2], c[3]); math.Abs(s-c[4]) > 1e-6 {
			t.Errorf("Separation%v = %v", c[:4], s)
		}
		u, v := Unit(c[0], c[1]), Unit(c[2], c[3])
		dot := u[0]*v[0] + u[1]*v[1] + u[2]*v[2]
		if math.Abs(dot-math.Cos(c[4]*deg)) > 1e-9 {
			t.Errorf("Unit dot %v = %v", c[:4], dot)
		}
	}
	if u := Unit(90, 0); math.Abs(u[1]-1) > 1e-12 {
		t.Errorf("Unit(90,0) = %v", u)
	}
}

// Annual precession at (0h, 0°): m = 3.075 s = 46.12″ in RA, n = 20.04″ in Dec.
func TestMutPrecess(t *testing.T) {
	ra, dec := PrecessT(0, 0, 0.5)
	if math.Abs(ra-46.124*50/3600) > 0.003 || math.Abs(dec-20.043*50/3600) > 0.003 {
		t.Errorf("PrecessT(0,0,0.5) = %v %v", ra, dec)
	}
	ra, dec = PrecessT(180, 0, 0.5)
	if math.Abs(ra-180-46.124*50/3600) > 0.003 || math.Abs(dec+20.043*50/3600) > 0.003 {
		t.Errorf("PrecessT(180,0,0.5) = %v %v", ra, dec)
	}
	// Round trip.
	r2, d2 := PrecessT(ra, dec, -0.5)
	if mutAngle(r2, 180) > 0.001 || math.Abs(d2) > 0.001 {
		t.Errorf("round trip %v %v", r2, d2)
	}
	// RA stays in [0, 360) and the pole never NaN.
	if r, _ := PrecessT(359.99, 0, 0.5); r < 0 || r >= 360 {
		t.Errorf("RA wrap %v", r)
	}
	for i := range 3600 {
		if _, d := PrecessT(float64(i)/10, 90, 0.26); math.IsNaN(d) {
			t.Fatalf("pole NaN at ra %v", float64(i)/10)
		}
	}
	// The J2000 point that lands on the pole of date: c is 1 up to rounding.
	for i := range 20000 {
		tc := -2 + float64(i)*0.0002
		zeta := (2306.2181*tc + 0.30188*tc*tc + 0.017998*tc*tc*tc) / 3600
		theta := (2004.3109*tc - 0.42665*tc*tc - 0.041833*tc*tc*tc) / 3600
		if _, d := PrecessT(math.Mod(720-zeta, 360), 90-theta, tc); math.IsNaN(d) {
			t.Fatalf("precessed pole NaN at t %v", tc)
		}
	}
	r1, d1 := Precess(10, 20, time.Date(2050, 1, 1, 12, 0, 0, 0, time.UTC))
	r3, d3 := PrecessT(10, 20, DaysJ2000(time.Date(2050, 1, 1, 12, 0, 0, 0, time.UTC))/36525)
	if r1 != r3 || d1 != d3 || r1 == 10 {
		t.Errorf("Precess %v %v vs %v %v", r1, d1, r3, d3)
	}
}

func TestMutSexagesimal(t *testing.T) {
	for in, want := range map[string]float64{
		"12 30 00":   12.5,
		"-00 30 00":  -0.5,
		"+45 15":     45.25,
		" 05 34 31 ": 5 + 34.0/60 + 31.0/3600,
		"10 59.5":    10 + 59.5/60,
		"7":          7,
	} {
		if got, err := Sexagesimal(in); err != nil || math.Abs(got-want) > 1e-12 {
			t.Errorf("Sexagesimal(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "+-10", "--5", "1 2 3 4", "10 60", "10 30 60", "-1 -2", "+ -00 30", "NaN", "10 NaN", "Inf", "10 Inf", "1e400", "x 10"} {
		if got, err := Sexagesimal(in); err == nil {
			t.Errorf("Sexagesimal(%q) = %v, want error", in, got)
		}
	}
}

// Sun of date at the 2026 March equinox is at longitude 0; the J2000 frame
// is 50.29″/yr × 26.2 yr = 0.366° behind, so the Earth is at 179.63°.
func TestMutEarthHelio(t *testing.T) {
	x, y := EarthHelio(time.Date(2026, 3, 20, 14, 46, 0, 0, time.UTC))
	if l := math.Atan2(y, x) / deg; mutAngle(l, 179.63) > 0.03 {
		t.Errorf("Earth longitude %v", l)
	}
	// Perihelion 2026-01-03 (0.98330 AU), aphelion 2026-07-06 (1.01666 AU).
	x, y = EarthHelio(time.Date(2026, 1, 3, 17, 0, 0, 0, time.UTC))
	if r := math.Hypot(x, y); math.Abs(r-0.9833) > 0.0003 {
		t.Errorf("perihelion r %v", r)
	}
	x, y = EarthHelio(time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC))
	if r := math.Hypot(x, y); math.Abs(r-1.0167) > 0.0003 {
		t.Errorf("aphelion r %v", r)
	}
}

// Hand computation (Sun at ±23.44°, Zagreb 45.81° N): cos H = (sin −18° −
// sin φ sin δ)/(cos φ cos δ) gives a December night of 11h43m and a June
// one of 2h54m, centred on solar midnight ≈ 22:56 UTC.
func TestMutWindowZagreb(t *testing.T) {
	zg := mutLoc(t, "Europe/Zagreb")
	for _, c := range []struct {
		day  time.Time
		dur  time.Duration
		alt  float64
		name string
	}{
		{time.Date(2026, 12, 21, 0, 0, 0, 0, zg), 11*time.Hour + 43*time.Minute, Astronomical, "december"},
		{time.Date(2026, 6, 21, 0, 0, 0, 0, zg), 2*time.Hour + 54*time.Minute, Astronomical, "june"},
	} {
		dusk, dawn, ok := Window(c.day, mutZgLat, mutZgLon, c.alt)
		if !ok {
			t.Fatalf("%s: no night", c.name)
		}
		if d := dawn.Sub(dusk) - c.dur; d.Abs() > 6*time.Minute {
			t.Errorf("%s: night %v, want %v", c.name, dawn.Sub(dusk), c.dur)
		}
		mid := dusk.Add(dawn.Sub(dusk) / 2).UTC()
		want := time.Date(2026, c.day.Month(), c.day.Day(), 22, 56, 0, 0, time.UTC)
		if d := mid.Sub(want); d.Abs() > 6*time.Minute {
			t.Errorf("%s: midnight %v, want %v", c.name, mid, want)
		}
		if dusk.In(zg).Day() != c.day.Day() {
			t.Errorf("%s: dusk %v not on the evening of %v", c.name, dusk.In(zg), c.day)
		}
	}
	// Nautical dark at the June solstice: cos H = (sin −12° − 0.28516)/0.63964
	// gives H = 140.43°, a 5h17m night.
	d0, d1, ok := Window(time.Date(2026, 6, 21, 0, 0, 0, 0, zg), mutZgLat, mutZgLon, Nautical)
	if !ok || (d1.Sub(d0)-(5*time.Hour+17*time.Minute)).Abs() > 6*time.Minute {
		t.Errorf("nautical night %v-%v", d0, d1)
	}
	// 65° N in June: no astronomical night at all.
	if _, _, ok := Window(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 65, 25, Astronomical); ok {
		t.Error("65 N in June has an astronomical night")
	}
	// 88° N in December (noon Sun at −21.4°): polar night, the whole 24 hours.
	s, e, ok := Window(time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC), 88, 15, Astronomical)
	if !ok || e.Sub(s) != 24*time.Hour {
		t.Errorf("polar night %v %v %v", s, e, ok)
	}
}

// East of Greenwich far enough that a wrong-signed solar noon starts the
// search after dark, and Samoa, whose zone is a day ahead of its longitude.
func TestMutWindowZones(t *testing.T) {
	for _, c := range []struct {
		zone     string
		lat, lon float64
	}{{"Asia/Kolkata", 22.57, 88.36}, {"Asia/Kolkata", 19.08, 72.88}, {"Pacific/Apia", -13.83, -171.76}, {"Pacific/Honolulu", 21.31, -157.86}, {"America/New_York", 40.71, -74.01}} {
		loc := mutLoc(t, c.zone)
		day := time.Date(2026, 3, 10, 0, 0, 0, 0, loc)
		dusk, dawn, ok := Window(day, c.lat, c.lon, Astronomical)
		if !ok {
			t.Fatalf("%s: no night", c.zone)
		}
		l := dusk.In(loc)
		if l.Day() != 10 || l.Hour() < 18 || l.Hour() > 21 {
			t.Errorf("%s: dusk %v, want the evening of the 10th", c.zone, l)
		}
		if d := dawn.Sub(dusk); d < 8*time.Hour || d > 11*time.Hour {
			t.Errorf("%s: night %v", c.zone, d)
		}
	}
}

func TestMutTonight(t *testing.T) {
	zg := mutLoc(t, "Europe/Zagreb")
	// 01:00 on the 16th is still the night of the 15th, in progress.
	day, in := Tonight(time.Date(2026, 1, 16, 1, 0, 0, 0, zg), mutZgLat, mutZgLon, Astronomical)
	if day.Day() != 15 || !in {
		t.Errorf("01:00: %v %v, want 15 true", day, in)
	}
	// 14:00: tonight is today's, not begun.
	day, in = Tonight(time.Date(2026, 1, 16, 14, 0, 0, 0, zg), mutZgLat, mutZgLon, Astronomical)
	if day.Day() != 16 || in {
		t.Errorf("14:00: %v %v, want 16 false", day, in)
	}
	// 21:00: today's, in progress.
	day, in = Tonight(time.Date(2026, 1, 16, 21, 0, 0, 0, zg), mutZgLat, mutZgLon, Astronomical)
	if day.Day() != 16 || !in {
		t.Errorf("21:00: %v %v, want 16 true", day, in)
	}
}

func TestMutClipWindow(t *testing.T) {
	zg := mutLoc(t, "Europe/Zagreb")
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, zg)
	start := time.Date(2026, 1, 15, 18, 0, 0, 0, zg)
	end := time.Date(2026, 1, 16, 5, 30, 0, 0, zg)
	s, e, err := ClipWindow(day, zg, start, end, "22:00", "02:00")
	if err != nil || !s.Equal(time.Date(2026, 1, 15, 22, 0, 0, 0, zg)) || !e.Equal(time.Date(2026, 1, 16, 2, 0, 0, 0, zg)) {
		t.Errorf("22-02: %v %v %v", s, e, err)
	}
	// Never extends.
	s, e, err = ClipWindow(day, zg, start, end, "17:00", "06:00")
	if err != nil || !s.Equal(start) || !e.Equal(end) {
		t.Errorf("wider: %v %v %v", s, e, err)
	}
	// 12:30 is the same afternoon, 11:59 the next morning.
	s, _, err = ClipWindow(day, zg, time.Date(2026, 1, 15, 12, 0, 0, 0, zg), end, "12:30", "")
	if err != nil || !s.Equal(time.Date(2026, 1, 15, 12, 30, 0, 0, zg)) {
		t.Errorf("12:30: %v %v", s, err)
	}
	_, e, err = ClipWindow(day, zg, start, time.Date(2026, 1, 16, 13, 0, 0, 0, zg), "", "11:59")
	if err != nil || !e.Equal(time.Date(2026, 1, 16, 11, 59, 0, 0, zg)) {
		t.Errorf("11:59: %v %v", e, err)
	}
	for _, c := range [][2]string{{"03:00", "02:00"}, {"02:00", "02:00"}, {"25:00", ""}, {"", "x"}, {"06:00", ""}} {
		if _, _, err := ClipWindow(day, zg, start, end, c[0], c[1]); err == nil {
			t.Errorf("ClipWindow(%q, %q) accepted", c[0], c[1])
		}
	}
	// Spring forward 2026-03-29: 02:30 does not exist in Zagreb.
	sp := time.Date(2026, 3, 28, 0, 0, 0, 0, zg)
	if _, _, err := ClipWindow(sp, zg, time.Date(2026, 3, 28, 20, 0, 0, 0, zg), time.Date(2026, 3, 29, 5, 0, 0, 0, zg), "02:30", ""); err == nil {
		t.Error("skipped clock accepted")
	}
	// Fall back 2026-10-25: 02:30 happens at 00:30 and 01:30 UTC; from takes
	// the first, to the second.
	fb := time.Date(2026, 10, 24, 0, 0, 0, 0, zg)
	fs, fe := time.Date(2026, 10, 24, 20, 0, 0, 0, zg), time.Date(2026, 10, 25, 5, 0, 0, 0, zg)
	s, _, err = ClipWindow(fb, zg, fs, fe, "02:30", "")
	if err != nil || !s.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Errorf("fall-back from: %v %v", s.UTC(), err)
	}
	_, e, err = ClipWindow(fb, zg, fs, fe, "", "02:30")
	if err != nil || !e.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Errorf("fall-back to: %v %v", e.UTC(), err)
	}
}

// Dusk is a crossing: the minute before it is light (unless polar night), so
// a night already running at noon (the previous one's tail) is never
// returned as tonight. 88° N in late January: the noon Sun hovers at -18°.
func TestMutWindowDuskCrossing(t *testing.T) {
	for _, lat := range []float64{80, 84, 86, 87, 88, 89} {
		for d := range 60 {
			day := time.Date(2026, 1, 1+d, 0, 0, 0, 0, time.UTC)
			dusk, dawn, ok := Window(day, lat, 0, Astronomical)
			if !ok || dawn.Sub(dusk) == 24*time.Hour {
				continue
			}
			before := dusk.Add(-time.Minute)
			ra, dec := SunRADec(before)
			if Altitude(ra, dec, before, lat, 0) < Astronomical {
				t.Fatalf("lat %v %v: dusk %v is no crossing", lat, day.Format(time.DateOnly), dusk)
			}
		}
	}
}

// Window works on whole minutes from a whole-minute noon.
func TestMutWindowMinutes(t *testing.T) {
	dusk, dawn, ok := Window(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), mutZgLat, mutZgLon, Astronomical)
	if !ok || dusk.Second() != 0 || dusk.Nanosecond() != 0 || dawn.Second() != 0 {
		t.Errorf("Window %v %v not on whole minutes", dusk, dawn)
	}
}
