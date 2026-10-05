// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package weather fetches Open-Meteo weather and aerosol forecasts and 7Timer
// transparency and seeing.
package weather

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
)

var (
	errOpenMeteo  = errors.New("open-meteo")
	errAirQuality = errors.New("open-meteo air quality")
	errSevenTimer = errors.New("7timer")
)

type HourWeather struct {
	Cloud          float64 // effective cover, thin high cloud counted half
	Low, Mid, High float64
	Temp, DewPoint float64
	Wind, Gust     float64 // wind at the hour; gust maximum during the hour that starts there
}

// hourly is Open-Meteo's hourly block, values null when missing.
//
//nolint:tagliatelle // Open-Meteo's field names
type hourly struct {
	Time     []int64    `json:"time"`
	Low      []*float64 `json:"cloud_cover_low"`
	Mid      []*float64 `json:"cloud_cover_mid"`
	High     []*float64 `json:"cloud_cover_high"`
	Temp     []*float64 `json:"temperature_2m"`
	DewPoint []*float64 `json:"dew_point_2m"`
	Wind     []*float64 `json:"wind_speed_10m"`
	Gust     []*float64 `json:"wind_gusts_10m"`
}

// Forecast fetches hourly weather from Open-Meteo, keyed by unix hour.
func Forecast(ctx context.Context, lat, lon float64, start, end time.Time) (map[int64]HourWeather, error) {
	// 2 decimals (~1 km) is finer than the weather models and avoids sending an exact address.
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f"+
		"&hourly=cloud_cover_low,cloud_cover_mid,cloud_cover_high,temperature_2m,dew_point_2m,wind_speed_10m,wind_gusts_10m"+
		"&timeformat=unixtime&timezone=UTC", lat, lon)
	var body struct {
		Reason string `json:"reason"`
		Hourly hourly `json:"hourly"`
	}
	// One hour past the end: the last hour's gust is in the next entry, which
	// falls on the next UTC day when the night ends after 23:00 UTC.
	if err := getRange(ctx, url, start, end.Add(time.Hour), &body); err != nil {
		return nil, fmt.Errorf("%w: %w %s", errOpenMeteo, err, sanitize.Text(body.Reason))
	}
	out, err := body.Hourly.weather()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no data for %s", errOpenMeteo, start.Format(time.DateOnly))
	}

	return out, nil
}

// weather turns the hourly block into HourWeather keyed by unix hour.
func (h *hourly) weather() (map[int64]HourWeather, error) {
	series := [][]*float64{h.Low, h.Mid, h.High, h.Temp, h.DewPoint, h.Wind}
	for _, s := range append(series, h.Gust) {
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
		out[t] = HourWeather{cloud, *h.Low[i], *h.Mid[i], *h.High[i], *h.Temp[i], *h.DewPoint[i], *h.Wind[i], gust}
	}

	return out, nil
}

type AstroBlock struct{ Seeing, Transparency int } // 7Timer scales, 1 = best, 8 = worst

// AstroForecast fetches 7Timer's ASTRO product (3-hourly, ~3 days ahead),
// keyed by the unix time of each forecast point.
func AstroForecast(ctx context.Context, lat, lon float64) (map[int64]AstroBlock, error) {
	url := fmt.Sprintf("https://www.7timer.info/bin/astro.php?lon=%.2f&lat=%.2f&ac=0&unit=metric&output=json", lon, lat)
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
		// Out-of-range values (e.g. -9999) mean missing data.
		if d.Seeing < 1 || d.Seeing > 8 || d.Transparency < 1 || d.Transparency > 8 {
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

// SeeingLabel renders 7Timer seeing classes as arc second ranges.
func SeeingLabel(s int) string {
	return [...]string{`<0.5"`, `0.5-0.75"`, `0.75-1"`, `1-1.25"`, `1.25-1.5"`, `1.5-2"`, `2-2.5"`, `>2.5"`}[s-1]
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
	if body.Elevation == nil || len(body.Hourly.AOD) != len(body.Hourly.Time) {
		return nil, math.NaN(), fmt.Errorf("%w: malformed response", errAirQuality)
	}
	out := map[int64]float64{}
	for i, t := range body.Hourly.Time {
		if a := body.Hourly.AOD[i]; a != nil {
			out[t] = *a
		}
	}

	return out, *body.Elevation, nil
}

// getRange GETs an Open-Meteo url for the UTC dates of start to end, decoding
// into v. Open-Meteo rejects a range reaching past its data instead of
// returning the part it has (HTTP 400), so a rejected multi-day range is
// retried once ending on its first day: a night that runs past the forecast's
// last day still gets its evening, and BuildSky warns about the uncovered
// rest. Other failures (rate limits, server errors) are not retried.
func getRange(ctx context.Context, url string, start, end time.Time, v any) error {
	from, to := start.UTC().Format(time.DateOnly), end.UTC().Format(time.DateOnly)
	err := fetch.GetJSON(ctx, url+"&start_date="+from+"&end_date="+to, v)
	var status *fetch.StatusError
	if errors.As(err, &status) && status.Code == http.StatusBadRequest && to != from {
		err = fetch.GetJSON(ctx, url+"&start_date="+from+"&end_date="+from, v)
	}

	return err
}
