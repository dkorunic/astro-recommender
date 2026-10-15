// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dkorunic/astro-recommender/internal/catalog"
)

// Response is a colour sensor's spectral response for the sub-exposure
// estimate, relative to its peak QE: each channel's (red, green, blue) at
// each catalog.Line, and each channel's mean over atmos.CameraLo-CameraHi,
// the continuum's. The dyes overlap, so every channel sees some of every
// line.
type Response struct {
	Lines [catalog.NumLines][3]float64
	Broad [3]float64
}

// Variant is a sensor's colour or mono camera: its peak QE and its read
// noise at Gain, the vendor's deep-sky setting in its own units (ZWO's and
// Player One's 0.1 dB), where the high conversion gain mode switches on;
// Response for a colour one.
type Variant struct {
	Response      *Response
	QE, ReadNoise float64
	Gain          int
}

// Sensor is a -camera preset: the chip's pixels and its colour and mono
// cameras, nil when no astro camera ships that version.
type Sensor struct {
	Colour, Mono    *Variant
	Pixel           float64 // µm
	PxLong, PxShort int
}

// The colour responses, each digitized by scripts/qe_digitize.py from the
// vendor's chart (gridlines found, curves told apart by hue, relative to the
// chart's peak; [O III] with 495.9 nm at a third of 500.7's flux, [S II] the
// doublet's mean), each channel's mean over 400-700 nm the continuum's.
// Several vendors share one chart across chips; where two of a chip's charts
// disagree, the comment says which was taken.
var (
	// ZWO ASI678MC manual V1.0, §3.3.
	imx678 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.774, 0.341, 0.107},
			catalog.OIII:    {0.047, 0.974, 0.527},
			catalog.SII:     {0.715, 0.387, 0.120},
			catalog.Hb:      {0.038, 0.939, 0.794},
			catalog.NII6548: {0.782, 0.337, 0.107},
			catalog.NII6583: {0.766, 0.344, 0.108},
		},
		Broad: [3]float64{0.397, 0.565, 0.394},
	}
	// ZWO ASI2600MC Pro manual (2026), p8.
	imx571 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.804, 0.151, 0.049},
			catalog.OIII:    {0.032, 0.931, 0.505},
			catalog.SII:     {0.748, 0.178, 0.066},
			catalog.Hb:      {0.027, 0.735, 0.680},
			catalog.NII6548: {0.810, 0.152, 0.047},
			catalog.NII6583: {0.801, 0.151, 0.051},
		},
		Broad: [3]float64{0.349, 0.459, 0.292},
	}
	// ZWO ASI533MC Pro manual (2026), p8.
	imx533 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.808, 0.152, 0.047},
			catalog.OIII:    {0.031, 0.900, 0.553},
			catalog.SII:     {0.752, 0.172, 0.066},
			catalog.Hb:      {0.029, 0.665, 0.714},
			catalog.NII6548: {0.812, 0.154, 0.044},
			catalog.NII6583: {0.803, 0.150, 0.050},
		},
		Broad: [3]float64{0.342, 0.456, 0.294},
	}
	// ZWO ASI585MC Pro manual (2026), p9; the ASI585MC's CN manual V1.0 has the IMX662's curve instead, green at Hα 0.37.
	imx585 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.894, 0.160, 0.058},
			catalog.OIII:    {0.031, 0.944, 0.463},
			catalog.SII:     {0.832, 0.203, 0.075},
			catalog.Hb:      {0.027, 0.763, 0.650},
			catalog.NII6548: {0.896, 0.158, 0.056},
			catalog.NII6583: {0.890, 0.162, 0.060},
		},
		Broad: [3]float64{0.382, 0.461, 0.310},
	}
	// the IMX662's and IMX664's, one chart in ZWO's ASI662MC and ASI664MC manuals (p8, the absolute QE).
	starvis2 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.920, 0.371, 0.050},
			catalog.OIII:    {0.046, 0.976, 0.488},
			catalog.SII:     {0.871, 0.364, 0.067},
			catalog.Hb:      {0.032, 0.919, 0.683},
			catalog.NII6548: {0.925, 0.373, 0.047},
			catalog.NII6583: {0.915, 0.370, 0.050},
		},
		Broad: [3]float64{0.425, 0.540, 0.315},
	}
	// ZWO ASI676MC manual (2024), p8.
	imx676 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.934, 0.384, 0.135},
			catalog.OIII:    {0.043, 0.848, 0.453},
			catalog.SII:     {0.877, 0.421, 0.167},
			catalog.Hb:      {0.034, 0.718, 0.583},
			catalog.NII6548: {0.938, 0.384, 0.133},
			catalog.NII6583: {0.930, 0.383, 0.139},
		},
		Broad: [3]float64{0.409, 0.547, 0.317},
	}
	// ZWO ASI485MC manual V1.0, p6, the ASI482MC manual's chart too, so the IMX482's.
	imx485 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.808, 0.153, 0.044},
			catalog.OIII:    {0.038, 0.944, 0.561},
			catalog.SII:     {0.745, 0.195, 0.062},
			catalog.Hb:      {0.022, 0.670, 0.749},
			catalog.NII6548: {0.817, 0.153, 0.044},
			catalog.NII6583: {0.799, 0.153, 0.044},
		},
		Broad: [3]float64{0.380, 0.485, 0.342},
	}
	// Player One's Neptune-C II chart, near the IMX462's (ZWO ASI462MC manual V1.1).
	imx464 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.826, 0.253, 0.124},
			catalog.OIII:    {0.041, 0.764, 0.440},
			catalog.SII:     {0.772, 0.286, 0.145},
			catalog.Hb:      {0.032, 0.598, 0.562},
			catalog.NII6548: {0.830, 0.252, 0.120},
			catalog.NII6583: {0.818, 0.253, 0.126},
		},
		Broad: [3]float64{0.365, 0.438, 0.292},
	}
	// ZWO ASI178 manual V1.3, p7 (relative, 400-700 nm).
	imx178 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.770, 0.152, 0.054},
			catalog.OIII:    {0.039, 0.923, 0.486},
			catalog.SII:     {0.691, 0.179, 0.068},
			catalog.Hb:      {0.032, 0.740, 0.650},
			catalog.NII6548: {0.779, 0.152, 0.053},
			catalog.NII6583: {0.759, 0.153, 0.057},
		},
		Broad: [3]float64{0.356, 0.452, 0.288},
	}
	// ZWO ASI462 manual V1.1, p6 (relative, peaking at 810 nm).
	imx462 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.814, 0.249, 0.124},
			catalog.OIII:    {0.041, 0.758, 0.429},
			catalog.SII:     {0.787, 0.296, 0.150},
			catalog.Hb:      {0.032, 0.601, 0.553},
			catalog.NII6548: {0.817, 0.248, 0.122},
			catalog.NII6583: {0.808, 0.252, 0.128},
		},
		Broad: [3]float64{0.375, 0.428, 0.293},
	}
	// ZWO ASI224MC manual V1.4, p7.
	imx224 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.949, 0.191, 0.083},
			catalog.OIII:    {0.053, 0.834, 0.458},
			catalog.SII:     {0.864, 0.229, 0.100},
			catalog.Hb:      {0.046, 0.666, 0.588},
			catalog.NII6548: {0.953, 0.189, 0.081},
			catalog.NII6583: {0.941, 0.192, 0.086},
		},
		Broad: [3]float64{0.416, 0.454, 0.267},
	}
	// ZWO ASI294MC Pro manual (2026), p8.
	imx294 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.888, 0.168, 0.052},
			catalog.OIII:    {0.033, 0.908, 0.497},
			catalog.SII:     {0.810, 0.190, 0.067},
			catalog.Hb:      {0.027, 0.700, 0.672},
			catalog.NII6548: {0.894, 0.168, 0.051},
			catalog.NII6583: {0.880, 0.168, 0.055},
		},
		Broad: [3]float64{0.383, 0.466, 0.288},
	}
	// ZWO ASI2400MC Pro manual V1.2, p6.
	imx410 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.826, 0.109, 0.040},
			catalog.OIII:    {0.051, 0.596, 0.493},
			catalog.SII:     {0.743, 0.138, 0.052},
			catalog.Hb:      {0.048, 0.456, 0.658},
			catalog.NII6548: {0.832, 0.108, 0.039},
			catalog.NII6583: {0.819, 0.111, 0.042},
		},
		Broad: [3]float64{0.372, 0.404, 0.275},
	}
	// Player One's Zeus 455C chart (ZWO's ASI6200 manuals have none).
	imx455 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.800, 0.070, 0.020},
			catalog.OIII:    {0.016, 0.835, 0.499},
			catalog.SII:     {0.722, 0.092, 0.028},
			catalog.Hb:      {0.012, 0.650, 0.657},
			catalog.NII6548: {0.805, 0.071, 0.019},
			catalog.NII6583: {0.791, 0.070, 0.021},
		},
		Broad: [3]float64{0.337, 0.405, 0.264},
	}
)

// The chips the -scope presets share, as Cameras keys.
const (
	chipIMX178 = "imx178"
	chipIMX224 = "imx224"
	chipIMX462 = "imx462"
	chipIMX585 = "imx585"
	chipIMX662 = "imx662"
	chipIMX676 = "imx676"
	chipIMX678 = "imx678"
)

// Cameras are the -camera presets by lower-case chip name, from the camera
// makers' manuals and product pages (ZWO's, else Player One's, October
// 2026): the pixels of the camera's output, its peak QE (the vendor's, often
// an estimate) and its read noise at the vendor's deep-sky gain, where the
// high conversion gain mode switches on (ZWO's gain in 0.1 dB, read off its
// chart to about ±0.2 e⁻). A version is left out where no astro camera
// ships it or its peak QE is unpublished (the IMX183's and IMX174's colour
// ones); a mono version takes the mono response (lineQE in scoring). The
// IMX678 preset is ZWO's at gain 182; the Origin keeps its own read noise
// (Scopes' ReadNoise).
var Cameras = map[string]Sensor{
	"imx571": {
		Pixel: 3.76, PxLong: 6248, PxShort: 4176,
		Colour: &Variant{Response: &imx571, Gain: 100, QE: 0.80, ReadNoise: 1.4},
		Mono:   &Variant{Gain: 100, QE: 0.91, ReadNoise: 1.4},
	},
	"imx533": {
		Pixel: 3.76, PxLong: 3008, PxShort: 3008,
		Colour: &Variant{Response: &imx533, Gain: 100, QE: 0.80, ReadNoise: 1.5},
		Mono:   &Variant{Gain: 100, QE: 0.91, ReadNoise: 1.5},
	},
	chipIMX585: {
		Pixel: 2.9, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &imx585, Gain: 200, QE: 0.91, ReadNoise: 1.0},
		Mono:   &Variant{Gain: 200, QE: 0.91, ReadNoise: 1.0},
	},
	"imx455": {
		Pixel: 3.76, PxLong: 9576, PxShort: 6388,
		Colour: &Variant{Response: &imx455, Gain: 100, QE: 0.80, ReadNoise: 1.5},
		Mono:   &Variant{Gain: 100, QE: 0.91, ReadNoise: 1.5},
	},
	"imx294": {
		Pixel: 4.63, PxLong: 4144, PxShort: 2822,
		Colour: &Variant{Response: &imx294, Gain: 120, QE: 0.75, ReadNoise: 1.7},
	},
	// The ASI294MM's chip, at its default 2x2 binning.
	"imx492": {
		Pixel: 4.63, PxLong: 4144, PxShort: 2822,
		Mono: &Variant{Gain: 120, QE: 0.90, ReadNoise: 1.8},
	},
	// No high conversion gain mode: unity gain.
	"imx183": {
		Pixel: 2.4, PxLong: 5496, PxShort: 3672,
		Mono: &Variant{Gain: 120, QE: 0.84, ReadNoise: 2.1},
	},
	chipIMX678: {
		Pixel: 2.0, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &imx678, Gain: 182, QE: 0.83, ReadNoise: 0.9},
	},
	chipIMX662: {
		Pixel: 2.9, PxLong: 1920, PxShort: 1080,
		Colour: &Variant{Response: &starvis2, Gain: 200, QE: 0.91, ReadNoise: 1.2},
		Mono:   &Variant{Gain: 200, QE: 0.91, ReadNoise: 1.2},
	},
	"imx664": {
		Pixel: 2.9, PxLong: 2704, PxShort: 1536,
		Colour: &Variant{Response: &starvis2, Gain: 252, QE: 0.91, ReadNoise: 0.8},
	},
	chipIMX676: {
		Pixel: 2.0, PxLong: 3552, PxShort: 3552,
		Colour: &Variant{Response: &imx676, Gain: 180, QE: 0.83, ReadNoise: 0.75},
		Mono:   &Variant{Gain: 180, QE: 0.83, ReadNoise: 0.75},
	},
	"imx410": {
		Pixel: 5.94, PxLong: 6072, PxShort: 4042,
		Colour: &Variant{Response: &imx410, Gain: 140, QE: 0.80, ReadNoise: 1.5},
	},
	"imx461": {
		Pixel: 3.76, PxLong: 11656, PxShort: 8750,
		Mono: &Variant{Gain: 100, QE: 0.91, ReadNoise: 1.45},
	},
	"imx432": {
		Pixel: 9.0, PxLong: 1608, PxShort: 1104,
		Mono: &Variant{Gain: 140, QE: 0.79, ReadNoise: 4.3},
	},
	// No high conversion gain mode: ZWO's figure at gain 300.
	"imx174": {
		Pixel: 5.86, PxLong: 1936, PxShort: 1216,
		Mono: &Variant{Gain: 300, QE: 0.77, ReadNoise: 3.5},
	},
	// The colour peak QE from ZWO's ASI462MC/ASI662MC comparison ("about
	// 81%"); the read noise its chart's at gain 80, for both versions.
	chipIMX462: {
		Pixel: 2.9, PxLong: 1936, PxShort: 1096,
		Colour: &Variant{Response: &imx462, Gain: 80, QE: 0.81, ReadNoise: 1.05},
		Mono:   &Variant{Gain: 80, QE: 0.89, ReadNoise: 1.05},
	},
	// The original Origin's and Stellina's: ZWO publishes no peak QE (its
	// ASI178 manual says TBD); 0.70 is the colour figure rk.edu.pl quotes
	// from Sony's datasheet, second-hand. The read noise ZWO's chart's at
	// gain 100.
	chipIMX178: {
		Pixel: 2.4, PxLong: 3096, PxShort: 2080,
		Colour: &Variant{Response: &imx178, Gain: 100, QE: 0.70, ReadNoise: 1.7},
	},
	"imx485": {
		Pixel: 2.9, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &imx485, Gain: 80, QE: 0.85, ReadNoise: 2.3},
	},
	"imx482": {
		Pixel: 5.8, PxLong: 1920, PxShort: 1080,
		Colour: &Variant{Response: &imx485, Gain: 80, QE: 0.85, ReadNoise: 4.5},
	},
	chipIMX224: {
		Pixel: 3.75, PxLong: 1304, PxShort: 976,
		Colour: &Variant{Response: &imx224, Gain: 60, QE: 0.78, ReadNoise: 1.6},
	},
	// Player One's Neptune-C II, gain 83 its HCG step.
	"imx464": {
		Pixel: 2.9, PxLong: 2712, PxShort: 1538,
		Colour: &Variant{Response: &imx464, Gain: 83, QE: 0.90, ReadNoise: 1.2},
	},
}

// CameraNames returns the -camera presets, sorted.
func CameraNames() []string { return slices.Sorted(maps.Keys(Cameras)) }

// CameraLabel names -camera's preset as the header and JSON show it, e.g.
// "IMX571 colour"; "" without one.
func (cfg *Config) CameraLabel() string {
	if _, ok := Cameras[cfg.Camera]; !ok {
		return ""
	}
	if cfg.Mono {
		return strings.ToUpper(cfg.Camera) + " mono"
	}

	return strings.ToUpper(cfg.Camera) + " colour"
}

// ColourResponse returns the colour response the sub-exposure estimate uses:
// -camera's, else the IMX678's (the Origin's, and the default for a colour
// camera described by -sensor/-pixel alone).
func (cfg *Config) ColourResponse() *Response {
	if s, ok := Cameras[cfg.Camera]; ok && s.Colour != nil {
		return s.Colour.Response
	}

	return &imx678
}

// applyCamera fills -sensor and -pixel from -camera's chip unless given
// (or -fov, -scale), and returns the variant whose QE and read noise camera()
// takes as defaults; nil without -camera. -mono picks the mono version.
func (cfg *Config) applyCamera(set map[string]bool, name string, sensor *string, pixel *float64) (*Variant, error) {
	if !set["camera"] {
		return nil, nil //nolint:nilnil // no -camera is no preset, not an error
	}
	key := strings.ToLower(strings.TrimSpace(name))
	s, ok := Cameras[key]
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: -camera must be one of %s", errInvalidFlag, strings.Join(CameraNames(), ", "))
	case !set["focal"]:
		return nil, fmt.Errorf("%w: -camera needs -focal", errInvalidFlag)
	}
	v, kind := s.Colour, "colour"
	if cfg.Mono {
		v, kind = s.Mono, "mono"
	}
	if v == nil {
		return nil, fmt.Errorf("%w: no %s %s camera in the presets", errInvalidFlag, kind, strings.ToUpper(key))
	}
	cfg.Camera = key
	if !set["pixel"] && !set["scale"] {
		*pixel = s.Pixel
		set["pixel"] = true
	}
	if !set["sensor"] && !set["fov"] {
		*sensor = fmt.Sprintf("%gx%g", float64(s.PxLong)*s.Pixel/1000, float64(s.PxShort)*s.Pixel/1000)
		set["sensor"] = true
	}

	return v, nil
}
