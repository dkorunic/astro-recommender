// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
)

// csvHeader is Telescopius's observing-list header, the one N.I.N.A.'s
// target import reads: it takes Familiar Name, else Catalogue Entry, and the
// "(J2000)" coordinate columns. A plain "Right Ascension" column would be
// read as the declination too (NINA SimpleSequenceVM.ImportTargets), so the
// columns keep their suffix.
var csvHeader = []string{"Catalogue Entry", "Familiar Name", "Right Ascension (J2000)", "Declination (J2000)"}

// CSV prints the top -n targets, or with -plan the planned ones in plan
// order, as CSV for N.I.N.A.'s simple sequencer: J2000 positions to the
// second. A comet's is its mid-window track point, which is of date, moved
// back to J2000 (PrecessT at minus the epoch, within an arcsecond) since
// N.I.N.A. slews to it. With no results it prints the header.
func CSV(cfg *config.Config, s *scoring.Sky, slots []plan.Slot, results []scoring.Result) error {
	var list []*scoring.Result
	if cfg.Plan > 0 {
		seen := map[*scoring.Result]bool{}
		for _, sl := range slots {
			if sl.Result != nil && !seen[sl.Result] {
				seen[sl.Result] = true
				list = append(list, sl.Result)
			}
		}
	} else {
		for i := range results[:min(cfg.Top, len(results))] {
			list = append(list, &results[i])
		}
	}
	w := csv.NewWriter(os.Stdout)
	_ = w.Write(csvHeader)
	for _, res := range list {
		raS, decS := sexagesimal(J2000(s, res.Target))
		_ = w.Write([]string{cell(res.Name), "", raS, decS})
	}
	w.Flush()

	return w.Error()
}

// cell keeps a -targets name from running as a spreadsheet formula when the
// CSV is opened in one: a leading =, +, - or @ gets an apostrophe before it
// (OWASP's CSV injection advice). No embedded list name starts with one.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}

	return s
}

// sexagesimal formats J2000 degrees as "05h 35m 17.3s" and "-05° 23′ 28″"
// (primes, not quotes, which CSV would have to escape), rounded as a whole so
// that 59.95 seconds carries into the minute. N.I.N.A. reads only the digits
// and a minus sign.
func sexagesimal(ra, dec float64) (string, string) {
	t := int(math.Round(math.Mod(ra+360, 360)/15*36000)) % (24 * 36000) // tenths of a second
	a := int(math.Round(math.Abs(dec) * 3600))
	sign := "+"
	if dec < 0 && a > 0 {
		sign = "-"
	}

	return fmt.Sprintf("%02dh %02dm %04.1fs", t/36000, t/600%60, float64(t%600)/10),
		fmt.Sprintf("%s%02d° %02d′ %02d″", sign, a/3600, a/60%60, a%60)
}

// J2000 returns the target's position in J2000 degrees: Position(), with a
// comet's mid-window track point, which is of date, moved back to J2000
// (PrecessT at minus the epoch, within an arcsecond). The CSV and the web
// page's sky links take it, since N.I.N.A., Aladin and hips2fits expect J2000.
func J2000(s *scoring.Sky, t catalog.Target) (float64, float64) {
	ra, dec := t.Position()
	if t.Track != nil {
		ra, dec = astro.PrecessT(ra, dec, -astro.DaysJ2000(s.Grid[len(s.Grid)/2])/36525)
	}

	return ra, dec
}
