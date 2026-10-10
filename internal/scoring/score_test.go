// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

func TestScoreSizeLimits(t *testing.T) {
	// Circumpolar target, always observable from Zagreb. Framing with an
	// Origin-like frame: 79.2' x 45'.
	cfg := &config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, NoWeather: true, ExtinctionSet: true, Extinction: 0.2,
		Framing: true, FOVLong: 79.2, FOVShort: 45, SizeMin: 0, SizeMax: 45,
	}
	start := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(time.Hour))
	tg := catalog.Target{Name: "X", RADeg: 37.95, DecDeg: 89.26} // Polaris
	big, unknown, half := tg, tg, tg
	big.Size, unknown.Size, half.Size = 60, -9999, 22.5

	res := Score(cfg, &s, []catalog.Target{big, unknown, half}, false)
	if len(res) != 2 {
		t.Fatalf("got %d results, want unknown and 22.5' only", len(res))
	}
	for _, r := range res {
		if r.Frame != 1 {
			t.Errorf("size %v: Frame = %v, want 1", r.Size, r.Frame)
		}
	}
	// 40' fills 89% of the short side and gets a frame penalty; -size-min 10 drops unknown sizes.
	cfg.SizeMin = 10
	big.Size = 40
	res = Score(cfg, &s, []catalog.Target{big, unknown}, false)
	if len(res) != 1 {
		t.Fatalf("got %d results, want the 40' object only", len(res))
	}
	if res[0].Frame >= 1 || res[0].Score == 0 {
		t.Errorf("40' in a 45' frame: Frame = %v, Score = %v; want 0 < Frame < 1", res[0].Frame, res[0].Score)
	}
	// Exactly the short side cannot be framed and is not listed...
	big.Size = 45
	if res = Score(cfg, &s, []catalog.Target{big}, false); len(res) != 0 {
		t.Errorf("45' in a 45' frame listed with Score %v", res[0].Score)
	}
	// ...but without framing -size-max is inclusive (Rho Ophiuchi is exactly 300').
	cfg.Framing, cfg.SizeMax = false, 45
	if res = Score(cfg, &s, []catalog.Target{big}, false); len(res) != 1 || res[0].Frame != 1 {
		t.Errorf("45' with -size-max 45 and no framing: %d results, want 1 with Frame 1", len(res))
	}
	// Per-minute slices are kept only with perMinute, as each result's own copy.
	if res[0].Alt != nil || res[0].Weight != nil {
		t.Error("per-minute slices kept without perMinute")
	}
	half.Size = 30
	res = Score(cfg, &s, []catalog.Target{big, half}, true)
	if len(res) != 2 || len(res[0].Alt) != len(s.Grid) || &res[0].Alt[0] == &res[1].Alt[0] || res[0].Weight[0] == 0 {
		t.Errorf("perMinute: per-minute slices missing or shared: %d results", len(res))
	}
}

// The -ra/-dec region drops targets outside it before scoring; a comet is
// judged by its mid-track position, not its zero RADeg/DecDeg.
func TestScoreRegion(t *testing.T) {
	cfg := &config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, NoWeather: true, ExtinctionSet: true, Extinction: 0.2,
		SizeMin: 0, SizeMax: 300, RASet: true, DecSet: true, RA: 37.95, Dec: 89.26, Tol: 5,
	}
	start := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(time.Hour))
	in := catalog.Target{Name: "in", RADeg: 37.95, DecDeg: 89.26} // Polaris
	out := catalog.Target{Name: "out", RADeg: 37.95, DecDeg: 80}  // 9° south of it
	comet := catalog.Target{Name: "comet", Track: make([][2]float64, len(s.Grid))}
	for i := range comet.Track {
		comet.Track[i] = [2]float64{37.95, 80} // out of the region...
	}
	comet.Track[len(comet.Track)/2] = [2]float64{37.95, 89.26} // ...except mid-window
	res := Score(cfg, &s, []catalog.Target{in, out, comet}, false)
	if len(res) != 2 || res[0].Name == "out" || res[1].Name == "out" {
		t.Fatalf("got %v, want in and comet only", res)
	}
}

func TestFrameFill(t *testing.T) {
	for fill, want := range map[float64]float64{0.1: 0.4, 0.5: 1, 0.9: 0.5, 1.2: 0} {
		if got := frameFill(fill); math.Abs(got-want) > 1e-9 {
			t.Errorf("frameFill(%v) = %v, want %v", fill, got, want)
		}
	}
}

// frame is frameFill of the major axis over the short side for one frame
// without -rotate, as before mosaics; -rotate lets a known minor axis lie
// across, and a mosaic shares the score among its panels.
func TestFrame(t *testing.T) {
	single := []config.Mosaic{{Cols: 1, Rows: 1, W: 80, H: 45}}
	mosaics := (&config.Config{FOVLong: 80, FOVShort: 45, Mosaic: 4}).Mosaics()
	edgeOn := catalog.Target{Size: 60, Minor: 10}
	for _, c := range []struct {
		name    string
		tg      catalog.Target
		mosaics []config.Mosaic
		rotate  bool
		want    float64
		panels  int
	}{
		{"fits short side", catalog.Target{Size: 20}, single, false, frameFill(20.0 / 45), 1},
		{"edge-on, unknown angle", edgeOn, single, false, 0, 0},
		{"edge-on, rotated", edgeOn, single, true, frameFill(60.0 / 80), 1},
		{"round, rotated", catalog.Target{Size: 60}, single, true, 0, 0},                // minor unknown: a 60' disc
		{"mosaic", catalog.Target{Size: 60}, mosaics, false, frameFill(60.0/80) / 2, 2}, // 1x2: 80x85.5'
		{"too big", catalog.Target{Size: 200}, mosaics, false, 0, 0},
	} {
		got, m := frame(c.tg, c.mosaics, c.rotate)
		if math.Abs(got-c.want) > 1e-9 || m.Panels() != c.panels {
			t.Errorf("%s: %v on %d panels, want %v on %d", c.name, got, m.Panels(), c.want, c.panels)
		}
	}
}

func TestSkyK(t *testing.T) {
	pn := catalog.Target{Type: "Planetary Nebula"}
	gx := catalog.Target{Type: "Galaxy"}
	if !catalog.EmissionLine(pn) || catalog.EmissionLine(gx) || !catalog.EmissionLine(catalog.Target{Name: "IC 1396", Type: "Dark Nebula"}) {
		t.Error("emissionLine misclassifies")
	}
	if skyK(pn, true, 0.2) != 0.2 || skyK(pn, false, 0.2) != 1 || skyK(gx, true, 0.2) != 1 {
		t.Error("skyK misapplies the filter")
	}
	if skyK(catalog.Target{Type: "Open cluster"}, false, 0.2) != 0.5 {
		t.Error("skyK misses LBN's \"Open cluster\" spelling")
	}
	if skyK(catalog.Target{Type: "Cluster Nebulosity"}, false, 0.2) != 1 || skyK(catalog.Target{Name: "M 16", Type: "Open Cluster"}, false, 0.2) != 1 {
		t.Error("skyK halves the sky for a cluster with nebulosity, which is imaged for the glow")
	}
}

// BenchmarkPipeline runs the offline hot path of a typical run (one night's
// sky, then every GaryImmFull target scored minute by minute); task pgo
// profiles it into default.pgo for the release build.
func BenchmarkPipeline(b *testing.B) {
	targets, _, err := catalog.Load("GaryImmFull", "")
	if err != nil {
		b.Fatal(err)
	}
	cfg := &config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true,
		ExtinctionSet: true, Extinction: 0.2, FilterK: 0.25, Filter: true,
	}
	start := time.Date(2026, 10, 5, 18, 8, 0, 0, time.UTC)
	end := start.Add(9*time.Hour + 14*time.Minute)
	for b.Loop() {
		s := BuildSky(cfg, NoForecast(), start, end)
		Score(cfg, &s, targets, false)
	}
}

// BenchmarkPipelineMoon is BenchmarkPipeline on a full-moon night (3 Jan
// 2026), when nearly every observable minute is tested against the Moon,
// which BenchmarkPipeline's 24% Moon rarely reaches; task pgo profiles both.
func BenchmarkPipelineMoon(b *testing.B) {
	targets, _, err := catalog.Load("GaryImmFull", "")
	if err != nil {
		b.Fatal(err)
	}
	cfg := &config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true,
		ExtinctionSet: true, Extinction: 0.2, FilterK: 0.25, Filter: true,
	}
	start, end, ok := astro.Window(time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	if !ok {
		b.Fatal("no night")
	}
	for b.Loop() {
		s := BuildSky(cfg, NoForecast(), start, end)
		Score(cfg, &s, targets, false)
	}
}

func TestScoreSurfaceBrightness(t *testing.T) {
	// Under a Bortle 8 sky the sky-glow weight depends on the object's own
	// surface brightness: a bright-surface object loses little, a faint one a
	// lot, and one without data counts as no object signal at all, the same
	// as an object far fainter than any sky.
	cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, SizeMax: 300, Bortle: 8}
	start := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	polaris := catalog.Target{RADeg: 37.95, DecDeg: 89.26}
	score := func(cfg *config.Config) map[string]Result {
		s := BuildSky(cfg, NoForecast(), start, start.Add(time.Hour))
		out := map[string]Result{}
		for _, sb := range []struct {
			name string
			sb   float64
		}{{"bright", 18}, {"faint", 24}, {"none", 0}, {"dim", 40}} {
			tg := polaris
			tg.Name, tg.SurfBr = sb.name, sb.sb
			for _, r := range Score(cfg, &s, []catalog.Target{tg}, false) {
				out[r.Name] = r
			}
		}

		return out
	}
	r := score(cfg)
	if !(r["bright"].Score > r["faint"].Score && r["faint"].Score > r["none"].Score) {
		t.Errorf("Bortle 8: bright %.4f, faint %.4f, none %.4f; want descending", r["bright"].Score, r["faint"].Score, r["none"].Score)
	}
	if math.Abs(r["none"].Score-r["dim"].Score) > 1e-6 { // 40 mag/arcsec² is ~1e-9 of the sky, not exactly 0
		t.Errorf("no data scores %.6f, a 40 mag/arcsec² object %.6f; want equal", r["none"].Score, r["dim"].Score)
	}
	// Under a pristine sky the object's brightness matters much less (the sky
	// at 46° altitude is still brighter than the zenith reference, so not nothing).
	dark := *cfg
	dark.Bortle = 1
	d := score(&dark)
	if gap, darkGap := r["bright"].Score-r["none"].Score, d["bright"].Score-d["none"].Score; darkGap >= gap/2 {
		t.Errorf("bright-none gap %.4f at Bortle 8, %.4f at Bortle 1; want the dark-sky gap much smaller", gap, darkGap)
	}
}

// A target that crosses -alt-max mid-window is observable in two runs; the
// longest is reported and -min-run judges it, not the total. -skip drops a
// target by normalized name.
func TestScoreRuns(t *testing.T) {
	cfg := &config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, SizeMin: 0, SizeMax: 300,
	}
	start := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(4*time.Hour))
	// Transits the zenith 100 minutes in: above 80° for ~56 minutes either side.
	zenith := catalog.Target{Name: "Zenith X", RADeg: astro.LST(s.Grid[100], cfg.Lon), DecDeg: cfg.Lat}
	polaris := catalog.Target{Name: "Polaris", RADeg: 37.95, DecDeg: 89.26}
	res := Score(cfg, &s, []catalog.Target{zenith, polaris}, false)
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	for _, r := range res {
		switch r.Name {
		case "Polaris":
			if r.Runs != 1 || !r.RunFrom.Equal(s.Start) || !r.RunTo.Equal(s.End) {
				t.Errorf("Polaris: %d runs %v - %v, want 1 over the window", r.Runs, r.RunFrom, r.RunTo)
			}
		case "Zenith X":
			// The setting side is the longer run (more of the window remains).
			if r.Runs != 2 || !r.RunFrom.After(s.Grid[100]) || !r.RunTo.Equal(s.End) || r.Foto >= 1 {
				t.Errorf("zenith target: %d runs %v - %v, foto %v; want 2 with the later one longest", r.Runs, r.RunFrom, r.RunTo, r.Foto)
			}
		}
	}
	cfg.MinRun = 2 * time.Hour // the two runs total more, the longest is less
	if res = Score(cfg, &s, []catalog.Target{zenith, polaris}, false); len(res) != 1 || res[0].Name != "Polaris" {
		t.Errorf("-min-run 2h kept %v, want Polaris only", res)
	}
	cfg.MinRun, cfg.Skip = 0, map[string]bool{catalog.NameKey("polaris"): true}
	if res = Score(cfg, &s, []catalog.Target{zenith, polaris}, false); len(res) != 1 || res[0].Name != "Zenith X" {
		t.Errorf("-skip polaris kept %v", res)
	}
	// A window ending mid-minute (a -to clip in a zone whose offset has
	// seconds) has a last grid minute shorter than a minute: -min-run judges
	// the clock span, so a 30-second window fails a 1-minute minimum.
	cfg.Skip, cfg.MinRun = nil, time.Minute
	short := BuildSky(cfg, NoForecast(), start, start.Add(30*time.Second))
	if res = Score(cfg, &short, []catalog.Target{polaris}, false); len(res) != 0 {
		t.Errorf("30 s window with -min-run 1m kept %v", res)
	}
	cfg.MinRun = 0
	if res = Score(cfg, &short, []catalog.Target{polaris}, false); len(res) != 1 || !res[0].RunTo.Equal(short.End) {
		t.Errorf("30 s window: %v, want Polaris with the run ending at the window's end", res)
	}
}
