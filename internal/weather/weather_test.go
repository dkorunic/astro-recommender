// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package weather

import (
	"testing"
	"time"
)

func TestAstroKey(t *testing.T) {
	if k := AstroKey(time.Date(2026, 10, 5, 22, 0, 0, 0, time.UTC)); k != time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("astroKey(22:00) = %v, want 21:00", time.Unix(k, 0).UTC())
	}
}

func TestGustShift(t *testing.T) {
	// Gusts at 21:00 cover 20:00-21:00, so they belong to the hour starting at 20:00.
	p := func(v float64) *float64 { return &v }
	zero := []*float64{p(0), p(0), p(0)}
	h := hourly{
		Time: []int64{72000, 75600, 79200},
		Low:  zero, Mid: zero, High: zero, Temp: zero, DewPoint: zero, Wind: zero,
		Gust: []*float64{p(10), p(30), p(50)},
	}
	out, err := h.weather()
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{30, 50, 50} {
		if got := out[h.Time[i]].Gust; got != want {
			t.Errorf("hour %d: gust %v, want %v", i, got, want)
		}
	}
	// A null next gust leaves the hour its own value rather than skipping it.
	h.Gust[1] = nil
	if out, _ = h.weather(); out[h.Time[0]].Gust != 10 {
		t.Errorf("null next gust: got %v, want 10", out[h.Time[0]].Gust)
	}
}
