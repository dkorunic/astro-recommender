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
	Filter  string        // the -filter choice that scored best, "" when none beat no filter
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
	choices := append(cfg.FilterNames(), "") // "" is no filter
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
		if !cfg.Near(tg.Position()) || catalog.HasKey(cfg.Skip, tg.Name) || !cfg.Wanted(tg) {
			continue
		}
		// Each -filter choice that passes some of the target's light is a
		// candidate, scored once (each minute already takes the better of it
		// and no filter); the best that helped wins, the first of equals. A
		// target no choice helps (a galaxy, which a broadband filter dims as
		// much as the sky) is scored without, so its weights are exactly the
		// unfiltered ones, not their sum of sky parts.
		var best Result
		found := false
		for _, name := range choices {
			fp := passes(cfg, tg, name)
			if (name != "" && fp.w == 0) || (name == "" && found) {
				continue
			}
			r, ok := scoreTarget(cfg, s, tg, fp, mosaics, alt, weight)
			if !ok || (name != "" && r.Filter == "") || (found && r.Score <= best.Score) {
				continue
			}
			if r.Filter != "" {
				r.Filter = name
			}
			if perMinute {
				r.Alt, r.Weight = slices.Clone(alt), slices.Clone(weight)
			}
			best, found = r, true
		}
		if found {
			results = append(results, best)
		}
	}

	// Weighted score first, mean altitude breaks ties.
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(b.MeanAlt, a.MeanAlt))
	})

	return results
}

// scoreTarget fills weight (len(s.Grid), overwritten) with the target's
// per-minute weight through fp, and alt with its altitude when alt is not
// nil. Its Result.Filter is non-empty (a placeholder Score names) when fp
// mattered: some minute's filtered weight beat the broadband one, or only
// fp's wider Moon limit let the minute in.
func scoreTarget(cfg *config.Config, s *Sky, tg catalog.Target, fp filterPass, mosaics []config.Mosaic, alt, weight []float64) (Result, bool) {
	var ra, dec float64
	mid := s.Grid[len(s.Grid)/2]
	if tg.Track == nil {
		// Catalogs are J2000; the hour angle comes from sidereal time of date.
		ra, dec = astro.Precess(tg.RADeg, tg.DecDeg, mid)
	}
	hz := astro.NewHorizontal(cfg.Lat, dec)
	// The zodiacal and galactic light where the target stands (its
	// mid-window position of date, a comet's too) against what the zenith
	// brightness includes: half a magnitude more on the ecliptic 90° from
	// the Sun, a little less at the ecliptic pole; up to 1.7 more in the
	// Milky Way, 0.3 less at the galactic poles; never below both minima.
	mra, mdec := ra, dec
	if tg.Track != nil {
		mra, mdec = tg.Track[len(s.Grid)/2][0], tg.Track[len(s.Grid)/2][1]
	}
	lon, lat := astro.Ecliptic(mra, mdec, mid)
	gl, gb := astro.Galactic(mra, mdec) // of date for J2000: 0.4°, against 10° cells
	zenithNL := max(s.ZenithNL+atmos.ZodiacalExcess(lon-s.SunLon, lat)+atmos.GalacticExcess(gl, gb), atmos.ZodiacalPoleNL+atmos.GalacticMinNL)
	// cos(LST - ra) by the difference formula: one Sincos per target instead
	// of a Cos per target-minute.
	sinRA, cosRA := math.Sincos(ra * deg)

	// Fraction of sky glow (moonlight, light pollution) that counts against
	// this target unfiltered; with what a -filter passes, it scales the Moon
	// separation limit.
	k := skyK(tg)
	// The Moon separation is the Acos of a dot product of unit vectors, and a
	// minute closer than the limit (at most 100°, where the cosine still
	// falls) is rejected on the cosine alone. On a moonlit night with low
	// altitude limits Separation's trigonometry was 40% of scoring.
	unit := astro.Unit(ra, dec) // a comet's is redone per minute
	cosMoonSep := math.Cos(min(k, fp.moonK()) * s.MoonSep * deg)
	cosMoonSepK := math.Cos(k * s.MoonSep * deg) // the limit without fp
	helped := false

	// The limits compare as sines, so the Asin runs only for the minutes
	// that pass them (and for alt): the bulk of the minutes do not.
	sinMin, sinMax := math.Sin(cfg.AltMin*deg), math.Sin(cfg.AltMax*deg)
	r := Result{Target: tg, Frame: 1}
	// Unknown sizes get no frame penalty. An object no mosaic holds cannot be
	// framed whatever -size-max says (with -rotate the limit is the long side,
	// which only a known minor axis lets an object use), so it skips the
	// per-minute loop; the frame score only orders those that fit.
	if cfg.Framing && tg.HasSize() {
		if r.Frame, r.Mosaic = frame(tg, mosaics, cfg.Rotate); r.Frame == 0 {
			return r, false
		}
	}
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
			helped = helped || c > cosMoonSepK
			rho = math.Acos(c) / deg
		}
		// SNR in a fixed time goes as signal/sqrt(signal+sky), relative to the
		// same object under a pristine dark sky: an object much brighter than
		// the sky loses nothing, a faint or unknown one (objNL = 0) is
		// sky-limited, sqrt(RefNL/(k·sky)). Through a filter, the better of
		// that and the filtered weight, each with its own extinction (the
		// bands' Rayleigh and aerosol parts scale with wavelength); the sky
		// is then the sum of its parts (SkyBrightnessLit's to rounding), not
		// computed twice.
		var sb, w float64
		air := atmos.Airmass(a)
		ext := atmos.Transmission(air, s.Ext[i])
		if fp.w > 0 {
			glow, rayleigh, mie := atmos.SkyParts(zenithNL, s.Ext[i], a, rho, s.MoonLight[i])
			sb = glow + rayleigh + mie
			broad := ext * min(1, math.Sqrt(objRef/(objNL+k*sb)))
			filtered := atmos.Transmission(air, s.ExtRayleigh[i]*fp.ray+atmos.OzoneExtinction+s.ExtAerosol[i]*fp.aer) *
				fp.weight(objNL, objRef, k*glow, k*rayleigh, k*mie)
			helped = helped || filtered > broad
			w = s.Quality[i] * max(broad, filtered)
		} else {
			sb = atmos.SkyBrightnessLit(zenithNL, s.Ext[i], a, rho, s.MoonLight[i])
			w = s.Quality[i] * ext * min(1, math.Sqrt(objRef/(objNL+k*sb)))
		}
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
		weight[i] = w
		weighted += w
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
	r.Score = weighted / float64(len(s.Grid)) * r.Frame
	if helped {
		r.Filter = "+"
	}

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

// filterPass is what one -filter choice passes for one target: w, its share of the
// target's light (the lines in the filter's bands, catalog.Target.Lines), and
// glow, rayleigh and mie, the shares of atmos.SkyParts' parts (atmos.BandSky,
// summed over the bands, which a dual-band filter takes onto one frame). w is
// 0 without a filter or, for a line filter, a target without lines. ray and aer scale the
// extinction's Rayleigh and aerosol parts to the bands: (λ/551)⁻⁴ and
// (λ/551)^-1.3, each line's weighted by its share of the light passed.
type filterPass struct{ w, glow, rayleigh, mie, ray, aer float64 }

// passes returns what the -filter choice name passes for tg; nothing for ""
// (no filter).
func passes(cfg *config.Config, tg catalog.Target, name string) filterPass {
	var fp filterPass
	if name == "" {
		return fp
	}
	lines := tg.Lines()
	if bands := config.Broadband[name]; bands != nil {
		return broadPasses(tg, lines, bands)
	}
	for _, l := range config.Filters[name] {
		glow, rayleigh, mie := atmos.BandSky(catalog.LineNM[l], cfg.FilterNM)
		fp.w += lines[l]
		fp.glow += glow
		fp.rayleigh += rayleigh
		fp.mie += mie
		ray, aer := atmos.BandScale(catalog.LineNM[l])
		fp.ray += lines[l] * ray
		fp.aer += lines[l] * aer
	}
	if fp.w > 0 {
		fp.ray, fp.aer = fp.ray/fp.w, fp.aer/fp.w
	}

	return fp
}

// broadPasses is passes for a broadband filter's bands, against the
// unfiltered camera band (atmos.BroadSky) rather than the V band a line
// filter's width is a share of: an emission-line target's w is its passed
// lines' share of all of them, Hβ included (so passing every line is 1, as
// unfiltered), anything else's its continuum's, taken flat per nm.
func broadPasses(tg catalog.Target, lines [4]float64, bands [][2]float64) filterPass {
	var fp filterPass
	emission := catalog.EmissionLine(tg)
	for _, b := range bands {
		glow, rayleigh, mie := atmos.BroadSky(b[0], b[1])
		fp.glow += glow
		fp.rayleigh += rayleigh
		fp.mie += mie
		if !emission {
			ray, aer := atmos.BandMean(b[0], b[1])
			fp.w += glow
			fp.ray += glow * ray
			fp.aer += glow * aer

			continue
		}
		for l, nm := range catalog.LineNM {
			if nm >= b[0] && nm <= b[1] {
				ray, aer := atmos.BandScale(nm)
				fp.w += lines[l]
				fp.ray += lines[l] * ray
				fp.aer += lines[l] * aer
			}
		}
	}
	if fp.w > 0 {
		fp.ray, fp.aer = fp.ray/fp.w, fp.aer/fp.w
	}
	if emission {
		fp.w /= lines[catalog.Ha] + lines[catalog.OIII] + lines[catalog.SII] + lines[catalog.Hb]
	}

	return fp
}

// weight is the sky weight through the filter, the counterpart of the
// broadband min(1, √(objRef/(objNL + k·sky))): the target's signal is w of
// it, the sky the bands' share of each part, so SNR² goes as
// (w·S)²/(w·S + sky in the bands) against S²/(S + RefNL) for the reference.
func (fp filterPass) weight(objNL, objRef, glow, rayleigh, mie float64) float64 {
	sky := fp.glow*glow + fp.rayleigh*rayleigh + fp.mie*mie

	return min(1, math.Sqrt(fp.w*fp.w*objRef/(fp.w*objNL+sky)))
}

// moonK is the fraction of the moonlight, against the target's own share of
// light passed, that the filter lets count: rayleigh/w², the sky-limited
// weight's ratio to broadband under a Moon well away (where Rayleigh
// scattering dominates K&S's), so it scales the Moon separation limit as k
// does. 1 without a filter, and at most 1: a filter that does not beat the
// Moon leaves the target to the broadband limit.
func (fp filterPass) moonK() float64 {
	if fp.w == 0 {
		return 1
	}

	return min(1, fp.rayleigh/(fp.w*fp.w))
}

// skyK returns the fraction of sky glow (moonlight, light pollution) that
// counts against a target imaged without a filter: 1 for faint targets, 0.5
// for clusters (point sources; a cluster with nebulosity is imaged for the
// glow and is not a catalog.Target.Cluster).
func skyK(tg catalog.Target) float64 {
	if tg.Cluster() {
		return 0.5
	}

	return 1
}
