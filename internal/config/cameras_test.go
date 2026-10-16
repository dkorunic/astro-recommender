// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// Every preset is keyed by its chip's name, has at least one version, plausible
// pixels, QE and read noise and a gain to quote it at, and a colour version
// carries a response: each value 0-1, red leading at Hα and [S II], green at
// [O III], blue at Hβ against red, every dye passing some of every line.
func TestCameras(t *testing.T) {
	if len(Cameras) != 22 {
		t.Errorf("%d presets, want 22", len(Cameras))
	}
	for key, s := range Cameras {
		if key != strings.ToLower(key) || !strings.HasPrefix(key, "imx") && key != chipOS08B10 || s.Pixel <= 0 || s.Pixel > 20 || s.PxShort <= 0 || s.PxLong < s.PxShort {
			t.Errorf("%s: %+v", key, s)
		}
		if s.Colour == nil && s.Mono == nil {
			t.Errorf("%s: no version", key)
		}
		for kind, v := range map[string]*Variant{"colour": s.Colour, "mono": s.Mono} {
			if v == nil {
				continue
			}
			if v.QE <= 0.5 || v.QE > 1 || v.ReadNoise <= 0 || v.ReadNoise > 10 || v.Gain <= 0 {
				t.Errorf("%s %s: %+v", key, kind, v)
			}
			if (v.Response != nil) != (kind == "colour") {
				t.Errorf("%s %s: response %v", key, kind, v.Response)
			}
		}
		if s.Colour == nil {
			continue
		}
		r := s.Colour.Response
		for l := range r.Lines {
			for i, x := range r.Line(catalog.Line(l)) {
				if !(x > 0 && x <= 1) {
					t.Errorf("%s: line %d channel %d response %v", key, l, i, x)
				}
			}
		}
		ha, o3, s2, hb := r.Lines[catalog.Ha], r.Lines[catalog.OIII], r.Lines[catalog.SII], r.Lines[catalog.Hb]
		if ha[0] <= ha[1] || s2[0] <= s2[1] || o3[1] <= o3[2] || o3[1] <= o3[0] || hb[2] <= hb[0] {
			t.Errorf("%s: dyes out of order: Hα %v, [O III] %v, [S II] %v, Hβ %v", key, ha, o3, s2, hb)
		}
		for i, b := range r.Broad {
			if !(b > 0.2 && b < 0.7) {
				t.Errorf("%s: channel %d continuum %v", key, i, b)
			}
		}
	}
}

// -camera fills -sensor, -pixel, -qe and -read-noise from its chip, each
// given flag winning, and the colour response; -mono picks the mono version.
// It needs -focal, refuses -scope, an unknown chip and a version not made;
// without it the response is the IMX678's, the Origin's.
func TestCameraFlag(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-camera", " IMX571 ", "-focal", "400", "-aperture", "80")...)
	if err != nil {
		t.Fatal(err)
	}
	s := Cameras["imx571"]
	if cfg.Camera != "imx571" || cfg.QE != s.Colour.QE || cfg.ReadNoise != s.Colour.ReadNoise || cfg.ColourResponse() != s.Colour.Response || cfg.CameraLabel() != "IMX571 colour" {
		t.Errorf("colour: %q QE %v RN %v label %q", cfg.Camera, cfg.QE, cfg.ReadNoise, cfg.CameraLabel())
	}
	if want := pixelScale(s.Pixel, 400); math.Abs(cfg.Scale-want) > 1e-12 {
		t.Errorf("scale %v, want %v", cfg.Scale, want)
	}
	if want := fovSide(float64(s.PxShort)*s.Pixel/1000, 400); math.Abs(cfg.FOVShort-want) > 1e-9 {
		t.Errorf("short side %v', want %v'", cfg.FOVShort, want)
	}
	cfg, err = mutParse(t, mutBase("-camera", "imx571", "-mono", "-focal", "400", "-aperture", "80", "-read-noise", "3", "-sensor", "10x10")...)
	if err != nil || cfg.QE != s.Mono.QE || cfg.ReadNoise != 3 || cfg.FOVShort != fovSide(10, 400) || cfg.CameraLabel() != "IMX571 mono" {
		t.Errorf("mono with -read-noise and -sensor: QE %v RN %v short %v (%v)", cfg.QE, cfg.ReadNoise, cfg.FOVShort, err)
	}
	for _, args := range [][]string{
		{"-camera", "imx571"},
		{"-camera", "imx571", "-scope", "origin", "-focal", "400"},
		{"-camera", "imx999", "-focal", "400"},
		{"-camera", "imx294", "-mono", "-focal", "400"},
		{"-camera", "imx492", "-focal", "400"},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if cfg, _ := mutParse(t, mutBase("-scope", "origin")...); cfg.ColourResponse() != &imx678 || cfg.CameraLabel() != "IMX678 colour" || cfg.ScopeLabel() != "Celestron Origin Mark II (IMX678)" {
		t.Errorf("the Origin: camera %q, scope %q", cfg.CameraLabel(), cfg.ScopeLabel())
	}
	if cfg, _ := mutParse(t, mutBase("-fov", "2x1")...); cfg.ColourResponse() != &imx678 || cfg.CameraLabel() != "" || cfg.ScopeLabel() != "" {
		t.Error("a camera given by -fov alone is not the IMX678's")
	}
}

// typicalDark is the median of the charted chips' dark currents, each die
// once (the IMX492 is the IMX294's).
func TestTypicalDark(t *testing.T) {
	var darks []float64
	for key, s := range Cameras {
		if s.Dark > 0 && key != "imx492" {
			darks = append(darks, s.Dark)
		}
	}
	slices.Sort(darks)
	n := len(darks)
	if median := (darks[(n-1)/2] + darks[n/2]) / 2; math.Abs(median-typicalDark) > 0.005 {
		t.Errorf("median of %v = %v, typicalDark %v", darks, median, typicalDark)
	}
}

// The throughput and dark current defaults: the scope's design and chip, a
// typical chip's without a chart, another telescope's with -aperture, the
// flags' own when given.
func TestCameraDefaults(t *testing.T) {
	for _, tc := range []struct {
		args             []string
		throughput, dark float64
	}{
		{[]string{"-scope", "origin"}, throughputMirror, typicalDark},
		{[]string{"-scope", "seestar-s30-pro"}, throughputLens, 0.25},
		{[]string{"-scope", "origin", "-aperture", "200"}, throughputOther, typicalDark},
		{[]string{"-camera", "imx571", "-focal", "400", "-aperture", "80"}, throughputOther, 0.065},
		{[]string{"-scope", "origin", "-throughput", "0.7", "-dark", "0"}, 0.7, 0},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if cfg.Throughput != tc.throughput || cfg.Dark != tc.dark || cfg.SensorTempSet || cfg.Reject != 0.15 {
			t.Errorf("%v: throughput %v, dark %v, sensor-temp set %v, reject %v", tc.args, cfg.Throughput, cfg.Dark, cfg.SensorTempSet, cfg.Reject)
		}
	}
	for _, args := range [][]string{
		{"-scope", "origin", "-throughput", "0"},
		{"-scope", "origin", "-throughput", "1.1"},
		{"-scope", "origin", "-dark", "-1"},
		{"-scope", "origin", "-dark", "60"},
		{"-scope", "origin", "-sensor-temp", "-80"},
		{"-scope", "origin", "-reject", "0.95"},
		{"-reject", "0.1"},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
	if cfg, err := mutParse(t, mutBase("-scope", "origin", "-sensor-temp", "-10")...); err != nil || !cfg.SensorTempSet || cfg.SensorTemp != -10 {
		t.Errorf("-sensor-temp -10: %+v, %v", cfg.SensorTemp, err)
	}
}

// -stack defaults to mean with -scope (smart telescopes' live stacks) and
// to weighted otherwise; -residual is bounded.
func TestStackDefaults(t *testing.T) {
	for _, tc := range []struct {
		args []string
		mean bool
	}{
		{[]string{"-scope", "seestar-s50"}, true},
		{[]string{"-fov", "2x1.5", "-scale", "2"}, false},
		{[]string{"-scope", "origin", "-stack", "weighted"}, false},
		{[]string{"-camera", "imx571", "-focal", "400", "-aperture", "80", "-stack", "mean"}, true},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		if err != nil || cfg.MeanStack != tc.mean || cfg.Residual != 0.002 {
			t.Errorf("%v: mean %v, residual %v, %v", tc.args, cfg.MeanStack, cfg.Residual, err)
		}
	}
	for _, args := range [][]string{
		{"-scope", "origin", "-stack", "median"},
		{"-scope", "origin", "-residual", "0.1"},
		{"-residual", "0.01"},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}
