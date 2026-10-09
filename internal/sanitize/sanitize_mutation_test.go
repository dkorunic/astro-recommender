// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package sanitize

import "testing"

func TestMutText(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b[31mb":         "a[31mb",
		"x\u009b2Jy":         "x2Jy",
		"bad\x9bbyte":        "badbyte",
		"rev‮ersed":          "reversed",
		"del\x7f":            "del",
		"Zagreb – Črnomerec": "Zagreb – Črnomerec",
		"tab\tnl\n":          "tabnl",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}
