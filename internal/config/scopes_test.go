// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"cmp"
	"math"
	"testing"
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
		"evscope":       {long: 37.0 / 60, short: 27.0 / 60, tol: 0.04}, // rounded to the minute
		"evscope-2":     {long: 45.6 / 60, short: 34.2 / 60},
		"odyssey":       {long: 45.0 / 60, short: 33.6 / 60},
		"orbis-60edx":   {}, // trade-show specifications state no field
	}
	if len(stated) != len(Scopes) {
		t.Errorf("%d stated fields for %d scopes", len(stated), len(Scopes))
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
// is published, the sub-exposure estimate's camera; -origin is -scope
// origin, byte for byte.
func TestScopeFlag(t *testing.T) {
	a, err := mutParse(t, mutBase("-origin")...)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mutParse(t, mutBase("-scope", "ORIGIN")...)
	if err != nil || a.FOVLong != b.FOVLong || a.Scale != b.Scale || a.Obstruction != b.Obstruction || a.Scope != b.Scope {
		t.Errorf("-origin %+v and -scope origin %+v differ (%v)", a, b, err)
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

// -mono behind a scope takes its chip's mono version where one is made, the
// scope's own read noise being its colour camera's; a chip made in colour
// only keeps the colour QE and read noise, labelled as such.
func TestScopeMono(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-scope", "seestar-s50", "-mono")...)
	m := Cameras["imx462"].Mono
	if err != nil || cfg.QE != m.QE || cfg.ReadNoise != m.ReadNoise || cfg.CameraLabel() != "IMX462 mono" {
		t.Errorf("seestar-s50 -mono: QE %v RN %v %q (%v), want %+v", cfg.QE, cfg.ReadNoise, cfg.CameraLabel(), err, m)
	}
	cfg, err = mutParse(t, mutBase("-scope", "origin", "-mono")...)
	if err != nil || cfg.QE != Cameras["imx678"].Colour.QE || cfg.ReadNoise != Scopes["origin"].ReadNoise || cfg.CameraLabel() != "IMX678 colour, as mono" {
		t.Errorf("origin -mono: QE %v RN %v %q (%v)", cfg.QE, cfg.ReadNoise, cfg.CameraLabel(), err)
	}
	if cfg, _ := mutParse(t, mutBase("-scope", "seestar-s50")...); cfg.QE != Cameras["imx462"].Colour.QE || cfg.CameraLabel() != "IMX462 colour" {
		t.Errorf("seestar-s50: QE %v %q", cfg.QE, cfg.CameraLabel())
	}
}
