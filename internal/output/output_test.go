// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"slices"
	"testing"
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

// position rounds on the whole value so minutes never print as 60.
func TestPosition(t *testing.T) {
	for _, c := range []struct {
		ra, dec         float64
		wantRA, wantDec string
	}{
		{13.0 * 15, 40.25, "13 00.0", "+40 15"},
		{23.99999 * 15, -0.004, "00 00.0", "+00 00"},
		{12.99999 * 15, -12.9999, "13 00.0", "-13 00"},
		{0, -90, "00 00.0", "-90 00"},
	} {
		ra, dec := position(c.ra, c.dec)
		if ra != c.wantRA || dec != c.wantDec {
			t.Errorf("position(%v, %v) = %q %q, want %q %q", c.ra, c.dec, ra, dec, c.wantRA, c.wantDec)
		}
	}
}

func TestWrap(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  []string
	}{
		{"mag/arcsec² sky; - unknown, scored", 17, []string{"mag/arcsec² sky;", "- unknown, scored"}},
		{"size in arcminutes (- unknown)", 21, []string{"size in arcminutes", "(- unknown)"}},
		{"used (~ estimated)", 7, []string{"used", "(~ estimated)"}},
	} {
		if got := wrap(tc.text, tc.width); !slices.Equal(got, tc.want) {
			t.Errorf("wrap(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
}
