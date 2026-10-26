// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"testing"
	"time"
)

// TestLunarEclipse checks the phases against Astronomy Engine's
// SearchLunarEclipse (v2.1.19, Python): each phase holds from its peak to
// 2 minutes inside its semi-duration and has ended 2 minutes past it, the
// margin for our Almanac Sun against its VSOP87 one.
func TestLunarEclipse(t *testing.T) {
	const margin = 2 * time.Minute
	for _, c := range []struct {
		peak                      string
		kind                      EclipsePhase
		penumbral, partial, total float64 // semi-durations, minutes
	}{
		{"2025-03-14T06:58:42Z", Total, 181.66, 109.48, 33.25},
		{"2025-09-07T18:11:42Z", Total, 163.65, 105.02, 41.44},
		{"2026-03-03T11:33:40Z", Total, 169.64, 103.92, 29.71},
		{"2026-08-28T04:12:49Z", Partial, 169.22, 99.42, 0},
		{"2027-02-20T23:12:44Z", Penumbral, 120.87, 0, 0},
		{"2027-07-18T16:02:55Z", Penumbral, 12.69, 0, 0},
		{"2028-01-12T04:13:01Z", Partial, 125.73, 29.13, 0},
	} {
		peak, err := time.Parse(time.RFC3339, c.peak)
		if err != nil {
			t.Fatal(err)
		}
		if got := LunarEclipse(peak).Phase; got != c.kind {
			t.Errorf("%s: phase %d at the peak, want %d", c.peak, got, c.kind)
		}
		for phase, sd := range map[EclipsePhase]float64{Penumbral: c.penumbral, Partial: c.partial, Total: c.total} {
			if sd == 0 {
				continue
			}
			d := time.Duration(sd * float64(time.Minute))
			for _, sign := range []time.Duration{-1, 1} {
				if in := LunarEclipse(peak.Add(sign * (d - margin))); in.Phase < phase {
					t.Errorf("%s: phase %d %v from the peak, want at least %d", c.peak, in.Phase, sign*(d-margin), phase)
				}
				if out := LunarEclipse(peak.Add(sign * (d + margin))); out.Phase >= phase {
					t.Errorf("%s: phase %d %v from the peak, want below %d", c.peak, out.Phase, sign*(d+margin), phase)
				}
			}
		}
	}

	// Totality leaves the refracted light alone, a deep partial eclipse
	// (96.6% of the disc in the umbra) about the rest, and outside the
	// shadow the Moon is untouched.
	total := LunarEclipse(time.Date(2025, 9, 7, 18, 11, 42, 0, time.UTC))
	if math.Abs(total.Light-umbraLight) > 1e-12 {
		t.Errorf("light at totality %g, want %g", total.Light, umbraLight)
	}
	if partial := LunarEclipse(time.Date(2026, 8, 28, 4, 12, 49, 0, time.UTC)).Light; partial <= umbraLight || partial > 0.034 {
		t.Errorf("light at the 2026-08-28 peak %g, want within (%g, 0.034]", partial, umbraLight)
	}
	if e := LunarEclipse(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); e != (Eclipse{Light: 1}) {
		t.Errorf("no eclipse: %+v", e)
	}
	// The light falls monotonically into the shadow.
	last := 1.0
	for m := -170; m <= 0; m++ {
		l := LunarEclipse(time.Date(2026, 8, 28, 4, 12, 49, 0, time.UTC).Add(time.Duration(m) * time.Minute)).Light
		if l > last+1e-9 {
			t.Fatalf("light rises from %g to %g %d minutes from the peak", last, l, m)
		}
		last = l
	}
}

func TestCovered(t *testing.T) {
	for _, c := range []struct{ a, b, c, want float64 }{
		{1, 1, 2, 0},          // touching
		{1, 2, 0.5, 1},        // inside the larger
		{2, 1, 0, 0.25},       // the smaller inside it
		{1, 1, 0, 1},          // the same disc
		{1, 1, 1, 0.39100221}, // two unit discs a radius apart: (2π/3 − √3/2)/π
	} {
		if got := covered(c.a, c.b, c.c); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("covered(%v, %v, %v) = %v, want %v", c.a, c.b, c.c, got, c.want)
		}
	}
}

// TestDeltaT pins deltaT to Astronomy Engine's DeltaT_EspenakMeeus (v2.1.19,
// Python) in every range: the 1986-2005 polynomial alone, which the first
// port used for all earlier years, read −919 s for 1960's 33.
func TestDeltaT(t *testing.T) {
	for _, c := range []struct{ year, want float64 }{
		{-1000, 25427.68},
		{0, 10583.6},
		{1000, 1574.2},
		{1650, 50.194016},
		{1750, 13.370070},
		{1850, 7.1069},
		{1880, -5.008487},
		{1910, 10.3884},
		{1930, 24.1329},
		{1950, 29.07},
		{1960, 33.103434},
		{1975, 45.45},
		{1990, 56.894641},
		{2026, 75.074584},
		{2100, 202.74},
		{2200, 442.08},
	} {
		if got := deltaT((c.year-2000)*365.24217 + 14); math.Abs(got-c.want) > 1e-6*max(1, math.Abs(c.want)) {
			t.Errorf("deltaT(%v) = %v, want %v", c.year, got, c.want)
		}
	}
}
