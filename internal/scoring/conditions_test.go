// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/weather"
)

// TestConditionsAt pins which forecast a minute reads: cloud and seeing the
// nearest hour's, rain its own hour's, 7Timer's block per minute.
func TestConditionsAt(t *testing.T) {
	t.Parallel()

	at := func(h, m int) time.Time { return time.Date(2026, 1, 15, h, m, 0, 0, time.UTC) }
	s := &Sky{
		Weather: map[int64]weather.HourWeather{
			at(18, 0).Unix(): {Cloud: 10, Seeing: 1, Precip: 0},
			at(19, 0).Unix(): {Cloud: 90, Seeing: 0, Precip: 1},
		},
		Astro: map[int64]weather.AstroBlock{
			at(18, 0).Unix(): {Seeing: 1},
			at(21, 0).Unix(): {Seeing: 8},
		},
	}
	for _, c := range []struct {
		t                     time.Time
		cloud, seeing, precip float64
	}{
		{at(18, 29), 10, 1, 0},
		{at(18, 30), 90, 1, 0}, // the nearest hour's cloud, its own's seeing where the nearest has none
		{at(19, 0), 90, weather.SeeingArcsec(1), 1},
		{at(19, 29), 90, weather.SeeingArcsec(1), 1},
		{at(19, 30), 90, weather.SeeingArcsec(8), 1}, // the next hour is not in the forecast: its own's
		{at(20, 0), math.NaN(), weather.SeeingArcsec(8), math.NaN()},
	} {
		got := s.ConditionsAt(c.t)
		if !same(got.Cloud, c.cloud) || got.Seeing != c.seeing || !same(got.Precip, c.precip) {
			t.Errorf("%s: %+v, want cloud %v seeing %v rain %v", c.t.Format("15:04"), got, c.cloud, c.seeing, c.precip)
		}
	}
}

func same(a, b float64) bool { return a == b || (math.IsNaN(a) && math.IsNaN(b)) }
