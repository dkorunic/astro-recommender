// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package geotz finds the IANA time zone of a location from its coordinates,
// offline, using github.com/ringsaturn/tzf (timezone-boundary-builder data,
// the same source as node-geo-tz).
package geotz

import (
	"errors"
	"fmt"

	"github.com/ringsaturn/tzf/v2"
)

var errNotFound = errors.New("no time zone found")

// Lookup returns the IANA time zone name at lat/lon (degrees). Open sea gets
// the nautical zone, e.g. Etc/GMT+2.
func Lookup(lat, lon float64) (string, error) {
	f, err := tzf.NewDefaultFinder() // ~150 ms: only called when -tz is not given
	if err != nil {
		return "", err
	}
	name := f.GetTimezoneName(lon, lat)
	if name == "" {
		return "", fmt.Errorf("%w at %.4f, %.4f", errNotFound, lat, lon)
	}

	return name, nil
}
