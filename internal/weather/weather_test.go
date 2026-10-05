// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
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
	// A null gust of its own keeps the hour (scored, not clear sky): it takes
	// the next entry's gust, or with none the mean wind.
	h.Gust[0], h.Gust[1], h.Wind[0] = nil, p(30), p(5)
	if out, _ = h.weather(); out[h.Time[0]].Gust != 30 {
		t.Errorf("null own gust: got %v (present %v), want 30", out[h.Time[0]].Gust, out[h.Time[0]] != (HourWeather{}))
	}
	h.Gust[1] = nil
	if out, _ = h.weather(); out[h.Time[0]].Gust != 5 {
		t.Errorf("null gusts: got %v, want the wind, 5", out[h.Time[0]].Gust)
	}
}

// A range Open-Meteo rejects (400) is retried once ending on its first day; a
// one-day range, a second rejection or any other status is the caller's error.
func TestGetRange(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("start_date")+".."+r.URL.Query().Get("end_date"))
		if r.URL.Query().Get("start_date") == "2026-10-01" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"reason":"slow down"}`))

			return
		}
		if r.URL.Query().Get("end_date") > "2026-10-20" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"reason":"end_date out of range"}`))

			return
		}
		_, _ = w.Write([]byte(`{"reason":""}`))
	}))
	defer srv.Close()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 20, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		start, end time.Time
		want       []string
		fail       bool
	}{
		{day(19), day(20), []string{"2026-10-19..2026-10-20"}, false},
		{day(20), day(21), []string{"2026-10-20..2026-10-21", "2026-10-20..2026-10-20"}, false},
		{day(21), day(22), []string{"2026-10-21..2026-10-22", "2026-10-21..2026-10-21"}, true},
		{day(21), day(21), []string{"2026-10-21..2026-10-21"}, true},
		{day(1), day(2), []string{"2026-10-01..2026-10-02"}, true}, // 429: no retry
	} {
		queries = nil
		var body struct{ Reason string }
		err := getRange(context.Background(), srv.URL+"?x=1", c.start, c.end, &body)
		if (err != nil) != c.fail || c.fail && !errors.Is(err, fetch.ErrStatus) || len(queries) != len(c.want) {
			t.Errorf("%s: err %v, queries %v, want %v", c.want[0], err, queries, c.want)

			continue
		}
		for i := range queries {
			if queries[i] != c.want[i] {
				t.Errorf("%s: queries %v, want %v", c.want[0], queries, c.want)
			}
		}
	}
}
