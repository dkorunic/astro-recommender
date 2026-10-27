// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/geotz"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/view"
)

// init serves web/index.html's time zone lookup: run as argv ["tz", lat, lon]
// it prints the zone and exits before main, so -h and the flags stay those
// of the command line.
func init() { //nolint:gochecknoinits // must run before main, which parses the flags
	if len(os.Args) != 3 || os.Args[0] != "tz" {
		return
	}
	lat, errLat := strconv.ParseFloat(os.Args[1], 64)
	lon, errLon := strconv.ParseFloat(os.Args[2], 64)
	if errLat != nil || errLon != nil || !(lat >= -90 && lat <= 90) || !(lon >= -180 && lon <= 180) {
		os.Exit(2)
	}
	name, err := geotz.Lookup(lat, lon)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(name)
	os.Exit(0)
}

// altitudes hands web/index.html the night as view.Build gathers it (each
// listed result's altitude curve, observable stretches, card, sub-exposure
// estimate and bright star; the Moon, the night's edges and the forecast
// hours) as JSON in /altitudes, a write-only file only the page's fs shim
// accepts (Go's js syscalls know no fd it did not open).
func altitudes(cfg *config.Config, s *scoring.Sky, place string, slots []plan.Slot, results []scoring.Result) {
	if len(s.Grid) == 0 {
		return
	}
	b, err := json.Marshal(view.Build(cfg, s, place, results, slots))
	if err != nil {
		return
	}
	if f, err := os.OpenFile("/altitudes", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
		_, _ = f.Write(b)
		_ = f.Close()
	}
}

// useTUI is false in the browser, whose page is the interactive view.
func useTUI(*config.Config) bool { return false }

// runTUI is never called in the browser (useTUI).
func runTUI(context.Context, config.Config, []catalog.Target) error { return nil }
