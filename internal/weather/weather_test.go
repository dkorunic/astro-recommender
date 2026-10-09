// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package weather

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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

func TestSeeingLabel(t *testing.T) {
	for s, want := range map[int]string{1: `<0.5"`, 8: `>2.5"`, 0: "-", 9: "-", -9999: "-"} {
		if got := SeeingLabel(s); got != want {
			t.Errorf("SeeingLabel(%d) = %q, want %q", s, got, want)
		}
	}
}

// profile builds the six standard levels from a temperature (°C) and a wind
// speed (km/h) per level, all winds from the same direction.
func profile(temp, wind []float64, dir float64) []Level {
	heights := []float64{1500, 3000, 5600, 9200, 10400, 11800}
	var out []Level
	for i, p := range pressures {
		out = append(out, Level{P: float64(p), Z: heights[i], T: temp[i], Wind: wind[i], Dir: dir})
	}

	return out
}

func TestSeeing(t *testing.T) {
	// A stable lapse rate, little shear and a weak jet: steady. A dry-adiabatic
	// (negative Ri) lowest layer, 18 m/s/km shear and a 260 km/h jet: turbulent.
	stable := profile([]float64{8, -5, -20, -45, -52, -58}, []float64{20, 20, 25, 30, 30, 30}, 270)
	turbulent := profile([]float64{20, 0, -20, -45, -52, -58}, []float64{30, 90, 180, 180, 260, 260}, 270)
	// A stable profile under a 150 km/h jet with 7 m/s/km shear: poor.
	jetty := profile([]float64{8, -5, -20, -45, -52, -58}, []float64{20, 20, 60, 150, 150, 150}, 270)
	// The steady profile with a convective lowest layer: the Ri flag alone adds a class.
	convective := profile([]float64{20, 0, -20, -45, -52, -58}, []float64{20, 20, 25, 30, 30, 30}, 270)
	for _, tc := range []struct {
		name   string
		levels []Level
		gust   float64
		want   int
	}{
		{"no profile", nil, 50, 0},
		{"two levels", stable[:2], 0, 0},
		{"no jet level", turbulent[:3], 0, 0},
		{"stable", stable, 0, 1},
		{"stable, gusty", stable, 31, 2},
		{"convective layer", convective, 0, 2},
		{"jet", jetty, 0, 4},
		{"turbulent", turbulent, 0, 5},
		{"turbulent, gusty", turbulent, 50, 5},
	} {
		if got := Seeing(HourWeather{Levels: tc.levels, Gust: tc.gust}); got != tc.want {
			t.Errorf("Seeing(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
	// Direction matters: the same speeds veering 90° between levels are shear.
	veering := profile([]float64{8, -5, -20, -45, -52, -58}, []float64{60, 60, 60, 60, 60, 60}, 270)
	for i := range veering {
		veering[i].Dir = float64(270 + 90*(i%2))
	}
	if same, veer := Seeing(HourWeather{Levels: stable}), Seeing(HourWeather{Levels: veering}); veer <= same {
		t.Errorf("veering winds class %d, want worse than the steady %d", veer, same)
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
	out, err := h.weather(math.NaN())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{30, 50, 50} {
		if got := out[h.Time[i]].Gust; got != want {
			t.Errorf("hour %d: gust %v, want %v", i, got, want)
		}
	}
	// Gust is optional per hour but must still cover every hour.
	short := h
	short.Gust = h.Gust[:2]
	if _, err := short.weather(math.NaN()); err == nil {
		t.Error("short gust series accepted")
	}
	// A null next gust leaves the hour its own value rather than skipping it.
	h.Gust[1] = nil
	if out, _ = h.weather(math.NaN()); out[h.Time[0]].Gust != 10 {
		t.Errorf("null next gust: got %v, want 10", out[h.Time[0]].Gust)
	}
	// A null gust of its own keeps the hour (scored, not clear sky): it takes
	// the next entry's gust, or with none the mean wind.
	h.Gust[0], h.Gust[1], h.Wind[0] = nil, p(30), p(5)
	if out, _ = h.weather(math.NaN()); out[h.Time[0]].Gust != 30 {
		_, present := out[h.Time[0]]
		t.Errorf("null own gust: got %v (present %v), want 30", out[h.Time[0]].Gust, present)
	}
	// Without precipitation and upper-air series every hour stays, unknown.
	if out, err = h.weather(math.NaN()); err != nil || len(out) != 3 || !math.IsNaN(out[h.Time[0]].Precip) || out[h.Time[0]].Levels != nil {
		t.Errorf("no precipitation/levels: err %v, %d hours, precip %v, levels %v", err, len(out), out[h.Time[0]].Precip, out[h.Time[0]].Levels)
	}
	// Precipitation is the preceding hour's sum, shifted like the gusts, but
	// an hour without a next entry stays unknown rather than taking its own
	// value, which is the previous hour's rain.
	h.Precip = []*float64{p(0), p(0.3)}
	if out, _ = h.weather(math.NaN()); out[h.Time[0]].Precip != 0.3 || !math.IsNaN(out[h.Time[1]].Precip) || !math.IsNaN(out[h.Time[2]].Precip) {
		t.Errorf("precipitation: got %v, %v, %v, want 0.3, NaN, NaN", out[h.Time[0]].Precip, out[h.Time[1]].Precip, out[h.Time[2]].Precip)
	}
	// A level is kept only with all four values; a null drops that level alone.
	for k := range h.Level {
		h.Level[k] = levelSeries{Z: []*float64{p(1500), p(1500), nil}, T: zero, Wind: zero, Dir: zero}
	}
	h.Level[1].T = []*float64{p(1), nil, p(1)}
	h.Level[2].Wind = []*float64{p(0), p(-9999), p(0)} // a sentinel speed drops the level like a null
	h.Level[3].Dir = []*float64{p(0), p(0), p(-9999)}  // and so does a sentinel direction
	out, _ = h.weather(math.NaN())
	if l := out[h.Time[0]].Levels; len(l) != len(pressures) || l[0].P != 850 || l[0].Z != 1500 || l[1].T != 1 {
		t.Errorf("hour 0 levels: %+v", l)
	}
	if l := out[h.Time[1]].Levels; len(l) != len(pressures)-2 || l[1].P != 300 {
		t.Errorf("hour 1 levels (null 700 hPa temperature, -9999 500 hPa wind): %+v", l)
	}
	if l := out[h.Time[2]].Levels; l != nil {
		t.Errorf("hour 2 levels (null heights): %+v", l)
	}
	for k := range h.Level {
		h.Level[k].Z[2] = p(1500)
	}
	out, _ = h.weather(math.NaN())
	if l := out[h.Time[2]].Levels; len(l) != len(pressures)-1 || l[3].P != 250 {
		t.Errorf("hour 2 levels (-9999 300 hPa direction): %+v", l)
	}
	// A level below the site is underground: dropped, one at it kept.
	if l := mustWeather(t, &h, 1500)[h.Time[0]].Levels; len(l) != len(pressures) {
		t.Errorf("site at the levels' 1500 m: %d levels, want all %d", len(l), len(pressures))
	}
	h.Level[0].Z[0] = p(1400)
	if l := mustWeather(t, &h, 1450)[h.Time[0]].Levels; len(l) != len(pressures)-1 || l[0].P != 700 {
		t.Errorf("site at 1450 m, 850 hPa at 1400 m: %+v, want 850 hPa dropped", l)
	}
	h.Gust[1] = nil
	if out, _ = h.weather(math.NaN()); out[h.Time[0]].Gust != 5 {
		t.Errorf("null gusts: got %v, want the wind, 5", out[h.Time[0]].Gust)
	}
}

// decodeHourly fills the pressure levels from the keys levelKeys names, a
// level missing from the response stays empty, and a block the level decode
// cannot read as series (a non-array value) still yields the weather.
func TestDecodeHourly(t *testing.T) {
	h, err := decodeHourly(json.RawMessage(`{"time":[0,3600],"cloud_cover_low":[1,2],"geopotential_height_850hPa":[1500,1510],
		"temperature_850hPa":[8,7],"wind_speed_850hPa":[20,25],"wind_direction_850hPa":[270,280],"wind_speed_200hPa":[100,null]}`))
	if err != nil {
		t.Fatal(err)
	}
	if h.Time[1] != 3600 || *h.Low[1] != 2 || *h.Level[0].Z[1] != 1510 || *h.Level[0].T[0] != 8 || *h.Level[0].Wind[1] != 25 || *h.Level[0].Dir[1] != 280 {
		t.Errorf("850 hPa: %+v", h.Level[0])
	}
	if h.Level[1].Z != nil || h.Level[5].Wind == nil || h.Level[5].Wind[1] != nil || h.Level[5].Z != nil {
		t.Errorf("700 hPa (absent) %+v, 200 hPa (wind only) %+v", h.Level[1], h.Level[5])
	}
	if h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"units":"percent"}`)); err != nil || len(h.Time) != 1 || h.Level[0].Z != nil {
		t.Errorf("non-series value: err %v, %d hours, levels %+v", err, len(h.Time), h.Level[0])
	}
	// An unreadable level series costs that series only; a named one is an error.
	h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"temperature_700hPa":"oops","wind_speed_700hPa":[1,"x"],"wind_direction_700hPa":[90]}`))
	if err != nil || len(h.Time) != 1 || h.Level[1].T != nil || h.Level[1].Wind != nil || len(h.Level[1].Dir) != 1 {
		t.Errorf("bad level series: err %v, 700 hPa %+v", err, h.Level[1])
	}
	if _, err = decodeHourly(json.RawMessage(`{"time":[0],"precipitation":"oops"}`)); err == nil {
		t.Error("bad precipitation series accepted")
	}
	if h, err = decodeHourly(nil); err != nil || h.Time != nil {
		t.Errorf("no block: err %v, %+v", err, h)
	}
	if _, err = decodeHourly(json.RawMessage(`{"time":"x"}`)); err == nil {
		t.Error("malformed block accepted")
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
	fetch.RetryDelay = 0
	t.Cleanup(func() { fetch.RetryDelay = time.Second })
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
		{day(1), day(2), []string{"2026-10-01..2026-10-02", "2026-10-01..2026-10-02"}, true}, // 429: fetch retries as is, never narrowed
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

func mustWeather(t *testing.T, h *hourly, elev float64) map[int64]HourWeather {
	t.Helper()
	out, err := h.weather(elev)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// Sentinel and overflowing level values are not data: one huge wind and
// height made the shear Inf/Inf = NaN and Seeing "steady" on arm64.
func TestPlausible(t *testing.T) {
	ok := Level{P: 200, Z: 11800, T: -55, Wind: 150, Dir: 270}
	if !plausible(ok) {
		t.Fatalf("%+v rejected", ok)
	}
	for name, l := range map[string]Level{
		"huge height": {P: 200, Z: 1e308, T: -55, Wind: 150, Dir: 270},
		"huge wind":   {P: 200, Z: 11800, T: -55, Wind: 1e308, Dir: 270},
		"huge temp":   {P: 200, Z: 11800, T: 1e308, Wind: 150, Dir: 270},
		"sentinel":    {P: 200, Z: 11800, T: -9999, Wind: 150, Dir: 270},
		"direction":   {P: 200, Z: 11800, T: -55, Wind: 150, Dir: 361},
	} {
		if plausible(l) {
			t.Errorf("%s: %+v accepted", name, l)
		}
	}
}
