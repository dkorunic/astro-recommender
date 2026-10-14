// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

// expCfg is mutCfg with the Origin's frame and camera.
func expCfg() *config.Config {
	cfg := mutCfg()
	cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort = true, 1.2314, 79.2, 45
	cfg.Aperture, cfg.Obstruction, cfg.Sub, cfg.QE, cfg.ReadNoise = 152, 63, 15, 0.83, 2.15

	return cfg
}

// expResult scores tg on s with per-minute data, as main's results have
// with -plan, so Subs reads its Obs.
func expResult(t *testing.T, cfg *config.Config, s *Sky, tg catalog.Target) Result {
	t.Helper()
	r, ok := mutOne(t, cfg, s, tg, true)
	if !ok {
		t.Fatalf("%s not observable", tg.Name)
	}

	return r
}

// moonlit puts a full Moon 45° up at minute i of s, with the extinction
// (0.2 mag/airmass) its light needs to scatter.
func moonlit(s *Sky, i int) {
	s.Ext[i] = 0.2
	s.ExtRayleigh[i], s.ExtAerosol[i] = atmos.SplitExtinction(0.2)
	s.MoonAlt[i] = 45
	s.MoonLight[i] = atmos.MoonLight(0.2, s.ExtRayleigh[i], s.ExtAerosol[i], 45, 0, 60.27)
}

func expGalaxy(sb float64) catalog.Target {
	return catalog.Target{Name: "G", Type: "Galaxy", RADeg: 90.5, Size: 20, SurfBr: sb}
}

// The photon zero point, area, QE, pixel and band pinned: e⁻/s per pixel
// per nanoLambert for the Origin, unfiltered, colour.
func TestCameraGain(t *testing.T) {
	gain := func(c camera, name string, lines [4]float64) float64 { obj, _ := c.gains(name, lines); return obj }
	c, ok := newCamera(expCfg())
	if !ok {
		t.Fatal("no camera")
	}
	area := math.Pi / 4 * (15.2*15.2 - 6.3*6.3)
	want := area * 0.83 * 1.2314 * 1.2314 * 1005 * 880 / atmos.NanoLamberts(0) * 0.34 * 300 / 88
	if got := gain(c, "", [4]float64{}); math.Abs(got/want-1) > 1e-9 {
		t.Errorf("gain %v, want %v", got, want)
	}
	// A colour sensor's Hα is its red quarter; mono sees it with every pixel.
	lines := [4]float64{catalog.Ha: 1}
	cfg := expCfg()
	cfg.Mono = true
	mono, _ := newCamera(cfg)
	if r := gain(c, "ha", lines) / gain(mono, "ha", lines); math.Abs(r-0.25) > 1e-12 {
		t.Errorf("colour/mono Hα %v, want 0.25", r)
	}
	// [O III] on green: half the pixels.
	lines = [4]float64{catalog.OIII: 1}
	if r := gain(c, "oiii", lines) / gain(mono, "oiii", lines); math.Abs(r-0.5) > 1e-12 {
		t.Errorf("colour/mono [O III] %v, want 0.5", r)
	}
	if gain(c, "ha", [4]float64{}) != 0 {
		t.Error("a line filter passes nothing of an object without lines")
	}
}

// The absolute scale against what Origin users report: a galaxy of SB 22
// under a 21 mag/arcsec² sky reaches the good level in 10-120 minutes of
// 15 s subs (M 31 from a Bortle 4-5 site takes about an hour). A wrong zero
// point or area moves it by a large factor.
func TestSubsReality(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	s.ZenithNL = atmos.NanoLamberts(21)
	e := Subs(cfg, s, expResult(t, cfg, s, expGalaxy(22)), 0)
	if minutes := float64(e.Good) * e.Sub / 60; minutes < 10 || minutes > 120 {
		t.Errorf("good level in %.0f minutes (%+v)", minutes, e)
	}
	if e.Modest >= e.Good || e.Good >= e.Best || e.Best < 15*e.Modest || e.Best > 17*e.Modest {
		t.Errorf("levels %+v: best should be 16x modest (SNR 20 vs 5)", e)
	}
	// 90 observable minutes (altitude 30-80°: minutes 31-80 and 101-140) of 15 s subs.
	if e.Max != 90*60/15 || e.Panels != 1 || e.Assumed {
		t.Errorf("%+v", e)
	}
}

func TestSubsMonotonic(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	base := Subs(cfg, s, expResult(t, cfg, s, expGalaxy(22)), 0).Good
	if g := Subs(cfg, s, expResult(t, cfg, s, expGalaxy(23)), 0).Good; g <= base {
		t.Errorf("fainter: %d, base %d", g, base)
	}
	s2 := mutSky(141)
	s2.ZenithNL = atmos.NanoLamberts(19)
	if g := Subs(cfg, s2, expResult(t, cfg, s2, expGalaxy(22)), 0).Good; g <= base {
		t.Errorf("brighter sky: %d, base %d", g, base)
	}
	s3 := mutSky(141)
	for i := range s3.Quality {
		s3.Quality[i] = 0.5
	}
	if g := Subs(cfg, s3, expResult(t, cfg, s3, expGalaxy(22)), 0).Good; g < 2*base-1 || g > 2*base+1 {
		t.Errorf("half the subs lost: %d, want ~%d", g, 2*base)
	}
	big := expCfg()
	big.Aperture, big.Obstruction = 300, 0
	if g := Subs(big, s, expResult(t, big, s, expGalaxy(22)), 0).Good; g >= base {
		t.Errorf("larger aperture: %d, base %d", g, base)
	}
}

// Under a bright Moon an Hα filter beats no filter on an emission nebula
// once the sky, not the read noise, limits the sub (mono, 300 s), and a mono
// camera beats a colour one through it. At the Origin's 15 s a colour
// camera's Hα quarter is read-noise limited, so there the filter gains
// little or nothing: scoring, which has no read noise, can still pick it.
func TestSubsFilter(t *testing.T) {
	s := mutSky(141)
	// Scattered moonlight needs extinction: mutSky has none.
	for i := range s.Grid {
		moonlit(s, i)
	}
	s.MoonSep = 0
	neb := catalog.Target{Name: "N", Type: "HII region", RADeg: 90.5, Size: 20, SurfBr: 22}
	plain := expCfg()
	plain.Mono, plain.Sub = true, 300
	none := Subs(plain, s, expResult(t, plain, s, neb), 0).Good
	ha := expCfg()
	ha.Filter, ha.Mono, ha.Sub = "ha", true, 300
	r := expResult(t, ha, s, neb)
	if r.Filter != "ha" {
		t.Fatalf("the Hα filter did not help: %+v", r)
	}
	mono := Subs(ha, s, r, 0).Good
	if mono >= none {
		t.Errorf("Hα %d, unfiltered %d", mono, none)
	}
	ha.Mono = false
	if colour := Subs(ha, s, r, 0).Good; colour <= mono {
		t.Errorf("colour %d, mono %d", colour, mono)
	}
}

// The stack adds SNR²: a night half dark, half moonlit needs the harmonic
// mean of the halves' counts, not a count from averaged rates.
func TestSubsSNR2Average(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	for i := 70; i < len(s.Grid); i++ {
		moonlit(s, i)
	}
	s.MoonSep = 0
	r := expResult(t, cfg, s, expGalaxy(22))
	half := func(from, to int) int {
		h := r
		h.Obs = make([]bool, len(r.Obs))
		copy(h.Obs[from:to], r.Obs[from:to])

		return Subs(cfg, s, h, 0).Best
	}
	dark, lit, both := half(0, 70), half(70, len(s.Grid)), Subs(cfg, s, r, 0).Best
	// Weighted by each half's observable minutes.
	nd, nl := 0, 0
	for i, ok := range r.Obs {
		switch {
		case ok && i < 70:
			nd++
		case ok:
			nl++
		}
	}
	want := float64(nd+nl) / (float64(nd)/float64(dark) + float64(nl)/float64(lit))
	if math.Abs(float64(both)/want-1) > 0.02 {
		t.Errorf("both %d, want %.0f (dark %d over %d min, lit %d over %d min)", both, want, dark, nd, lit, nl)
	}
}

func TestSubsMosaicAndPlan(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	r := expResult(t, cfg, s, expGalaxy(22))
	single := Subs(cfg, s, r, 0)
	r.Mosaic = config.Mosaic{Cols: 2, Rows: 1}
	two := Subs(cfg, s, r, 0)
	if two.Good != single.Good || two.Panels != 2 || two.Max != single.Max/2 {
		t.Errorf("mosaic %+v, single %+v", two, single)
	}
	r.Mosaic = config.Mosaic{}
	if e := Subs(cfg, s, r, time.Hour); e.Max != 240 || e.Good != single.Good {
		t.Errorf("planned hour: %+v", e)
	}
}

func TestSubsNoCamera(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	r := expResult(t, cfg, s, expGalaxy(22))
	noFraming, handBuilt := expCfg(), expCfg()
	noFraming.Framing = false
	handBuilt.Aperture, handBuilt.Sub, handBuilt.QE = 0, 0, 0
	for name, c := range map[string]*config.Config{"no framing": noFraming, "hand-built cfg": handBuilt} {
		if e := Subs(c, s, r, 0); e != (Estimate{}) {
			t.Errorf("%s: %+v", name, e)
		}
	}
	cl := r
	cl.Target = catalog.Target{Name: "K", Type: "Open Cluster", RADeg: 90.5, Size: 20}
	if e := Subs(cfg, s, cl, 0); e != (Estimate{}) {
		t.Errorf("cluster: %+v", e)
	}
	rain := mutSky(141)
	for i := range rain.Quality {
		rain.Quality[i] = 0
	}
	if e := Subs(cfg, rain, r, 0); e != (Estimate{}) {
		t.Errorf("rained out: %+v", e)
	}
	never := r
	never.Obs = make([]bool, len(r.Obs))
	if e := Subs(cfg, s, never, 0); e != (Estimate{}) {
		t.Errorf("never observable: %+v", e)
	}
	// Absurdly faint: capped, not an overflowed int.
	faint := expResult(t, cfg, s, expGalaxy(34))
	if e := Subs(cfg, s, faint, 0); e.Best != maxSubs || e.Good <= 0 {
		t.Errorf("faint: %+v", e)
	}
	// An unknown surface brightness takes the class's typical one.
	if e := Subs(cfg, s, expResult(t, cfg, s, expGalaxy(0)), 0); !e.Assumed || e.Good == 0 {
		t.Errorf("unknown SB: %+v", e)
	}
	// Without per-minute data Subs rescores the target.
	r.Obs = nil
	if e := Subs(cfg, s, r, 0); e.Good == 0 {
		t.Errorf("rescored: %+v", e)
	}
}

// An emission nebula's light is its lines, which the camera band collects
// once, not a continuum spread over it as the sky is: unfiltered its gain is
// the lines' response, and through a dual-band filter under the Moon the
// Origin needs fewer subs than without, as scoring's FILTER choice says.
func TestSubsEmissionLines(t *testing.T) {
	c, _ := newCamera(expCfg())
	lines := catalog.Target{Type: "HII region"}.Lines()
	obj, sky := c.gains("", lines)
	var resp, sum float64
	for l, w := range lines {
		resp += w * lineQE[l] * bayer[l]
		sum += w
	}
	g := c.area * c.qe * c.omega * photonsV0 / atmos.NanoLamberts(0)
	if math.Abs(obj/(g*resp/sum)-1) > 1e-12 || math.Abs(sky/(g*broadQEColour*300/88)-1) > 1e-12 {
		t.Errorf("unfiltered emission gains %v %v", obj, sky)
	}
	if obj, sky := c.gains("", [4]float64{}); obj != sky {
		t.Errorf("a continuum object shares the sky's gain: %v %v", obj, sky)
	}
	s := mutSky(141)
	for i := range s.Grid {
		moonlit(s, i)
	}
	s.MoonSep = 0
	neb := catalog.Target{Name: "N", Type: "HII region", RADeg: 90.5, Size: 20, SurfBr: 22}
	plain, dual := expCfg(), expCfg()
	dual.Filter = "dual"
	r := expResult(t, dual, s, neb)
	if r.Filter != "dual" {
		t.Fatalf("dual did not help: %+v", r)
	}
	if f, n := Subs(dual, s, r, 0).Good, Subs(plain, s, expResult(t, plain, s, neb), 0).Good; f >= n {
		t.Errorf("dual %d, unfiltered %d", f, n)
	}
}
