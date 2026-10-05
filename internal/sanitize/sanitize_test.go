// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package sanitize

import "testing"

func TestSanitize(t *testing.T) {
	// sanitize strips ESC, 8-bit CSI, BEL and newlines but keeps Unicode text.
	if got := Text("Zagreb\x1b]0;pwned\x07\x9b2J\nčćž °"); got != "Zagreb]0;pwned2Jčćž °" {
		t.Errorf("sanitize = %q", got)
	}
}

// Bidi overrides and isolates must not reach the terminal.
func TestTextBidi(t *testing.T) {
	if got := Text("M 31\u202e\u2066 Andromeda\u2069"); got != "M 31 Andromeda" {
		t.Errorf("Text = %q", got)
	}
}
