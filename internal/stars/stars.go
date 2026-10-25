// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package stars finds the bright stars near a target: in a small
// telescope's frame one throws halos, spikes and reflections across the
// object, which no sky model scores.
package stars

import (
	"math"

	"github.com/dkorunic/astro-recommender/internal/astro"
)

const deg = math.Pi / 180

// Star is a bright star: its name (the Bayer letter, else the Flamsteed
// number, with the constellation; else HR N), V magnitude and J2000
// position in degrees.
type Star struct {
	Name    string
	Mag     float64
	RA, Dec float64
}

// Brightest returns the brightest star of bright within radius degrees of
// ra, dec (J2000 degrees) and its separation in degrees, false when none is.
func Brightest(ra, dec, radius float64) (Star, float64, bool) {
	u, cosR := astro.Unit(ra, dec), math.Cos(radius*deg)
	for _, s := range bright { // brightest first
		v := astro.Unit(s.RA, s.Dec)
		if c := u[0]*v[0] + u[1]*v[1] + u[2]*v[2]; c >= cosR {
			return s, math.Acos(min(1, c)) / deg, true
		}
	}

	return Star{}, 0, false
}
