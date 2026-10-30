// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package catalog

import (
	"syscall/js"
	"testing"
)

// setLocation gives Node the page location a browser has.
func setLocation(href string) {
	js.Global().Set("location", map[string]any{"href": href})
}

// The browser build fetches the lists it does not embed, gzipped or plain
// (a server's Content-Encoding unzips them first), and reads both.
func TestListFetched(t *testing.T) {
	for _, name := range []string{"Messier", "Caldwell"} { // gzipped, plain
		if tg, got, err := Load(name, ""); err != nil || got != name || len(tg) == 0 {
			t.Errorf("Load(%s) = %d targets %q %v", name, len(tg), got, err)
		}
	}
	if listsPlain.Load() == 0 || listsGzipped.Load() == 0 {
		t.Errorf("served %d plain and %d gzipped: want both read", listsPlain.Load(), listsGzipped.Load())
	}
}
