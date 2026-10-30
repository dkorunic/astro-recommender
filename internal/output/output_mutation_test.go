// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/horizon"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// mutCapture returns what f prints to stdout.
func mutCapture(t *testing.T, f func()) string {
	t.Helper()
	// A file, not os.Pipe, which js (the browser build's tests, under Node)
	// does not implement.
	tmp, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	old := os.Stdout
	os.Stdout = tmp
	defer func() { os.Stdout = old }()
	f()
	os.Stdout = old
	b, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func mutColor(t *testing.T, on bool) {
	t.Helper()
	old := UseColor
	UseColor = on
	t.Cleanup(func() { UseColor = old })
}

func mutZone(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Skip(err)
	}

	return loc
}

// mutSky is two hours of a Zagreb night from 20:00 local, the Moon rising
// at 20:30 and setting at 21:15.
func mutSky(t *testing.T) (*config.Config, *scoring.Sky) {
	t.Helper()
	// Results and Plan leave what they printed for the legend; start clean.
	noTargets, emptyPlan = false, false
	loc := mutZone(t)
	cfg := &config.Config{
		Lat: 45.81, Lon: 15.98, Loc: loc, Twilight: "astronomical", ListName: "GaryImm", Top: 20,
		SizeMin: 10, SizeMax: 300, Extinction: 0.2, CometMag: 12,
	}
	start := time.Date(2026, 1, 15, 20, 0, 0, 0, loc)
	s := &scoring.Sky{Start: start, End: start.Add(2 * time.Hour), Night: [2]time.Time{start, start.Add(2 * time.Hour)}, Elevation: math.NaN(), Illum: 0.42, MoonSep: 42}
	for i := range 120 {
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
		alt := -5.0
		if i >= 30 && i < 75 {
			alt = 10
		}
		s.MoonAlt = append(s.MoonAlt, alt)
		s.Ext = append(s.Ext, 0.2)
	}

	return cfg, s
}

func TestMutPosition(t *testing.T) {
	for _, c := range []struct {
		ra, dec float64
		r, d    string
	}{
		{0, 0, "00 00.0", "+00 00"},
		{359.9999, 0, "00 00.0", "+00 00"},
		{15 * (5 + 59.96/60), 10.5, "06 00.0", "+10 30"},
		{83.82, -5.39, "05 35.3", "-05 23"},
		{-15, -0.004, "23 00.0", "+00 00"},
		{10.68, 89.99999, "00 42.7", "+90 00"},
		{180, -89.5, "12 00.0", "-89 30"},
		{37.5, 41.999, "02 30.0", "+42 00"},
	} {
		r, d := position(c.ra, c.dec)
		if r != c.r || d != c.d {
			t.Errorf("position(%v, %v) = %q %q, want %q %q", c.ra, c.dec, r, d, c.r, c.d)
		}
	}
}

func TestMutScalePaint(t *testing.T) {
	for _, c := range []struct {
		v, ok, bad float64
		want       string
	}{
		{0.66, 0.66, 0.33, "32"},
		{0.9, 0.66, 0.33, "32"},
		{0.5, 0.66, 0.33, "33"},
		{0.33, 0.66, 0.33, "31"},
		{0.1, 0.66, 0.33, "31"},
		{30, 30, 70, "32"},
		{10, 30, 70, "32"},
		{50, 30, 70, "33"},
		{70, 30, 70, "31"},
		{90, 30, 70, "31"},
	} {
		if got := scale(c.v, c.ok, c.bad); got != c.want {
			t.Errorf("scale(%v, %v, %v) = %s, want %s", c.v, c.ok, c.bad, got, c.want)
		}
	}
	mutColor(t, false)
	if paint("32", "x") != "x" {
		t.Error("paint without color")
	}
	mutColor(t, true)
	if got := paint("32", "x"); got != "\x1b[32mx\x1b[0m" {
		t.Errorf("paint = %q", got)
	}
}

func TestMutWrap(t *testing.T) {
	got := wrap("aaa bbb ccc dd", 7)
	if strings.Join(got, "|") != "aaa bbb|ccc dd" {
		t.Errorf("wrap = %q", got)
	}
	// A word without letters stays with the next one.
	got = wrap("size - unknown here", 8)
	if strings.Join(got, "|") != "size|- unknown|here" {
		t.Errorf("wrap = %q", got)
	}
	got = wrap("ab cd", 5)
	if len(got) != 1 {
		t.Errorf("exact fit wrapped: %q", got)
	}
	got = wrap("ÅÅ ÅÅ", 5)
	if len(got) != 1 {
		t.Errorf("runes, not bytes: %q", got)
	}
}

func TestMutMoonTextAndUp(t *testing.T) {
	cfg, s := mutSky(t)
	_ = cfg
	if got := moonText(s); got != "rises 20:30, sets 21:15" {
		t.Errorf("moonText = %q", got)
	}
	up := moonUp(s)
	if len(up) != 1 || !up[0].From.Equal(s.Grid[30]) || !up[0].To.Equal(s.Grid[75]) {
		t.Errorf("moonUp = %v", up)
	}
	for i := range s.MoonAlt {
		s.MoonAlt[i] = 5
	}
	s.MoonAlt[0] = astro.MoonHorizon // the upper limb exactly on the refracted horizon is down
	if got := moonText(s); got != "rises 20:01" {
		t.Errorf("moonText = %q", got)
	}
	s.MoonAlt[0] = 5
	if got := moonText(s); got != "up throughout" {
		t.Errorf("moonText = %q", got)
	}
	if up := moonUp(s); len(up) != 1 || !up[0].From.Equal(s.Grid[0]) || !up[0].To.Equal(s.End) {
		t.Errorf("moonUp throughout = %v", up)
	}
	for i := range s.MoonAlt {
		s.MoonAlt[i] = -1 // the centre 1° down: the limb is below the horizon
	}
	if got := moonText(s); got != "never up" {
		t.Errorf("moonText = %q", got)
	}
	if up := moonUp(s); up == nil || len(up) != 0 {
		t.Errorf("moonUp never = %#v", up)
	}
}

func TestMutSmallCells(t *testing.T) {
	for _, c := range []struct {
		tg   catalog.Target
		want string
	}{
		{catalog.Target{Type: "Galaxy", Size: 10, Mag: 10}, "~23.6"},
		{catalog.Target{Type: "Galaxy", SurfBr: 21.54}, "21.5"},
		{catalog.Target{Type: "Galaxy"}, "-"},
	} {
		if got := sbText(scoring.Result{Target: c.tg}); got != c.want {
			t.Errorf("sbText(%+v) = %q, want %q", c.tg, got, c.want)
		}
	}
	_, s := mutSky(t)
	r := scoring.Result{RunFrom: s.Grid[5], RunTo: s.Grid[65], Runs: 1}
	if got := runText(s, r); got != "20:05-21:05" {
		t.Errorf("runText = %q", got)
	}
	r.Runs = 2
	if got := runText(s, r); got != "20:05-21:05+" {
		t.Errorf("runText = %q", got)
	}
	// A polar night's whole day must not read as an empty 12:00-12:00.
	if got := spanText(s, s.Grid[0], s.Grid[0].Add(24*time.Hour)); got != "20:00-20:00+1d" {
		t.Errorf("spanText(24h) = %q", got)
	}
	if got := spanText(s, s.Grid[0], s.Grid[0].Add(24*time.Hour-time.Minute)); got != "20:00-19:59" {
		t.Errorf("spanText(24h-1m) = %q", got)
	}
	if magText(catalog.Target{Mag: 8.24}) != " 8.2" || magText(catalog.Target{Mag: -9999, BMag: 12.3}) != "12.3B" || magText(catalog.Target{Mag: -9999}) != "-" {
		t.Error("magText")
	}
	if sizeText(catalog.Target{Size: -9999}, "%.0f'", -9999) != "-" || sizeText(catalog.Target{Size: 12.4}, "%.0f'", 12.4) != "12'" {
		t.Error("sizeText")
	}
	if typeColor(catalog.Target{Type: "HII region"}) != "35" || typeColor(catalog.Target{Type: "Comet", Track: [][2]float64{{0, 0}}}) != "33" || typeColor(catalog.Target{Type: "Galaxy"}) != "39" {
		t.Error("typeColor")
	}
}

// A night across the October fall-back shows zone abbreviations.
func TestMutClockLayout(t *testing.T) {
	loc := mutZone(t)
	s := &scoring.Sky{Night: [2]time.Time{time.Date(2026, 10, 24, 19, 0, 0, 0, loc), time.Date(2026, 10, 25, 5, 0, 0, 0, loc)}}
	if got := clock(s, s.Night[1]); got != "05:00 CET" {
		t.Errorf("clock = %q", got)
	}
	s.Night[0] = time.Date(2026, 10, 25, 3, 30, 0, 0, loc)
	if got := clock(s, s.Night[1]); got != "05:00" {
		t.Errorf("clock = %q", got)
	}
}

func TestMutColorTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	if !ColorTerminal() {
		t.Error("CLICOLOR_FORCE=1 without color")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorTerminal() {
		t.Error("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if ColorTerminal() {
		t.Error("TERM=dumb ignored")
	}
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "0")
	if ColorTerminal() { // stdout under go test is not a terminal
		t.Error("CLICOLOR_FORCE=0 forced color")
	}
}

func mutResults(s *scoring.Sky) []scoring.Result {
	mk := func(name, typ string, size, score float64) scoring.Result {
		return scoring.Result{
			Name: name, Type: typ, Description: "desc " + name, Constellation: "Andromeda", RADeg: 10.68, DecDeg: 41.27, Size: size, SurfBr: 21,
			Foto: 0.5, Score: score, Frame: 1, Runs: 1, RunFrom: s.Grid[0], RunTo: s.Grid[60], MaxAlt: 70, MaxAt: s.Grid[30], SkyMag: 21.2,
		}
	}

	return []scoring.Result{
		mk("M 31", "Galaxy", 190, 0.8),
		mk("NGC 7000 long name", "HII region", -9999, 0.5),
		{Name: "C/2026 A1", Type: "Comet", Track: [][2]float64{{1, 2}, {3, 4}, {5, 6}}, Foto: 0.2, Score: 0.1, Frame: 1, RunFrom: s.Grid[0], RunTo: s.Grid[1], MaxAt: s.Grid[0]},
	}
}

func mutStrip(s string) string { return sgr.ReplaceAllString(s, "") }

// mutTable splits a table's output into lines after its title line, which
// it checks.
func mutTable(t *testing.T, out, title string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(mutStrip(out), "\n"), "\n")
	if lines[0] != title {
		t.Fatalf("title %q, want %q:\n%s", lines[0], title, out)
	}

	return lines[1:]
}

// mutCol is the screen column of byte offset i in line: "°" is two bytes.
func mutCol(line string, i int) int { return utf8.RuneCountInString(line[:i]) }

// Colors must not move columns: stripped of escapes the colored table is
// the plain one.
func TestMutResultsAlignment(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.Framing, cfg.Scale, cfg.FOVShort, cfg.FOVLong = true, 2, 300, 400
	res := mutResults(s)
	res[0].Exposure = &scoring.Estimate{Modest: 30, Good: 120, Best: 480, Max: 100, Sub: 15, SNRPerSub: 0.9, Panels: 1, Assumed: true}
	mutColor(t, false)
	var plainW int
	plain := mutCapture(t, func() { plainW = Results(cfg, s, res) })
	mutColor(t, true)
	var colorW int
	colored := mutCapture(t, func() { colorW = Results(cfg, s, res) })
	for _, e := range regexp.MustCompile("\x1b\\[[0-9]*m").FindAllString(colored, -1) {
		if !regexp.MustCompile("^\x1b\\[([0-9][0-9]|0)m$").MatchString(e) {
			t.Fatalf("escape %q is not a 2-digit color or reset", e)
		}
	}
	// Every cell opens with a 2-digit color and closes with a reset.
	if o, c := len(regexp.MustCompile("\x1b\\[[0-9][0-9]m").FindAllString(colored, -1)), strings.Count(colored, "\x1b[0m"); o != c || o == 0 {
		t.Errorf("%d colors, %d resets", o, c)
	}
	if mutStrip(colored) != plain {
		t.Errorf("colored table misaligned:\n%s\nvs\n%s", mutStrip(colored), plain)
	}
	if plainW != colorW || plainW == 0 {
		t.Errorf("widths %d %d", plainW, colorW)
	}
	lines := mutTable(t, plain, "Targets, ranked by SCORE")
	maxLen := 0
	for _, l := range lines {
		maxLen = max(maxLen, len([]rune(strings.TrimRight(l, " "))))
	}
	if plainW != maxLen {
		t.Errorf("width %d, longest line %d", plainW, maxLen)
	}
	if len(lines) != 4 || !strings.Contains(lines[1], "5700") || !strings.Contains(lines[1], "00 42.7") || !strings.Contains(lines[1], "+41 16") {
		t.Errorf("table:\n%s", plain)
	}
	// Columns of the first row: rank 1, PX 190·60/2 = 5700.
	f := strings.Fields(lines[1])
	// SUBS last: more than the 100 subs tonight allows, as nights; STAR
	// after SB, μ And (3.9) 1.4° off in the 400' x 300' frame.
	if f[0] != "1" || f[len(f)-1] != "nights" || f[len(f)-3] != "5700" || strings.Join(f[len(f)-6:len(f)-3], " ") != "μ And 3.9" || f[len(f)-7] != "21.0" {
		t.Errorf("row %q", f)
	}
	if !strings.Contains(lines[2], " -  ") || !strings.HasSuffix(strings.TrimRight(lines[3], " "), "-") {
		t.Errorf("unknown size / comet rows:\n%s", plain)
	}
	// With -filter, FILTER sits before PX: the choice, or "none", aligned and
	// as many escapes per cell when colored.
	cfg.Filter, res[0].Filter = "dual,ha", "dual"
	mutColor(t, false)
	plain = mutCapture(t, func() { Results(cfg, s, res) })
	mutColor(t, true)
	if colored := mutCapture(t, func() { Results(cfg, s, res) }); mutStrip(colored) != plain {
		t.Errorf("colored table with FILTER misaligned:\n%s\nvs\n%s", mutStrip(colored), plain)
	}
	lines = mutTable(t, plain, "Targets, ranked by SCORE")
	h, f0, f1 := strings.Fields(lines[0]), strings.Fields(lines[1]), strings.Fields(lines[2])
	// Row 1's SUBS, "N nights", is two fields.
	if h[len(h)-3] != "FILTER" || f0[len(f0)-4] != "dual" || f1[len(f1)-3] != "none" {
		t.Errorf("FILTER cells:\n%s", plain)
	}
	if col := mutCol(lines[0], strings.Index(lines[0], "FILTER")); mutCol(lines[1], strings.Index(lines[1], "dual")) != col || !strings.HasPrefix(string([]rune(lines[2])[col:]), "none") {
		t.Errorf("FILTER column not aligned:\n%s", plain)
	}
	cfg.Filter = ""
	cfg.Top = 2
	if out := mutCapture(t, func() { Results(cfg, s, res) }); strings.Count(out, "\n") != 4 {
		t.Errorf("-n 2 printed:\n%s", out)
	}
	if out := mutCapture(t, func() {
		if w := Results(cfg, s, nil); w != 0 {
			t.Errorf("empty width %d", w)
		}
	}); !strings.Contains(out, "No objects") {
		t.Errorf("empty: %q", out)
	}
	// A mosaic that scored best follows the pixels in PX, still aligned.
	cfg.Top, res[0].Mosaic = 1, config.Mosaic{Cols: 2, Rows: 1}
	mutColor(t, false)
	plain = mutCapture(t, func() { Results(cfg, s, res) })
	if !strings.Contains(plain, "  5700 2x1  ") {
		t.Errorf("PX without the mosaic:\n%s", plain)
	}
	mutColor(t, true)
	if colored := mutCapture(t, func() { Results(cfg, s, res) }); mutStrip(colored) != plain {
		t.Errorf("colored table with a mosaic misaligned:\n%s\nvs\n%s", mutStrip(colored), plain)
	}
}

func TestMutWeatherTable(t *testing.T) {
	cfg, s := mutSky(t)
	h0 := s.Start.Truncate(time.Hour).Unix()
	s.Weather = map[int64]weather.HourWeather{
		h0:        {Cloud: 20, Low: 10, Mid: 5, High: 10, Temp: 5, DewPoint: 1, Wind: 8, Gust: 15, Precip: 0.05, Seeing: 1.2},
		h0 + 3600: {Cloud: 90, Temp: 5, DewPoint: 4, Wind: 8, Gust: 40, Precip: 0.04},
	}
	s.Astro = map[int64]weather.AstroBlock{weather.AstroKey(s.Start.Add(time.Hour)): {Seeing: 3, Transparency: 2}}
	mutColor(t, false)
	out := mutCapture(t, func() { Weather(cfg, s) })
	lines := mutTable(t, out, "Forecast")
	if len(lines) != 3 {
		t.Fatalf("weather table:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "20:00") || !strings.Contains(lines[1], "20% 0.1mm") || !regexp.MustCompile(`~\d\.\d"`).MatchString(lines[1]) || !strings.Contains(lines[1], " 4.0°C") || strings.Contains(lines[1], "-4.0") || !strings.Contains(lines[1], "8/15 km/h") {
		t.Errorf("row 1 %q", lines[1])
	}
	// A gust past 3x the wind shows as scored, capped and marked.
	if !strings.HasPrefix(lines[2], "21:00") || strings.Contains(lines[2], "mm") || !strings.Contains(lines[2], "2/8") || !strings.Contains(lines[2], `0.75-1"`) || !strings.Contains(lines[2], "8/~24 km/h") {
		t.Errorf("row 2 %q", lines[2])
	}
	js := mutCapture(t, func() {
		if err := JSON(cfg, s, "", nil, nil); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(js, `"gust": 15,`) || !strings.Contains(js, `"gust": 24,`) || strings.Count(js, `"gustForecast": 40`) != 1 || strings.Count(js, "gustForecast") != 1 {
		t.Errorf("JSON gusts:\n%s", js)
	}
	mutColor(t, true)
	if colored := mutCapture(t, func() { Weather(cfg, s) }); mutStrip(colored) != out {
		t.Errorf("colored weather table misaligned:\n%s", mutStrip(colored))
	}
	s.Weather, s.Astro = nil, nil
	if out := mutCapture(t, func() { Weather(cfg, s) }); out != "" {
		t.Errorf("no forecast printed %q", out)
	}
}

func TestMutPlanTable(t *testing.T) {
	cfg, s := mutSky(t)
	res := mutResults(s)
	slots := []plan.Slot{
		{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.7, PeakAlt: 66, PeakAt: s.Grid[59]},
		{Start: s.Grid[60], End: s.End},
	}
	mutColor(t, false)
	out := mutCapture(t, func() { Plan(cfg, s, slots) })
	if !strings.Contains(out, "20:00-21:00") || !strings.Contains(out, "0.70") || !strings.Contains(out, "66° 20:59") || !strings.Contains(out, "no target") {
		t.Errorf("plan:\n%s", out)
	}
	mutColor(t, true)
	if colored := mutCapture(t, func() { Plan(cfg, s, slots) }); mutStrip(colored) != out {
		t.Errorf("colored plan misaligned:\n%s", mutStrip(colored))
	}
	if strings.Contains(out, "FILTER") {
		t.Errorf("FILTER without -filter:\n%s", out)
	}
	// With -filter, FILTER closes each row: the target's choice, "-" for an
	// empty block, still aligned when colored.
	cfg.Filter, res[0].Filter = "dual,ha", "ha"
	mutColor(t, false)
	out = mutCapture(t, func() { Plan(cfg, s, slots) })
	lines := mutTable(t, out, "Night plan")
	if f := strings.Fields(lines[0]); f[len(f)-1] != "FILTER" || !strings.HasSuffix(strings.TrimRight(lines[1], " "), "  ha") || !strings.HasSuffix(strings.TrimRight(lines[2], " "), "  -") {
		t.Errorf("plan with FILTER:\n%s", out)
	}
	if mutCol(lines[0], strings.Index(lines[0], "FILTER")) != mutCol(lines[1], strings.LastIndex(lines[1], "ha")) {
		t.Errorf("FILTER column not aligned:\n%s", out)
	}
	mutColor(t, true)
	if colored := mutCapture(t, func() { Plan(cfg, s, slots) }); mutStrip(colored) != out {
		t.Errorf("colored plan with FILTER misaligned:\n%s", mutStrip(colored))
	}
}

func TestMutHeader(t *testing.T) {
	cfg, s := mutSky(t)
	mutColor(t, false)
	s.Ext[10] = 0.24
	out := mutCapture(t, func() { Header(cfg, s, "Zagreb") })
	for _, want := range []string{
		"Location: 45.8100, 15.9800 (Zagreb), Europe/Zagreb",
		"Window:   2026-01-15 20:00 - 2026-01-15 22:00 (2h 00m)\n",
		"Moon:     42% illuminated, targets kept at least 42° away, rises 20:30, sets 21:15",
		"Size:     10.0' - 300.0'",
		"0 comets brighter than mag 12.0",
		"dark, 22.0 mag/arcsec² (default), extinction 0.20-0.24 (default) mag/airmass",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	s.Ext[10] = 0.204
	cfg.SQM, cfg.SQMSource, cfg.ExtinctionSet, cfg.Twilight = 21.6, "DSS", true, "nautical"
	cfg.Filter, cfg.FilterNM, cfg.NoComets = "dual", 7, true
	cfg.RASet, cfg.RA, cfg.Tol = true, 187.5, 5
	cfg.MinRun = time.Hour
	out = mutCapture(t, func() { Header(cfg, s, "") })
	for _, want := range []string{
		"Location: 45.8100, 15.9800, Europe/Zagreb",
		"(2h 00m, nautical night)",
		"SQM 21.60 mag/arcsec² (DSS, ≈ Bortle 3)",
		"extinction 0.20 (set) mag/airmass\n",
		"Filter:   dual (7 nm line bands)",
		"GaryImm, comets off",
		"Region:   within 5° of RA 12 30.0",
		"Min run:  at least 1h 00m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	// Clipped window: the full night is shown.
	cfg.SQM, cfg.Bortle = 0, 6
	s.Start = s.Grid[30]
	s.Elevation, s.AOD = 120, map[int64]float64{1: 0.1}
	cfg.ExtinctionSet = false
	out = mutCapture(t, func() { Header(cfg, s, "") })
	for _, want := range []string{"nautical night 20:00-22:00", "Bortle 6 (zenith 19.95 mag/arcsec²)", "(elevation 120 m, aerosols)"} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	s.AOD = nil
	if out = mutCapture(t, func() { Header(cfg, s, "") }); !strings.Contains(out, "(elevation 120 m, typical aerosols)") {
		t.Errorf("header without AOD:\n%s", out)
	}
	// A clipped polar night: noon to noon reads as a whole day, not empty.
	s.Night[1] = s.Night[0].Add(24 * time.Hour)
	if out = mutCapture(t, func() { Header(cfg, s, "") }); !strings.Contains(out, "nautical night 20:00-20:00+1d") {
		t.Errorf("header of a 24 h night:\n%s", out)
	}
	if strings.Contains(out, "Frame:") || strings.Contains(out, "Horizon:") {
		t.Errorf("Frame/Horizon lines without framing or -horizon:\n%s", out)
	}
	// The optional lines: a Dec region, a -targets file, lost comets, -type,
	// -skip, framing with mosaics and rotation, a horizon; -bortle 0 asked
	// for the dark sky rather than defaulting to it.
	cfg.DecSet, cfg.Dec = true, -5.5
	cfg.TargetsFile, cfg.NoComets, s.CometsLost = "mine.yaml", false, true
	cfg.Types = []string{"galaxy", "nebula"}
	cfg.SkipFile, cfg.Skip = "done.txt", map[string]bool{"m31": true, "m33": true}
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort, cfg.Mosaic, cfg.Rotate = true, 2, 400, 300, 4, true
	cfg.Horizon = [][2]float64{{0, 10}, {180, 25}, {270, 5}}
	cfg.Bortle, cfg.SkySet = 0, true
	out = mutCapture(t, func() { Header(cfg, s, "") })
	for _, want := range []string{
		"Region:   within 5° of RA 12 30.0, Dec -05 30\n",
		"Targets:  mine.yaml, comets unavailable, only galaxy, nebula, skipping 2 names from done.txt\n",
		"Frame:    6.67° x 5.00°, 2.00\"/px (12000 x 9000 px), mosaics of up to 4 panels, rotatable\n",
		"Horizon:  3 points, 5° - 25°\n",
		"Sky:      dark, 22.0 mag/arcsec² (-bortle/-sqm 0)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	// A single frame, fixed: neither suffix.
	cfg.Mosaic, cfg.Rotate = 1, false
	if out = mutCapture(t, func() { Header(cfg, s, "") }); !strings.Contains(out, "(12000 x 9000 px)\n") {
		t.Errorf("single-frame header:\n%s", out)
	}
}

func TestMutJSON(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.SQM, cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort, cfg.Top = 20.9, true, 2, 400, 300, 2
	h0 := s.Start.Truncate(time.Hour).Unix()
	s.Weather = map[int64]weather.HourWeather{
		h0:        {Cloud: 20, Temp: 5, DewPoint: 1, Precip: math.NaN()},
		h0 + 3600: {Cloud: 30, Temp: 6, DewPoint: 1, Precip: 0.3, Seeing: 1.2345},
	}
	s.Astro = map[int64]weather.AstroBlock{weather.AstroKey(s.Start): {Seeing: 3, Transparency: 2}}
	cfg.Mosaic, cfg.Rotate = 4, true
	res := mutResults(s)
	res[0].Exposure = &scoring.Estimate{Modest: 3, Good: 10, Best: 40, Max: 200, Sub: 15, SNRPerSub: 3.2, Panels: 1}
	res[0].Minor, res[0].BMag, res[0].Mosaic = 120, 4.4, config.Mosaic{Cols: 2, Rows: 1}
	// A peak of exactly 0° (-alt-min 0) is a value, not absent.
	slots := []plan.Slot{{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.7, PeakAlt: 0, PeakAt: s.Grid[59]}, {Start: s.Grid[60], End: s.End}}
	out := mutCapture(t, func() {
		if err := JSON(cfg, s, "Zagreb", slots, res); err != nil {
			t.Error(err)
		}
	})
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	tj := doc["targets"].([]any)
	if sj, ok := tj[0].(map[string]any)["subs"].(map[string]any); !ok || sj["good"] != 10.0 || sj["sub"] != 15.0 || sj["assumedSurfaceBrightness"] != nil {
		t.Errorf("subs %v", tj[0].(map[string]any)["subs"])
	}
	if _, ok := tj[1].(map[string]any)["subs"]; ok {
		t.Error("subs emitted without an estimate")
	}
	skyJ := doc["sky"].(map[string]any)
	if skyJ["bortle"] != float64(atmos.BortleClass(20.9)) || skyJ["bortle"] != 4.0 || skyJ["zenithMag"] != 20.9 || skyJ["filterK"] != nil {
		t.Errorf("sky %v", skyJ)
	}
	if fr := doc["frame"].(map[string]any); fr["scale"] != 2.0 || fr["fovShort"] != 300.0 || fr["maxPanels"] != 4.0 || fr["rotate"] != true {
		t.Errorf("frame %v", fr)
	}
	moonJ := doc["moon"].(map[string]any)
	up := moonJ["up"].([]any)
	if len(up) != 1 || moonJ["minSeparation"] != 42.0 || moonJ["illumination"] != 0.42 {
		t.Errorf("moon %v", moonJ)
	}
	fc := doc["forecast"].([]any)
	if len(fc) != 2 {
		t.Fatalf("forecast %v", fc)
	}
	if _, ok := fc[0].(map[string]any)["precipitation"]; ok {
		t.Error("NaN precipitation emitted")
	}
	if p := fc[1].(map[string]any)["precipitation"]; p != 0.3 || fc[1].(map[string]any)["dewSpread"] != 5.0 {
		t.Errorf("hour 2 %v", fc[1])
	}
	// The upper-air estimate is seeingArcsec, two decimals, left out when unknown.
	if _, ok := fc[0].(map[string]any)["seeingArcsec"]; ok {
		t.Error("unknown seeing emitted")
	}
	if v := fc[1].(map[string]any)["seeingArcsec"]; v != 1.23 {
		t.Errorf("seeingArcsec %v, want 1.23", v)
	}
	// 7Timer's point gives its hour transparency and seeing class.
	if h := fc[0].(map[string]any); h["transparency"] != 2.0 || h["seeing"] != `0.75-1"` {
		t.Errorf("hour 1 without 7Timer's values %v", h)
	}
	pl := doc["plan"].([]any)
	if p0, p1 := pl[0].(map[string]any), pl[1].(map[string]any); p0["name"] != "M 31" || p0["peakAt"] == nil || p0["peakAlt"] != 0.0 ||
		p1["name"] != nil || p1["peakAt"] != nil || p1["peakAlt"] != nil {
		t.Errorf("plan %v", pl)
	}
	tg := doc["targets"].([]any)
	if len(tg) != 2 {
		t.Fatalf("targets %d, want -n 2", len(tg))
	}
	t0, t1 := tg[0].(map[string]any), tg[1].(map[string]any)
	if t0["rank"] != 1.0 || t0["size"] != 190.0 || t0["px"] != 5700.0 || t0["ra"] != 10.68 || t0["surfaceBrightness"] != 21.0 || t0["surfaceBrightnessEstimated"] != nil || t0["comet"] != nil {
		t.Errorf("target 0 %v", t0)
	}
	if t0["minor"] != 120.0 || t0["bmag"] != 4.4 || !reflect.DeepEqual(t0["mosaic"], map[string]any{"cols": 2.0, "rows": 1.0}) {
		t.Errorf("target 0 minor/bmag/mosaic %v", t0)
	}
	if t1["rank"] != 2.0 || t1["size"] != nil || t1["px"] != nil || t1["minor"] != nil || t1["bmag"] != nil || t1["mosaic"] != nil {
		t.Errorf("target 1 %v", t1)
	}
	if obs := t0["observable"].(map[string]any); obs["runs"] != 1.0 || obs["from"] == nil {
		t.Errorf("observable %v", obs)
	}
	// Comets are flagged; Bortle without SQM is the flag's; the filter with -filter.
	cfg.Top, cfg.SQM, cfg.Bortle, cfg.Filter, cfg.FilterNM, cfg.Framing = 3, 0, 5, "oiii", 3, false
	res[1].Filter = "oiii"
	out = mutCapture(t, func() { _ = JSON(cfg, s, "", nil, res) })
	doc = nil
	_ = json.Unmarshal([]byte(out), &doc)
	skyJ = doc["sky"].(map[string]any)
	if skyJ["bortle"] != 5.0 || !reflect.DeepEqual(skyJ["filter"], map[string]any{"mode": "oiii", "bandwidth": 3.0}) || skyJ["zenithMag"] != 20.55 {
		t.Errorf("sky %v", skyJ)
	}
	if _, ok := doc["frame"]; ok {
		t.Error("frame without framing")
	}
	// Broadband filters have their own passbands: no -filter-nm to report.
	cfg.Filter = "cls,lpro"
	out = mutCapture(t, func() { _ = JSON(cfg, s, "", nil, res) })
	var bb map[string]any
	_ = json.Unmarshal([]byte(out), &bb)
	if f := bb["sky"].(map[string]any)["filter"]; !reflect.DeepEqual(f, map[string]any{"mode": "cls,lpro"}) {
		t.Errorf("broadband filter %v, want no bandwidth", f)
	}
	cfg.Filter = "oiii"
	// A comet's of-date track point is moved back to J2000, as the CSV's.
	cra, cdec := J2000(s, res[2].Target)
	if c := doc["targets"].([]any)[2].(map[string]any); c["comet"] != true || c["ra"] != cra || c["dec"] != cdec || cra == 3 || c["surfaceBrightness"] != nil || c["size"] != nil {
		t.Errorf("comet %v, want J2000 %v %v", c, cra, cdec)
	}
	if _, ok := doc["plan"]; ok {
		t.Error("empty plan emitted")
	}
	// A target's FILTER, left out when none helped.
	if tg := doc["targets"].([]any); tg[1].(map[string]any)["filter"] != "oiii" || tg[0].(map[string]any)["filter"] != nil {
		t.Errorf("filter %v / %v", tg[1].(map[string]any)["filter"], tg[0].(map[string]any)["filter"])
	}
}

func TestMutSubsText(t *testing.T) {
	for _, c := range []struct {
		e          *scoring.Estimate
		code, text string
	}{
		{nil, "39", "-"},
		{&scoring.Estimate{Good: 120, Max: 400}, "39", "120"},
		{&scoring.Estimate{Good: 120, Max: 400, Assumed: true}, "39", "~120"},
		{&scoring.Estimate{Good: 500, Max: 400}, "33", "1.2 nights"},
		{&scoring.Estimate{Good: 500, Max: 400, Assumed: true}, "33", "1.2 nights"},
		{&scoring.Estimate{Good: 3980, Max: 400}, "33", "10 nights"},
		{&scoring.Estimate{Good: 39_799, Max: 400}, "33", "99 nights"},
		{&scoring.Estimate{Good: 1_000_000_000, Max: 400}, "33", ">99 nights"},
		{&scoring.Estimate{Good: 5, Max: 0}, "33", ">0"},
		{&scoring.Estimate{Good: 1_000_000_000, Max: 400, SNRLimit: 9.5}, "33", "never"},
		{&scoring.Estimate{Good: 1_000_000_000, Max: 400, SNRLimit: 10}, "33", "never"},
		{&scoring.Estimate{Good: 300, Max: 400, SNRLimit: 14}, "39", "300"},
	} {
		if code, text := SubsText(c.e); code != c.code || text != c.text {
			t.Errorf("%+v: %q %q, want %q %q", c.e, code, text, c.code, c.text)
		}
	}
}

// A terminal narrower than the table cuts the descriptions, and only them,
// by the overflow, never below minDesc; a wide one or none changes nothing.
func TestMutResultsFit(t *testing.T) {
	cfg, s := mutSky(t)
	res := mutResults(s)
	res[0].Description = strings.Repeat("x", 50) // no comma, so cut exactly
	mutColor(t, false)
	t.Cleanup(func() { TermWidth = 0 })
	var full int
	wide := mutCapture(t, func() { full = Results(cfg, s, res) })
	if !strings.Contains(wide, strings.Repeat("x", descWidth-1)+"…") {
		t.Fatalf("not cut to descWidth:\n%s", wide)
	}
	TermWidth = full + 5
	if got := mutCapture(t, func() { Results(cfg, s, res) }); got != wide {
		t.Errorf("a wide terminal changed the table:\n%s", got)
	}
	TermWidth = full - 10
	var w int
	got := mutCapture(t, func() { w = Results(cfg, s, res) })
	if w != TermWidth || !strings.Contains(got, " "+strings.Repeat("x", descWidth-11)+"… ") {
		t.Errorf("width %d on a %d-column terminal (table %d):\n%s", w, TermWidth, full, got)
	}
	TermWidth = 20
	got = mutCapture(t, func() { w = Results(cfg, s, res) })
	if w != 20 || !strings.Contains(got, " "+strings.Repeat("x", minDesc-1)+"… ") {
		t.Errorf("floor: width %d, want the legend's 20, descriptions at %d:\n%s", w, minDesc, got)
	}
	// Below descKeep RA and DEC go first, from the table and its legend.
	header := func(out string) []string { return strings.Fields(mutTable(t, out, "Targets, ranked by SCORE")[0]) }
	h20 := header(got)
	if h := h20; slices.Contains(h, "RA") || slices.Contains(h, "DEC") {
		t.Errorf("RA/DEC kept on a 20-column terminal: %q", h)
	}
	if l := mutCapture(t, func() { Legend(cfg, s, 20) }); strings.Contains(l, "    RA ") {
		t.Errorf("legend explains a dropped RA:\n%s", l)
	}
	// Every column of dropOrder goes before the names fall below descKeep.
	for _, g := range dropOrder {
		if slices.ContainsFunc(h20, func(c string) bool { return slices.Contains(g, c) }) {
			t.Errorf("%v kept on a 20-column terminal: %q", g, h20)
		}
	}
	TermWidth = full - 10 // descriptions still above descKeep
	if h := header(mutCapture(t, func() { Results(cfg, s, res) })); !slices.Contains(h, "RA") {
		t.Errorf("RA dropped with room for the descriptions: %q", h)
	}
	// With framing SIZE goes right after SB, the rest in dropOrder's order.
	framed := framedOrder(dropOrder)
	sb := slices.IndexFunc(framed, func(g []string) bool { return slices.Contains(g, "SB") })
	if len(framed) != len(dropOrder) || !slices.Equal(framed[sb+1], []string{sizeColumn}) ||
		!slices.EqualFunc(slices.DeleteFunc(slices.Clone(framed), func(g []string) bool { return slices.Contains(g, sizeColumn) }),
			slices.DeleteFunc(slices.Clone(dropOrder), func(g []string) bool { return slices.Contains(g, sizeColumn) }), slices.Equal) {
		t.Errorf("framedOrder = %v", framed)
	}
	// STAR, a warning only, is the first column to go: at the widest
	// terminal without it every other column is still there.
	for TermWidth = full; TermWidth > 20; TermWidth-- {
		if h := header(mutCapture(t, func() { Results(cfg, s, res) })); !slices.Contains(h, starColumn) {
			if !slices.Contains(h, "RA") || !slices.Contains(h, "CON") {
				t.Errorf("STAR dropped only after RA or CON, at %d columns: %q", TermWidth, h)
			}

			break
		}
	}
}

// SCORE keeps its absolute colour on every row; TYPE goes plain when every
// row shares one.
func TestMutColumnColors(t *testing.T) {
	cfg, s := mutSky(t)
	res := mutResults(s)[:2]
	res[0].Type, res[1].Type = "HII region", "Emission Nebula"
	res[0].Score, res[1].Score = 0.1, 0.2
	mutColor(t, true)
	out := mutCapture(t, func() { Results(cfg, s, res) })
	if strings.Contains(out, "\x1b[35mHII region") || !strings.Contains(out, "\x1b[39mHII region") {
		t.Errorf("uniform TYPE coloured:\n%q", out)
	}
	if !strings.Contains(out, "\x1b[31m0.10") || !strings.Contains(out, "\x1b[31m0.20") {
		t.Errorf("poor SCORE not red on every row:\n%q", out)
	}
}

// The legend explains only the tables that were printed, each description
// wrapped so that no line passes the table's width.
func TestMutLegend(t *testing.T) {
	cfg, s := mutSky(t)
	mutColor(t, false)
	out := mutCapture(t, func() { Legend(cfg, s, 0) })
	for _, absent := range []string{"Forecast:", "Night plan:", "    FILTER", "    PX", "    SUBS"} {
		if strings.Contains(out, absent) {
			t.Errorf("legend has %q without its table or flag:\n%s", absent, out)
		}
	}
	if !strings.HasPrefix(out, "\nLegend\n  Targets:\n    #  ") || !strings.Contains(out, "green good, yellow fair, red poor\n") || !strings.Contains(out, "UP, SKY and TYPE are plain when every row is alike") {
		t.Errorf("legend:\n%s", out)
	}
	// width: every line within it, the Colors lines aside (fixed text).
	check := func(width int) []string {
		t.Helper()
		out := mutCapture(t, func() { Legend(cfg, s, width) })
		body, _, _ := strings.Cut(out, "  Colors:")
		lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		for _, l := range lines {
			if n := utf8.RuneCountInString(l); n > width {
				t.Errorf("%d-rune line on a %d-column table: %q", n, width, l)
			}
		}

		return lines
	}
	// The widest column name, not the section title, sets the indent here.
	check(80)
	// The plan's FILTER only with -filter.
	cfg.Plan = time.Hour
	if out := mutCapture(t, func() { Legend(cfg, s, 0) }); !strings.Contains(out, "  Night plan:") || strings.Contains(out, "    FILTER") {
		t.Errorf("legend with -plan, no -filter:\n%s", out)
	}
	s.Weather = map[int64]weather.HourWeather{s.Start.Unix(): {}}
	cfg.Filter, cfg.Framing = "dual", true
	lines := check(100)
	text := strings.Join(lines, "\n")
	f, p, tg := strings.Index(text, "  Forecast:"), strings.Index(text, "  Night plan:"), strings.Index(text, "  Targets:")
	if f < 0 || p < f || tg < p {
		t.Errorf("sections missing or out of order:\n%s", text)
	}
	for _, want := range []string{"\n    CLOUD ", "\n    FILTER ", "\n    PX ", "\n    SUBS "} {
		if !strings.Contains(text, want) {
			t.Errorf("legend lacks %q:\n%s", want, text)
		}
	}
	narrow := check(60)
	if len(narrow) <= len(lines) {
		t.Errorf("60 columns gave %d lines, 100 gave %d", len(narrow), len(lines))
	}
	// Continuation lines start in the description column of the first.
	desc := strings.Index(narrow[3], "local hour")
	for _, l := range narrow[3:] {
		if !strings.HasSuffix(l, ":") && (len(l) <= desc || l[desc-1] != ' ' || l[desc] == ' ') {
			t.Errorf("description not in column %d: %q", desc, l)
		}
	}
	// The name is written once, on a description's first line.
	if !slices.ContainsFunc(narrow[3:], func(l string) bool { return len(l) > desc && strings.TrimSpace(l[:desc]) == "" }) {
		t.Errorf("no continuation line with a blank name:\n%s", strings.Join(narrow, "\n"))
	}
	// A terminal narrower than the names still leaves 20 runes of words.
	out = mutCapture(t, func() { Legend(cfg, s, 1) })
	if !strings.Contains(out, "local hour") {
		t.Errorf("legend under the floor broke short descriptions:\n%s", out)
	}
}

// A colour every row shares is dropped, a varied one kept, and one row
// keeps its colour.
func TestVaried(t *testing.T) {
	code := func(v float64) string { return scale(v, 0.66, 0.33) }
	if got := varied([]float64{0.1, 0.2}, code); !slices.Equal(got, []string{"39", "39"}) {
		t.Errorf("uniform red = %q, want plain", got)
	}
	if got := varied([]float64{0.9, 0.2}, code); !slices.Equal(got, []string{"32", "31"}) {
		t.Errorf("varied = %q, want green, red", got)
	}
	if got := varied([]float64{0.1}, code); !slices.Equal(got, []string{"31"}) {
		t.Errorf("one row = %q, want red", got)
	}
}

// A narrow terminal drops the plan's TYPE, then PEAK ALT, from the table and
// its legend, before cutting names; what to shoot when, and how well, stay.
func TestMutPlanFit(t *testing.T) {
	cfg, s := mutSky(t)
	res := mutResults(s)
	res[0].Description = "Andromeda Galaxy"
	cfg.Plan = time.Hour
	slots := []plan.Slot{{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.7, PeakAlt: 66, PeakAt: s.Grid[59]}, {Start: s.Grid[60], End: s.End}}
	mutColor(t, false)
	t.Cleanup(func() { TermWidth = 0 })
	header := func() []string {
		return strings.Fields(mutTable(t, mutCapture(t, func() { Plan(cfg, s, slots) }), "Night plan")[0])
	}
	full := header()
	if !slices.Contains(full, "TYPE") || !slices.Contains(full, "PEAK") {
		t.Fatalf("wide plan lacks columns: %q", full)
	}
	TermWidth = 56
	h := header()
	if slices.Contains(h, "TYPE") || slices.Contains(h, "PEAK") {
		t.Errorf("TYPE/PEAK ALT kept on a 56-column terminal: %q", h)
	}
	for _, want := range []string{"TIME", "NAME", "BLOCK", "SCORE"} {
		if !slices.Contains(h, want) {
			t.Errorf("%s dropped: %q", want, h)
		}
	}
	if out := mutCapture(t, func() { Plan(cfg, s, slots) }); !strings.Contains(out, "Andromeda Galaxy") {
		t.Errorf("name cut while columns could go:\n%s", out)
	}
	if l := mutCapture(t, func() { Legend(cfg, s, 0) }); strings.Contains(l, "PEAK ALT") {
		t.Errorf("legend explains a dropped PEAK ALT:\n%s", l)
	}
}

// Header values wrap at the terminal under their own first column.
func TestMutHeaderWrap(t *testing.T) {
	cfg, s := mutSky(t)
	mutColor(t, true)
	t.Cleanup(func() { TermWidth = 0 })
	TermWidth = 60
	out := mutStrip(mutCapture(t, func() { Header(cfg, s, "") }))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	wrapped := false
	for _, l := range lines {
		if n := utf8.RuneCountInString(l); n > 60 {
			t.Errorf("%d-column line on a 60-column terminal: %q", n, l)
		}
		if strings.HasPrefix(l, " ") {
			wrapped = true
			if !strings.HasPrefix(l, strings.Repeat(" ", headerIndent)) || l[headerIndent] == ' ' {
				t.Errorf("continuation not under the values: %q", l)
			}
		}
	}
	if !wrapped {
		t.Errorf("nothing wrapped on 60 columns:\n%s", out)
	}
}

// With no target the output says which limits to loosen, and the legend
// explains no table that was not printed; an empty plan is one line.
func TestMutEmptyState(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.SizeMin, cfg.SizeMax, cfg.AltMin, cfg.AltMax, cfg.Types = 290, 300, 30, 80, []string{"galaxy"}
	mutColor(t, false)
	out := mutCapture(t, func() { Results(cfg, s, nil) })
	for _, want := range []string{"No objects within the limits", "size 290.0'-300.0'", "altitude 30-80°", "type galaxy", "-size-min/-size-max", "-type"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "-min-run") || strings.Contains(out, "-tol") || strings.Contains(out, "-horizon") || strings.Contains(out, "-skip") {
		t.Errorf("empty state names unset limits: %q", out)
	}
	// The Moon's distance while it is up, which only -filter loosens, is
	// named; with the Moon down all night it is not.
	if !strings.Contains(out, "at least 42° from the Moon while it is up)") {
		t.Errorf("empty state lacks the Moon limit: %q", out)
	}
	// A filter loosens nothing for -type galaxy: no emission-line target to
	// pass it.
	cfg.Filter, cfg.FilterNM = "ha", 7
	if out := mutCapture(t, func() { Results(cfg, s, nil) }); strings.Contains(out, "through a filter") {
		t.Errorf("empty state names a filter's Moon limit for galaxies: %q", out)
	}
	cfg.Types = nil
	if out := mutCapture(t, func() { Results(cfg, s, nil) }); !strings.Contains(out, "while it is up (down to 13° through a filter)") {
		t.Errorf("empty state lacks the filter's Moon limit: %q", out)
	}
	// lpro dims the moonlight less: 0.8 of the limit, not the narrowband floor.
	cfg.Filter = "lpro"
	if out := mutCapture(t, func() { Results(cfg, s, nil) }); !strings.Contains(out, "while it is up (down to 34° through a filter)") {
		t.Errorf("empty state misstates lpro's Moon limit: %q", out)
	}
	cfg.Filter, cfg.Types = "", []string{"galaxy"}
	cfg.Horizon, cfg.Skip = horizon.Horizon{{0, 10}}, map[string]bool{"m31": true}
	out = mutCapture(t, func() { Results(cfg, s, nil) })
	for _, want := range []string{"the local horizon", "1 skipped", "-horizon", "-skip"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state lacks %q: %q", want, out)
		}
	}
	cfg.Horizon, cfg.Skip = nil, nil
	moonAlt := s.MoonAlt
	s.MoonAlt = make([]float64, len(moonAlt))
	for i := range s.MoonAlt {
		s.MoonAlt[i] = -5
	}
	if out := mutCapture(t, func() { Results(cfg, s, nil) }); strings.Contains(out, "Moon") {
		t.Errorf("empty state names a Moon that is down: %q", out)
	}
	s.MoonAlt = moonAlt
	if l := mutCapture(t, func() { Legend(cfg, s, 0) }); l != "" {
		t.Errorf("legend with no table printed:\n%s", l)
	}
	cfg.Plan = time.Hour
	empty := mutCapture(t, func() { Plan(cfg, s, []plan.Slot{{Start: s.Grid[0], End: s.End}}) })
	if empty != "Night plan\nNo target fits any block.\n\n" {
		t.Errorf("empty plan: %q", empty)
	}
}

// With a mosaic listed PX stays, as it carries the grid SUBS counts per panel.
func TestMutMosaicKeepsPX(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.Framing, cfg.Scale, cfg.FOVShort, cfg.FOVLong = true, 2, 300, 400
	res := mutResults(s)
	res[0].Mosaic = config.Mosaic{Cols: 2, Rows: 1}
	mutColor(t, false)
	t.Cleanup(func() { TermWidth = 0 })
	TermWidth = 60
	out := mutCapture(t, func() { Results(cfg, s, res) })
	if h := strings.Fields(mutTable(t, out, "Targets, ranked by SCORE")[0]); !slices.Contains(h, "PX") || !strings.Contains(out, " 2x1") {
		t.Errorf("PX dropped with a mosaic listed:\n%s", out)
	}
	res[0].Mosaic = config.Mosaic{}
	out = mutCapture(t, func() { Results(cfg, s, res) })
	if h := strings.Fields(mutTable(t, out, "Targets, ranked by SCORE")[0]); slices.Contains(h, "PX") {
		t.Errorf("PX kept on 60 columns without a mosaic:\n%s", out)
	}
}

// On a phone-width terminal the table drops down to what and when to shoot,
// how well and for how long (its floor then set by the names); the legend,
// its colour lines included, wraps to the width.
func TestMutNarrowFits(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort, cfg.Filter = true, 2, 400, 300, "ha"
	res := mutResults(s)
	mutColor(t, false)
	t.Cleanup(func() { TermWidth = 0 })
	for _, w := range []int{60, 40} {
		TermWidth = w
		table := mutCapture(t, func() { Results(cfg, s, res) })
		if h := strings.Fields(mutTable(t, table, "Targets, ranked by SCORE")[0]); !slices.Equal(h, []string{"#", "NAME", "DESCRIPTION", "SCORE", "OBSERVABLE", "SUBS"}) {
			t.Errorf("%d columns keep %q", w, h)
		}
		for line := range strings.Lines(mutCapture(t, func() { Legend(cfg, s, w) })) {
			if n := visible(strings.TrimRight(line, "\n")); n > w {
				t.Errorf("%d columns: a %d-wide legend line %q", w, n, line)
			}
		}
	}
}

// The page's sky links and the CSV take J2000: a catalogue position as it
// is, a comet's mid-window track point (of date) moved back.
func TestMutJ2000(t *testing.T) {
	_, s := mutSky(t)
	if ra, dec := J2000(s, catalog.Target{RADeg: 10, DecDeg: 20}); ra != 10 || dec != 20 {
		t.Errorf("catalogue target moved: %v %v", ra, dec)
	}
	comet := catalog.Target{Track: [][2]float64{{1, 2}, {3, 4}, {5, 6}}}
	ra, dec := J2000(s, comet)
	t0 := astro.DaysJ2000(s.Grid[len(s.Grid)/2]) / 36525
	if back, d := astro.PrecessT(ra, dec, t0); math.Abs(back-3) > 1e-3 || math.Abs(d-4) > 1e-3 || math.Abs(ra-3) < 0.1 {
		t.Errorf("comet J2000 %v %v, of date again %v %v", ra, dec, back, d)
	}
}

// A forecast with no usable hour replaces the tables with one line: cloud
// when the weather alone does it, else the seeing.
func TestMutClouded(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.Plan = time.Hour
	s.Quality = make([]float64, len(s.Grid))
	for i := range s.Quality {
		s.Quality[i] = scoring.MinQuality / 2
	}
	res := mutResults(s)
	washed := func() string { return mutCapture(t, func() { Results(cfg, s, res) }) }
	if out := washed(); out != washoutText(s, "cloud")+"\n" {
		t.Errorf("clouded results: %q", out)
	}
	if out := mutCapture(t, func() { Plan(cfg, s, []plan.Slot{{Start: s.Grid[0], End: s.End, Result: &res[0]}}) }); out != "" {
		t.Errorf("clouded plan: %q", out)
	}
	if l := mutCapture(t, func() { Legend(cfg, s, 0) }); l != "" {
		t.Errorf("legend with no table printed:\n%s", l)
	}
	// 59 clear minutes in a row are still no session; 60 are.
	for i := 30; i < 89; i++ {
		s.Quality[i] = scoring.MinQuality
	}
	if out := washed(); out != washoutText(s, "cloud")+"\n" {
		t.Errorf("59 usable minutes: %q", out)
	}
	s.Quality[89] = scoring.MinQuality
	if out := washed(); strings.Contains(out, "No target is observable") {
		t.Errorf("an hour usable still clouded out: %q", out)
	}
	// Seeing at the limit in one of those minutes breaks the hour.
	s.Seeing = make([]float64, len(s.Grid))
	for i := range s.Seeing {
		s.Seeing[i] = 2
	}
	s.Seeing[60] = scoring.MaxSeeing
	if out := washed(); out != washoutText(s, "seeing")+"\n" {
		t.Errorf("bad-seeing results: %q", out)
	}
	s.Seeing[60] = scoring.MaxSeeing - 0.1
	if out := washed(); strings.Contains(out, "No target is observable") {
		t.Errorf("seeing under the limit still washed out: %q", out)
	}
	if got := washoutText(s, "cloud"); !strings.Contains(got, "no 60 minutes in a row at") {
		t.Errorf("cloud text: %q", got)
	}
	// Minutes the forecast does not cover hold only the rest's mean, so an
	// hour of them is no washout.
	kept := slices.Clone(s.Quality)
	for i := range s.Quality {
		s.Quality[i] = 0
	}
	s.Covered = make([]bool, len(s.Grid))
	for i := range 60 {
		s.Covered[i] = true
	}
	if s.Washout() || s.Clouded() {
		t.Error("an uncovered hour washed out")
	}
	s.Covered[60] = true // 59 uncovered minutes left
	if !s.Washout() {
		t.Error("59 uncovered minutes kept the list")
	}
	s.Covered, s.Quality = nil, kept
	// A window shorter than an hour needs every minute, and says so.
	s.Grid, s.Quality, s.Seeing = s.Grid[:30], s.Quality[30:60], s.Seeing[30:60]
	if s.Washout() {
		t.Error("a 30-minute window usable throughout washed out")
	}
	s.Quality[29] = 0
	if !s.Washout() {
		t.Error("a 30-minute window with an unusable minute not washed out")
	}
	if got := washoutText(s, "cloud"); !strings.Contains(got, "no 30 minutes in a row (the whole window) at") {
		t.Errorf("short-window text: %q", got)
	}
}

// The forecast table and JSON add the hour the window's last minutes round
// to, when the forecast has it, since those minutes read its cloud.
func TestMutLastHour(t *testing.T) {
	_, s := mutSky(t)
	last := s.Grid[len(s.Grid)-1]
	if got := s.LastHour(); !got.Equal(last.Truncate(time.Hour)) {
		t.Errorf("without the next hour: %v", got)
	}
	s.Weather = map[int64]weather.HourWeather{s.End.Unix(): {}}
	if got := s.LastHour(); !got.Equal(s.End) {
		t.Errorf("with the next hour: %v, want %v", got, s.End)
	}
	s.Grid = s.Grid[:len(s.Grid)-30] // last minute 21:29 rounds down
	if got := s.LastHour(); !got.Equal(last.Truncate(time.Hour)) {
		t.Errorf("rounding down: %v", got)
	}
}

// The hour after the window shows its cloud, not the rain and gusts that
// fall after it, in the table and in JSON, which also flags a washout.
func TestMutAfterHour(t *testing.T) {
	cfg, s := mutSky(t)
	s.Weather = map[int64]weather.HourWeather{s.End.Unix(): {Cloud: 63, Mid: 63, Temp: 5, DewPoint: 4, Wind: 2, Gust: 30, Precip: 0.5}}
	mutColor(t, false)
	out := mutCapture(t, func() { Weather(cfg, s) })
	lines := mutTable(t, out, "Forecast")
	last := lines[len(lines)-1]
	if f := strings.Fields(last); len(f) != 8 || f[1] != "63%" || f[4] != "-" || f[7] != "-" {
		t.Errorf("hour after the window: %q", last)
	}
	s.Quality = make([]float64, len(s.Grid)) // all clouded
	doc := map[string]any{}
	if err := json.Unmarshal([]byte(mutCapture(t, func() {
		if err := JSON(cfg, s, "", nil, nil); err != nil {
			t.Error(err)
		}
	})), &doc); err != nil {
		t.Fatal(err)
	}
	if sk := doc["sky"].(map[string]any); sk["washout"] != true || sk["washoutReason"] != "cloud" {
		t.Errorf("sky %v", sk)
	}
	fc := doc["forecast"].([]any)
	h := fc[len(fc)-1].(map[string]any)
	if h["cloud"] != 63.0 || h["dewSpread"] != 1.0 || h["precipitation"] != nil || h["gust"] != nil || h["wind"] != nil || h["extinction"] != nil {
		t.Errorf("hour after the window %v", h)
	}
}

func TestMutEclipseText(t *testing.T) {
	cfg, s := mutSky(t)
	if got := eclipseText(s); got != "" || eclipseJSON(s) != nil || MoonFloor(cfg, s) != "" {
		t.Errorf("no eclipse: %q %v", got, eclipseJSON(s))
	}
	// Penumbral from 20:10, umbral 20:40-21:20 with totality 21:00-21:09,
	// still penumbral at the window's end.
	s.Eclipse = make([]astro.Eclipse, len(s.Grid))
	for i := range s.Eclipse {
		switch {
		case i >= 60 && i < 70:
			s.Eclipse[i] = astro.Eclipse{Phase: astro.Total}
		case i >= 40 && i < 80:
			s.Eclipse[i] = astro.Eclipse{Phase: astro.Partial}
		case i >= 10:
			s.Eclipse[i] = astro.Eclipse{Phase: astro.Penumbral}
		default:
			s.Eclipse[i] = astro.Eclipse{Light: 1}
		}
	}
	want := "; lunar eclipse, penumbral 20:10-22:00, umbral 20:40-21:20, total 21:00-21:10 (dimming its light)"
	if got := eclipseText(s); got != want {
		t.Errorf("eclipseText = %q, want %q", got, want)
	}
	// The Moon limit, 42°, falls with the least light left: none in totality
	// (the zero Eclipses here), half of it in a shallow eclipse.
	if got := MoonFloor(cfg, s); got != " (down to 0° in the eclipse)" {
		t.Errorf("moonFloor in totality = %q", got)
	}
	saved := slices.Clone(s.Eclipse)
	for i := range s.Eclipse {
		s.Eclipse[i].Light = max(0.5, s.Eclipse[i].Light)
	}
	if got := MoonFloor(cfg, s); got != " (down to 21° in the eclipse)" {
		t.Errorf("moonFloor at half light = %q", got)
	}
	// A filter's 0.3 (minMoonK, Hα on an H II region) applies in the
	// eclipse too: 42° × 0.5 × 0.3.
	cfg.Filter, cfg.FilterNM = "ha", 7
	if got := MoonFloor(cfg, s); got != " (down to 13° through a filter, 6° in the eclipse)" {
		t.Errorf("moonFloor at half light through a filter = %q", got)
	}
	// A grazing eclipse's 99.9% rounds to the filter's own 13°: not repeated.
	for i := range s.Eclipse {
		s.Eclipse[i].Light = max(0.999, s.Eclipse[i].Light)
	}
	if got := MoonFloor(cfg, s); got != " (down to 13° through a filter)" {
		t.Errorf("moonFloor in a grazing eclipse through a filter = %q", got)
	}
	cfg.Filter = ""
	s.Eclipse = saved
	e := eclipseJSON(s)
	if e == nil || !e.Penumbral.To.Equal(s.End) || !e.Umbral.From.Equal(s.Grid[40]) || !e.Total.To.Equal(s.Grid[70]) {
		t.Errorf("eclipseJSON = %+v", e)
	}
	// A penumbral eclipse alone has no umbral or total stretch.
	for i := range s.Eclipse {
		s.Eclipse[i].Phase = min(s.Eclipse[i].Phase, astro.Penumbral)
	}
	if e := eclipseJSON(s); e == nil || e.Umbral != nil || e.Total != nil {
		t.Errorf("penumbral eclipseJSON = %+v", e)
	}
}
