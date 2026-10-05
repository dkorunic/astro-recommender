// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package comets

import (
	"math"
	"testing"
	"time"
)

func TestCometPosition(t *testing.T) {
	// Comets: 12P/Pons-Brooks (MPC line) at its 2024 perihelion against JPL
	// Horizons: RA 51.916°, Dec +9.927°, r 0.7808, Δ 1.6054 AU, T-mag 4.42.
	line := "0012P         2024 04 21.2039  0.781900  0.954747  199.0349  255.8675   74.1483  20261004   5.0  6.0  12P/Pons-Brooks                                          MPC194198"
	cs, err := parseComets([]byte(line))
	if err != nil || len(cs) != 1 {
		t.Fatalf("parseComets = %v, %v", cs, err)
	}
	ra, dec, r, delta := cs[0].position(time.Date(2024, 4, 21, 0, 0, 0, 0, time.UTC))
	mag := cs[0].h + 5*math.Log10(delta) + 2.5*cs[0].k*math.Log10(r)
	if math.Abs(ra-51.916) > 0.1 || math.Abs(dec-9.927) > 0.1 || math.Abs(delta-1.6054) > 0.005 || math.Abs(mag-4.42) > 0.05 {
		t.Errorf("12P = RA %.3f Dec %.3f r %.4f Δ %.4f mag %.2f", ra, dec, r, delta, mag)
	}
}
