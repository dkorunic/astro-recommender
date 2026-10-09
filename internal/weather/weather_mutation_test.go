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
			*s = append(*s, new(0.0))
		}
	}

	return h
}

func TestMutWeatherCloudAndShift(t *testing.T) {
	h := mutHourly(3)
	h.Low[0], h.Mid[0], h.High[0] = new(50.0), new(0.0), new(100.0)
	h.Low[1], h.Mid[1], h.High[1] = new(20.0), new(50.0), new(40.0)
	h.Wind = []*float64{new(5.0), new(6.0), new(7.0)}
	h.Gust = []*float64{new(10.0), new(20.0), new(30.0)}
	h.Precip = []*float64{new(9.0), new(0.3), new(0.7)}
	out, err := h.weather()
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
	h.Gust = []*float64{nil, new(40.0), new(50.0), nil}
	h.Wind = []*float64{new(3.0), new(4.0), new(5.0), new(6.0)}
	h.Precip = []*float64{new(0.0), new(-1.0), new(2.0), new(3.0)}
	out, err := h.weather()
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
	h.High[1] = new(101.0)
	h.Gust[2] = nil
	out, _ = h.weather()
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
	h.Low[1] = new(-1.0)
	if out, _ = h.weather(); len(out) != 2 {
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
		if _, err := h.weather(); err == nil {
			t.Error("short series accepted")
		}
	}
	// A short precipitation series only costs the rain gate.
	h = mutHourly(3)
	h.Precip = h.Precip[:1]
	if out, err := h.weather(); err != nil || len(out) != 3 {
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
	h.SurfP, h.Dir = []*float64{new(980.0)}, []*float64{new(90.0)}
	var vals [len(pressures)][4]float64
	for k, z := range [...]float64{110, 800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800} {
		vals[k] = [4]float64{z, 15 - 6.5*z/1000, 10, 0}
	}
	vals[5][2], vals[7][3] = -1, 400 // 500 hPa: negative wind; 300 hPa: 400°
	mutLevels(&h, 0, vals)
	got := h.levels(2000)[mutT0]
	// The surface at 2002 m, then 1000-850 lie below the site.
	if len(got) != 8 || got[0] != (Level{P: 980, Z: 2002, Wind: 0, Dir: 90}) || got[1].P != 700 || got[1].Wind != 10 || got[3].P != 400 || got[4].P != 250 {
		t.Errorf("levels %+v", got)
	}
	if l := h.levels(math.NaN())[mutT0]; len(l) != 10 || l[0].P != 1000 {
		t.Errorf("NaN elevation levels %+v", l)
	}
	if l := h.levels(1500)[mutT0]; len(l) != 8 || l[0].Z != 1502 || l[1].P != 700 {
		t.Errorf("850 hPa at the surface's own height kept: %+v", l)
	}
	h.Dir[0] = nil
	if l, ok := h.levels(1500)[mutT0]; ok {
		t.Errorf("no surface wind direction: hour kept as %+v", l)
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
	if len(h.Level[2].Z) != 2 || *h.Level[2].Z[1] != 1510 || h.Level[2].T != nil || *h.Level[3].Wind[0] != 30 {
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
	}{{new(120.0), 120}, {new(-500.0), -500}, {new(9000.0), 9000}, {new(-501.0), math.NaN()}, {new(9001.0), math.NaN()}, {new(math.Inf(1)), math.NaN()}, {nil, math.NaN()}} {
		got := elevation(c.p)
		if !(got == c.want || math.IsNaN(got) && math.IsNaN(c.want)) {
			t.Errorf("elevation = %v, want %v", got, c.want)
		}
	}
}

func TestMutWind(t *testing.T) {
	// From the north at 36 km/h: blowing south at 10 m/s.
	u, v := wind(Level{Wind: 36, Dir: 0})
	mutNear(t, "u N", u, 0, 1e-9)
	mutNear(t, "v N", v, -10, 1e-9)
	u, v = wind(Level{Wind: 36, Dir: 90})
	mutNear(t, "u E", u, -10, 1e-9)
	mutNear(t, "v E", v, 0, 1e-9)
}

// mutProfile builds the standard levels (6.5 K/km, isothermal above 11 km)
// from a wind speed (km/h) and direction per level.
func mutProfile(winds, dirs [len(pressures)]float64) []Level {
	z := [len(pressures)]float64{110, 800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800}
	var out []Level
	for i, p := range pressures {
		out = append(out, Level{P: float64(p), Z: z[i], T: 15 - 6.5*min(z[i], 11000)/1000, Wind: winds[i], Dir: dirs[i]})
	}

	return out
}

func mutUniform(v float64) (out [len(pressures)]float64) {
	for i := range out {
		out[i] = v
	}

	return out
}

// Dewan's Cn² for one layer by hand: 850-700 hPa, 8 and -4.5 °C (T 274.9 K),
// 1.5 km apart, 20 and 56 km/h from the west (shear 10 m/s / 1500 m):
// M = -79e-6 775/274.9² (-8.33e-3 + 9.8e-3) = -1.19e-9, Y = 1.64 + 0.28,
// Cn² = 2.8 M² 0.0464 10^1.92 = 1.53e-17 m^-2/3.
func TestMutCn2(t *testing.T) {
	a, b := Level{P: 850, Z: 1500, T: 8, Wind: 20, Dir: 270}, Level{P: 700, Z: 3000, T: -4.5, Wind: 56, Dir: 270}
	mutNear(t, "cn2", cn2(a, b, false), 1.53e-17, 0.02e-17)
	// Shear is a vector: the same speeds from opposite directions shear
	// 76 km/h (14.1e-3 1/s), Y rises by 0.31 and Cn² doubles (10^0.31 = 2.04).
	b.Dir = 90
	if r := cn2(a, b, false) / 1.53e-17; r < 2 || r > 2.1 {
		t.Errorf("opposed winds: %.2f times the westerly layer, want 2.04", r)
	}
	// An exactly adiabatic layer has no gradient of refractive index.
	b.Dir, b.T = 270, 8-9.8*1.5
	mutNear(t, "adiabatic", cn2(a, b, false), 0, 1e-30)
}

func TestMutSeeing(t *testing.T) {
	west := mutUniform(270)
	calm := Seeing(HourWeather{Levels: mutProfile(mutUniform(10), west)})
	if calm < 0.5 || calm > 0.9 {
		t.Errorf("calm standard atmosphere = %.2f, want 0.5-0.9", calm)
	}
	// Speed without shear changes nothing.
	mutNear(t, "uniform 144 km/h", Seeing(HourWeather{Levels: mutProfile(mutUniform(144), west)}), calm, 1e-9)
	// Shear below 300 hPa counts even without a jet aloft.
	low := mutUniform(10)
	low[2], low[3] = 100, 10
	if s := Seeing(HourWeather{Levels: mutProfile(low, west)}); s <= calm {
		t.Errorf("sheared 850-700 = %.2f, want above %.2f", s, calm)
	}
	// A pair closer than 50 m is skipped: a 700 hPa level 30 m above 850 hPa
	// with the wind reversed would be 20 m/s of shear over 30 m, blowing the
	// estimate up; instead the profile reads as if 850 hPa were not there.
	full := mutProfile(mutUniform(36), west)
	thin := []Level{full[2], full[3], full[5], full[7]}
	thin[1].Z, thin[1].Dir = thin[0].Z+30, 90
	mutNear(t, "thin layer", Seeing(HourWeather{Levels: thin}), Seeing(HourWeather{Levels: thin[1:]}), 1e-12)
	// Veering 180° at 36 km/h between 850 and 700 hPa: 20 m/s of shear.
	dirs := west
	dirs[3] = 90
	w := mutUniform(10)
	w[2], w[3] = 36, 36
	if s := Seeing(HourWeather{Levels: mutProfile(w, dirs)}); s <= calm {
		t.Errorf("veer = %.2f, want above %.2f", s, calm)
	}
	// Gusts no longer enter: the surface wind is a level of its own.
	mutNear(t, "gusty", Seeing(HourWeather{Levels: mutProfile(mutUniform(10), west), Gust: 80}), calm, 0)
	full = mutProfile(mutUniform(10), west)
	for name, lv := range map[string][]Level{"two levels": full[:2], "no level at 300 hPa": full[:7], "two levels up to 300 hPa": full[6:8]} {
		if s := Seeing(HourWeather{Levels: lv}); s != 0 {
			t.Errorf("%s = %v", name, s)
		}
	}
	if s := Seeing(HourWeather{Levels: full[5:8]}); s <= 0 || s > calm {
		t.Errorf("500-300 only = %.2f, want 0 < s <= %.2f", s, calm)
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

// A series that decodes only in part is dropped whole.
func TestMutDecodePartialLevel(t *testing.T) {
	h, err := decodeHourly(json.RawMessage(`{"time":[1,2],"temperature_500hPa":[1,"x"]}`))
	if err != nil || h.Level[2].T != nil {
		t.Errorf("partial level series kept: %v %v", h.Level[2].T, err)
	}
}
