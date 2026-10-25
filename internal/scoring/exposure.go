// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"cmp"
	"math"
	"slices"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

// Levels are the stacked SNR per arcsec², at the object's mean surface
// brightness, of a modest, good and best image of an object as bright as a
// pristine sky (Sky.RefNL) or brighter; a fainter one's are √ of its
// brightness over the sky's of them (levelScale).
var Levels = [3]float64{5, 10, 20}

// levelScale is the share of Levels' SNR² that makes a modest, good and best
// image of tg at surface brightness sb: its light against the pristine sky
// refNL as scoreTarget sets it (an emission-line target's lines against the
// camera band), at most 1. Imagers stretch a faint object harder and judge
// its brighter structure, so what they stack for one grows as 1/S, not the
// 1/S² of a fixed SNR under a sky-limited stack: against reported totals
// (Seestar S50 and Origin, 2026) a fixed SNR 10 made M 101 and M 33 20-40x
// too long while M 51, M 42 and the Veil were 1-3x.
// ponytail: an acceptance rule fitted to anecdotes, not optics; a profile
// (the bright arms' own surface brightness) would replace it.
func levelScale(tg catalog.Target, sb, refNL float64) float64 {
	nl := lightNL(tg, sb)
	if catalog.EmissionLine(tg) {
		nl /= atmos.CameraBand
	}

	return min(1, nl/refNL)
}

// channelShare is each colour channel's share of the array (RGGB).
var channelShare = [3]float64{red: 0.25, green: 0.5, blue: 0.25}

// A colour sensor's channels.
const (
	red = iota
	green
	blue
)

// An uncooled sensor runs sensorWarm (°C) above the air, its dark current
// doubling every darkDoubling (ZWO's charts: the IMX585's and IMX676's at
// 15-30 °C); subGap is the time between subs (download and the live stack,
// s), which tonight's sub count (Estimate.Max) loses.
// ponytail: sensorWarm from Seestar's note that its battery runs about 10 °C
// above the air, in the same housing; no smart telescope publishes its
// sensor's. -sensor-temp sets a cooled camera's.
const (
	sensorWarm   = 10.0
	darkDoubling = 5.0
	subGap       = 1.0
)

// maxSubs caps a count, so an absurdly faint object cannot overflow an int.
const maxSubs = 1_000_000_000

// Estimate is one result's sub-exposure estimate; the zero value (Good 0)
// means none.
type Estimate struct { // betteralign:ignore
	Modest    int     `json:"modest"`                             // subs to shoot for Levels[0], per panel
	Good      int     `json:"good"`                               // Levels[1]
	Best      int     `json:"best"`                               // Levels[2]
	Max       int     `json:"max"`                                // subs tonight's time allows per panel (with -plan, its blocks), less the gaps between them and -reject's share
	Sub       float64 `json:"sub"`                                // sub length, s
	SNRPerSub float64 `json:"snrPerSub"`                          // per arcsec², √ of the mean SNR² the stack adds, weather included, against Levels (levelScale)
	SNRLimit  float64 `json:"snrLimit,omitempty"`                 // per arcsec² against Levels, the most the stack reaches however many subs (-residual); 0 = none
	Panels    int     `json:"panels"`                             // the mosaic the counts are per panel of
	Assumed   bool    `json:"assumedSurfaceBrightness,omitempty"` // the surface brightness is its class's typical one
}

// Unreachable reports whether no number of subs reaches stacked SNR level
// under the stack's ceiling (SNRLimit, -residual).
func (e *Estimate) Unreachable(level float64) bool {
	return e.SNRLimit > 0 && e.SNRLimit <= level
}

// camera is what the estimate needs of the telescope and camera.
type camera struct {
	response                        *config.Response     // a colour sensor's, cfg.ColourResponse
	monoResponse                    *config.MonoResponse // a mono sensor's, cfg.MonoResponse
	dark                            []float64            // e⁻/s per pixel per grid minute; nil for none
	seeing                          []float64            // Sky.Seeing
	beam                            beam                 // line filters' cone, beamOf
	area, qe, readNoise, sub, omega float64              // cm² (times the throughput), peak, e⁻, s, arcsec² per pixel
	optics2, residual               float64              // the optics' and pixels' blur, FWHM² arcsec²; cfg.Residual
	mono, mean                      bool                 // mean: cfg.MeanStack
}

// pixelFWHM is a square pixel's blur as a Gaussian FWHM, in pixels: its
// box response has the variance p²/12 of a Gaussian of σ = p/√12, whose
// FWHM is 2√(2 ln 2) σ = 0.68 p (a full pixel, p², would count it twice).
var pixelFWHM = 2 * math.Sqrt(2*math.Ln2) / math.Sqrt(12)

// newCamera returns cfg's camera under s's temperatures; false without
// framing or with a hand-built Config that does not describe one (a zero
// Throughput there is 1, a zero Dark none).
func newCamera(cfg *config.Config, s *Sky) (camera, bool) {
	if !cfg.Framing || cfg.Aperture <= cfg.Obstruction || cfg.Sub <= 0 || cfg.QE <= 0 || cfg.Scale <= 0 {
		return camera{}, false
	}
	d, o := cfg.Aperture/10, cfg.Obstruction/10
	c := camera{
		area: math.Pi / 4 * (d*d - o*o) * cmp.Or(cfg.Throughput, 1), qe: cfg.QE, readNoise: cfg.ReadNoise, sub: cfg.Sub,
		omega: cfg.Scale * cfg.Scale, mono: cfg.Mono, beam: beamOf(cfg), response: cfg.ColourResponse(),
		monoResponse: cfg.MonoResponse(),
		mean:         cfg.MeanStack, residual: cfg.Residual, seeing: s.Seeing,
	}
	c.optics2 = opticsFWHM2(cfg)
	if cfg.Dark > 0 && len(s.Temp) > 0 {
		c.dark = make([]float64, len(s.Temp))
		for i, t := range s.Temp {
			if cfg.SensorTempSet {
				t = cfg.SensorTemp
			} else {
				t += sensorWarm
			}
			c.dark[i] = cfg.Dark * math.Exp2((t-25)/darkDoubling)
		}
	}

	return c, true
}

// channel is one set of pixels that see alike, which an optimally weighted
// stack adds by SNR²: a colour sensor's red, green or blue, a mono sensor's
// every pixel. share is its share of the array; obj the object's e⁻/s per
// pixel of it per nanoLambert of the object's light, the share passed
// included; sky the same per nanoLambert of each of atmos.SkyParts' glow,
// Rayleigh and Mie parts.
type channel struct {
	sky        [3]float64
	share, obj float64
}

// channels returns the sensor's channels through the -filter choice name
// ("" none), fp what it passes and width the line bands' width. The sky, and
// an object without emission lines (lines zero, Target.Photons), are
// continuum: unfiltered and broadband spread over the camera band, the glow
// flat per nm and the moonlight as it scatters (atmos.BandMean against V's),
// at each channel's mean response over the band (config.Response.Broad), through a
// broadband filter its passbands' shares (fp). Through a line filter each
// band passes its own share of the sky (atmos.BandSky, against V) at its
// line's response, to every channel by its dye's (config.Response). An
// emission-line object's light is its lines wherever the band passes them,
// collected once, each channel taking its dye's response: through Hα a
// colour IMX678's green pixels respond at 0.34 of the peak against the red
// ones' 0.77, the blue ones at 0.11. The object's gains are 0 when a line filter passes
// none of its lines.
// The channels overwrite dst's, so Score reuses each tally's across targets.
func (c camera) channels(dst []channel, name string, width float64, lines [catalog.NumLines]float64, fp filterPass) []channel {
	g := c.area * c.qe * c.omega * atmos.PhotonsV0 / atmos.NanoLamberts(0)
	chans := dst[:0]
	if c.mono {
		chans = append(chans, channel{share: 1})
	} else {
		chans = append(chans, channel{share: channelShare[red]}, channel{share: channelShare[green]}, channel{share: channelShare[blue]})
	}
	// resp is channel i's response to line l relative to the peak.
	resp := func(l catalog.Line, i int) float64 {
		if c.mono {
			return c.monoResponse.Line(l)
		}

		return c.response.Line(l)[i]
	}
	emission := lines != [catalog.NumLines]float64{}
	if ls := config.Filters[name]; ls != nil {
		// Each band clipped where a wide -filter-nm would overlap its
		// neighbour (lineBands), so the shared sky is charged once; BandSky
		// scatters at the line's centre, so only the width matters.
		all := lineBands(ls, width)
		bands := all[:len(ls)]
		for b, l := range ls {
			glow, rayleigh, mie := atmos.BandSky(catalog.LineNM[l], bands[b][1]-bands[b][0])
			for i := range chans {
				r := g * resp(l, i)
				chans[i].sky[0] += glow * r
				chans[i].sky[1] += rayleigh * r
				chans[i].sky[2] += mie * r
			}
		}
		// The object: every line a band reaches (passes' w), [N II] in Hα's,
		// as much of it as the beam lets through.
		for l, nm := range catalog.LineNM {
			if t := c.beam.passed(nm, bands); t > 0 {
				for i := range chans {
					chans[i].obj += lines[l] * t * g * resp(catalog.Line(l), i)
				}
			}
		}

		return chans
	}
	shares := [3]float64{1, 1, 1}
	if fp.w > 0 {
		shares = [3]float64{fp.glow, fp.rayleigh, fp.mie}
	}
	ray, aer := atmos.BandMean(atmos.CameraLo, atmos.CameraHi)
	// ponytail: each channel's continuum is its mean response, the moonlight's
	// colour weighted as over the whole band (atmos.BandMean), not by the
	// channel's own curve; a per-channel weighting would give blue more
	// Rayleigh moonlight.
	for i := range chans {
		qe := c.monoResponse.Broad
		if !c.mono {
			qe = c.response.Broad[i]
		}
		cont := g * qe * atmos.CameraBand
		chans[i].sky = [3]float64{cont * shares[0], cont * ray * shares[1], cont * aer * shares[2]}
		if !emission {
			// A continuum object through a broadband filter: its passbands' share.
			chans[i].obj = cont * cmp.Or(fp.w, 1)
		}
	}
	if emission {
		// A zero share adds nothing here, and camera.add's ch.obj > 0 gate is
		// where a channel that sees none of the object drops out, read noise
		// included: change the two together.
		for l, share := range lines {
			if passes := config.Broadband[name]; passes == nil || inBands(catalog.LineNM[l], passes) {
				for i := range chans {
					chans[i].obj += share * g * resp(catalog.Line(l), i)
				}
			}
		}
	}

	return chans
}

// inBands reports whether nm falls in one of bands.
func inBands(nm float64, bands [][2]float64) bool {
	for _, b := range bands {
		if nm >= b[0] && nm <= b[1] {
			return true
		}
	}

	return false
}

// stack sums what a target's stacked SNR needs over its observable minutes
// (n), each weighted by the share q of its subs the weather lets through:
// the optimally weighted stack's SNR² per pixel (snr2), and per channel the
// object's e⁻ per sub per pixel (sig), their variance (noise: object, sky,
// dark current and read noise) and the sky's alone (sky), which equal
// weights and the residual need. Dark current is subtracted with a master
// dark, whose own noise is left out.
type stack struct {
	sig, noise, sky [3]float64
	snr2            float64
	n               int
}

// add counts grid minute i into st: the object's light obj (nanoLamberts,
// absolute, dimmed by its extinction and blur) under a sky of glow,
// rayleigh and mie, with that minute's dark current, the weather letting q
// of its subs through. Scoring chooses a filter by it, so it must stay Subs'
// own.
func (c camera) add(st *stack, chans []channel, i int, q, obj, glow, rayleigh, mie float64) {
	st.n++
	var dark float64
	if c.dark != nil {
		dark = c.dark[i] * c.sub
	}
	rn2 := c.readNoise * c.readNoise
	for j, ch := range chans {
		if ch.obj <= 0 {
			continue
		}
		sig := ch.obj * obj * c.sub
		sky := (ch.sky[0]*glow + ch.sky[1]*rayleigh + ch.sky[2]*mie) * c.sub
		v := sig + sky + dark + rn2
		st.snr2 += q * ch.share * sig * sig / v
		st.sig[j] += q * sig
		st.noise[j] += q * v
		st.sky[j] += q * sky
	}
}

// rates returns what each sub the night allows adds to the stack's SNR² per
// arcsec² (m) and the stack's ceiling (ceil, SNR² per arcsec², +Inf without
// -residual). An optimally weighted stack adds each sub's SNR², S²/σ²; one
// of equal weights (-stack mean) reaches (ΣS)²/Σσ², so a noisy sub dilutes
// it rather than adding little. The residual, a share of the sky that flats
// and gradient removal leave, does not average down and spans many pixels,
// so it caps the SNR at S/(residual·B) whatever the pixel count; the
// channels add by share, as the subs' SNR² do.
func (c camera) rates(st *stack, chans []channel) (float64, float64) {
	if st.n == 0 {
		return 0, math.Inf(1)
	}
	m, ceil := st.snr2, 0.0
	if c.mean {
		m = 0
		for j, ch := range chans {
			if st.noise[j] > 0 {
				m += ch.share * st.sig[j] * st.sig[j] / st.noise[j]
			}
		}
	}
	for j, ch := range chans {
		if r := c.residual * st.sky[j]; r > 0 {
			ceil += ch.share * (st.sig[j] / r) * (st.sig[j] / r)
		}
	}
	if ceil == 0 {
		ceil = math.Inf(1)
	}

	return m / float64(st.n) / c.omega, ceil
}

// need is the subs a stack adding m per sub reaches level with under its
// ceiling: 1/level² = 1/(N·m) + 1/ceil; maxSubs when it cannot.
func need(level, m, ceil float64) int {
	l2 := level * level
	if m <= 0 || l2 >= ceil {
		return maxSubs
	}

	return int(min(math.Ceil(l2/m/(1-l2/ceil)), maxSubs))
}

// subsGain orders the -filter choices by the subs the good level needs, most
// gain first: m(1 - L²/ceil) is L²/N for a reachable level; an unreachable
// one is negative, the higher ceiling ranking first.
func subsGain(m, ceil float64) float64 {
	l2 := Levels[1] * Levels[1]
	if l2 >= ceil {
		return ceil/l2 - 1
	}

	return m * (1 - l2/ceil)
}

// blur is the share of an object of axes a and b (arcseconds; 0: no size,
// none) whose surface brightness survives minute i's blur at airmass air:
// spread by a FWHM F of the seeing (as X^0.6), the optics' diffraction and
// the pixels in quadrature, a uniform ellipse keeps about
// ab/√((a²+F²)(b²+F²)) of it.
func (c camera) blur(i int, air, a, b float64) float64 {
	if a <= 0 || c.seeing == nil {
		return 1
	}
	see := c.seeing[i] * math.Pow(air, 0.6)
	f2 := see*see + c.optics2

	return a * b / math.Sqrt((a*a+f2)*(b*b+f2))
}

// axesArcsec returns tg's axes in arcseconds for blur, 0 without a size.
func axesArcsec(tg catalog.Target) (float64, float64) {
	if !tg.HasSize() || tg.Track != nil {
		return 0, 0
	}
	a, b := tg.Axes()

	return a * 60, b * 60
}

// Subs estimates the subs r needs tonight through r.Filter for each of
// Levels: an optimally weighted stack adds the per-sub SNR², so the mean
// over r's observable minutes (r.Obs; rescored through r.Filter when nil,
// whose Moon limit sets them), each weighted by
// the share of its subs the weather (Quality) lets through, sets the count.
// Counts are per panel of r.Mosaic. planned marks the grid minutes -plan
// gives r (nil: not planned, the whole night): the estimate then counts only
// its observable minutes within them, for both the mean per-sub SNR² and
// Max, so a block the target sets in or spends low counts as it is. The
// zero Estimate means none: no camera, no surface brightness
// (catalog.Target.ExposureSB), or no observable minute; a night the weather
// loses entirely is maxSubs counts with SNRPerSub 0.
func Subs(cfg *config.Config, s *Sky, r Result, planned []bool) Estimate {
	c, ok := newCamera(cfg, s)
	sb, assumed := r.ExposureSB()
	if !ok || sb <= 0 {
		return Estimate{}
	}
	fp := passes(cfg, r.Target, r.Filter)
	obs := r.Obs
	if obs == nil {
		// Rescore through r.Filter itself, not Score, which would choose its
		// own filter: another choice's Moon limit (moonK) admits a different
		// set of minutes, and the estimate must count r.Filter's.
		ta := &tally{weight: make([]float64, len(s.Grid)), obs: make([]bool, len(s.Grid))}
		ta.reset(fp)
		base, ok := scoreTarget(cfg, s, r.Target, []*tally{ta}, cfg.Mosaics(), nil, &c, 0)
		if !ok {
			return Estimate{}
		}
		if _, ok := ta.result(cfg, s, base); !ok {
			return Estimate{}
		}
		obs = ta.obs
	}
	if planned != nil {
		in := make([]bool, len(obs))
		for i, up := range obs {
			in[i] = up && i < len(planned) && planned[i]
		}
		obs = in
	}
	lines := r.Photons()
	chans := c.channels(nil, r.Filter, cfg.FilterNM, lines, fp)
	if !slices.ContainsFunc(chans, func(ch channel) bool { return ch.obj > 0 }) {
		return Estimate{}
	}
	// Unfiltered, an emission-line object's lines are dimmed at their own
	// wavelengths, as scoreTarget dims them (0, 0: V's, for a continuum).
	var lineRay, lineAer float64
	if catalog.EmissionLine(r.Target) {
		all := broadPasses(r.Target, lines, cameraBand)
		lineRay, lineAer = all.ray-1, all.aer-1
	}
	objNL := lightNL(r.Target, sb)
	ax, bx := axesArcsec(r.Target)
	ra, dec, extra := where(s, r.Target)
	hz := astro.NewHorizontal(cfg.Lat, dec)
	unit := astro.Unit(ra, dec)
	var st stack
	for i, up := range obs {
		if !up {
			continue
		}
		a := math.Asin(hz.SinAlt(s.LST[i]-ra)) / deg
		rho := 180.0
		if s.MoonAlt[i] > 0 {
			m := &s.MoonUnit[i]
			rho = math.Acos(max(-1, min(1, unit[0]*m[0]+unit[1]*m[1]+unit[2]*m[2]))) / deg
		}
		// The sky's parts and the object as scoreTarget sets them, in
		// absolute photons: the object's lines are not moved against the
		// camera band, which the channels' sky gains already span, and what
		// a filter passes of it is in the channels' object gains. The object
		// is catalogued above the atmosphere, so the zenith's extinction dims
		// it too (atmos.Absolute); the sky is measured on the ground, but
		// for its zodiacal and galactic excess, taken above the atmosphere.
		air := atmos.Airmass(a)
		glow, rayleigh, mie := atmos.SkyParts(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i])
		glow += extra * atmos.Absolute(air, s.Ext[i])
		var obj float64
		if fp.w > 0 {
			obj = objNL * atmos.Absolute(air, s.ExtRayleigh[i]*fp.ray+s.ozone(i)+s.ExtAerosol[i]*fp.aer)
		} else {
			obj = objNL * atmos.Absolute(air, s.Ext[i]+s.ExtRayleigh[i]*lineRay+s.ExtAerosol[i]*lineAer)
		}
		// The weather loses the minute's subs in proportion, so it weighs
		// that minute's SNR², not the night's mean: cloud during the best
		// minutes costs more than the same cloud during the worst.
		c.add(&st, chans, i, s.Quality[i], obj*c.blur(i, air, ax, bx), glow, rayleigh, mie)
	}
	// A night the weather loses entirely still has its observable time: the
	// counts hit maxSubs, past Max, rather than reading as no estimate.
	if st.n == 0 {
		return Estimate{}
	}
	// In the levels' units: a faint object's levels are lower (levelScale),
	// which reads as a higher SNR² against the same levels.
	m, ceil := c.rates(&st, chans)
	scale := levelScale(r.Target, sb, s.RefNL)
	m, ceil = m/scale, ceil/scale
	panels := max(1, r.Mosaic.Panels())
	minutes := float64(st.n)
	var limit float64
	if !math.IsInf(ceil, 1) {
		limit = math.Sqrt(ceil)
	}

	return Estimate{
		Modest: need(Levels[0], m, ceil), Good: need(Levels[1], m, ceil), Best: need(Levels[2], m, ceil), SNRLimit: limit,
		Max: int(minutes*60/(c.sub+subGap)*(1-cfg.Reject)) / panels, Sub: c.sub, SNRPerSub: math.Sqrt(m),
		Panels: panels, Assumed: assumed,
	}
}
