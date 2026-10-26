// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// TestEclipseNight runs the total lunar eclipse of 3 March 2026 (totality
// 11:04-12:03 UT) from Honolulu, the Moon high: the shadow dims the
// moonlight and the Moon limit with it, so a target 20° from a full Moon,
// kept out before the eclipse, is observable in totality.
func TestEclipseNight(t *testing.T) {
	cfg := mutCfg()
	cfg.Lat, cfg.Lon, cfg.Extinction = 21.3, -157.9, 0.2
	start := time.Date(2026, 3, 3, 8, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(6*time.Hour))
	if s.Eclipse == nil {
		t.Fatal("no eclipse")
	}
	before, peak := 20, 213 // 08:20 (the penumbra from 08:44) and 11:33 UT
	if e := s.Eclipse[before]; e != (astro.Eclipse{Light: 1}) {
		t.Errorf("at 08:20 %+v, want outside the shadow", e)
	}
	if e := s.Eclipse[peak]; e.Phase != astro.Total {
		t.Errorf("at 11:33 %+v, want total", e)
	}
	lit := atmos.MoonLight(s.Ext[peak], s.ExtRayleigh[peak], s.ExtAerosol[peak], s.MoonAlt[peak], astro.MoonPhaseAngle(s.Grid[peak]), astro.MoonDistance(s.Grid[peak]))
	if got, want := s.MoonLight[peak].NL, lit.NL*s.Eclipse[peak].Light; math.Abs(got-want) > 1e-12*want || got > 1e-3*lit.NL {
		t.Errorf("moonlight in totality %g, want %g", got, want)
	}

	ra, dec := s.MoonPos[peak][0], s.MoonPos[peak][1]
	tg := catalog.Target{Name: "near", Type: "Galaxy", RADeg: ra, DecDeg: dec + 20, Size: 20}
	r := Score(cfg, &s, []catalog.Target{tg}, true)
	if len(r) != 1 {
		t.Fatalf("%d results", len(r))
	}
	if r[0].Obs[before] || !r[0].Obs[peak] {
		t.Errorf("observable at 08:20 %v, at 11:33 %v; want false, true", r[0].Obs[before], r[0].Obs[peak])
	}

	// The 25-minute penumbral graze of 18 July 2027 (peak 16:03 UT) at the
	// end of a 12 h window, the farthest the pre-check lets through.
	graze := time.Date(2027, 7, 18, 4, 10, 0, 0, time.UTC)
	if s := BuildSky(cfg, NoForecast(), graze, graze.Add(12*time.Hour)); s.Eclipse == nil {
		t.Error("no graze on 18 July 2027")
	}
	// A night without an eclipse keeps nil.
	if s := BuildSky(cfg, NoForecast(), start.AddDate(0, 0, 3), start.AddDate(0, 0, 3).Add(time.Hour)); s.Eclipse != nil {
		t.Error("eclipse on 6 March")
	}
}
