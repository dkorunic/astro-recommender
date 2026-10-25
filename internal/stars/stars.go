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

// maxMag is the faintest star or planet named, V: scripts/bsc2go.py's MAX_MAG.
const maxMag = 4.0

// Star is a bright star: its name (the Bayer letter, else the Flamsteed
// number, with the constellation; else HR N), V magnitude and J2000
// position in degrees.
type Star struct {
	Name    string
	Mag     float64
	RA, Dec float64
}

// Brightest returns the brightest of bright and planets (Planets at the
// time wanted) within radius degrees of ra, dec (J2000 degrees) and its
// separation in degrees, false when none is.
func Brightest(ra, dec, radius float64, planets []Star) (Star, float64, bool) {
	u, cosR := astro.Unit(ra, dec), math.Cos(radius*deg)
	var best Star
	sep, found := 0.0, false
	// near takes s when it is within reach and the brightest yet.
	near := func(s Star) bool {
		v := astro.Unit(s.RA, s.Dec)
		c := u[0]*v[0] + u[1]*v[1] + u[2]*v[2]
		if c < cosR || found && s.Mag >= best.Mag {
			return false
		}
		best, sep, found = s, math.Acos(min(1, c))/deg, true

		return true
	}
	for _, p := range planets {
		near(p)
	}
	// bright is brightest first: past the best so far, or once one is
	// taken, none can do better.
	for _, s := range bright {
		if found && s.Mag >= best.Mag || near(s) {
			break
		}
	}

	return best, sep, found
}
