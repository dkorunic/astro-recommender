// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import (
	"math"
	"testing"
)

// FuzzZodiacal: every finite direction, however far outside the table's
// axes, gives a positive finite brightness, so no target's sky turns NaN.
func FuzzZodiacal(f *testing.F) {
	for _, c := range [][2]float64{{0, 0}, {180, 90}, {-725, -95}, {1e300, -1e300}, {5, 7.5}} {
		f.Add(c[0], c[1])
	}
	f.Fuzz(func(t *testing.T, dLon, lat float64) {
		if math.IsNaN(dLon) || math.IsInf(dLon, 0) || math.IsNaN(lat) || math.IsInf(lat, 0) {
			return
		}
		if v := Zodiacal(dLon, lat); math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			t.Fatalf("Zodiacal(%v, %v) = %v", dLon, lat, v)
		}
	})
}
