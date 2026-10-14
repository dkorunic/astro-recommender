// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

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

// Levels are the stacked SNR per arcsec², at the object's mean surface
// brightness, of a modest, good and best image.
var Levels = [3]float64{5, 10, 20}

// photonsV0 is a V = 0 source's photon flux over the V band, photons s⁻¹
// cm⁻²: 1005 per Å at 550 nm (Bessell 1998) over atmos.VWidth. The sky
// model's nanoLamberts are V-band, so x nL is photonsV0·x/NanoLamberts(0)
// per arcsec².
const photonsV0 = 1005 * 10 * atmos.VWidth

// lineQE is the IMX678's response at each line relative to its peak, read
// off Sony's curve; lineChannel the colour sensor's channel of pixels whose
// dye passes it (red Hα and [S II], green [O III] and Hβ), and channelShare
// each channel's share of the array (RGGB).
var (
	lineQE       = [4]float64{catalog.Ha: 0.95, catalog.OIII: 0.85, catalog.SII: 0.9, catalog.Hb: 0.8}
	lineChannel  = [4]int{catalog.Ha: red, catalog.OIII: green, catalog.SII: red, catalog.Hb: green}
	channelShare = [3]float64{red: 0.25, green: 0.5, blue: 0.25}
)

// A colour sensor's channels.
const (
	red = iota
	green
	blue
)

// broadQEMono and broadQEColour are the response over atmos.CameraLo-CameraHi
// relative to the peak: a mono sensor's mean, and a colour one's, where each
// pixel's dye passes about a third of the band (the RGGB mean of Sony's
// colour curves).
const broadQEMono, broadQEColour = 0.8, 0.34

// maxSubs caps a count, so an absurdly faint object cannot overflow an int.
const maxSubs = 1_000_000_000

// Estimate is one result's sub-exposure estimate; the zero value (Good 0)
// means none.
type Estimate struct { // betteralign:ignore
	Modest    int     `json:"modest"`                             // subs to shoot for Levels[0], per panel
	Good      int     `json:"good"`                               // Levels[1]
	Best      int     `json:"best"`                               // Levels[2]
	Max       int     `json:"max"`                                // subs tonight's time allows per panel (with -plan, its blocks)
	Sub       float64 `json:"sub"`                                // sub length, s
	SNRPerSub float64 `json:"snrPerSub"`                          // per arcsec², √ of the mean SNR² the stack adds, weather included
	Panels    int     `json:"panels"`                             // the mosaic the counts are per panel of
	Assumed   bool    `json:"assumedSurfaceBrightness,omitempty"` // the surface brightness is its class's typical one
}

// camera is what the estimate needs of the telescope and camera.
type camera struct {
	area, qe, readNoise, sub, omega float64 // cm², peak, e⁻, s, arcsec² per pixel
	mono                            bool
}

// newCamera returns cfg's camera; false without framing or with a
// hand-built Config that does not describe one.
func newCamera(cfg *config.Config) (camera, bool) {
	if !cfg.Framing || cfg.Aperture <= cfg.Obstruction || cfg.Sub <= 0 || cfg.QE <= 0 || cfg.Scale <= 0 {
		return camera{}, false
	}
	d, o := cfg.Aperture/10, cfg.Obstruction/10

	return camera{
		area: math.Pi / 4 * (d*d - o*o), qe: cfg.QE, readNoise: cfg.ReadNoise, sub: cfg.Sub,
		omega: cfg.Scale * cfg.Scale, mono: cfg.Mono,
	}, true
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
// an object without emission lines (lines zero, Target.Lines), are
// continuum: unfiltered and broadband spread over the camera band, the glow
// flat per nm and the moonlight as it scatters (atmos.BandMean against V's),
// at the band's mean response, which each of a colour sensor's dyes passes
// alike (about a third of the band), through a broadband filter its
// passbands' shares (fp). Through a line filter each band passes its own
// share of the sky (atmos.BandSky, against V) at its own line's response, to
// the channel whose dye passes the line. An emission-line object's light is
// its lines wherever the band passes them, collected once, each at its
// response by the channel that sees it: through Hα a colour sensor's green
// and blue pixels see nothing, and add nothing, their read noise included.
// The object's gains are 0 when a line filter passes none of its lines.
func (c camera) channels(name string, width float64, lines [4]float64, fp filterPass) []channel {
	g := c.area * c.qe * c.omega * photonsV0 / atmos.NanoLamberts(0)
	chans := []channel{{share: 1}}
	if !c.mono {
		chans = []channel{{share: channelShare[red]}, {share: channelShare[green]}, {share: channelShare[blue]}}
	}
	at := func(l catalog.Line) *channel {
		if c.mono {
			return &chans[0]
		}

		return &chans[lineChannel[l]]
	}
	emission := lines != [4]float64{}
	if bands := config.Filters[name]; bands != nil {
		for _, l := range bands {
			glow, rayleigh, mie := atmos.BandSky(catalog.LineNM[l], width)
			ch, r := at(l), g*lineQE[l]
			ch.sky[0] += glow * r
			ch.sky[1] += rayleigh * r
			ch.sky[2] += mie * r
			ch.obj += lines[l] * r
		}

		return chans
	}
	qe := broadQEColour
	if c.mono {
		qe = broadQEMono
	}
	cont := g * qe * atmos.CameraBand
	shares := [3]float64{1, 1, 1}
	if fp.w > 0 {
		shares = [3]float64{fp.glow, fp.rayleigh, fp.mie}
	}
	ray, aer := atmos.BandMean(atmos.CameraLo, atmos.CameraHi)
	for i := range chans {
		chans[i].sky = [3]float64{cont * shares[0], cont * ray * shares[1], cont * aer * shares[2]}
		if !emission {
			// A continuum object through a broadband filter: its passbands' share.
			chans[i].obj = cont * cmp.Or(fp.w, 1)
		}
	}
	if emission {
		for l, share := range lines {
			if passes := config.Broadband[name]; passes == nil || inBands(catalog.LineNM[l], passes) {
				at(catalog.Line(l)).obj += share * g * lineQE[l]
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

// perSub is one sub's SNR² per arcsec² for object and sky rates in e⁻/s
// per pixel: S²/(S + B + RN²) over the sub, a pixel holding omega arcsec².
// Dark current is left out.
func (c camera) perSub(obj, sky float64) float64 {
	sig := obj * c.sub

	return sig * sig / (sig + sky*c.sub + c.readNoise*c.readNoise) / c.omega
}

// minuteSNR2 is one minute's per-sub SNR² per arcsec² over chans: the
// object's light obj (nanoLamberts, absolute, dimmed by its extinction)
// under a sky of glow, rayleigh and mie; scoring chooses a filter by it, so
// it must stay Subs' own.
func (c camera) minuteSNR2(chans []channel, obj, glow, rayleigh, mie float64) float64 {
	var snr2 float64
	for _, ch := range chans {
		if ch.obj > 0 {
			snr2 += ch.share * c.perSub(ch.obj*obj, ch.sky[0]*glow+ch.sky[1]*rayleigh+ch.sky[2]*mie)
		}
	}

	return snr2
}

// Subs estimates the subs r needs tonight through r.Filter for each of
// Levels: an optimally weighted stack adds the per-sub SNR², so the mean
// over r's observable minutes (r.Obs; rescored when nil), each weighted by
// the share of its subs the weather (Quality) lets through, sets the count.
// Counts are per panel of r.Mosaic. planned is the time -plan gives r (0: its observable
// time), which with the panels sets Max. The zero Estimate means none: no
// camera, no surface brightness (catalog.Target.ExposureSB), or no
// observable minute; a night the weather loses entirely is maxSubs counts
// with SNRPerSub 0.
func Subs(cfg *config.Config, s *Sky, r Result, planned time.Duration) Estimate {
	c, ok := newCamera(cfg)
	sb, assumed := r.ExposureSB()
	if !ok || sb <= 0 {
		return Estimate{}
	}
	obs := r.Obs
	if obs == nil {
		rs := Score(cfg, s, []catalog.Target{r.Target}, true)
		if len(rs) != 1 {
			return Estimate{}
		}
		obs = rs[0].Obs
	}
	fp := passes(cfg, r.Target, r.Filter)
	lines := r.Lines()
	chans := c.channels(r.Filter, cfg.FilterNM, lines, fp)
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
	ra, dec, extra := where(s, r.Target)
	hz := astro.NewHorizontal(cfg.Lat, dec)
	unit := astro.Unit(ra, dec)
	var snr2 float64
	n := 0
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
		// a filter passes of it is in the channels' object gains.
		air := atmos.Airmass(a)
		ext := atmos.Transmission(air, s.Ext[i])
		glow, rayleigh, mie := atmos.SkyParts(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i])
		glow += extra * ext
		var obj float64
		if fp.w > 0 {
			obj = objNL * atmos.Transmission(air, s.ExtRayleigh[i]*fp.ray+atmos.OzoneExtinction+s.ExtAerosol[i]*fp.aer)
		} else {
			obj = objNL * atmos.Transmission(air, s.Ext[i]+s.ExtRayleigh[i]*lineRay+s.ExtAerosol[i]*lineAer)
		}
		// The weather loses the minute's subs in proportion, so it weighs
		// that minute's SNR², not the night's mean: cloud during the best
		// minutes costs more than the same cloud during the worst.
		snr2 += s.Quality[i] * c.minuteSNR2(chans, obj, glow, rayleigh, mie)
		n++
	}
	// A night the weather loses entirely still has its observable time: the
	// counts hit maxSubs, past Max, rather than reading as no estimate.
	if n == 0 {
		return Estimate{}
	}
	mean := snr2 / float64(n)
	panels := max(1, r.Mosaic.Panels())
	minutes := float64(n)
	if planned > 0 {
		minutes = planned.Minutes()
	}
	need := func(level float64) int { return int(min(math.Ceil(level*level/mean), maxSubs)) }

	return Estimate{
		Modest: need(Levels[0]), Good: need(Levels[1]), Best: need(Levels[2]),
		Max: int(minutes*60/c.sub) / panels, Sub: c.sub, SNRPerSub: math.Sqrt(mean),
		Panels: panels, Assumed: assumed,
	}
}
