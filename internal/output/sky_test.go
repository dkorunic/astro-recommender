// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// web/index.html's sky() scrapes each result row's RA, Dec and SIZE cells, as
// position() and sizeText format them between tabwriter's padding, with a
// regex read here from the page itself: every row must match it and the
// captures read back the row's position and size, the header must not, or
// the page's sky links and thumbnails vanish with no Go test failing.
func TestSkyRegex(t *testing.T) {
	page, err := os.ReadFile("../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`function sky\(html\)[\s\S]*?\.match\(/(.*)/\)`).FindSubmatch(page)
	if m == nil {
		t.Fatal("sky()'s regex not found in web/index.html")
	}
	re, err := regexp.Compile(string(m[1])) // JS and RE2 agree on its syntax
	if err != nil {
		t.Fatal(err)
	}
	cfg, s := mutSky(t)
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort = true, 2, 400, 300
	res := mutResults(s)
	res[0].Minor = 60 // 190x60' with -rotate
	mutColor(t, true) // as the page runs, CLICOLOR_FORCE=1
	for _, cfg.Rotate = range []bool{false, true} {
		lines := strings.Split(strings.TrimRight(mutStrip(mutCapture(t, func() { Results(cfg, s, res) })), "\n"), "\n")
		if len(lines) != len(res)+1 {
			t.Fatalf("rotate %v: %d lines, want header and %d rows:\n%s", cfg.Rotate, len(lines), len(res), strings.Join(lines, "\n"))
		}
		if re.MatchString(lines[0]) {
			t.Errorf("rotate %v: the header matches: %q", cfg.Rotate, lines[0])
		}
		for i, r := range res {
			sm := re.FindStringSubmatch(lines[i+1])
			if sm == nil {
				t.Errorf("rotate %v: %s: no match in %q", cfg.Rotate, r.Name, lines[i+1])

				continue
			}
			ra, dec := position(r.Position())
			size := ""
			if r.HasSize() {
				size = fmt.Sprintf("%.0f", r.Size)
			}
			if got := sm[1] + " " + sm[2]; got != ra {
				t.Errorf("rotate %v: %s: RA %q, want %q", cfg.Rotate, r.Name, got, ra)
			}
			if got := sm[3] + sm[4] + " " + sm[5]; got != dec {
				t.Errorf("rotate %v: %s: Dec %q, want %q", cfg.Rotate, r.Name, got, dec)
			}
			if sm[6] != size {
				t.Errorf("rotate %v: %s: size %q, want %q", cfg.Rotate, r.Name, sm[6], size)
			}
		}
	}
}
