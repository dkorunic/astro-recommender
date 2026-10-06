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

func TestParseCometsBadDate(t *testing.T) {
	// 12P's line with the perihelion month (cols 19-21) or day (22-29) out of range.
	line := "0012P         2024 04 21.2039  0.781900  0.954747  199.0349  255.8675   74.1483  20261004   5.0  6.0  12P/Pons-Brooks                                          MPC194198"
	for _, bad := range []string{line[:19] + "13" + line[21:], line[:19] + "00" + line[21:], line[:22] + "9999999" + line[29:], line[:22] + "00.0000" + line[29:]} {
		if cs, err := parseComets([]byte(bad)); err == nil {
			t.Errorf("parseComets(%q) = %v, want no elements", bad[14:29], cs)
		}
	}
}

func TestTrackNaN(t *testing.T) {
	// a = q/(e-1) underflows a^1.5 to 0, so the mean anomaly is infinite.
	c := comet{q: 1e-300, e: 2}
	grid := []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
	if c.track(grid, make([][2]float64, len(grid))) {
		t.Error("track with an unsolvable orbit = true, want false")
	}
}
