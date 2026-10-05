// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import "testing"

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
