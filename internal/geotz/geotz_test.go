// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package geotz

import (
	"errors"
	"testing"
	"time"
)

func TestLookup(t *testing.T) {
	for _, c := range []struct {
		lat, lon float64
		want     string
	}{
		{45.83, 16.07, "Europe/Zagreb"},
		{-33.87, 151.21, "Australia/Sydney"},
		{37.77, -122.42, "America/Los_Angeles"},
		{69.65, 18.96, "Europe/Oslo"},
		{0, -30, "Etc/GMT+2"}, // open Atlantic
	} {
		got, err := Lookup(c.lat, c.lon)
		if err != nil || got != c.want {
			t.Errorf("Lookup(%v, %v) = %q, %v; want %q", c.lat, c.lon, got, err, c.want)
		}
		if _, err := time.LoadLocation(got); err != nil {
			t.Errorf("%q does not load: %v", got, err)
		}
	}
}

// Off the globe the finder has no polygon, not even a nautical zone.
func TestLookupNotFound(t *testing.T) {
	for _, c := range [][2]float64{{91, 0}, {0, 181}} {
		if got, err := Lookup(c[0], c[1]); !errors.Is(err, errNotFound) || got != "" {
			t.Errorf("Lookup%v = %q, %v; want errNotFound", c, got, err)
		}
	}
}
