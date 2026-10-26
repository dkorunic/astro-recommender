// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"cmp"
	"math"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// Each scope's frame, worked out from its pixels and focal length, is the
// field of view its maker states (to 3%, tol where the maker rounds or
// disagrees with itself; a diagonal when that is all it gives), so the chip,
// binning and optics in the table agree; its camera is a preset whose colour
// version it can take, or none.
func TestScopes(t *testing.T) {
	stated := map[string]struct{ long, short, diag, tol float64 }{ // degrees
		"origin":          {long: 1.32, short: 0.75},
		"origin-mk1":      {long: 1.27, short: 0.85},
		"seestar-s50":     {long: 1.29, short: 0.73},
		"seestar-s30":     {diag: 2.46},
		"seestar-s30-pro": {diag: 4.6},
		"seestar-s50-pro": {diag: 2.8},
		"dwarf-2":         {}, // no maker figure; reviews round to 3°
		"dwarf-3":         {long: 2.95, short: 1.66},
		"dwarf-mini":      {diag: 2.45},
		"draco":           {long: 1.649, short: 1.238},
		"stellina":        {diag: 1.25},
		"vespera":         {long: 1.6, short: 0.9},
		"vespera-2":       {long: 2.5, short: 1.4},
		"vespera-pro":     {long: 1.6, short: 1.6},
		// Vaonis states 245 mm but the Vespera II's 250 mm field and 2.39"/px.
		"vespera-3":     {long: 2.5, short: 1.4, tol: 0.05},
		"vespera-pro-2": {long: 1.6, short: 1.6, tol: 0.05},
		"atlas":         {long: 1.78, short: 1.00},
		"evscope":       {long: 37.0 / 60, short: 27.0 / 60, tol: 0.04}, // rounded to the minute
		"evscope-2":     {long: 45.6 / 60, short: 34.2 / 60},
		"odyssey":       {long: 45.0 / 60, short: 33.6 / 60},
		"orbis-60edx":   {}, // trade-show specifications state no field
	}
	if len(stated) != len(Scopes) {
		t.Errorf("%d stated fields for %d scopes", len(stated), len(Scopes))
	}
	// Every preset is alt-az; Draco's rotating sensor and the ORBIS'
	// de-rotator take the field rotation out.
	derotated := map[string]bool{"draco": true, "orbis-60edx": true}
	for key, s := range Scopes {
		if want := map[bool]string{true: MountDerotated, false: MountAltAz}[derotated[key]]; s.Mount != want {
			t.Errorf("%s: mount %q, want %q", key, s.Mount, want)
		}
	}
	for key, s := range Scopes {
		long, short := s.fov()
		long, short = long/60, short/60
		want, ok := stated[key]
		tol := cmp.Or(want.tol, 0.03)
		near := func(got, want float64) bool { return want == 0 || math.Abs(got/want-1) <= tol }
		if !ok || !near(long, want.long) || !near(short, want.short) || !near(math.Hypot(long, short), want.diag) {
			t.Errorf("%s: %.3f° x %.3f° (diagonal %.3f°), maker %+v", key, long, short, math.Hypot(long, short), want)
		}
		if s.Name == "" || (s.Sensor == "") == (s.Camera == "") || s.Aperture <= s.Obstruction || s.Focal <= 0 || s.Sub <= 0 || s.PxLong < s.PxShort {
			t.Errorf("%s: %+v", key, s)
		}
		if c, ok := Cameras[s.Camera]; s.Camera != "" && (!ok || c.Colour == nil) {
			t.Errorf("%s: camera %q has no colour preset", key, s.Camera)
		}
	}
}

// -scope sets the frame and, where its chip has a preset and its obstruction
// is published, the sub-exposure estimate's camera; its key ignores case
// and spaces.
func TestScopeFlag(t *testing.T) {
	a, err := mutParse(t, mutBase("-scope", "origin")...)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mutParse(t, mutBase("-scope", " ORIGIN ")...)
	if err != nil || a.FOVLong != b.FOVLong || a.Scale != b.Scale || a.Obstruction != b.Obstruction || a.Scope != b.Scope {
		t.Errorf("-scope origin %+v and -scope \" ORIGIN \" %+v differ (%v)", a, b, err)
	}
	s := Scopes["vespera-pro"]
	cfg, err := mutParse(t, mutBase("-scope", "vespera-pro")...)
	if err != nil || !cfg.Framing || cfg.Scale != s.scale() || cfg.FOVLong != cfg.FOVShort || cfg.ColourResponse() != Cameras["imx676"].Colour.Response {
		t.Errorf("vespera-pro: framing %v scale %v FOV %v x %v (%v)", cfg.Framing, cfg.Scale, cfg.FOVLong, cfg.FOVShort, err)
	}
	// Given values still win; a scope without a camera model takes SUBS from them.
	cfg, err = mutParse(t, mutBase("-scope", "draco", "-qe", "0.8", "-read-noise", "1.2", "-obstruction", "40", "-sub", "60")...)
	if err != nil || cfg.QE != 0.8 || cfg.ReadNoise != 1.2 || cfg.Obstruction != 40 || cfg.Sub != 60 || cfg.Aperture != 90 {
		t.Errorf("draco with a camera: %+v (%v)", cfg, err)
	}
}

// -scope alone sets everything the sub-exposure estimate and the frame read,
// for every preset: the frame, optics, sub, its chip's QE, read noise (the
// scope's own over the preset's) and colour response, and the -filter-std
// beam; no chip preset or an unpublished obstruction leaves QE and read
// noise 0, so SUBS is left out.
func TestScopeSetsAll(t *testing.T) {
	for key, s := range Scopes {
		cfg, err := mutParse(t, mutBase("-scope", key, "-filter", "ha", "-filter-std")...)
		if s.NoSubs {
			// -filter-std needs the unpublished obstruction.
			if err == nil {
				t.Errorf("%s: -filter-std without -obstruction accepted", key)
			}
			cfg, err = mutParse(t, mutBase("-scope", key)...)
		}
		if err != nil {
			t.Errorf("%s: %v", key, err)

			continue
		}
		long, short := s.fov()
		if !cfg.Framing || cfg.Scope != key || cfg.Camera != s.Camera || cfg.Mono ||
			cfg.FOVLong != long || cfg.FOVShort != short || cfg.Scale != s.scale() ||
			cfg.Aperture != s.Aperture || cfg.Obstruction != s.Obstruction || cfg.Sub != s.Sub {
			t.Errorf("%s: frame/optics %+v, want %+v", key, cfg, s)
		}
		var qe, rn float64
		if v := s.variant(); v != nil {
			qe, rn = v.QE, v.ReadNoise
			if cfg.ColourResponse() != Cameras[s.Camera].Colour.Response {
				t.Errorf("%s: response not %s's", key, s.Camera)
			}
		}
		if cfg.QE != qe || cfg.ReadNoise != rn || (rn != 0 && s.ReadNoise != 0 && rn != s.ReadNoise) {
			t.Errorf("%s: QE %v RN %v, want %v %v", key, cfg.QE, cfg.ReadNoise, qe, rn)
		}
		if !s.NoSubs && (cfg.FRatio != s.Focal/s.Aperture || cfg.Obstructed != s.Obstruction/s.Aperture) {
			t.Errorf("%s: beam f/%v %v, want f/%v %v", key, cfg.FRatio, cfg.Obstructed, s.Focal/s.Aperture, s.Obstruction/s.Aperture)
		}
		if cfg.ScopeLabel() == "" || cfg.SizeMax != cfg.MaxFramed() {
			t.Errorf("%s: label %q, size-max %v of %v", key, cfg.ScopeLabel(), cfg.SizeMax, cfg.MaxFramed())
		}
	}
}

// -focal with -pixel alone keeps the scope's sensor, not its field: the field
// at the new focal length matches the scale, so the frame keeps its pixels.
func TestScopeFocal(t *testing.T) {
	s := Scopes["seestar-s50"]
	cfg, err := mutParse(t, mutBase("-scope", "seestar-s50", "-focal", "1000", "-pixel", "2.9")...)
	if err != nil {
		t.Fatal(err)
	}
	if px, _ := cfg.FramePx(); math.Abs(px-float64(s.PxLong)) > 1 || math.Abs(cfg.FOVShort-fovSide(float64(s.PxShort)*s.Pixel/1000, 1000)) > 1e-9 {
		t.Errorf("%v px across, short side %v'", px, cfg.FOVShort)
	}
}

// A scope without a camera model takes SUBS from -qe and -read-noise
// together, and one with an unpublished obstruction also from -obstruction
// (or -aperture, another telescope); neither defaults to the optimistic 0.
func TestScopeNoCameraModel(t *testing.T) {
	for _, c := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"-scope", "dwarf-2", "-qe", "0.8"}, false},
		{[]string{"-scope", "dwarf-2", "-read-noise", "1"}, false},
		{[]string{"-scope", "dwarf-2", "-qe", "0.8", "-read-noise", "1"}, true},
		{[]string{"-scope", "draco", "-qe", "0.8", "-read-noise", "1"}, false},
		{[]string{"-scope", "draco", "-qe", "0.8", "-read-noise", "1", "-obstruction", "40"}, true},
		{[]string{"-scope", "draco", "-qe", "0.8", "-read-noise", "1", "-aperture", "80"}, true},
		{[]string{"-scope", "draco"}, true},
		{[]string{"-scope", "seestar-s50", "-qe", "0.9"}, true}, // its chip's read noise
	} {
		if _, err := mutParse(t, mutBase(c.args...)...); (err == nil) != c.ok {
			t.Errorf("%v: %v, want ok %v", c.args, err, c.ok)
		}
	}
}

// A smart telescope's camera is colour: -mono with -scope is an
// error, and a mono camera is another telescope's, -camera's or -fov's.
func TestScopeMono(t *testing.T) {
	for _, args := range [][]string{{"-scope", "seestar-s50", "-mono"}, {"-scope", "origin", "-mono"}} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil || !strings.Contains(err.Error(), "camera is colour") {
			t.Errorf("%v: %v", args, err)
		}
	}
	m := Cameras["imx462"].Mono
	if cfg, err := mutParse(t, mutBase("-camera", "imx462", "-mono", "-focal", "250", "-aperture", "50")...); err != nil || cfg.QE != m.QE || cfg.CameraLabel() != "IMX462 mono" {
		t.Errorf("-camera imx462 -mono: QE %v %q (%v)", cfg.QE, cfg.CameraLabel(), err)
	}
	if cfg, err := mutParse(t, mutBase("-fov", "2x1", "-mono")...); err != nil || !cfg.Mono {
		t.Errorf("-fov -mono: %v", err)
	}
}

// A scope's built-in filter is -filter's default and gives the bands their
// widths; a given -filter-nm replaces the widths, a given -filter only the
// choice, and a scope without one, or no -scope, changes nothing.
func TestBuiltInFilter(t *testing.T) {
	for key, s := range Scopes {
		if s.Filter == "" {
			if s.Bands != [catalog.NumLines]float64{} {
				t.Errorf("%s: bands without a filter", key)
			}

			continue
		}
		for _, l := range Filters[s.Filter] {
			if w := s.Bands[l]; w < 1 || w > 30 {
				t.Errorf("%s: %s band %v nm, want 1-30", key, s.Filter, w)
			}
		}
	}
	seestar := [catalog.NumLines]float64{catalog.Ha: 20, catalog.OIII: 30}
	for _, tc := range []struct {
		args   []string
		filter string
		bands  [catalog.NumLines]float64
	}{
		{[]string{"-scope", "seestar-s50"}, "dual", seestar},
		{[]string{"-scope", "seestar-s50", "-filter-nm", "7"}, "dual", [catalog.NumLines]float64{}},
		{[]string{"-scope", "seestar-s50", "-filter", "ha,cls"}, "ha,cls", seestar},
		{[]string{"-scope", "origin"}, "", [catalog.NumLines]float64{}},
		{nil, "", [catalog.NumLines]float64{}},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		if err != nil || cfg.Filter != tc.filter || cfg.BandNM != tc.bands {
			t.Errorf("%v: filter %q bands %v, want %q %v (%v)", tc.args, cfg.Filter, cfg.BandNM, tc.filter, tc.bands, err)
		}
	}
	cfg := Config{FilterNM: 7, BandNM: seestar}
	if w := cfg.BandWidths(); w[catalog.Ha] != 20 || w[catalog.OIII] != 30 || w[catalog.SII] != 7 {
		t.Errorf("BandWidths %v", w)
	}
}
