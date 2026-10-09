// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package geotz

import "testing"

func TestMutLookup(t *testing.T) {
	for _, c := range []struct {
		lat, lon float64
		want     string
	}{
		{45.81, 15.98, "Europe/Zagreb"},
		{-13.83, -171.76, "Pacific/Apia"},
		{40.71, -74.01, "America/New_York"},
		{0, -30, "Etc/GMT+2"},
	} {
		if got, err := Lookup(c.lat, c.lon); err != nil || got != c.want {
			t.Errorf("Lookup(%v, %v) = %q %v, want %q", c.lat, c.lon, got, err, c.want)
		}
	}
}
