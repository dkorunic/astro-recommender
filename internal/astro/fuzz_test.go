// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"testing"
)

// FuzzSexagesimal: whatever the text, a parse that succeeds is a finite
// number, so a catalogue or -ra/-dec typo cannot reach the scoring as NaN.
func FuzzSexagesimal(f *testing.F) {
	for _, s := range []string{"10 20 30", "-00 30 00", "+1", "1e400", "0x1p3 2", " - 1", "+-10", "12 60", "NaN", "Inf 0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if v, err := Sexagesimal(s); err == nil && (math.IsNaN(v) || math.IsInf(v, 0)) {
			t.Fatalf("Sexagesimal(%q) = %v", s, v)
		}
	})
}
