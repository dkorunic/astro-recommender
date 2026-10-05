// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package scoring evaluates targets minute by minute over the observing window.
package scoring

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

type Result struct { // betteralign:ignore (embedded target first, per embeddedstructfieldcheck)
	catalog.Target

	MaxAt   time.Time
	Alt     []float64 // per grid minute
	Weight  []float64 // per grid minute, 0 when not observable
	Foto    float64   // fraction of time observable
	Score   float64   // foto weighted by clouds, moonlight and framing
	Frame   float64   // framing factor (1 without framing)
	SkyMag  float64   // mean sky brightness at the target while observable, mag/arcsec²
	MeanAlt float64
	MaxAlt  float64
}

// Score scores every target within the size limits and returns the
// observable ones, best first.
func Score(cfg *config.Config, s *Sky, targets []catalog.Target) []Result {
	var results []Result
	for _, tg := range targets {
		// Comet comae have no catalog size, so size limits do not apply.
		// An unknown size counts as 0, so -size-min 0 keeps it.
		size := 0.0
		if tg.HasSize() {
			size = tg.Size
		}
		if tg.Track == nil && (size < cfg.SizeMin || size > cfg.SizeMax) {
			continue
		}
		if r, ok := scoreTarget(cfg, s, tg); ok {
			results = append(results, r)
		}
	}

	// Weighted score first, mean altitude breaks ties.
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(b.MeanAlt, a.MeanAlt))
	})

	return results
}

func scoreTarget(cfg *config.Config, s *Sky, tg catalog.Target) (Result, bool) {
	var fixedRA, fixedDec float64
	if tg.Track == nil {
		// Coordinates were validated by loadTargets.
		ra, _ := astro.Sexagesimal(tg.RA)
		dec, _ := astro.Sexagesimal(tg.Dec)
		// Catalogs are J2000; the hour angle comes from sidereal time of date.
		fixedRA, fixedDec = astro.Precess(ra*15, dec, s.Grid[len(s.Grid)/2]) // hours -> degrees
	}
	pos := func(i int) (float64, float64) {
		if tg.Track != nil {
			return tg.Track[i][0], tg.Track[i][1]
		}

		return fixedRA, fixedDec
	}

	// Fraction of sky glow (moonlight, light pollution) that counts against
	// this target; also scales its Moon separation limit.
	k := skyK(tg, cfg.Filter, cfg.FilterK)

	r := Result{Target: tg, MaxAlt: -90, Frame: 1, Alt: make([]float64, len(s.Grid)), Weight: make([]float64, len(s.Grid))}
	var good int
	var altSum, weighted, skySum float64
	for i, t := range s.Grid {
		ra, dec := pos(i)
		alt, az := astro.AltAz(ra, dec, t, cfg.Lat, cfg.Lon)
		r.Alt[i] = alt
		if alt > r.MaxAlt {
			r.MaxAlt, r.MaxAt = alt, t
		}
		if alt < cfg.AltMin || alt > cfg.AltMax || alt < cfg.Horizon.At(az) {
			continue
		}
		rho := 180.0
		if s.MoonAlt[i] > 0 {
			if rho = astro.Separation(ra, dec, s.MoonPos[i][0], s.MoonPos[i][1]); rho < k*s.MoonSep {
				continue
			}
		}
		sb := atmos.SkyBrightness(s.ZenithNL, s.Ext[i], alt, s.MoonAlt[i], rho, s.MoonPhase, s.MoonDist)
		// Sky-limited imaging: SNR in a fixed time goes as signal/sqrt(sky). Full
		// credit for a pristine dark sky; a filter cuts the sky the target sees to k.
		skyW := min(1, math.Sqrt(s.RefNL/(k*sb)))
		good++
		altSum += alt
		skySum += atmos.MagFromNL(sb)
		r.Weight[i] = s.Quality[i] * atmos.Extinction(alt, s.Ext[i]) * skyW
		weighted += r.Weight[i]
	}
	if good == 0 {
		return r, false
	}
	r.Foto = float64(good) / float64(len(s.Grid))
	r.MeanAlt = altSum / float64(good)
	r.SkyMag = skySum / float64(good)
	// The size is the major axis and its position angle is unknown, so it
	// must fit the short side. Unknown sizes get no frame penalty.
	if cfg.Framing && tg.HasSize() {
		// An object as large as the short side cannot be framed whatever
		// -size-max says; the frame score only orders those that fit.
		if tg.Size >= cfg.FOVShort {
			return r, false
		}
		r.Frame = frameFill(tg.Size / cfg.FOVShort)
	}
	r.Score = weighted / float64(len(s.Grid)) * r.Frame

	return r, true
}

// frameFill scores how well an object filling fill (0..1) of the short FOV side
// frames: 1 between 25% and 80%, falling off linearly outside.
func frameFill(fill float64) float64 {
	switch {
	case fill < 0.25:
		return fill / 0.25
	case fill > 0.8:
		return max(0, (1-fill)/0.2)
	}

	return 1
}

// skyK returns the fraction of sky glow (moonlight, light pollution) that
// counts against a target: 1 for faint broadband targets, less for bright
// clusters, and filterK for emission-line targets shot through a narrowband filter.
func skyK(tg catalog.Target, filter bool, filterK float64) float64 {
	if filter && catalog.EmissionLine(tg) {
		return filterK
	}
	switch tg.Type {
	case "Globular Cluster", "Open Cluster":
		return 0.5 // high surface brightness
	}

	return 1
}
