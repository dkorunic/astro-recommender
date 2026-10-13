// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"math"
	"regexp"
	"testing"
)

var (
	csvRA  = regexp.MustCompile(`^([01]\d|2[0-3])h [0-5]\dm [0-5]\d\.\ds$`)
	csvDec = regexp.MustCompile(`^[+-]\d\d° [0-5]\d′ [0-5]\d″$`)
	colRA  = regexp.MustCompile(`^([01]\d|2[0-3]) [0-5]\d\.\d$`)
	colDec = regexp.MustCompile(`^[+-]\d\d [0-5]\d$`)
)

// FuzzCoordinates: the CSV's and the table's coordinate formats carry
// rounding into the next unit (never 60s, 60′ or 24h) for any position.
func FuzzCoordinates(f *testing.F) {
	for _, c := range [][2]float64{{359.99999, -0.0000001}, {0, 90}, {179.999999, -89.9999999}, {14.99999, 0}} {
		f.Add(c[0], c[1])
	}
	f.Fuzz(func(t *testing.T, ra, dec float64) {
		if !(ra >= 0 && ra < 360) || !(math.Abs(dec) <= 90) {
			return
		}
		if r, d := sexagesimal(ra, dec); !csvRA.MatchString(r) || !csvDec.MatchString(d) {
			t.Fatalf("sexagesimal(%v, %v) = %q, %q", ra, dec, r, d)
		}
		if r, d := position(ra, dec); !colRA.MatchString(r) || !colDec.MatchString(d) {
			t.Fatalf("position(%v, %v) = %q, %q", ra, dec, r, d)
		}
	})
}
