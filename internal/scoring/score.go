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

const deg = math.Pi / 180

type Result struct { // betteralign:ignore (embedded target first, per embeddedstructfieldcheck)
	catalog.Target

	MaxAt   time.Time
	RunFrom time.Time // longest continuous observable stretch, [RunFrom, RunTo)
	RunTo   time.Time
	Alt     []float64     // per grid minute; only from Score with perMinute, for plan.Make
	Weight  []float64     // per grid minute, 0 when not observable; as Alt
	Runs    int           // continuous observable stretches
	Foto    float64       // fraction of time observable
	Score   float64       // foto weighted by clouds, moonlight and framing
	Frame   float64       // framing factor (1 without framing), divided among the mosaic's panels
	Mosaic  config.Mosaic // the grid Frame was scored on; zero without framing
	SkyMag  float64       // mean sky brightness at the target while observable, mag/arcsec²
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
	mosaics := cfg.Mosaics()
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
		if !cfg.Near(tg.Position()) || cfg.Skip[catalog.NameKey(tg.Name)] {
			continue
		}
		if r, ok := scoreTarget(cfg, s, tg, mosaics, alt, weight); ok {
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
func scoreTarget(cfg *config.Config, s *Sky, tg catalog.Target, mosaics []config.Mosaic, alt, weight []float64) (Result, bool) {
	var ra, dec float64
	if tg.Track == nil {
		// Catalogs are J2000; the hour angle comes from sidereal time of date.
		ra, dec = astro.Precess(tg.RADeg, tg.DecDeg, s.Grid[len(s.Grid)/2])
	}
	hz := astro.NewHorizontal(cfg.Lat, dec)
	// cos(LST - ra) by the difference formula: one Sincos per target instead
	// of a Cos per target-minute.
	sinRA, cosRA := math.Sincos(ra * deg)

	// Fraction of sky glow (moonlight, light pollution) that counts against
	// this target; also scales its Moon separation limit.
	k := skyK(tg, cfg.Filter, cfg.FilterK)
	// The Moon separation is the Acos of a dot product of unit vectors, and a
	// minute closer than the limit (at most 100°, where the cosine still
	// falls) is rejected on the cosine alone. On a moonlit night with low
	// altitude limits Separation's trigonometry was 40% of scoring.
	unit := astro.Unit(ra, dec) // a comet's is redone per minute
	cosMoonSep := math.Cos(k * s.MoonSep * deg)

	// The limits compare as sines, so the Asin runs only for the minutes
	// that pass them (and for alt): the bulk of the minutes do not.
	sinMin, sinMax := math.Sin(cfg.AltMin*deg), math.Sin(cfg.AltMax*deg)
	r := Result{Target: tg, Frame: 1}
	// The object's own surface brightness, in the sky's units; 0 when unknown,
	// which scores it as sky-limited, as every target was before SB existed.
	var objNL float64
	if sb, _ := tg.SurfaceBrightness(); sb > 0 {
		objNL = atmos.NanoLamberts(sb)
	}
	objRef := objNL + s.RefNL // the same object under a pristine sky
	clear(weight)
	var good int
	var altSum, weighted, skySum float64
	maxSin, maxAt := -2.0, 0
	// Continuous observable stretches: the current one starts at runStart and
	// the longest so far is [best, best+bestLen).
	runStart, prev, best, bestLen := 0, -2, 0, 0
	for i := range s.Grid {
		if tg.Track != nil {
			ra, dec = tg.Track[i][0], tg.Track[i][1]
			hz = hz.WithDec(dec)
		}
		ha := s.LST[i] - ra
		var sinAlt float64
		if tg.Track != nil {
			sinAlt = hz.SinAlt(ha)
		} else {
			sinAlt = hz.SinAltCos(s.CosLST[i]*cosRA + s.SinLST[i]*sinRA)
		}
		if sinAlt > maxSin {
			maxSin, maxAt = sinAlt, i
		}
		var a float64
		if alt != nil {
			a = math.Asin(sinAlt) / deg
			alt[i] = a
		}
		if sinAlt < sinMin || sinAlt > sinMax {
			continue
		}
		if alt == nil {
			a = math.Asin(sinAlt) / deg
		}
		// The azimuth is only needed against a horizon profile.
		if len(cfg.Horizon) > 0 && a < cfg.Horizon.At(hz.Az(ha)) {
			continue
		}
		rho := 180.0
		if s.MoonAlt[i] > 0 {
			if tg.Track != nil {
				unit = astro.Unit(ra, dec)
			}
			m := &s.MoonUnit[i]
			c := max(-1, min(1, unit[0]*m[0]+unit[1]*m[1]+unit[2]*m[2]))
			if c > cosMoonSep {
				continue
			}
			rho = math.Acos(c) / deg
		}
		sb := atmos.SkyBrightnessLit(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i])
		// SNR in a fixed time goes as signal/sqrt(signal+sky), relative to the
		// same object under a pristine dark sky: an object much brighter than
		// the sky loses nothing, a faint or unknown one (objNL = 0) is
		// sky-limited, sqrt(RefNL/(k·sky)). A filter cuts the sky the target
		// sees to k; the emission-line signal that passes it is unchanged.
		skyW := min(1, math.Sqrt(objRef/(objNL+k*sb)))
		if i != prev+1 {
			runStart = i
			r.Runs++
		}
		prev = i
		if n := i - runStart + 1; n > bestLen {
			best, bestLen = runStart, n
		}
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
	// The grid is minute-aligned from Start, so the run ends a minute after
	// its last minute, or at End for the last grid minute (which End may cut
	// short, so -min-run judges the clock span, not the minute count).
	r.RunFrom, r.RunTo = s.Grid[best], s.End
	if best+bestLen < len(s.Grid) {
		r.RunTo = s.Grid[best+bestLen]
	}
	if r.RunTo.Sub(r.RunFrom) < cfg.MinRun {
		return r, false
	}
	r.Foto = float64(good) / float64(len(s.Grid))
	r.MeanAlt = altSum / float64(good)
	r.SkyMag = skySum / float64(good)
	// Unknown sizes get no frame penalty.
	if cfg.Framing && tg.HasSize() {
		// An object no mosaic holds cannot be framed whatever -size-max says;
		// the frame score only orders those that fit.
		if r.Frame, r.Mosaic = frame(tg, mosaics, cfg.Rotate); r.Frame == 0 {
			return r, false
		}
	}
	r.Score = weighted / float64(len(s.Grid)) * r.Frame

	return r, true
}

// frame returns the best frame score over the mosaics, each one's frameFill
// divided by its panels (the night's time is shared among them), and the
// mosaic that gave it; 0 when none holds the object. The fewest panels win a
// tie.
func frame(tg catalog.Target, mosaics []config.Mosaic, rotate bool) (float64, config.Mosaic) {
	major, minor := tg.Axes()
	best, at := 0.0, config.Mosaic{}
	for _, m := range mosaics {
		// Without -rotate the position angle in the frame is unknown, so the
		// major axis must fit the short side; with it the major axis lies
		// along whichever side gives the better fit and the minor axis
		// across, which only differs from that when the minor axis is known.
		fill := major / min(m.W, m.H)
		if rotate {
			fill = min(max(major/m.W, minor/m.H), max(major/m.H, minor/m.W))
		}
		if f := frameFill(fill) / float64(m.Panels()); f > best {
			best, at = f, m
		}
	}

	return best, at
}

// frameFill scores how well an object filling fill (0..1) of the frame
// frames: 1 between 25% and 80%, falling off linearly outside; 0 from 1 up.
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
// counts against a target: 1 for faint broadband targets, 0.5 for clusters
// (point sources; a cluster with nebulosity is imaged for the glow and is not
// a catalog.Target.Cluster), and filterK for emission-line targets shot through a narrowband filter.
func skyK(tg catalog.Target, filter bool, filterK float64) float64 {
	if filter && catalog.EmissionLine(tg) {
		return filterK
	}
	if tg.Cluster() {
		return 0.5
	}

	return 1
}
