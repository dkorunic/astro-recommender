// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"cmp"
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
	Night        [2]time.Time // full night at -twilight's Sun limit; start/end may be clipped inside it
	Start, End   time.Time
	Weather      map[int64]weather.HourWeather
	Astro        map[int64]weather.AstroBlock
	AOD          map[int64]float64 // aerosol optical depth at 550 nm per unix hour (CAMS)
	Grid         []time.Time
	LST          []float64 // local sidereal time per grid minute, degrees
	SinLST       []float64 // sin and cos of LST, so a fixed target's cos(hour angle) needs no Cos
	CosLST       []float64
	MoonPos      [][2]float64 // topocentric RA/Dec of date, degrees
	MoonUnit     [][3]float64 // astro.Unit of MoonPos, for the per-minute Moon separations
	MoonAlt      []float64
	Ext          []float64       // extinction per grid minute, mag per airmass, at 550 nm: ExtRayleigh + atmos.OzoneExtinction + ExtAerosol
	ExtRayleigh  []float64       // its Rayleigh part, which a filter band scales as λ⁻⁴
	ExtAerosol   []float64       // its aerosol part, scaled as λ^-1.3
	MoonLight    []atmos.Moon    // atmos.MoonLight per grid minute, dimmed by Eclipse
	Eclipse      []astro.Eclipse // astro.LunarEclipse per grid minute; nil when the Moon misses the Earth's shadow all window
	Quality      []float64       // clear-sky fraction x rain x dew x gust x transparency, 1 = perfect
	Covered      []bool          // per grid minute, the weather forecast's own hour (else fillGaps' mean); nil in a hand-built Sky = all
	Temp         []float64       // air temperature per grid minute, °C: the forecast's, its mean where it has none, nightTemp without one
	Seeing       []float64       // zenith seeing per grid minute, FWHM arcsec at 500 nm: the upper-air estimate, else 7Timer's, else typicalSeeing
	Elevation    float64         // site elevation in m, from Open-Meteo; NaN if unknown
	AODElevation float64         // the ground AOD's column stands on, m (Forecast.AODElevation); NaN if unknown
	SunLon       float64         // the Sun's ecliptic longitude at mid-window, degrees, for the zodiacal light
	Illum        float64
	MoonPhase    float64 // phase angle in degrees, 0 = full
	MoonDist     float64 // geocentric distance in Earth radii
	MoonSep      float64 // uptonight: min separation in degrees = illumination %
	ZenithNL     float64 // site's moonless zenith sky brightness, nanoLamberts, Airglow included
	Airglow      float64 // nanoLamberts the solar cycle adds to the configured zenith (atmos.AirglowExcess); 0 when the flux is unknown
	RefNL        float64 // refZenithMag in nanoLamberts
	Comets       int     // comets added as targets
	CometsLost   bool    // the comet elements could not be fetched
}

// Forecast is the network input of BuildSky; the zero value (with NaN
// Elevation, see NoForecast) means none.
type Forecast struct {
	Weather   map[int64]weather.HourWeather
	Astro     map[int64]weather.AstroBlock
	AOD       map[int64]float64
	Elevation float64 // NaN if unknown
	// AODElevation is the ground AOD's column stands on (CAMS's cell, by
	// the IFS grid's orography), NaN if unknown: the site's own then.
	AODElevation float64
}

// NoForecast is a Forecast with nothing fetched: perfect sky, fixed extinction.
func NoForecast() Forecast { return Forecast{Elevation: math.NaN(), AODElevation: math.NaN()} }

// FetchForecast downloads the weather, upper-air profile, transparency and
// aerosol forecasts for [start, end) concurrently, as -no-weather and
// -extinction allow. A failed source is a stderr warning and stays empty.
func FetchForecast(ctx context.Context, cfg *config.Config, start, end time.Time) Forecast {
	f := NoForecast()
	if cfg.NoWeather {
		return f
	}
	var wg sync.WaitGroup
	weatherElev, aodElev := math.NaN(), math.NaN()
	var profile map[int64][]weather.Level
	var profileErr error
	wg.Go(func() {
		var err error
		if f.Weather, weatherElev, err = weather.Forecast(ctx, cfg.Lat, cfg.Lon, start, end); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no weather forecast:", err)
		}
	})
	wg.Go(func() {
		profile, profileErr = weather.Profile(ctx, cfg.Lat, cfg.Lon, start, end)
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
		wg.Go(func() {
			var err error
			if f.AODElevation, err = weather.GridElevation(ctx, cfg.Lat, cfg.Lon); err != nil {
				fmt.Fprintln(os.Stderr, "warning: aerosols not moved to the site's elevation:", err)
			}
		})
	}
	wg.Wait()
	mergeProfile(f.Weather, profile)
	if w := seeingWarning(f, profileErr, start, end); w != "" {
		fmt.Fprintln(os.Stderr, "warning:", w)
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

// mergeProfile computes the seeing estimate of each hour the surface
// forecast has from the upper-air levels, once: the table and JSON read it
// through Weather. Without a surface forecast there is no table to show it
// in, so a profile then goes unused; the weather warning already says so.
func mergeProfile(w map[int64]weather.HourWeather, profile map[int64][]weather.Level) {
	for t, lv := range profile {
		if h, ok := w[t]; ok {
			h.Seeing = weather.Seeing(lv)
			w[t] = h
		}
	}
}

// seeingWarning is the stderr warning about the upper-air estimate, "" for
// none: 7Timer's class stands in for an hour without an estimate, so where
// it covers every hour a failed or thin profile says nothing; else the fetch
// error (profileErr) when the profile failed, else the hours without an
// estimate when there are any.
func seeingWarning(f Forecast, profileErr error, start, end time.Time) string {
	missing, needed := missingSeeing(f, start, end)
	switch {
	case needed == 0:
		return ""
	case profileErr != nil:
		return "no upper-air profile, no seeing estimate: " + profileErr.Error()
	case missing > 0:
		return fmt.Sprintf("no usable upper-air seeing estimate for %d of the %d hours without a 7Timer point", missing, needed)
	}

	return ""
}

// missingSeeing counts the hours of [start, end) whose SEEING cell falls
// back to the upper-air estimate (weather but no 7Timer point), and how many
// of them get none: the profile's only reader, so its gaps would otherwise
// show only as dashes (seeingWarning). A fetched profile may miss hours
// (the ECMWF range ends a day before best_match's, and a narrowed getRange
// covers one day), have too few levels, or give estimates Seeing rejects.
func missingSeeing(f Forecast, start, end time.Time) (int, int) {
	missing, needed := 0, 0
	for t := start.Truncate(time.Hour); t.Before(end); t = t.Add(time.Hour) {
		h, ok := f.Weather[t.Unix()]
		if _, astro := f.Astro[weather.AstroKey(t)]; !ok || astro {
			continue
		}
		needed++
		if h.Seeing == 0 {
			missing++
		}
	}

	return missing, needed
}

// eclipses returns astro.LunarEclipse per minute of grid, nil when the Moon
// misses the Earth's shadow throughout. The shadow reaches 1.59° from the
// antisolar point at most (the penumbra and the Moon's radius at perigee),
// and the Moon moves against it under 0.6°/h, so a night whose Moon at
// mid-window, illum lit (geocentric: (1 + cos d)/2 at d from the antisolar
// point), is farther than that and half the window's motion needs no look.
func eclipses(grid []time.Time, illum float64) []astro.Eclipse {
	if len(grid) == 0 {
		return nil
	}
	half := grid[len(grid)-1].Sub(grid[0]).Hours() / 2
	if math.Acos(max(-1, min(1, 2*illum-1)))/deg > 1.7+0.6*half {
		return nil
	}
	var out []astro.Eclipse
	for i, t := range grid {
		e := astro.LunarEclipse(t)
		if e.Phase == astro.NoEclipse {
			continue
		}
		if out == nil {
			out = make([]astro.Eclipse, len(grid))
			for j := range out {
				out[j].Light = 1
			}
		}
		out[i] = e
	}

	return out
}

// BuildSky samples the night [start, end) on a 1-minute grid with forecast
// f; it does no I/O.
func BuildSky(cfg *config.Config, f Forecast, start, end time.Time) Sky {
	s := Sky{
		Start: start, End: end, Weather: f.Weather, Astro: f.Astro, AOD: f.AOD, Elevation: f.Elevation, AODElevation: f.AODElevation,
		Grid: make([]time.Time, 0, end.Sub(start)/time.Minute+1),
	}
	for t := start; t.Before(end); t = t.Add(time.Minute) {
		s.Grid = append(s.Grid, t)
	}
	mid := s.Grid[len(s.Grid)/2]
	s.Illum = astro.MoonIllumination(mid)
	s.MoonSep = s.Illum * 100
	s.MoonPhase = astro.MoonPhaseAngle(mid)
	s.MoonDist = astro.MoonDistance(mid)
	s.SunLon = astro.SunLongitude(mid)
	s.Airglow = atmos.AirglowExcess(cfg.SolarFlux)
	s.ZenithNL = atmos.NanoLamberts(cfg.ZenithMag()) + s.Airglow
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
	s.Eclipse = eclipses(s.Grid, s.Illum)

	s.Quality = make([]float64, len(s.Grid))
	s.Ext = make([]float64, len(s.Grid))
	s.ExtRayleigh, s.ExtAerosol = make([]float64, len(s.Grid)), make([]float64, len(s.Grid))
	s.MoonLight = make([]atmos.Moon, len(s.Grid))
	// Weather and transparency per minute; ok marks the minutes each forecast covers.
	sky, skyOK := make([]float64, len(s.Grid)), make([]bool, len(s.Grid))
	s.Temp = make([]float64, len(s.Grid))
	s.Seeing = make([]float64, len(s.Grid))
	transp, transpOK := make([]float64, len(s.Grid)), make([]bool, len(s.Grid))
	for i, t := range s.Grid {
		s.Ext[i] = s.ExtinctionAt(t, cfg.Extinction)
		s.ExtRayleigh[i], s.ExtAerosol[i] = s.extinctionParts(t, cfg.Extinction)
		s.MoonLight[i] = atmos.MoonLight(s.Ext[i], s.ExtRayleigh[i], s.ExtAerosol[i], s.MoonAlt[i], s.MoonPhase, s.MoonDist)
		if s.Eclipse != nil {
			s.MoonLight[i].NL *= s.Eclipse[i].Light
		}
		// Cloud, temperature, dew point, wind and the upper-air seeing are
		// Open-Meteo's values at the hour, so a minute takes the nearest
		// hour's (a cloud bank arrives half an hour sooner than read from the
		// hour before); gusts and rain are over the hour that starts at the
		// key (hourly.weather shifts them), so the minute's own hour, its
		// gust capped against that hour's wind as the forecast table shows it.
		hour := t.Truncate(time.Hour).Unix()
		if h, ok := s.Weather[hour]; ok {
			at := h
			if n, ok := s.Weather[t.Round(time.Hour).Unix()]; ok {
				at = n
			}
			// ponytail: guessed ramps; dew (spread 4 -> 1 °C) and gusts (20 -> 40 km/h)
			// cost up to 30% and 50% of usable frames; rain (0 -> RainGate mm
			// in the hour) all of them, whatever the cloud layers say. Like cloud it
			// weighs the score only: FOTO and the observable runs are geometric.
			sky[i] = (1 - at.Cloud/100) * ramp(at.Temp-at.DewPoint, 4, 1, 0.7) * ramp(Gust(h.Gust, h.Wind), gustFree, 40, 0.5)
			s.Temp[i] = at.Temp
			if h.Precip > 0 { // NaN (unknown) does not gate
				sky[i] *= ramp(h.Precip, 0, weather.RainGate, 0)
			}
			skyOK[i] = true
		}
		// Transparency: CAMS's aerosols are already in Ext, which dims the
		// target by the airmass; 7Timer's class, its haze too, would count
		// them twice, so it weighs only the hours without CAMS (-extinction,
		// a failed or out-of-range fetch).
		a, astroOK := s.Astro[weather.AstroKey(t)]
		switch _, cams := s.AOD[t.Truncate(time.Hour).Unix()]; {
		case cams:
			transp[i], transpOK[i] = 1, true
		case astroOK:
			transp[i], transpOK[i] = ramp(float64(a.Transparency), 1, 8, 0.5), true
		}
		// Seeing: the upper-air estimate, calibrated against DIMMs, else
		// 7Timer's class, else typicalSeeing.
		s.Seeing[i] = s.Weather[hour].Seeing
		if n, ok := s.Weather[t.Round(time.Hour).Unix()]; ok && n.Seeing > 0 {
			s.Seeing[i] = n.Seeing
		}
		if astroOK && s.Seeing[i] == 0 {
			s.Seeing[i] = weather.SeeingArcsec(a.Seeing)
		}
		s.Seeing[i] = cmp.Or(s.Seeing[i], typicalSeeing)
	}
	fillGaps("weather", sky, skyOK, len(s.Weather) > 0, end.Sub(start))
	s.Covered = skyOK
	fillTemp(s.Temp, skyOK)
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
			what, hm(window))
	default:
		fmt.Fprintf(os.Stderr, "warning: no %s forecast for %s of the %s window; those hours get the rest's mean (%.0f%%)\n",
			what, hm(time.Duration(len(v)-n)*time.Minute), hm(window), 100*mean)
	}
}

// maxGustFactor caps a forecast gust at this many times the mean wind:
// Open-Meteo's gust over the hour runs a median 2.5x the 10 m wind at
// lowland and Paranal points, but 4.8x at La Palma's and 10.6x at Mauna
// Kea's, where the grid's gust parameterisation, not the summit's air,
// makes 30-50 km/h gusts in a 5-10 km/h breeze.
// ponytail: one factor for every site; measured gusts would calibrate it.
const maxGustFactor = 3

// gustFree is the gust below which the sky factor loses nothing.
const gustFree = 20

// Gust is the gust the sky factor counts: the forecast's, at most
// maxGustFactor times the mean wind, but never capped below gustFree, where
// the cap would change no score and only misreport a calm hour's gusts.
// The forecast table and JSON show it.
func Gust(g, wind float64) float64 { return min(g, max(maxGustFactor*wind, gustFree)) }

// typicalSeeing is the zenith seeing (FWHM arcsec) assumed for the minutes
// neither 7Timer nor the upper-air estimate covers.
// ponytail: one value for every site; amateur sites see 2-3".
const typicalSeeing = 2.5

// nightTemp is the air temperature (°C) assumed without a forecast, for the
// dark current of an uncooled camera.
// ponytail: one value for every site and season; the forecast's replaces it.
const nightTemp = 10.0

// fillTemp gives the minutes the forecast does not cover (ok false) the
// covered minutes' mean temperature, or nightTemp without any; fillGaps has
// already warned about them.
func fillTemp(temp []float64, ok []bool) {
	var sum float64
	var n int
	for i, t := range temp {
		if ok[i] {
			sum += t
			n++
		}
	}
	mean := nightTemp
	if n > 0 {
		mean = sum / float64(n)
	}
	for i := range temp {
		if !ok[i] {
			temp[i] = mean
		}
	}
}

// ramp returns 1 when v is at or on the good side of good, floor at or beyond
// bad, and interpolates linearly between. good and bad may be in either order.
func ramp(v, good, bad, floor float64) float64 {
	f := max(0, min(1, (v-good)/(bad-good)))

	return 1 - f*(1-floor)
}

// MinQuality is the Quality below which a minute is no use for imaging:
// four fifths of the sky clouded, or rain, dew and gusts as bad.
const MinQuality = 0.2

// MaxSeeing is the zenith seeing, FWHM arcseconds (Sky.Seeing), from which
// a minute is no use for imaging either.
const MaxSeeing = 4.0

// MinUsable is the shortest unbroken stretch of usable minutes worth a
// session; a window shorter than it needs to be usable throughout.
const MinUsable = time.Hour

// Clouded reports that the forecast leaves no MinUsable stretch of the
// window at MinQuality or better, so no target is really observable
// whatever its geometry. Without a forecast Quality is 1 and it is false;
// minutes the forecast does not cover count as usable, since their Quality
// is only the covered minutes' mean.
func (s *Sky) Clouded() bool {
	return !s.usable(func(i int) bool { return s.unknown(i) || s.Quality[i] >= MinQuality })
}

// Washout reports that no MinUsable stretch of the window is usable: at
// MinQuality or better and with seeing under MaxSeeing (a Sky without
// Seeing judges the weather alone). Clouded says whether the weather alone
// does it.
func (s *Sky) Washout() bool {
	return !s.usable(func(i int) bool {
		return s.unknown(i) || s.Quality[i] >= MinQuality && (i >= len(s.Seeing) || s.Seeing[i] < MaxSeeing)
	})
}

// UsableNeed is the unbroken usable minutes Clouded and Washout ask for:
// MinUsable's, or every minute of a shorter window.
func (s *Sky) UsableNeed() int { return min(int(MinUsable/time.Minute), len(s.Quality)) }

// LastHour is the last forecast hour the window's minutes read: the hour
// the last minute rounds to when the forecast has it (BuildSky takes cloud
// from the nearest hour), which starts at or after End, else the last
// minute's own. Before Start's hour for an empty grid.
func (s *Sky) LastHour() time.Time {
	if len(s.Grid) == 0 {
		return s.Start.Truncate(time.Hour).Add(-time.Hour)
	}
	last := s.Grid[len(s.Grid)-1]
	if r := last.Round(time.Hour); r.After(last) {
		if _, ok := s.Weather[r.Unix()]; ok {
			return r
		}
	}

	return last.Truncate(time.Hour)
}

// ExtinctionAt returns the extinction for time t: from that hour's aerosol
// optical depth when known, typicalAOD when only the elevation is known,
// and fallback otherwise (or when -extinction was given, as elevation is NaN then).
func (s *Sky) ExtinctionAt(t time.Time, fallback float64) float64 {
	if math.IsNaN(s.Elevation) {
		return fallback
	}

	return atmos.ExtinctionCoeff(s.Elevation, s.aodAt(t))
}

// extinctionParts returns the Rayleigh and aerosol parts of ExtinctionAt,
// which a filter band scales by its wavelength; a fixed coefficient is
// split by atmos.SplitExtinction.
func (s *Sky) extinctionParts(t time.Time, fallback float64) (float64, float64) {
	if math.IsNaN(s.Elevation) {
		return atmos.SplitExtinction(fallback)
	}

	return atmos.RayleighExtinction(s.Elevation), atmos.AerosolExtinction(s.aodAt(t))
}

// ozone returns minute i's ozone part of Ext, which a filter band keeps
// whole: atmos.OzoneExtinction, or all of Ext when a fixed -extinction is
// below it (atmos.SplitExtinction then leaves no Rayleigh or aerosol part),
// so that no band is dimmed more than the unfiltered light.
func (s *Sky) ozone(i int) float64 {
	return min(atmos.OzoneExtinction, s.Ext[i])
}

// aodAt returns the hour's aerosol optical depth above the site: CAMS's
// over the ground of its cell (AODElevation; the site's when unknown, as
// it was before), else TypicalAOD over sea level, each thinned by
// atmos.AerosolAbove for the height the site stands above it.
func (s *Sky) aodAt(t time.Time) float64 {
	if aod, ok := s.AOD[t.Truncate(time.Hour).Unix()]; ok {
		if math.IsNaN(s.AODElevation) {
			return aod
		}

		return atmos.AerosolAbove(aod, s.Elevation-s.AODElevation)
	}

	return atmos.AerosolAbove(atmos.TypicalAOD, s.Elevation)
}

// hm formats a duration as the report's header does, "10h 05m".
func hm(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)

	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// usable reports whether ok holds for MinUsable of consecutive grid
// minutes, or for every minute of a shorter window; true for an empty
// Quality (a hand-built Sky), which judges nothing.
func (s *Sky) usable(ok func(i int) bool) bool {
	need := s.UsableNeed()
	run := 0
	for i := range s.Quality {
		if !ok(i) {
			run = 0

			continue
		}
		if run++; run >= need {
			return true
		}
	}

	return need == 0
}

// unknown reports that the weather forecast does not cover minute i.
func (s *Sky) unknown(i int) bool { return i < len(s.Covered) && !s.Covered[i] }
