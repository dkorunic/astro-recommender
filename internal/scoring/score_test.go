// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

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

	res := Score(cfg, &s, []catalog.Target{big, unknown, half})
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
	res = Score(cfg, &s, []catalog.Target{big, unknown})
	if len(res) != 1 {
		t.Fatalf("got %d results, want the 40' object only", len(res))
	}
	if res[0].Frame >= 1 || res[0].Score == 0 {
		t.Errorf("40' in a 45' frame: Frame = %v, Score = %v; want 0 < Frame < 1", res[0].Frame, res[0].Score)
	}
	// Exactly the short side cannot be framed and is not listed...
	big.Size = 45
	if res = Score(cfg, &s, []catalog.Target{big}); len(res) != 0 {
		t.Errorf("45' in a 45' frame listed with Score %v", res[0].Score)
	}
	// ...but without framing -size-max is inclusive (Rho Ophiuchi is exactly 300').
	cfg.Framing, cfg.SizeMax = false, 45
	if res = Score(cfg, &s, []catalog.Target{big}); len(res) != 1 || res[0].Frame != 1 {
		t.Errorf("45' with -size-max 45 and no framing: %d results, want 1 with Frame 1", len(res))
	}
	// Per-minute slices are kept only for -plan, as each result's own copy.
	if res[0].Alt != nil || res[0].Weight != nil {
		t.Error("per-minute slices kept without -plan")
	}
	cfg.Plan = time.Hour
	half.Size = 30
	res = Score(cfg, &s, []catalog.Target{big, half})
	if len(res) != 2 || len(res[0].Alt) != len(s.Grid) || &res[0].Alt[0] == &res[1].Alt[0] || res[0].Weight[0] == 0 {
		t.Errorf("-plan: per-minute slices missing or shared: %d results", len(res))
	}
}

func TestFrameFill(t *testing.T) {
	for fill, want := range map[float64]float64{0.1: 0.4, 0.5: 1, 0.9: 0.5, 1.2: 0} {
		if got := frameFill(fill); math.Abs(got-want) > 1e-9 {
			t.Errorf("frameFill(%v) = %v, want %v", fill, got, want)
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
	for b.Loop() {
		s := BuildSky(cfg, NoForecast(), start, start.Add(9*time.Hour+14*time.Minute))
		Score(cfg, &s, targets)
	}
}
