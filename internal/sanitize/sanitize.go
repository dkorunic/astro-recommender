// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package sanitize strips terminal control characters from untrusted text.
package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text drops control characters (C0, DEL, C1 including ESC and CSI) and
// invalid UTF-8 from untrusted text, so it cannot inject terminal escape sequences when printed.
func Text(s string) string {
	return strings.Map(func(r rune) rune {
		// Invalid UTF-8 decodes to RuneError (a raw 0x9b byte is CSI on 8-bit
		// terminals); format characters (Cf) include bidi overrides like U+202E
		// that reorder how the rest of the line displays.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}

		return r
	}, s)
}
