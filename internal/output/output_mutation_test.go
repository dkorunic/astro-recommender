// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// mutCapture returns what f prints to stdout.
func mutCapture(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	os.Stdout = old

	return <-done
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
	s.MoonAlt[0] = 0 // exactly on the horizon is down
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
		s.MoonAlt[i] = -1
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

// Colors must not move columns: stripped of escapes the colored table is
// the plain one.
func TestMutResultsAlignment(t *testing.T) {
	cfg, s := mutSky(t)
	cfg.Framing, cfg.Scale, cfg.FOVShort, cfg.FOVLong = true, 2, 300, 400
	res := mutResults(s)
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
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
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
	if f[0] != "1" || f[len(f)-1] != "5700" || f[len(f)-2] != "21.0" {
		t.Errorf("row %q", f)
	}
	if !strings.Contains(lines[2], " -  ") || !strings.HasSuffix(strings.TrimRight(lines[3], " "), "-") {
		t.Errorf("unknown size / comet rows:\n%s", plain)
	}
	cfg.Top = 2
	if out := mutCapture(t, func() { Results(cfg, s, res) }); strings.Count(out, "\n") != 3 {
		t.Errorf("-n 2 printed:\n%s", out)
	}
	if out := mutCapture(t, func() {
		if w := Results(cfg, s, nil); w != 0 {
			t.Errorf("empty width %d", w)
		}
	}); !strings.Contains(out, "No objects") {
		t.Errorf("empty: %q", out)
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
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("weather table:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "20:00") || !strings.Contains(lines[1], "20% 0.1mm") || !regexp.MustCompile(`~\d\.\d"`).MatchString(lines[1]) || !strings.Contains(lines[1], " 4.0°C") || strings.Contains(lines[1], "-4.0") || !strings.Contains(lines[1], "8/15 km/h") {
		t.Errorf("row 1 %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "21:00") || strings.Contains(lines[2], "mm") || !strings.Contains(lines[2], "2/8") || !strings.Contains(lines[2], `0.75-1"`) {
		t.Errorf("row 2 %q", lines[2])
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
	_, s := mutSky(t)
	res := mutResults(s)
	slots := []plan.Slot{
		{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.7, PeakAlt: 66, PeakAt: s.Grid[59]},
		{Start: s.Grid[60], End: s.End},
	}
	mutColor(t, false)
	out := mutCapture(t, func() { Plan(s, slots) })
	if !strings.Contains(out, "20:00-21:00") || !strings.Contains(out, "0.70") || !strings.Contains(out, "66° @ 20:59") || !strings.Contains(out, "no target") {
		t.Errorf("plan:\n%s", out)
	}
	mutColor(t, true)
	if colored := mutCapture(t, func() { Plan(s, slots) }); mutStrip(colored) != out {
		t.Errorf("colored plan misaligned:\n%s", mutStrip(colored))
	}
}

func TestMutHeader(t *testing.T) {
	cfg, s := mutSky(t)
	mutColor(t, false)
	s.Ext[10] = 0.24
	out := mutCapture(t, func() { Header(cfg, s, "Zagreb") })
	for _, want := range []string{
		"Location: 45.8100, 15.9800 (Zagreb), Europe/Zagreb",
		"Window:   2026-01-15 20:00 - 2026-01-15 22:00 (2h0m0s)\n",
		"Moon:     42% illuminated, min separation 42°, rises 20:30, sets 21:15",
		"Size:     10.0' - 300.0'",
		"0 comets brighter than mag 12.0",
		"Bortle not set (dark sky, 22.0 mag/arcsec²), extinction 0.20-0.24 (default) mag/airmass",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	s.Ext[10] = 0.204
	cfg.SQM, cfg.SQMSource, cfg.ExtinctionSet, cfg.Twilight = 21.6, "DSS", true, "nautical"
	cfg.Filter, cfg.FilterK, cfg.NoComets = true, 0.25, true
	cfg.RASet, cfg.RA, cfg.Tol = true, 187.5, 5
	cfg.MinRun = time.Hour
	out = mutCapture(t, func() { Header(cfg, s, "") })
	for _, want := range []string{
		"Location: 45.8100, 15.9800, Europe/Zagreb",
		"(2h0m0s, nautical night)",
		"SQM 21.60 mag/arcsec² (DSS, ≈ Bortle 2)",
		"extinction 0.20 (set) mag/airmass, filter k=0.25",
		"GaryImm, comets off",
		"Region:   within 5° of RA 12 30.0",
		"Min run:  at least 1h0m0s",
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
	for _, want := range []string{"nautical night 20:00 - 22:00", "Bortle 6 (zenith 18.80 mag/arcsec²)", "(elevation 120 m, aerosols)"} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
	s.AOD = nil
	if out = mutCapture(t, func() { Header(cfg, s, "") }); !strings.Contains(out, "(elevation 120 m, typical aerosols)") {
		t.Errorf("header without AOD:\n%s", out)
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
	res := mutResults(s)
	slots := []plan.Slot{{Start: s.Grid[0], End: s.Grid[60], Result: &res[0], Score: 0.7, PeakAlt: 66, PeakAt: s.Grid[59]}, {Start: s.Grid[60], End: s.End}}
	out := mutCapture(t, func() {
		if err := JSON(cfg, s, "Zagreb", slots, res); err != nil {
			t.Error(err)
		}
	})
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	skyJ := doc["sky"].(map[string]any)
	if skyJ["bortle"] != float64(atmos.BortleClass(20.9)) || skyJ["bortle"] != 4.0 || skyJ["zenithMag"] != 20.9 || skyJ["filterK"] != nil {
		t.Errorf("sky %v", skyJ)
	}
	if fr := doc["frame"].(map[string]any); fr["scale"] != 2.0 || fr["fovShort"] != 300.0 {
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
	pl := doc["plan"].([]any)
	if pl[0].(map[string]any)["name"] != "M 31" || pl[0].(map[string]any)["peakAt"] == nil || pl[1].(map[string]any)["name"] != nil || pl[1].(map[string]any)["peakAt"] != nil {
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
	if t1["rank"] != 2.0 || t1["size"] != nil || t1["px"] != nil {
		t.Errorf("target 1 %v", t1)
	}
	if obs := t0["observable"].(map[string]any); obs["runs"] != 1.0 || obs["from"] == nil {
		t.Errorf("observable %v", obs)
	}
	// Comets are flagged; Bortle without SQM is the flag's; filterK with -filter.
	cfg.Top, cfg.SQM, cfg.Bortle, cfg.Filter, cfg.FilterK, cfg.Framing = 3, 0, 5, true, 0.3, false
	out = mutCapture(t, func() { _ = JSON(cfg, s, "", nil, res) })
	doc = nil
	_ = json.Unmarshal([]byte(out), &doc)
	skyJ = doc["sky"].(map[string]any)
	if skyJ["bortle"] != 5.0 || skyJ["filterK"] != 0.3 || skyJ["zenithMag"] != 19.75 {
		t.Errorf("sky %v", skyJ)
	}
	if _, ok := doc["frame"]; ok {
		t.Error("frame without framing")
	}
	if c := doc["targets"].([]any)[2].(map[string]any); c["comet"] != true || c["ra"] != 3.0 || c["dec"] != 4.0 || c["surfaceBrightness"] != nil || c["size"] != nil {
		t.Errorf("comet %v", c)
	}
	if _, ok := doc["plan"]; ok {
		t.Error("empty plan emitted")
	}
}
