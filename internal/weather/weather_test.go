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
	"slices"
	"strings"
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

// heights are the standard-atmosphere geopotential heights of pressures.
var heights = [...]float64{110, 800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800}

// profile builds the standard levels from a sea-level temperature (°C), a
// lapse rate (K/km, isothermal above 11 km) and a wind speed (km/h) and
// direction per level.
func profile(seaT, lapse float64, winds, dirs []float64) []Level {
	var out []Level
	for i, p := range pressures {
		out = append(out, Level{P: float64(p), Z: heights[i], T: seaT - lapse*min(heights[i], 11000)/1000, Wind: winds[i], Dir: dirs[i]})
	}

	return out
}

func uniform(v float64) []float64 { return slices.Repeat([]float64{v}, len(pressures)) }

// Dewan's model on the standard atmosphere (6.5 K/km) with a uniform
// 20 km/h wind integrates to r0 ≈ 0.15 m at 500 nm: 0.67" (scripts/dewan.py
// reproduces every number here).
func TestSeeing(t *testing.T) {
	west := uniform(270)
	calm := profile(15, 6.5, uniform(20), west)
	windy := profile(15, 6.5, uniform(50), west)
	jet := profile(15, 6.5, []float64{20, 20, 30, 50, 80, 120, 180, 250, 250, 200, 120, 60}, west)
	veer := profile(15, 6.5, uniform(60), west)
	for i := range veer {
		veer[i].Dir = float64(270 + 90*(i%2))
	}
	for _, tc := range []struct {
		name   string
		levels []Level
		lo, hi float64
	}{
		{"no profile", nil, 0, 0},
		{"two levels", calm[:2], 0, 0},
		{"no level at 300 hPa", calm[:7], 0, 0},
		{"standard atmosphere", calm, 0.5, 0.9},
		{"standard, 700-100 hPa only", calm[3:], 0.4, 0.9},
		{"jet", jet, 0.9, 1.5},
		{"veering every level", veer, 2, 4},
	} {
		if got := Seeing(HourWeather{Levels: tc.levels}); got < tc.lo || got > tc.hi || math.IsNaN(got) {
			t.Errorf("Seeing(%s) = %.2f, want %.1f-%.1f", tc.name, got, tc.lo, tc.hi)
		}
	}
	// Values plausible one by one can still be nonsense together: opposed
	// 1000 km/h winds 700 m apart put 10^470 in the outer scale. Unknown, not
	// a 1e19" cell or a +Inf the JSON encoder refuses.
	wild := []Level{{P: 1000, Z: 100, T: 10, Wind: 1000, Dir: 90}, {P: 925, Z: 800, T: 5, Wind: 1000, Dir: 270}, {P: 300, Z: 9200, T: -45, Wind: 50, Dir: 270}}
	if got := Seeing(HourWeather{Levels: wild}); got != 0 {
		t.Errorf("opposed 1000 km/h winds = %v, want 0", got)
	}
	// Speed without shear is not turbulence: a uniform 50 km/h equals 20 km/h.
	if a, b := Seeing(HourWeather{Levels: calm}), Seeing(HourWeather{Levels: windy}); math.Abs(a-b) > 1e-9 {
		t.Errorf("uniform 20 km/h %.3f, 50 km/h %.3f: want equal", a, b)
	}
	// A nocturnal inversion in the ground layer (10 °C at the 120 m site, 12 °C
	// at 925 hPa, 10 to 20 km/h) roughly doubles the free-atmosphere seeing.
	above := slices.DeleteFunc(profile(12, 6.5, uniform(20), west), func(l Level) bool { return l.Z < 120 })
	above[0].T = 12 // 925 hPa warmer than the surface
	surface := Level{P: 1000, Z: 120, T: 10, Wind: 10, Dir: 270}
	neutral := slices.DeleteFunc(profile(10, 6.5, uniform(20), west), func(l Level) bool { return l.Z < 120 })
	inv, neu, free := Seeing(HourWeather{Levels: append([]Level{surface}, above...)}),
		Seeing(HourWeather{Levels: append([]Level{surface}, neutral...)}), Seeing(HourWeather{Levels: neutral})
	if !(inv > 1.2 && inv < 1.6 && neu >= free && neu < free+0.1) {
		t.Errorf("inversion %.2f, neutral ground %.2f, no ground layer %.2f", inv, neu, free)
	}
}

// tropopause is the WMO rule on the forecast levels: searched above
// 500 hPa, the base of the lowest layer cooling less than 2 K/km whose next
// layer, when there is one, does too.
func TestTropopause(t *testing.T) {
	west := uniform(270)
	standard := profile(15, 6.5, uniform(20), west) // isothermal from 11 km: 250-200 hPa still cools 2.8 K/km
	polar := profile(0, 6.5, uniform(20), west)
	for i := range polar {
		polar[i].T = max(polar[i].T, polar[6].T) // isothermal from 400 hPa (7.2 km)
	}
	cooling := slices.Clone(polar)
	cooling[9].T = cooling[8].T - 4 // 250-200 hPa cools 2.9 K/km above a tropopause found at 400
	tropical := profile(30, 6.5, uniform(20), west)
	for i := range tropical {
		tropical[i].T = 30 - 6.5*tropical[i].Z/1000 // cooling to the top
	}
	topOnly := slices.Clone(tropical)
	topOnly[11].T = topOnly[10].T // 150-100 hPa isothermal, nothing above it
	inversion := slices.Clone(standard)
	inversion[3].T = inversion[2].T + 1 // 850-700 hPa warms: below 500 hPa, not a tropopause
	thin := slices.Clone(standard)
	thin[7].Z, thin[7].T = thin[6].Z+30, thin[6].T // 300 hPa 30 m above 400: no layer to judge
	for _, tc := range []struct {
		name   string
		levels []Level
		want   int
	}{
		{"standard atmosphere", standard, 9},
		{"polar, isothermal from 400 hPa", polar, 6},
		{"cooling above a found tropopause", cooling, 6},
		{"tropical, cooling to the top", tropical, 11},
		{"top layer alone", topOnly, 10},
		{"low inversion ignored", inversion, 9},
		{"thin pair ignored", thin, 9},
		{"no levels", nil, -1},
	} {
		if got := tropopause(tc.levels); got != tc.want {
			t.Errorf("tropopause(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The stratospheric branch of cn2: an isothermal layer sheared 10 m/s over
// 1.4 km has about 14 times less Cn² than the same layer read as
// tropospheric.
func TestCn2Stratospheric(t *testing.T) {
	a, b := Level{P: 250, Z: 10400, T: -56.5, Wind: 100, Dir: 270}, Level{P: 200, Z: 11800, T: -56.5, Wind: 136, Dir: 270}
	strat, trop := cn2(a, b, true), cn2(a, b, false)
	if !(strat > 0 && strat < trop/10) {
		t.Errorf("cn2 stratospheric %.3g, tropospheric %.3g", strat, trop)
	}
	// Seeing reads the branch off tropopause: the polar profile, isothermal
	// from 400 hPa, integrates to 0.68" with its tropopause there, 1.14" with
	// the stratosphere starting at 200 hPa and 1.22" read as all troposphere.
	polar := profile(0, 6.5, uniform(20), uniform(270))
	for i := range polar {
		polar[i].T = max(polar[i].T, polar[6].T)
	}
	if got := Seeing(HourWeather{Levels: polar}); got < 0.6 || got > 0.75 {
		t.Errorf("polar profile %.3f, want 0.6-0.75", got)
	}
}

// levels keeps a pressure level with all four values, physical and above
// the site, and starts the profile at the surface (the site elevation with
// the 2 m temperature and 10 m wind) when the elevation is known.
func TestLevels(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	h := hourly{Time: []int64{3600}, Temp: []*float64{p(10)}, Wind: []*float64{p(10)}, Dir: []*float64{p(270)}, SurfP: []*float64{p(998)}}
	for k, v := range map[int][4]float64{0: {110, 14, 10, 270}, 2: {1500, 5, 20, 270}, 3: {3000, -5, 30, 270}, 5: {5600, -20, -1, 270}, 6: {7200, -31, 50, 270}, 7: {9200, -45, 60, 400}, 8: {10400, -50, 80, 270}} {
		h.Level[k] = levelSeries{Z: []*float64{p(v[0])}, T: []*float64{p(v[1])}, Wind: []*float64{p(v[2])}, Dir: []*float64{p(v[3])}}
	}
	got := h.levels(120)[3600]
	// The surface sits at 2 m, where the temperature is read. 1000 hPa is
	// below the 120 m site, 500 has a negative wind, 300 a 400° direction.
	if len(got) != 5 || got[0] != (Level{P: 998, Z: 122, T: 10, Wind: 10, Dir: 270}) || got[1].P != 850 || got[4].P != 250 {
		t.Errorf("levels %+v", got)
	}
	// Without the elevation there is no surface level, and the estimate would
	// miss its largest term: no hour.
	if got, ok := h.levels(math.NaN())[3600]; ok {
		t.Errorf("unknown elevation: hour kept as %+v", got)
	}
	// The first level above the surface must be 300 m up: Dewan's outer scale
	// was fitted on kilometre-thick layers, and a 50 m ground layer turns an
	// ordinary 15 km/h wind difference into a 100" estimate.
	if got := h.levels(1500)[3600]; len(got) != 4 || got[0].Z != 1502 || got[1].P != 700 {
		t.Errorf("surface at 850 hPa's height: 850 hPa kept beside it: %+v", got)
	}
	h.Level[0].Z[0] = p(421)
	if got := h.levels(120)[3600]; len(got) != 5 || got[1].P != 850 {
		t.Errorf("1000 hPa 299 m above the surface kept: %+v", got)
	}
	h.Level[0].Z[0] = p(422)
	if got := h.levels(120)[3600]; len(got) != 6 || got[1].P != 1000 {
		t.Errorf("1000 hPa 300 m above the surface dropped: %+v", got)
	}
	// Fewer than three pressure levels: two multi-kilometre layers are no
	// profile, the hour goes.
	h.Level[3].T[0] = nil
	if got := h.levels(120)[3600]; len(got) != 5 || got[2].P != 850 {
		t.Errorf("null 700 hPa: %+v", got)
	}
	h.Level[0].Z[0], h.Level[6].T[0] = p(110), nil // 850 and 250 hPa left
	if got, ok := h.levels(120)[3600]; ok {
		t.Errorf("two pressure levels: hour kept as %+v", got)
	}
	// Without a surface level the estimate would miss its dominant term: no hour.
	h.Level[0].Z[0], h.SurfP[0] = p(422), nil
	if got, ok := h.levels(120)[3600]; ok {
		t.Errorf("no surface pressure: hour kept as %+v", got)
	}
	h.Time = nil
	if got := h.levels(120); len(got) != 0 {
		t.Errorf("no hours: %+v", got)
	}
}

// Profile fetches the upper-air levels from ECMWF IFS 0.25 in a request of
// its own, surface first; Forecast carries no pressure levels any more.
func TestProfile(t *testing.T) {
	m := mutServe(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("models") != "ecmwf_ifs025" {
			w.WriteHeader(http.StatusNotFound)

			return
		}
		_, _ = w.Write([]byte(`{"elevation":120,"hourly":{"time":[1791748800,1791752400,1791756000],
			"surface_pressure":[998,998,998],"temperature_2m":[10,10,10],"wind_speed_10m":[10,10,10],"wind_direction_10m":[270,270,270],
			"geopotential_height_850hPa":[1500,1500,1500],"temperature_850hPa":[8,8,8],"wind_speed_850hPa":[20,20,20],"wind_direction_850hPa":[270,270,270],
			"geopotential_height_500hPa":[5600,5600,5600],"temperature_500hPa":[-20,-20,-20],"wind_speed_500hPa":[40,40,40],"wind_direction_500hPa":[270,270,270],
			"geopotential_height_300hPa":[9200,9200,9200],"temperature_300hPa":[-45,-45,-45],"wind_speed_300hPa":[80,80,80],"wind_direction_300hPa":[270,270,null]}}`))
	})
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	out, err := Profile(context.Background(), 45.8149, 15.9781, start, start.Add(2*time.Hour))
	if err != nil || len(out) != 2 {
		t.Fatalf("Profile %v %v", out, err)
	}
	if l := out[1791748800]; len(l) != 4 || l[0] != (Level{P: 998, Z: 122, T: 10, Wind: 10, Dir: 270}) || l[3].P != 300 {
		t.Errorf("levels %+v", l)
	}
	if s := Seeing(HourWeather{Levels: out[1791752400]}); s <= 0 {
		t.Errorf("seeing %v", s)
	}
	if l, ok := out[1791756000]; ok {
		t.Errorf("null 300 hPa direction leaves two pressure levels: hour kept as %+v", l)
	}
	q := m.reqs[0].Query()
	if h := q.Get("hourly"); strings.Contains(h, "cloud_cover") || !strings.Contains(h, "wind_speed_300hPa") || !strings.Contains(h, "surface_pressure") || q.Get("latitude") != "45.81" {
		t.Errorf("query %v", q)
	}
	for _, body := range []string{`{"elevation":120,"hourly":{}}`, `{"elevation":120,"hourly":{"time":[1791748800],"surface_pressure":[998]}}`} {
		mutServe(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if out, err := Profile(context.Background(), 1, 2, start, start); err == nil {
			t.Errorf("%s: accepted as %v", body, out)
		}
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
	// Gust is optional per hour but must still cover every hour.
	short := h
	short.Gust = h.Gust[:2]
	if _, err := short.weather(); err == nil {
		t.Error("short gust series accepted")
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
		_, present := out[h.Time[0]]
		t.Errorf("null own gust: got %v (present %v), want 30", out[h.Time[0]].Gust, present)
	}
	// Without precipitation and upper-air series every hour stays, unknown.
	if out, err = h.weather(); err != nil || len(out) != 3 || !math.IsNaN(out[h.Time[0]].Precip) || out[h.Time[0]].Levels != nil {
		t.Errorf("no precipitation/levels: err %v, %d hours, precip %v, levels %v", err, len(out), out[h.Time[0]].Precip, out[h.Time[0]].Levels)
	}
	// Precipitation is the preceding hour's sum, shifted like the gusts, but
	// an hour without a next entry stays unknown rather than taking its own
	// value, which is the previous hour's rain.
	h.Precip = []*float64{p(0), p(0.3)}
	if out, _ = h.weather(); out[h.Time[0]].Precip != 0.3 || !math.IsNaN(out[h.Time[1]].Precip) || !math.IsNaN(out[h.Time[2]].Precip) {
		t.Errorf("precipitation: got %v, %v, %v, want 0.3, NaN, NaN", out[h.Time[0]].Precip, out[h.Time[1]].Precip, out[h.Time[2]].Precip)
	}
	// A level is kept only with all four values; a null drops that level
	// alone. A known elevation needs the surface fields; the site is 1100 m,
	// the levels 1500 m.
	for k := range h.Level {
		h.Level[k] = levelSeries{Z: []*float64{p(1500), p(1500), nil}, T: zero, Wind: zero, Dir: zero}
	}
	if l, ok := h.levels(1100)[h.Time[0]]; ok {
		t.Errorf("known elevation, no surface fields: hour kept as %+v", l)
	}
	h.SurfP, h.Dir = []*float64{p(850), p(850), p(850)}, zero
	h.Level[3].T = []*float64{p(1), nil, p(1)}
	h.Level[5].Wind = []*float64{p(0), p(-9999), p(0)} // a sentinel speed drops the level like a null
	h.Level[7].Dir = []*float64{p(0), p(0), p(-9999)}  // and so does a sentinel direction
	lv := h.levels(1100)
	if l := lv[h.Time[0]]; len(l) != len(pressures)+1 || l[0].Z != 1102 || l[1].P != 1000 || l[1].Z != 1500 || l[4].T != 1 {
		t.Errorf("hour 0 levels: %+v", l)
	}
	if l := lv[h.Time[1]]; len(l) != len(pressures)-1 || l[6].P != 300 {
		t.Errorf("hour 1 levels (null 700 hPa temperature, -9999 500 hPa wind): %+v", l)
	}
	if l, ok := lv[h.Time[2]]; ok {
		t.Errorf("hour 2 levels (null heights): %+v", l)
	}
	for k := range h.Level {
		h.Level[k].Z[2] = p(1500)
	}
	if l := h.levels(1100)[h.Time[2]]; len(l) != len(pressures) || l[8].P != 250 {
		t.Errorf("hour 2 levels (-9999 300 hPa direction): %+v", l)
	}
	// A level less than 300 m above the surface is dropped, the ground layer
	// being too thin for the model.
	h.Level[2].Z[0] = p(1400)
	if l := h.levels(1100)[h.Time[0]]; len(l) != len(pressures) || l[3].P != 700 {
		t.Errorf("850 hPa 298 m above the surface: %+v, want it dropped", l)
	}
	h.Gust[1] = nil
	if out, _ = h.weather(); out[h.Time[0]].Gust != 5 {
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
	if h.Time[1] != 3600 || *h.Low[1] != 2 || *h.Level[2].Z[1] != 1510 || *h.Level[2].T[0] != 8 || *h.Level[2].Wind[1] != 25 || *h.Level[2].Dir[1] != 280 {
		t.Errorf("850 hPa: %+v", h.Level[2])
	}
	if h.Level[3].Z != nil || h.Level[9].Wind == nil || h.Level[9].Wind[1] != nil || h.Level[9].Z != nil {
		t.Errorf("700 hPa (absent) %+v, 200 hPa (wind only) %+v", h.Level[3], h.Level[9])
	}
	if h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"units":"percent"}`)); err != nil || len(h.Time) != 1 || h.Level[0].Z != nil {
		t.Errorf("non-series value: err %v, %d hours, levels %+v", err, len(h.Time), h.Level[0])
	}
	// An unreadable level series costs that series only; a named one is an error.
	h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"temperature_700hPa":"oops","wind_speed_700hPa":[1,"x"],"wind_direction_700hPa":[90]}`))
	if err != nil || len(h.Time) != 1 || h.Level[3].T != nil || h.Level[3].Wind != nil || len(h.Level[3].Dir) != 1 {
		t.Errorf("bad level series: err %v, 700 hPa %+v", err, h.Level[3])
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

func mustWeather(t *testing.T, h *hourly) map[int64]HourWeather {
	t.Helper()
	out, err := h.weather()
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
