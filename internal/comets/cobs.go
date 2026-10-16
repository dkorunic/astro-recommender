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
// CORS headers (checked at run time, like fetch's identify).
func FetchObserved(ctx context.Context, day time.Time) (Observed, error) {
	if runtime.GOOS == "js" {
		return Observed{}, nil
	}
	to := day.UTC()
	from := to.AddDate(0, 0, -cobsDays)
	url := fmt.Sprintf(cobsURL, from.Format(time.DateOnly), to.Format(time.DateOnly))

	return fetch.Cached(ctx, url, "cobs-"+to.Format(time.DateOnly)+".json", cobsCacheAge, parseObserved)
}

// parseObserved reads COBS's obs_list JSON; a record without a designation,
// date or numeric magnitude in range is skipped.
func parseObserved(data []byte) (Observed, error) {
	//nolint:tagliatelle // COBS's field names
	var body struct {
		Objects []struct {
			Comet struct {
				MPCName string `json:"mpc_name"`
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
		out[o.Comet.MPCName] = append(out[o.Comet.MPCName], obs{t, mag})
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
