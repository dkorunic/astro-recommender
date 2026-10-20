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

// Line is the response to line l: [O III] 495.9 takes the [O III] entry,
// the doublet's flux-weighted mean, as the charts' 10 nm grid does not tell
// the two lines apart.
func (r *Response) Line(l catalog.Line) [3]float64 {
	if l == catalog.OIII4959 {
		l = catalog.OIII
	}

	return r.Lines[l]
}

// MonoResponse is a mono sensor's spectral response relative to its peak
// QE: at each catalog.Line and its mean over atmos.CameraLo-CameraHi.
type MonoResponse struct {
	Lines [catalog.NumLines]float64
	Broad float64
}

// Line is the response to line l, [O III] 495.9 taking the [O III] entry
// as Response.Line does.
func (r *MonoResponse) Line(l catalog.Line) float64 {
	if l == catalog.OIII4959 {
		l = catalog.OIII
	}

	return r.Lines[l]
}

// sonyMono is the mono response of a preset without its own: read off
// Sony's curve for its back-illuminated chips, which a front-illuminated
// chip or a CCD falls well below in the red.
var sonyMono = MonoResponse{
	Lines: [catalog.NumLines]float64{catalog.Ha: 0.95, catalog.OIII: 0.85, catalog.SII: 0.9, catalog.Hb: 0.8, catalog.NII6548: 0.95, catalog.NII6583: 0.95},
	Broad: 0.8,
}

// Variant is a sensor's colour or mono camera: its peak QE and its read
// noise at Gain, the vendor's deep-sky setting in its own units (ZWO's and
// Player One's 0.1 dB), where the high conversion gain mode switches on;
// Response for a colour one, MonoResponse for a mono one whose maker charts
// it (nil is sonyMono). Gain is 0 for a CCD, which has no gain setting.
type Variant struct {
	Response      *Response
	MonoResponse  *MonoResponse
	QE, ReadNoise float64
	Gain          int
}

// Sensor is a -camera preset: the chip's pixels and its colour and mono
// cameras, nil when no astro camera ships that version. Dark is its dark
// current at a sensor temperature of 25 °C, e⁻/s per pixel, read off the
// maker's chart; 0 where none is published (typicalDark stands in).
type Sensor struct {
	Colour, Mono    *Variant
	Pixel           float64 // µm
	Dark            float64 // e⁻/s per pixel at 25 °C
	PxLong, PxShort int
}

// typicalDark is the dark current at 25 °C (e⁻/s per pixel) of a chip whose
// maker publishes no chart: the median of the charted presets', each chip
// once (the IMX492 is the IMX294's die), which TestTypicalDark recomputes.
const typicalDark = 0.16

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
	// ZWO ASI715MC manual (2024), p6, relative and peaking at 820 nm, so its
	// visible may read low as the ASI462 manual's did (imx462).
	imx715 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.940, 0.373, 0.139},
			catalog.OIII:    {0.061, 0.840, 0.470},
			catalog.SII:     {0.887, 0.413, 0.175},
			catalog.Hb:      {0.047, 0.690, 0.595},
			catalog.NII6548: {0.944, 0.372, 0.136},
			catalog.NII6583: {0.936, 0.374, 0.142},
		},
		Broad: [3]float64{0.411, 0.540, 0.309},
	}
	// ZWO ASI4400MC Pro product page's chart (ToupTek's SkyEye45AC Plus
	// page has the same one, to 0.01).
	imx366 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.793, 0.169, 0.053},
			catalog.OIII:    {0.061, 0.880, 0.497},
			catalog.SII:     {0.710, 0.186, 0.068},
			catalog.Hb:      {0.059, 0.726, 0.641},
			catalog.NII6548: {0.805, 0.171, 0.051},
			catalog.NII6583: {0.782, 0.167, 0.055},
		},
		Broad: [3]float64{0.366, 0.470, 0.290},
	}
	// ToupTek ATR428M/C product page's colour chart.
	imx428 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.966, 0.188, 0.065},
			catalog.OIII:    {0.026, 0.772, 0.461},
			catalog.SII:     {0.909, 0.220, 0.084},
			catalog.Hb:      {0.023, 0.592, 0.583},
			catalog.NII6548: {0.969, 0.191, 0.063},
			catalog.NII6583: {0.961, 0.189, 0.068},
		},
		Broad: [3]float64{0.398, 0.447, 0.255},
	}
	// ZWO ASI290 manual, p7 (ToupTek's G3M290C page has no chart).
	imx290 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.892, 0.173, 0.066},
			catalog.OIII:    {0.051, 0.861, 0.457},
			catalog.SII:     {0.816, 0.205, 0.082},
			catalog.Hb:      {0.041, 0.698, 0.605},
			catalog.NII6548: {0.898, 0.173, 0.064},
			catalog.NII6583: {0.886, 0.173, 0.068},
		},
		Broad: [3]float64{0.401, 0.464, 0.265},
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
	// Player One's Neptune-C II chart, relative and peaking in the near
	// infrared like the ASI462 manual's, so its visible may read low as that
	// one did (imx462).
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
	// ZWO's ASI662MC/ASI462MC comparison (March 2023, p3), the absolute QE
	// whose 81% peak is the green at 510 nm (its ASI662MC chart reproduces
	// starvis2 to 0.007). The ASI462 manual V1.1's relative chart peaks at
	// 810 nm, and scaled to that 81% it put the visible 10-35% low.
	imx462 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.775, 0.263, 0.117},
			catalog.OIII:    {0.051, 0.995, 0.553},
			catalog.SII:     {0.752, 0.295, 0.141},
			catalog.Hb:      {0.038, 0.949, 0.739},
			catalog.NII6548: {0.783, 0.263, 0.116},
			catalog.NII6583: {0.768, 0.264, 0.121},
		},
		Broad: [3]float64{0.414, 0.502, 0.391},
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

// The Seestar S50 Pro's OmniVision OS08B10: no maker's chart for the chip,
// so OmniVision's absolute QE chart for its Nyxel 2 technology, which a ZWO
// user group slide (October 2026) attributes to it ("believed to be used");
// digitized from that slide's 971 px copy, a second copy agreeing to 0.03.
// The weakest-sourced response here.
var os08b10 = Response{
	Lines: [catalog.NumLines][3]float64{
		catalog.Ha:      {0.912, 0.207, 0.082},
		catalog.OIII:    {0.038, 0.933, 0.546},
		catalog.SII:     {0.859, 0.263, 0.113},
		catalog.Hb:      {0.032, 0.695, 0.722},
		catalog.NII6548: {0.916, 0.206, 0.079},
		catalog.NII6583: {0.908, 0.207, 0.084},
	},
	Broad: [3]float64{0.378, 0.482, 0.346},
}

// The mono responses, each digitized by scripts/qe_digitize.py --mono from
// the maker's chart, relative to its peak: a CCD's and a front-illuminated
// chip's red falls well below sonyMono's.
var (
	// ZWO ASI294 manual V2.2, p6 (ToupTek's ATR294M chart reads the same).
	imx492Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.862, catalog.OIII: 0.982, catalog.SII: 0.829, catalog.Hb: 0.955, catalog.NII6548: 0.866, catalog.NII6583: 0.857},
		Broad: 0.913,
	}
	// ZWO ASI174 manual, p7 (absolute, its 78% peak).
	imx174Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.632, catalog.OIII: 0.998, catalog.SII: 0.579, catalog.Hb: 0.997, catalog.NII6548: 0.632, catalog.NII6583: 0.626},
		Broad: 0.823,
	}
	// ZWO ASI Mini manual, p6 (the ASI290MM Mini's).
	imx290Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.939, catalog.OIII: 0.907, catalog.SII: 0.914, catalog.Hb: 0.876, catalog.NII6548: 0.940, catalog.NII6583: 0.936},
		Broad: 0.882,
	}
	// ToupTek's GPM174M page.
	imx249Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.803, catalog.OIII: 0.974, catalog.SII: 0.761, catalog.Hb: 0.942, catalog.NII6548: 0.808, catalog.NII6583: 0.799},
		Broad: 0.865,
	}
	// ZWO ASI178 manual V1.3, p7 (ToupTek's G3M178M chart reads the same
	// to 0.005).
	imx178Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.809, catalog.OIII: 0.976, catalog.SII: 0.788, catalog.Hb: 0.966, catalog.NII6548: 0.813, catalog.NII6583: 0.805},
		Broad: 0.891,
	}
	// ZWO ASI183 manual V1.4, p7.
	imx183Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.799, catalog.OIII: 0.976, catalog.SII: 0.779, catalog.Hb: 0.954, catalog.NII6548: 0.803, catalog.NII6583: 0.797},
		Broad: 0.892,
	}
	// ZWO ASI432 manual V1.0, p6.
	imx432Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.954, catalog.OIII: 0.853, catalog.SII: 0.928, catalog.Hb: 0.827, catalog.NII6548: 0.957, catalog.NII6583: 0.952},
		Broad: 0.870,
	}
	// ZWO's absolute chart for its 3.76 µm chips, one curve in the ASI2600MM
	// Pro (p8), ASI533MM Pro (p8) and ASI461MM Pro (p8) manuals to 0.003 (the
	// ASI6200 manual has none, so the IMX455 keeps sonyMono).
	zwo376Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.642, catalog.OIII: 0.976, catalog.SII: 0.595, catalog.Hb: 0.981, catalog.NII6548: 0.646, catalog.NII6583: 0.636},
		Broad: 0.836,
	}
	// ZWO ASI585MC/MM Pro manual, p9 (absolute).
	imx585Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.881, catalog.OIII: 0.994, catalog.SII: 0.859, catalog.Hb: 0.993, catalog.NII6548: 0.883, catalog.NII6583: 0.878},
		Broad: 0.954,
	}
	// ZWO ASI662MC/MM manual, p8, peaking at Hα.
	imx662Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 1.000, catalog.OIII: 0.837, catalog.SII: 1.000, catalog.Hb: 0.815, catalog.NII6548: 1.000, catalog.NII6583: 1.000},
		Broad: 0.884,
	}
	// ZWO ASI676MM manual, p8.
	imx676Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.984, catalog.OIII: 0.844, catalog.SII: 1.000, catalog.Hb: 0.812, catalog.NII6548: 0.983, catalog.NII6583: 0.986},
		Broad: 0.888,
	}
	// On Semi's absolute chart in ZWO's ASI Mini manual, p6 (dark grey
	// gridlines: --black-max 0.3).
	ar0130Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.928, catalog.OIII: 0.898, catalog.SII: 0.893, catalog.Hb: 0.873, catalog.NII6548: 0.933, catalog.NII6583: 0.922},
		Broad: 0.895,
	}
	// ZWO ASI Mini manual (2026), p10, the ASI220MM Mini's absolute chart.
	sc2210Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.871, catalog.OIII: 1.000, catalog.SII: 0.845, catalog.Hb: 0.994, catalog.NII6548: 0.876, catalog.NII6583: 0.871},
		Broad: 0.942,
	}
	// Starlight Xpress's Trius PRO-694 page, its "H674/694" curve (absolute,
	// tick marks: --xpx); its 0.84 at Hα is the page's "65% at H alpha"
	// against the 77% peak. Sony's relative chart for the ICX694ALG reads
	// 0.91 there.
	icx694Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.838, catalog.OIII: 0.988, catalog.SII: 0.796, catalog.Hb: 0.984, catalog.NII6548: 0.847, catalog.NII6583: 0.835},
		Broad: 0.919,
	}
	// Sony's ICX814ALG/AQG overview, figure 2 (white gridlines on a tint:
	// --light-grid).
	icx814Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.896, catalog.OIII: 0.958, catalog.SII: 0.839, catalog.Hb: 0.944, catalog.NII6548: 0.900, catalog.NII6583: 0.889},
		Broad: 0.901,
	}
)

// The chips the -scope presets share, as Cameras keys.
const (
	chipIMX178  = "imx178"
	chipIMX224  = "imx224"
	chipIMX462  = "imx462"
	chipIMX585  = "imx585"
	chipIMX662  = "imx662"
	chipIMX676  = "imx676"
	chipIMX678  = "imx678"
	chipOS08B10 = "os08b10"
)

// Cameras are the -camera presets by lower-case chip name, from the camera
// makers' manuals and product pages (ZWO's, else Player One's, else
// ToupTek's, October 2026; ToupTek's gain is its HCG mode's gain value, 100
// at 0 dB): the pixels of the camera's output, its peak QE (the vendor's, often
// an estimate) and its read noise at the vendor's deep-sky gain, where the
// high conversion gain mode switches on (ZWO's gain in 0.1 dB, read off its
// chart to about ±0.2 e⁻). A version is left out where no astro camera
// ships it or its peak QE is unpublished (the IMX183's and IMX174's colour
// ones); a mono version takes its MonoResponse, else sonyMono. The
// IMX678 preset is ZWO's ASI678MC/MM at gain 182 (one QE for both, as ZWO
// quotes it); the Origin keeps its own read noise (Scopes' ReadNoise).
var Cameras = map[string]Sensor{
	// Dark current: ZWO ASI2600MC/MM Pro manual (2024), §3.5.
	"imx571": {
		Pixel: 3.76, Dark: 0.065, PxLong: 6248, PxShort: 4176,
		Colour: &Variant{Response: &imx571, Gain: 100, QE: 0.80, ReadNoise: 1.4},
		Mono:   &Variant{MonoResponse: &zwo376Mono, Gain: 100, QE: 0.91, ReadNoise: 1.4},
	},
	// Dark current: ZWO ASI533MC Pro manual (2026), §3.6.
	"imx533": {
		Pixel: 3.76, Dark: 0.063, PxLong: 3008, PxShort: 3008,
		Colour: &Variant{Response: &imx533, Gain: 100, QE: 0.80, ReadNoise: 1.5},
		Mono:   &Variant{MonoResponse: &zwo376Mono, Gain: 100, QE: 0.91, ReadNoise: 1.5},
	},
	// Dark current: ZWO ASI585MC/MM Pro manual (2026), §3.6.
	chipIMX585: {
		Pixel: 2.9, Dark: 0.25, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &imx585, Gain: 200, QE: 0.91, ReadNoise: 1.0},
		Mono:   &Variant{MonoResponse: &imx585Mono, Gain: 200, QE: 0.91, ReadNoise: 1.0},
	},
	// The read noise ZWO's current ASI6200 chart's (its v1.4 manual's 1.5
	// predates the 0.86 e⁻ revision).
	"imx455": {
		Pixel: 3.76, PxLong: 9576, PxShort: 6388,
		Colour: &Variant{Response: &imx455, Gain: 100, QE: 0.80, ReadNoise: 1.35},
		Mono:   &Variant{Gain: 100, QE: 0.91, ReadNoise: 1.35},
	},
	// Dark current: ZWO ASI294MC Pro manual (2026), §3.6.
	"imx294": {
		Pixel: 4.63, Dark: 0.37, PxLong: 4144, PxShort: 2822,
		Colour: &Variant{Response: &imx294, Gain: 120, QE: 0.75, ReadNoise: 1.7},
	},
	// The ASI294MM's chip, at its default 2x2 binning.
	// Dark current: the IMX294's die (ZWO's ASI294MC Pro chart).
	"imx492": {
		Pixel: 4.63, Dark: 0.37, PxLong: 4144, PxShort: 2822,
		Mono: &Variant{MonoResponse: &imx492Mono, Gain: 120, QE: 0.90, ReadNoise: 1.8},
	},
	// No high conversion gain mode: unity gain.
	"imx183": {
		Pixel: 2.4, PxLong: 5496, PxShort: 3672,
		Mono: &Variant{MonoResponse: &imx183Mono, Gain: 120, QE: 0.84, ReadNoise: 2.1},
	},
	// No dark current: the ASI678 CN manual's chart labels its -20 °C point
	// 0.0078 e⁻/s against an axis reading 0.12, so typicalDark stands in.
	chipIMX678: {
		Pixel: 2.0, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &imx678, Gain: 182, QE: 0.83, ReadNoise: 0.9},
		Mono:   &Variant{Gain: 182, QE: 0.83, ReadNoise: 0.9},
	},
	chipIMX662: {
		Pixel: 2.9, PxLong: 1920, PxShort: 1080,
		Colour: &Variant{Response: &starvis2, Gain: 200, QE: 0.91, ReadNoise: 1.2},
		Mono:   &Variant{MonoResponse: &imx662Mono, Gain: 200, QE: 0.91, ReadNoise: 1.2},
	},
	"imx664": {
		Pixel: 2.9, PxLong: 2704, PxShort: 1536,
		Colour: &Variant{Response: &starvis2, Gain: 252, QE: 0.91, ReadNoise: 0.8},
	},
	// Dark current: ZWO ASI676MC manual (2024), §3.4.
	chipIMX676: {
		Pixel: 2.0, Dark: 0.060, PxLong: 3552, PxShort: 3552,
		Colour: &Variant{Response: &imx676, Gain: 180, QE: 0.83, ReadNoise: 0.75},
		Mono:   &Variant{MonoResponse: &imx676Mono, Gain: 180, QE: 0.83, ReadNoise: 0.75},
	},
	// The read noise ZWO's chart's at gain 100, where it drops from 1.7 e⁻
	// at 50 (the conversion gain switch).
	"imx715": {
		Pixel: 1.45, PxLong: 3864, PxShort: 2192,
		Colour: &Variant{Response: &imx715, Gain: 100, QE: 0.80, ReadNoise: 1.2},
	},
	// ZWO's ASI4400MC Pro (also ToupTek's SkyEye45AC Plus): the read noise
	// its chart's at gain 136, where it drops from 5.8 e⁻ (its table's 1.1
	// is gain 400's). No Dark yet: its chart's 0.126 e⁻/s at 25 °C would
	// move typicalDark, the charted median, from 0.16 to 0.13.
	"imx366": {
		Pixel: 4.40, PxLong: 8192, PxShort: 5460,
		Colour: &Variant{Response: &imx366, Gain: 136, QE: 0.80, ReadNoise: 1.7},
	},
	// ToupTek's ATR428M/C (global shutter, sold for solar imaging): one peak
	// QE for both, as ToupTek quotes it; each version's HCG read noise at
	// gain 100.
	"imx428": {
		Pixel: 4.5, PxLong: 3200, PxShort: 2200,
		Colour: &Variant{Response: &imx428, Gain: 100, QE: 0.79, ReadNoise: 2.4},
		Mono:   &Variant{Gain: 100, QE: 0.79, ReadNoise: 2.5},
	},
	// ToupTek's G3M290C/M: its peak QE (ZWO's ASI290 manual says TBD); the
	// read noise and the frame ZWO's (ASI290 manual, gain 60, where it drops
	// from 2.6 e⁻ at 50).
	"imx290": {
		Pixel: 2.9, PxLong: 1936, PxShort: 1096,
		Colour: &Variant{Response: &imx290, Gain: 60, QE: 0.81, ReadNoise: 1.65},
		Mono:   &Variant{MonoResponse: &imx290Mono, Gain: 60, QE: 0.81, ReadNoise: 1.65},
	},
	// ToupTek's GPM174M guide camera. No high conversion gain mode: its
	// chart's read noise at gain 1778, where it levels off.
	"imx249": {
		Pixel: 5.86, PxLong: 1920, PxShort: 1200,
		Mono: &Variant{MonoResponse: &imx249Mono, Gain: 1778, QE: 0.89, ReadNoise: 4.0},
	},
	// ZWO's ASI120MM Mini (also Omegon's Guide 1200 and Lacerta's MGEN-3).
	// No high conversion gain mode: its chart's read noise at gain 33, past
	// the last step down (6.5 e⁻ to 15, 4.7 to 32); "almost 80%" its peak.
	"ar0130": {
		Pixel: 3.75, PxLong: 1280, PxShort: 960,
		Mono: &Variant{MonoResponse: &ar0130Mono, Gain: 33, QE: 0.78, ReadNoise: 3.9},
	},
	// SmartSens' SC2210 in ZWO's ASI220MM Mini (its Duo and Air cameras'
	// guide sensor too): the read noise its chart's at gain 106, where it
	// drops from 2.0 e⁻.
	"sc2210": {
		Pixel: 4.0, PxLong: 1920, PxShort: 1080,
		Mono: &Variant{MonoResponse: &sc2210Mono, Gain: 106, QE: 0.92, ReadNoise: 1.03},
	},
	// Starlight Xpress's Trius PRO-694 and PRO-674 CCDs (Sony EXview II, which
	// Sony ended): their pages' peak QE and typical read noise. The colour
	// versions' peak QE is unpublished (SX repeats the mono's).
	"icx694": {
		Pixel: 4.54, PxLong: 2750, PxShort: 2200,
		Mono: &Variant{MonoResponse: &icx694Mono, QE: 0.77, ReadNoise: 3.5},
	},
	"icx674": {
		Pixel: 4.54, PxLong: 1940, PxShort: 1460,
		Mono: &Variant{MonoResponse: &icx694Mono, QE: 0.77, ReadNoise: 3.5},
	},
	// Starlight Xpress's Trius PRO-814 (Sony EXview II).
	"icx814": {
		Pixel: 3.69, PxLong: 3388, PxShort: 2712,
		Mono: &Variant{MonoResponse: &icx814Mono, QE: 0.77, ReadNoise: 3.0},
	},
	"imx410": {
		Pixel: 5.94, PxLong: 6072, PxShort: 4042,
		Colour: &Variant{Response: &imx410, Gain: 140, QE: 0.80, ReadNoise: 1.5},
	},
	"imx461": {
		Pixel: 3.76, PxLong: 11656, PxShort: 8750,
		Mono: &Variant{MonoResponse: &zwo376Mono, Gain: 100, QE: 0.91, ReadNoise: 1.45},
	},
	"imx432": {
		Pixel: 9.0, PxLong: 1608, PxShort: 1104,
		Mono: &Variant{MonoResponse: &imx432Mono, Gain: 140, QE: 0.79, ReadNoise: 4.3},
	},
	// No high conversion gain mode: ZWO's figure at gain 300.
	"imx174": {
		Pixel: 5.86, PxLong: 1936, PxShort: 1216,
		Mono: &Variant{MonoResponse: &imx174Mono, Gain: 300, QE: 0.77, ReadNoise: 3.5},
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
	// from Sony's datasheet, second-hand; the mono's 0.81 ToupTek's G3M178M
	// figure. The read noise ZWO's chart's at gain 100, for both.
	// Dark current: ZWO ASI678 CN manual V1.0's ASI178MC chart.
	chipIMX178: {
		Pixel: 2.4, Dark: 0.95, PxLong: 3096, PxShort: 2080,
		Colour: &Variant{Response: &imx178, Gain: 100, QE: 0.70, ReadNoise: 1.7},
		Mono:   &Variant{MonoResponse: &imx178Mono, Gain: 100, QE: 0.81, ReadNoise: 1.7},
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
	// The Seestar S50 Pro's, in no astro camera: the peak QE the Nyxel 2
	// chart's (os08b10's comment), 80% green at 530 nm; the read noise
	// Cuiv's measurement (The Lazy Geek, 2026) at gain 150, where it drops
	// from 2.4 e⁻ at 100 (the conversion gain switch). Neither is the
	// maker's.
	chipOS08B10: {
		Pixel: 2.9, PxLong: 3840, PxShort: 2160,
		Colour: &Variant{Response: &os08b10, Gain: 150, QE: 0.80, ReadNoise: 0.84},
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

// MonoResponse returns the mono response the sub-exposure estimate uses:
// -camera's mono version's, else sonyMono.
func (cfg *Config) MonoResponse() *MonoResponse {
	if s, ok := Cameras[cfg.Camera]; ok && s.Mono != nil && s.Mono.MonoResponse != nil {
		return s.Mono.MonoResponse
	}

	return &sonyMono
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
