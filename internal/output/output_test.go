// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"testing"

	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// NO_COLOR and TERM=dumb win over CLICOLOR_FORCE.
func TestColorTerminal(t *testing.T) {
	for _, c := range []struct {
		noColor, term, force string
		want                 bool
	}{
		{"", "xterm", "1", true},
		{"", "xterm", "0", false}, // test stdout is not a terminal
		{"1", "xterm", "1", false},
		{"", "dumb", "1", false},
	} {
		t.Setenv("NO_COLOR", c.noColor)
		t.Setenv("TERM", c.term)
		t.Setenv("CLICOLOR_FORCE", c.force)
		if got := ColorTerminal(); got != c.want {
			t.Errorf("NO_COLOR=%q TERM=%q CLICOLOR_FORCE=%q: %v, want %v", c.noColor, c.term, c.force, got, c.want)
		}
	}
}

// position rounds on the whole value so minutes never print as 60, and
// comets show their mid-track position.
func TestPosition(t *testing.T) {
	for _, c := range []struct {
		tg     catalog.Target
		ra, de string
	}{
		{catalog.Target{RADeg: 13.0 * 15, DecDeg: 40.25}, "13 00.0", "+40 15"},
		{catalog.Target{RADeg: 23.99999 * 15, DecDeg: -0.004}, "00 00.0", "+00 00"},
		{catalog.Target{RADeg: 12.99999 * 15, DecDeg: -12.9999}, "13 00.0", "-13 00"},
		{catalog.Target{RADeg: 0, DecDeg: -90}, "00 00.0", "-90 00"},
		{catalog.Target{Track: [][2]float64{{0, 0}, {7.5, -12.5}, {30, 30}}}, "00 30.0", "-12 30"},
	} {
		ra, de := position(c.tg)
		if ra != c.ra || de != c.de {
			t.Errorf("position(%v, %v) = %q %q, want %q %q", c.tg.RADeg, c.tg.DecDeg, ra, de, c.ra, c.de)
		}
	}
}
