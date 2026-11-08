// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package dss

import "testing"

func TestFOV(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ size, want float64 }{
		{0, 0.5},  // unknown
		{1, 0.1},  // the floor
		{40, 1},   // half again the object
		{1000, 5}, // the ceiling
	} {
		if got := FOV(c.size); got != c.want {
			t.Errorf("FOV(%v) = %v, want %v", c.size, got, c.want)
		}
	}
}

func TestAladin(t *testing.T) {
	t.Parallel()

	const want = "https://aladin.cds.unistra.fr/AladinLite/?target=10.6847%20-41.2687&fov=1.00&survey=CDS%2FP%2FDSS2%2Fcolor"
	if got := Aladin(10.6847, -41.2687, 40); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
