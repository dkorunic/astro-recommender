// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package comets

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/num"
)

const (
	cobsURL      = "https://cobs.si/api/obs_list.api?format=json&from_date=%s&to_date=%s"
	cobsDays     = 30            // observations this far before the night count
	cobsCacheAge = 6 * time.Hour // observers report through the day
	// cobsKeep is how long a night's cached answer is kept before Prune
	// removes it: long past cobsCacheAge, so a run on another -date keeps
	// today's.
	cobsKeep = 7 * 24 * time.Hour
	// cobsMaxPages bounds the pages of 2500 records followed: 30 days held
	// 3236 records in the autumn of 2025, two pages.
	cobsMaxPages = 10
	// cobsMin is the observations a comet needs before they move its
	// magnitude: one report can be a misidentification or a typo.
	cobsMin = 3
	// minObsMag and maxObsMag bound a plausible reported magnitude.
	minObsMag, maxObsMag = -10.0, 25.0
)

// obs is one reported total magnitude.
type obs struct {
	t   time.Time
	mag float64
}

// Observed are recent observed magnitudes from COBS (the Comet Observation
// database), by the comet's MPC designation as comet.key spells it ("10P",
// "K22E020").
type Observed map[string][]obs

// FetchObserved returns the COBS observations of the cobsDays up to day
// (UTC), cached for cobsCacheAge; none in the browser, since COBS sends no
// CORS headers (checked at run time, like fetch's identify). COBS answers
// 2500 records a page, newest first, so every page is read (allPages):
// page 1 alone would drop a busy month's oldest observations.
func FetchObserved(ctx context.Context, day time.Time) (Observed, error) {
	if runtime.GOOS == "js" {
		return Observed{}, nil
	}
	to := day.UTC()
	from := to.AddDate(0, 0, -cobsDays)
	url := fmt.Sprintf(cobsURL, from.Format(time.DateOnly), to.Format(time.DateOnly))
	get := func(ctx context.Context) ([]byte, error) { return allPages(ctx, url) }
	fetch.Prune("cobs-*.json", cobsKeep) // one file per night, each a few MB

	return fetch.CachedGet(ctx, get, "cobs-"+to.Format(time.DateOnly)+"-all.json", cobsCacheAge, parseObserved)
}

// allPages downloads every page of COBS's answer at url, up to cobsMaxPages,
// as one document of its shape, which is cached as one: the pages run
// newest first, so as reports arrive records move onto the next page, and
// pages fetched hours apart would repeat or skip those at their boundaries.
// One failed page fails them all, rather than leave a part of the month.
func allPages(ctx context.Context, url string) ([]byte, error) {
	var objects []json.RawMessage
	pages := 1
	for p := 1; p <= min(pages, cobsMaxPages); p++ {
		u := url
		if p > 1 {
			u = fmt.Sprintf("%s&page=%d", url, p)
		}
		data, err := fetch.GetText(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("page %d of %d: %w", p, pages, err)
		}
		var body struct {
			Objects []json.RawMessage `json:"objects"`
			Info    struct {
				Pages int `json:"pages"`
			} `json:"info"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return nil, fmt.Errorf("%w: cobs page %d: %w", errComets, p, err)
		}
		if p == 1 {
			pages = body.Info.Pages
		}
		objects = append(objects, body.Objects...)
	}

	return json.Marshal(map[string][]json.RawMessage{"objects": objects})
}

// parseObserved reads COBS's obs_list JSON; a record without a designation,
// date or numeric magnitude in range is skipped.
func parseObserved(data []byte) (Observed, error) {
	//nolint:tagliatelle // COBS's field names
	var body struct {
		Objects []struct {
			Comet struct {
				MPCName   string `json:"mpc_name"`
				Component string `json:"component"`
			} `json:"comet"`
			Date      string `json:"obs_date"`
			Magnitude string `json:"magnitude"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("%w: cobs: %w", errComets, err)
	}
	out := Observed{}
	for _, o := range body.Objects {
		t, err := time.Parse(time.DateTime, o.Date)
		mag, err2 := strconv.ParseFloat(strings.TrimSpace(o.Magnitude), 64)
		if o.Comet.MPCName == "" || err != nil || err2 != nil || !num.Finite(mag) || mag < minObsMag || mag > maxObsMag {
			continue
		}
		// A fragment (73P-BT) is its own comet, keyed as comet.key spells
		// it, not its parent's.
		key := o.Comet.MPCName + strings.ToLower(strings.TrimSpace(o.Comet.Component))
		out[key] = append(out[key], obs{t, mag})
	}

	return out, nil
}

// offset returns the median of the observed magnitudes less c's
// H + 5 log Δ + 2.5 K log r at each, and whether c has cobsMin of them:
// MPC's H and K are fitted once over an apparition and run 1-4 mag off
// what observers see weeks later (10P in October 2026: 14.7 against 10.6).
func (c *comet) offset(o Observed) (float64, bool) {
	var d []float64
	for _, x := range o[c.key] {
		if m, ok := c.mag(x.t); ok {
			d = append(d, x.mag-m)
		}
	}
	if len(d) < cobsMin {
		return 0, false
	}
	slices.Sort(d)
	if n := len(d); n%2 == 0 {
		return (d[n/2-1] + d[n/2]) / 2, true
	}

	return d[len(d)/2], true
}
