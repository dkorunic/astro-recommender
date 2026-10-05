// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Command astro-recommender lists the best deep sky objects for a location,
// tonight, between the start and end of astronomical night.
// Scoring follows uptonight (https://github.com/mawinkler/uptonight).
package main

import (
	"context"
	"fmt"
	"os"
	"time"
	_ "time/tzdata" // zone database for Windows and minimal containers, which lack one

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/comets"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
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

	// Without -date, "tonight" after midnight is the night still in progress,
	// and only its remaining part is planned.
	day, inProgress := cfg.Day, false
	if !cfg.DateSet {
		day, inProgress = astro.Tonight(cfg.Day, cfg.Lat, cfg.Lon)
	}
	duskT, dawnT, ok := astro.Window(day, cfg.Lat, cfg.Lon)
	if !ok {
		fmt.Println("No astronomical night at this location tonight.")

		return
	}
	duskT, dawnT = duskT.In(cfg.Loc), dawnT.In(cfg.Loc) // all times display in -tz
	start, end, err := astro.ClipWindow(day, cfg.Loc, duskT, dawnT, cfg.From, cfg.To)
	if err != nil {
		fatal(err.Error())
	}
	if now := cfg.Day.Truncate(time.Minute); inProgress && now.After(start) {
		if start = now; !start.Before(end) {
			fmt.Println("The requested part of tonight has already passed.")

			return
		}
	}

	s := scoring.BuildSky(ctx, &cfg, start, end)
	s.Night = [2]time.Time{duskT, dawnT}
	if !cfg.NoComets {
		cometList, err := comets.Targets(ctx, s.Grid, cfg.CometMag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: no comets:", err)
		}
		s.Comets = len(cometList)
		targets = append(targets, cometList...)
	}
	results := scoring.Score(&cfg, &s, targets)

	output.UseColor = output.ColorTerminal()
	output.Header(ctx, &cfg, &s)
	output.Weather(&cfg, &s)
	if cfg.Plan > 0 {
		output.Plan(plan.Make(&s, results, cfg.Plan))
	}
	output.Results(&cfg, results)
	output.Legend(&s, cfg.Plan > 0)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
