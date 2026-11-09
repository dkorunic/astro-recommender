// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

// earthRate is the Earth's sidereal rotation, radians per second.
const earthRate = 7.2921150e-5

// rotation is the field rotation of an alt-az mount for one target: the
// field turns about the frame's centre at earthRate cos φ cos A / cos h
// (Meeus ch. 14's parallactic angle, differentiated), and with
// cos h cos A = sin δ cos φ − cos δ sin φ cos H that needs no azimuth. The
// zero value checks nothing (an equatorial mount, no framing, a comet, an
// object without a size).
type rotation struct {
	seeing                         []float64 // Sky.Seeing; nil: typicalSeeing
	k                              float64   // earthRate × cos φ × sub × the radius in px: the trail, px, per unit of rate's factor
	sinDec, cosDec, sinLat, cosLat float64
	optics2, scale2                float64 // the optics' and pixels' blur, FWHM² arcsec²; the scale², arcsec²/px²
}

// newRotation returns the check for tg at declination dec (of date) on
// cfg's alt-az mount: the trail is taken at the object's edge, its major
// axis' half (its core's, catalog.Target.FrameAxes), which a frame's corner caps (a mosaic's panel's, each turning
// about its own centre). An object without a size is not checked: the
// corner would judge it stricter than any object of known size.
func newRotation(cfg *config.Config, s *Sky, tg catalog.Target, dec float64) rotation {
	if !cfg.Framing || !cfg.AltAz || tg.Track != nil || !tg.HasSize() || cfg.Sub <= 0 || cfg.Scale <= 0 {
		return rotation{}
	}
	major, _ := tg.FrameAxes()                                      // a core's, when the frame holds that
	radius := min(math.Hypot(cfg.FOVLong, cfg.FOVShort)/2, major/2) // arcminutes
	sinLat, cosLat := math.Sincos(cfg.Lat * deg)
	sinDec, cosDec := math.Sincos(dec * deg)
	// Without -aperture or -scope the aperture is the Origin's,
	// whose diffraction says nothing of this telescope's: the pixels'
	// blur alone, under the seeing that dominates the star's width anyway.
	optics2 := opticsFWHM2(cfg)
	if !cfg.ApertureSet {
		px := pixelFWHM * cfg.Scale
		optics2 = px * px
	}

	return rotation{
		seeing: s.Seeing, k: earthRate * cosLat * cfg.Sub * radius * 60 / cfg.Scale,
		sinDec: sinDec, cosDec: cosDec, sinLat: sinLat, cosLat: cosLat,
		optics2: optics2, scale2: cfg.Scale * cfg.Scale,
	}
}

// trailed reports whether minute i's sub, at altitude a (sine sinAlt) and
// hour angle of cosine cosHA, trails the object's edge by more than a
// star's width: the FWHM of the seeing (as X^0.6), the optics and the
// pixels, as camera.blur takes it.
func (r *rotation) trailed(i int, a, sinAlt, cosHA float64) bool {
	return r.k != 0 && r.trail(i, a, sinAlt, cosHA)
}

// trail is trailed's check, kept out of line so that trailed inlines.
func (r *rotation) trail(i int, a, sinAlt, cosHA float64) bool {
	// At the zenith the rotation is unbounded: there 0/0 would be NaN, which
	// compares false and passed the minute.
	cos2 := 1 - sinAlt*sinAlt
	if cos2 <= 0 {
		return true
	}
	trail := r.k * (r.sinDec*r.cosLat - r.cosDec*r.sinLat*cosHA) / cos2
	see := typicalSeeing
	if r.seeing != nil {
		see = r.seeing[i]
	}
	// X^0.6 is at least 1 and only widens the star, so a trail within the
	// zenith's width needs no airmass: most minutes, where the Pow and the
	// Airmass were 14% of a framed alt-az run.
	t2 := trail * trail * r.scale2
	if t2 <= see*see+r.optics2 {
		return false
	}
	see *= math.Pow(atmos.Airmass(a), 0.6)

	return t2 > see*see+r.optics2
}

// opticsFWHM2 is cfg's optics' and pixels' blur, FWHM² in arcsec²: the
// diffraction FWHM, 1.03 λ/D at 550 nm, and a pixel's (pixelFWHM) in
// quadrature, to which the seeing's is added.
func opticsFWHM2(cfg *config.Config) float64 {
	diff := diffraction(cfg)
	px := pixelFWHM * cfg.Scale

	return diff*diff + px*px
}

// diffraction is the optics' FWHM in arcsec at 550 nm, 1.03 λ/D.
func diffraction(cfg *config.Config) float64 {
	return 1.03 * 550e-9 / (cfg.Aperture / 1000) * 206265
}

// Sampling is how a frame's pixels meet the night's stars.
type Sampling struct {
	Verdict string  // "well sampled" at 2-3 pixels across a star
	FWHM    float64 // the star's, arcsec
	Pixels  float64 // pixels across it, to the 0.1 shown, which the verdict judges
}

// Sampling judges cfg's pixel scale against the night's mean zenith seeing
// (typicalSeeing without one) and the diffraction of a known aperture. The
// verdict is the usual 2-3 pixels per FWHM.
func (s *Sky) Sampling(cfg *config.Config) Sampling {
	see := typicalSeeing
	if len(s.Seeing) > 0 {
		sum := 0.0
		for _, v := range s.Seeing {
			sum += v
		}
		see = sum / float64(len(s.Seeing))
	}
	fwhm := see
	if cfg.ApertureSet && cfg.Aperture > 0 {
		fwhm = math.Hypot(see, diffraction(cfg))
	}
	// Rounded before the verdict, so 1.96 does not print as an undersampled 2.0.
	r := Sampling{FWHM: fwhm, Pixels: math.Round(fwhm/cfg.Scale*10) / 10}
	switch {
	case r.Pixels < 1:
		r.Verdict = "severely undersampled"
	case r.Pixels < 2:
		r.Verdict = "undersampled"
	case r.Pixels <= 3:
		r.Verdict = "well sampled"
	case r.Pixels <= 5:
		r.Verdict = "oversampled"
	default:
		r.Verdict = "severely oversampled"
	}

	return r
}
