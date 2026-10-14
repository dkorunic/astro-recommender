// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
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
// off Sony's curve; bayer the share of a colour sensor's pixels that see it
// (RGGB: red Hα and [S II], green [O III] and Hβ).
var (
	lineQE = [4]float64{catalog.Ha: 0.95, catalog.OIII: 0.85, catalog.SII: 0.9, catalog.Hb: 0.8}
	bayer  = [4]float64{catalog.Ha: 0.25, catalog.OIII: 0.5, catalog.SII: 0.25, catalog.Hb: 0.5}
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
	Modest    int     `json:"modest"`              // subs to shoot for Levels[0], per panel
	Good      int     `json:"good"`                // Levels[1]
	Best      int     `json:"best"`                // Levels[2]
	Max       int     `json:"max"`                 // subs tonight's time allows per panel (with -plan, its blocks)
	Sub       float64 `json:"sub"`                 // sub length, s
	SNRPerSub float64 `json:"snrPerSub"`           // per arcsec², √ of the mean SNR² the stack adds
	Panels    int     `json:"panels"`              // the mosaic the counts are per panel of
	Assumed   bool    `json:"assumedSB,omitempty"` // the surface brightness is its class's typical one
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

// allLines are the lines an unfiltered or broadband camera collects.
var allLines = []catalog.Line{catalog.Ha, catalog.OIII, catalog.SII, catalog.Hb}

// gains returns e⁻/s per pixel per nanoLambert of the object and of the sky
// through the -filter choice name ("" none). The sky, and an object without
// emission lines (lines zero, Target.Lines), are continuum: unfiltered and
// broadband the sky model's V-band shares spread over the camera band, flat
// per nm, at the band's mean response. A line filter's shares are already
// its bands' own, and an emission-line object's light is its lines wherever
// the band passes them, collected once: each line at its response and Bayer
// share, weighted by the object's light in it. The object's gain is 0 when a
// line filter passes none of its lines.
func (c camera) gains(name string, lines [4]float64) (obj, sky float64) {
	g := c.area * c.qe * c.omega * photonsV0 / atmos.NanoLamberts(0)
	line := func(ls []catalog.Line) float64 {
		var resp, w float64
		for _, l := range ls {
			b := bayer[l]
			if c.mono {
				b = 1
			}
			resp += lines[l] * lineQE[l] * b
			w += lines[l]
		}
		if w == 0 {
			return 0
		}

		return g * resp / w
	}
	if bands := config.Filters[name]; bands != nil {
		r := line(bands)

		return r, r
	}
	qe := broadQEColour
	if c.mono {
		qe = broadQEMono
	}
	sky = g * qe * (atmos.CameraHi - atmos.CameraLo) / atmos.VWidth
	if obj = line(allLines); obj == 0 {
		obj = sky
	}

	return obj, sky
}

// perSub is one sub's SNR² per arcsec² for object and sky rates in e⁻/s
// per pixel: S²/(S + B + RN²) over the sub, a pixel holding omega arcsec².
// Dark current is left out.
func (c camera) perSub(obj, sky float64) float64 {
	sig := obj * c.sub

	return sig * sig / (sig + sky*c.sub + c.readNoise*c.readNoise) / c.omega
}

// Subs estimates the subs r needs tonight through r.Filter for each of
// Levels: an optimally weighted stack adds the per-sub SNR², so the mean
// over r's observable minutes (r.Obs; rescored when nil) sets the count,
// divided by the mean weather quality, which loses subs. Counts are per
// panel of r.Mosaic. planned is the time -plan gives r (0: its observable
// time), which with the panels sets Max. The zero Estimate means none: no
// camera, no surface brightness (catalog.Target.ExposureSB), or no minute
// the target is up and the weather lets subs through.
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
	objGain, skyGain := c.gains(r.Filter, r.Lines())
	if objGain == 0 {
		return Estimate{}
	}
	objNL := atmos.NanoLamberts(sb)
	ra, dec, extra := where(s, r.Target)
	hz := astro.NewHorizontal(cfg.Lat, dec)
	unit := astro.Unit(ra, dec)
	var snr2, quality float64
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
		// The sky and the object as scoreTarget sets them against each other.
		air := atmos.Airmass(a)
		ext := atmos.Transmission(air, s.Ext[i])
		var obj, sky float64
		if fp.w > 0 {
			glow, rayleigh, mie := atmos.SkyParts(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i])
			glow += extra * ext
			sky = fp.glow*glow + fp.rayleigh*rayleigh + fp.mie*mie
			obj = fp.w * objNL * atmos.Transmission(air, s.ExtRayleigh[i]*fp.ray+atmos.OzoneExtinction+s.ExtAerosol[i]*fp.aer)
		} else {
			sky = atmos.SkyBrightnessLit(s.ZenithNL, s.Ext[i], a, rho, s.MoonLight[i]) + extra*ext
			obj = objNL * ext
		}
		snr2 += c.perSub(objGain*obj, skyGain*sky)
		quality += s.Quality[i]
		n++
	}
	if n == 0 || quality == 0 {
		return Estimate{}
	}
	mean, q := snr2/float64(n), quality/float64(n)
	panels := max(1, r.Mosaic.Panels())
	minutes := float64(n)
	if planned > 0 {
		minutes = planned.Minutes()
	}
	need := func(level float64) int { return int(min(math.Ceil(level*level/(mean*q)), maxSubs)) }

	return Estimate{
		Modest: need(Levels[0]), Good: need(Levels[1]), Best: need(Levels[2]),
		Max: int(minutes*60/c.sub) / panels, Sub: c.sub, SNRPerSub: math.Sqrt(mean),
		Panels: panels, Assumed: assumed,
	}
}
