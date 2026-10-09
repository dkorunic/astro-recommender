// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package weather fetches Open-Meteo weather and aerosol forecasts and 7Timer
// transparency and seeing, with a turbulence seeing class from the upper-air
// profile as the fallback.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
)

var (
	errOpenMeteo  = errors.New("open-meteo")
	errAirQuality = errors.New("open-meteo air quality")
	errSevenTimer = errors.New("7timer")
)

type HourWeather struct {
	Levels         []Level // the upper-air levels Open-Meteo had, lowest first (display-only: Seeing)
	Cloud          float64 // effective cover, thin high cloud counted half
	Low, Mid, High float64
	Temp, DewPoint float64
	Wind, Gust     float64 // wind at the hour; gust maximum during the hour that starts there
	Precip         float64 // mm during the hour that starts there; NaN when unknown
}

// RainGate is the precipitation (mm in the hour) from which the sky factor
// is 0; scoring ramps down to it and the table shows rain from half of it.
const RainGate = 0.1

// Level is an upper-air point: pressure (hPa), geopotential height (m),
// temperature (°C), wind speed (km/h) and direction (°).
type Level struct{ P, Z, T, Wind, Dir float64 }

// pressures are the levels fetched, from the top of the boundary layer to
// the jet stream, in the order Open-Meteo's heights increase.
var pressures = [...]int{850, 700, 500, 300, 250, 200}

// levelSeries is one pressure level's hourly block.
type levelSeries struct{ Z, T, Wind, Dir []*float64 }

// hourly is Open-Meteo's hourly block, values null when missing.
type hourly struct {
	Time                                               []int64
	Low, Mid, High, Temp, DewPoint, Wind, Gust, Precip []*float64
	Level                                              [len(pressures)]levelSeries
}

// Forecast fetches hourly weather from Open-Meteo, keyed by unix hour, plus
// the site elevation (NaN if missing), which the aerosol forecast also gives
// but only over its shorter range.
func Forecast(ctx context.Context, lat, lon float64, start, end time.Time) (map[int64]HourWeather, float64, error) {
	vars := []string{"cloud_cover_low", "cloud_cover_mid", "cloud_cover_high", "temperature_2m", "dew_point_2m", "wind_speed_10m", "wind_gusts_10m", "precipitation"}
	for _, p := range pressures {
		k := levelKeys(p)
		vars = append(vars, k[:]...)
	}
	// 2 decimals (~1 km) is finer than the weather models and avoids sending an exact address.
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f&hourly=%s&timeformat=unixtime&timezone=UTC",
		lat, lon, strings.Join(vars, ","))
	var body struct {
		Elevation *float64        `json:"elevation"`
		Reason    string          `json:"reason"`
		Hourly    json.RawMessage `json:"hourly"`
	}
	// One hour past the end: the last hour's gust is in the next entry, which
	// falls on the next UTC day when the night ends after 23:00 UTC.
	if err := getRange(ctx, url, start, end.Add(time.Hour), &body); err != nil {
		return nil, math.NaN(), fmt.Errorf("%w: %w %s", errOpenMeteo, err, sanitize.Text(body.Reason))
	}
	h, err := decodeHourly(body.Hourly)
	if err != nil {
		return nil, math.NaN(), err
	}
	elev := elevation(body.Elevation)
	out, err := h.weather(elev)
	if err != nil {
		return nil, math.NaN(), err
	}
	if len(out) == 0 {
		return nil, math.NaN(), fmt.Errorf("%w: no data for %s", errOpenMeteo, start.Format(time.DateOnly))
	}

	return out, elev, nil
}

// elevation returns a response's site elevation, NaN when missing or
// implausible: a bad value would carry through ExtinctionCoeff's exponential
// (-10000000 m overflows it to +Inf) into every score.
func elevation(p *float64) float64 {
	if p == nil || !num.Finite(*p) || *p < minElevation || *p > maxElevation {
		return math.NaN()
	}

	return *p
}

// maxAOD is the largest aerosol optical depth taken as data.
const maxAOD = 10

// The Dead Sea shore to above Everest, in metres.
const minElevation, maxElevation = -500, 9000

// levelKeys names a pressure level's hourly variables, in levelSeries order:
// both the request and the lookup use these, so they cannot drift apart.
func levelKeys(p int) [4]string {
	return [4]string{
		fmt.Sprintf("geopotential_height_%dhPa", p), fmt.Sprintf("temperature_%dhPa", p),
		fmt.Sprintf("wind_speed_%dhPa", p), fmt.Sprintf("wind_direction_%dhPa", p),
	}
}

// decodeHourly decodes Open-Meteo's hourly block (nil or empty: no data):
// the named series strictly, the pressure levels best-effort, since they
// are display-only and a failure there must cost only them.
func decodeHourly(raw json.RawMessage) (hourly, error) {
	var h hourly
	if len(raw) == 0 {
		return h, nil
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return h, fmt.Errorf("%w: %w", errOpenMeteo, err)
	}
	for key, dst := range map[string]any{
		"time": &h.Time, "cloud_cover_low": &h.Low, "cloud_cover_mid": &h.Mid, "cloud_cover_high": &h.High,
		"temperature_2m": &h.Temp, "dew_point_2m": &h.DewPoint, "wind_speed_10m": &h.Wind,
		"wind_gusts_10m": &h.Gust, "precipitation": &h.Precip,
	} {
		if b, ok := block[key]; ok {
			if err := json.Unmarshal(b, dst); err != nil {
				return h, fmt.Errorf("%w: %s: %w", errOpenMeteo, key, err)
			}
		}
	}
	for k, p := range pressures {
		l := &h.Level[k]
		for j, dst := range [...]*[]*float64{&l.Z, &l.T, &l.Wind, &l.Dir} {
			if b, ok := block[levelKeys(p)[j]]; !ok || json.Unmarshal(b, dst) != nil {
				*dst = nil // absent or unreadable: that series only
			}
		}
	}

	return h, nil
}

// at returns s[i] when present and finite.
func at(s []*float64, i int) (float64, bool) {
	if i >= len(s) || s[i] == nil || !num.Finite(*s[i]) {
		return 0, false
	}

	return *s[i], true
}

// weather turns the hourly block into HourWeather keyed by unix hour. Levels
// below elev (the site, in m; NaN keeps them all) are dropped: at a high site
// 850 hPa lies underground, where the model's values are extrapolated.
func (h *hourly) weather(elev float64) (map[int64]HourWeather, error) {
	// Gust is not in series: a null gust does not drop the hour (see below).
	// Precipitation and the levels are optional too: a missing or short
	// series costs only the rain gate or that hour's seeing class.
	series := [][]*float64{h.Low, h.Mid, h.High, h.Temp, h.DewPoint, h.Wind}
	if len(h.Gust) != len(h.Time) {
		return nil, fmt.Errorf("%w: malformed response", errOpenMeteo)
	}
	for _, s := range series {
		if len(s) != len(h.Time) {
			return nil, fmt.Errorf("%w: malformed response", errOpenMeteo)
		}
	}
	out := map[int64]HourWeather{}
next:
	for i, t := range h.Time {
		// Missing values come back as null; skip rather than read them as clear
		// sky. Gusts are not required: a missing one falls back below.
		for _, s := range series {
			if s[i] == nil {
				continue next
			}
		}
		low, mid, high := *h.Low[i]/100, *h.Mid[i]/100, *h.High[i]/100
		if min(low, mid, high) < 0 || max(low, mid, high) > 1 { // bad data, skipped like null
			continue
		}
		// ponytail: layers treated as independent; thin cirrus blocks ~half the light.
		cloud := 100 * (1 - (1-low)*(1-mid)*(1-high/2))
		// Gusts are the maximum over the preceding hour, so the hour starting
		// at t has its gust in the next entry; the last hour keeps its own,
		// and with neither the mean wind is the floor gusts never go below.
		gust := *h.Wind[i]
		if i+1 < len(h.Time) && h.Time[i+1] == t+3600 && h.Gust[i+1] != nil {
			gust = *h.Gust[i+1]
		} else if h.Gust[i] != nil {
			gust = *h.Gust[i]
		}
		// Precipitation is the preceding hour's sum like the gusts, shifted the
		// same way; without a next entry it stays unknown, since the hour's own
		// value is the previous hour's rain, and a gate must not misfire.
		precip := math.NaN()
		if v, ok := at(h.Precip, i+1); ok && v >= 0 && i+1 < len(h.Time) && h.Time[i+1] == t+3600 {
			precip = v
		}
		var levels []Level
		for k, p := range pressures {
			l := Level{P: float64(p)}
			var okZ, okT, okW, okD bool
			l.Z, okZ = at(h.Level[k].Z, i)
			l.T, okT = at(h.Level[k].T, i)
			l.Wind, okW = at(h.Level[k].Wind, i)
			l.Dir, okD = at(h.Level[k].Dir, i)
			if okZ && okT && okW && okD && plausible(l) && (math.IsNaN(elev) || l.Z >= elev) {
				levels = append(levels, l)
			}
		}
		out[t] = HourWeather{
			Levels: levels, Cloud: cloud, Low: *h.Low[i], Mid: *h.Mid[i], High: *h.High[i],
			Temp: *h.Temp[i], DewPoint: *h.DewPoint[i], Wind: *h.Wind[i], Gust: gust, Precip: precip,
		}
	}

	return out, nil
}

// plausible reports whether an upper-air level's values are physical: a
// sentinel (-9999) or an overflowing value would carry Inf, then NaN,
// through the shear and Richardson number into Seeing.
func plausible(l Level) bool {
	return l.Z >= -1000 && l.Z <= 30000 && l.T >= -150 && l.T <= 60 &&
		l.Wind >= 0 && l.Wind <= 1000 && l.Dir >= 0 && l.Dir <= 360
}

// Seeing classes the hour's seeing 1 (steady) to 5 (turbulent) from the
// upper-air profile, 0 without one (fewer than three levels, or none at
// 300 hPa or above, which the jet term reads): the display-only fallback
// for hours 7Timer's seeing does not cover. The
// strongest wind shear between adjacent levels and the jet (the fastest wind
// at 300 hPa and above) weigh equally into a 0-100 quality that maps onto
// the classes in steps of 20. A layer whose gradient Richardson number
// (buoyancy over shear squared) is below 0.25, dynamically unstable or
// convective, adds a class; between levels kilometres apart the shear
// averages out and Ri is 4-19 on an ordinary night, so this is a flag for
// the exceptional layer, not a graded term. Gusts above 30 km/h, stirring
// the boundary layer below the lowest level, add another.
// ponytail: thresholds are amateur rules of thumb after
// markusschierz/astro-forecast (shear 2.5-18 m/s/km, jet 8-40 m/s), not a
// Cn² model.
func Seeing(h HourWeather) int {
	if len(h.Levels) < 3 || h.Levels[len(h.Levels)-1].P > 300 { // lowest first
		return 0
	}
	const g = 9.80665
	minRi, maxShear, jet := math.Inf(1), 0.0, 0.0
	for i := 1; i < len(h.Levels); i++ {
		a, b := h.Levels[i-1], h.Levels[i]
		dz := b.Z - a.Z
		if dz < 50 {
			continue
		}
		au, av := wind(a)
		bu, bv := wind(b)
		shear2 := max(((bu-au)*(bu-au)+(bv-av)*(bv-av))/(dz*dz), 1e-8)
		ta, tb := theta(a), theta(b)
		minRi = min(minRi, g/((ta+tb)/2)*(tb-ta)/dz/shear2)
		maxShear = max(maxShear, math.Sqrt(shear2)*1000)
	}
	for _, l := range h.Levels {
		if l.P <= 300 {
			jet = max(jet, l.Wind/3.6)
		}
	}
	q := 0.5*quality(maxShear, 2.5, 18) + 0.5*quality(jet, 8, 40)
	class := 1 + int((100-q)/20)
	if minRi < 0.25 {
		class++
	}
	if h.Gust > 30 {
		class++
	}

	return min(class, 5)
}

// wind returns the level's wind as east and north components in m/s.
func wind(l Level) (float64, float64) {
	s, c := math.Sincos(l.Dir * math.Pi / 180)
	ms := l.Wind / 3.6

	return -ms * s, -ms * c
}

// theta is the level's potential temperature in K.
func theta(l Level) float64 { return (l.T + 273.15) * math.Pow(1000/l.P, 0.286) }

// quality maps v onto 100 at or below good, 0 at or above bad, linearly between.
func quality(v, good, bad float64) float64 { return 100 * (1 - max(0, min(1, (v-good)/(bad-good)))) }

type AstroBlock struct{ Seeing, Transparency int } // 7Timer scales, 1 = best, 8 = worst

// AstroForecast fetches 7Timer's ASTRO product (3-hourly, ~3 days ahead),
// keyed by the unix time of each forecast point.
func AstroForecast(ctx context.Context, lat, lon float64) (map[int64]AstroBlock, error) {
	url := fmt.Sprintf("https://www.7timer.info/bin/astro.php?lon=%.2f&lat=%.2f&ac=0&unit=metric&output=json", lon, lat)
	// 7Timer sends no CORS headers, so the browser build goes through a
	// Cloudflare Worker (github.com/dkorunic/astro-recommender-cloudflare)
	// that adds them for the GitHub Pages origin only, caches for an hour, and
	// appends output=json itself (it rejects parameters it does not know).
	if runtime.GOOS == "js" {
		url = fmt.Sprintf("https://astro-recommender-proxy.dkorunic.workers.dev/api?lon=%.2f&lat=%.2f&product=astro&unit=metric&ac=0", lon, lat)
	}
	var body struct {
		Init       string `json:"init"` // YYYYMMDDHH UTC
		Dataseries []struct {
			Timepoint    int `json:"timepoint"` // hours after init
			Seeing       int `json:"seeing"`
			Transparency int `json:"transparency"`
		} `json:"dataseries"`
	}
	if err := fetch.GetJSON(ctx, url, &body); err != nil {
		return nil, fmt.Errorf("%w: %w", errSevenTimer, err)
	}
	init, err := time.Parse("2006010215", body.Init)
	if err != nil {
		return nil, fmt.Errorf("%w: bad init %q", errSevenTimer, body.Init)
	}
	out := map[int64]AstroBlock{}
	for _, d := range body.Dataseries {
		// Out-of-range values (e.g. -9999) mean missing data. The product runs
		// 72 h ahead, so a timepoint past 30 days is garbage, not a forecast.
		if d.Seeing < 1 || d.Seeing > 8 || d.Transparency < 1 || d.Transparency > 8 || d.Timepoint < 0 || d.Timepoint > 24*30 {
			continue
		}
		out[init.Add(time.Duration(d.Timepoint)*time.Hour).Unix()] = AstroBlock{d.Seeing, d.Transparency}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no data", errSevenTimer)
	}

	return out, nil
}

// AstroKey maps t to the nearest 7Timer forecast point. Runs start at 00/06/12/18 UTC
// and step 3 h, so points sit on UTC hours divisible by 3 (time.Round is UTC-aligned).
func AstroKey(t time.Time) int64 {
	return t.Round(3 * time.Hour).Unix()
}

// SeeingLabel renders 7Timer seeing classes (1-8) as arc second ranges, "-"
// for any other value.
func SeeingLabel(s int) string {
	labels := [...]string{`<0.5"`, `0.5-0.75"`, `0.75-1"`, `1-1.25"`, `1.25-1.5"`, `1.5-2"`, `2-2.5"`, `>2.5"`}
	if s < 1 || s > len(labels) {
		return "-"
	}

	return labels[s-1]
}

// AerosolForecast fetches hourly aerosol optical depth at 550 nm (CAMS) from
// the Open-Meteo Air Quality API, keyed by unix hour, plus the site elevation.
func AerosolForecast(ctx context.Context, lat, lon float64, start, end time.Time) (map[int64]float64, float64, error) {
	url := fmt.Sprintf("https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.2f&longitude=%.2f"+
		"&hourly=aerosol_optical_depth&timeformat=unixtime&timezone=UTC", lat, lon)
	//nolint:tagliatelle // Open-Meteo's field names
	var body struct {
		Reason    string   `json:"reason"`
		Elevation *float64 `json:"elevation"`
		Hourly    struct {
			Time []int64    `json:"time"`
			AOD  []*float64 `json:"aerosol_optical_depth"`
		} `json:"hourly"`
	}
	if err := getRange(ctx, url, start, end, &body); err != nil {
		return nil, math.NaN(), fmt.Errorf("%w: %w %s", errAirQuality, err, sanitize.Text(body.Reason))
	}
	// A missing elevation is NaN, not a failure: the AOD does not depend on
	// it, and FetchForecast falls back to the weather forecast's.
	elev := elevation(body.Elevation)
	if len(body.Hourly.AOD) != len(body.Hourly.Time) {
		return nil, math.NaN(), fmt.Errorf("%w: malformed response", errAirQuality)
	}
	out := map[int64]float64{}
	for i, t := range body.Hourly.Time {
		// Negative AOD is bad data: it would push extinction below 0 and scores
		// above 1. Above maxAOD (real CAMS values stay under ~5, dust storms
		// included) it would zero every score for the hour; both fall back
		// to TypicalAOD like a missing hour.
		if a := body.Hourly.AOD[i]; a != nil && *a >= 0 && *a <= maxAOD {
			out[t] = *a
		}
	}

	return out, elev, nil
}

// getRange GETs an Open-Meteo url for the UTC dates of start to end, decoding
// into v. Open-Meteo rejects a range reaching past its data instead of
// returning the part it has (HTTP 400), so a rejected multi-day range is
// retried once ending on its first day: a night that runs past the forecast's
// last day still gets its evening, and BuildSky gives the uncovered rest the
// covered hours' mean quality. Other failures are not narrowed (fetch retries
// a rate limit or server error once as is).
func getRange[T any](ctx context.Context, url string, start, end time.Time, v *T) error {
	from, to := start.UTC().Format(time.DateOnly), end.UTC().Format(time.DateOnly)
	err := fetch.GetJSON(ctx, url+"&start_date="+from+"&end_date="+to, v)
	var status *fetch.StatusError
	if errors.As(err, &status) && status.Code == http.StatusBadRequest && to != from {
		// Cleared: a retry failing without a body must not report the 400's reason.
		*v = *new(T)
		err = fetch.GetJSON(ctx, url+"&start_date="+from+"&end_date="+from, v)
	}

	return err
}
