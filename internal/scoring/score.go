// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package scoring evaluates targets minute by minute over the observing window.
package scoring

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

const deg = math.Pi / 180

type Result struct { // betteralign:ignore (embedded target first, per embeddedstructfieldcheck)
	catalog.Target

	MaxAt   time.Time
	Alt     []float64 // per grid minute; only from Score with perMinute, for plan.Make
	Weight  []float64 // per grid minute, 0 when not observable; as Alt
	Foto    float64   // fraction of time observable
	Score   float64   // foto weighted by clouds, moonlight and framing
	Frame   float64   // framing factor (1 without framing)
	SkyMag  float64   // mean sky brightness at the target while observable, mag/arcsec²
	MeanAlt float64
	MaxAlt  float64
}

// Score scores every target within the size limits and returns the
// observable ones, best first. perMinute keeps each result's per-minute
// Alt and Weight, which plan.Make needs.
func Score(cfg *config.Config, s *Sky, targets []catalog.Target, perMinute bool) []Result {
	var results []Result
	// Per-minute scratch reused across targets: most are never observable,
	// and only perMinute needs a Result's own copy (and the altitudes at all).
	weight := make([]float64, len(s.Grid))
	var alt []float64
	if perMinute {
		alt = make([]float64, len(s.Grid))
	}
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
		if r, ok := scoreTarget(cfg, s, tg, alt, weight); ok {
			if perMinute {
				r.Alt, r.Weight = slices.Clone(alt), slices.Clone(weight)
			}
			results = append(results, r)
		}
	}

	// Weighted score first, mean altitude breaks ties.
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(b.MeanAlt, a.MeanAlt))
	})

	return results
}

// scoreTarget fills weight (len(s.Grid), overwritten) with the target's
// per-minute weight, and alt with its altitude when alt is not nil.
func scoreTarget(cfg *config.Config, s *Sky, tg catalog.Target, alt, weight []float64) (Result, bool) {
	var ra, dec float64
	var hz astro.Horizontal
	if tg.Track == nil {
		// Catalogs are J2000; the hour angle comes from sidereal time of date.
		ra, dec = astro.Precess(tg.RADeg, tg.DecDeg, s.Grid[len(s.Grid)/2])
		hz = astro.NewHorizontal(cfg.Lat, dec)
	}

	// Fraction of sky glow (moonlight, light pollution) that counts against
	// this target; also scales its Moon separation limit.
	k := skyK(tg, cfg.Filter, cfg.FilterK)

	// The limits compare as sines, so the Asin runs only for the minutes
	// that pass them (and for alt): the bulk of the minutes do not.
	sinMin, sinMax := math.Sin(cfg.AltMin*deg), math.Sin(cfg.AltMax*deg)
	r := Result{Target: tg, Frame: 1}
	clear(weight)
	var good int
	var altSum, weighted, skySum float64
	maxSin, maxAt := -2.0, 0
	for i := range s.Grid {
		if tg.Track != nil {
			ra, dec = tg.Track[i][0], tg.Track[i][1]
			hz = astro.NewHorizontal(cfg.Lat, dec)
		}
		ha := s.LST[i] - ra
		sinAlt := hz.SinAlt(ha)
		if sinAlt > maxSin {
			maxSin, maxAt = sinAlt, i
		}
		if alt != nil {
			alt[i] = math.Asin(sinAlt) / deg
		}
		if sinAlt < sinMin || sinAlt > sinMax {
			continue
		}
		a := math.Asin(sinAlt) / deg
		// The azimuth is only needed against a horizon profile.
		if len(cfg.Horizon) > 0 && a < cfg.Horizon.At(hz.Az(ha)) {
			continue
		}
		rho := 180.0
		if s.MoonAlt[i] > 0 {
			if rho = astro.Separation(ra, dec, s.MoonPos[i][0], s.MoonPos[i][1]); rho < k*s.MoonSep {
				continue
			}
		}
		sb := atmos.SkyBrightness(s.ZenithNL, s.Ext[i], a, s.MoonAlt[i], rho, s.MoonPhase, s.MoonDist)
		// Sky-limited imaging: SNR in a fixed time goes as signal/sqrt(sky). Full
		// credit for a pristine dark sky; a filter cuts the sky the target sees to k.
		skyW := min(1, math.Sqrt(s.RefNL/(k*sb)))
		good++
		altSum += a
		skySum += atmos.MagFromNL(sb)
		weight[i] = s.Quality[i] * atmos.Extinction(a, s.Ext[i]) * skyW
		weighted += weight[i]
	}
	r.MaxAlt, r.MaxAt = math.Asin(maxSin)/deg, s.Grid[maxAt]
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
	switch strings.ToLower(tg.Type) { // LBN spells it "Open cluster"
	case "globular cluster", "open cluster":
		return 0.5 // high surface brightness
	}

	return 1
}
