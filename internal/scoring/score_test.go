// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"cmp"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
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

// config.MaxFramed and frame encode the fit rule separately: the -size-max
// limit must be exactly the largest major axis some mosaic holds (with a
// known minor axis across under -rotate), a hair below it framed, a hair
// above it not, so an accepted -size-max never admits what frame drops.
func TestMaxFramed(t *testing.T) {
	for _, cfg := range []*config.Config{
		{FOVLong: 80, FOVShort: 45},
		{FOVLong: 80, FOVShort: 45, Rotate: true},
		{FOVLong: 80, FOVShort: 45, Mosaic: 4},
		{FOVLong: 80, FOVShort: 45, Mosaic: 3, Rotate: true},
		{FOVLong: 80, FOVShort: 45, Mosaic: 25, Rotate: true},
	} {
		lim := cfg.MaxFramed()
		under, _ := frame(catalog.Target{Size: lim - 1e-3, Minor: 1}, cfg.Mosaics(), cfg.Rotate)
		over, _ := frame(catalog.Target{Size: lim + 1e-3, Minor: 1}, cfg.Mosaics(), cfg.Rotate)
		if under <= 0 || over != 0 {
			t.Errorf("%vx%v' mosaic %d rotate %v: MaxFramed %v frames %v just under, %v just over",
				cfg.FOVLong, cfg.FOVShort, cfg.Mosaic, cfg.Rotate, lim, under, over)
		}
	}
}

// An object no frame holds whole is scored at its core (catalog.Target.AtCore),
// centred and framed there, its size kept; one a frame holds, or any without
// framing, stays whole.
func TestScoreCore(t *testing.T) {
	tg := catalog.Target{Name: "NGC 7000", Type: "Emission Nebula", RADeg: 314.696, DecDeg: 44.33, Size: 120, Minor: 30}
	for _, tc := range []struct {
		framing     bool
		long, short float64
		core        bool
	}{
		{true, 80, 50, true},
		{true, 150, 130, false},
		{false, 80, 50, false},
	} {
		cfg := mutCfg()
		cfg.Framing, cfg.FOVLong, cfg.FOVShort, cfg.Scale = tc.framing, tc.long, tc.short, 2
		if tc.framing {
			cfg.SizeMax = cfg.MaxFramed()
		}
		rs := Score(cfg, mutSky(400), []catalog.Target{tg}, false)
		if len(rs) != 1 {
			t.Fatalf("%v: %d results", tc, len(rs))
		}
		r := rs[0]
		major, _ := r.FrameAxes()
		if (r.Core != nil) != tc.core || r.Size != 120 || tc.core && (r.RADeg != r.Core.RADeg || major != 45) || !tc.core && r.RADeg != tg.RADeg {
			t.Errorf("%v: core %v at %v, frame axis %v, size %v", tc, r.Core, r.RADeg, major, r.Size)
		}
	}
}

func TestEmissionLineKinds(t *testing.T) {
	pn := catalog.Target{Type: "Planetary Nebula"}
	gx := catalog.Target{Type: "Galaxy"}
	if !catalog.EmissionLine(pn) || catalog.EmissionLine(gx) || !catalog.EmissionLine(catalog.Target{Name: "IC 1396", Type: "Dark Nebula"}) {
		t.Error("emissionLine misclassifies")
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
		ExtinctionSet: true, Extinction: 0.2, Filter: "dual", FilterNM: 7,
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
		ExtinctionSet: true, Extinction: 0.2, Filter: "dual", FilterNM: 7,
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

// Through a filter, worn all night, an emission-line target scores by the
// share of its light in the bands against the bands' share of each part of
// the camera band's sky; no filter wins unless one beats it. Under the
// first-quarter Moon of 26 January 2026 from a Bortle 6 site, a sky-limited
// target in Ursa Major: an H II region gains most from Hα, whose 7 nm band
// also passes [N II] (and less from dual-band, whose [O III] half lets in
// three times the moonlight per nm for under a tenth of its photons),
// nothing from [O III] or [S II]; a planetary most from dual-band, its
// [N II] as bright as Hα putting two-fifths of its photons in the Hα band
// beside [O III]'s 0.42, then Hα, whose band sees less moonlight and
// extinction, then [O III]; a remnant most from Hα with its [N II], then
// quad-band, a little from [S II]; a galaxy or a cluster is never
// filtered.
func TestScoreFilter(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, Bortle: 6, FilterNM: 7}
	start, end, _ := astro.Window(time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	s := BuildSky(cfg, NoForecast(), start, end)
	score := func(filter, typ string) float64 {
		cfg.Filter = filter
		r := Score(cfg, &s, []catalog.Target{{Name: "T", Type: typ, RADeg: 150, DecDeg: 60, Size: 60}}, false)
		if len(r) != 1 {
			t.Fatalf("%s through %q: not observable", typ, filter)
		}

		return r[0].Score
	}
	for _, c := range []struct {
		typ   string
		order []string        // strictly best first
		equal map[string]bool // filters that score as none
	}{
		// Broadband filters fall between the line filters and none on
		// nebulae, the narrower first; continuum (a galaxy, a cluster) loses
		// as much light as sky through them, so they never help it.
		{"HII Region", []string{"ha", "dual", "cls", "lpro", ""}, map[string]bool{"oiii": true, "sii": true}},
		{"Planetary Nebula", []string{"dual", "ha", "oiii", "cls", "lpro", ""}, map[string]bool{"sii": true}},
		{"Supernova Remnant", []string{"ha", "quad", "tri", "cls", "sii", "lpro", ""}, map[string]bool{"oiii": true}},
		{"Spiral Galaxy", []string{""}, map[string]bool{"ha": true, "oiii": true, "sii": true, "dual": true, "tri": true, "quad": true, "cls": true, "lpro": true}},
		{"Globular Cluster", []string{""}, map[string]bool{"cls": true, "lpro": true}},
	} {
		for i := 1; i < len(c.order); i++ {
			if a, b := score(c.order[i-1], c.typ), score(c.order[i], c.typ); a <= b {
				t.Errorf("%s: %q scores %v, not above %q's %v", c.typ, c.order[i-1], a, c.order[i], b)
			}
		}
		none := score("", c.typ)
		if c.typ != "Spiral Galaxy" && c.typ != "Globular Cluster" {
			for _, f := range []string{"dual", "tri", "quad"} {
				if score(f, c.typ) <= none {
					t.Errorf("%s: %s-band does not beat no filter", c.typ, f)
				}
			}
		}
		// A filter that passes too little of the light loses to none, which
		// then scores exactly as without -filter.
		for f := range c.equal {
			if got := score(f, c.typ); got != none {
				t.Errorf("%s through %q: %v, want the unfiltered %v", c.typ, f, got, none)
			}
		}
	}
	// With every part of the sky cut to k and all the light passed, the
	// filtered weight is the broadband one at that k.
	fp := filterPass{w: 1, glow: 0.3, rayleigh: 0.3, mie: 0.3}
	objNL, objRef := atmos.NanoLamberts(21), atmos.NanoLamberts(21)+s.RefNL
	if got, want := min(1, fp.gain(objNL, objRef, 400, 500, 100)), math.Sqrt(objRef/(objNL+0.3*1000)); math.Abs(got-want) > 1e-12 {
		t.Errorf("weight = %v, want the broadband %v", got, want)
	}
	// The Moon limit: through a 7 nm Hα or dual-band filter an H II region
	// would beat the Moon to within a few degrees, which its halo does not
	// allow, so both hold at the floor; [O III], a tenth of its light in
	// the band the Moon scatters most into, does not beat it.
	hii := catalog.Target{Type: "HII Region"}
	ha, dual, o3 := passes(cfg, hii, "ha").moonK(), passes(cfg, hii, "dual").moonK(), passes(cfg, hii, "oiii").moonK()
	if !(ha == minMoonK && dual == minMoonK && o3 == 1) {
		t.Errorf("moonK H II: ha %v, dual %v, oiii %v", ha, dual, o3)
	}

	// Through a broadband filter passing every line an emission-line
	// target keeps all its light (Hβ included), a galaxy the passbands'
	// share of the camera band.
	if w := passes(cfg, hii, "lpro").w; math.Abs(w-1) > 1e-12 {
		t.Errorf("lpro passes %v of an H II region, want 1", w)
	}
	if w := passes(cfg, catalog.Target{Type: "Spiral Galaxy"}, "cls").w; math.Abs(w-0.4) > 1e-12 {
		t.Errorf("cls passes %v of a galaxy, want 0.4", w)
	}

	// A list scores each target with each choice and keeps the best: the
	// same score as that filter alone, named in Filter, whatever the order;
	// "" when no choice beats no filter (a galaxy, an H II region through
	// [S II]).
	result := func(filter, typ string) Result {
		cfg.Filter = filter
		r := Score(cfg, &s, []catalog.Target{{Name: "T", Type: typ, RADeg: 150, DecDeg: 60, Size: 60}}, true)
		if len(r) != 1 {
			t.Fatalf("%s through %q: not observable", typ, filter)
		}

		return r[0]
	}
	for _, c := range []struct{ list, typ, want string }{
		{"dual,ha,oiii", "HII Region", "ha"},
		{"oiii,ha,dual", "HII Region", "ha"},
		{"oiii,ha", "Planetary Nebula", "ha"},
		{"ha,oiii,dual", "Spiral Galaxy", ""},
		{"sii", "HII Region", ""},
		{"dual", "HII Region", "dual"},
		{"lpro,cls", "HII Region", "cls"},
		{"cls,lpro", "Spiral Galaxy", ""},
	} {
		r := result(c.list, c.typ)
		single := result(cmp.Or(c.want, "sii"), c.typ)
		if r.Filter != c.want || r.Score != single.Score || !slices.Equal(r.Weight, single.Weight) {
			t.Errorf("%s through %q: filter %q score %v, want %q and the single filter's %v", c.typ, c.list, r.Filter, r.Score, c.want, single.Score)
		}
	}
}

// The filter is chosen by how much it gains, not by the capped weight: under
// a pristine dark sky every helpful line filter's weight saturates at 1, and
// the cap would leave the band's extinction to choose, Hα's, the lowest,
// for a planetary too, whose light is half [O III]. Sky-limited (no surface
// brightness), [O III] gains it more.
func TestScoreFilterUncapped(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, FilterNM: 7, Filter: "ha,oiii"}
	start, end, _ := astro.Window(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	s := BuildSky(cfg, NoForecast(), start, end)
	for typ, want := range map[string]string{"Planetary Nebula": "oiii", "HII Region": "ha"} {
		r := Score(cfg, &s, []catalog.Target{{Name: "T", Type: typ, RADeg: 300, DecDeg: 40, Size: 20}}, false)
		if len(r) != 1 {
			t.Fatalf("%s: %d results, want 1", typ, len(r))
		}
		if r[0].Filter != want {
			t.Errorf("%s: filter %q, want %q", typ, r[0].Filter, want)
		}
		if r[0].Score > 1 {
			t.Errorf("%s: score %v above 1", typ, r[0].Score)
		}
	}
}

// With a camera (framing) the filter is chosen by the sub-exposure model,
// read noise included, so FILTER is the choice that stacks the most SNR
// tonight and SUBS agrees with it: on the Origin's 15 s colour subs under a
// pristine sky a 7 nm Hα band is read-noise limited, and dual-band (its
// [O III] on the green pixels) or cls, collecting every line over 120 nm,
// needs fewer: ~390 subs each against Hα's ~410.
func TestScoreFilterCamera(t *testing.T) {
	cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, FilterNM: 7, Filter: "ha,dual,cls"}
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort = true, 1.2314, 79.2, 45
	cfg.Aperture, cfg.Obstruction, cfg.Sub, cfg.QE, cfg.ReadNoise = 152, 63, 15, 0.83, 2.15
	start, end, _ := astro.Window(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	s := BuildSky(cfg, NoForecast(), start, end)
	neb := catalog.Target{Name: "N", Type: "HII Region", RADeg: 300, DecDeg: 40, Size: 20} // the typical surface brightness
	r := Score(cfg, &s, []catalog.Target{neb}, false)
	if len(r) != 1 {
		t.Fatal("not observable")
	}
	fewest, best := "", 0
	for _, f := range []string{"", "ha", "dual", "cls"} {
		one := *cfg
		one.Filter = f
		rr := r[0]
		rr.Filter, rr.Obs = f, nil
		if e := Subs(&one, &s, rr, nil); best == 0 || e.Good < best {
			fewest, best = f, e.Good
		}
	}
	if r[0].Filter != fewest || fewest == "ha" || fewest == "" {
		t.Errorf("filter %q, fewest subs %q (%d)", r[0].Filter, fewest, best)
	}

	// Under the first-quarter Moon of 26 January 2026 the choices' minute
	// sets diverge from their sub counts: ~30° from the Moon a bright H II
	// region (SurfBr 20) is observable unfiltered only in the ~50 Moon-down
	// minutes (the separation limit is ~59°), while Hα's moonK floor keeps
	// most of the night — yet unfiltered it needs fewer subs (~12 against
	// ~18: bright enough that the filter only costs light). The choice is the
	// fewest-subs one, the mean per-sub SNR² over each choice's own minutes;
	// the total over its minutes would pick Hα on minutes alone. Subs with a
	// nil Obs must also rescore through the result's own filter, not Score's
	// re-chosen one: the two choices' Max must differ.
	cfg.Filter = "ha"
	start, end, _ = astro.Window(time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
	s = BuildSky(cfg, NoForecast(), start, end)
	moon := s.MoonPos[len(s.Grid)/2]
	neb = catalog.Target{Name: "N", Type: "HII Region", RADeg: moon[0] + 45, DecDeg: moon[1] + 15, Size: 20, SurfBr: 20}
	r = Score(cfg, &s, []catalog.Target{neb}, false)
	if len(r) != 1 {
		t.Fatal("moonlit: not observable")
	}
	goods := map[string]int{}
	var none, ha Estimate
	for f, e := range map[string]*Estimate{"": &none, "ha": &ha} {
		rr := r[0]
		rr.Filter, rr.Obs = f, nil
		*e = Subs(cfg, &s, rr, nil)
		goods[f] = e.Good
		t.Logf("moonlit %4q: good %d, max %d", f, e.Good, e.Max)
	}
	if none.Max == ha.Max {
		t.Error("moonlit: both choices rescored to the same minutes; Subs must count r.Filter's own, not Score's choice")
	}
	if none.Good == 0 || ha.Good == 0 || none.Good >= ha.Good {
		t.Fatalf("moonlit: unfiltered needs %d subs, Hα %d; the geometry no longer separates them", none.Good, ha.Good)
	}
	if r[0].Filter != "" {
		t.Errorf("moonlit: chose %q (%d subs) over no filter (%d): the total won over the mean", r[0].Filter, goods[r[0].Filter], none.Good)
	}
}

// A wide -filter-nm overlaps tri/quad's neighbouring bands (Hβ meets [O III]
// past 14.6 nm, Hα meets [S II] past 16.1): lineBands clips them at the
// lines' midpoint, so passes charges the merged unions' sky once, as a
// physical filter's merged bands would pass it; the default 7 nm bands stay
// untouched.
func TestLineBandsClip(t *testing.T) {
	quad := config.Filters["quad"]
	for i, l := range quad {
		if b, nm := lineBands(quad, flat(7))[i], catalog.LineNM[l]; b[0] != nm-3.5 || b[1] != nm+3.5 {
			t.Errorf("7 nm band %d: %v, want λ ± 3.5", i, b)
		}
	}
	bands := lineBands(quad, flat(30))
	var g, r, m float64
	for i := range quad {
		glow, ray, mie := atmos.BroadSky(bands[i][0], bands[i][1])
		g, r, m = g+glow, r+ray, m+mie
	}
	// BroadSky is additive over wavelength, so the clipped bands summing to
	// the unions carry exactly the unions' sky, wherever the cut falls.
	g1, r1, m1 := atmos.BroadSky(486.1-15, 500.7+15)
	g2, r2, m2 := atmos.BroadSky(656.3-15, 672.4+15)
	if math.Abs(g-(g1+g2)) > 1e-12 || math.Abs(r-(r1+r2)) > 1e-12 || math.Abs(m-(m1+m2)) > 1e-12 {
		t.Errorf("quad at 30 nm: %v %v %v, want the unions' %v %v %v", g, r, m, g1+g2, r1+r2, m1+m2)
	}
	fp := passes(&config.Config{FilterNM: 30}, catalog.Target{Type: "HII Region"}, "quad")
	if fp.glow != g || fp.rayleigh != r || fp.mie != m {
		t.Errorf("passes: %v %v %v, want the clipped bands' %v %v %v", fp.glow, fp.rayleigh, fp.mie, g, r, m)
	}
}

// A standard filter in the original Origin's f/2.2 cone (its 63 mm
// obstruction leaving only the steeper rays) shifts its band blue: a 3 nm Hα band keeps
// a quarter of Hα, most of [N II] 654.8 and none of 658.3; a 7 nm band most
// of Hα, all of 654.8 and a quarter of 658.3. Pinned against a numerical
// integration over the pupil; a collimated beam passes a line inside the
// band whole, and an f/5 one barely shifts a 7 nm band. Through passes the
// target's light falls by the lines' transmissions, the sky not at all.
func TestBeamThrough(t *testing.T) {
	origin := beam{335.0 / 152, 63.0 / 152}
	for _, c := range []struct {
		b         beam
		width, nm float64
		want      float64
	}{
		{origin, 3, 656.3, 0.2505},
		{origin, 3, 654.8, 0.7255},
		{origin, 3, 658.3, 0},
		{origin, 7, 656.3, 0.8843},
		{origin, 7, 654.8, 1},
		{origin, 7, 658.3, 0.249},
		{beam{5, 0}, 7, 656.3, 1},
		{beam{}, 3, 657.8, 1},
		{beam{}, 3, 657.9, 0},
	} {
		band := [2]float64{656.3 - c.width/2, 656.3 + c.width/2}
		if got := c.b.through(c.nm, band); math.Abs(got-c.want) > 0.002 {
			t.Errorf("%v %g nm band at %g nm: %.4f, want %.4f", c.b, c.width, c.nm, got, c.want)
		}
	}
	if got := origin.through(500.7, [2]float64{499.2, 502.2}); math.Abs(got-0.3957) > 0.002 {
		t.Errorf("3 nm [O III] band at f/2.2: %.4f, want 0.3957", got)
	}
	cfg := &config.Config{FilterNM: 7}
	hii := catalog.Target{Type: "HII Region"}
	fast := *cfg
	fast.FRatio, fast.Obstructed = origin.fRatio, origin.obstructed
	slow, shifted := passes(cfg, hii, "ha"), passes(&fast, hii, "ha")
	lines := hii.Photons()
	want := lines[catalog.Ha]*0.8843 + lines[catalog.NII6548] + lines[catalog.NII6583]*0.249
	if math.Abs(shifted.w-want) > 0.002 || shifted.glow != slow.glow || shifted.rayleigh != slow.rayleigh || shifted.w >= slow.w {
		t.Errorf("Hα through f/2.2: %+v, want w %.4f and the collimated sky of %+v", shifted, want, slow)
	}
}

// FILTER stays the fewest-subs choice under equal weights, the residual and
// the dark current, on a bright sky where they matter.
func TestScoreFilterCameraModes(t *testing.T) {
	for _, mode := range []struct {
		mean           bool
		residual, dark float64
	}{{true, 0, 0}, {false, 0.002, 0}, {true, 0.002, 0.16}} {
		cfg := &config.Config{Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, NoWeather: true, ExtinctionSet: true, Extinction: 0.2, FilterNM: 7, Filter: "ha,dual,cls", Bortle: 7}
		cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort = true, 1.2314, 79.2, 45
		cfg.Aperture, cfg.Obstruction, cfg.Sub, cfg.QE, cfg.ReadNoise = 152, 63, 15, 0.83, 2.15
		cfg.MeanStack, cfg.Residual, cfg.Dark = mode.mean, mode.residual, mode.dark
		start, end, _ := astro.Window(time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC), cfg.Lat, cfg.Lon, astro.Astronomical)
		s := BuildSky(cfg, NoForecast(), start, end)
		neb := catalog.Target{Name: "N", Type: "HII Region", RADeg: 60, DecDeg: 40, Size: 20}
		r := Score(cfg, &s, []catalog.Target{neb}, false)
		if len(r) != 1 {
			t.Fatalf("%+v: not observable", mode)
		}
		fewest, best := "", 0
		for _, f := range []string{"", "ha", "dual", "cls"} {
			one := *cfg
			one.Filter = f
			rr := r[0]
			rr.Filter, rr.Obs = f, nil
			if e := Subs(&one, &s, rr, nil); best == 0 || e.Good < best {
				fewest, best = f, e.Good
			}
		}
		if r[0].Filter != fewest {
			t.Errorf("%+v: filter %q, fewest subs %q (%d)", mode, r[0].Filter, fewest, best)
		}
	}
}

// TestLoosestMoonK pins the least Moon limit factor of the -filter choices:
// the narrowband floor only for a line filter, cls's and lpro's own above it,
// and every moonKTypes entry an emission-line type whose ratios it stands for.
func TestLoosestMoonK(t *testing.T) {
	for _, typ := range moonKTypes {
		if !catalog.EmissionLine(catalog.Target{Type: typ}) {
			t.Errorf("%s is not an emission-line type", typ)
		}
	}
	for filter, want := range map[string][2]float64{
		"": {1, 1}, "ha": {minMoonK, minMoonK}, "oiii": {minMoonK, minMoonK}, "cls": {0.35, 0.37},
		"lpro": {0.79, 0.81}, "lpro,cls": {0.35, 0.37}, "lpro,dual": {minMoonK, minMoonK},
	} {
		if k := LoosestMoonK(&config.Config{Filter: filter, FilterNM: 7}); k < want[0] || k > want[1] {
			t.Errorf("LoosestMoonK(%q) = %.3f, want %.2f-%.2f", filter, k, want[0], want[1])
		}
	}
}

// flat is every line filter band at width nm, -filter-nm's.
func flat(nm float64) [catalog.NumLines]float64 {
	return (&config.Config{FilterNM: nm}).BandWidths()
}

// A built-in filter's bands are each its own width (config.Config.BandNM):
// the Seestar's 30 nm [O III] band reaches Hβ, so it passes more of an
// H II region than a 7 nm one.
func TestLineBandsWidths(t *testing.T) {
	cfg := &config.Config{FilterNM: 7, BandNM: [catalog.NumLines]float64{catalog.Ha: 20, catalog.OIII: 30}}
	b := lineBands(config.Filters["dual"], cfg.BandWidths())
	if math.Abs(b[0][1]-b[0][0]-20) > 1e-9 || math.Abs(b[1][1]-b[1][0]-30) > 1e-9 {
		t.Errorf("bands %v", b[:2])
	}
	hii := catalog.Target{Type: "HII Region"}
	if wide, narrow := passes(cfg, hii, "dual"), passes(&config.Config{FilterNM: 7}, hii, "dual"); wide.w-narrow.w < hii.Photons()[catalog.Hb]-1e-9 {
		t.Errorf("30 nm [O III] passes %v of the light, 7 nm %v: Hβ %v missing", wide.w, narrow.w, hii.Photons()[catalog.Hb])
	}
}
