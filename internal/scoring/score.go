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
	Obs     []bool        // per grid minute, observable; as Alt (a minute's weight can be 0 under rain)
	Runs    int           // continuous observable stretches
	Foto    float64       // fraction of time observable
	Score   float64       // foto weighted by clouds, moonlight and framing
	Frame   float64       // framing factor (1 without framing), divided among the mosaic's panels
	Mosaic  config.Mosaic // the grid Frame was scored on; zero without framing
	Filter  string        // the -filter choice that scored best, "" when none beat no filter
	SkyMag  float64       // mean sky brightness at the target while observable, mag/arcsec²
	MeanAlt float64
	MaxAlt  float64
	// Sub-exposure estimate, set by main (Subs) for the listed results with
	// framing; nil for none. A pointer: Score copies and sorts Results by
	// value, and the 64-byte Estimate inline cost BenchmarkPipeline 4%.
	Exposure *Estimate
}

// Score scores every target within the size limits and returns the
// observable ones, best first. perMinute keeps each result's per-minute
// Alt and Weight, which plan.Make needs, and Obs.
func Score(cfg *config.Config, s *Sky, targets []catalog.Target, perMinute bool) []Result {
	var results []Result
	mosaics := cfg.Mosaics()
	names := append([]string{""}, cfg.FilterNames()...) // "" is no filter
	// Per-minute scratch per choice, reused across targets: most are never
	// observable, and only perMinute needs a Result's own copy (and the
	// altitudes at all).
	tallies := make([]tally, len(names))
	for c := range tallies {
		tallies[c].weight = make([]float64, len(s.Grid))
		if perMinute {
			tallies[c].obs = make([]bool, len(s.Grid))
		}
	}
	var alt []float64
	if perMinute {
		alt = make([]float64, len(s.Grid))
	}
	active, choice := make([]*tally, 0, len(names)), make([]int, 0, len(names))
	cam, camOK := newCamera(cfg, s)
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
		// No filter, then each -filter choice that passes some of the
		// target's light, scored as worn all night, as it is shot: a filter
		// that costs a bright nebula under a dark sky costs it every minute.
		// All in one pass over the grid, which shares the geometry. The
		// choice that gains most wins, the first of equals, so a filter must
		// beat no filter; a target none beats (a galaxy, which a broadband
		// filter dims as much as the sky) keeps exactly its unfiltered
		// weights. Gains most: with a camera and a surface brightness to
		// estimate subs from (catalog.Target.ExposureSB), the fewest subs
		// the sub-exposure model needs for the good level (subsGain over
		// rates: read noise, dark current, -stack and the -residual ceiling
		// included), over the choice's own observable minutes, so FILTER is
		// the choice SUBS counts for (the total SNR² would let a narrowband filter win on minutes alone: its
		// moonK admits moonlit, poor minutes no filter keeps); else the most
		// uncapped weight, since under a dark sky every helpful filter's
		// capped weight is 1 and the band's extinction alone would choose
		// (Hα's, even for a planetary).
		camNL, scale := 0.0, 1.0
		if camOK {
			if sb, _ := tg.ExposureSB(); sb > 0 {
				camNL, scale = lightNL(tg, sb), levelScale(tg, sb, s.RefNL)
			}
		}
		active, choice = active[:0], choice[:0]
		for c, name := range names {
			fp := passes(cfg, tg, name)
			if c > 0 && fp.w == 0 {
				continue
			}
			tallies[c].reset(fp)
			if camNL > 0 {
				tallies[c].chans = cam.channels(tallies[c].chans, name, cfg.FilterNM, tg.Lines(), fp)
			}
			active, choice = append(active, &tallies[c]), append(choice, c)
		}
		base, ok := scoreTarget(cfg, s, tg, active, mosaics, alt, &cam, camNL)
		if !ok {
			continue
		}
		gains := func(t *tally) float64 {
			if camNL > 0 {
				// The fewest-subs choice; the stack counted minutes, since
				// gains runs only for a tally whose result held.
				m, ceil := cam.rates(&t.st, t.chans)

				return subsGain(m/scale, ceil/scale)
			}

			return t.raw
		}
		var best Result
		var bestGain float64
		found := false
		for j, t := range active {
			r, ok := t.result(cfg, s, base)
			if !ok || (found && gains(t) <= bestGain) {
				continue
			}
			bestGain = gains(t)
			r.Filter = names[choice[j]]
			if perMinute {
				r.Alt, r.Weight, r.Obs = slices.Clone(alt), slices.Clone(t.weight), slices.Clone(t.obs)
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

// tally is one -filter choice's running score in scoreTarget: what it
// passes, its Moon limit, and the observable minutes it has counted, with
// their weights (capped at 1, the score), uncapped (raw) and, with a camera
// (chans), the sub-exposure model's stack sums (st), each weighted by the
// minute's weather.
type tally struct {
	chans                    []channel
	weight                   []float64 // per-minute weight, len(s.Grid)
	obs                      []bool    // per-minute observable; nil unless perMinute
	fp                       filterPass
	cosMoon                  float64 // the Moon limit's cosine: a minute closer is not observable
	altSum, weighted, skySum float64
	raw                      float64
	st                       stack // the sub-exposure model's sums, with chans
	good, runs, prev         int
	// Continuous observable stretches: the current one starts at runStart
	// and the longest so far is [best, best+bestLen).
	runStart, best, bestLen int
}

// reset starts t over for a target through fp.
func (t *tally) reset(fp filterPass) {
	weight, obs := t.weight, t.obs
	clear(weight)
	clear(obs)
	*t = tally{chans: t.chans[:0], weight: weight, obs: obs, fp: fp, prev: -2} // chans: channels' buffer
}

// add counts minute i, observable at altitude a under a sky of sb with
// weight w (raw uncapped).
func (t *tally) add(i int, a, w, raw, sb float64) {
	if i != t.prev+1 {
		t.runStart = i
		t.runs++
	}
	t.prev = i
	if n := i - t.runStart + 1; n > t.bestLen {
		t.best, t.bestLen = t.runStart, n
	}
	t.good++
	t.altSum += a
	t.skySum += atmos.MagFromNL(sb)
	t.weight[i] = w
	if t.obs != nil {
		t.obs[i] = true
	}
	t.weighted += w
	t.raw += raw
}

// result completes base (scoreTarget's frame, mosaic and peak) with what t
// counted; false when it counted no minute or its longest stretch is shorter
// than -min-run.
func (t *tally) result(cfg *config.Config, s *Sky, base Result) (Result, bool) {
	r := base
	if t.good == 0 {
		return r, false
	}
	// The grid is minute-aligned from Start, so the run ends a minute after
	// its last minute, or at End for the last grid minute (which End may cut
	// short, so -min-run judges the clock span, not the minute count).
	r.Runs = t.runs
	r.RunFrom, r.RunTo = s.Grid[t.best], s.End
	if t.best+t.bestLen < len(s.Grid) {
		r.RunTo = s.Grid[t.best+t.bestLen]
	}
	if r.RunTo.Sub(r.RunFrom) < cfg.MinRun {
		return r, false
	}
	r.Foto = float64(t.good) / float64(len(s.Grid))
	r.MeanAlt = t.altSum / float64(t.good)
	r.SkyMag = t.skySum / float64(t.good)
	r.Score = t.weighted / float64(len(s.Grid)) * r.Frame

	return r, true
}

// scoreTarget counts the target's observable minutes and their weights into
// each of ts (reset, a filterPass each; fp.w 0 is no filter) in one pass over
// the grid, and fills alt with its altitude when not nil; a tally with chans
// also counts cam's per-sub SNR² of the object's light camNL (lightNL of its
// ExposureSB, absolute). It returns the result every tally shares (frame,
// mosaic, peak) for tally.result, false when no mosaic holds the target.
func scoreTarget(cfg *config.Config, s *Sky, tg catalog.Target, ts []*tally, mosaics []config.Mosaic, alt []float64, cam *camera, camNL float64) (Result, bool) {
	ra, dec, extra := where(s, tg)
	hz := astro.NewHorizontal(cfg.Lat, dec)
	// cos(LST - ra) by the difference formula: one Sincos per target instead
	// of a Cos per target-minute.
	sinRA, cosRA := math.Sincos(ra * deg)

	// The Moon separation is the Acos of a dot product of unit vectors, and a
	// minute closer than every limit (at most 100°, where the cosine still
	// falls) is rejected on the cosine alone. On a moonlit night with low
	// altitude limits Separation's trigonometry was 40% of scoring.
	unit := astro.Unit(ra, dec) // a comet's is redone per minute
	loosest := -1.0
	for _, t := range ts {
		t.cosMoon = math.Cos(t.fp.moonK() * s.MoonSep * deg)
		loosest = max(loosest, t.cosMoon)
	}

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
	// The object's own light (lightNL), in the sky's units; 0 when unknown,
	// which scores it as sky-limited, as every target was before SB existed.
	var objNL float64
	if sb, _ := tg.SurfaceBrightness(); sb > 0 {
		objNL = lightNL(tg, sb)
	}
	// An emission-line target's light is its lines, which the camera collects
	// once while its band holds CameraBand times the V band's sky: against the
	// V sky that stands for the camera's, it is that much fainter (a continuum
	// scales with the band, as the sky does). Unfiltered its lines are dimmed
	// at their own wavelengths, as through a filter: V's coefficient, with the
	// Rayleigh and aerosol parts moved to the lines (0, 0 for a continuum).
	var lineRay, lineAer float64
	emission := catalog.EmissionLine(tg)
	if emission {
		objNL /= atmos.CameraBand
		all := broadPasses(tg, tg.Lines(), cameraBand)
		lineRay, lineAer = all.ray-1, all.aer-1
	}
	objRef := objNL + s.RefNL // the same object under a pristine sky
	ax, bx := axesArcsec(tg)
	maxSin, maxAt := -2.0, 0
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
		// c is the cosine of the Moon separation, -1 (no limit) with the
		// Moon down.
		rho, c := 180.0, -1.0
		if s.MoonAlt[i] > 0 {
			if tg.Track != nil {
				unit = astro.Unit(ra, dec)
			}
			m := &s.MoonUnit[i]
			c = max(-1, min(1, unit[0]*m[0]+unit[1]*m[1]+unit[2]*m[2]))
			if c > loosest {
				continue
			}
			rho = math.Acos(c) / deg
		}
		// SNR in a fixed time goes as signal/sqrt(signal+sky), relative to the
		// same object under a pristine dark sky: an object much brighter than
		// the sky loses nothing, a faint or unknown one (objNL = 0) is
		// sky-limited, sqrt(RefNL/(k·sky)). Through a filter, the filtered
		// weight with the bands' own extinction (their Rayleigh and aerosol
		// parts scale with wavelength); the sky is then the sum of its parts
		// (SkyBrightnessLit's to rounding), computed once for every filter.
		air := atmos.Airmass(a)
		ext := atmos.Transmission(air, s.Ext[i])
		// The zodiacal and galactic excess is tabulated above the
		// atmosphere, so the whole column dims it, not the zenith-relative
		// ext of the ground-measured sky and the score.
		extraNL := extra * atmos.Absolute(air, s.Ext[i])
		var glow, rayleigh, mie float64
		parts, blur := false, -1.0
		for _, t := range ts {
			if c > t.cosMoon {
				continue
			}
			if !parts && (t.fp.w > 0 || len(t.chans) > 0) {
				glow, rayleigh, mie = atmos.SkyParts(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i])
				glow += extraNL
				parts = true
			}
			// kb is the object's band's extinction coefficient, for the
			// camera; trans keeps its own inline expression, which the
			// compiler may fuse (FMA), so the score stays bit for bit.
			var sb, trans, gain, kb float64
			if t.fp.w > 0 {
				sb = glow + rayleigh + mie
				trans = atmos.Transmission(air, s.ExtRayleigh[i]*t.fp.ray+atmos.OzoneExtinction+s.ExtAerosol[i]*t.fp.aer)
				kb = s.ExtRayleigh[i]*t.fp.ray + atmos.OzoneExtinction + s.ExtAerosol[i]*t.fp.aer
				gain = t.fp.gain(objNL, objRef, glow, rayleigh, mie)
			} else {
				sb = atmos.SkyBrightnessLit(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i]) + extraNL
				trans, kb = ext, s.Ext[i]
				if emission {
					trans = atmos.Transmission(air, s.Ext[i]+s.ExtRayleigh[i]*lineRay+s.ExtAerosol[i]*lineAer)
					kb = s.Ext[i] + s.ExtRayleigh[i]*lineRay + s.ExtAerosol[i]*lineAer
				}
				gain = math.Sqrt(objRef / (objNL + sb))
			}
			if len(t.chans) > 0 {
				if blur < 0 {
					blur = cam.blur(i, air, ax, bx)
				}
				// Absolute photons: the object is catalogued above the
				// atmosphere, so the zenith's extinction dims it too.
				cam.add(&t.st, t.chans, i, s.Quality[i], camNL*atmos.Absolute(air, kb)*blur, glow, rayleigh, mie)
			}
			t.add(i, a, s.Quality[i]*trans*min(1, gain), s.Quality[i]*trans*gain, sb)
		}
	}
	r.MaxAlt, r.MaxAt = math.Asin(maxSin)/deg, s.Grid[maxAt]

	return r, true
}

// where returns tg's mid-window position of date (degrees; a comet's
// mid-track one) and extra, the zodiacal and galactic light there against
// what the zenith brightness includes: half a magnitude more on the ecliptic
// 90° from the Sun, a little less at the ecliptic pole; up to 1.7 more in the
// Milky Way, 0.3 less at the galactic poles; at the zenith never below both
// minima. It comes from beyond the atmosphere, so extinction dims it and,
// unlike the glow K&S brighten along a longer path through the air, nothing
// adds to it: it stays outside their dark-sky term.
func where(s *Sky, tg catalog.Target) (float64, float64, float64) {
	mid := s.Grid[len(s.Grid)/2]
	var ra, dec float64
	if tg.Track == nil {
		// Catalogs are J2000; the hour angle comes from sidereal time of date.
		ra, dec = astro.Precess(tg.RADeg, tg.DecDeg, mid)
	} else {
		ra, dec = tg.Track[len(s.Grid)/2][0], tg.Track[len(s.Grid)/2][1]
	}
	lon, lat := astro.Ecliptic(ra, dec, mid)
	gl, gb := astro.Galactic(ra, dec) // of date for J2000: 0.4°, against 10° cells
	extra := max(atmos.ZodiacalExcess(lon-s.SunLon, lat)+atmos.GalacticExcess(gl, gb), atmos.ZodiacalPoleNL+atmos.GalacticMinNL-s.ZenithNL)

	return ra, dec, extra
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

// filterPass is what one -filter choice passes for one target: w, its share
// of the target's light (the lines in the filter's bands, catalog.Target.Lines),
// and glow, rayleigh and mie, the shares of atmos.SkyParts' parts against the
// unfiltered camera band (atmos.BroadSky over each band, summed over the
// bands, which a dual-band filter takes onto one frame). w is 0 without a
// filter or, for a line filter, a target without lines. ray and aer scale
// the extinction's Rayleigh and aerosol parts to the bands: (λ/551)⁻⁴ and
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
	// Against the camera band, as a broadband filter: a line filter's bands
	// were once shares of V's 88 nm, which charged them CameraBand times the
	// sky per nm a broadband filter is charged against the same unfiltered sky.
	ls := config.Filters[name]
	all := lineBands(ls, cfg.FilterNM)
	bands := all[:len(ls)]
	for _, b := range bands {
		glow, rayleigh, mie := atmos.BroadSky(b[0], b[1])
		fp.glow += glow
		fp.rayleigh += rayleigh
		fp.mie += mie
	}
	// Every line a band reaches, the filter's own and the others in it
	// ([N II] in Hα's), each once, as much of it as the beam lets through.
	bm := beamOf(cfg)
	for l, nm := range catalog.LineNM {
		if t := bm.passed(nm, bands); t > 0 {
			ray, aer := atmos.BandScale(nm)
			fp.w += lines[l] * t
			fp.ray += lines[l] * t * ray
			fp.aer += lines[l] * t * aer
		}
	}
	if fp.w > 0 {
		fp.ray, fp.aer = fp.ray/fp.w, fp.aer/fp.w
	}

	return fp
}

// lineBands returns each of ls's passbands, λ ± width/2 nm, clipped at the
// midpoint between two lines whose bands would overlap (-filter-nm goes to
// 30: Hβ meets [O III] past 14.6 nm, Hα meets [S II] past 16.1), so the
// shared wavelengths charge the sky once, as a physical filter's merged
// bands pass them once, each side at its own line's response. An array:
// at most 4 lines (quad), and passes runs per target in Score's loop.
func lineBands(ls []catalog.Line, width float64) [4][2]float64 {
	var bands [4][2]float64
	for i, l := range ls {
		nm := catalog.LineNM[l]
		bands[i] = [2]float64{nm - width/2, nm + width/2}
	}
	for i := range ls {
		for j := range ls {
			ci, cj := catalog.LineNM[ls[i]], catalog.LineNM[ls[j]]
			if ci < cj && bands[i][1] > bands[j][0] {
				mid := (ci + cj) / 2
				bands[i][1] = min(bands[i][1], mid)
				bands[j][0] = max(bands[j][0], mid)
			}
		}
	}

	return bands
}

// filterN is a standard interference filter's effective index: tilted by θ
// its band moves to λ·√(1 − sin²θ/n²). 2.05 fits Chroma's published curves
// of its 3, 5 and 8 nm Hα, Hβ, [O III] and [S II] filters at f/2 to f/5 as
// top-hats of their nominal width (rms 0.06 in the line's transmission, no
// bias; scripts/filter_shift.py).
const filterN = 2.05

// beam is the light cone line filters sit in (config.Config.FRatio,
// Obstructed): its focal ratio and central obstruction as a fraction of the
// aperture. The zero beam is collimated, the filters made for it: a band
// passes a line inside it whole.
type beam struct{ fRatio, obstructed float64 }

func beamOf(cfg *config.Config) beam { return beam{cfg.FRatio, cfg.Obstructed} }

// passed returns the share of a line at nm the bands let through, the most
// any one does. In a cone a standard filter's band shifts blue with each
// ray's angle θ (tan θ = r/2N across the pupil, r from the obstruction to
// 1), so a line inside the collimated band leaves it in the outer rays and
// one just blue of it enters: the share is the pupil area, uniform in r²,
// whose shifted band holds the line. The sky is the bands' own: the shift
// moves each band, not its width.
func (b beam) passed(nm float64, bands [][2]float64) float64 {
	var t float64
	for _, band := range bands {
		t = max(t, b.through(nm, band))
	}

	return t
}

// through is passed for one band [lo, hi]: the line is in while
// n²(1 − (nm/lo)²) ≤ sin²θ ≤ n²(1 − (nm/hi)²).
func (b beam) through(nm float64, band [2]float64) float64 {
	if b.fRatio == 0 {
		if nm >= band[0] && nm <= band[1] {
			return 1
		}

		return 0
	}
	// r² across the pupil at which sin²θ is reached; past 1 no ray is
	// that steep.
	r2 := func(sin2 float64) float64 {
		if sin2 <= 0 {
			return 0
		}
		if sin2 >= 1 {
			return math.Inf(1)
		}

		return sin2 / (1 - sin2) * 4 * b.fRatio * b.fRatio
	}
	n2, e2 := filterN*filterN, b.obstructed*b.obstructed
	from := max(e2, r2(n2*(1-(nm/band[0])*(nm/band[0]))))
	to := min(1, r2(n2*(1-(nm/band[1])*(nm/band[1])))) // 0 for a line red of the band

	return max(0, to-from) / (1 - e2)
}

// vContinuum (mag) is the share of an emission nebula's catalogued V light
// that is not its lines (the stars inside it, scattered continuum), fitted by
// scripts/calibrate_lines.py: the median over 15 nebulae of 30' and more of
// Finkbeiner (2003)'s Hα map against their lines derived from V by catalog.LineV
// (interquartile range 0.22-1.92 mag).
const vContinuum = 0.99

// lightNL returns the light in nanoLamberts of tg at V surface brightness
// sb: a continuum's is its V; an emission-line target's is its lines, which
// V measures only as far as its passband reaches them (catalog.LineV) less what of
// it is continuum (vContinuum): 1.63 mag brighter than V for an H II region,
// 0.36 for a planetary, whose [O III] V holds. A surface brightness from a
// measured line flux (catalog.Target.FluxSB) holds no stars and was summed
// in photons, so it is undone exactly: V's photon-weighted share of the
// lines, no vContinuum.
func lightNL(tg catalog.Target, sb float64) float64 {
	nl := atmos.NanoLamberts(sb)
	if _, ok := tg.FluxSB(); ok {
		var through, all float64
		for l, w := range tg.Lines() {
			through += w * catalog.LineNM[l] * catalog.LineV[l]
			all += w * catalog.LineNM[l]
		}

		return nl * all / through
	}
	var fv float64
	for l, w := range tg.Lines() {
		fv += w * catalog.LineV[l]
	}
	if fv == 0 {
		return nl
	}

	return nl / fv * math.Pow(10, -0.4*vContinuum)
}

// cameraBand is the unfiltered camera's one passband.
var cameraBand = [][2]float64{{atmos.CameraLo, atmos.CameraHi}}

// broadPasses is passes for a broadband filter's bands, against the
// unfiltered camera band (atmos.BroadSky): an emission-line target's w is
// its passed lines' share of them (Target.Lines sum to 1, so passing every
// line is 1, as unfiltered), anything else's its continuum's, taken flat per
// nm.
func broadPasses(tg catalog.Target, lines [catalog.NumLines]float64, bands [][2]float64) filterPass {
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
		}
	}
	if emission {
		// inBands (the channels' test too), so a line counts once however
		// many passbands reach it.
		for l, nm := range catalog.LineNM {
			if inBands(nm, bands) {
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

	return fp
}

// gain is the filter's SNR against the same object unfiltered under a
// pristine sky, uncapped: the target's signal is w of it, the sky the bands'
// share of each part, so SNR² goes as (w·S)²/(w·S + sky in the bands)
// against S²/(S + RefNL) for the reference. scoreTarget caps it at 1, the
// counterpart of the broadband min(1, √(objRef/(objNL + k·sky))): a minute
// as good as unfiltered under a pristine sky.
func (fp filterPass) gain(objNL, objRef, glow, rayleigh, mie float64) float64 {
	sky := fp.glow*glow + fp.rayleigh*rayleigh + fp.mie*mie

	return math.Sqrt(fp.w * fp.w * objRef / (fp.w*objNL + sky))
}

// minMoonK floors moonK: near the Moon its halo and the gradients across the
// frame, which no λ⁻⁴ scattering model holds, limit narrowband imagers to
// some 30° from a full Moon however narrow the band.
const minMoonK = 0.3

// moonK is the fraction of the moonlight, against the target's own share of
// light passed, that the filter lets count: rayleigh/w², the sky-limited
// weight's ratio to broadband under a Moon well away (where Rayleigh
// scattering dominates the moonlight), so it scales the Moon separation
// limit as k does; no less than minMoonK. 1 without a filter, and at most 1:
// a filter that does not beat the Moon leaves the target to the broadband
// limit.
func (fp filterPass) moonK() float64 {
	if fp.w == 0 {
		return 1
	}

	return max(minMoonK, min(1, fp.rayleigh/(fp.w*fp.w)))
}
