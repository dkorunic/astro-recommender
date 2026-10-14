// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Command astro-recommender lists the best deep sky objects for a location,
// tonight, between the start and end of astronomical night.
// Scoring follows uptonight (https://github.com/mawinkler/uptonight).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
	_ "time/tzdata" // zone database for Windows and minimal containers, which lack one

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/comets"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/geocode"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/sqm"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		fatal(err.Error())
	}
	if cfg.Version {
		fmt.Println(versionString())

		return
	}
	ctx := context.Background()
	targets, listName, err := catalog.Load(cfg.ListName, cfg.TargetsFile)
	if err != nil {
		fatal(err.Error())
	}
	cfg.ListName = listName

	// Without -date, "tonight" between dusk and dawn is the night in progress,
	// and only its remaining part is planned.
	day, inProgress := cfg.Day, false
	if !cfg.DateSet {
		day, inProgress = astro.Tonight(cfg.Day, cfg.Lat, cfg.Lon, cfg.SunAlt())
	}
	duskT, dawnT, ok := astro.Window(day, cfg.Lat, cfg.Lon, cfg.SunAlt())
	if !ok {
		nothing(&cfg, fmt.Sprintf("No %s night at this location tonight.", cfg.Twilight))

		return
	}
	now := cfg.Day.Truncate(time.Minute)
	if inProgress {
		dawnT = astro.ContinuePolar(day, duskT, dawnT, now, cfg.Lat, cfg.Lon, cfg.SunAlt())
	}
	duskT, dawnT = duskT.In(cfg.Loc), dawnT.In(cfg.Loc) // all times display in -tz
	start, end, err := astro.ClipWindow(day, cfg.Loc, duskT, dawnT, cfg.From, cfg.To)
	if err != nil {
		fatal(err.Error())
	}
	if inProgress && now.After(start) {
		if start = now; !start.Before(end) {
			nothing(&cfg, "The requested part of tonight has already passed.")

			return
		}
	}

	// All network sources at once, after the early exits above so those never
	// pay for a round-trip. The goroutines only read cfg; results go to
	// locals, applied after Wait.
	var (
		wg        sync.WaitGroup
		forecast  scoring.Forecast
		place     string
		cometEls  comets.Elements
		cometErr  error
		sqmVal    float64
		sqmSource string
	)
	wg.Go(func() { forecast = scoring.FetchForecast(ctx, &cfg, start, end) })
	// An explicit -sqm or -bortle (even 0) wins; the lookup needs a key.
	if key := sqm.Key(); key != "" && !cfg.NoSQM && !cfg.SkySet {
		wg.Go(func() {
			var err error
			if sqmVal, sqmSource, err = sqm.Lookup(ctx, key, cfg.Lat, cfg.Lon); err != nil {
				fmt.Fprintln(os.Stderr, "warning: no sky brightness lookup, assuming a dark sky:", err)
			}
		})
	}
	if !cfg.NoGeocode {
		wg.Go(func() {
			var err error
			if place, err = geocode.Reverse(ctx, cfg.Lat, cfg.Lon); err != nil {
				fmt.Fprintln(os.Stderr, "warning: no reverse geocoding:", err)
			}
		})
	}
	if !cfg.NoComets {
		wg.Go(func() {
			if cometEls, cometErr = comets.Fetch(ctx); cometErr != nil {
				fmt.Fprintln(os.Stderr, "warning: no comets:", cometErr)
			}
		})
	}
	wg.Wait()
	if sqmSource != "" {
		cfg.SQM, cfg.SQMSource = sqmVal, sqmSource
	}

	s := scoring.BuildSky(&cfg, forecast, start, end)
	s.Night = [2]time.Time{duskT, dawnT}
	cometList := cometEls.Targets(s.Grid, cfg.CometMag)
	s.Comets, s.CometsLost = len(cometList), cometErr != nil
	targets = append(targets, cometList...)
	results := scoring.Score(&cfg, &s, targets, cfg.Plan > 0)
	var slots []plan.Slot
	if cfg.Plan > 0 {
		slots = plan.Make(&s, results, cfg.Plan)
	}
	exposures(&cfg, &s, slots, results)

	if cfg.JSON {
		if err := output.JSON(&cfg, &s, place, slots, results); err != nil {
			fatal(err.Error())
		}

		return
	}
	if cfg.CSV {
		if err := output.CSV(&cfg, &s, slots, results); err != nil {
			fatal(err.Error())
		}

		return
	}
	output.UseColor = output.ColorTerminal()
	output.Header(&cfg, &s, place)
	output.Weather(&cfg, &s)
	if cfg.Plan > 0 {
		output.Plan(&cfg, &s, slots)
	}
	output.Legend(&cfg, &s, output.Results(&cfg, &s, results))
	altitudes(&cfg, &s, results)
}

// nothing reports a night with nothing to plan: as text, or with -json or
// -csv as a document a consumer can still parse (the CSV's header alone, the
// message on stderr).
func nothing(cfg *config.Config, msg string) {
	if cfg.CSV {
		fmt.Fprintln(os.Stderr, msg)
		if err := output.CSV(cfg, nil, nil, nil); err != nil {
			fatal(err.Error())
		}

		return
	}
	if cfg.JSON {
		// json, not %q: Go's escapes (\x..) are not all valid JSON.
		b, err := json.MarshalIndent(struct {
			Message string   `json:"message"`
			Targets []string `json:"targets"`
		}{msg, []string{}}, "", "  ")
		if err != nil {
			fatal(err.Error())
		}
		fmt.Println(string(b))

		return
	}
	fmt.Println(msg)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

// exposures attaches the sub-exposure estimate to each listed result. It
// needs a camera, which framing describes, and -csv never shows it.
func exposures(cfg *config.Config, s *scoring.Sky, slots []plan.Slot, results []scoring.Result) {
	if !cfg.Framing || cfg.CSV {
		return
	}
	for i := range results[:min(cfg.Top, len(results))] {
		// Rescored once here, so Subs and the browser's altitudes share it.
		if results[i].Obs == nil {
			if rs := scoring.Score(cfg, s, []catalog.Target{results[i].Target}, true); len(rs) == 1 {
				results[i].Obs = rs[0].Obs
			}
		}
		if e := scoring.Subs(cfg, s, results[i], planned(slots, &results[i])); e.Good > 0 {
			results[i].Exposure = &e
		}
	}
}

// planned returns the time the plan gives r, its blocks' length; 0 without
// -plan or for a listed target it does not plan.
func planned(slots []plan.Slot, r *scoring.Result) time.Duration {
	var d time.Duration
	for _, sl := range slots {
		if sl.Result == r {
			d += sl.End.Sub(sl.Start)
		}
	}

	return d
}
