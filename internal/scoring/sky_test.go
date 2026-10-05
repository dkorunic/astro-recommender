// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

func TestRamp(t *testing.T) {
	// ramp: higher-is-worse (transparency) and higher-is-better (dew spread).
	for _, c := range [][5]float64{
		{1, 1, 8, 0.5, 1},
		{8, 1, 8, 0.5, 0.5},
		{4.5, 1, 8, 0.5, 0.75},
		{9, 1, 8, 0.5, 0.5},
		{5, 4, 1, 0.7, 1},
		{2.5, 4, 1, 0.7, 0.85},
		{0, 4, 1, 0.7, 0.7},
	} {
		if got := ramp(c[0], c[1], c[2], c[3]); math.Abs(got-c[4]) > 1e-9 {
			t.Errorf("ramp%v = %v", c[:4], got)
		}
	}
}

func TestExtinctionAt(t *testing.T) {
	s := Sky{Elevation: math.NaN()}
	if k := s.ExtinctionAt(time.Now(), 0.2); k != 0.2 {
		t.Errorf("extinctionAt without elevation = %v, want fallback 0.2", k)
	}
	s.Elevation = 0
	if k := s.ExtinctionAt(time.Now(), 0.2); math.Abs(k-atmos.ExtinctionCoeff(0, atmos.TypicalAOD)) > 1e-12 {
		t.Errorf("extinctionAt without AOD = %v, want typical", k)
	}
}

// Grid minutes without a forecast hour are counted; no forecast at all is not.
func TestUncovered(t *testing.T) {
	start := time.Date(2026, 10, 5, 20, 30, 0, 0, time.UTC)
	s := Sky{}
	for i := range 150 { // 20:30-23:00
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
	}
	if got := s.uncovered(); got != 0 {
		t.Errorf("no forecast: %v, want 0", got)
	}
	s.Weather = map[int64]weather.HourWeather{start.Truncate(time.Hour).Unix(): {}} // 20:00 only
	if got := s.uncovered(); got != 2*time.Hour {
		t.Errorf("20:00 only: %v, want 2h (21:00-23:00)", got)
	}
}
