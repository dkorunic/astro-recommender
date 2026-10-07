// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package sqm looks up a location's zenith sky brightness from the
// DarkSkySites light pollution map (https://www.darkskysites.com/api-access).
package sqm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
)

// KeyEnv names the environment variable holding the DarkSkySites API key.
const KeyEnv = "DARKSKYSITES_API_KEY"

var errDarkSky = errors.New("darkskysites")

// baseURL is a variable so that tests can point it at a local server.
var baseURL = "https://www.darkskysites.com/api/sqm"

// proxyURL is the Cloudflare Worker the browser build goes through:
// DarkSkySites sends no CORS headers, and the Worker holds the API key.
const proxyURL = "https://astro-recommender-proxy.dkorunic.workers.dev/sqm"

// Key returns the API key from KeyEnv; empty means no lookup, except in the
// browser, where Lookup sends none and the proxy adds its own.
func Key() string {
	if runtime.GOOS == "js" {
		return "proxy"
	}

	return os.Getenv(KeyEnv)
}

// Lookup returns the modelled zenith sky brightness in mag/arcsec² at the
// location and a source label (attribution and monthly dataset) for display.
func Lookup(ctx context.Context, key string, lat, lon float64) (float64, string, error) {
	// Same ~1 km rounding as the other services: approximate on purpose.
	url := fmt.Sprintf("%s?lat=%.2f&lng=%.2f", baseURL, lat, lon)
	hdr := http.Header{"X-Api-Key": {key}}
	if runtime.GOOS == "js" {
		// No key header: it would only force a CORS preflight.
		url, hdr = fmt.Sprintf("%s?lat=%.2f&lng=%.2f", proxyURL, lat, lon), nil
	}
	var body struct {
		Attribution string `json:"attribution"`
		Error       string `json:"error"`
		Dataset     struct {
			ID string `json:"id"`
		} `json:"dataset"`
		SQM float64 `json:"sqm"`
	}
	err := fetch.GetJSONHeader(ctx, url, hdr, &body)
	// The error field carries the reason whatever the status (quota, no data).
	if body.Error != "" {
		if err != nil {
			return 0, "", fmt.Errorf("%w: %w: %s", errDarkSky, err, sanitize.Text(body.Error))
		}

		return 0, "", fmt.Errorf("%w: %s", errDarkSky, sanitize.Text(body.Error))
	}
	if err != nil {
		return 0, "", fmt.Errorf("%w: %w", errDarkSky, err)
	}
	if !num.Finite(body.SQM) || body.SQM < atmos.MinSQM || body.SQM > atmos.MaxSQM {
		return 0, "", fmt.Errorf("%w: implausible SQM %g", errDarkSky, body.SQM)
	}
	source := sanitize.Text(cmp.Or(body.Attribution, "darkskysites.com"))
	if id := sanitize.Text(body.Dataset.ID); id != "" {
		source += " " + id
	}

	return body.SQM, source, nil
}
