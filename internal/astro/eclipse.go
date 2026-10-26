// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-FileCopyrightText: 2019-2023 Don Cross <cosinekitty@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"time"
)

// EclipsePhase is how deep the Moon is in the Earth's shadow.
type EclipsePhase int8

// The phases of a lunar eclipse, as Astronomy Engine's SearchLunarEclipse
// tells them apart: some of the Moon in the penumbra, some in the umbra, all
// of it in the umbra.
const (
	NoEclipse EclipsePhase = iota
	Penumbral
	Partial
	Total
)

// Eclipse is the Moon against the Earth's shadow at a moment: Light is the
// share of its light the shadow leaves, 1 outside it.
type Eclipse struct {
	Light float64
	Phase EclipsePhase
}

// The shadow's geometry is Astronomy Engine's (_CalcShadow, MIT): the
// Earth's mean radius and 88 km of atmosphere cast it, the Sun's radius and
// the Moon's mean radius in km.
const (
	sunRadiusKm    = 695700.0
	earthShadowKm  = 6371.0 + 88
	moonRadiusKm   = 1737.4
	earthRadiusKm  = 6378.1366 // equatorial, moon's distance unit
	auKm           = 1.4959787069098932e8
	umbraLight     = 1e-4 // ponytail: flat, a bright totality's (Danjon L = 4, about 10 mag down); the umbra's gradient and the Danjon scale left out
	eclipseRings   = 8    // rings of equal area the Moon's disc is sampled on
	eclipseSamples = 4    // points on the innermost ring, 2j + 1 times that on ring j
)

// LunarEclipse returns the Moon's place in the Earth's shadow at t. The light
// is the mean over the Moon's disc, taken as uniformly bright (near full it
// is, the opposition surge aside) and sampled on eclipseRings rings, of the
// share of the Sun's disc each point sees past the Earth's, the solar limb
// darkening left out; in the umbra, where it sees none, umbraLight, the
// sunlight the Earth's atmosphere refracts into the shadow.
func LunarEclipse(t time.Time) Eclipse {
	n := DaysJ2000(t)
	g := (357.528 + 0.9856003*n) * deg
	sunKm := (1.00014 - 0.01671*math.Cos(g) - 0.00014*math.Cos(2*g)) * auKm
	sun := Unit(SunRADec(t))
	ra, dec, dist := moon(t)
	m := Unit(ra, dec)
	moonKm := dist * earthRadiusKm
	// The shadow's axis runs from the Sun through the Earth's centre; the
	// Moon's plane across it lies along km beyond the Earth, u of the Sun's
	// distance, and the Moon's centre r km off the axis.
	along := -moonKm * (m[0]*sun[0] + m[1]*sun[1] + m[2]*sun[2])
	u := along / sunKm
	r := math.Sqrt(max(0, moonKm*moonKm-along*along))
	umbra := sunRadiusKm - (1+u)*(sunRadiusKm-earthShadowKm)
	penumbra := -sunRadiusKm + (1+u)*(sunRadiusKm+earthShadowKm)
	if along <= 0 || r >= penumbra+moonRadiusKm {
		return Eclipse{Light: 1}
	}
	e := Eclipse{Phase: Penumbral}
	switch {
	case r+moonRadiusKm < umbra:
		e.Phase = Total
	case r < umbra+moonRadiusKm:
		e.Phase = Partial
	}
	// Seen from a point c km off the axis in the Moon's plane, the Sun's and
	// the Earth's discs scale to radii (penumbra ∓ umbra)/2 with their
	// centres c apart: they touch at the penumbra's edge and the Earth's
	// covers the Sun's within the umbra.
	sunDisc, earthDisc := (penumbra-umbra)/2, (penumbra+umbra)/2
	for j := range eclipseRings {
		rho := moonRadiusKm * math.Sqrt((float64(j)+0.5)/eclipseRings)
		points := eclipseSamples * (2*j + 1)
		for p := range points {
			sinP, cosP := math.Sincos(2 * math.Pi * (float64(p) + 0.5) / float64(points))
			c := math.Hypot(r+rho*cosP, rho*sinP)
			e.Light += max(umbraLight, 1-covered(sunDisc, earthDisc, c)) / float64(eclipseRings*points)
		}
	}

	return e
}

// covered is the share of a disc of radius a that one of radius b covers
// with their centres c apart (Astronomy Engine's _Obscuration).
func covered(a, b, c float64) float64 {
	if c >= a+b {
		return 0
	}
	x := (a*a - b*b + c*c) / (2 * c)
	radicand := a*a - x*x
	if c == 0 || radicand <= 0 { // one disc inside the other
		if a <= b {
			return 1
		}

		return b * b / (a * a)
	}
	y := math.Sqrt(radicand)
	// The cosines are clamped: near tangency rounding can carry one a hair
	// past ±1 while the radicand is still positive, and Acos would be NaN.
	lens1 := a*a*math.Acos(max(-1, min(1, x/a))) - x*y
	lens2 := b*b*math.Acos(max(-1, min(1, (c-x)/b))) - (c-x)*y

	return (lens1 + lens2) / (math.Pi * a * a)
}
