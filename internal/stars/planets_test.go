// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package stars

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/hebl/gofa"
)

// TestPlanetsSOFA checks the geocentric positions against SOFA's Plan94
// (and Epv00's Earth) over random dates in 2000-2050, and the magnitudes
// against the ranges each planet shows (Venus' to −3.0 for inferior
// conjunction, as at its 2004 transit, past which the formula holds); a
// planet fainter than V 4 is left out, so Mercury only sometimes appears.
// The limits are JPL's stated heliocentric errors (Mars 40″, Jupiter 400″)
// as seen from the Earth at the planet's closest.
func TestPlanetsSOFA(t *testing.T) {
	limit := map[string]float64{"Mercury": 1, "Venus": 2, "Mars": 3, "Jupiter": 12, "Saturn": 15} // arcminutes; 0.7, 1.4, 2.7, 10.5, 11.1 seen
	mags := map[string][2]float64{"Mercury": {-2.5, 4}, "Venus": {-4.9, -3.0}, "Mars": {-3, 1.9}, "Jupiter": {-2.95, -1.6}, "Saturn": {-0.6, 1.5}}
	index := map[string]int{"Mercury": 1, "Venus": 2, "Mars": 4, "Jupiter": 5, "Saturn": 6}
	worst := map[string]float64{}
	rng := rand.New(rand.NewPCG(5, 7))
	for range 2000 {
		ut := time.Unix(rng.Int64N(50*365*86400)+946684800, 0).UTC()
		jd := float64(ut.Unix())/86400 + 2440587.5
		var pvh, pvb [2][3]float64
		gofa.Epv00(jd, 0, &pvh, &pvb)
		seen := map[string]bool{}
		for _, p := range Planets(ut) {
			seen[p.Name] = true
			var pv [2][3]float64
			gofa.Plan94(jd, 0, index[p.Name], &pv)
			g := [3]float64{pv[0][0] - pvh[0][0], pv[0][1] - pvh[0][1], pv[0][2] - pvh[0][2]}
			ra, dec := equatorial(g)
			worst[p.Name] = max(worst[p.Name], astro.Separation(p.RA, p.Dec, ra, dec)*60)
			if m := mags[p.Name]; p.Mag < m[0] || p.Mag > m[1] {
				t.Errorf("%s on %s at V %.2f, outside %v", p.Name, ut.Format(time.DateOnly), p.Mag, m)
			}
		}
		for _, name := range []string{"Venus", "Mars", "Jupiter", "Saturn"} {
			if !seen[name] {
				t.Errorf("%s missing on %s", name, ut.Format(time.DateOnly))
			}
		}
	}
	for name, w := range worst {
		if w > limit[name] {
			t.Errorf("%s: worst %.2f′ from SOFA, limit %v′", name, w, limit[name])
		} else {
			t.Logf("%s: worst %.2f′ (limit %v′)", name, w, limit[name])
		}
	}
}

// A planet beats the fainter stars around it.
func TestBrightestPlanet(t *testing.T) {
	jupiter := Star{Name: "Jupiter", Mag: -2.4, RA: 85.3, Dec: -1.9} // beside Alnitak
	if s, _, ok := Brightest(85.42, -1.85, 0.75, []Star{jupiter}); !ok || s.Name != "Jupiter" {
		t.Errorf("got %q %v, want Jupiter", s.Name, ok)
	}
	faint := Star{Name: "Saturn", Mag: 2.3, RA: 85.3, Dec: -1.9}
	if s, _, ok := Brightest(85.42, -1.85, 0.75, []Star{faint}); !ok || s.Name != "ζ Ori" {
		t.Errorf("got %q %v, want ζ Ori over a fainter planet", s.Name, ok)
	}
}
