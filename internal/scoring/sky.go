// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// Sky holds everything about the night that does not depend on the target,
// sampled on a 1-minute grid (as in uptonight).
type Sky struct {
	Night      [2]time.Time // full night at -twilight's Sun limit; start/end may be clipped inside it
	Start, End time.Time
	Weather    map[int64]weather.HourWeather
	Astro      map[int64]weather.AstroBlock
	AOD        map[int64]float64 // aerosol optical depth at 550 nm per unix hour (CAMS)
	Grid       []time.Time
	LST        []float64 // local sidereal time per grid minute, degrees
	SinLST     []float64 // sin and cos of LST, so a fixed target's cos(hour angle) needs no Cos
	CosLST     []float64
	MoonPos    [][2]float64 // topocentric RA/Dec of date, degrees
	MoonUnit   [][3]float64 // astro.Unit of MoonPos, for the per-minute Moon separations
	MoonAlt    []float64
	Ext        []float64 // extinction per grid minute, mag per airmass
	MoonLight  []float64 // atmos.MoonLight per grid minute
	Quality    []float64 // clear-sky fraction x rain x dew x gust x transparency, 1 = perfect
	Elevation  float64   // site elevation in m, from Open-Meteo; NaN if unknown
	Illum      float64
	MoonPhase  float64 // phase angle in degrees, 0 = full
	MoonDist   float64 // geocentric distance in Earth radii
	MoonSep    float64 // uptonight: min separation in degrees = illumination %
	ZenithNL   float64 // site's moonless zenith sky brightness, nanoLamberts
	RefNL      float64 // refZenithMag in nanoLamberts
	Comets     int     // comets added as targets
	CometsLost bool    // the comet elements could not be fetched
}

// Forecast is the network input of BuildSky; the zero value (with NaN
// Elevation, see NoForecast) means none.
type Forecast struct {
	Weather   map[int64]weather.HourWeather
	Astro     map[int64]weather.AstroBlock
	AOD       map[int64]float64
	Elevation float64 // NaN if unknown
}

// NoForecast is a Forecast with nothing fetched: perfect sky, fixed extinction.
func NoForecast() Forecast { return Forecast{Elevation: math.NaN()} }

// FetchForecast downloads the weather, transparency and aerosol forecasts
// for [start, end) concurrently, as -no-weather and -extinction allow. A
// failed source is a stderr warning and stays empty.
func FetchForecast(ctx context.Context, cfg *config.Config, start, end time.Time) Forecast {
	f := NoForecast()
	if cfg.NoWeather {
		return f
	}
	var wg sync.WaitGroup
	weatherElev, aodElev := math.NaN(), math.NaN()
	var profile map[int64][]weather.Level
	wg.Go(func() {
		var err error
		if f.Weather, weatherElev, err = weather.Forecast(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no weather forecast:", err)
		}
	})
	wg.Go(func() {
		var err error
		if profile, err = weather.Profile(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no upper-air profile, no seeing estimate:", err)
		}
	})
	wg.Go(func() {
		var err error
		if f.Astro, err = weather.AstroForecast(ctx, cfg.Lat, cfg.Lon); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no transparency forecast:", err)
		}
	})
	if !cfg.ExtinctionSet {
		wg.Go(func() {
			var err error
			if f.AOD, aodElev, err = weather.AerosolForecast(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
				fmt.Fprintln(os.Stderr, "warning: no aerosol forecast, typical aerosols:", err)
			}
		})
	}
	wg.Wait()
	mergeProfile(f.Weather, profile)
	if noSeeing(f, profile != nil, start, end) {
		fmt.Fprintln(os.Stderr, "warning: no upper-air profile for the hours without a 7Timer point; no seeing estimate")
	}
	// The elevation alone gives extinction with typical aerosols, so the
	// weather forecast's (longer range) stands in when CAMS has none. An
	// explicit -extinction keeps it NaN: fixed extinction.
	if !cfg.ExtinctionSet {
		f.Elevation = aodElev
		if math.IsNaN(f.Elevation) {
			f.Elevation = weatherElev
		}
	}

	return f
}

// mergeProfile attaches the upper-air levels to the hours the surface
// forecast has: the table and JSON read them through Weather. Without a
// surface forecast there is no table to show them in, so a profile then
// goes unused; the weather warning already says so.
func mergeProfile(w map[int64]weather.HourWeather, profile map[int64][]weather.Level) {
	for t, lv := range profile {
		if h, ok := w[t]; ok {
			h.Levels = lv
			w[t] = h
		}
	}
}

// noSeeing reports whether the SEEING column falls back to the upper-air
// estimate for some hour of [start, end) (weather but no 7Timer point) and
// gets none for any: the profile's only reader, so its absence would
// otherwise show only as a column of dashes. A profile that was not fetched
// (fetched false) has had its own warning. One that was may still miss the
// hours (the ECMWF range ends a day before best_match's) or be unusable;
// the warning does not tell which.
func noSeeing(f Forecast, fetched bool, start, end time.Time) bool {
	if !fetched {
		return false
	}
	needed := false
	for t := start.Truncate(time.Hour); t.Before(end); t = t.Add(time.Hour) {
		h, ok := f.Weather[t.Unix()]
		if _, astro := f.Astro[weather.AstroKey(t)]; !ok || astro {
			continue
		}
		if weather.Seeing(h) > 0 {
			return false
		}
		needed = true
	}

	return needed
}

// BuildSky samples the night [start, end) on a 1-minute grid with forecast
// f; it does no I/O.
func BuildSky(cfg *config.Config, f Forecast, start, end time.Time) Sky {
	s := Sky{Start: start, End: end, Weather: f.Weather, Astro: f.Astro, AOD: f.AOD, Elevation: f.Elevation}
	for t := start; t.Before(end); t = t.Add(time.Minute) {
		s.Grid = append(s.Grid, t)
	}
	mid := s.Grid[len(s.Grid)/2]
	s.Illum = astro.MoonIllumination(mid)
	s.MoonSep = s.Illum * 100
	s.MoonPhase = astro.MoonPhaseAngle(mid)
	s.MoonDist = astro.MoonDistance(mid)
	s.ZenithNL = atmos.NanoLamberts(cfg.ZenithMag())
	s.RefNL = atmos.NanoLamberts(atmos.RefZenithMag)

	s.LST = make([]float64, len(s.Grid))
	s.MoonPos = make([][2]float64, len(s.Grid))
	s.MoonAlt = make([]float64, len(s.Grid))
	s.MoonUnit = make([][3]float64, len(s.Grid))
	s.SinLST, s.CosLST = make([]float64, len(s.Grid)), make([]float64, len(s.Grid))
	for i, t := range s.Grid {
		s.LST[i] = astro.LST(t, cfg.Lon)
		s.SinLST[i], s.CosLST[i] = math.Sincos(s.LST[i] * deg)
		s.MoonPos[i][0], s.MoonPos[i][1] = astro.MoonTopo(t, cfg.Lat, cfg.Lon)
		s.MoonUnit[i] = astro.Unit(s.MoonPos[i][0], s.MoonPos[i][1])
		s.MoonAlt[i] = astro.Altitude(s.MoonPos[i][0], s.MoonPos[i][1], t, cfg.Lat, cfg.Lon)
	}

	s.Quality = make([]float64, len(s.Grid))
	s.Ext = make([]float64, len(s.Grid))
	s.MoonLight = make([]float64, len(s.Grid))
	// Weather and transparency per minute; ok marks the minutes each forecast covers.
	sky, skyOK := make([]float64, len(s.Grid)), make([]bool, len(s.Grid))
	transp, transpOK := make([]float64, len(s.Grid)), make([]bool, len(s.Grid))
	for i, t := range s.Grid {
		s.Ext[i] = s.ExtinctionAt(t, cfg.Extinction)
		s.MoonLight[i] = atmos.MoonLight(s.Ext[i], s.MoonAlt[i], s.MoonPhase, s.MoonDist)
		if h, ok := s.Weather[t.Truncate(time.Hour).Unix()]; ok {
			// ponytail: guessed ramps; dew (spread 4 -> 1 °C) and gusts (20 -> 40 km/h)
			// cost up to 30% and 50% of usable frames; rain (0 -> RainGate mm
			// in the hour) all of them, whatever the cloud layers say. Like cloud it
			// weighs the score only: FOTO and the observable runs are geometric.
			sky[i] = (1 - h.Cloud/100) * ramp(h.Temp-h.DewPoint, 4, 1, 0.7) * ramp(h.Gust, 20, 40, 0.5)
			if h.Precip > 0 { // NaN (unknown) does not gate
				sky[i] *= ramp(h.Precip, 0, weather.RainGate, 0)
			}
			skyOK[i] = true
		}
		if a, ok := s.Astro[weather.AstroKey(t)]; ok {
			transp[i] = ramp(float64(a.Transparency), 1, 8, 0.5)
			transpOK[i] = true
		}
	}
	fillGaps("weather", sky, skyOK, len(s.Weather) > 0, end.Sub(start))
	fillGaps("transparency", transp, transpOK, len(s.Astro) > 0, end.Sub(start))
	for i := range s.Quality {
		s.Quality[i] = sky[i] * transp[i]
	}

	return s
}

// fillGaps gives the minutes a forecast does not cover (ok false) the covered
// minutes' mean, not a perfect sky that would favour targets up only then,
// and warns about them when a forecast was fetched. Without any coverage
// every minute is perfect (1): silently without a forecast (-no-weather,
// failed fetch), with a warning when the fetched one misses the whole window
// (7Timer has no date parameter, so a -date past its ~3 days gets a forecast
// for other nights). 7Timer ends sooner than Open-Meteo, so their gaps differ.
func fillGaps(what string, v []float64, ok []bool, fetched bool, window time.Duration) {
	var sum float64
	var n int
	for i := range v {
		if ok[i] {
			sum += v[i]
			n++
		}
	}
	mean := 1.0
	if n > 0 {
		mean = sum / float64(n)
	}
	for i := range v {
		if !ok[i] {
			v[i] = mean
		}
	}
	switch {
	case !fetched || n == len(v):
	case n == 0:
		fmt.Fprintf(os.Stderr, "warning: the %s forecast does not cover the %s window; perfect sky assumed\n",
			what, window.Round(time.Minute))
	default:
		fmt.Fprintf(os.Stderr, "warning: no %s forecast for %s of the %s window; those hours get the rest's mean (%.0f%%)\n",
			what, time.Duration(len(v)-n)*time.Minute, window.Round(time.Minute), 100*mean)
	}
}

// ramp returns 1 when v is at or on the good side of good, floor at or beyond
// bad, and interpolates linearly between. good and bad may be in either order.
func ramp(v, good, bad, floor float64) float64 {
	f := max(0, min(1, (v-good)/(bad-good)))

	return 1 - f*(1-floor)
}

// ExtinctionAt returns the extinction for time t: from that hour's aerosol
// optical depth when known, typicalAOD when only the elevation is known,
// and fallback otherwise (or when -extinction was given, as elevation is NaN then).
func (s *Sky) ExtinctionAt(t time.Time, fallback float64) float64 {
	if math.IsNaN(s.Elevation) {
		return fallback
	}
	aod, ok := s.AOD[t.Truncate(time.Hour).Unix()]
	if !ok {
		aod = atmos.TypicalAOD
	}

	return atmos.ExtinctionCoeff(s.Elevation, aod)
}
