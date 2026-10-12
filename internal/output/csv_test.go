// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/plan"
)

func TestSexagesimal(t *testing.T) {
	for _, c := range []struct {
		ra, dec  float64
		raS, deS string
	}{
		{10.684708, 41.26875, "00h 42m 44.3s", "+41° 16′ 08″"},
		{83.82208, -5.39111, "05h 35m 17.3s", "-05° 23′ 28″"},
		{359.99999, -0.0001, "00h 00m 00.0s", "+00° 00′ 00″"}, // carries round to 24h = 0h; -0″ has no sign
		{15 * (1 + 59.0/60 + 59.96/3600), 0, "02h 00m 00.0s", "+00° 00′ 00″"},
	} {
		if ra, de := sexagesimal(c.ra, c.dec); ra != c.raS || de != c.deS {
			t.Errorf("sexagesimal(%g, %g) = %q, %q, want %q, %q", c.ra, c.dec, ra, de, c.raS, c.deS)
		}
	}
}

// The header N.I.N.A. matches, the top -n targets, or the planned ones in
// plan order without repeats, and the header alone when there is nothing.
func TestCSV(t *testing.T) {
	cfg, s := mutSky(t)
	res := mutResults(s)
	read := func(out string) [][]string {
		t.Helper()
		rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Join(rows[0], ",") != "Catalogue Entry,Familiar Name,Right Ascension (J2000),Declination (J2000)" {
			t.Errorf("header %q", rows[0])
		}

		return rows[1:]
	}

	cfg.Top = 2
	rows := read(mutCapture(t, func() { _ = CSV(cfg, s, nil, res) }))
	if len(rows) != 2 || rows[0][0] != "M 31" || rows[1][0] != "NGC 7000 long name" || rows[0][2] != "00h 42m 43.2s" || rows[0][3] != "+41° 16′ 12″" {
		t.Errorf("top -n rows %q", rows)
	}

	cfg.Plan = s.End.Sub(s.Start) / 3
	slots := []plan.Slot{{Result: &res[2]}, {}, {Result: &res[0]}, {Result: &res[2]}}
	rows = read(mutCapture(t, func() { _ = CSV(cfg, s, slots, res) }))
	// The comet at its mid-track position (3°, 4°) of date, moved back to J2000.
	cra, cdec := astro.PrecessT(3, 4, -astro.DaysJ2000(s.Grid[len(s.Grid)/2])/36525)
	craS, cdecS := sexagesimal(cra, cdec)
	if len(rows) != 2 || rows[0][0] != "C/2026 A1" || rows[1][0] != "M 31" || rows[0][2] != craS || rows[0][3] != cdecS || craS == "00h 12m 00.0s" {
		t.Errorf("plan rows %q, comet want %s %s", rows, craS, cdecS)
	}
	// Reversing is good to an arcsecond: J2000 → date → J2000.
	for _, p := range [][2]float64{{3, 4}, {83.8, -5.4}, {280, 89}} {
		T := astro.DaysJ2000(s.Grid[0]) / 36525
		ra, dec := astro.PrecessT(p[0], p[1], T)
		ra, dec = astro.PrecessT(ra, dec, -T)
		if d := astro.Separation(ra, dec, p[0], p[1]) * 3600; d > 1 {
			t.Errorf("%v round trip off by %.2f″", p, d)
		}
	}

	if rows = read(mutCapture(t, func() { _ = CSV(cfg, s, nil, nil) })); len(rows) != 0 {
		t.Errorf("empty plan rows %q", rows)
	}
}
