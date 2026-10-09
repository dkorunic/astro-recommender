// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package weather fetches Open-Meteo weather, upper-air and aerosol forecasts
// and 7Timer transparency and seeing, with a seeing estimate from the
// upper-air profile as the fallback.
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
	Levels         []Level // the upper-air levels (Profile), lowest first (display-only: Seeing)
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
// temperature (°C), wind speed (km/h) and direction (°). The surface is one
// too: the site elevation with the surface pressure, 2 m temperature and
// 10 m wind.
type Level struct{ P, Z, T, Wind, Dir float64 }

// pressures are the levels Profile fetches, from the boundary layer to the
// lower stratosphere, in the order Open-Meteo's heights increase. ECMWF IFS
// 0.25 has no 975-950 or 900 hPa.
var pressures = [...]int{1000, 925, 850, 700, 600, 500, 400, 300, 250, 200, 150, 100}

// levelSeries is one pressure level's hourly block.
type levelSeries struct{ Z, T, Wind, Dir []*float64 }

// hourly is Open-Meteo's hourly block, values null when missing. Forecast
// fills the surface weather, Profile the levels with SurfP, Temp, Wind and Dir.
type hourly struct {
	Time                                                           []int64
	Low, Mid, High, Temp, DewPoint, Wind, Gust, Precip, SurfP, Dir []*float64
	Level                                                          [len(pressures)]levelSeries
}

// Forecast fetches hourly weather from Open-Meteo, keyed by unix hour, plus
// the site elevation (NaN if missing), which the aerosol forecast also gives
// but only over its shorter range.
func Forecast(ctx context.Context, lat, lon float64, start, end time.Time) (map[int64]HourWeather, float64, error) {
	vars := "cloud_cover_low,cloud_cover_mid,cloud_cover_high,temperature_2m,dew_point_2m,wind_speed_10m,wind_gusts_10m,precipitation"
	// 2 decimals (~1 km) is finer than the weather models and avoids sending an exact address.
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f&hourly=%s&timeformat=unixtime&timezone=UTC",
		lat, lon, vars)
	// One hour past the end: the last hour's gust is in the next entry, which
	// falls on the next UTC day when the night ends after 23:00 UTC.
	h, elev, err := fetchHourly(ctx, url, start, end.Add(time.Hour))
	if err != nil {
		return nil, math.NaN(), err
	}
	out, err := h.weather()
	if err != nil {
		return nil, math.NaN(), err
	}
	if len(out) == 0 {
		return nil, math.NaN(), fmt.Errorf("%w: no data for %s", errOpenMeteo, start.UTC().Format(time.DateOnly))
	}

	return out, elev, nil
}

// fetchHourly gets an Open-Meteo forecast for [start, end) and decodes its
// hourly block; Forecast and Profile differ only in the URL and in what they
// make of the block. The elevation is NaN when missing or implausible.
func fetchHourly(ctx context.Context, url string, start, end time.Time) (hourly, float64, error) {
	var body struct {
		Elevation *float64        `json:"elevation"`
		Reason    string          `json:"reason"`
		Hourly    json.RawMessage `json:"hourly"`
	}
	if err := getRange(ctx, url, start, end, &body); err != nil {
		return hourly{}, math.NaN(), fmt.Errorf("%w: %w %s", errOpenMeteo, err, sanitize.Text(body.Reason))
	}
	h, err := decodeHourly(body.Hourly)

	return h, elevation(body.Elevation), err
}

// Profile fetches the upper-air levels per unix hour from ECMWF IFS 0.25
// (3-hourly, interpolated to hours by Open-Meteo, 15 days ahead) for
// Seeing. Its own model rather than Forecast's best_match blend, whose
// pressure levels come from whichever model covers the site, so the
// estimate means the same thing everywhere; the surface fields come along
// so the ground layer's gradient is read within one model.
func Profile(ctx context.Context, lat, lon float64, start, end time.Time) (map[int64][]Level, error) {
	vars := []string{"surface_pressure", "temperature_2m", "wind_speed_10m", "wind_direction_10m"}
	for _, p := range pressures {
		k := levelKeys(p)
		vars = append(vars, k[:]...)
	}
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f&models=ecmwf_ifs025&hourly=%s&timeformat=unixtime&timezone=UTC",
		lat, lon, strings.Join(vars, ","))
	h, elev, err := fetchHourly(ctx, url, start, end)
	if err != nil {
		return nil, err
	}
	out := h.levels(elev)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no upper-air data for %s", errOpenMeteo, start.UTC().Format(time.DateOnly))
	}

	return out, nil
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
		"wind_gusts_10m": &h.Gust, "precipitation": &h.Precip, "surface_pressure": &h.SurfP, "wind_direction_10m": &h.Dir,
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

// weather turns the hourly block into HourWeather keyed by unix hour.
func (h *hourly) weather() (map[int64]HourWeather, error) {
	// Gust is not in series: a null gust does not drop the hour (see below).
	// Precipitation is optional too: a missing or short series costs only
	// the rain gate.
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
		out[t] = HourWeather{
			Cloud: cloud, Low: *h.Low[i], Mid: *h.Mid[i], High: *h.High[i],
			Temp: *h.Temp[i], DewPoint: *h.DewPoint[i], Wind: *h.Wind[i], Gust: gust, Precip: precip,
		}
	}

	return out, nil
}

// levels turns the profile block into the upper-air levels per unix hour,
// lowest first: the surface (elev, the site in m, with the surface pressure,
// 2 m temperature and 10 m wind), then every pressure level with all four
// values, physical, and at least groundLayer above the surface. The surface
// sits at 2 m, where its temperature is read. An hour is left out without
// the elevation or a surface level, since the ground layer is the
// estimate's largest term (the surface fields come in the same request, so
// their absence means a broken hour), and with fewer than three pressure
// levels, since two multi-kilometre layers are no profile.
// ponytail: the level heights are the model's, absolute, while elev is
// Open-Meteo's 90 m terrain model, so in a deep valley a level underground
// in the model's own coarse orography can pass with extrapolated values;
// nothing from our side tells, and groundLayer keeps such a level from
// forming a thin ground layer at least.
func (h *hourly) levels(elev float64) map[int64][]Level {
	out := map[int64][]Level{}
	if math.IsNaN(elev) {
		return out
	}
	for i, t := range h.Time {
		l := Level{Z: elev + 2}
		if !(fill(&l.P, h.SurfP, i) && fill(&l.T, h.Temp, i) && fill(&l.Wind, h.Wind, i) && fill(&l.Dir, h.Dir, i) && plausible(l)) {
			continue
		}
		levels, minZ := []Level{l}, l.Z+groundLayer
		for k, p := range pressures {
			l, s := Level{P: float64(p)}, h.Level[k]
			if fill(&l.Z, s.Z, i) && fill(&l.T, s.T, i) && fill(&l.Wind, s.Wind, i) && fill(&l.Dir, s.Dir, i) && plausible(l) && l.Z >= minZ {
				levels = append(levels, l)
			}
		}
		if len(levels) >= 4 {
			out[t] = levels
		}
	}

	return out
}

// groundLayer is the least height (m) of the first level above the surface.
// Dewan's outer scale was fitted on kilometre-thick radiosonde layers: over
// a 50 m ground layer an ordinary 15 km/h wind difference reads as a shear
// that puts 10^2-10^4 into Cn² and a 100" estimate on the table. At 300 m
// the ground layer is surface to 925 hPa (800 m) or higher at any site, and
// the same cases come out at 1.2-1.3".
const groundLayer = 300

// fill stores s[i] in dst and reports whether it was present and finite.
func fill(dst *float64, s []*float64, i int) bool {
	v, ok := at(s, i)
	*dst = v

	return ok
}

// plausible reports whether a level's values are physical: a sentinel
// (-9999) or an overflowing value would carry Inf, then NaN, through the
// shear and the temperature gradient into Seeing.
func plausible(l Level) bool {
	return l.P >= 50 && l.P <= 1100 && l.Z >= -1000 && l.Z <= 30000 && l.T >= -150 && l.T <= 60 &&
		l.Wind >= 0 && l.Wind <= 1000 && l.Dir >= 0 && l.Dir <= 360
}

// Seeing estimates the hour's seeing, the FWHM of a star image in
// arcseconds at the zenith and 500 nm, from the upper-air profile: 0 without
// one (fewer than three levels, or none at 300 hPa or above, so the integral
// always spans the jet). Each layer between adjacent levels, the surface
// first when known, gets Dewan et al.'s (1993) Cn² (cn2) from its
// temperature gradient and wind shear; the integral over height gives
// Fried's r0 = (0.423 k² ∫Cn² dz)^(-3/5) and FWHM = 0.98 λ/r0. Above
// maxSeeing, or not finite, it is 0 too: values plausible one by one can
// still be nonsense together (opposed 1000 km/h winds put 10^470 in the
// outer scale), and a +Inf would abort the JSON encoder. It is
// display-only, for hours 7Timer's seeing does not cover.
// ponytail: the levels are 0.7-2 km apart, so the gradients average out
// and thin turbulent layers are missed, and the ground layer is one
// gradient from 2 m to the first level above. Fine for ranking nights; an
// observatory's Cn² profiler it is not.
func Seeing(h HourWeather) float64 {
	if len(h.Levels) < 3 || h.Levels[len(h.Levels)-1].P > 300 { // lowest first
		return 0
	}
	integral, trop := 0.0, tropopause(h.Levels)
	for i := 1; i < len(h.Levels); i++ {
		a, b := h.Levels[i-1], h.Levels[i]
		if dz := b.Z - a.Z; dz >= 50 {
			integral += cn2(a, b, i-1 >= trop) * dz
		}
	}
	const lambda = 500e-9
	k := 2 * math.Pi / lambda
	r0 := math.Pow(0.423*k*k*integral, -0.6)
	fwhm := 0.98 * lambda / r0 * 180 / math.Pi * 3600
	if !num.Finite(fwhm) || fwhm > maxSeeing {
		return 0
	}

	return fwhm
}

// maxSeeing is the largest FWHM (arcseconds) taken as an estimate; the
// worst sites see 5".
const maxSeeing = 30

// l0Scale is 0.1^(4/3), the outer scale's prefactor in cn2.
const l0Scale = 0.046415888336127795

// cn2 is the refractive index structure constant (m^-2/3) of the layer from
// a up to b after Dewan et al. (1993), the AFGL radiosonde model:
// Cn² = 2.8 M² L0^(4/3), with M = -79e-6 P/T² (dT/dz + Γ) the gradient of
// the potential refractive index (P in hPa, T in K, Γ = 9.8 K/km the dry
// adiabatic lapse rate) and the outer scale L0^(4/3) = 0.1^(4/3) 10^Y,
// Y = 1.64 + 42 S in the troposphere, 0.506 + 50 S in the stratosphere
// (stratospheric), S the vector wind shear in 1/s.
func cn2(a, b Level, stratospheric bool) float64 {
	dz := b.Z - a.Z
	p, t := (a.P+b.P)/2, (a.T+b.T)/2+273.15
	au, av := wind(a)
	bu, bv := wind(b)
	s := math.Hypot(bu-au, bv-av) / dz
	m := -79e-6 * p / (t * t) * ((b.T-a.T)/dz + 9.8e-3)
	y := 1.64 + 42*s
	if stratospheric {
		y = 0.506 + 50*s
	}

	return 2.8 * m * m * l0Scale * math.Pow(10, y)
}

// tropopause returns the index of the level at the tropopause, the WMO
// definition read on the forecast levels: searched above 500 hPa, the base
// of the lowest layer whose lapse rate is 2 K/km or less (cooling less than
// that, or warming) and whose next layer, when there is one, qualifies too,
// standing in for the rule's average over the next 2 km (the levels there
// are 1.2-2.3 km apart). The layers from it up are stratospheric, whatever
// their own lapse rate. Without one every layer is tropospheric: the top
// index (-1 for no levels).
func tropopause(levels []Level) int {
	// qualifies is the layer from level i up; a pair under 50 m apart has no
	// readable gradient and is Seeing's skipped layer too.
	qualifies := func(i int) bool {
		dz := levels[i+1].Z - levels[i].Z

		return dz >= 50 && (levels[i+1].T-levels[i].T)/dz >= -2e-3
	}
	for i := 0; i+1 < len(levels); i++ {
		if (levels[i].P+levels[i+1].P)/2 <= 500 && qualifies(i) && (i+2 >= len(levels) || qualifies(i+1)) {
			return i
		}
	}

	return len(levels) - 1
}

// wind returns the level's wind as east and north components in m/s.
func wind(l Level) (float64, float64) {
	s, c := math.Sincos(l.Dir * math.Pi / 180)
	ms := l.Wind / 3.6

	return -ms * s, -ms * c
}

type AstroBlock struct{ Seeing, Transparency int } // 7Timer scales, 1 = best, 8 = worst

// AstroForecast fetches 7Timer's ASTRO product (3-hourly, ~3 days ahead),
// keyed by the unix time of each forecast point.
func AstroForecast(ctx context.Context, lat, lon float64) (map[int64]AstroBlock, error) {
	url := fmt.Sprintf("https://www.7timer.info/bin/astro.php?lon=%.2f&lat=%.2f&ac=0&unit=metric&output=json", lon, lat)
	// 7Timer sends no CORS headers, so the browser build goes through a
	// Cloudflare Worker (github.com/dkorunic/astro-recommender-cloudflare)
	// that adds them for the site's origin (astrorecommender.org) only, caches
	// for an hour, and appends output=json itself (it rejects parameters it
	// does not know).
	if runtime.GOOS == "js" {
		url = fmt.Sprintf("https://proxy.astrorecommender.org/api?lon=%.2f&lat=%.2f&product=astro&unit=metric&ac=0", lon, lat)
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
