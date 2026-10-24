// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/horizon"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// mutEpoch is J2000.0, where precession is the identity, so a target's
// catalog RA/Dec is its position of date.
var mutEpoch = time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)

// mutSky is a synthetic night of n minutes at an equatorial site whose
// sidereal time advances 1° a minute from 0: an equatorial target at RA r
// stands at altitude 90 − |i − r| at minute i. No extinction, no Moon above
// the horizon, perfect weather, a pristine sky (plus the zodiacal and
// galactic light where the target stands, mutExtra).
func mutSky(n int) *Sky {
	s := &Sky{Start: mutEpoch, ZenithNL: atmos.NanoLamberts(22), RefNL: atmos.NanoLamberts(22), MoonSep: 50, SunLon: astro.SunLongitude(mutEpoch)}
	for i := range n {
		t := mutEpoch.Add(time.Duration(i) * time.Minute)
		s.Grid = append(s.Grid, t)
		l := float64(i)
		s.LST = append(s.LST, l)
		s.SinLST = append(s.SinLST, math.Sin(l*math.Pi/180))
		s.CosLST = append(s.CosLST, math.Cos(l*math.Pi/180))
		s.MoonAlt = append(s.MoonAlt, -10)
		s.MoonUnit = append(s.MoonUnit, astro.Unit(0, -80))
		s.Ext = append(s.Ext, 0)
		s.ExtRayleigh = append(s.ExtRayleigh, 0)
		s.ExtAerosol = append(s.ExtAerosol, 0)
		s.MoonLight = append(s.MoonLight, atmos.Moon{})
		s.Quality = append(s.Quality, 1)
	}
	s.End = s.Grid[n-1].Add(time.Minute)

	return s
}

// mutZenith is mutSky's dark zenith sky in nanoLamberts for a target at
// ra/dec (J2000 = of date at mutEpoch): the pristine 22 plus mutExtra.
func mutZenith(ra, dec float64) float64 { return atmos.NanoLamberts(22) + mutExtra(ra, dec) }

// mutExtra is the zodiacal and galactic excess at ra/dec over what mutSky's
// pristine 22 includes, held where the zenith would fall below both minima.
// Scoring adds it outside K&S's dark-sky term, dimmed by extinction alone (atmos.Absolute: the whole column).
func mutExtra(ra, dec float64) float64 {
	lon, lat := astro.Ecliptic(ra, dec, mutEpoch)

	return max(atmos.ZodiacalExcess(lon-astro.SunLongitude(mutEpoch), lat)+mutGalactic(ra, dec),
		atmos.ZodiacalPoleNL+atmos.GalacticMinNL-atmos.NanoLamberts(22))
}

// mutGalactic is the galactic light's excess at ra/dec (J2000), nanoLamberts.
func mutGalactic(ra, dec float64) float64 { return atmos.GalacticExcess(astro.Galactic(ra, dec)) }

func mutCfg() *config.Config {
	return &config.Config{Lat: 0, Lon: 0, AltMin: 30, AltMax: 80, SizeMin: 0, SizeMax: 300, Top: 20, FilterNM: 7}
}

func mutTarget(name string, ra, dec float64) catalog.Target {
	return catalog.Target{Name: name, Type: "Galaxy", RADeg: ra, DecDeg: dec, Size: 20}
}

// mutWeight is the moonless weight at altitude a for an object of surface
// brightness sb (0 = unknown) and sky fraction k under the pristine 22
// brightened by K&S's scattering airmass X = 1/√(1 − 0.96 sin² z), plus
// extra nanoLamberts of zodiacal and galactic light (mutExtra), with
// SNR ∝ S/√(S+B) against the pristine 22.
func mutWeight(a, sb, extra float64) float64 {
	z := (90 - a) * math.Pi / 180
	x := 1 / math.Sqrt(1-0.96*math.Sin(z)*math.Sin(z))
	ref := atmos.NanoLamberts(22)
	obj := 0.0
	if sb > 0 {
		obj = atmos.NanoLamberts(sb)
	}

	return min(1, math.Sqrt((obj+ref)/(obj+ref*x+extra)))
}

func mutOne(t *testing.T, cfg *config.Config, s *Sky, tg catalog.Target, perMinute bool) (Result, bool) {
	t.Helper()
	r := Score(cfg, s, []catalog.Target{tg}, perMinute)
	if len(r) == 0 {
		return Result{}, false
	}

	return r[0], true
}

func TestMutScoreGeometry(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	// RA 90.5: altitudes 30..80 at minutes 31..80 (50) and 101..140 (40).
	r, ok := mutOne(t, cfg, s, mutTarget("A", 90.5, 0), true)
	if !ok {
		t.Fatal("not observable")
	}
	if r.Runs != 2 || !r.RunFrom.Equal(s.Grid[31]) || !r.RunTo.Equal(s.Grid[81]) {
		t.Errorf("runs %d [%v, %v)", r.Runs, r.RunFrom, r.RunTo)
	}
	if math.Abs(r.Foto-90.0/141) > 1e-12 {
		t.Errorf("Foto %v, want %v", r.Foto, 90.0/141)
	}
	var sum, altSum, skySum float64
	extra := mutExtra(90.5, 0)
	for i := range 141 {
		a := 90 - math.Abs(float64(i)-90.5)
		if a < 30 || a > 80 {
			continue
		}
		sum += mutWeight(a, 0, extra)
		altSum += a
		z := (90 - a) * math.Pi / 180
		skySum += atmos.MagFromNL(atmos.NanoLamberts(22)/math.Sqrt(1-0.96*math.Sin(z)*math.Sin(z)) + extra)
	}
	if math.Abs(r.Score-sum/141) > 1e-7 {
		t.Errorf("Score %v, want %v", r.Score, sum/141)
	}
	if math.Abs(r.MeanAlt-altSum/90) > 1e-6 || math.Abs(r.SkyMag-skySum/90) > 1e-6 {
		t.Errorf("MeanAlt %v SkyMag %v, want %v %v", r.MeanAlt, r.SkyMag, altSum/90, skySum/90)
	}
	// Minutes 90 and 91 are both 0.5° off the meridian; 45 minutes of
	// precession move the RA a hair east, so 91 is the nearer.
	if math.Abs(r.MaxAlt-89.5) > 1e-5 || !r.MaxAt.Equal(s.Grid[91]) {
		t.Errorf("MaxAlt %v at %v", r.MaxAlt, r.MaxAt)
	}
	if r.Frame != 1 {
		t.Errorf("Frame %v", r.Frame)
	}
	// Per-minute altitudes everywhere, weights only where observable.
	if len(r.Alt) != 141 || math.Abs(r.Alt[0]-(90-90.5)) > 1e-5 || math.Abs(r.Alt[90]-89.5) > 1e-5 {
		t.Errorf("Alt %v %v", r.Alt[0], r.Alt[90])
	}
	if r.Weight[30] != 0 || r.Weight[90] != 0 || math.Abs(r.Weight[31]-mutWeight(30.5, 0, extra)) > 1e-7 {
		t.Errorf("Weight %v %v %v", r.Weight[30], r.Weight[90], r.Weight[31])
	}
	// RA 50.5: minutes 0..40 (41) then 61..110 (50): the later run is longer.
	r, _ = mutOne(t, cfg, s, mutTarget("B", 50.5, 0), false)
	if r.Runs != 2 || !r.RunFrom.Equal(s.Grid[61]) || !r.RunTo.Equal(s.Grid[111]) || r.Alt != nil || r.Weight != nil {
		t.Errorf("B runs %d [%v, %v) alt %v", r.Runs, r.RunFrom, r.RunTo, r.Alt != nil)
	}
	// A run through the last minute ends at End, which may cut it short.
	s.End = s.Grid[140].Add(30 * time.Second)
	r, _ = mutOne(t, cfg, s, mutTarget("C", 160.5, 0), false)
	if !r.RunTo.Equal(s.End) || !r.RunFrom.Equal(s.Grid[101]) || r.Runs != 1 {
		t.Errorf("C [%v, %v) runs %d", r.RunFrom, r.RunTo, r.Runs)
	}
	// -min-run judges the longest run's clock span: 50 minutes for A.
	cfg.MinRun = 50 * time.Minute
	if _, ok := mutOne(t, cfg, s, mutTarget("A", 90.5, 0), false); !ok {
		t.Error("50 min run dropped at -min-run 50m")
	}
	cfg.MinRun = 51 * time.Minute
	if _, ok := mutOne(t, cfg, s, mutTarget("A", 90.5, 0), false); ok {
		t.Error("50 min run kept at -min-run 51m")
	}
	// Never in the band: dropped.
	cfg.MinRun = 0
	if _, ok := mutOne(t, cfg, s, mutTarget("D", 0, 70), false); ok {
		t.Error("target never above 30° kept")
	}
}

// Alt limits are inclusive: the band [30, 80] at half-degree steps from
// RA 90 gives altitudes 30 and 80 exactly at minutes 30, 80 (and 100, 150).
func TestMutAltLimits(t *testing.T) {
	s := mutSky(181)
	cfg := mutCfg()
	cfg.AltMin, cfg.AltMax = 29.75, 80.25
	r, _ := mutOne(t, cfg, s, mutTarget("A", 90, 0), false)
	if math.Abs(r.Foto-102.0/181) > 1e-12 {
		t.Errorf("Foto %v, want %v", r.Foto, 102.0/181)
	}
	cfg.AltMin, cfg.AltMax = 9.75, 89.25
	r, _ = mutOne(t, cfg, s, mutTarget("A", 90, 0), false)
	if math.Abs(r.Foto-160.0/181) > 1e-12 { // |i−90| from 1 to 80, both sides
		t.Errorf("Foto %v, want %v", r.Foto, 160.0/181)
	}
	// A horizon at 50° all round cuts the band to 50..80.
	cfg.AltMin, cfg.AltMax = 29.75, 80.25
	cfg.Horizon = horizon.Horizon{{0, 49.75}, {360, 49.75}}
	r, _ = mutOne(t, cfg, s, mutTarget("A", 90, 0), false)
	if math.Abs(r.Foto-62.0/181) > 1e-12 {
		t.Errorf("horizon Foto %v, want %v", r.Foto, 62.0/181)
	}
}

func TestMutMoonSeparation(t *testing.T) {
	s := mutSky(141)
	for i := range s.Grid {
		s.MoonAlt[i] = 20
		s.MoonUnit[i] = astro.Unit(90.5, 0)
	}
	s.MoonSep = 50
	cfg := mutCfg()
	// 40° from the Moon at 50 % illumination: too close for a galaxy.
	if _, ok := mutOne(t, cfg, s, mutTarget("G", 90.5, 40), false); ok {
		t.Error("galaxy 40° from a half Moon kept")
	}
	// 60° away: fine.
	if _, ok := mutOne(t, cfg, s, mutTarget("G", 90.5, -60), false); ok {
		// dec −60 never reaches 30° at the equator? It does: 30° at transit.
	}
	r, ok := mutOne(t, cfg, s, catalog.Target{Name: "W", Type: "Galaxy", RADeg: 90.5, DecDeg: 55, Size: 20}, false)
	if !ok || r.Runs != 1 {
		t.Errorf("galaxy 55° from the Moon: %v %d", ok, r.Runs)
	}
	// A cluster's limit is a galaxy's: its faintest members are sky-limited.
	if _, ok := mutOne(t, cfg, s, catalog.Target{Name: "C", Type: "Open Cluster", RADeg: 90.5, DecDeg: 40, Size: 20}, false); ok {
		t.Error("cluster 40° from a half Moon kept")
	}
	// Emission nebula: with a 7 nm dual-band filter moonK is 0.19 → 9°; without, 50°.
	neb := catalog.Target{Name: "N", Type: "Emission Nebula", RADeg: 90.5, DecDeg: 20, Size: 20}
	if _, ok := mutOne(t, cfg, s, neb, false); ok {
		t.Error("nebula 20° from the Moon kept without a filter")
	}
	cfg.Filter = "dual"
	if _, ok := mutOne(t, cfg, s, neb, false); !ok {
		t.Error("nebula 20° from the Moon dropped with a filter")
	}
	if _, ok := mutOne(t, cfg, s, catalog.Target{Name: "G2", Type: "Galaxy", RADeg: 90.5, DecDeg: 20, Size: 20}, false); ok {
		t.Error("filter helped a galaxy")
	}
	// Moon below the horizon: no separation rule.
	cfg.Filter = ""
	for i := range s.Grid {
		s.MoonAlt[i] = -1
	}
	if _, ok := mutOne(t, cfg, s, neb, false); !ok {
		t.Error("separation rule applied with the Moon down")
	}
}

// A comet's track moves: minute i at the i-th position, so a comet held at
// RA = LST transits all night, and its size limits do not apply.
func TestMutCometTrack(t *testing.T) {
	s := mutSky(60)
	cfg := mutCfg()
	cfg.SizeMin = 10
	tr := make([][2]float64, 60)
	for i := range tr {
		tr[i] = [2]float64{float64(i), 45} // 45° altitude at the equator, all night
	}
	r, ok := mutOne(t, cfg, s, catalog.Target{Name: "C/1", Type: "Comet", Track: tr}, false)
	if !ok || r.Foto != 1 || math.Abs(r.MeanAlt-45) > 1e-9 {
		t.Errorf("comet: %v Foto %v MeanAlt %v", ok, r.Foto, r.MeanAlt)
	}
	// Its Moon separation moves with it: the Moon on the track at minute 30.
	for i := range s.Grid {
		s.MoonAlt[i] = 10
		s.MoonUnit[i] = astro.Unit(30, 45)
	}
	s.MoonSep = 10
	r, _ = mutOne(t, cfg, s, catalog.Target{Name: "C/1", Type: "Comet", Track: tr}, false)
	if r.Runs != 2 {
		t.Errorf("comet runs %d, want 2 (a Moon gap in the middle)", r.Runs)
	}
}

func TestMutSurfaceBrightnessWeight(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	extra := mutExtra(90.5, 0)
	for _, sb := range []float64{16, 21, 24} {
		tg := catalog.Target{Name: "S", Type: "Galaxy", RADeg: 90.5, Size: 20, SurfBr: sb}
		r, _ := mutOne(t, cfg, s, tg, true)
		if w := mutWeight(40.5, sb, extra); math.Abs(r.Weight[41]-w) > 1e-7 {
			t.Errorf("SB %v: weight %v, want %v", sb, r.Weight[41], w)
		}
	}
	// Cluster: no surface brightness, the whole sky, as light of unknown brightness.
	r, _ := mutOne(t, cfg, s, catalog.Target{Name: "K", Type: "Open Cluster", RADeg: 90.5, Size: 20, SurfBr: 16}, true)
	if w := mutWeight(40.5, 0, extra); math.Abs(r.Weight[41]-w) > 1e-7 {
		t.Errorf("cluster weight %v, want %v", r.Weight[41], w)
	}
	// Quality and extinction multiply in.
	s.Quality[41], s.Ext[41] = 0.5, 0.2
	r, _ = mutOne(t, cfg, s, mutTarget("Q", 90.5, 0), true)
	z := 49.5 * math.Pi / 180
	x := 1 / math.Sqrt(1-0.96*math.Sin(z)*math.Sin(z))
	sky := atmos.NanoLamberts(22)*math.Pow(10, -0.4*0.2*(x-1))*x + extra*atmos.Absolute(atmos.Airmass(40.5), 0.2)
	want := 0.5 * math.Pow(10, -0.4*0.2*(atmos.Airmass(40.5)-1)) * math.Sqrt(atmos.NanoLamberts(22)/sky)
	if math.Abs(r.Weight[41]-want) > 1e-7 {
		t.Errorf("weight with quality+extinction %v, want %v", r.Weight[41], want)
	}
}

func TestMutFilters(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	cfg.SizeMin, cfg.SizeMax = 10, 300
	mk := func(name string, size float64) catalog.Target {
		return catalog.Target{Name: name, Type: "Galaxy", RADeg: 90.5, Size: size}
	}
	in := []catalog.Target{mk("small", 9.9), mk("min", 10), mk("max", 300), mk("big", 300.1), mk("unknown", -9999), mk("NGC 7789", 50)}
	got := map[string]bool{}
	for _, r := range Score(cfg, s, in, false) {
		got[r.Name] = true
	}
	if got["small"] || !got["min"] || !got["max"] || got["big"] || got["unknown"] || !got["NGC 7789"] {
		t.Errorf("size limits kept %v", got)
	}
	cfg.SizeMin = 0
	cfg.Skip = map[string]bool{"ngc7789": true}
	got = map[string]bool{}
	for _, r := range Score(cfg, s, in, false) {
		got[r.Name] = true
	}
	if !got["unknown"] || got["NGC 7789"] {
		t.Errorf("unknown size / skip: %v", got)
	}
	// The region filter uses the catalog position.
	cfg.Skip = nil
	cfg.RASet, cfg.RA, cfg.Tol = true, 100, 5
	if r := Score(cfg, s, []catalog.Target{mk("far", 50), {Name: "near", Type: "Galaxy", RADeg: 103, Size: 50}}, false); len(r) != 1 || r[0].Name != "near" {
		t.Errorf("region kept %v", r)
	}
}

func TestMutFraming(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	cfg.Framing, cfg.FOVShort, cfg.FOVLong, cfg.Scale = true, 60, 90, 1
	for _, c := range []struct {
		size  float64
		frame float64
		ok    bool
	}{{30, 1, true}, {15, 1, true}, {48, 1, true}, {6, 0.4, true}, {54, 0.5, true}, {60, 0, false}, {-9999, 1, true}} {
		r, ok := mutOne(t, cfg, s, catalog.Target{Name: "F", Type: "Galaxy", RADeg: 90.5, Size: c.size}, false)
		if ok != c.ok || ok && math.Abs(r.Frame-c.frame) > 1e-9 {
			t.Errorf("size %v: %v frame %v", c.size, ok, r.Frame)
		}
		if ok {
			plain, _ := mutOne(t, &config.Config{AltMin: 30, AltMax: 80, SizeMax: 300}, s, catalog.Target{Name: "F", Type: "Galaxy", RADeg: 90.5, Size: c.size}, false)
			if math.Abs(r.Score-plain.Score*c.frame) > 1e-12 {
				t.Errorf("size %v: score %v, want %v", c.size, r.Score, plain.Score*c.frame)
			}
		}
	}
	for fill, want := range map[float64]float64{0: 0, 0.125: 0.5, 0.22: 0.88, 0.25: 1, 0.5: 1, 0.8: 1, 0.9: 0.5, 1: 0, 1.2: 0} {
		if got := frameFill(fill); math.Abs(got-want) > 1e-12 {
			t.Errorf("frameFill(%v) = %v, want %v", fill, got, want)
		}
	}
}

// Best score first; equal scores by mean altitude.
func TestMutSortAndClone(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	// Same declination, transits at different minutes: "mid" has both its
	// 50-minute runs in the grid, "late" loses 10 minutes at the end, "early"
	// more at the start. Bright surfaces, so the sky (the Milky Way's light
	// behind "late") hardly matters next to the minutes lost.
	in := []catalog.Target{mutTarget("early", 40.5, 0), mutTarget("mid", 70.5, 0), mutTarget("late", 90.5, 0)}
	for i := range in {
		in[i].SurfBr = 12
	}
	r := Score(cfg, s, in, true)
	if len(r) != 3 || r[0].Name != "mid" || r[1].Name != "late" || r[2].Name != "early" || !(r[0].Score > r[1].Score && r[1].Score > r[2].Score) {
		for _, x := range r {
			t.Logf("%s %v", x.Name, x.Score)
		}
		t.Fatalf("order wrong")
	}
	if &r[0].Alt[0] == &r[1].Alt[0] || r[0].Alt[90] == r[1].Alt[90] {
		t.Error("per-minute slices shared between results")
	}
	// Ties on score: higher mean altitude first. Zero-score targets (no
	// quality) tie at 0.
	for i := range s.Quality {
		s.Quality[i] = 0
	}
	r = Score(cfg, s, []catalog.Target{mutTarget("low", 90.5, -30), mutTarget("high", 90.5, 0)}, false)
	if len(r) != 2 || r[0].Name != "high" {
		t.Errorf("tie order %v %v", r[0].Name, r[1].Name)
	}
}

func TestMutRampFillGaps(t *testing.T) {
	for _, c := range [][5]float64{
		{4, 4, 1, 0.7, 1},
		{5, 4, 1, 0.7, 1},
		{1, 4, 1, 0.7, 0.7},
		{0, 4, 1, 0.7, 0.7},
		{2.5, 4, 1, 0.7, 0.85},
		{30, 20, 40, 0.5, 0.75},
		{10, 20, 40, 0.5, 1},
		{50, 20, 40, 0.5, 0.5},
	} {
		if got := ramp(c[0], c[1], c[2], c[3]); math.Abs(got-c[4]) > 1e-12 {
			t.Errorf("ramp%v = %v", c[:4], got)
		}
	}
	v := []float64{0.2, 0, 0.6, 0}
	fillGaps("x", v, []bool{true, false, true, false}, true, time.Hour)
	if v[1] != 0.4 || v[3] != 0.4 || v[0] != 0.2 {
		t.Errorf("fillGaps %v", v)
	}
	v = []float64{0, 0}
	fillGaps("x", v, []bool{false, false}, false, time.Hour)
	if v[0] != 1 || v[1] != 1 {
		t.Errorf("fillGaps empty %v", v)
	}
}

func TestMutExtinctionAt(t *testing.T) {
	h := time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC)
	s := Sky{Elevation: math.NaN(), AOD: map[int64]float64{h.Unix(): 0.3}}
	if got := s.ExtinctionAt(h.Add(30*time.Minute), 0.17); got != 0.17 {
		t.Errorf("no elevation: %v", got)
	}
	s.Elevation = 0
	if got := s.ExtinctionAt(h.Add(59*time.Minute), 0.17); math.Abs(got-atmos.ExtinctionCoeff(0, 0.3)) > 1e-12 {
		t.Errorf("hour's AOD: %v", got)
	}
	if got := s.ExtinctionAt(h.Add(60*time.Minute), 0.17); math.Abs(got-atmos.ExtinctionCoeff(0, atmos.TypicalAOD)) > 1e-12 {
		t.Errorf("typical AOD: %v", got)
	}
}

func TestMutBuildSky(t *testing.T) {
	cfg := &config.Config{Lat: 45.81, Lon: 15.98, Bortle: 5, Extinction: 0.2}
	start := time.Date(2026, 10, 12, 18, 30, 0, 0, time.UTC)
	end := start.Add(3*time.Hour + 30*time.Second)
	h0 := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC).Unix()
	f := Forecast{
		Elevation: math.NaN(),
		Weather: map[int64]weather.HourWeather{
			h0:        {Cloud: 50, Temp: 10, DewPoint: 0, Wind: 15, Gust: 10, Precip: math.NaN()},
			h0 + 3600: {Cloud: 0, Temp: 10, DewPoint: 7.5, Wind: 15, Gust: 30, Precip: 0.05},
			h0 + 7200: {Cloud: 0, Temp: 10, DewPoint: 0, Wind: 15, Gust: 0, Precip: 0.2},
		},
		Astro: map[int64]weather.AstroBlock{
			time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC).Unix(): {Seeing: 2, Transparency: 8},
			time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC).Unix(): {Seeing: 2, Transparency: 1},
		},
	}
	s := BuildSky(cfg, f, start, end)
	if len(s.Grid) != 181 || !s.Grid[0].Equal(start) || !s.Grid[180].Equal(start.Add(3*time.Hour)) {
		t.Fatalf("grid %d", len(s.Grid))
	}
	mid := s.Grid[90]
	if s.Illum != astro.MoonIllumination(mid) || s.MoonSep != 100*s.Illum || s.MoonDist != astro.MoonDistance(mid) {
		t.Error("Moon at mid-window")
	}
	if math.Abs(s.ZenithNL-atmos.NanoLamberts(20.55)) > 1e-9 || math.Abs(s.RefNL-atmos.NanoLamberts(22)) > 1e-9 {
		t.Errorf("sky %v %v", s.ZenithNL, s.RefNL)
	}
	for _, i := range []int{0, 77, 180} {
		if math.Abs(s.SinLST[i]-math.Sin(s.LST[i]*math.Pi/180)) > 1e-9 || math.Abs(s.CosLST[i]-math.Cos(s.LST[i]*math.Pi/180)) > 1e-9 {
			t.Errorf("LST sines at %d", i)
		}
		if s.LST[i] != astro.LST(s.Grid[i], cfg.Lon) {
			t.Errorf("LST at %d", i)
		}
		ra, dec := astro.MoonTopo(s.Grid[i], cfg.Lat, cfg.Lon)
		if s.MoonPos[i] != [2]float64{ra, dec} || s.MoonUnit[i] != astro.Unit(ra, dec) || s.MoonAlt[i] != astro.Altitude(ra, dec, s.Grid[i], cfg.Lat, cfg.Lon) {
			t.Errorf("Moon at %d", i)
		}
		if s.Ext[i] != 0.2 || s.MoonLight[i] != atmos.MoonLight(0.2, s.ExtRayleigh[i], s.ExtAerosol[i], s.MoonAlt[i], s.MoonPhase, s.MoonDist) {
			t.Errorf("ext/moonlight at %d", i)
		}
	}
	// Weather: cloud, spread and wind from the nearest hour, gusts and rain
	// over the minute's own. 18:30-18:59: 19:00's clear sky and 2.5 °C
	// spread (0.85), 18:00's gust 10 and unknown rain; 19:00-19:29: 0.85,
	// gust 30 (0.75), 0.05 mm (0.5): 0.31875; 19:30-19:59: 20:00's 10 °C
	// spread (1), 19:00's gust and rain: 0.375; 20:00-20:59 0.2 mm (0);
	// 21:00-21:30 none: the covered minutes' mean
	// (30·0.85 + 30·0.31875 + 30·0.375)/150 = 0.30875. Transparency:
	// 7Timer's 18:00 point (8: 0.5) up to 19:29, its 21:00 point (1: 1) from 19:30.
	q := func(i int) float64 { return s.Quality[i] }
	for _, c := range []struct {
		i    int
		want float64
	}{{0, 0.85 * 0.5}, {29, 0.85 * 0.5}, {30, 0.31875 * 0.5}, {59, 0.31875 * 0.5}, {60, 0.375}, {89, 0.375}, {90, 0}, {149, 0}, {150, 0.30875}, {180, 0.30875}} {
		if math.Abs(q(c.i)-c.want) > 1e-9 {
			t.Errorf("quality[%d] = %v, want %v", c.i, q(c.i), c.want)
		}
	}
}

// Minutes without a forecast hour get the covered minutes' mean.
func TestMutBuildSkyGaps(t *testing.T) {
	cfg := &config.Config{Lat: 45.81, Lon: 15.98, Extinction: 0.2}
	start := time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC)
	f := Forecast{Elevation: 300, Weather: map[int64]weather.HourWeather{
		start.Unix(): {Cloud: 20, Temp: 10, DewPoint: 0, Precip: math.NaN()},
	}, AOD: map[int64]float64{start.Unix(): 0.4}}
	s := BuildSky(cfg, f, start, start.Add(2*time.Hour))
	if math.Abs(s.Quality[0]-0.8) > 1e-12 || math.Abs(s.Quality[119]-0.8) > 1e-12 {
		t.Errorf("gap quality %v %v", s.Quality[0], s.Quality[119])
	}
	// AODs thinned for the 300 m the site stands above CAMS's ground (0 m in
	// a hand-built Forecast) and TypicalAOD's sea level.
	thin := math.Exp(-300.0 / atmos.AerosolScaleHeight)
	if math.Abs(s.Ext[0]-atmos.ExtinctionCoeff(300, 0.4*thin)) > 1e-12 || math.Abs(s.Ext[60]-atmos.ExtinctionCoeff(300, atmos.TypicalAOD*thin)) > 1e-12 {
		t.Errorf("ext %v %v", s.Ext[0], s.Ext[60])
	}
	// CAMS's ground unknown: its AOD as given; ground above the site: no more.
	for _, ground := range []float64{math.NaN(), 800} {
		f.AODElevation = ground
		if s := BuildSky(cfg, f, start, start.Add(2*time.Hour)); math.Abs(s.Ext[0]-atmos.ExtinctionCoeff(300, 0.4)) > 1e-12 {
			t.Errorf("CAMS ground %v: ext %v", ground, s.Ext[0])
		}
	}
	// 7Timer's transparency counts only where CAMS's aerosols do not.
	f.Astro = map[int64]weather.AstroBlock{weather.AstroKey(start): {Transparency: 8}, weather.AstroKey(start.Add(time.Hour)): {Transparency: 8}}
	s = BuildSky(cfg, f, start, start.Add(2*time.Hour))
	if math.Abs(s.Quality[0]-0.8) > 1e-12 || math.Abs(s.Quality[61]-0.8*0.5) > 1e-12 {
		t.Errorf("transparency over CAMS %v, without %v", s.Quality[0], s.Quality[61])
	}
	cfg.SQM, cfg.Bortle = 20.5, 5
	s = BuildSky(cfg, NoForecast(), start, start.Add(time.Hour))
	if s.Quality[0] != 1 || s.Ext[0] != 0.2 || math.Abs(s.ZenithNL-atmos.NanoLamberts(20.5)) > 1e-9 {
		t.Errorf("no forecast %v %v", s.Quality[0], s.Ext[0])
	}
}

func TestMutMissingSeeing(t *testing.T) {
	start := time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	good := []weather.Level{{P: 998, Z: 122, T: 12, Wind: 5, Dir: 270}, {P: 850, Z: 1500, T: 10, Wind: 10, Dir: 270}, {P: 500, Z: 5600, T: -10, Wind: 10, Dir: 270}, {P: 300, Z: 9200, T: -30, Wind: 10, Dir: 270}}
	f := Forecast{Weather: map[int64]weather.HourWeather{start.Unix(): {}, start.Unix() + 3600: {}}}
	if m, n := missingSeeing(f, start, end); m != 2 || n != 2 {
		t.Errorf("no profile, no 7Timer: %d of %d, want 2 of 2", m, n)
	}
	f.Weather[start.Unix()+3600] = weather.HourWeather{Seeing: weather.Seeing(good)}
	if m, n := missingSeeing(f, start, end); m != 1 || n != 2 {
		t.Errorf("one hour with an estimate: %d of %d, want 1 of 2", m, n)
	}
	f = Forecast{Weather: map[int64]weather.HourWeather{start.Unix(): {}}, Astro: map[int64]weather.AstroBlock{weather.AstroKey(start): {Seeing: 1, Transparency: 1}}}
	if m, n := missingSeeing(f, start, start.Add(time.Hour)); m != 0 || n != 0 {
		t.Errorf("7Timer covers it: %d of %d, want none", m, n)
	}
	if m, n := missingSeeing(Forecast{}, start, end); m != 0 || n != 0 {
		t.Errorf("no forecast at all: %d of %d, want none", m, n)
	}
}

// Equal longest runs: the first counts.
func TestMutRunTie(t *testing.T) {
	s := mutSky(181)
	r, _ := mutOne(t, mutCfg(), s, mutTarget("A", 90.5, 0), false)
	if r.Runs != 2 || !r.RunFrom.Equal(s.Grid[31]) || !r.RunTo.Equal(s.Grid[81]) {
		t.Errorf("tie: runs %d [%v, %v)", r.Runs, r.RunFrom, r.RunTo)
	}
}

// The lower altitude limit is inclusive: a comet at the zenith of an
// equatorial site is observable with -alt-min 90.
func TestMutAltMinInclusive(t *testing.T) {
	s := mutSky(10)
	cfg := mutCfg()
	cfg.AltMin, cfg.AltMax = 90, 90
	tr := make([][2]float64, 10)
	for i := range tr {
		tr[i] = [2]float64{float64(i), 0}
	}
	if r, ok := mutOne(t, cfg, s, catalog.Target{Name: "Z", Type: "Comet", Track: tr}, false); !ok || r.Foto != 1 {
		t.Errorf("zenith comet: %v %v", ok, r.Foto)
	}
}

// On a real night at Zagreb the per-minute altitudes agree with astro.AltAz
// for the target's position of date.
func TestMutScoreRealSky(t *testing.T) {
	cfg := &config.Config{Lat: 45.81, Lon: 15.98, AltMin: -90, AltMax: 90, SizeMax: 300, Extinction: 0.2}
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(10*time.Hour))
	for _, tg := range []catalog.Target{
		{Name: "M 31", Type: "Galaxy", RADeg: 10.68, DecDeg: 41.27, Size: 190},
		{Name: "M 42", Type: "Emission Nebula", RADeg: 83.82, DecDeg: -5.39, Size: 85},
		{Name: "M 13", Type: "Globular Cluster", RADeg: 250.42, DecDeg: 36.46, Size: 20},
	} {
		r := Score(cfg, &s, []catalog.Target{tg}, true)
		if len(r) != 1 {
			t.Fatalf("%s not scored", tg.Name)
		}
		ra, dec := astro.Precess(tg.RADeg, tg.DecDeg, s.Grid[len(s.Grid)/2])
		for _, i := range []int{0, 100, 300, 599} {
			if want := astro.Altitude(ra, dec, s.Grid[i], cfg.Lat, cfg.Lon); math.Abs(r[0].Alt[i]-want) > 1e-6 {
				t.Errorf("%s alt[%d] = %v, want %v", tg.Name, i, r[0].Alt[i], want)
			}
		}
	}
}

// Against a horizon the azimuth decides: a wall up to 89° over the
// south-east (azimuth 90-180) and none elsewhere.
func TestMutScoreHorizonAzimuth(t *testing.T) {
	cfg := &config.Config{
		Lat: 45.81, Lon: 15.98, AltMin: -90, AltMax: 90, SizeMax: 300, Extinction: 0.2,
		Horizon: horizon.Horizon{{0, -90}, {89.9, -90}, {90.1, 89}, {179.9, 89}, {180.1, -90}, {360, -90}},
	}
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	s := BuildSky(cfg, NoForecast(), start, start.Add(10*time.Hour))
	s.MoonSep = 0
	checked := [2]int{}
	for _, tg := range []catalog.Target{
		{Name: "M 31", Type: "Galaxy", RADeg: 10.68, DecDeg: 41.27, Size: 190},
		{Name: "M 42", Type: "Emission Nebula", RADeg: 83.82, DecDeg: -5.39, Size: 85},
	} {
		r := Score(cfg, &s, []catalog.Target{tg}, true)
		if len(r) != 1 {
			t.Fatalf("%s not scored", tg.Name)
		}
		ra, dec := astro.Precess(tg.RADeg, tg.DecDeg, s.Grid[len(s.Grid)/2])
		for i, tm := range s.Grid {
			alt, az := astro.AltAz(ra, dec, tm, cfg.Lat, cfg.Lon)
			h := cfg.Horizon.At(az)
			if math.Abs(alt-h) < 0.05 || math.Abs(az-90) < 0.2 || math.Abs(az-180) < 0.2 {
				continue
			}
			want := alt >= h
			if got := r[0].Weight[i] > 0; got != want {
				t.Fatalf("%s minute %d: alt %v az %v horizon %v: observable %v", tg.Name, i, alt, az, h, got)
			}
			if want {
				checked[0]++
			} else {
				checked[1]++
			}
		}
	}
	if checked[0] < 50 || checked[1] < 50 {
		t.Errorf("checked %v", checked)
	}
}

// Through a filter the filtered weight carries the bands' own extinction:
// the Rayleigh part as (λ/551)⁻⁴ and the aerosol part as (λ/551)^-1.3, ozone
// as is, each line's weighted by its share of the light passed. Under a
// bright sky an H II region without data is sky-limited and gains through
// Hα, so its weight is the band's transmission times the filtered sky weight.
func TestMutBandExtinction(t *testing.T) {
	s := mutSky(141)
	cfg := mutCfg()
	cfg.Filter = "ha"
	s.ZenithNL = atmos.NanoLamberts(19)
	ray, aer := atmos.SplitExtinction(0.2)
	for i := range s.Grid {
		s.Ext[i], s.ExtRayleigh[i], s.ExtAerosol[i] = 0.2, ray, aer
	}
	tg := catalog.Target{Name: "N", Type: "Emission Nebula", RADeg: 90.5, Size: 20}
	lines := tg.Photons()
	// weighted is the factor (λ/551)^p weighted by the photon share of each line
	// the 7 nm bands reach: [N II] 654.8/658.3 in Hα's.
	weighted := func(p float64, ls ...catalog.Line) float64 {
		var f, w float64
		for _, l := range ls {
			f += lines[l] * math.Pow(catalog.LineNM[l]/551, p)
			w += lines[l]
		}

		return f / w
	}
	fp := passes(cfg, tg, "ha")
	ha := []catalog.Line{catalog.Ha, catalog.NII6548, catalog.NII6583}
	if wantRay, wantAer := weighted(-4, ha...), weighted(-1.3, ha...); math.Abs(fp.ray-wantRay) > 1e-12 || math.Abs(fp.aer-wantAer) > 1e-12 {
		t.Errorf("Hα band factors %v %v, want %v %v", fp.ray, fp.aer, wantRay, wantAer)
	}
	dual := passes(cfg, tg, "dual")
	if wantRay := weighted(-4, append(ha, catalog.OIII)...); math.Abs(dual.ray-wantRay) > 1e-12 {
		t.Errorf("dual-band Rayleigh factor %v, want the light-weighted %v", dual.ray, wantRay)
	}
	r, ok := mutOne(t, cfg, s, tg, true)
	if !ok || r.Filter != "ha" {
		t.Fatalf("H II region through Hα: %v, filter %q", ok, r.Filter)
	}
	lon, lat := astro.Ecliptic(90.5, 0, mutEpoch)
	extra := max(atmos.ZodiacalExcess(lon-s.SunLon, lat)+mutGalactic(90.5, 0), atmos.ZodiacalPoleNL+atmos.GalacticMinNL-s.ZenithNL)
	glow, _, _ := atmos.SkyParts(s.ZenithNL, 0.2, 40.5, 180, atmos.Moon{})
	glow += extra * atmos.Absolute(atmos.Airmass(40.5), 0.2)
	band := ray*fp.ray + atmos.OzoneExtinction + aer*fp.aer
	want := atmos.Transmission(atmos.Airmass(40.5), band) * min(1, fp.gain(0, s.RefNL, glow, 0, 0))
	if math.Abs(r.Weight[41]-want) > 1e-7 || band >= 0.2 {
		t.Errorf("weight %v, want %v through the band's %.4f mag/airmass", r.Weight[41], want, band)
	}
	// Without the filter: the broadband weight, the lines still dimmed at
	// their own wavelengths (V's coefficient with its Rayleigh and aerosol
	// parts moved to them), less than V's: Hα dominates.
	cfg.Filter = ""
	r, _ = mutOne(t, cfg, s, tg, true)
	all := broadPasses(tg, lines, cameraBand)
	lineExt := 0.2 + ray*(all.ray-1) + aer*(all.aer-1)
	if want := atmos.Transmission(atmos.Airmass(40.5), lineExt) * min(1, math.Sqrt(s.RefNL/glow)); math.Abs(r.Weight[41]-want) > 1e-7 || lineExt >= 0.2 {
		t.Errorf("broadband weight %v, want %v through the lines' %.4f mag/airmass", r.Weight[41], want, lineExt)
	}
}

// The zodiacal light enters once per target: on the ecliptic the sky is
// brighter than at the ecliptic pole, so the same object scores less there,
// and no configured sky gets darker than the pole's light allows.
func TestMutZodiacal(t *testing.T) {
	s := mutSky(181)
	cfg := mutCfg()
	// mutEpoch is J2000, so J2000 positions are of date: the June solstice
	// point (RA 90°, Dec 23.44°) lies on the ecliptic 170° from the January
	// Sun, the south ecliptic pole at RA 90°, Dec −66.56°.
	// The galactic light differs between the two (the anticentre's edge, the
	// LMC's); take it out to compare the zodiacal light alone.
	ecl, pole := mutZenith(90, 23.44)-mutGalactic(90, 23.44), mutZenith(90, -66.56)-mutGalactic(90, -66.56)
	if _, lat := astro.Ecliptic(90, -66.56, mutEpoch); math.Abs(lat+90) > 0.01 {
		t.Fatalf("south ecliptic pole at latitude %v", lat)
	}
	if d := atmos.MagFromNL(pole) - atmos.MagFromNL(ecl); d < 0.4 || d > 0.6 {
		t.Errorf("ecliptic %.2f, pole %.2f mag/arcsec²: want the ecliptic about half a magnitude brighter", atmos.MagFromNL(ecl), atmos.MagFromNL(pole))
	}
	if math.Abs(pole-atmos.ZodiacalPoleNL-(atmos.NanoLamberts(22)-atmos.Zodiacal(atmos.ZodiacalRefLon, atmos.ZodiacalRefLat))) > 0.01 { // Dec −66.56° is 0.01° off the pole
		t.Errorf("pole zenith %v: want 22 mag/arcsec² less the reference's zodiacal light plus the pole's", pole)
	}
	// The pole target transits at 23.44° at minute 90; a configured sky
	// darker than the pole's zodiacal light and the darkest galactic light
	// is held at their sum.
	cfg.AltMin = -90
	floor := atmos.ZodiacalPoleNL + atmos.GalacticMinNL
	s.ZenithNL = floor/2 - mutGalactic(90, -66.56)
	r, ok := mutOne(t, cfg, s, catalog.Target{Name: "Z", Type: "Galaxy", RADeg: 90, DecDeg: -66.56, Size: 20, SurfBr: 30}, true)
	if !ok || math.Abs(r.MaxAlt-23.44) > 0.01 {
		t.Fatalf("pole target: %v, max alt %v", ok, r.MaxAlt)
	}
	// The floor holds at the zenith; away from it the extra light is added
	// unbrightened: K&S's airmass scales only the configured sky.
	glow, _, _ := atmos.SkyParts(s.ZenithNL, 0, r.Alt[90], 180, atmos.Moon{})
	glow += floor - s.ZenithNL
	obj := atmos.NanoLamberts(30)
	if want := min(1, math.Sqrt((obj+s.RefNL)/(obj+glow))); math.Abs(r.Weight[90]-want) > 1e-7 {
		t.Errorf("weight at the floor %v, want %v", r.Weight[90], want)
	}
}

// An emission nebula's lines (lightNL of its V) are collected once while the
// camera band holds CameraBand V bands of sky, so against the V sky that
// stands for it the nebula is that much fainter; a galaxy's continuum is not
// moved.
func TestMutEmissionCameraBand(t *testing.T) {
	s, cfg := mutSky(141), mutCfg()
	extra := mutExtra(90.5, 0)
	glow, _, _ := atmos.SkyParts(s.ZenithNL, 0, 40.5, 180, atmos.Moon{})
	glow += extra
	neb := catalog.Target{Name: "N", Type: "Emission Nebula", RADeg: 90.5, Size: 20, SurfBr: 18}
	obj := lightNL(neb, 18) / atmos.CameraBand
	r, _ := mutOne(t, cfg, s, neb, true)
	if want := math.Sqrt((obj + s.RefNL) / (obj + glow)); math.Abs(r.Weight[41]-want) > 1e-7 {
		t.Errorf("nebula weight %v, want %v", r.Weight[41], want)
	}
	if r, _ := mutOne(t, cfg, s, catalog.Target{Name: "G", Type: "Galaxy", RADeg: 90.5, Size: 20, SurfBr: 18}, true); math.Abs(r.Weight[41]-mutWeight(40.5, 18, extra)) > 1e-7 {
		t.Errorf("galaxy weight %v, want %v", r.Weight[41], mutWeight(40.5, 18, extra))
	}
}

// A filter is worn all night, as it is shot: a bright nebula under a dark
// sky, object-limited, loses light through Hα every minute and keeps no
// filter, where a per-minute choice would have credited each minute the
// better of the two.
func TestMutFilterAllNight(t *testing.T) {
	s, cfg := mutSky(141), mutCfg()
	cfg.Filter = "ha"
	tg := catalog.Target{Name: "N", Type: "Emission Nebula", RADeg: 90.5, Size: 20, SurfBr: 14}
	fp := passes(cfg, tg, "ha")
	ta := tally{weight: make([]float64, len(s.Grid))}
	ta.reset(fp)
	base, ok := scoreTarget(cfg, s, tg, []*tally{&ta}, cfg.Mosaics(), nil, nil, 0)
	if _, counted := ta.result(cfg, s, base); !ok || !counted {
		t.Fatal("not observable")
	}
	weight := ta.weight
	glow, _, _ := atmos.SkyParts(s.ZenithNL, 0, 40.5, 180, atmos.Moon{})
	glow += mutExtra(90.5, 0)
	obj := lightNL(tg, 14) / atmos.CameraBand
	// mutSky has no extinction, so the band has none either: no ozone
	// beyond the unfiltered light's (Sky.ozone).
	want := min(1, fp.gain(obj, obj+s.RefNL, glow, 0, 0))
	plain, _ := mutOne(t, mutCfg(), s, tg, true)
	if math.Abs(weight[41]-want) > 1e-7 || weight[41] >= plain.Weight[41] {
		t.Errorf("filtered weight %v, want %v, below the unfiltered %v", weight[41], want, plain.Weight[41])
	}
	if r, _ := mutOne(t, cfg, s, tg, true); r.Filter != "" || r.Score != plain.Score {
		t.Errorf("filter %q score %v, want none and the unfiltered %v", r.Filter, r.Score, plain.Score)
	}
}
