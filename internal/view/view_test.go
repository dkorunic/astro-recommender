// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package view

import (
	"testing"
	"time"
)

// The page hands Zone to Intl, which throws a RangeError on Go's "Local"
// and "Factory" (-tz Local, -tz Factory) and so stops the whole render.
func TestZone(t *testing.T) {
	factory, err := time.LoadLocation("Factory")
	if err != nil {
		t.Skip(err)
	}
	zagreb, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Skip(err)
	}
	for loc, want := range map[*time.Location]string{time.Local: "", factory: "UTC", zagreb: "Europe/Zagreb", time.UTC: "UTC"} {
		if got := zone(loc); got != want {
			t.Errorf("zone(%s) = %q, want %q", loc, got, want)
		}
	}
}
