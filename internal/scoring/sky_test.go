// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"errors"
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
	// Rain is a gate: 0.1 mm in the hour zeroes it whatever the cloud cover,
	// a trace halves it, and an unknown amount (NaN) does nothing.
	wet := clear
	wet.Precip = 0.1
	trace := clear
	trace.Precip = 0.05
	unknown := clear
	unknown.Precip = math.NaN()
	f.Weather = map[int64]weather.HourWeather{start.Unix(): wet, start.Add(time.Hour).Unix(): trace, start.Add(2 * time.Hour).Unix(): unknown}
	s = BuildSky(cfg, f, start, end)
	for i, want := range map[int]float64{0: 0, 60: 0.5, 120: 1} {
		if got := s.Quality[i]; math.Abs(got-want) > 1e-9 {
			t.Errorf("rain minute %d: quality %v, want %v", i, got, want)
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

// missingSeeing counts the fallback hours (weather, no 7Timer point) and
// those among them without an estimate; a partial profile shows up too.
func TestMissingSeeing(t *testing.T) {
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	h0, h1 := start.Unix(), start.Add(time.Hour).Unix()
	profile := weather.HourWeather{Seeing: 1.2}
	astro := map[int64]weather.AstroBlock{weather.AstroKey(start): {}, weather.AstroKey(start.Add(time.Hour)): {}}
	for name, c := range map[string]struct {
		f               Forecast
		missing, needed int
	}{
		"no weather":                {Forecast{}, 0, 0},
		"no levels":                 {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: {}}}, 2, 2},
		"7Timer covers all":         {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: {}}, Astro: astro}, 0, 0},
		"one hour with an estimate": {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: profile}}, 1, 2},
		"both with an estimate":     {Forecast{Weather: map[int64]weather.HourWeather{h0: profile, h1: profile}}, 0, 2},
	} {
		if missing, needed := missingSeeing(c.f, start, end); missing != c.missing || needed != c.needed {
			t.Errorf("%s: missingSeeing = %d of %d, want %d of %d", name, missing, needed, c.missing, c.needed)
		}
	}
}

// seeingWarning speaks only when some hour needs the estimate: then the
// fetch error if the profile failed, else the count of hours without one.
func TestSeeingWarning(t *testing.T) {
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	h0, h1 := start.Unix(), start.Add(time.Hour).Unix()
	astro := map[int64]weather.AstroBlock{weather.AstroKey(start): {}, weather.AstroKey(start.Add(time.Hour)): {}}
	failed := errors.New("open-meteo: HTTP status 500")
	for name, c := range map[string]struct {
		f    Forecast
		err  error
		want string
	}{
		"7Timer covers all, profile failed": {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: {}}, Astro: astro}, failed, ""},
		"needed, profile failed":            {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: {}}}, failed, "no upper-air profile, no seeing estimate: open-meteo: HTTP status 500"},
		"needed, one missing":               {Forecast{Weather: map[int64]weather.HourWeather{h0: {}, h1: {Seeing: 1.2}}}, nil, "no usable upper-air seeing estimate for 1 of the 2 hours without a 7Timer point"},
		"needed, none missing":              {Forecast{Weather: map[int64]weather.HourWeather{h0: {Seeing: 1.1}, h1: {Seeing: 1.2}}}, nil, ""},
		"no weather at all":                 {Forecast{}, failed, ""},
	} {
		if got := seeingWarning(c.f, c.err, start, end); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// mergeProfile computes the estimate for the hours the surface forecast
// has; hours only the profile has are not weather.
func TestMergeProfile(t *testing.T) {
	w := map[int64]weather.HourWeather{0: {Cloud: 10}, 3600: {Cloud: 20}}
	lv := []weather.Level{{P: 998, Z: 122, T: 12, Wind: 5, Dir: 270}, {P: 850, Z: 1500, T: 10, Dir: 270}, {P: 500, Z: 5500, T: -20, Wind: 20, Dir: 270}, {P: 250, Z: 10500, T: -50, Wind: 30, Dir: 270}}
	mergeProfile(w, map[int64][]weather.Level{3600: lv, 7200: lv})
	if len(w) != 2 || w[0].Seeing != 0 || w[3600].Seeing != weather.Seeing(lv) || w[3600].Seeing == 0 || w[3600].Cloud != 20 {
		t.Errorf("merged %+v", w)
	}
	mergeProfile(w, nil)
	if w[3600].Seeing == 0 {
		t.Error("a nil profile cleared the estimate")
	}
}
