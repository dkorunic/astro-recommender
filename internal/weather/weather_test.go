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
var heights = [...]float64{800, 1500, 3000, 4200, 5600, 7200, 9200, 10400, 11800, 13500, 15800}

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
// 20 km/h wind integrates to 0.74" at 500 nm (groundFactor on its lowest
// layer, freeFactor above; scripts/dewan.py reproduces every number here).
func TestSeeing(t *testing.T) {
	west := uniform(270)
	calm := profile(15, 6.5, uniform(20), west)
	windy := profile(15, 6.5, uniform(50), west)
	jet := profile(15, 6.5, []float64{20, 30, 50, 80, 120, 180, 250, 250, 200, 120, 60}, west)
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
		{"three levels", calm[4:7], 0, 0},
		{"no level at 300 hPa", calm[:6], 0, 0},
		{"standard atmosphere", calm, 0.5, 0.9},
		{"standard, 700-100 hPa only", calm[2:], 0.4, 0.9},
		{"jet", jet, 0.9, 1.5},
		{"veering every level", veer, 1.5, 4},
	} {
		if got := Seeing(tc.levels); got < tc.lo || got > tc.hi || math.IsNaN(got) {
			t.Errorf("Seeing(%s) = %.2f, want %.1f-%.1f", tc.name, got, tc.lo, tc.hi)
		}
	}
	// Values plausible one by one can still be nonsense together: opposed
	// 1000 km/h winds 700 m apart put 10^470 in the outer scale. Unknown, not
	// a 1e19" cell or a +Inf the JSON encoder refuses.
	wild := []Level{{P: 1000, Z: 100, T: 10, Wind: 1000, Dir: 90}, {P: 925, Z: 800, T: 5, Wind: 1000, Dir: 270}, {P: 850, Z: 1500, T: 0, Wind: 50, Dir: 270}, {P: 300, Z: 9200, T: -45, Wind: 50, Dir: 270}}
	if got := Seeing(wild); got != 0 {
		t.Errorf("opposed 1000 km/h winds = %v, want 0", got)
	}
	// A profile adiabatic throughout (9.8 K/km to the top) has no gradient of
	// refractive index: whatever rounding leaves is below minSeeing, unknown.
	adiabatic := slices.Clone(calm)
	for i := range adiabatic {
		adiabatic[i].T = 15 - 9.8*adiabatic[i].Z/1000
	}
	if got := Seeing(adiabatic); got != 0 {
		t.Errorf("adiabatic profile = %v, want 0", got)
	}
	// Speed without shear is not turbulence: a uniform 50 km/h equals 20 km/h;
	// shear is, in order.
	if a, b := Seeing(calm), Seeing(windy); math.Abs(a-b) > 1e-9 {
		t.Errorf("uniform 20 km/h %.3f, 50 km/h %.3f: want equal", a, b)
	}
	if c, j, v := Seeing(calm), Seeing(jet), Seeing(veer); !(c < j && j < v) {
		t.Errorf("calm %.2f, jet %.2f, veering %.2f: want increasing", c, j, v)
	}
	// A nocturnal inversion in the ground layer (10 °C at the 120 m site, 12 °C
	// at 925 hPa, 10 to 20 km/h) adds about a quarter to the free-atmosphere
	// seeing (0.99" against 0.78"; it doubled it before groundFactor).
	above := slices.DeleteFunc(profile(12, 6.5, uniform(20), west), func(l Level) bool { return l.Z < 120 })
	above[0].T = 12 // 925 hPa warmer than the surface
	surface := Level{P: 1000, Z: 120, T: 10, Wind: 10, Dir: 270}
	neutral := slices.DeleteFunc(profile(10, 6.5, uniform(20), west), func(l Level) bool { return l.Z < 120 })
	inv, neu, free := Seeing(append([]Level{surface}, above...)),
		Seeing(append([]Level{surface}, neutral...)), Seeing(neutral)
	if !(inv > 0.9 && inv < 1.1 && neu >= free && neu < free+0.1) {
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
		polar[i].T = max(polar[i].T, polar[5].T) // isothermal from 400 hPa (7.2 km)
	}
	cooling := slices.Clone(polar)
	cooling[8].T = cooling[7].T - 4 // 250-200 hPa cools 2.9 K/km above a tropopause found at 400
	tropical := profile(30, 6.5, uniform(20), west)
	for i := range tropical {
		tropical[i].T = 30 - 6.5*tropical[i].Z/1000 // cooling to the top
	}
	topOnly := slices.Clone(tropical)
	topOnly[10].T = topOnly[9].T // 150-100 hPa isothermal, nothing above it
	inversion := slices.Clone(standard)
	inversion[2].T = inversion[1].T + 1 // 850-700 hPa warms: below 500 hPa, not a tropopause
	thin := slices.Clone(standard)
	thin[6].Z, thin[6].T = thin[5].Z+30, thin[5].T // 300 hPa 30 m above 400: no layer to judge
	vetoed := slices.Clone(polar)
	vetoed[7].Z, vetoed[7].T = vetoed[6].Z+30, vetoed[6].T // 250 hPa 30 m above 300: the next readable layer confirms 400
	// With 500 hPa missing, the 600-400 hPa layer's mean is 500 exactly, but
	// its base is at 600 hPa, 4.2 km: not a candidate.
	missing := slices.Delete(slices.Clone(polar), 4, 5)
	missing[3].T = polar[5].T // 600 hPa isothermal with 400 and above
	// A 5500 m site: the ground layer's mean pressure is under 500 hPa and a
	// nocturnal inversion makes it qualify, but the rule is about the free
	// atmosphere; the isothermal column above puts the tropopause at 400 hPa.
	high := []Level{
		{P: 505, Z: 5502, T: 0, Wind: 10, Dir: 270},
		{P: 400, Z: 7200, T: 1, Wind: 20, Dir: 270},
		{P: 300, Z: 9200, T: 1, Wind: 20, Dir: 270},
		{P: 250, Z: 10400, T: 1, Wind: 20, Dir: 270},
		{P: 200, Z: 11800, T: 1, Wind: 20, Dir: 270},
	}
	for _, tc := range []struct {
		name   string
		levels []Level
		want   int
	}{
		{"standard atmosphere", standard, 8},
		{"polar, isothermal from 400 hPa", polar, 5},
		{"cooling above a found tropopause", cooling, 5},
		{"tropical, cooling to the top", tropical, 10},
		{"top layer alone", topOnly, 9},
		{"low inversion ignored", inversion, 8},
		{"thin pair ignored", thin, 8},
		{"thin confirming pair looked past", vetoed, 5},
		{"ground layer never a candidate", high, 1},
		{"600-400 hPa with 500 missing", missing, 4},
		{"no levels", nil, -1},
	} {
		if got := tropopause(tc.levels); got != tc.want {
			t.Errorf("tropopause(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The stratospheric branch of cn2: an isothermal layer sheared 10 m/s over
// 1.4 km has about 12 times less Cn² than the same layer read as
// tropospheric.
func TestCn2Stratospheric(t *testing.T) {
	a, b := Level{P: 250, Z: 10400, T: -56.5, Wind: 100, Dir: 270}, Level{P: 200, Z: 11800, T: -56.5, Wind: 136, Dir: 270}
	strat, trop := cn2(a, b, true), cn2(a, b, false)
	if !(strat > 0 && strat < trop/10) {
		t.Errorf("cn2 stratospheric %.3g, tropospheric %.3g", strat, trop)
	}
	// Seeing reads the branch off tropopause: the polar profile, isothermal
	// from 400 hPa, integrates to 0.72" with its tropopause there, 1.33" with
	// the stratosphere starting at 200 hPa and 1.44" read as all troposphere.
	polar := profile(0, 6.5, uniform(20), uniform(270))
	for i := range polar {
		polar[i].T = max(polar[i].T, polar[5].T)
	}
	if got := Seeing(polar); got < 0.6 || got > 0.75 {
		t.Errorf("polar profile %.3f, want 0.6-0.75", got)
	}
}

// levels keeps a pressure level with all four values, physical, consistent
// with the level before it, and starts the profile at the surface (2 m
// above the site elevation with the surface pressure, 2 m temperature and
// 10 m wind). The fixture is the standard atmosphere at a 120 m site.
func TestLevels(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	h := hourly{Time: []int64{3600}, Temp: []*float64{p(10)}, Wind: []*float64{p(10)}, Dir: []*float64{p(270)}, SurfP: []*float64{p(998)}}
	for k, v := range map[int][4]float64{0: {110, 14, 10, 270}, 1: {1500, 5, 20, 270}, 2: {3000, -5, 30, 270}, 4: {5600, -20, -1, 270}, 5: {7200, -31, 50, 270}, 6: {9200, -45, 60, 400}, 7: {10400, -50, 80, 270}} {
		h.Level[k] = levelSeries{Z: []*float64{p(v[0])}, T: []*float64{p(v[1])}, Wind: []*float64{p(v[2])}, Dir: []*float64{p(v[3])}}
	}
	got := h.levels(120)[3600]
	// The surface sits at 2 m, where the temperature is read. 925 hPa is at
	// 110 m, below the 120 m site, 500 has a negative wind, 300 a 400° direction.
	if len(got) != 5 || got[0] != (Level{P: 998, Z: 122, T: 10, Wind: 10, Dir: 270}) || got[1].P != 850 || got[4].P != 250 {
		t.Errorf("levels %+v", got)
	}
	// Without the elevation (Profile rejects it first) the surface fails
	// plausible: no hour.
	if got, ok := h.levels(math.NaN())[3600]; ok {
		t.Errorf("unknown elevation: hour kept as %+v", got)
	}
	// A 1500 m site (850 hPa at the surface): 850 hPa is dropped beside it.
	h.SurfP[0] = p(850)
	if got := h.levels(1500)[3600]; len(got) != 4 || got[0].Z != 1502 || got[1].P != 700 {
		t.Errorf("surface at 850 hPa's height: 850 hPa kept beside it: %+v", got)
	}
	// The first level above the surface must be minLayer (300 m) up: Dewan's
	// outer scale was fitted on kilometre-thick layers, and a 50 m ground
	// layer turns an ordinary 15 km/h wind difference into a 100" estimate.
	// 925 hPa at 800 m is 300 m above a 500 m surface under 959 hPa.
	h.SurfP[0], h.Level[0].Z[0] = p(959), p(800)
	if got := h.levels(498)[3600]; len(got) != 6 || got[1].P != 925 {
		t.Errorf("925 hPa 300 m above the surface dropped: %+v", got)
	}
	if got := h.levels(499)[3600]; len(got) != 5 || got[1].P != 850 {
		t.Errorf("925 hPa 299 m above the surface kept: %+v", got)
	}
	h.SurfP[0], h.Level[0].Z[0] = p(998), p(110)
	// Each level must sit where the hypsometric equation puts it above the
	// level kept before: 850 hPa reported at 5000 m is a glitch, dropped, and
	// 700 hPa at 3000 m then reads against the surface.
	h.Level[1].Z[0] = p(5000)
	if got := h.levels(120)[3600]; len(got) != 4 || got[1].P != 700 {
		t.Errorf("850 hPa at 5000 m kept: %+v", got)
	}
	h.Level[1].Z[0] = p(1500)
	// So is a 700 hPa reported under 850 hPa.
	h.Level[2].Z[0] = p(1400)
	if got := h.levels(120)[3600]; len(got) != 4 || got[2].P != 400 {
		t.Errorf("700 hPa below 850 hPa kept: %+v", got)
	}
	h.Level[2].Z[0] = p(3000)
	// Fewer than three pressure levels: two multi-kilometre layers are no
	// profile, the hour goes.
	h.Level[2].T[0] = nil
	if got := h.levels(120)[3600]; len(got) != 4 || got[2].P != 400 {
		t.Errorf("null 700 hPa: %+v", got)
	}
	h.Level[5].T[0] = nil // 850 and 250 hPa left
	if got, ok := h.levels(120)[3600]; ok {
		t.Errorf("two pressure levels: hour kept as %+v", got)
	}
	// The top must reach 300 hPa, so the integral spans the jet: 850, 700 and
	// 400 hPa are three levels, but the hour goes.
	h.Level[2].T[0], h.Level[5].T[0], h.Level[7].T[0] = p(-5), p(-31), nil
	if got, ok := h.levels(120)[3600]; ok {
		t.Errorf("top at 400 hPa: hour kept as %+v", got)
	}
	h.Level[7].T[0] = p(-50)
	// A surface pressure that does not fit the elevation anchors the whole
	// chain wrongly: sea-level pressure at a 2000 m site let 400 hPa pass as
	// the first level above a 5 km "ground layer". No hour.
	h.SurfP[0] = p(1013)
	if got, ok := h.levels(2000)[3600]; ok {
		t.Errorf("sea-level pressure at 2000 m: hour kept as %+v", got)
	}
	// Without a surface level the estimate would miss its dominant term: no hour.
	h.SurfP[0] = nil
	if got, ok := h.levels(120)[3600]; ok {
		t.Errorf("no surface pressure: hour kept as %+v", got)
	}
	h.Time = nil
	if got := h.levels(120); len(got) != 0 {
		t.Errorf("no hours: %+v", got)
	}
}

// barometric accepts a surface pressure within 12% of the elevation's for
// the column's mean temperature: the cold Antarctic plateau sits well under
// a temperate column's pressure, and its surface air, under the winter
// inversion, well under the column's own mean.
func TestBarometric(t *testing.T) {
	for _, tc := range []struct {
		name       string
		p, elev, t float64
		want       bool
	}{
		{"sea level", 1013, 0, 15, true},
		{"deep low", 960, 0, 15, true},
		{"typhoon", 910, 0, 28, true},
		{"strong high", 1050, 0, 15, true},
		{"2000 m", 795, 2000, 10, true},
		{"sea-level pressure at 2000 m", 1013, 2000, 10, false},
		{"700 hPa at 120 m", 700, 120, 10, false},
		{"5500 m", 505, 5500, 0, true},
		{"South Pole winter", 641, 2835, -60, true},
		{"Vostok winter", 624, 3488, -68, true},
		{"Dome A winter", 575, 4093, -70, true},
	} {
		if got := barometric(tc.p, tc.elev, tc.t); got != tc.want {
			t.Errorf("%s: barometric(%v hPa, %v m, %v °C) = %v", tc.name, tc.p, tc.elev, tc.t, got)
		}
	}
}

// hypsometric accepts the standard atmosphere's layers and rejects a level
// a third too high or too low for its pressure.
func TestHypsometric(t *testing.T) {
	std := profile(15, 6.5, uniform(20), uniform(270))
	for i := 1; i < len(std); i++ {
		if !hypsometric(std[i-1], std[i]) {
			t.Errorf("standard %v-%v hPa rejected", std[i-1].P, std[i].P)
		}
	}
	surface := Level{P: 998, Z: 122, T: 10}
	for _, tc := range []struct {
		name string
		z    float64
		want bool
	}{{"850 hPa at 1500 m", 1500, true}, {"at 1000 m", 1000, false}, {"at 1900 m", 1900, false}, {"below the surface", 100, false}} {
		if got := hypsometric(surface, Level{P: 850, Z: tc.z, T: 5}); got != tc.want {
			t.Errorf("surface to %s: %v", tc.name, got)
		}
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
		_, _ = w.Write([]byte(`{"elevation":120,"hourly":{"time":[1791763200,1791766800,1791770400],
			"surface_pressure":[998,998,700],"temperature_2m":[10,10,10],"wind_speed_10m":[10,10,10],"wind_direction_10m":[270,270,270],
			"geopotential_height_850hPa":[1500,1500,1500],"temperature_850hPa":[8,8,8],"wind_speed_850hPa":[20,20,20],"wind_direction_850hPa":[270,270,270],
			"geopotential_height_500hPa":[5600,5600,5600],"temperature_500hPa":[-20,-20,-20],"wind_speed_500hPa":[40,40,40],"wind_direction_500hPa":[270,270,270],
			"geopotential_height_300hPa":[9200,9200,9200],"temperature_300hPa":[-45,-45,-45],"wind_speed_300hPa":[80,80,80],"wind_direction_300hPa":[270,270,270]}}`))
	})
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	out, err := Profile(context.Background(), 45.8149, 15.9781, start, start.Add(2*time.Hour))
	if err != nil || len(out) != 2 {
		t.Fatalf("Profile %v %v", out, err)
	}
	if l := out[1791763200]; len(l) != 4 || l[0] != (Level{P: 998, Z: 122, T: 10, Wind: 10, Dir: 270}) || l[3].P != 300 {
		t.Errorf("levels %+v", l)
	}
	if s := Seeing(out[1791766800]); s <= 0 {
		t.Errorf("seeing %v", s)
	}
	if l, ok := out[1791770400]; ok {
		t.Errorf("700 hPa surface pressure at 120 m: hour kept as %+v", l)
	}
	q := m.reqs[0].Query()
	if h := q.Get("hourly"); strings.Contains(h, "cloud_cover") || !strings.Contains(h, "wind_speed_300hPa") || !strings.Contains(h, "surface_pressure") || q.Get("latitude") != "45.81" {
		t.Errorf("query %v", q)
	}
	if q.Get("start_date") != "2026-10-12" || q.Get("end_date") != "2026-10-12" || q.Get("models") != "ecmwf_ifs025" {
		t.Errorf("range and model %v", q)
	}
	// A window ending exactly at a UTC midnight needs no hour of the next day.
	if _, err := Profile(context.Background(), 45.8149, 15.9781, start.Add(22*time.Hour), start.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if q := m.reqs[len(m.reqs)-1].Query(); q.Get("end_date") != "2026-10-12" {
		t.Errorf("window to midnight: end_date %s, want 2026-10-12", q.Get("end_date"))
	}
	for body, want := range map[string]string{
		`{}`:                            "no upper-air data",
		`{"elevation":120,"hourly":{}}`: "no upper-air data",
		`{"elevation":120,"hourly":{"time":[1791763200],"surface_pressure":[998]}}`:                                                                                                 "temperature_2m series has 0 of 1 hours",
		`{"elevation":120,"hourly":{"time":[1791763200],"temperature_2m":[10],"wind_speed_10m":[10],"wind_direction_10m":[270],"geopotential_height_850hPa":[1500]}}`:               "surface_pressure series has 0 of 1 hours",
		`{"elevation":120,"hourly":{"time":[1791763200,1791766800],"surface_pressure":[998],"temperature_2m":[10,10],"wind_speed_10m":[10,10],"wind_direction_10m":[270,270]}}`:     "surface_pressure series has 1 of 2 hours",
		`{"elevation":120,"hourly":{"time":[1791763200,1791766800],"surface_pressure":[700,700],"temperature_2m":[10,10],"wind_speed_10m":[10,10],"wind_direction_10m":[270,270]}}`: "no usable hour of 2",
		`{"hourly":{"time":[1791763200],"surface_pressure":[998]}}`:                                                                                                                 "no site elevation",
		`{"elevation":-9999,"hourly":{"time":[1791763200],"surface_pressure":[998]}}`:                                                                                               "no site elevation",
	} {
		mutServe(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if out, err := Profile(context.Background(), 1, 2, start, start); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v %v, want %q", body, out, err, want)
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
	if out, err = h.weather(); err != nil || len(out) != 3 || !math.IsNaN(out[h.Time[0]].Precip) {
		t.Errorf("no precipitation: err %v, %d hours, precip %v", err, len(out), out[h.Time[0]].Precip)
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
	// so 925 hPa (800 m) lies below it and ten levels remain.
	for k := range h.Level {
		tk := p(15 - 6.5*heights[k]/1000)
		h.Level[k] = levelSeries{Z: []*float64{p(heights[k]), p(heights[k]), nil}, T: []*float64{tk, tk, tk}, Wind: zero, Dir: zero}
	}
	if l, ok := h.levels(1100)[h.Time[0]]; ok {
		t.Errorf("known elevation, no surface fields: hour kept as %+v", l)
	}
	h.SurfP, h.Dir = []*float64{p(890), p(890), p(890)}, zero
	h.Level[2].T = []*float64{p(1), nil, p(1)}
	h.Level[4].Wind = []*float64{p(0), p(-9999), p(0)} // a sentinel speed drops the level like a null
	h.Level[6].Dir = []*float64{p(0), p(0), p(-9999)}  // and so does a sentinel direction
	lv := h.levels(1100)
	if l := lv[h.Time[0]]; len(l) != len(pressures) || l[0].Z != 1102 || l[1].P != 850 || l[1].Z != 1500 || l[2].T != 1 {
		t.Errorf("hour 0 levels: %+v", l)
	}
	if l := lv[h.Time[1]]; len(l) != len(pressures)-2 || l[4].P != 300 {
		t.Errorf("hour 1 levels (null 700 hPa temperature, -9999 500 hPa wind): %+v", l)
	}
	if l, ok := lv[h.Time[2]]; ok {
		t.Errorf("hour 2 levels (null heights): %+v", l)
	}
	for k := range h.Level {
		h.Level[k].Z[2] = p(heights[k])
	}
	if l := h.levels(1100)[h.Time[2]]; len(l) != len(pressures)-1 || l[6].P != 250 {
		t.Errorf("hour 2 levels (-9999 300 hPa direction): %+v", l)
	}
	// A level less than 300 m above the surface is dropped, the ground layer
	// being too thin for the model.
	h.Level[1].Z[0] = p(1400)
	if l := h.levels(1100)[h.Time[0]]; len(l) != len(pressures)-1 || l[1].P != 700 {
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
	if h.Time[1] != 3600 || *h.Low[1] != 2 || *h.Level[1].Z[1] != 1510 || *h.Level[1].T[0] != 8 || *h.Level[1].Wind[1] != 25 || *h.Level[1].Dir[1] != 280 {
		t.Errorf("850 hPa: %+v", h.Level[1])
	}
	if h.Level[2].Z != nil || h.Level[8].Wind == nil || h.Level[8].Wind[1] != nil || h.Level[8].Z != nil {
		t.Errorf("700 hPa (absent) %+v, 200 hPa (wind only) %+v", h.Level[2], h.Level[8])
	}
	if h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"units":"percent"}`)); err != nil || len(h.Time) != 1 || h.Level[0].Z != nil {
		t.Errorf("non-series value: err %v, %d hours, levels %+v", err, len(h.Time), h.Level[0])
	}
	// An unreadable level series costs that series only; a named one is an error.
	h, err = decodeHourly(json.RawMessage(`{"time":[0],"cloud_cover_low":[1],"temperature_700hPa":"oops","wind_speed_700hPa":[1,"x"],"wind_direction_700hPa":[90]}`))
	if err != nil || len(h.Time) != 1 || h.Level[2].T != nil || h.Level[2].Wind != nil || len(h.Level[2].Dir) != 1 {
		t.Errorf("bad level series: err %v, 700 hPa %+v", err, h.Level[2])
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
