// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
)

func mutSetVersion(t *testing.T, tag, commit, dirty, built string) {
	t.Helper()
	old := [4]string{GitTag, GitCommit, GitDirty, BuildTime}
	GitTag, GitCommit, GitDirty, BuildTime = tag, commit, dirty, built
	t.Cleanup(func() { GitTag, GitCommit, GitDirty, BuildTime = old[0], old[1], old[2], old[3] })
}

func TestMutDescribe(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
		},
	}
	mutSetVersion(t, "", "", "", "")
	got := describe(info)
	if !strings.HasPrefix(got, "astro-recommender v1.2.3 (commit 0123456.dirty, built 2026-01-02T03:04:05Z, ") {
		t.Errorf("describe(buildinfo) = %q", got)
	}
	// -X values win, and a -X commit without a -X dirty is clean: the
	// build info's vcs.modified must not mark it.
	mutSetVersion(t, "v9", "abc1234", "", "today")
	got = describe(info)
	if !strings.HasPrefix(got, "astro-recommender v9 (commit abc1234, built today, ") {
		t.Errorf("describe(-X) = %q", got)
	}
	mutSetVersion(t, "", "", " .dirty\n", "")
	if got = describe(nil); !strings.HasPrefix(got, "astro-recommender dev (commit unknown.dirty, built unknown, ") {
		t.Errorf("describe(nil) = %q", got)
	}
	mutSetVersion(t, "", "", "", "")
	if got = describe(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}); !strings.HasPrefix(got, "astro-recommender dev (commit unknown, ") {
		t.Errorf("describe(devel) = %q", got)
	}
}

func TestPlanned(t *testing.T) {
	results := make([]scoring.Result, 3)
	t0 := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	slots := []plan.Slot{
		{Start: t0, End: t0.Add(time.Hour), Result: &results[0]},
		{Start: t0.Add(time.Hour), End: t0.Add(2 * time.Hour), Result: &results[1]},
		{Start: t0.Add(2 * time.Hour), End: t0.Add(150 * time.Minute), Result: &results[0]},
		{Start: t0.Add(150 * time.Minute), End: t0.Add(3 * time.Hour)}, // empty block
	}
	grid := make([]time.Time, 180)
	for i := range grid {
		grid[i] = t0.Add(time.Duration(i) * time.Minute)
	}
	// The first and third blocks: minutes 0-59 and 120-149.
	got := planned(slots, grid, &results[0])
	n := 0
	for i, in := range got {
		if in {
			n++
		}
		if in != (i < 60 || i >= 120 && i < 150) {
			t.Fatalf("two blocks: minute %d %v", i, in)
		}
	}
	if n != 90 {
		t.Errorf("two blocks: %d minutes", n)
	}
	// Listed but not planned: nil, so Subs takes the whole night.
	if got := planned(slots, grid, &results[2]); got != nil {
		t.Errorf("unplanned: %v", got)
	}
	if got := planned(nil, grid, &results[0]); got != nil {
		t.Errorf("no -plan: %v", got)
	}
}

// capture returns what f writes to stdout and stderr, through files, not
// os.Pipe, which js (the browser build's tests, under Node) does not
// implement.
func capture(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	swap := func(p **os.File, name string) (*os.File, func()) {
		tmp, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		old := *p
		*p = tmp

		return tmp, func() { *p = old }
	}
	read := func(f *os.File) string {
		_ = f.Close()
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}

		return string(b)
	}
	out, restoreOut := swap(&os.Stdout, "stdout")
	defer restoreOut()
	errs, restoreErr := swap(&os.Stderr, "stderr")
	defer restoreErr()
	f()
	restoreOut()
	restoreErr()

	return read(out), read(errs)
}

// nothing keeps -csv and -json parseable: the CSV's header alone with the
// message on stderr, and a JSON document whose message survives control
// characters and non-ASCII, which %q would escape as invalid JSON.
func TestNothing(t *testing.T) {
	const msg = "nothing \x01 \u00e9\t tonight"
	if out, errs := capture(t, func() { nothing(&config.Config{}, msg) }); out != msg+"\n" || errs != "" {
		t.Errorf("text: stdout %q, stderr %q", out, errs)
	}
	out, errs := capture(t, func() { nothing(&config.Config{CSV: true}, msg) })
	if out != "Catalogue Entry,Familiar Name,Right Ascension (J2000),Declination (J2000)\n" || errs != msg+"\n" {
		t.Errorf("csv: stdout %q, stderr %q", out, errs)
	}
	out, errs = capture(t, func() { nothing(&config.Config{JSON: true}, msg) })
	var doc struct {
		Targets []string `json:"targets"`
		Message string   `json:"message"`
	}
	if !json.Valid([]byte(out)) || json.Unmarshal([]byte(out), &doc) != nil || doc.Message != msg || doc.Targets == nil || len(doc.Targets) != 0 || errs != "" {
		t.Errorf("json: stdout %q, stderr %q", out, errs)
	}
}

// exposures estimates the listed results only (the first -n), rescoring
// those without per-minute data once and keeping any they have; without
// framing (no camera) or with -csv (never shown) it does nothing.
func TestExposures(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, Top: 2, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, FilterNM: 7}
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort = true, 1.2314, 79.2, 45
	cfg.Aperture, cfg.Obstruction, cfg.Sub, cfg.QE, cfg.ReadNoise = 152, 63, 15, 0.83, 2.15
	start, end, _ := astro.Window(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	s := scoring.BuildSky(cfg, scoring.NoForecast(), start, end)
	var targets []catalog.Target
	for i, name := range []string{"A", "B", "C"} {
		targets = append(targets, catalog.Target{Name: name, Type: "Galaxy", RADeg: 300 + 10*float64(i), DecDeg: 40, Size: 10, SurfBr: 21})
	}
	fresh := func() []scoring.Result {
		rs := scoring.Score(cfg, &s, targets, false)
		if len(rs) != 3 || rs[0].Obs != nil {
			t.Fatalf("scored %d, want 3 without per-minute data", len(rs))
		}

		return rs
	}
	for name, c := range map[string]func(*config.Config){"no framing": func(c *config.Config) { c.Framing = false }, "csv": func(c *config.Config) { c.CSV = true }} {
		one := *cfg
		c(&one)
		rs := fresh()
		exposures(&one, &s, nil, rs)
		for _, r := range rs {
			if r.Obs != nil || r.Exposure != nil {
				t.Errorf("%s: %s rescored or estimated", name, r.Name)
			}
		}
	}
	rs := fresh()
	kept := make([]bool, len(s.Grid)) // never observable: Subs gives none
	rs[1].Obs = kept
	exposures(cfg, &s, nil, rs)
	if rs[0].Obs == nil || rs[0].Exposure == nil || rs[0].Exposure.Good <= 0 {
		t.Errorf("listed: obs %v, exposure %+v", rs[0].Obs != nil, rs[0].Exposure)
	}
	if &rs[1].Obs[0] != &kept[0] || rs[1].Exposure != nil {
		t.Errorf("own per-minute data: replaced %v, exposure %+v", &rs[1].Obs[0] != &kept[0], rs[1].Exposure)
	}
	if rs[2].Obs != nil || rs[2].Exposure != nil {
		t.Error("past -n: rescored or estimated")
	}
}
