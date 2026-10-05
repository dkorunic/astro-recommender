// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// Sky holds everything about the night that does not depend on the target,
// sampled on a 1-minute grid (as in uptonight).
type Sky struct {
	Night      [2]time.Time // full astronomical night; start/end may be clipped inside it
	Start, End time.Time
	Weather    map[int64]weather.HourWeather
	Astro      map[int64]weather.AstroBlock
	AOD        map[int64]float64 // aerosol optical depth at 550 nm per unix hour (CAMS)
	Grid       []time.Time
	MoonPos    [][2]float64 // topocentric RA/Dec of date, degrees
	MoonAlt    []float64
	Ext        []float64 // extinction per grid minute, mag per airmass
	Quality    []float64 // clear-sky fraction x transparency x dew x gust, 1 = perfect
	Elevation  float64   // site elevation in m, from Open-Meteo; NaN if unknown
	Illum      float64
	MoonPhase  float64 // phase angle in degrees, 0 = full
	MoonDist   float64 // geocentric distance in Earth radii
	MoonSep    float64 // uptonight: min separation in degrees = illumination %
	ZenithNL   float64 // site's moonless zenith sky brightness, nanoLamberts
	RefNL      float64 // refZenithMag in nanoLamberts
	Comets     int     // comets added as targets
}

func BuildSky(ctx context.Context, cfg *config.Config, start, end time.Time) Sky {
	s := Sky{Start: start, End: end}
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

	s.MoonPos = make([][2]float64, len(s.Grid))
	s.MoonAlt = make([]float64, len(s.Grid))
	for i, t := range s.Grid {
		s.MoonPos[i][0], s.MoonPos[i][1] = astro.MoonTopo(t, cfg.Lat, cfg.Lon)
		s.MoonAlt[i] = astro.Altitude(s.MoonPos[i][0], s.MoonPos[i][1], t, cfg.Lat, cfg.Lon)
	}

	if !cfg.NoWeather {
		var err error
		if s.Weather, err = weather.Forecast(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no weather forecast:", err)
		}
		if s.Astro, err = weather.AstroForecast(ctx, cfg.Lat, cfg.Lon); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no transparency forecast:", err)
		}
	}
	s.Elevation = math.NaN()
	if !cfg.NoWeather && !cfg.ExtinctionSet {
		var err error
		if s.AOD, s.Elevation, err = weather.AerosolForecast(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no aerosol forecast, fixed extinction:", err)
		}
	}
	s.Quality = make([]float64, len(s.Grid))
	s.Ext = make([]float64, len(s.Grid))
	for i, t := range s.Grid {
		s.Ext[i] = s.ExtinctionAt(t, cfg.Extinction)
		s.Quality[i] = 1
		if h, ok := s.Weather[t.Truncate(time.Hour).Unix()]; ok {
			// ponytail: guessed ramps; dew (spread 4 -> 1 °C) and gusts (20 -> 40 km/h)
			// cost up to 30% and 50% of usable frames.
			s.Quality[i] = (1 - h.Cloud/100) * ramp(h.Temp-h.DewPoint, 4, 1, 0.7) * ramp(h.Gust, 20, 40, 0.5)
		}
		if a, ok := s.Astro[weather.AstroKey(t)]; ok {
			s.Quality[i] *= ramp(float64(a.Transparency), 1, 8, 0.5)
		}
	}

	return s
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
