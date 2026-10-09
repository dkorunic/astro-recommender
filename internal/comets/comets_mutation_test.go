// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package comets

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
)

func mutAng(a, b float64) float64 { return math.Abs(math.Remainder(a-b, 360)) }

// The conic: r = q(1+e)/(1+e cos ν), and the time equations solved for ν.
func TestMutAnomaly(t *testing.T) {
	for _, c := range []struct{ q, e float64 }{{1, 0}, {0.5, 0.3}, {0.6, 0.85}, {0.3, 0.97}, {0.1, 0.999}, {0.05, 0.9999}, {1.2, 1}, {1, 1.001}, {0.5, 1.01}, {0.9, 1.3}, {2, 3}} {
		cm := comet{q: c.q, e: c.e}
		for _, dt := range []float64{-400, -30, -1, -0.01, 0, 0.003, 0.5, 2, 10, 77, 365, 2000, 1e5} {
			nu, r := cm.anomaly(dt)
			if math.IsNaN(nu) || math.IsNaN(r) {
				t.Fatalf("q %v e %v dt %v: NaN", c.q, c.e, dt)
			}
			if want := c.q * (1 + c.e) / (1 + c.e*math.Cos(nu)); math.Abs(r-want) > 1e-9*want {
				t.Errorf("q %v e %v dt %v: r %v, conic %v", c.q, c.e, dt, r, want)
			}
			// Mean motion against the time since perihelion.
			var lhs, rhs float64
			switch {
			case c.e == 1:
				s := math.Tan(nu / 2)
				lhs, rhs = s*s*s+3*s, 3*gaussK/math.Sqrt(2*c.q*c.q*c.q)*dt
			case c.e < 1:
				a := c.q / (1 - c.e)
				E := 2 * math.Atan(math.Sqrt((1-c.e)/(1+c.e))*math.Tan(nu/2))
				lhs, rhs = E-c.e*math.Sin(E), math.Remainder(gaussK/math.Pow(a, 1.5)*dt, 2*math.Pi)
			default:
				a := c.q / (c.e - 1)
				H := 2 * math.Atanh(math.Sqrt((c.e-1)/(c.e+1))*math.Tan(nu/2))
				lhs, rhs = c.e*math.Sinh(H)-H, gaussK/math.Pow(a, 1.5)*dt
			}
			if math.Abs(math.Remainder(lhs-rhs, 2*math.Pi)) > 1e-7 {
				t.Errorf("q %v e %v dt %v: time equation %v vs %v", c.q, c.e, dt, lhs, rhs)
			}
			if math.Abs(dt) <= 77 && (dt > 0 && nu <= 0 || dt < 0 && nu >= 0) {
				t.Errorf("q %v e %v dt %v: ν %v moves the wrong way", c.q, c.e, dt, nu)
			}
		}
	}
	// A circular 1 AU orbit takes a sidereal year (365.2569 d): a quarter
	// of it is 90°.
	if nu, r := (&comet{q: 1, e: 0}).anomaly(365.2569 / 4); math.Abs(nu-math.Pi/2) > 1e-4 || math.Abs(r-1) > 1e-12 {
		t.Errorf("quarter year: ν %v r %v", nu, r)
	}
	// e = 0.5, a = 1: aphelion 1.5 AU after half a year.
	if nu, r := (&comet{q: 0.5, e: 0.5}).anomaly(365.2569 / 2); math.Abs(math.Abs(nu)-math.Pi) > 1e-4 || math.Abs(r-1.5) > 1e-6 {
		t.Errorf("half year: ν %v r %v", nu, r)
	}
}

// Far away (q = 10⁶ AU, at perihelion) a comet is seen in the direction of
// its heliocentric position: ecliptic longitude 0 is RA 0 Dec 0, longitude
// 90° is RA 90° Dec +ε, the ecliptic north pole RA 270° Dec 90° − ε.
func TestMutPosition(t *testing.T) {
	const eps = 23.4392911
	tp := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name             string
		peri, node, incl float64
		ra, dec          float64
	}{
		{"lon 0", 0, 0, 0, 0, 0},
		{"lon 90 by peri", 90, 0, 0, 90, eps},
		{"lon 90 by node", 0, 90, 0, 90, eps},
		{"lon 180", 0, 180, 0, 180, 0},
		{"north pole", 90, 0, 90, 270, 90 - eps},
		{"south pole", 270, 0, 90, 90, -(90 - eps)},
		{"inclined node", 0, 90, 60, 90, eps},
	} {
		cm := comet{perihelion: tp, q: 1e6, e: 0, peri: c.peri * deg, node: c.node * deg, incl: c.incl * deg}
		ra, dec, r, delta := cm.position(tp)
		if mutAng(ra, c.ra) > 0.001 || math.Abs(dec-c.dec) > 0.001 || math.Abs(r-1e6) > 1e-5 || math.Abs(delta-1e6) > 2 {
			t.Errorf("%s: %v %v r %v Δ %v, want %v %v", c.name, ra, dec, r, delta, c.ra, c.dec)
		}
	}
	// A quarter of a sidereal year after perihelion on a circular 1 AU orbit
	// (prograde, in the ecliptic) the comet is 90° on, at (0, 1, 0).
	tq := tp.Add(time.Duration(365.2569 / 4 * 24 * float64(time.Hour)))
	cq := comet{perihelion: tp, q: 1, e: 0}
	ra, dec, _, _ := cq.position(tq)
	qx, qy := astro.EarthHelio(tq)
	vx, vy := -qx, 1-qy
	se, ce := math.Sincos(eps * deg)
	wra := math.Mod(math.Atan2(vy*ce, vx)/deg+360, 360)
	wdec := math.Asin(vy*se/math.Hypot(vx, vy)) / deg
	if mutAng(ra, wra) > 0.01 || math.Abs(dec-wdec) > 0.01 {
		t.Errorf("quarter orbit: %v %v, want %v %v", ra, dec, wra, wdec)
	}
	// Δ is the distance from the Earth, not the Sun: a comet on the Earth's
	// heliocentric direction at 2 AU is 2 − R⊕ away.
	ex, ey := astro.EarthHelio(tp)
	lon := math.Atan2(ey, ex)
	cm := comet{perihelion: tp, q: 2, e: 0, node: lon}
	_, _, r, delta := cm.position(tp)
	if math.Abs(r-2) > 1e-9 || math.Abs(delta-(2-math.Hypot(ex, ey))) > 1e-9 {
		t.Errorf("Δ %v r %v", delta, r)
	}
}

// mutLine lays MPC CometEls.txt fields into their fixed columns.
func mutLine(year, month, day, q, e, peri, node, incl, h, k, name string) string {
	b := []byte(strings.Repeat(" ", 102))
	put := func(end int, s string) { copy(b[end-len(s):end], s) }
	put(18, year)
	put(21, month)
	put(29, day)
	put(39, q)
	put(49, e)
	put(59, peri)
	put(69, node)
	put(79, incl)
	put(95, h)
	put(100, k)

	return string(b) + name
}

func TestMutParseComets(t *testing.T) {
	good := mutLine("2026", "04", "15.5000", "0.781234", "0.995000", "152.5000", "255.3000", "139.1000", "6.5", "4.0", "C/2025 A1 (Test)")
	el, err := parseComets([]byte(good + "\n"))
	if err != nil || len(el) != 1 {
		t.Fatalf("parse: %v %v", el, err)
	}
	c := el[0]
	if !c.perihelion.Equal(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)) || c.q != 0.781234 || c.e != 0.995 ||
		math.Abs(c.peri-152.5*deg) > 1e-12 || math.Abs(c.node-255.3*deg) > 1e-12 || math.Abs(c.incl-139.1*deg) > 1e-12 ||
		c.h != 6.5 || c.k != 4 || c.name != "C/2025 A1 (Test)" {
		t.Errorf("parsed %+v", c)
	}
	for _, bad := range []string{
		mutLine("2026", "13", "15.5", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", "bad month"),
		mutLine("2026", "00", "15.5", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", "month 0"),
		mutLine("2026", "04", "32.0", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", "day 32"),
		mutLine("2026", "04", "0.5", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", "day 0.5"),
		mutLine("2026", "04", "15.5", "0", "0.9", "1", "2", "3", "6.5", "4.0", "q 0"),
		mutLine("2026", "04", "15.5", "0.7", "-0.1", "1", "2", "3", "6.5", "4.0", "e<0"),
		mutLine("2026", "04", "15.5", "NaN", "0.9", "1", "2", "3", "6.5", "4.0", "q NaN"),
		mutLine("2026", "04", "15.5", "0.7", "0.9", "Inf", "2", "3", "6.5", "4.0", "peri Inf"),
		mutLine("2026", "04", "15.5", "0.7", "0.9", "1", "2", "3", "", "4.0", "no H"),
		mutLine("2026", "04", "15.5", "0.7", "0.9", "1", "2", "3", "6.5", "", "no K"),
		mutLine("2026", "04", "15.5", "0.7", "0.9", "1", "2", "", "6.5", "4.0", "no incl"),
		good[:101],
		good[:102],
	} {
		if el, err := parseComets([]byte(bad + "\n" + good + "\n")); err != nil || len(el) != 1 {
			t.Errorf("%q: %d parsed, %v", bad, len(el), err)
		}
	}
	if el, err := parseComets([]byte(mutLine("2026", "04", "31.9", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", "d31.9") + "\n" + good)); err != nil || len(el) != 2 {
		t.Errorf("day 31.9 rejected: %v", err)
	}
	if _, err := parseComets([]byte("short\n")); err == nil {
		t.Error("no elements accepted")
	}
	// The name stops at column 158.
	long := mutLine("2026", "04", "15.5", "0.7", "0.9", "1", "2", "3", "6.5", "4.0", strings.Repeat("n", 56)+"EXTRA")
	if el, _ := parseComets([]byte(long)); len(el) != 1 || len(el[0].name) != 56 {
		t.Errorf("long name %q", el[0].name)
	}
}

func TestMutTargets(t *testing.T) {
	start := time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC)
	grid := make([]time.Time, 241)
	for i := range grid {
		grid[i] = start.Add(time.Duration(i) * time.Minute)
	}
	// Brightness is judged at the middle of the grid: a fast comet 20 days
	// past perihelion fades over a 10-day grid.
	long := make([]time.Time, 241)
	for i := range long {
		long[i] = start.Add(time.Duration(i) * time.Hour)
	}
	fast := Elements{{name: "C/2026 F1", perihelion: long[120].Add(-20 * 24 * time.Hour), q: 0.3, e: 1, peri: 30 * deg, node: 40 * deg, incl: 50 * deg, h: 8, k: 4}}
	_, _, fr, fd := fast[0].position(long[120])
	fm := 8 + 5*math.Log10(fd) + 10*math.Log10(fr)
	if n := len(fast.Targets(long, fm+0.005)); n != 1 {
		t.Errorf("fast comet at m+0.005: %d", n)
	}
	if n := len(fast.Targets(long, fm-0.005)); n != 0 {
		t.Errorf("fast comet at m-0.005: %d", n)
	}
	mid := grid[120]
	// At perihelion straight above the ecliptic pole, 3 AU from the Sun:
	// r = 3, Δ = √(R⊕² + 9); H 5, K 4: m = 5 + 5 log Δ + 10 log 3.
	ex, ey := astro.EarthHelio(mid)
	delta := math.Sqrt(ex*ex + ey*ey + 9)
	m := 5 + 5*math.Log10(delta) + 10*math.Log10(3)
	el := Elements{
		{name: "C/2026 Z9 (Mutant)", perihelion: mid, q: 3, e: 1, peri: 90 * deg, incl: 90 * deg, h: 5, k: 4},
		{name: "12P/Pons-Brooks", perihelion: mid, q: 3, e: 1, peri: 90 * deg, incl: 90 * deg, h: 5.5, k: 4},
	}
	out := el.Targets(grid, m+0.01)
	if len(out) != 1 {
		t.Fatalf("Targets(m+0.01) = %d", len(out))
	}
	tg := out[0]
	if tg.Name != "C/2026 Z9" || !strings.HasPrefix(tg.Description, "Mutant mag ") || tg.Type != "Comet" || tg.Constellation == "" {
		t.Errorf("target %q %q %q %q", tg.Name, tg.Description, tg.Type, tg.Constellation)
	}
	if !strings.Contains(tg.Description, " r 3.00 AU") {
		t.Errorf("description %q", tg.Description)
	}
	if len(tg.Track) != len(grid) {
		t.Fatalf("track %d", len(tg.Track))
	}
	for _, i := range []int{0, 120, 240} {
		ra, dec, _, _ := el[0].position(grid[i])
		wr, wd := astro.Precess(ra, dec, grid[i])
		if tg.Track[i][0] != wr || tg.Track[i][1] != wd {
			t.Errorf("track[%d] = %v, want %v %v", i, tg.Track[i], wr, wd)
		}
	}
	if out := el.Targets(grid, m-0.01); len(out) != 0 {
		t.Errorf("Targets(m-0.01) = %d", len(out))
	}
	out = el.Targets(grid, m+1)
	if len(out) != 2 || out[1].Name != "12P/Pons-Brooks" || !strings.HasPrefix(out[1].Description, "mag ") {
		t.Errorf("periodic: %+v", out)
	}
}
