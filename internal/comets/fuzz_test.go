// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package comets

import (
	"math"
	"testing"
	"time"
)

// FuzzParseComets: any MPC text either fails to parse or gives elements
// whose kept comets have finite tracks, which scoring indexes per minute.
func FuzzParseComets(f *testing.F) {
	f.Add("    CK25A010  2025 03 14.1234  0.123456  1.000500  123.4567  234.5678   12.3456  20250101  10.0  4.0  C/2025 A1 (Test)                                         MPEC 2025-A01")
	f.Add("0001P         1986 02 09.4589  0.585978  0.967142  111.8657   58.8601  162.2422  20260101   4.0  6.0  1P/Halley                                                 98, 1083")
	f.Fuzz(func(t *testing.T, s string) {
		els, err := parseComets([]byte(s))
		if err != nil {
			return
		}
		grid := []time.Time{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)}
		for _, tg := range els.Targets(grid, 99, nil) {
			if len(tg.Track) != len(grid) {
				t.Fatalf("%s: track of %d for %d minutes", tg.Name, len(tg.Track), len(grid))
			}
			for _, p := range tg.Track {
				if math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
					t.Fatalf("%s: track point %v", tg.Name, p)
				}
			}
		}
	})
}
