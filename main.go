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
	"slices"
	"sync"
	"time"
	_ "time/tzdata" // zone database for Windows and minimal containers, which lack one

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/comets"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/geocode"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/solar"
	"github.com/dkorunic/astro-recommender/internal/sqm"
)

func main() {
	config.UsageWidth = output.TerminalWidth()
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
	if useTUI(&cfg) {
		if err := runTUI(ctx, cfg, targets); err != nil {
			fatal(err.Error())
		}

		return
	}

	n, err := compute(ctx, cfg, targets, session{})
	if err != nil {
		fatal(err.Error())
	}
	if n.msg != "" {
		nothing(&n.cfg, n.msg)

		return
	}
	cfg, s := n.cfg, n.sky
	if cfg.JSON {
		if err := output.JSON(&cfg, &s, n.place, n.slots, n.results); err != nil {
			fatal(err.Error())
		}

		return
	}
	if cfg.CSV {
		if err := output.CSV(&cfg, &s, n.slots, n.results); err != nil {
			fatal(err.Error())
		}

		return
	}
	output.UseColor = output.ColorTerminal()
	output.TermWidth = output.TerminalWidth()
	output.Header(&cfg, &s, n.place)
	output.Weather(&cfg, &s)
	if cfg.Plan > 0 {
		output.Plan(&cfg, &s, n.slots)
	}
	output.Legend(&cfg, &s, output.Results(&cfg, &s, n.results))
	altitudes(&cfg, &s, n.place, n.slots, n.results)
}

// night is one computed night, what every output shows: cfg with the
// lookups applied (sky brightness, solar flux), the sky, the place name,
// the ranked results and the -plan slots. day is the evening's date; msg,
// when not empty, says there is nothing to plan, and only cfg and day are
// set.
type night struct {
	day     time.Time
	place   string
	msg     string
	results []scoring.Result
	slots   []plan.Slot
	sky     scoring.Sky
	cfg     config.Config
}

// warn prints a warning unless ctx is done: a cancelled compute's fetches
// fail by design, which is no news.
func warn(ctx context.Context, a ...any) {
	if ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, a...)
	}
}

// session holds what compute's network sources know of the site alone, for
// the terminal view's nights to share (newSession); its zero value, a single
// run's, asks every time.
type session struct {
	sqm  *fetch.Memo[sqmAnswer]
	site scoring.Site
}

// sqmAnswer is sqm.Lookup's answer.
type sqmAnswer struct {
	source string
	mag    float64
}

// newSession returns a session that remembers the site's sources: the
// DarkSkySites map is monthly, and its quota counts every ← and →.
func newSession() session { return session{sqm: fetch.NewMemo[sqmAnswer](), site: scoring.NewSite()} }

// compute runs the pipeline for cfg's night over targets: the window (the
// night in progress, or -date's), every network source at once, the sky,
// the scores, the plan and the sub-exposure estimates. targets is not
// modified, so the terminal UI computes other nights from the same list,
// through one ses.
func compute(ctx context.Context, cfg config.Config, targets []catalog.Target, ses session) (night, error) {
	// Without -date, "tonight" between dusk and dawn is the night in progress,
	// and only its remaining part is planned.
	day, inProgress := cfg.Day, false
	if !cfg.DateSet {
		day, inProgress = astro.Tonight(cfg.Day, cfg.Lat, cfg.Lon, cfg.SunAlt())
	}
	n := night{cfg: cfg, day: day}
	duskT, dawnT, ok := astro.Window(day, cfg.Lat, cfg.Lon, cfg.SunAlt())
	if !ok {
		n.msg = fmt.Sprintf("No %s night at this location tonight.", cfg.Twilight)

		return n, nil
	}
	now := cfg.Day.Truncate(time.Minute)
	if inProgress {
		dawnT = astro.ContinuePolar(day, dawnT, now, cfg.Lat, cfg.Lon, cfg.SunAlt())
	}
	duskT, dawnT = duskT.In(cfg.Loc), dawnT.In(cfg.Loc) // all times display in -tz
	start, end, err := astro.ClipWindow(day, cfg.Loc, duskT, dawnT, cfg.From, cfg.To)
	if err != nil {
		return n, err
	}
	if inProgress && now.After(start) {
		if start = now; !start.Before(end) {
			n.msg = "The requested part of tonight has already passed."

			return n, nil
		}
	}

	// All network sources at once, after the early exits above so those never
	// pay for a round-trip. The goroutines only read cfg; results go to
	// locals, applied after Wait.
	var (
		wg         sync.WaitGroup
		forecast   scoring.Forecast
		cometEls   comets.Elements
		cometErr   error
		cometSeen  comets.Observed
		sqmVal     float64
		sqmSource  string
		solarSFU   float64
		solarBasis string
	)
	wg.Go(func() { forecast = scoring.FetchForecast(ctx, &cfg, start, end, ses.site) })
	// An explicit -sqm or -bortle (even 0) wins; the lookup needs a key.
	if key := sqm.Key(); key != "" && !cfg.NoSQM && !cfg.SkySet {
		wg.Go(func() {
			a, err := ses.sqm.Get(ctx, 0, func(ctx context.Context) (sqmAnswer, error) {
				mag, source, err := sqm.Lookup(ctx, key, cfg.Lat, cfg.Lon)

				return sqmAnswer{source, mag}, err
			})
			if sqmVal, sqmSource = a.mag, a.source; err != nil {
				warn(ctx, "warning: no sky brightness lookup, assuming a dark sky:", err)
			}
		})
	}
	if !cfg.NoGeocode {
		wg.Go(func() {
			var err error
			if n.place, err = geocode.Reverse(ctx, cfg.Lat, cfg.Lon); err != nil {
				warn(ctx, "warning: no reverse geocoding:", err)
			}
		})
	}
	if !cfg.NoComets {
		wg.Go(func() {
			if cometEls, cometErr = comets.Fetch(ctx); cometErr != nil {
				warn(ctx, "warning: no comets:", cometErr)
			}
		})
		wg.Go(func() {
			var err error
			if cometSeen, err = comets.FetchObserved(ctx, start); err != nil {
				warn(ctx, "warning: comet magnitudes from MPC's elements, not observations:", err)
			}
		})
	}
	// A measured -sqm already holds its night's airglow; any other zenith
	// has 22.0's natural sky, solar minimum's, which the flux scales.
	if !cfg.NoSolar && cfg.SQM == 0 {
		wg.Go(func() {
			var err error
			if solarSFU, solarBasis, err = solar.Flux(ctx, start); err != nil {
				warn(ctx, "warning: sky not scaled for the solar cycle:", err)
			}
		})
	}
	wg.Wait()
	// A superseded night (the terminal view's ← and →) stops here: its
	// fetches failed as it was cancelled, and its scores would be dropped.
	if err := ctx.Err(); err != nil {
		return n, err
	}
	if sqmSource != "" {
		cfg.SQM, cfg.SQMSource = sqmVal, sqmSource
	}
	cfg.SolarFlux, cfg.SolarBasis = solarSFU, solarBasis

	s := scoring.BuildSky(&cfg, forecast, start, end)
	s.Night = [2]time.Time{duskT, dawnT}
	cometList := cometEls.Targets(s.Grid, cfg.CometMag, cometSeen)
	s.Comets, s.CometsLost = len(cometList), cometErr != nil
	// A new slice: appending to targets could write into the caller's.
	n.results = scoring.Score(&cfg, &s, slices.Concat(targets, cometList), cfg.Plan > 0)
	if cfg.Plan > 0 {
		n.slots = plan.Make(&s, n.results, cfg.Plan)
	}
	exposures(&cfg, &s, n.slots, n.results)
	n.cfg, n.sky = cfg, s

	return n, nil
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
		if e := scoring.Subs(cfg, s, results[i], planned(slots, s.Grid, &results[i])); e.Good > 0 {
			results[i].Exposure = &e
		}
	}
}

// planned marks the grid minutes the plan gives r, its blocks; nil without
// -plan or for a listed target it does not plan, which Subs reads as the
// whole night.
func planned(slots []plan.Slot, grid []time.Time, r *scoring.Result) []bool {
	var in []bool
	for _, sl := range slots {
		if sl.Result != r {
			continue
		}
		if in == nil {
			in = make([]bool, len(grid))
		}
		for i, t := range grid {
			if !t.Before(sl.Start) && t.Before(sl.End) {
				in[i] = true
			}
		}
	}

	return in
}
