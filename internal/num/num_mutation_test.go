// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package num

import (
	"math"
	"testing"
)

func TestMutFinite(t *testing.T) {
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if Finite(x) {
			t.Errorf("Finite(%v) = true", x)
		}
	}
	for _, x := range []float64{0, -1e308, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		if !Finite(x) {
			t.Errorf("Finite(%v) = false", x)
		}
	}
}
