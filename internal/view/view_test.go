// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/scoring"
)

// The page hands Zone to Intl, which throws a RangeError on Go's "Local"
// and "Factory" (-tz Local, -tz Factory) and so stops the whole render.
func TestZone(t *testing.T) {
	factory, err := time.LoadLocation("Factory")
	if err != nil {
		t.Skip(err)
	}
	zagreb, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Skip(err)
	}
	for loc, want := range map[*time.Location]string{time.Local: "", factory: "UTC", zagreb: "Europe/Zagreb", time.UTC: "UTC"} {
		if got := zone(loc); got != want {
			t.Errorf("zone(%s) = %q, want %q", loc, got, want)
		}
	}
}

// The Moon's hours span dusk to dawn, as the bars do, not the window -from
// clips: on 20 October 2026 from Zagreb the first-quarter Moon is up at dusk
// and sets at 23:41 UTC, so a window from 00:30 holds none of it.
func TestMoonUpWholeNight(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, Loc: time.UTC, Extinction: 0.2, AltMin: 30, AltMax: 80}
	dusk, dawn, ok := astro.Window(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	if !ok {
		t.Fatal("no night")
	}
	s := scoring.BuildSky(cfg, scoring.NoForecast(), time.Date(2026, 10, 21, 0, 30, 0, 0, time.UTC), dawn)
	s.Night = [2]time.Time{dusk, dawn}
	up := Build(cfg, &s, "", nil, nil).Night.MoonUp
	if len(up) != 1 || up[0][0] != dusk.Unix() || up[0][1] >= s.Start.Unix() || up[0][1] <= dusk.Unix() {
		t.Errorf("Moon up %v, want one span from dusk %v to before the window's %v", up, dusk, s.Start)
	}
}

// The page's and the terminal view's Moon limit carry the header's floors:
// through a narrowband filter the limit drops to its moonK.
func TestMoonFloor(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, Loc: time.UTC, Extinction: 0.2, AltMin: 30, AltMax: 80, Filter: "ha", FilterNM: 7}
	start := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	s := scoring.BuildSky(cfg, scoring.NoForecast(), start, start.Add(10*time.Hour))
	s.Night = [2]time.Time{s.Start, s.End}
	if n := Build(cfg, &s, "", nil, nil).Night; !strings.Contains(n.MoonLow, "through a filter") {
		t.Errorf("Moon limit %.0f° with floor %q, want a filter's", n.MoonSep, n.MoonLow)
	}
}

// moonUp samples every Step minutes and bisects the crossings: the spans of
// the minute-by-minute scan, over a month of nights at three latitudes.
func TestMoonUp(t *testing.T) {
	minutes := func(cfg *config.Config, dusk, dawn time.Time) [][2]int64 {
		spans := [][2]int64{}
		was := false
		for t := dusk; t.Before(dawn); t = t.Add(time.Minute) {
			ra, dec := astro.MoonTopo(t, cfg.Lat, cfg.Lon)
			up := astro.Altitude(ra, dec, t, cfg.Lat, cfg.Lon) > astro.MoonHorizon
			switch {
			case up && !was:
				spans = append(spans, [2]int64{t.Unix(), dawn.Unix()})
			case !up && was:
				spans[len(spans)-1][1] = t.Unix()
			}
			was = up
		}

		return spans
	}
	for _, lat := range []float64{45.8, -33.9, 78.2} {
		cfg := &config.Config{Lat: lat, Lon: 16}
		for d := range 30 {
			dusk := time.Date(2026, 11, 1+d, 17, 3, 20, 0, time.UTC)
			dawn := dusk.Add(13*time.Hour + 41*time.Minute + 30*time.Second)
			if got, want := moonUp(cfg, dusk, dawn), minutes(cfg, dusk, dawn); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("lat %g, %s: %v, want %v", lat, dusk.Format(time.DateOnly), got, want)
			}
		}
	}
}
