// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"slices"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/scoring"
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

// moonText and moonUp read the Moon's rises and sets off the per-minute
// altitudes; a Moon up (or down) throughout has none.
func TestMoon(t *testing.T) {
	start := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)
	s := &scoring.Sky{Start: start, End: start.Add(5 * time.Minute), Night: [2]time.Time{start, start.Add(5 * time.Minute)}}
	for i := range 5 {
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
	}
	for _, c := range []struct {
		alt  []float64
		text string
		up   []span
	}{
		{[]float64{1, 2, 3, 4, 5}, "up throughout", []span{{s.Grid[0], s.End}}},
		{[]float64{-1, -2, -3, -4, -5}, "never up", []span{}},
		{[]float64{1, 1, -1, -1, -1}, "sets 20:02", []span{{s.Grid[0], s.Grid[2]}}},
		{[]float64{-1, 1, 1, -1, 1}, "rises 20:01, sets 20:03, rises 20:04", []span{{s.Grid[1], s.Grid[3]}, {s.Grid[4], s.End}}},
	} {
		s.MoonAlt = c.alt
		if got := moonText(s); got != c.text {
			t.Errorf("moonText(%v) = %q, want %q", c.alt, got, c.text)
		}
		if got := moonUp(s); !slices.Equal(got, c.up) {
			t.Errorf("moonUp(%v) = %v, want %v", c.alt, got, c.up)
		}
	}
}
