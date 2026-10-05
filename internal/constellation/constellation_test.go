// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package constellation

import "testing"

func TestConstellationOf(t *testing.T) {
	// constellationOf: known objects (J2000), including the pole-encircling
	// Ursa Minor, Octans at the south pole and both halves of Serpens.
	for _, c := range []struct {
		ra, dec float64
		want    string
	}{
		{10.6847, 41.2690, "Andromeda"},   // M31
		{37.9529, 89.2641, "Ursa Minor"},  // Polaris
		{180, 89.9, "Ursa Minor"},         // near the north pole
		{0, -89.9, "Octans"},              // near the south pole
		{355.9417, -15.2786, "Aquarius"},  // R Aquarii (GaryImm says Aquila)
		{274.7000, -13.8167, "Serpens"},   // M16, Serpens Cauda
		{229.6375, 2.0811, "Serpens"},     // M5, Serpens Caput
		{83.8221, -5.3911, "Orion"},       // M42
		{201.3651, -43.0191, "Centaurus"}, // Centaurus A
	} {
		if got := Of(c.ra, c.dec); got != c.want {
			t.Errorf("constellationOf(%v, %v) = %s, want %s", c.ra, c.dec, got, c.want)
		}
	}
}
