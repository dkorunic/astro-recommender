// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package geocode reverse geocodes a location to an approximate place name
// (OpenStreetMap Nominatim).
package geocode

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
)

var errNominatim = errors.New("nominatim")

// baseURL is a variable so that tests can point it at a local server.
var baseURL = "https://nominatim.openstreetmap.org/reverse"

// Reverse returns an approximate (suburb-level) place name from
// OpenStreetMap Nominatim. Usage policy: identify via User-Agent, <=1 req/s.
func Reverse(ctx context.Context, lat, lon float64) (string, error) {
	// Same ~1 km rounding as forecast(): approximate on purpose.
	url := fmt.Sprintf("%s?format=jsonv2&lat=%.2f&lon=%.2f&zoom=14", baseURL, lat, lon)
	//nolint:tagliatelle // Nominatim's field names
	var body struct {
		DisplayName string `json:"display_name"`
		Error       string `json:"error"`
	}
	if err := fetch.GetJSON(ctx, url, &body); err != nil {
		return "", fmt.Errorf("%w: %w", errNominatim, err)
	}
	if body.DisplayName == "" {
		return "", fmt.Errorf("%w: %s", errNominatim, sanitize.Text(cmp.Or(body.Error, "no result")))
	}

	return sanitize.Text(body.DisplayName), nil
}
