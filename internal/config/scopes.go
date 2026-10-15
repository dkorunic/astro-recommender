// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Scope is a -scope preset, a smart telescope: its optics, the frames its
// main camera saves (pixel size after any binning) and its usual sub
// length, from the maker's pages (October 2026; a value from elsewhere says
// so). Camera is its chip's Cameras key, whose colour version gives the QE,
// read noise and response; "" when the chip has no preset (OmniVision's, the
// IMX347, the IMX415), and NoSubs when the central obstruction is
// unpublished: either way SUBS needs -qe and -read-noise (and -obstruction)
// given. ReadNoise, when set, is the scope's own figure over the preset's.
type Scope struct {
	Name, Sensor                 string // as shown; Sensor names a chip without a preset
	Camera                       string
	Focal, Aperture, Obstruction float64 // mm
	Pixel                        float64 // µm
	Sub, ReadNoise               float64 // s, e⁻
	PxLong, PxShort              int
	NoSubs                       bool
}

// originKey is -origin's -scope.
const originKey = "origin"

// Scopes are the -scope presets by lower-case key. -origin is "origin", the
// Mark II, which is also where an unset frame value comes from.
var Scopes = map[string]Scope{
	// Celestron's RASA 152 f/2.2: the Mark II (2025) with the IMX678 and a
	// 77 mm central obstruction (Celestron: 50.7%), 10 s subs; its read
	// noise a vendor sheet's for the same sensor at 8.5 dB gain, the middle
	// of its 0.6-3.5 e⁻ (the Origin's ISO 1000 has no published gain).
	"origin": {
		Name: "Celestron Origin Mark II", Camera: chipIMX678, Focal: 335, Aperture: 152, Obstruction: 77,
		Pixel: 2.0, PxLong: 3856, PxShort: 2180, Sub: 10, ReadNoise: 2.15,
	},
	// The original (2023, discontinued): the IMX178 behind a 63 mm
	// obstruction (41%).
	"origin-mk1": {
		Name: "Celestron Origin 2023", Camera: chipIMX178, Focal: 335, Aperture: 152, Obstruction: 63,
		Pixel: 2.4, PxLong: 3096, PxShort: 2080, Sub: 10,
	},
	// ZWO's refractors, saving portrait frames (here long x short); 10 s
	// subs in alt-az.
	"seestar-s50":     {Name: "ZWO Seestar S50", Camera: chipIMX462, Focal: 250, Aperture: 50, Pixel: 2.9, PxLong: 1920, PxShort: 1080, Sub: 10},
	"seestar-s30":     {Name: "ZWO Seestar S30", Camera: chipIMX662, Focal: 150, Aperture: 30, Pixel: 2.9, PxLong: 1920, PxShort: 1080, Sub: 10},
	"seestar-s30-pro": {Name: "ZWO Seestar S30 Pro", Camera: chipIMX585, Focal: 160, Aperture: 30, Pixel: 2.9, PxLong: 3840, PxShort: 2160, Sub: 10},
	"seestar-s50-pro": {Name: "ZWO Seestar S50 Pro", Sensor: "OmniVision OS08B10", Focal: 260, Aperture: 50, Pixel: 2.9, PxLong: 3840, PxShort: 2160, Sub: 10},
	// DwarfLab: 15 s, the auto mode's limit on the DWARF II and mini (the
	// DWARF 3's is unpublished). The DWARF II's IMX415 and focal length are
	// from reviews; its 1.45 µm the chip's, which matches their 3° field.
	// Draco saves a 2x2 binned 50 MP frame and has a central obstruction of
	// unpublished size.
	"dwarf-2":    {Name: "DwarfLab DWARF II", Sensor: "IMX415", Focal: 100, Aperture: 24, Pixel: 1.45, PxLong: 3840, PxShort: 2160, Sub: 15},
	"dwarf-3":    {Name: "DwarfLab DWARF 3", Camera: chipIMX678, Focal: 150, Aperture: 35, Pixel: 2.0, PxLong: 3840, PxShort: 2160, Sub: 15},
	"dwarf-mini": {Name: "DwarfLab DWARF mini", Camera: chipIMX662, Focal: 150, Aperture: 30, Pixel: 2.9, PxLong: 1920, PxShort: 1080, Sub: 15},
	"draco":      {Name: "DwarfLab Draco", Sensor: "OmniVision OV50Q40", Focal: 340, Aperture: 90, Pixel: 2.394, PxLong: 4096, PxShort: 3072, Sub: 15, NoSubs: true},
	// Vaonis' refractors, 10 s subs. Stellina's chip is named by third
	// parties only; its pixels are the maker's and the IMX178's.
	"stellina":    {Name: "Vaonis Stellina", Camera: chipIMX178, Focal: 400, Aperture: 80, Pixel: 2.4, PxLong: 3096, PxShort: 2080, Sub: 10},
	"vespera":     {Name: "Vaonis Vespera", Camera: chipIMX462, Focal: 200, Aperture: 50, Pixel: 2.9, PxLong: 1920, PxShort: 1080, Sub: 10},
	"vespera-2":   {Name: "Vaonis Vespera II", Camera: chipIMX585, Focal: 250, Aperture: 50, Pixel: 2.9, PxLong: 3840, PxShort: 2160, Sub: 10},
	"vespera-pro": {Name: "Vaonis Vespera Pro", Camera: chipIMX676, Focal: 250, Aperture: 50, Pixel: 2.0, PxLong: 3536, PxShort: 3536, Sub: 10},
	// Vaonis states 245 mm for the 2026 models but repeats the Vespera II's
	// field and 2.39"/px, which are 250 mm's; the stated focal length kept.
	"vespera-3":     {Name: "Vaonis Vespera 3", Camera: chipIMX585, Focal: 245, Aperture: 50, Pixel: 2.9, PxLong: 3840, PxShort: 2160, Sub: 10},
	"vespera-pro-2": {Name: "Vaonis Vespera Pro 2", Camera: chipIMX676, Focal: 245, Aperture: 50, Pixel: 2.0, PxLong: 3536, PxShort: 3536, Sub: 10},
	// Sky-Watcher's first, shown at Imaging World in October 2026 and not yet
	// on sale: a 60 mm f/5 ED refractor (a flip mirror sends the light to an
	// eyepiece instead), the IMX585 at 3840x2160, from the trade-show
	// specifications, "subject to change"; its sub length unstated, 10 s as
	// the other small refractors'.
	"orbis-60edx": {Name: "Sky-Watcher ORBIS 60EDX", Camera: chipIMX585, Focal: 300, Aperture: 60, Pixel: 2.9, PxLong: 3840, PxShort: 2160, Sub: 10},
	// Unistellar's 114 mm f/4 Newtonians, the sensor at prime focus behind
	// an obstruction of unpublished size; 4 s subs. The eVscope 2's frame
	// is the 2048x1536 crop that its stated 34.2'x45.6' field gives; the
	// Odyssey's chip is unnamed, its frame worked out from the stated
	// 33.6'x45' and 1.45 µm.
	"evscope":   {Name: "Unistellar eVscope / eQuinox", Camera: chipIMX224, Focal: 450, Aperture: 114, Pixel: 3.75, PxLong: 1304, PxShort: 976, Sub: 4, NoSubs: true},
	"evscope-2": {Name: "Unistellar eVscope 2 / eQuinox 2", Sensor: "IMX347", Focal: 450, Aperture: 114, Pixel: 2.9, PxLong: 2048, PxShort: 1536, Sub: 4, NoSubs: true},
	"odyssey":   {Name: "Unistellar Odyssey / Odyssey Pro", Sensor: "unnamed 1.45 µm", Focal: 320, Aperture: 85, Pixel: 1.45, PxLong: 2888, PxShort: 2156, Sub: 4, NoSubs: true},
}

// ScopeNames returns the -scope presets, sorted.
func ScopeNames() []string { return slices.Sorted(maps.Keys(Scopes)) }

// scopeChoices lists the presets for -scope's help as "key = Name", the
// Origin Mark II first, then by name, so a maker's models stand together,
// case aside and with "II" read as 2, so generations run in order (Vespera,
// II, 3); the web page shows the names in its select and sends the keys.
func scopeChoices() string {
	keys := ScopeNames()
	slices.SortStableFunc(keys, func(a, b string) int {
		// -origin's Mark II first: the scope the tool was built around.
		first := func(k string) int {
			if k == originKey {
				return 0
			}

			return 1
		}

		order := func(k string) string { return strings.ReplaceAll(strings.ToLower(Scopes[k].Name), " ii", " 2") }

		return cmp.Or(cmp.Compare(first(a), first(b)), strings.Compare(order(a), order(b)))
	})
	choices := make([]string, len(keys))
	for i, k := range keys {
		choices[i] = k + " = " + Scopes[k].Name
	}

	return strings.Join(choices, ", ")
}

// fov returns the scope's frame's long and short sides in arc minutes.
func (s Scope) fov() (float64, float64) {
	return fovSide(float64(s.PxLong)*s.Pixel/1000, s.Focal), fovSide(float64(s.PxShort)*s.Pixel/1000, s.Focal)
}

// scale returns the scope's pixel scale in arc seconds per pixel.
func (s Scope) scale() float64 { return pixelScale(s.Pixel, s.Focal) }

// variant returns the scope's camera's colour version, nil without a preset
// or with NoSubs.
func (s Scope) variant() *Variant {
	if c, ok := Cameras[s.Camera]; ok && !s.NoSubs {
		return c.Colour
	}

	return nil
}

// applyScope resolves -scope (or -origin, the same as -scope origin) and
// returns the scope whose values unset frame and camera flags take: it, or
// the Origin without one. A scope's camera is fixed: -camera is an error
// with it (-mono asks what a mono camera would do behind its optics).
func (cfg *Config) applyScope(set map[string]bool, origin bool, name string) (Scope, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if origin {
		if set["scope"] && key != originKey {
			return Scope{}, fmt.Errorf("%w: -origin is -scope origin, not -scope %s", errInvalidFlag, name)
		}
		key = originKey
	}
	if key == "" && !set["scope"] {
		return Scopes[originKey], nil
	}
	s, ok := Scopes[key]
	switch {
	case !ok:
		return Scope{}, fmt.Errorf("%w: -scope must be one of %s", errInvalidFlag, strings.Join(ScopeNames(), ", "))
	case set["camera"]:
		return Scope{}, fmt.Errorf("%w: -scope fixes its camera; -camera is for another telescope, with -focal", errInvalidFlag)
	}
	cfg.Scope, cfg.Camera = key, s.Camera

	return s, nil
}

// ScopeLabel names -scope's preset as the header and JSON show it, e.g.
// "ZWO Seestar S50 (IMX462)"; "" without one.
func (cfg *Config) ScopeLabel() string {
	s, ok := Scopes[cfg.Scope]
	if !ok {
		return ""
	}

	return s.Name + " (" + cmp.Or(s.Sensor, strings.ToUpper(s.Camera)) + ")"
}
