// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package horizon

import (
	"cmp"
	"math"
	"slices"
	"testing"
)

// FuzzAt: any horizon of valid points, sorted as Load leaves it, gives an
// altitude within ±90° at every azimuth, across north and on vertical steps.
func FuzzAt(f *testing.F) {
	f.Add(0.0, 10.0, 360.0, 20.0, 180.0, 5.0, 359.5)
	f.Add(90.0, 30.0, 90.0, 60.0, 270.0, -5.0, 90.0)
	f.Fuzz(func(t *testing.T, a1, h1, a2, h2, a3, h3, az float64) {
		var h Horizon
		for _, p := range [][2]float64{{a1, h1}, {a2, h2}, {a3, h3}} {
			if p[0] >= 0 && p[0] <= 360 && p[1] >= -90 && p[1] <= 90 {
				h = append(h, p)
			}
		}
		slices.SortStableFunc(h, func(a, b [2]float64) int { return cmp.Compare(a[0], b[0]) })
		if !(az >= 0 && az < 360) {
			return
		}
		if v := h.At(az); math.IsNaN(v) || v < -90 || v > 90 {
			t.Fatalf("%v at %v = %v", h, az, v)
		}
	})
}
