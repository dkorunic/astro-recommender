// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// Every preset is keyed by its chip's name, has at least one version, plausible
// pixels, QE and read noise and a gain to quote it at, and a colour version
// carries a response: each value 0-1, red leading at Hα and [S II], green at
// [O III], blue at Hβ against red, every dye passing some of every line.
func TestCameras(t *testing.T) {
	if len(Cameras) != 45 {
		t.Errorf("%d presets, want 45", len(Cameras))
	}
	for key, s := range Cameras {
		if key != strings.ToLower(key) || s.Pixel <= 0 || s.Pixel > 20 || s.PxShort <= 0 || s.PxLong < s.PxShort {
			t.Errorf("%s: %+v", key, s)
		}
		if s.Colour == nil && s.Mono == nil {
			t.Errorf("%s: no version", key)
		}
		for kind, v := range map[string]*Variant{"colour": s.Colour, "mono": s.Mono} {
			if v == nil {
				continue
			}
			if v.QE <= 0.3 || v.QE > 1 || v.ReadNoise <= 0 || v.ReadNoise > 10 || v.Gain < 0 {
				t.Errorf("%s %s: %+v", key, kind, v)
			}
			if (v.Response != nil) != (kind == "colour") || v.MonoResponse != nil && kind != "mono" {
				t.Errorf("%s %s: response %v, mono response %v", key, kind, v.Response, v.MonoResponse)
			}
			if m := v.MonoResponse; m != nil {
				for l := range m.Lines {
					if x := m.Line(catalog.Line(l)); !(x > 0 && x <= 1) {
						t.Errorf("%s: line %d mono response %v", key, l, x)
					}
				}
				if !(m.Broad > 0.5 && m.Broad <= 1) {
					t.Errorf("%s: mono continuum %v", key, m.Broad)
				}
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
	if cfg, _ := mutParse(t, mutBase("-camera", "icx694", "-mono", "-focal", "400", "-aperture", "80")...); cfg.MonoResponse() != &icx694Mono || cfg.CameraLabel() != "ICX694 mono" {
		t.Errorf("a charted mono preset: response %v, label %q", cfg.MonoResponse(), cfg.CameraLabel())
	}
	if cfg, _ := mutParse(t, mutBase("-camera", "imx678", "-mono", "-focal", "400", "-aperture", "80")...); cfg.MonoResponse() != &sonyMono {
		t.Error("an uncharted mono preset is not sonyMono")
	}
	if cfg, _ := mutParse(t, mutBase("-fov", "2x1")...); cfg.ColourResponse() != &imx678 || cfg.CameraLabel() != "" || cfg.ScopeLabel() != "" {
		t.Error("a camera given by -fov alone is not the IMX678's")
	}
}

// typicalDark is the median of the charted CMOS chips' dark currents, each
// die once (the IMX492 is the IMX294's), the CCDs' left out.
func TestTypicalDark(t *testing.T) {
	var darks []float64
	for key, s := range Cameras {
		if s.Dark > 0 && key != "imx492" && !strings.HasPrefix(key, "kaf") && !strings.HasPrefix(key, "icx") {
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
		{[]string{"-focal", "400", "-pixel", "3.76", "-sensor", "23.5x15.6", "-aperture", "80", "-qe", "0.8", "-read-noise", "1.5", "-mono"}, throughputOther, typicalDark},
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

// -mount defaults to -scope's mount (alt-az, Draco's and the ORBIS'
// derotated) and to eq otherwise, and like the camera flags needs framing.
func TestMountDefaults(t *testing.T) {
	for _, tc := range []struct {
		mount string
		args  []string
	}{
		{MountAltAz, []string{"-scope", "seestar-s50"}},
		{MountEq, []string{"-scope", "seestar-s50", "-mount", "eq"}},
		{MountDerotated, []string{"-scope", "draco"}},
		{MountAltAz, []string{"-scope", "draco", "-mount", "altaz"}},
		{MountEq, []string{"-fov", "2x1.5", "-scale", "2"}},
		{MountAltAz, []string{"-fov", "2x1.5", "-scale", "2", "-mount", "altaz", "-sub", "30"}},
		{MountDerotated, []string{"-fov", "2x1.5", "-scale", "2", "-mount", "derotated"}},
		{MountEq, nil},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		if err != nil || cfg.Mount != tc.mount || cfg.AltAz != (tc.mount == MountAltAz) {
			t.Errorf("%v: mount %q (alt-az %v), want %q, %v", tc.args, cfg.Mount, cfg.AltAz, tc.mount, err)
		}
	}
	// Without -scope, altaz needs -sub: the Origin's 10 s says nothing of
	// another telescope's.
	for _, args := range [][]string{{"-scope", "origin", "-mount", "dobson"}, {"-mount", "altaz"}, {"-fov", "2x1.5", "-scale", "2", "-mount", "altaz"}} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

// ApertureSet is the telescope's own aperture, -scope's or -aperture's, not
// the Origin's stand-in that framing without either borrows.
func TestApertureSet(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"-scope", "seestar-s50"}, true},
		{[]string{"-fov", "2x1.5", "-scale", "2", "-aperture", "80"}, true},
		{[]string{"-fov", "2x1.5", "-scale", "2"}, false},
		{nil, false},
	} {
		if cfg, err := mutParse(t, mutBase(tc.args...)...); err != nil || cfg.ApertureSet != tc.want {
			t.Errorf("%v: ApertureSet %v, want %v (%v)", tc.args, cfg.ApertureSet, tc.want, err)
		}
	}
}

// On -scope, -focal puts its camera behind other optics, and a -pixel
// unlike its own another camera: SUBS needs what the scope's would
// otherwise stand for, and the aperture is no longer the telescope's own.
func TestScopeOtherCamera(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		needs       string
		apertureSet bool
	}{
		{[]string{"-scope", "origin", "-focal", "600", "-pixel", "2"}, "-aperture", false},
		{[]string{"-scope", "origin", "-focal", "600", "-sensor", "5x3", "-aperture", "100"}, "", true},
		{[]string{"-scope", "origin", "-focal", "600", "-pixel", "3.76", "-aperture", "100"}, "-qe and -read-noise", true},
		{[]string{"-scope", "origin", "-focal", "600", "-pixel", "3.76"}, "-aperture and -qe and -read-noise", false},
		{[]string{"-scope", "origin", "-focal", "600", "-pixel", "3.76", "-aperture", "100", "-qe", "0.8", "-read-noise", "1.5"}, "", true},
		{[]string{"-scope", "origin", "-fov", "1x0.5"}, "", true},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		if err != nil || cfg.SubsNeeds != tc.needs || cfg.ApertureSet != tc.apertureSet || (cfg.QE == 0) != (tc.needs != "") {
			t.Errorf("%v: SubsNeeds %q ApertureSet %v QE %v, want %q %v (%v)", tc.args, cfg.SubsNeeds, cfg.ApertureSet, cfg.QE, tc.needs, tc.apertureSet, err)
		}
	}
	// The header names what is left of the scope.
	for args, want := range map[string]string{
		"-focal 600 -pixel 2":    "Celestron Origin Mark II's IMX678 behind other optics",
		"-focal 600 -pixel 3.76": "Celestron Origin Mark II settings with another camera and optics",
		"-fov 1x0.5":             "Celestron Origin Mark II (IMX678)",
	} {
		cfg, err := mutParse(t, mutBase(append([]string{"-scope", "origin"}, strings.Fields(args)...)...)...)
		if err != nil || cfg.ScopeLabel() != want {
			t.Errorf("%s: label %q, want %q (%v)", args, cfg.ScopeLabel(), want, err)
		}
	}
}

// Another camera on -scope is not the scope's chip: its dark current,
// response and label are not the chip's (the original Origin's IMX178 has
// 0.95 e⁻/s, six times typicalDark).
func TestScopeOtherChipCamera(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-scope", "origin-mk1", "-focal", "500", "-pixel", "3.76", "-aperture", "100", "-qe", "0.8", "-read-noise", "1")...)
	if err != nil || !cfg.OtherChip {
		t.Fatalf("OtherChip %v (%v)", cfg.OtherChip, err)
	}
	if cfg.Camera != "" || cfg.CameraLabel() != "" || cfg.Dark != typicalDark {
		t.Errorf("camera %q label %q dark %g, want none and %g", cfg.Camera, cfg.CameraLabel(), cfg.Dark, typicalDark)
	}
	if got, want := cfg.ColourResponse(), &imx678; got != want {
		t.Errorf("response %+v, want the default IMX678's %+v", got, want)
	}
}

// A -scale unlike -scope's own, or a -fov wider than its frame, is another
// rig's: SUBS needs its aperture, QE and read noise, and the label and
// camera are no longer the scope's. Its own scale to two decimals, or a crop
// of its field, is still the scope.
func TestScopeOtherRig(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		other bool
	}{
		{[]string{"-scale", "0.5"}, true},
		{[]string{"-fov", "2x1"}, true},
		{[]string{"-scale", "0.5", "-aperture", "100", "-qe", "0.8", "-read-noise", "1"}, true},
		{[]string{"-scale", "2.39"}, false},
		{[]string{"-fov", "1x0.5"}, false},
		{[]string{"-fov", "1.27x0.72"}, false},
	} {
		cfg, err := mutParse(t, mutBase(append([]string{"-scope", "seestar-s50"}, tc.args...)...)...)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if cfg.OtherOptics != tc.other || cfg.OtherChip != tc.other || (cfg.Camera == "") != tc.other {
			t.Errorf("%v: OtherOptics %v OtherChip %v camera %q, want other %v", tc.args, cfg.OtherOptics, cfg.OtherChip, cfg.Camera, tc.other)
		}
		if tc.other && len(tc.args) == 2 && (cfg.QE != 0 || cfg.SubsNeeds != "-aperture and -qe and -read-noise") {
			t.Errorf("%v: QE %g, SubsNeeds %q; want none until given", tc.args, cfg.QE, cfg.SubsNeeds)
		}
		if want := "ZWO Seestar S50 settings with another camera and optics"; tc.other && cfg.ScopeLabel() != want {
			t.Errorf("%v: label %q, want %q", tc.args, cfg.ScopeLabel(), want)
		}
	}
}

// -qe's and -read-noise's help give the values the Origin actually takes.
func TestOriginHelpValues(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-scope", "origin")...)
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]float64{"qe": cfg.QE, "read-noise": cfg.ReadNoise} {
		if u := flag.CommandLine.Lookup(name).Usage; !strings.HasSuffix(u, " "+strconv.FormatFloat(v, 'g', -1, 64)) {
			t.Errorf("-%s help %q does not end in the Origin's %g", name, u, v)
		}
	}
}

// Another camera and optics on a scope are not behind its built-in filter,
// and its own camera stays colour: -mono only describes another camera.
func TestScopeOtherChipFilterMono(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-scope", "seestar-s50", "-focal", "500", "-pixel", "3.76", "-aperture", "100", "-qe", "0.8", "-read-noise", "1")...)
	if err != nil || cfg.Filter != "" || cfg.BuiltInBands() {
		t.Errorf("another rig: filter %q built-in %v, want none (%v)", cfg.Filter, cfg.BuiltInBands(), err)
	}
	if cfg, err := mutParse(t, mutBase("-scope", "seestar-s50", "-focal", "500", "-sensor", "5x3")...); err != nil || cfg.Filter != filterDual {
		t.Errorf("its camera behind other optics: filter %q, want the built-in %s (%v)", cfg.Filter, filterDual, err)
	}
	if _, err := mutParse(t, mutBase("-scope", "seestar-s50", "-mono")...); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Errorf("-mono on the scope's own camera: %v, want the colour error", err)
	}
	cfg, err = mutParse(t, mutBase("-scope", "seestar-s50", "-scale", "0.5", "-aperture", "100", "-qe", "0.8", "-read-noise", "1", "-mono")...)
	if err != nil || !cfg.Mono || cfg.MonoResponse() != &sonyMono || cfg.QE != 0.8 {
		t.Errorf("-mono on another camera: mono %v QE %g (%v)", cfg.Mono, cfg.QE, err)
	}
}

// -filter-std takes the beam from the telescope the frame describes: the
// scope's, or -focal and -aperture; another telescope's frame without them
// (-fov alone, another rig on a scope) would borrow the Origin's or the
// scope's f-ratio.
func TestFilterStdBeam(t *testing.T) {
	for _, tc := range []struct {
		args []string
		f    float64 // 0: an error
	}{
		{[]string{"-filter", "ha", "-filter-std"}, 335.0 / 152},
		{[]string{"-scope", "seestar-s50", "-filter", "ha", "-filter-std"}, 5},
		{[]string{"-fov", "2x1", "-filter", "ha", "-filter-std"}, 0},
		{[]string{"-scope", "seestar-s50", "-scale", "0.5", "-filter", "ha", "-filter-std"}, 0},
		{[]string{"-fov", "2x1", "-focal", "400", "-aperture", "80", "-filter", "ha", "-filter-std"}, 0}, // -fov with -focal: frameFOV's error
		{[]string{"-focal", "400", "-pixel", "3.76", "-aperture", "80", "-filter", "ha", "-filter-std"}, 5},
	} {
		cfg, err := mutParse(t, mutBase(tc.args...)...)
		switch {
		case tc.f == 0 && err == nil:
			t.Errorf("%v: f/%g accepted, want an error", tc.args, cfg.FRatio)
		case tc.f != 0 && (err != nil || math.Abs(cfg.FRatio-tc.f) > 0.01):
			t.Errorf("%v: f/%g (%v), want f/%g", tc.args, cfg.FRatio, err, tc.f)
		}
	}
}
