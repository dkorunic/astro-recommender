// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"fmt"
	"math"
	"os"
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
// per nanoLambert for the Origin, unfiltered, colour, each channel alike.
func TestCameraGain(t *testing.T) {
	c, ok := newCamera(expCfg())
	if !ok {
		t.Fatal("no camera")
	}
	area := math.Pi / 4 * (15.2*15.2 - 6.3*6.3)
	g := area * 0.83 * 1.2314 * 1.2314 * 1005 * 880 / atmos.NanoLamberts(0)
	plain := c.channels(nil, "", 7, [4]float64{}, filterPass{})
	if want := g * 0.34 * 300 / 88; len(plain) != 3 || math.Abs(plain[red].obj/want-1) > 1e-9 || plain[red].obj != plain[blue].obj || plain[red].obj != plain[red].sky[0] {
		t.Errorf("unfiltered channels %+v, want object and glow gain %v on each", plain, want)
	}
	if plain[red].share+plain[green].share+plain[blue].share != 1 {
		t.Errorf("channel shares %+v", plain)
	}
	// Hα reaches the red channel only, at the full response of each of its
	// pixels: the quarter is the channel's share, not its gain. Mono is one
	// channel of every pixel at that response.
	cfg := expCfg()
	cfg.Mono = true
	mono, _ := newCamera(cfg)
	ha := [4]float64{catalog.Ha: 1}
	col, mon := c.channels(nil, "ha", 7, ha, filterPass{}), mono.channels(nil, "ha", 7, ha, filterPass{})
	if len(mon) != 1 || mon[0].share != 1 || col[red].obj != mon[0].obj || col[green].obj != 0 || col[blue].obj != 0 || col[red].share != 0.25 {
		t.Errorf("Hα colour %+v, mono %+v", col, mon)
	}
	// [O III] on green, half the array.
	if col := c.channels(nil, "oiii", 7, [4]float64{catalog.OIII: 1}, filterPass{}); col[green].obj == 0 || col[red].obj != 0 || col[green].share != 0.5 {
		t.Errorf("[O III] colour %+v", col)
	}
	if col := c.channels(nil, "ha", 7, [4]float64{}, filterPass{}); col[red].obj != 0 {
		t.Error("a line filter passes nothing of an object without lines")
	}
	// Each band's sky falls on its own channel: through dual-band, 7 nm of
	// glow at Hα on the red, 7 nm at [O III] on the green, none on the blue,
	// whatever the object's mix of the two lines.
	for _, lines := range [][4]float64{catalog.Target{Type: "HII region"}.Lines(), catalog.Target{Type: "Planetary Nebula"}.Lines()} {
		ch := c.channels(nil, "dual", 7, lines, filterPass{})
		if r, gr := g*7/88*0.95, g*7/88*0.85; math.Abs(ch[red].sky[0]/r-1) > 1e-9 || math.Abs(ch[green].sky[0]/gr-1) > 1e-9 || ch[blue].sky[0] != 0 {
			t.Errorf("dual-band glow gains %v %v %v, want %v %v 0", ch[red].sky[0], ch[green].sky[0], ch[blue].sky[0], r, gr)
		}
	}
	// Unfiltered, the camera band's moonlight is bluer than V's: its
	// Rayleigh part per nm is the λ⁻⁴ mean over 400-700 nm, 1.30 of V's.
	if sky := plain[red].sky; math.Abs(sky[1]/sky[0]-1.302) > 0.001 || sky[2] <= sky[0] {
		t.Errorf("camera-band Rayleigh/glow %v, Mie %v", sky[1]/sky[0], sky[2]/sky[0])
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
// camera's red pixels are read-noise limited through Hα under a dark sky,
// so there the filter gains less than scoring, which has no read noise,
// credits it with.
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
	ch := c.channels(nil, "", 7, lines, filterPass{})
	g := c.area * c.qe * c.omega * photonsV0 / atmos.NanoLamberts(0)
	reds := g * (lines[catalog.Ha]*lineQE[catalog.Ha] + lines[catalog.SII]*lineQE[catalog.SII])
	greens := g * (lines[catalog.OIII]*lineQE[catalog.OIII] + lines[catalog.Hb]*lineQE[catalog.Hb])
	if math.Abs(ch[red].obj/reds-1) > 1e-12 || math.Abs(ch[green].obj/greens-1) > 1e-12 || ch[blue].obj != 0 || math.Abs(ch[blue].sky[0]/(g*broadQEColour*300/88)-1) > 1e-12 {
		t.Errorf("unfiltered emission channels %+v", ch)
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

// Through tri or quad, whose lines are all of an emission nebula's light,
// it collects no more than with no filter: scoring's w counts Hβ beyond the
// lines' sum (above 1), which as an absolute count would be photons from
// nowhere. Object-limited (bright object, no read noise, no extinction) the
// counts go as 1/S, so they must match.
func TestSubsLineShare(t *testing.T) {
	s := mutSky(141)
	// Short subs keep the count large enough to resolve.
	neb := catalog.Target{Name: "N", Type: "HII region", RADeg: 90.5, Size: 20, SurfBr: 16}
	plain, quad := expCfg(), expCfg()
	plain.Mono, plain.ReadNoise, plain.Sub = true, 0, 0.05
	quad.Mono, quad.ReadNoise, quad.Sub, quad.Filter = true, 0, 0.05, "quad"
	r := expResult(t, quad, s, neb)
	r.Filter = "quad"
	n, q := Subs(plain, s, expResult(t, plain, s, neb), 0).Best, Subs(quad, s, r, 0).Best
	if float64(q) < 0.98*float64(n) {
		t.Errorf("quad %d subs, unfiltered %d: quad collects more than every line", q, n)
	}
}

// Weather weighs each minute's SNR², not the night's mean: cloud during the
// target's best (dark) minutes only must cost what losing them costs.
func TestSubsWeatherPerMinute(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	for i := 70; i < len(s.Grid); i++ {
		moonlit(s, i)
	}
	s.MoonSep = 0
	r := expResult(t, cfg, s, expGalaxy(22))
	dark := r
	dark.Obs = make([]bool, len(r.Obs))
	copy(dark.Obs[:70], r.Obs[:70])
	nd, n := 0, 0
	for i, ok := range r.Obs {
		if ok {
			n++
			if i < 70 {
				nd++
			}
		}
	}
	darkOnly := Subs(cfg, s, dark, 0).Best
	// Clear only while moonlit: the dark minutes' subs are all lost.
	for i := range s.Quality {
		if i < 70 {
			s.Quality[i] = 0
		}
	}
	lit := r
	lit.Obs = make([]bool, len(r.Obs))
	copy(lit.Obs[70:], r.Obs[70:])
	s2 := *s
	s2.Quality = make([]float64, len(s.Quality))
	for i := range s2.Quality {
		s2.Quality[i] = 1
	}
	litOnly := Subs(cfg, &s2, lit, 0).Best
	// Over the whole night, only the lit minutes count, each at 1/n of it.
	want := float64(litOnly) * float64(n) / float64(n-nd)
	if got := Subs(cfg, s, r, 0).Best; math.Abs(float64(got)/want-1) > 0.02 {
		t.Errorf("clouded dark half: %d subs, want %.0f (dark-only %d)", got, want, darkOnly)
	}
}

// A night the forecast clouds out is an estimate past tonight's limit, not
// none: "-" is for objects no estimate applies to.
func TestSubsCloudedOut(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	r := expResult(t, cfg, s, expGalaxy(22))
	for i := range s.Quality {
		s.Quality[i] = 0
	}
	e := Subs(cfg, s, r, 0)
	if e.Good != maxSubs || e.Max <= 0 || e.Good <= e.Max || e.SNRPerSub != 0 {
		t.Errorf("clouded out: %+v, want good %d past a positive max, no SNR", e, maxSubs)
	}
}

// An emission-line target's V surface brightness measures its lines only as
// far as the V passband reaches them, and its catalogue magnitude also counts
// stars and continuum (vContinuum, fitted by scripts/calibrate_lines.py):
// an H II region's lines are 1.19 mag brighter than its V, a planetary's,
// [O III] inside V, as bright; a galaxy's light is its V.
func TestLightNL(t *testing.T) {
	for _, c := range []struct {
		typ   string
		boost float64
	}{{"HII region", 1.19}, {"Supernova Remnant", 1.10}, {"Wolf-Rayet Nebula", 0.52}, {"Planetary Nebula", 0.03}, {"Galaxy", 0}} {
		tg := catalog.Target{Type: c.typ}
		if got := atmos.MagFromNL(atmos.NanoLamberts(22)) - atmos.MagFromNL(lightNL(tg, 22)); math.Abs(got-c.boost) > 0.01 {
			t.Errorf("%s: lines %.3f mag brighter than V, want %.2f", c.typ, got, c.boost)
		}
	}
}

// TestCalibrationSample prints the emission nebulae scripts/calibrate_lines.py
// fits vContinuum on: at least 30' across (the Hα map's beam is 6'), with a
// surface brightness as scoring derives it. Run only by the script.
func TestCalibrationSample(t *testing.T) {
	if os.Getenv("CALIBRATE_LINES") == "" {
		t.Skip("run by scripts/calibrate_lines.py")
	}
	seen := map[string]bool{}
	for _, l := range []string{"GaryImm", "Messier", "OpenNGC", "OpenIC", "Sharpless", "Caldwell"} {
		ts, _, err := catalog.Load(l, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range ts {
			sb, _ := x.SurfaceBrightness()
			if !catalog.EmissionLine(x) || x.Size < 30 || sb <= 0 || seen[catalog.NameKey(x.Name)] {
				continue
			}
			seen[catalog.NameKey(x.Name)] = true
			ln := x.Lines()
			fmt.Printf("ROW|%s|%s|%s|%.4f|%.4f|%.1f|%.2f|%.4f|%.4f|%.4f|%.4f\n", l, x.Name, x.Type, x.RADeg, x.DecDeg, x.Size, sb, ln[0], ln[1], ln[2], ln[3])
		}
	}
}

// A colour sensor's channels add their SNR², each over its own pixels: through
// Hα only the red quarter sees the nebula, and the green and blue pixels,
// seeing nothing, add nothing to an optimally weighted stack, their read
// noise included. So it needs 4x a mono sensor's subs whether the read noise
// or the sky limits them; an average pixel, charged the read noise of all
// four, would need 16x read-noise limited.
func TestSubsBayerChannels(t *testing.T) {
	s := mutSky(141)
	neb := catalog.Target{Name: "N", Type: "HII region", RADeg: 90.5, Size: 20, SurfBr: 16}
	for _, c := range []struct {
		name    string
		rn, sub float64
		lo, hi  float64
	}{{"read-noise limited", 100, 0.05, 3.9, 4.1}, {"sky limited", 0, 0.05, 3.9, 4.1}} {
		colour, mono := expCfg(), expCfg()
		colour.Filter, colour.ReadNoise, colour.Sub = "ha", c.rn, c.sub
		mono.Filter, mono.ReadNoise, mono.Sub, mono.Mono = "ha", c.rn, c.sub, true
		r := expResult(t, colour, s, neb)
		r.Filter = "ha"
		if ratio := float64(Subs(colour, s, r, 0).Good) / float64(Subs(mono, s, r, 0).Good); ratio < c.lo || ratio > c.hi {
			t.Errorf("%s: colour needs %.2fx mono's subs through Hα, want 4", c.name, ratio)
		}
	}
}
