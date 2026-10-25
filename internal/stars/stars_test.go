// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package stars

import (
	"math"
	"testing"
)

func TestBrightest(t *testing.T) {
	for _, c := range []struct {
		name     string
		ra, dec  float64 // J2000 degrees
		radius   float64
		star     string
		sep      float64 // degrees, ±0.01
		mustFind bool
	}{
		{"NGC 2024, the Flame", 85.42, -1.85, 0.75, "ζ Ori", 0.25, true},
		{"NGC 404, Mirach's Ghost", 17.36, 35.72, 0.75, "β And", 0.11, true},
		{"the Flame past ζ Ori's reach", 85.42, -1.85, 0.2, "", 0, false},
		// Within 3° of both Sirius and Mirzam (β CMa, 2.0): the brighter wins.
		{"between Sirius and Mirzam", 98.5, -17.4, 3, "α CMa", 2.75, true},
	} {
		s, sep, ok := Brightest(c.ra, c.dec, c.radius)
		if ok != c.mustFind || ok && (s.Name != c.star || math.Abs(sep-c.sep) > 0.01) {
			t.Errorf("%s: %q %.3f° %v, want %q %.2f° %v", c.name, s.Name, sep, ok, c.star, c.sep, c.mustFind)
		}
	}
	for i := 1; i < len(bright); i++ {
		if bright[i].Mag < bright[i-1].Mag {
			t.Fatalf("%s (%v) after %s (%v): not brightest first", bright[i].Name, bright[i].Mag, bright[i-1].Name, bright[i-1].Mag)
		}
	}
}
