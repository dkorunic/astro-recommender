// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/config"
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

// Minutes the forecast does not cover get the covered minutes' mean quality,
// not a clear sky; no forecast at all is clear.
func TestBuildSkyPartialForecast(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, Extinction: 0.2}
	start := time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	f := NoForecast()
	if s := BuildSky(cfg, f, start, end); s.Quality[0] != 1 || s.Quality[len(s.Grid)-1] != 1 {
		t.Errorf("no forecast: quality %v..%v, want 1", s.Quality[0], s.Quality[len(s.Grid)-1])
	}
	// 20:00 overcast, 21:00 clear (dew spread and wind harmless), 22:00 missing.
	clear := weather.HourWeather{Temp: 10, DewPoint: 0}
	f.Weather = map[int64]weather.HourWeather{
		start.Unix():                {Cloud: 100, Temp: 10, DewPoint: 0},
		start.Add(time.Hour).Unix(): clear,
	}
	s := BuildSky(cfg, f, start, end)
	for i, want := range map[int]float64{0: 0, 60: 1, 120: 0.5, 179: 0.5} {
		if got := s.Quality[i]; math.Abs(got-want) > 1e-9 {
			t.Errorf("minute %d: quality %v, want %v", i, got, want)
		}
	}
	// Transparency only up to 22:30 (the 21:00 point; 7Timer ends sooner than
	// Open-Meteo): its worst class halves quality, and the uncovered rest
	// (minute 179) gets the same mean factor (0.5), not a perfect 1.
	f.Weather = nil
	f.Astro = map[int64]weather.AstroBlock{start.Add(time.Hour).Unix(): {Seeing: 1, Transparency: 8}}
	s = BuildSky(cfg, f, start, end)
	for i, want := range map[int]float64{0: 0.5, 60: 0.5, 179: 0.5} {
		if got := s.Quality[i]; math.Abs(got-want) > 1e-9 {
			t.Errorf("transparency minute %d: quality %v, want %v", i, got, want)
		}
	}
}

// TestFillGapsWarns: a fetched forecast that misses the whole window (7Timer
// with a far -date) must warn, not silently assume a perfect sky; nothing
// fetched stays silent.
func TestFillGapsWarns(t *testing.T) {
	stderr := func(fetched bool) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "stderr")
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stderr
		os.Stderr = f
		v := make([]float64, 3)
		fillGaps("transparency", v, make([]bool, 3), fetched, 3*time.Minute)
		os.Stderr = old
		out, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if v[0] != 1 {
			t.Errorf("fetched %v: uncovered quality %v, want 1", fetched, v[0])
		}

		return string(out)
	}
	if out := stderr(true); !strings.Contains(out, "does not cover") {
		t.Errorf("fetched but uncovered: stderr %q, want a warning", out)
	}
	if out := stderr(false); out != "" {
		t.Errorf("nothing fetched: stderr %q, want none", out)
	}
}
