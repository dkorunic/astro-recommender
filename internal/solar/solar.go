// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package solar looks up the monthly 10.7 cm solar radio flux, which drives
// the airglow's swing with the solar cycle (atmos.AirglowExcess).
package solar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/num"
)

// NOAA SWPC's monthly solar cycle indices, observed (the 10.7 cm flux from
// October 2004 to last month) and predicted (this cycle's, some months back
// to 2030), and the last 30 days' daily noon flux. All send
// Access-Control-Allow-Origin: *, so the browser build fetches them directly.
const (
	observedURL  = "https://services.swpc.noaa.gov/json/solar-cycle/observed-solar-cycle-indices.json"
	predictedURL = "https://services.swpc.noaa.gov/json/solar-cycle/predicted-solar-cycle.json"
	recentURL    = "https://services.swpc.noaa.gov/products/10cm-flux-30-day.json"
	cacheAge     = 24 * time.Hour
	maxSFU       = 1000 // above any month on record (~370 sfu); larger is junk
)

var errNoFlux = errors.New("no solar flux")

// What a flux is: the bases Flux returns.
const (
	Monthly   = "monthly mean"
	Recent    = "30-day mean"
	Predicted = "NOAA prediction"
)

// Flux returns the 10.7 cm solar flux in sfu for a night at t and its basis:
// NOAA's observed mean for t's UTC month (Monthly); else, until 30 days
// after the last daily value, the mean of the last 30 days (Recent), as
// the airglow follows the flux of the 30 days before (Patat 2008) and
// NOAA's monthly prediction for this month ran 20 sfu above it in October
// 2026; else that prediction (Predicted). The three lists are fetched at
// once, each cached for a day.
func Flux(ctx context.Context, t time.Time) (float64, string, error) {
	var (
		wg                      sync.WaitGroup
		observed, predict       map[string]float64
		latest                  recent
		obsErr, predErr, recErr error
	)
	wg.Go(func() { observed, obsErr = fetch.Cached(ctx, observedURL, "solar-observed.json", cacheAge, parse) })
	wg.Go(func() { predict, predErr = fetch.Cached(ctx, predictedURL, "solar-predicted.json", cacheAge, parse) })
	wg.Go(func() { latest, recErr = fetch.Cached(ctx, recentURL, "solar-30-day.json", cacheAge, parseRecent) })
	wg.Wait()

	return pick(observed, latest, predict, t, errors.Join(obsErr, recErr, predErr))
}

// recentSpan is how long after its last day the 30-day mean stands for a
// night: the airglow follows the flux of the month before.
const recentSpan = 30 * 24 * time.Hour

// recent is the 30-day list: its days' mean flux, the first and the last.
type recent struct {
	first, last time.Time
	mean        float64
}

// pick returns the flux for a night at t from observed, recent or predicted,
// in that order (Flux); err, the fetches' failures, is the reason when none
// has it.
func pick(observed map[string]float64, latest recent, predicted map[string]float64, t time.Time, err error) (float64, string, error) {
	month := t.UTC().Format("2006-01")
	if v, ok := observed[month]; ok {
		return v, Monthly, nil
	}
	if latest.mean > 0 && !t.Before(latest.first) && t.Before(latest.last.Add(recentSpan)) {
		return latest.mean, Recent, nil
	}
	if v, ok := predicted[month]; ok {
		return v, Predicted, nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("%w for %s: %w", errNoFlux, month, err)
	}

	return 0, "", fmt.Errorf("%w for %s: outside NOAA's observed and predicted months", errNoFlux, month)
}

// parseRecent reads the 30-day list's daily values, all of which must be
// plausible: a mean over a gap would stand for fewer days than it says.
func parseRecent(data []byte) (recent, error) {
	//nolint:tagliatelle // NOAA's field names
	var rows []struct {
		Tag  string  `json:"time_tag"`
		Flux float64 `json:"flux"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return recent{}, err
	}
	var r recent
	for _, row := range rows {
		day, err := time.Parse("2006-01-02T15:04:05", row.Tag)
		if err != nil || !num.Finite(row.Flux) || row.Flux <= 0 || row.Flux > maxSFU {
			return recent{}, fmt.Errorf("%w: bad 30-day entry %q", errNoFlux, row.Tag)
		}
		if r.first.IsZero() || day.Before(r.first) {
			r.first = day
		}
		if day.After(r.last) {
			r.last = day
		}
		r.mean += row.Flux
	}
	if len(rows) < 20 {
		return recent{}, fmt.Errorf("%w: %d days in the 30-day list", errNoFlux, len(rows))
	}
	r.mean /= float64(len(rows))

	return r, nil
}

// parse reads either list into month ("2006-01") → sfu, keeping only
// plausible values (NOAA writes -1 for none); a list with none is an error,
// so fetch.Cached never caches an error page.
func parse(data []byte) (map[string]float64, error) {
	//nolint:tagliatelle // NOAA's field names
	var rows []struct {
		Tag       string   `json:"time-tag"`
		Observed  *float64 `json:"f10.7"`
		Predicted *float64 `json:"predicted_f10.7"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	flux := map[string]float64{}
	for _, r := range rows {
		v := r.Observed
		if v == nil {
			v = r.Predicted
		}
		if v == nil || !num.Finite(*v) || *v <= 0 || *v > maxSFU {
			continue
		}
		if _, err := time.Parse("2006-01", r.Tag); err != nil {
			continue
		}
		flux[r.Tag] = *v
	}
	if len(flux) == 0 {
		return nil, fmt.Errorf("%w: no month with a 10.7 cm flux", errNoFlux)
	}

	return flux, nil
}
