// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// web/index.html's sky() finds each result row by the rank that opens it,
// with a regex read here from the page itself, and links it through the
// position main_js.go writes for that rank. Every result row must match and
// read back its rank, however narrow the table, and no other line of the
// report may match (a plan or forecast row would get some result's link),
// or the page's sky links and thumbnails go wrong with no Go test failing.
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
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort, cfg.Plan = true, 2, 400, 300, time.Hour
	res := mutResults(s)
	mutColor(t, false) // the page matches the text with the escapes' spans removed
	t.Cleanup(func() { TermWidth = 0 })
	for _, TermWidth = range []int{0, 60} { // wide, and with RA and DEC dropped
		lines := mutTable(t, mutCapture(t, func() { Results(cfg, s, res) }), "Targets, ranked by SCORE")
		if len(lines) != len(res)+1 {
			t.Fatalf("width %d: %d lines, want header and %d rows:\n%s", TermWidth, len(lines), len(res), strings.Join(lines, "\n"))
		}
		if re.MatchString(lines[0]) {
			t.Errorf("width %d: the header matches: %q", TermWidth, lines[0])
		}
		for i, r := range res {
			if sm := re.FindStringSubmatch(lines[i+1]); sm == nil || sm[1] != strconv.Itoa(i+1) {
				t.Errorf("width %d: %s: rank %q in %q, want %d", TermWidth, r.Name, sm, lines[i+1], i+1)
			}
		}
	}
	TermWidth = 0
	s.Weather = map[int64]weather.HourWeather{s.Start.Truncate(time.Hour).Unix(): {Cloud: 10}}
	slots := []plan.Slot{{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.5, PeakAt: s.Grid[0]}, {Start: s.Grid[60], End: s.End}}
	other := mutCapture(t, func() {
		Header(cfg, s, "Zagreb")
		Weather(cfg, s)
		Plan(cfg, s, slots)
		Legend(cfg, s, 0)
	})
	for l := range strings.SplitSeq(other, "\n") {
		if re.MatchString(l) {
			t.Errorf("a line outside the result rows matches: %q", l)
		}
	}
}

// The page closes the hover card before it replaces the report: a card left
// open under the pointer would go on showing the previous run's object, and
// show() would skip a link equal to the stale one it remembers.
func TestCardClosedOnRun(t *testing.T) {
	page, err := os.ReadFile("../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`show\(null\);\n(?:.*\n){0,2}\s*\$\("out"\)\.innerHTML = `)
	if n := strings.Count(string(page), `$("out").innerHTML = `); n == 0 || len(re.FindAll(page, -1)) != n {
		t.Errorf("%d report replacements, %d after show(null)", n, len(re.FindAll(page, -1)))
	}
}
