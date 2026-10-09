// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package weather

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
)

// mutTransport answers every request in-process: the package's URLs are
// constants, so the default transport is swapped for the test.
type mutTransport struct {
	h    http.HandlerFunc
	mu   sync.Mutex
	reqs []*url.URL
}

func (m *mutTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, r.URL)
	m.mu.Unlock()
	rec := httptest.NewRecorder()
	m.h(rec, r)

	return rec.Result(), nil
}

func mutServe(t *testing.T, h http.HandlerFunc) *mutTransport {
	t.Helper()
	m := &mutTransport{h: h}
	old, oldDelay := http.DefaultTransport, fetch.RetryDelay
	http.DefaultTransport, fetch.RetryDelay = m, 0
	t.Cleanup(func() { http.DefaultTransport, fetch.RetryDelay = old, oldDelay })

	return m
}

//go:fix inline
func mutF(v float64) *float64 { return new(v) }

func mutNear(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

const mutT0 = int64(1791748800) // 2026-10-12 00:00 UTC

func mutHourly(n int) hourly {
	h := hourly{}
	for i := range n {
		h.Time = append(h.Time, mutT0+int64(i)*3600)
		for _, s := range []*[]*float64{&h.Low, &h.Mid, &h.High, &h.Temp, &h.DewPoint, &h.Wind, &h.Gust, &h.Precip} {
			*s = append(*s, mutF(0))
		}
	}

	return h
}

func TestMutWeatherCloudAndShift(t *testing.T) {
	h := mutHourly(3)
	h.Low[0], h.Mid[0], h.High[0] = mutF(50), mutF(0), mutF(100)
	h.Low[1], h.Mid[1], h.High[1] = mutF(20), mutF(50), mutF(40)
	h.Wind = []*float64{mutF(5), mutF(6), mutF(7)}
	h.Gust = []*float64{mutF(10), mutF(20), mutF(30)}
	h.Precip = []*float64{mutF(9), new(0.3), new(0.7)}
	out, err := h.weather(math.NaN())
	if err != nil {
		t.Fatal(err)
	}
	// 1 − (1−0.5)(1−0)(1−1/2) = 75 %; 1 − 0.8·0.5·0.8 = 68 %.
	mutNear(t, "cloud0", out[mutT0].Cloud, 75, 1e-9)
	mutNear(t, "cloud1", out[mutT0+3600].Cloud, 68, 1e-9)
	if out[mutT0].Low != 50 || out[mutT0].High != 100 {
		t.Errorf("raw layers %+v", out[mutT0])
	}
	// Gust and precipitation of the hour starting at t are in the next entry.
	if g := out[mutT0].Gust; g != 20 {
		t.Errorf("gust0 = %v, want 20", g)
	}
	if g := out[mutT0+7200].Gust; g != 30 {
		t.Errorf("last gust = %v, want its own 30", g)
	}
	if p := out[mutT0].Precip; p != 0.3 {
		t.Errorf("precip0 = %v, want 0.3", p)
	}
	if p := out[mutT0+7200].Precip; !math.IsNaN(p) {
		t.Errorf("last precip = %v, want NaN", p)
	}
	if w := out[mutT0+3600].Wind; w != 6 {
		t.Errorf("wind1 = %v", w)
	}
}

func TestMutWeatherGaps(t *testing.T) {
	h := mutHourly(4)
	h.Time[2] = mutT0 + 5*3600 // a gap: hour 1's next entry is not its next hour
	h.Gust = []*float64{nil, mutF(40), mutF(50), nil}
	h.Wind = []*float64{mutF(3), mutF(4), mutF(5), mutF(6)}
	h.Precip = []*float64{mutF(0), mutF(-1), mutF(2), mutF(3)}
	out, err := h.weather(math.NaN())
	if err != nil {
		t.Fatal(err)
	}
	if g := out[mutT0].Gust; g != 40 {
		t.Errorf("gust0 = %v, want 40", g)
	}
	if g := out[mutT0+3600].Gust; g != 40 {
		t.Errorf("gust before gap = %v, want own 40", g)
	}
	if g := out[mutT0+3*3600].Gust; g != 6 {
		t.Errorf("gust without data = %v, want mean wind 6", g)
	}
	if p := out[mutT0].Precip; !math.IsNaN(p) {
		t.Errorf("negative precip = %v, want NaN", p)
	}
	if p := out[mutT0+3600].Precip; !math.IsNaN(p) {
		t.Errorf("precip across gap = %v, want NaN", p)
	}
	// Null or out-of-range cloud drops the hour.
	h = mutHourly(3)
	h.Mid[0] = nil
	h.High[1] = mutF(101)
	h.Gust[2] = nil
	out, _ = h.weather(math.NaN())
	if _, ok := out[mutT0]; ok {
		t.Error("null hour kept")
	}
	if _, ok := out[mutT0+3600]; ok {
		t.Error("101 % hour kept")
	}
	if _, ok := out[mutT0+7200]; !ok {
		t.Error("null gust dropped the hour")
	}
	h = mutHourly(3)
	h.Low[1] = mutF(-1)
	if out, _ = h.weather(math.NaN()); len(out) != 2 {
		t.Errorf("negative cloud kept: %d hours", len(out))
	}
	// Malformed series.
	for _, f := range []func(*hourly){
		func(h *hourly) { h.Gust = h.Gust[:2] },
		func(h *hourly) { h.DewPoint = h.DewPoint[:2] },
		func(h *hourly) { h.Wind = h.Wind[:2] },
	} {
		h = mutHourly(3)
		f(&h)
		if _, err := h.weather(math.NaN()); err == nil {
			t.Error("short series accepted")
		}
	}
	// A short precipitation series only costs the rain gate.
	h = mutHourly(3)
	h.Precip = h.Precip[:1]
	if out, err := h.weather(math.NaN()); err != nil || len(out) != 3 {
		t.Errorf("short precip: %d %v", len(out), err)
	}
}

func mutLevels(h *hourly, i int, vals [len(pressures)][4]float64) {
	for k := range pressures {
		l := &h.Level[k]
		for len(l.Z) <= i {
			l.Z, l.T, l.Wind, l.Dir = append(l.Z, nil), append(l.T, nil), append(l.Wind, nil), append(l.Dir, nil)
		}
		l.Z[i], l.T[i], l.Wind[i], l.Dir[i] = new(vals[k][0]), new(vals[k][1]), new(vals[k][2]), new(vals[k][3])
	}
}

func TestMutWeatherLevels(t *testing.T) {
	h := mutHourly(1)
	mutLevels(&h, 0, [len(pressures)][4]float64{
		{1500, 5, 10, 0}, {3000, -5, 20, 0}, {5600, -20, -1, 0}, {9200, -45, 60, 400}, {10400, -50, 80, 0}, {11800, -55, 90, 0},
	})
	out, _ := h.weather(2000)
	got := out[mutT0].Levels
	// 850 is below the 2000 m site, 500 has negative wind, 300 a 400° direction.
	if len(got) != 3 || got[0].P != 700 || got[1].P != 250 || got[2].P != 200 || got[0].Z != 3000 || got[0].Wind != 20 {
		t.Errorf("levels %+v", got)
	}
	out, _ = h.weather(math.NaN())
	if l := out[mutT0].Levels; len(l) != 4 || l[0].P != 850 {
		t.Errorf("NaN elevation levels %+v", l)
	}
	out, _ = h.weather(1500)
	if l := out[mutT0].Levels; len(l) != 4 {
		t.Errorf("level at the site's own height dropped: %+v", l)
	}
	for _, l := range []Level{{P: 500, Z: 30001, T: 0}, {P: 500, Z: -1001}, {P: 500, T: 61}, {P: 500, T: -151}, {P: 500, Wind: 1001}, {P: 500, Dir: -1}} {
		if plausible(l) {
			t.Errorf("plausible(%+v)", l)
		}
	}
	if !plausible(Level{P: 200, Z: 30000, T: -150, Wind: 1000, Dir: 360}) || !plausible(Level{P: 850, Z: -1000, T: 60}) {
		t.Error("edge levels rejected")
	}
}

func TestMutDecodeHourly(t *testing.T) {
	raw := `{"time":[1,2],"cloud_cover_low":[10,null],"precipitation":[0.5,0],
		"geopotential_height_850hPa":[1500,1510],"temperature_850hPa":"bad",
		"wind_speed_700hPa":[30,31]}`
	h, err := decodeHourly(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Time) != 2 || *h.Low[0] != 10 || h.Low[1] != nil || *h.Precip[0] != 0.5 {
		t.Errorf("decoded %+v", h)
	}
	if len(h.Level[0].Z) != 2 || *h.Level[0].Z[1] != 1510 || h.Level[0].T != nil || *h.Level[1].Wind[0] != 30 {
		t.Errorf("levels %+v", h.Level)
	}
	if _, err := decodeHourly(json.RawMessage(`{"time":["x"]}`)); err == nil {
		t.Error("bad time accepted")
	}
	if _, err := decodeHourly(json.RawMessage(`{"cloud_cover_mid":{}}`)); err == nil {
		t.Error("bad series accepted")
	}
	if h, err := decodeHourly(nil); err != nil || h.Time != nil {
		t.Errorf("empty: %v %v", h, err)
	}
}

func TestMutElevation(t *testing.T) {
	for _, c := range []struct {
		p    *float64
		want float64
	}{{mutF(120), 120}, {mutF(-500), -500}, {mutF(9000), 9000}, {mutF(-501), math.NaN()}, {mutF(9001), math.NaN()}, {new(math.Inf(1)), math.NaN()}, {nil, math.NaN()}} {
		got := elevation(c.p)
		if !(got == c.want || math.IsNaN(got) && math.IsNaN(c.want)) {
			t.Errorf("elevation = %v, want %v", got, c.want)
		}
	}
}

// Potential temperature: θ = T (1000/p)^(R/cp), R/cp = 0.286.
func TestMutThetaWindQuality(t *testing.T) {
	mutNear(t, "theta500", theta(Level{P: 500, T: -20}), 253.15*math.Pow(2, 0.286), 1e-9)
	mutNear(t, "theta1000", theta(Level{P: 1000, T: 15}), 288.15, 1e-9)
	// From the north at 36 km/h: blowing south at 10 m/s.
	u, v := wind(Level{Wind: 36, Dir: 0})
	mutNear(t, "u N", u, 0, 1e-9)
	mutNear(t, "v N", v, -10, 1e-9)
	u, v = wind(Level{Wind: 36, Dir: 90})
	mutNear(t, "u E", u, -10, 1e-9)
	mutNear(t, "v E", v, 0, 1e-9)
	mutNear(t, "q good", quality(2, 2.5, 18), 100, 0)
	mutNear(t, "q bad", quality(20, 2.5, 18), 0, 0)
	mutNear(t, "q mid", quality(10.25, 2.5, 18), 50, 1e-9)
}

// mutProfile builds six levels from 850 to 200 hPa, 1.5 km apart, warming in
// θ (stable) unless temps are given.
func mutProfile(winds, dirs [6]float64, temps *[6]float64) []Level {
	z := [6]float64{1500, 3000, 5600, 9200, 10400, 11800}
	tt := [6]float64{10, 5, -10, -30, -35, -40}
	if temps != nil {
		tt = *temps
	}
	var out []Level
	for i, p := range pressures {
		out = append(out, Level{P: float64(p), Z: z[i], T: tt[i], Wind: winds[i], Dir: dirs[i]})
	}

	return out
}

func TestMutSeeing(t *testing.T) {
	calm := [6]float64{10, 10, 10, 10, 10, 10}
	var north [6]float64
	if s := Seeing(HourWeather{Levels: mutProfile(calm, north, nil)}); s != 1 {
		t.Errorf("calm stable = %d, want 1", s)
	}
	// Uniform 144 km/h (40 m/s) everywhere: no shear (q 100), jet q 0, so
	// q = 50 and class 1 + 50/20 = 3.
	jet := [6]float64{144, 144, 144, 144, 144, 144}
	if s := Seeing(HourWeather{Levels: mutProfile(jet, north, nil)}); s != 3 {
		t.Errorf("uniform jet = %d, want 3", s)
	}
	// Strong wind below 300 hPa only does not count as jet.
	low := [6]float64{144, 144, 144, 10, 10, 10}
	lowLv := mutProfile(low, north, nil)
	lowLv[3].Z = lowLv[2].Z + 30 // below the 50 m step: that pair's shear is skipped
	lowLv[4].Z, lowLv[5].Z = lowLv[3].Z+1500, lowLv[3].Z+3000
	if s := Seeing(HourWeather{Levels: lowLv}); s != 1 {
		t.Errorf("low wind = %d, want 1", s)
	}
	// Gusts above 30 km/h add one; 30 does not.
	if s := Seeing(HourWeather{Levels: mutProfile(jet, north, nil), Gust: 31}); s != 4 {
		t.Errorf("gusty = %d, want 4", s)
	}
	if s := Seeing(HourWeather{Levels: mutProfile(jet, north, nil), Gust: 30}); s != 3 {
		t.Errorf("gust 30 = %d, want 3", s)
	}
	// Veering 180° at 36 km/h between 850 and 700 (1.5 km): 20 m/s / 1.5 km
	// = 13.3 m/s/km, q_shear = 100(1 − 10.83/15.5) = 30.1, q = 65, class 2.
	wind := [6]float64{36, 36, 10, 10, 10, 10}
	dirs := [6]float64{0, 180, 180, 180, 180, 180}
	if s := Seeing(HourWeather{Levels: mutProfile(wind, dirs, nil)}); s != 2 {
		t.Errorf("veer = %d, want 2", s)
	}
	// θ falling with height (superadiabatic): Ri < 0, one class worse.
	hot := [6]float64{40, -10, -40, -70, -80, -90}
	if s := Seeing(HourWeather{Levels: mutProfile(calm, north, &hot)}); s != 2 {
		t.Errorf("convective = %d, want 2", s)
	}
	// Everything bad caps at 5.
	worst := [6]float64{0, 300, 0, 300, 300, 300}
	if s := Seeing(HourWeather{Levels: mutProfile(worst, dirs, &hot), Gust: 80}); s != 5 {
		t.Errorf("worst = %d, want 5", s)
	}
	lv := mutProfile(calm, north, nil)
	if s := Seeing(HourWeather{Levels: lv[:2]}); s != 0 {
		t.Errorf("two levels = %d", s)
	}
	if s := Seeing(HourWeather{Levels: lv[:3]}); s != 0 {
		t.Errorf("no level at 300 hPa = %d", s)
	}
	if s := Seeing(HourWeather{Levels: lv[2:4]}); s != 0 {
		t.Errorf("two levels up to 300 hPa = %d", s)
	}
	if s := Seeing(HourWeather{Levels: lv[1:4]}); s != 1 {
		t.Errorf("700-300 = %d, want 1", s)
	}
}

func TestMutAstroKeyLabel(t *testing.T) {
	zg := time.FixedZone("CEST", 2*3600)
	for _, c := range [][2]time.Time{
		{time.Date(2026, 10, 12, 1, 29, 0, 0, time.UTC), time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 10, 12, 1, 31, 0, 0, time.UTC), time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)},
		{time.Date(2026, 10, 12, 23, 0, 0, 0, zg), time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC)},
	} {
		if got := AstroKey(c[0]); got != c[1].Unix() {
			t.Errorf("AstroKey(%v) = %v, want %v", c[0], time.Unix(got, 0).UTC(), c[1])
		}
	}
	for s, want := range map[int]string{0: "-", 1: `<0.5"`, 4: `1-1.25"`, 8: `>2.5"`, 9: "-", -1: "-"} {
		if got := SeeingLabel(s); got != want {
			t.Errorf("SeeingLabel(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestMutAstroForecast(t *testing.T) {
	m := mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"init":"2026101212","dataseries":[
			{"timepoint":3,"seeing":2,"transparency":3},
			{"timepoint":6,"seeing":0,"transparency":3},
			{"timepoint":9,"seeing":8,"transparency":8},
			{"timepoint":12,"seeing":9,"transparency":1},
			{"timepoint":15,"seeing":1,"transparency":-9999},
			{"timepoint":720,"seeing":1,"transparency":1},
			{"timepoint":721,"seeing":1,"transparency":1},
			{"timepoint":-3,"seeing":1,"transparency":1}]}`))
	})
	out, err := AstroForecast(context.Background(), 45.8149, 15.9781)
	if err != nil {
		t.Fatal(err)
	}
	init := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	want := map[int64]AstroBlock{
		init.Add(3 * time.Hour).Unix():   {2, 3},
		init.Add(9 * time.Hour).Unix():   {8, 8},
		init.Add(720 * time.Hour).Unix(): {1, 1},
	}
	if len(out) != len(want) {
		t.Errorf("AstroForecast = %v, want %v", out, want)
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("AstroForecast[%v] = %v, want %v", time.Unix(k, 0).UTC(), out[k], v)
		}
	}
	q := m.reqs[0].Query()
	if q.Get("lat") != "45.81" || q.Get("lon") != "15.98" {
		t.Errorf("query %v", q)
	}
	for _, body := range []string{`{"init":"bad","dataseries":[{"timepoint":3,"seeing":2,"transparency":3}]}`, `{"init":"2026101212","dataseries":[{"timepoint":3,"seeing":0,"transparency":3}]}`} {
		mutServe(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := AstroForecast(context.Background(), 1, 2); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestMutAerosolForecast(t *testing.T) {
	m := mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"elevation":158,"hourly":{"time":[10,20,30,40,50],"aerosol_optical_depth":[0.2,-0.1,null,10,10.5]}}`))
	})
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	out, elev, err := AerosolForecast(context.Background(), 45.8149, 15.9781, start, start.Add(10*time.Hour))
	if err != nil || elev != 158 {
		t.Fatalf("%v %v", elev, err)
	}
	if len(out) != 2 || out[10] != 0.2 || out[40] != 10 {
		t.Errorf("AOD %v", out)
	}
	q := m.reqs[0].Query()
	if q.Get("latitude") != "45.81" || q.Get("longitude") != "15.98" || q.Get("start_date") != "2026-10-12" || q.Get("end_date") != "2026-10-13" {
		t.Errorf("query %v", q)
	}
	mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hourly":{"time":[10,20],"aerosol_optical_depth":[0.2]}}`))
	})
	if _, _, err := AerosolForecast(context.Background(), 1, 2, start, start); err == nil {
		t.Error("malformed accepted")
	}
}

// A 400 for a multi-day range is retried once for the first day only, with
// the first answer's body cleared.
func TestMutGetRange(t *testing.T) {
	m := mutServe(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("end_date") != r.URL.Query().Get("start_date") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"reason":"out of range","a":1}`))

			return
		}
		_, _ = w.Write([]byte(`{"b":2}`))
	})
	var v struct {
		Reason string
		A, B   int
	}
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	err := getRange(context.Background(), "http://x/?q=1", start, start.Add(12*time.Hour), &v)
	if err != nil || v.B != 2 || v.A != 0 || v.Reason != "" || len(m.reqs) != 2 {
		t.Errorf("getRange: %v %+v %d requests", err, v, len(m.reqs))
	}
	if q := m.reqs[1].Query(); q.Get("start_date") != "2026-10-12" || q.Get("end_date") != "2026-10-12" {
		t.Errorf("retry query %v", q)
	}
	// Single day 400: no retry.
	m = mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{}`))
	})
	if err := getRange(context.Background(), "http://x/?q=1", start, start.Add(time.Hour), &v); err == nil || len(m.reqs) != 1 {
		t.Errorf("single day: %v, %d requests", err, len(m.reqs))
	}
	// A 404 is not narrowed.
	m = mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{}`))
	})
	if err := getRange(context.Background(), "http://x/?q=1", start, start.Add(24*time.Hour), &v); err == nil || len(m.reqs) != 1 {
		t.Errorf("404: %v, %d requests", err, len(m.reqs))
	}
	// Local times are sent as UTC dates.
	m = mutServe(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	zg := time.FixedZone("CEST", 2*3600)
	_ = getRange(context.Background(), "http://x/?q=1", time.Date(2026, 10, 13, 1, 0, 0, 0, zg), time.Date(2026, 10, 13, 5, 0, 0, 0, zg), &v)
	if q := m.reqs[0].Query(); q.Get("start_date") != "2026-10-12" || q.Get("end_date") != "2026-10-13" {
		t.Errorf("UTC dates %v", q)
	}
}

func TestMutForecast(t *testing.T) {
	m := mutServe(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"elevation":120,"hourly":{"time":[1791748800,1791752400],
			"cloud_cover_low":[0,0],"cloud_cover_mid":[0,0],"cloud_cover_high":[0,0],
			"temperature_2m":[10,9],"dew_point_2m":[5,5],"wind_speed_10m":[3,3],"wind_gusts_10m":[8,9],"precipitation":[0,0]}}`))
	})
	// A night ending at 23:30 UTC needs the next day's first gust.
	start := time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)
	out, elev, err := Forecast(context.Background(), 45.8149, 15.9781, start, start.Add(6*time.Hour+30*time.Minute))
	if err != nil || elev != 120 || len(out) != 2 || out[1791748800].Gust != 9 {
		t.Fatalf("Forecast %v %v %v", out, elev, err)
	}
	q := m.reqs[0].Query()
	if q.Get("end_date") != "2026-10-12" || q.Get("latitude") != "45.81" || q.Get("longitude") != "15.98" {
		t.Errorf("query %v", q)
	}
	mutServe(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"elevation":120,"hourly":{}}`)) })
	if _, _, err := Forecast(context.Background(), 1, 2, start, start); err == nil {
		t.Error("empty forecast accepted")
	}
}

// 72 km/h = 20 m/s jet, no shear: q_jet = 100(1 − 12/32) = 62.5, q = 81.25,
// class 1. A 180° veer at 74.88 km/h (20.8 m/s) between 850 and 700 hPa:
// shear 27.7 m/s/km (q 0) and jet q 60 give q = 30, class 4; isothermal
// −20 °C keeps Ri at 0.47, while θ rising only 4.2 K over that layer puts it
// at 0.13, a fifth class.
func TestMutSeeingClasses(t *testing.T) {
	var north [6]float64
	mid := [6]float64{72, 72, 72, 72, 72, 72}
	if s := Seeing(HourWeather{Levels: mutProfile(mid, north, nil)}); s != 1 {
		t.Errorf("20 m/s jet = %d, want 1", s)
	}
	w := [6]float64{74.88, 74.88, 74.88, 74.88, 74.88, 74.88}
	dirs := [6]float64{0, 180, 180, 180, 180, 180}
	iso := [6]float64{-20, -20, -20, -20, -20, -20}
	if s := Seeing(HourWeather{Levels: mutProfile(w, dirs, &iso)}); s != 4 {
		t.Errorf("veer + jet = %d, want 4", s)
	}
	weak := [6]float64{-20, -29.9, -20, -20, -20, -20}
	if s := Seeing(HourWeather{Levels: mutProfile(w, dirs, &weak)}); s != 5 {
		t.Errorf("veer + jet + Ri 0.13 = %d, want 5", s)
	}
}

// A series that decodes only in part is dropped whole.
func TestMutDecodePartialLevel(t *testing.T) {
	h, err := decodeHourly(json.RawMessage(`{"time":[1,2],"temperature_500hPa":[1,"x"]}`))
	if err != nil || h.Level[2].T != nil {
		t.Errorf("partial level series kept: %v %v", h.Level[2].T, err)
	}
}
