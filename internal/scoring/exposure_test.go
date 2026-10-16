// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"fmt"
	"math"
	"os"
	"slices"
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

// Score passes each tally's buffer from the previous target back into
// channels, which accumulates into its channels: whatever the buffer held,
// the channels must be exactly a fresh call's, colour or mono, line filter,
// broadband or none, emission or continuum.
func TestChannelsReuse(t *testing.T) {
	cfg := expCfg()
	for _, mono := range []bool{false, true} {
		cfg.Mono = mono
		c, _ := newCamera(cfg, &Sky{})
		// Every channel non-zero, and three of them even for mono.
		dirty := c.channels(nil, "", 7, catalog.Target{Type: "HII region"}.Lines(), filterPass{})
		dirty = append(dirty[:0:0], dirty[0], dirty[0], dirty[0])
		for _, name := range []string{"", "ha", "dual", "quad", "cls", "lpro"} {
			for _, tg := range []catalog.Target{{Type: "HII region"}, {Type: "Planetary Nebula"}, {Type: "Galaxy"}} {
				fp := passes(cfg, tg, name)
				want := c.channels(nil, name, 7, tg.Lines(), fp)
				got := c.channels(slices.Clone(dirty), name, 7, tg.Lines(), fp)
				if !slices.Equal(got, want) {
					t.Errorf("mono %v, %q, %s: reused %+v, fresh %+v", mono, name, tg.Type, got, want)
				}
			}
		}
	}
}

// The photon zero point, area, QE, pixel and band pinned: e⁻/s per pixel
// per nanoLambert for the Origin, unfiltered, colour, each channel at its
// own mean response over the camera band.
func TestCameraGain(t *testing.T) {
	c, ok := newCamera(expCfg(), &Sky{})
	if !ok {
		t.Fatal("no camera")
	}
	area := math.Pi / 4 * (15.2*15.2 - 6.3*6.3)
	g := area * 0.83 * 1.2314 * 1.2314 * 1005 * 880 / atmos.NanoLamberts(0)
	plain := c.channels(nil, "", 7, [catalog.NumLines]float64{}, filterPass{})
	for i, qe := range imx678.Broad {
		if want := g * qe * 300 / 88; len(plain) != 3 || math.Abs(plain[i].obj/want-1) > 1e-9 || plain[i].obj != plain[i].sky[0] {
			t.Errorf("unfiltered channel %d %+v, want object and glow gain %v", i, plain[i], want)
		}
	}
	if plain[red].share+plain[green].share+plain[blue].share != 1 {
		t.Errorf("channel shares %+v", plain)
	}
	// Hα reaches every channel by its dye (ZWO's IMX678 curve): red pixels
	// at 0.77 of the peak, green at 0.34, blue at 0.11; the
	// quarter is the channel's share, not its gain. Mono is one channel of
	// every pixel at the mono response.
	cfg := expCfg()
	cfg.Mono = true
	mono, _ := newCamera(cfg, &Sky{})
	ha := [catalog.NumLines]float64{catalog.Ha: 1}
	col, mon := c.channels(nil, "ha", 7, ha, filterPass{}), mono.channels(nil, "ha", 7, ha, filterPass{})
	if len(mon) != 1 || mon[0].share != 1 || math.Abs(mon[0].obj/(g*0.95)-1) > 1e-9 || col[red].share != 0.25 {
		t.Errorf("Hα mono %+v", mon)
	}
	for i, r := range imx678.Lines[catalog.Ha] {
		if math.Abs(col[i].obj/(g*r)-1) > 1e-9 {
			t.Errorf("Hα colour channel %d: %v, want %v", i, col[i].obj, g*r)
		}
	}
	// [O III] mostly on green, half the array, and half as much on blue.
	if col := c.channels(nil, "oiii", 7, [catalog.NumLines]float64{catalog.OIII: 1}, filterPass{}); math.Abs(col[green].obj/(g*imx678.Lines[catalog.OIII][green])-1) > 1e-9 || math.Abs(col[blue].obj/(g*imx678.Lines[catalog.OIII][blue])-1) > 1e-9 || col[red].obj >= col[blue].obj/10 || col[green].share != 0.5 {
		t.Errorf("[O III] colour %+v", col)
	}
	if col := c.channels(nil, "ha", 7, [catalog.NumLines]float64{}, filterPass{}); col[red].obj != 0 {
		t.Error("a line filter passes nothing of an object without lines")
	}
	// Each band's sky falls on every channel by its dye: through dual-band,
	// 7 nm of glow at Hα and 7 nm at [O III], each at the channel's response
	// to its line, whatever the object's mix of the two lines.
	for _, lines := range [][catalog.NumLines]float64{catalog.Target{Type: "HII region"}.Lines(), catalog.Target{Type: "Planetary Nebula"}.Lines()} {
		ch := c.channels(nil, "dual", 7, lines, filterPass{})
		for i := range ch {
			if want := g * 7 / 88 * (imx678.Lines[catalog.Ha][i] + imx678.Lines[catalog.OIII][i]); math.Abs(ch[i].sky[0]/want-1) > 1e-9 {
				t.Errorf("dual-band glow gain %d: %v, want %v", i, ch[i].sky[0], want)
			}
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
	// 90 observable minutes (altitude 30-80°: minutes 31-80 and 101-140) of
	// 15 s subs, each followed by subGap.
	if e.Max != int(math.Floor(90*60/(15+subGap))) || e.Panels != 1 || e.Assumed {
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
	if e := Subs(cfg, s, r, time.Hour); e.Max != int(math.Floor(3600/(15+subGap))) || e.Good != single.Good {
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
	// Rescored, none for one no mosaic frames (scoreTarget) or one never up,
	// nor for one whose observable minutes -min-run rejects (its runs are
	// 50 and 40 minutes): the minutes counted must not count.
	big, low := r, r
	big.Target.Size = 60 // over the 45' short side
	low.Target.DecDeg = -80
	for name, rr := range map[string]Result{"unframable": big, "never up": low} {
		if e := Subs(cfg, s, rr, 0); e != (Estimate{}) {
			t.Errorf("rescored %s: %+v", name, e)
		}
	}
	long := expCfg()
	long.MinRun = time.Hour
	if e := Subs(long, s, r, 0); e != (Estimate{}) {
		t.Errorf("rescored under -min-run: %+v", e)
	}
	// A line filter passes none of a continuum's light: no channel sees it.
	ha := expCfg()
	ha.Filter = "ha"
	g := expResult(t, ha, s, expGalaxy(22))
	g.Filter = "ha"
	if e := Subs(ha, s, g, 0); e != (Estimate{}) {
		t.Errorf("galaxy through Hα: %+v", e)
	}
}

// Every line passes both broadband filters, so only a direct call reaches a
// wavelength outside them: the L-Pro's sodium notch, and just past an edge.
func TestInBands(t *testing.T) {
	lpro := config.Broadband["lpro"]
	for nm, want := range map[float64]bool{589: false, 700.01: false, 399.99: false, 400: true, 700: true, 656.3: true} {
		if got := inBands(nm, lpro); got != want {
			t.Errorf("inBands(%v, lpro) = %v, want %v", nm, got, want)
		}
	}
	if inBands(500, nil) {
		t.Error("no bands passed 500 nm")
	}
}

// An emission nebula's light is its lines, which the camera band collects
// once, not a continuum spread over it as the sky is: unfiltered its gain is
// the lines' response, and through a dual-band filter under the Moon the
// Origin needs fewer subs than without, as scoring's FILTER choice says.
func TestSubsEmissionLines(t *testing.T) {
	c, _ := newCamera(expCfg(), &Sky{})
	lines := catalog.Target{Type: "HII region"}.Lines()
	ch := c.channels(nil, "", 7, lines, filterPass{})
	g := c.area * c.qe * c.omega * photonsV0 / atmos.NanoLamberts(0)
	for i := range ch {
		var want float64
		for l, share := range lines {
			want += g * share * imx678.Lines[l][i]
		}
		if math.Abs(ch[i].obj/want-1) > 1e-12 || math.Abs(ch[i].sky[0]/(g*imx678.Broad[i]*300/88)-1) > 1e-12 {
			t.Errorf("unfiltered emission channel %d %+v, want object %v", i, ch[i], want)
		}
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
// an H II region's lines are 1.27 mag brighter than its V, a planetary's,
// [O III] inside V, about as bright; a galaxy's light is its V.
func TestLightNL(t *testing.T) {
	for _, c := range []struct {
		typ   string
		boost float64
	}{{"HII region", 1.27}, {"Supernova Remnant", 1.02}, {"Wolf-Rayet Nebula", 0.41}, {"Planetary Nebula", -0.08}, {"Galaxy", 0}} {
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
			fmt.Printf("ROW|%s|%s|%s|%.4f|%.4f|%.1f|%.2f", l, x.Name, x.Type, x.RADeg, x.DecDeg, x.Size, sb)
			for _, s := range ln {
				fmt.Printf("|%.4f", s)
			}
			fmt.Println()
		}
	}
}

// A colour sensor's channels add their SNR², each over its own pixels: through
// Hα the red quarter sees the nebula at 0.77 of the peak, the green half at
// 0.34 and the blue quarter at 0.11 (the IMX678's config.Response), against a mono sensor's
// 0.95 on every pixel. Read-noise limited a channel's SNR² goes as its
// response squared, so colour needs 0.95²/Σ share·r² = 4.3x mono's subs;
// sky limited, its sky in the band scaling with the same response, as its
// response, 0.95/Σ share·r = 2.4x. An average pixel charged the read noise of
// all four would need far more; one that saw Hα on red only, 4x either way.
func TestSubsBayerChannels(t *testing.T) {
	s := mutSky(141)
	neb := catalog.Target{Name: "N", Type: "HII region", RADeg: 90.5, Size: 20, SurfBr: 16}
	var lin, sq float64
	for i, share := range channelShare {
		r := imx678.Lines[catalog.Ha][i]
		lin += share * r
		sq += share * r * r
	}
	m := lineQE[catalog.Ha]
	for _, c := range []struct {
		name    string
		rn, sub float64
		want    float64
	}{{"read-noise limited", 100, 0.05, m * m / sq}, {"sky limited", 0, 0.05, m / lin}} {
		colour, mono := expCfg(), expCfg()
		colour.Filter, colour.ReadNoise, colour.Sub = "ha", c.rn, c.sub
		mono.Filter, mono.ReadNoise, mono.Sub, mono.Mono = "ha", c.rn, c.sub, true
		r := expResult(t, colour, s, neb)
		r.Filter = "ha"
		if ratio := float64(Subs(colour, s, r, 0).Good) / float64(Subs(mono, s, r, 0).Good); math.Abs(ratio/c.want-1) > 0.025 {
			t.Errorf("%s: colour needs %.2fx mono's subs through Hα, want %.2f", c.name, ratio, c.want)
		}
	}
}

// imx678 is the default colour response, the Origin's sensor's.
var imx678 = (&config.Config{}).ColourResponse()

// Dark current adds to each pixel's noise as the sky does, doubling every
// darkDoubling from 25 °C: an uncooled sensor sensorWarm above the air, a
// cooled one at -sensor-temp, where it all but vanishes.
func TestSubsDarkCurrent(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	r := expResult(t, cfg, s, expGalaxy(22))
	none := Subs(cfg, s, r, 0).Best
	cfg.Dark = 0.25
	at := func(temp float64) int {
		s.Temp = make([]float64, len(s.Grid))
		for i := range s.Temp {
			s.Temp[i] = temp
		}

		return Subs(cfg, s, r, 0).Best
	}
	cool, warm := at(5), at(25)
	if none >= cool || cool >= warm {
		t.Errorf("best: no dark %d, 5 °C %d, 25 °C %d", none, cool, warm)
	}
	c, _ := newCamera(cfg, s)
	if want := 0.25 * math.Exp2((25+sensorWarm-25)/darkDoubling); math.Abs(c.dark[0]/want-1) > 1e-12 {
		t.Errorf("dark at 25 °C air %v, want %v", c.dark[0], want)
	}
	cfg.SensorTemp, cfg.SensorTempSet = -20, true
	if cooled := at(25); math.Abs(float64(cooled)/float64(none)-1) > 0.01 {
		t.Errorf("cooled to -20 °C: %d, no dark %d", cooled, none)
	}
	// A hand-built Sky without temperatures has no dark current.
	if c, _ := newCamera(cfg, &Sky{}); c.dark != nil {
		t.Errorf("dark without temperatures: %v", c.dark)
	}
}

// The throughput scales the collecting area; -reject and subGap only the
// subs tonight allows, never the count a level needs.
func TestSubsThroughputReject(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	r := expResult(t, cfg, s, expGalaxy(22))
	full := Subs(cfg, s, r, 0)
	cfg.Throughput = 0.5
	half := Subs(cfg, s, r, 0)
	if half.SNRPerSub >= full.SNRPerSub*0.75 || half.Good <= full.Good {
		t.Errorf("throughput 0.5: %+v, 1: %+v", half, full)
	}
	cfg.Throughput, cfg.Reject = 0, 0.2
	if e := Subs(cfg, s, r, 0); e.Good != full.Good || e.Max != int(math.Floor(90*60/(15+subGap)*0.8)) {
		t.Errorf("reject 0.2: %+v, none %+v", e, full)
	}
}

// Equal weights reach (ΣS)²/Σσ², so a noisy sub dilutes the stack where an
// optimally weighted one adds S²/σ²; the residual caps the SNR at
// S/(residual·B), and need solves 1/L² = 1/(N·m) + 1/ceil.
func TestStackRates(t *testing.T) {
	chans := []channel{{share: 1}}
	// Two minutes of S = 10: σ² 20 (dark) and 180 (moonlit), 2000 e⁻ of sky.
	st := stack{n: 2, snr2: 100.0/20 + 100.0/180, sig: [3]float64{20}, noise: [3]float64{200}, sky: [3]float64{2000}}
	c := camera{omega: 1}
	if m, ceil := c.rates(&st, chans); math.Abs(m-(100.0/20+100.0/180)/2) > 1e-12 || !math.IsInf(ceil, 1) {
		t.Errorf("weighted: m %v, ceil %v", m, ceil)
	}
	c.mean = true
	if m, _ := c.rates(&st, chans); math.Abs(m-400.0/200/2) > 1e-12 {
		t.Errorf("mean: m %v, want 1", m)
	}
	// 1% of 1000 e⁻ of sky per sub against 10 e⁻ of object: SNR 1 at most.
	c.residual = 0.01
	if _, ceil := c.rates(&st, chans); math.Abs(ceil-1) > 1e-12 {
		t.Errorf("ceil %v, want 1", ceil)
	}
	if n := need(10, 1, 400); n != 134 {
		t.Errorf("need(10, 1, 400) = %d, want 134", n)
	}
	if need(10, 1, 100) != maxSubs || need(10, 0, math.Inf(1)) != maxSubs || need(10, 1, math.Inf(1)) != 100 {
		t.Error("need at and past the ceiling, or without signal, must be maxSubs")
	}
	// A reachable level always beats an unreachable one, and among those
	// the higher ceiling wins.
	if subsGain(0.01, 101) <= 0 || subsGain(5, 50) >= 0 || subsGain(5, 50) <= subsGain(5, 20) {
		t.Errorf("subsGain: %v, %v, %v", subsGain(0.01, 101), subsGain(5, 50), subsGain(5, 20))
	}
}

// The blur keeps a large object's surface brightness, halves a disc as wide
// as the FWHM and grows with the airmass (X^0.6).
func TestBlur(t *testing.T) {
	c := camera{seeing: []float64{2}}
	if f := c.blur(0, 1, 600, 600); f < 0.9999 {
		t.Errorf("10' disc: %v", f)
	}
	if f := c.blur(0, 1, 2, 2); math.Abs(f-0.5) > 1e-12 {
		t.Errorf("2\" disc under 2\": %v, want 0.5", f)
	}
	if c.blur(0, 2, 2, 2) >= c.blur(0, 1, 2, 2) || c.blur(0, 1, 0, 0) != 1 {
		t.Error("blur must grow with airmass, and leave a sizeless object")
	}
	c.optics2 = 4
	if f := c.blur(0, 1, 2, 2); math.Abs(f-4.0/12) > 1e-12 {
		t.Errorf("with 2\" optics: %v, want 1/3", f)
	}
}

// Through Subs: the residual caps a faint galaxy under a bright sky (a
// finite SNRLimit, more subs, unreachable past it), and a small planetary
// needs more subs under worse seeing.
func TestSubsResidualSeeing(t *testing.T) {
	cfg, s := expCfg(), mutSky(141)
	s.ZenithNL = atmos.NanoLamberts(18)
	r := expResult(t, cfg, s, expGalaxy(22))
	free := Subs(cfg, s, r, 0)
	cfg.Residual = 0.002
	capped := Subs(cfg, s, r, 0)
	if free.SNRLimit != 0 || capped.SNRLimit <= 0 || capped.Good <= free.Good {
		t.Errorf("residual: %+v, none %+v", capped, free)
	}
	cfg.Residual = 0.05
	if e := Subs(cfg, s, r, 0); e.Good != maxSubs || e.SNRLimit >= Levels[1] {
		t.Errorf("residual 5%%: %+v", e)
	}
	cfg.Residual = 0
	cfg.SizeMin = 0
	pn := catalog.Target{Name: "P", Type: "Planetary Nebula", RADeg: 90.5, Size: 0.2, SurfBr: 20}
	pr := expResult(t, cfg, s, pn)
	seeing := func(fwhm float64) int {
		s.Seeing = make([]float64, len(s.Grid))
		for i := range s.Seeing {
			s.Seeing[i] = fwhm
		}

		return Subs(cfg, s, pr, 0).Good
	}
	if good, poor := seeing(1), seeing(4); good >= poor {
		t.Errorf("12\" planetary: %d subs under 1\", %d under 4\"", good, poor)
	}
}
