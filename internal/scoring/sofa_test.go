// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/hebl/gofa"
)

// TestScoreSOFA checks the per-minute altitudes as scoring computes them
// (BuildSky's sidereal time sines, Score's difference formula for cos(hour
// angle), precession at mid-window) against SOFA's apparent altitude, over
// random sites, nights in 2000-2050 and targets. astro's TestSOFA covers
// astro.AltAz, which scoring does not use; the limit is the same README
// Accuracy row (target altitude, 0.3').
func TestScoreSOFA(t *testing.T) {
	const mjdUnix = 40587 // MJD of the Unix epoch
	rng := rand.New(rand.NewPCG(2026, 10))
	var worst float64
	for range 300 {
		lat, lon := rng.Float64()*160-80, rng.Float64()*360-180
		start := time.Unix(rng.Int64N(50*365*86400)+946684800, 0).UTC().Truncate(time.Minute)
		end := start.Add(time.Duration(60+rng.IntN(720)) * time.Minute)
		// Every altitude passes, so a result comes back with its per-minute Alt.
		cfg := &config.Config{Lat: lat, Lon: lon, AltMin: -90, AltMax: 90, SizeMax: math.Inf(1), NoWeather: true, ExtinctionSet: true, Extinction: 0.2}
		s := BuildSky(cfg, NoForecast(), start, end)
		ra, dec := rng.Float64()*360, rng.Float64()*178-89
		res := Score(cfg, &s, []catalog.Target{{Name: "x", RADeg: ra, DecDeg: dec}}, true)
		if len(res) == 0 { // the Moon blocked every minute
			continue
		}
		var p [3]float64
		gofa.S2c(ra*deg, dec*deg, &p)
		for _, i := range []int{0, len(s.Grid) / 2, len(s.Grid) - 1} {
			ut := s.Grid[i]
			mjd := float64(ut.Unix())/86400 + mjdUnix
			var dat float64
			gofa.Dat(ut.Year(), int(ut.Month()), ut.Day(), 0, &dat)
			tt := mjd + (dat+32.184)/86400
			// J2000 -> true equator and equinox of date, against apparent sidereal time.
			var rnpb [3][3]float64
			var app [3]float64
			gofa.Pnm06a(2400000.5, tt, &rnpb)
			gofa.Rxp(rnpb, p, &app)
			var ara, adec float64
			gofa.C2s(app, &ara, &adec)
			gast := gofa.Gst06a(2400000.5, mjd, 2400000.5, tt) + lon*deg
			sinAlt := math.Sin(lat*deg)*math.Sin(adec) + math.Cos(lat*deg)*math.Cos(adec)*math.Cos(gast-ara)
			worst = max(worst, math.Abs(res[0].Alt[i]-math.Asin(sinAlt)/deg))
		}
	}
	if limit := 0.3 / 60; worst > limit {
		t.Errorf("scoring altitude: worst difference from SOFA %g° exceeds %g°", worst, limit)
	} else {
		t.Logf("scoring altitude (degrees): worst %.3g (limit %g)", worst, limit)
	}
}
