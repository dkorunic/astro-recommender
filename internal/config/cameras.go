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

// sonyMono is the mono response of a preset without its own (the IMX678's)
// and of -mono without -camera: the IMX585's, a STARVIS 2 chip whose maker
// charts its QE, as the IMX662's and IMX676's Sony charts read per watt
// within 0.03.
var sonyMono = imx585Mono

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
// maker publishes no chart: the median of the charted CMOS presets', each
// chip once (the IMX492 is the IMX294's die; a CCD's runs ten times higher),
// which TestTypicalDark recomputes.
const typicalDark = 0.16

// The colour responses, each digitized by scripts/qe_digitize.py from the
// vendor's chart (gridlines found, curves told apart by hue, relative to the
// chart's peak; [O III] with 495.9 nm at a third of 500.7's flux, [S II] the
// doublet's mean), each channel's mean over 400-700 nm the continuum's.
// Several vendors share one chart across chips; where two of a chip's charts
// disagree, the comment says which was taken. Sony's charts ("relative
// response", copied by most camera makers) are the response per watt: their
// curve / wavelength is the QE (Allied Vision's "absolute QE" curves and
// ZWO's own QE charts are exactly that), so they are read with --per-watt,
// each comment saying "per watt", and peak in the green.
var (
	// ZWO ASI678MC manual V1.0, p6, its QE chart (blue peaking highest, as
	// a per-photon curve does; no Sony chart to check it against).
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
	// ZWO ASI2600MC Pro manual (2026), p8, per watt.
	imx571 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.637, 0.119, 0.039},
			catalog.OIII:    {0.033, 0.969, 0.526},
			catalog.SII:     {0.578, 0.138, 0.051},
			catalog.Hb:      {0.029, 0.786, 0.727},
			catalog.NII6548: {0.643, 0.120, 0.037},
			catalog.NII6583: {0.633, 0.119, 0.040},
		},
		Broad: [3]float64{0.292, 0.437, 0.324},
	}
	// ZWO ASI533MC Pro manual (2026), p8, per watt.
	imx533 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.645, 0.122, 0.037},
			catalog.OIII:    {0.033, 0.951, 0.571},
			catalog.SII:     {0.586, 0.134, 0.052},
			catalog.Hb:      {0.031, 0.717, 0.770},
			catalog.NII6548: {0.650, 0.123, 0.035},
			catalog.NII6583: {0.639, 0.120, 0.039},
		},
		Broad: [3]float64{0.288, 0.437, 0.328},
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
	// ZWO ASI676MC manual (2024), p8, per watt.
	imx676 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.807, 0.332, 0.117},
			catalog.OIII:    {0.049, 0.963, 0.515},
			catalog.SII:     {0.740, 0.356, 0.141},
			catalog.Hb:      {0.039, 0.838, 0.680},
			catalog.NII6548: {0.812, 0.333, 0.115},
			catalog.NII6583: {0.802, 0.330, 0.120},
		},
		Broad: [3]float64{0.373, 0.559, 0.368},
	}
	// ZWO ASI715MC manual (2024), per watt; its chart is the ASI664 manual's
	// IMX664 one to 0.01, so ZWO may have reused it.
	imx715 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.815, 0.323, 0.120},
			catalog.OIII:    {0.069, 0.956, 0.535},
			catalog.SII:     {0.750, 0.349, 0.148},
			catalog.Hb:      {0.055, 0.807, 0.697},
			catalog.NII6548: {0.820, 0.323, 0.119},
			catalog.NII6583: {0.809, 0.323, 0.123},
		},
		Broad: [3]float64{0.376, 0.554, 0.360},
	}
	// ZWO ASI4400MC Pro product page's chart, per watt (ToupTek's SkyEye45AC
	// Plus page has the same one, to 0.01).
	imx366 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.643, 0.137, 0.043},
			catalog.OIII:    {0.065, 0.938, 0.530},
			catalog.SII:     {0.562, 0.147, 0.054},
			catalog.Hb:      {0.065, 0.795, 0.703},
			catalog.NII6548: {0.654, 0.139, 0.042},
			catalog.NII6583: {0.633, 0.135, 0.045},
		},
		Broad: [3]float64{0.320, 0.459, 0.329},
	}
	// ToupTek's ATR428M product page's colour chart (ATR428M-Wave_length.jpg,
	// Wayback 2026-09-26), per watt.
	imx428 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.844, 0.165, 0.057},
			catalog.OIII:    {0.030, 0.887, 0.530},
			catalog.SII:     {0.776, 0.187, 0.072},
			catalog.Hb:      {0.028, 0.699, 0.688},
			catalog.NII6548: {0.849, 0.167, 0.055},
			catalog.NII6583: {0.837, 0.165, 0.059},
		},
		Broad: [3]float64{0.364, 0.464, 0.308},
	}
	// ZWO ASI290 manual, p7, per watt (ToupTek's G3M290C page has no chart).
	imx290 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.743, 0.144, 0.055},
			catalog.OIII:    {0.056, 0.943, 0.500},
			catalog.SII:     {0.664, 0.167, 0.067},
			catalog.Hb:      {0.047, 0.785, 0.680},
			catalog.NII6548: {0.750, 0.144, 0.054},
			catalog.NII6583: {0.736, 0.144, 0.056},
		},
		Broad: [3]float64{0.354, 0.462, 0.306},
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
	// Player One's Neptune-C II chart, per watt (peaking in the near infrared
	// as charted, in the green per photon).
	imx464 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.767, 0.235, 0.115},
			catalog.OIII:    {0.050, 0.933, 0.537},
			catalog.SII:     {0.700, 0.259, 0.132},
			catalog.Hb:      {0.040, 0.751, 0.705},
			catalog.NII6548: {0.773, 0.235, 0.112},
			catalog.NII6583: {0.758, 0.235, 0.116},
		},
		Broad: [3]float64{0.359, 0.482, 0.366},
	}
	// ZWO ASI178 manual V1.3, p7 (400-700 nm), per watt.
	imx178 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.610, 0.121, 0.043},
			catalog.OIII:    {0.041, 0.960, 0.505},
			catalog.SII:     {0.534, 0.138, 0.053},
			catalog.Hb:      {0.034, 0.791, 0.694},
			catalog.NII6548: {0.618, 0.121, 0.042},
			catalog.NII6583: {0.599, 0.121, 0.045},
		},
		Broad: [3]float64{0.299, 0.431, 0.319},
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
	// ZWO ASI224MC manual V1.4, p7, per watt.
	imx224 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.812, 0.163, 0.071},
			catalog.OIII:    {0.060, 0.938, 0.515},
			catalog.SII:     {0.722, 0.192, 0.083},
			catalog.Hb:      {0.053, 0.770, 0.679},
			catalog.NII6548: {0.817, 0.162, 0.069},
			catalog.NII6583: {0.803, 0.164, 0.073},
		},
		Broad: [3]float64{0.377, 0.465, 0.314},
	}
	// ZWO ASI294MC Pro manual (2026), p8, per watt.
	imx294 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.706, 0.134, 0.042},
			catalog.OIII:    {0.034, 0.949, 0.520},
			catalog.SII:     {0.628, 0.148, 0.052},
			catalog.Hb:      {0.028, 0.751, 0.722},
			catalog.NII6548: {0.712, 0.134, 0.041},
			catalog.NII6583: {0.697, 0.133, 0.043},
		},
		Broad: [3]float64{0.321, 0.445, 0.321},
	}
	// ZWO ASI2400MC Pro manual V1.2, p6, per watt.
	imx410 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.674, 0.089, 0.033},
			catalog.OIII:    {0.055, 0.638, 0.529},
			catalog.SII:     {0.591, 0.110, 0.042},
			catalog.Hb:      {0.053, 0.502, 0.725},
			catalog.NII6548: {0.680, 0.088, 0.032},
			catalog.NII6583: {0.666, 0.090, 0.034},
		},
		Broad: [3]float64{0.324, 0.403, 0.315},
	}
	// Player One's Zeus 455C chart (ZWO's ASI6200 manuals have none; --dark):
	// labelled QE, but Sony's curve scaled to the 80% peak, so per watt.
	imx455 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.642, 0.056, 0.016},
			catalog.OIII:    {0.017, 0.881, 0.527},
			catalog.SII:     {0.566, 0.072, 0.022},
			catalog.Hb:      {0.013, 0.704, 0.712},
			catalog.NII6548: {0.648, 0.057, 0.015},
			catalog.NII6583: {0.634, 0.056, 0.017},
		},
		Broad: [3]float64{0.284, 0.396, 0.300},
	}
	// QHY's QHY5III568C chart (Sony's), from Lunatico's copy of QHY's page,
	// per watt.
	imx568 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.686, 0.246, 0.044},
			catalog.OIII:    {0.033, 0.930, 0.530},
			catalog.SII:     {0.608, 0.266, 0.055},
			catalog.Hb:      {0.030, 0.774, 0.702},
			catalog.NII6548: {0.691, 0.247, 0.043},
			catalog.NII6583: {0.680, 0.245, 0.046},
		},
		Broad: [3]float64{0.298, 0.520, 0.319},
	}
	// ZWO ASI1600 manual (rev 1.4, 2020), p7 (Panasonic's chart, relative).
	mn34230 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.797, 0.115, 0.050},
			catalog.OIII:    {0.033, 0.634, 0.566},
			catalog.SII:     {0.737, 0.148, 0.061},
			catalog.Hb:      {0.031, 0.465, 0.690},
			catalog.NII6548: {0.803, 0.114, 0.048},
			catalog.NII6583: {0.788, 0.116, 0.052},
		},
		Broad: [3]float64{0.359, 0.410, 0.292},
	}
	// onsemi's KAF-16200 datasheet (Rev. 2, 2017), figure 7 (absolute, also
	// on Moravian's G3-16200C page): its two green curves digitized apart
	// (the navy one recoloured) and averaged.
	kaf16200 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.742, 0.174, 0.126},
			catalog.OIII:    {0.040, 0.689, 0.678},
			catalog.SII:     {0.684, 0.204, 0.149},
			catalog.Hb:      {0.033, 0.488, 0.786},
			catalog.NII6548: {0.739, 0.167, 0.117},
			catalog.NII6583: {0.743, 0.176, 0.133},
		},
		Broad: [3]float64{0.351, 0.410, 0.370},
	}
	// The Imaging Source's DFK 33UX252 datasheet, p4 (Sony's IMX252LQR-C, per
	// watt; tick marks only, so read with the curve's own axes, ÷ λ).
	imx252 = Response{
		Lines: [catalog.NumLines][3]float64{
			catalog.Ha:      {0.822, 0.148, 0.052},
			catalog.OIII:    {0.027, 0.923, 0.440},
			catalog.SII:     {0.739, 0.181, 0.066},
			catalog.Hb:      {0.022, 0.753, 0.608},
			catalog.NII6548: {0.829, 0.149, 0.050},
			catalog.NII6583: {0.812, 0.149, 0.054},
		},
		Broad: [3]float64{0.361, 0.458, 0.284},
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
// the maker's chart, relative to its peak, a Sony chart per watt as the colour
// ones: a CCD's and a front-illuminated chip's red falls below sonyMono's.
var (
	// ZWO ASI294 manual V2.2, p6, per watt (ToupTek's ATR294M chart reads
	// the same).
	imx492Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.664, catalog.OIII: 0.993, catalog.SII: 0.623, catalog.Hb: 0.993, catalog.NII6548: 0.668, catalog.NII6583: 0.657},
		Broad: 0.859,
	}
	// ZWO ASI174 manual, p7 (absolute, its 78% peak).
	imx174Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.632, catalog.OIII: 0.998, catalog.SII: 0.579, catalog.Hb: 0.997, catalog.NII6548: 0.632, catalog.NII6583: 0.626},
		Broad: 0.823,
	}
	// ZWO ASI Mini manual, p6 (the ASI290MM Mini's), per watt.
	imx290Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.786, catalog.OIII: 0.998, catalog.SII: 0.747, catalog.Hb: 0.990, catalog.NII6548: 0.789, catalog.NII6583: 0.781},
		Broad: 0.887,
	}
	// ToupTek's GPM174M page, per watt: Sony's IMX174/IMX249 curve, which
	// then reads ZWO's ASI174 QE chart (imx174Mono) to 0.007.
	imx249Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.626, catalog.OIII: 0.997, catalog.SII: 0.578, catalog.Hb: 0.990, catalog.NII6548: 0.631, catalog.NII6583: 0.620},
		Broad: 0.822,
	}
	// ZWO ASI178 manual V1.3, p7, per watt (ToupTek's G3M178M chart reads
	// the same to 0.005).
	imx178Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.618, catalog.OIII: 0.979, catalog.SII: 0.587, catalog.Hb: 0.994, catalog.NII6548: 0.621, catalog.NII6583: 0.613},
		Broad: 0.833,
	}
	// ZWO ASI183 manual V1.4, p7, per watt.
	imx183Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.617, catalog.OIII: 0.991, catalog.SII: 0.587, catalog.Hb: 0.995, catalog.NII6548: 0.621, catalog.NII6583: 0.613},
		Broad: 0.844,
	}
	// ZWO ASI432 manual V1.0, p6, per watt; its QE peaks at or below the
	// chart's 400 nm edge, which it is taken relative to.
	imx432Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.834, catalog.OIII: 0.977, catalog.SII: 0.791, catalog.Hb: 0.972, catalog.NII6548: 0.837, catalog.NII6583: 0.828},
		Broad: 0.909,
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
	// ZWO ASI662MC/MM manual, p8, per watt (peaking at Hα as charted); its
	// QE peaks at or below the chart's 400 nm edge, as the IMX432's.
	imx662Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.876, catalog.OIII: 0.963, catalog.SII: 0.855, catalog.Hb: 0.963, catalog.NII6548: 0.878, catalog.NII6583: 0.873},
		Broad: 0.929,
	}
	// ZWO ASI676MM manual, p8, per watt.
	imx676Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.865, catalog.OIII: 0.975, catalog.SII: 0.859, catalog.Hb: 0.964, catalog.NII6548: 0.866, catalog.NII6583: 0.864},
		Broad: 0.939,
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
	// QHY's QHY5III568M chart (Sony's), from Lunatico's copy of QHY's page,
	// per watt (QHY's absolute IMX530 chart, imx530Mono, reads within 0.01).
	imx568Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.678, catalog.OIII: 0.959, catalog.SII: 0.650, catalog.Hb: 0.968, catalog.NII6548: 0.682, catalog.NII6583: 0.673},
		Broad: 0.852,
	}
	// QHY's QHY530 page, its absolute chart (a pale pink curve and grid:
	// --sat-min 0.1 --grid-max 0.9, --box and --xpx at the frame).
	imx530Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.686, catalog.OIII: 0.958, catalog.SII: 0.655, catalog.Hb: 0.972, catalog.NII6548: 0.689, catalog.NII6583: 0.680},
		Broad: 0.854,
	}
	// QHY's QHY550 page, its absolute chart (as imx530Mono's): the
	// front-illuminated Pregius' 70% peak, flat from 470 to 580 nm.
	imx250Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.839, catalog.OIII: 0.982, catalog.SII: 0.795, catalog.Hb: 0.985, catalog.NII6548: 0.845, catalog.NII6583: 0.834},
		Broad: 0.911,
	}
	// ZWO ASI1600 manual (rev 1.4, 2020), p7.
	mn34230Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.762, catalog.OIII: 0.930, catalog.SII: 0.703, catalog.Hb: 0.925, catalog.NII6548: 0.766, catalog.NII6583: 0.759},
		Broad: 0.829,
	}
	// onsemi's KAF-16200 datasheet (Rev. 2, 2017), figure 6 (absolute, also
	// on Moravian's G3-16200 page).
	kaf16200Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.799, catalog.OIII: 0.914, catalog.SII: 0.763, catalog.Hb: 0.860, catalog.NII6548: 0.809, catalog.NII6583: 0.794},
		Broad: 0.841,
	}
	// onsemi's KAF-16803 datasheet (Rev. 2, 2015), figure 6, without cover
	// glass (absolute, also on Moravian's G4-16000 page).
	kaf16803Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.769, catalog.OIII: 0.923, catalog.SII: 0.726, catalog.Hb: 0.886, catalog.NII6548: 0.774, catalog.NII6583: 0.761},
		Broad: 0.853,
	}
	// Atik's ICX825/ICX285 comparison chart (relative, points every 50 nm,
	// so coarse; its yellow-green curve hue-shifted into --mono green's band,
	// tick marks: --xpx). Starlight Xpress publishes none.
	icx825Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.963, catalog.OIII: 0.897, catalog.SII: 0.940, catalog.Hb: 0.859, catalog.NII6548: 0.963, catalog.NII6583: 0.960},
		Broad: 0.885,
	}
	// Sony's ICX828/829 overview, figure 1, the ICX829ALA panel (relative;
	// dark gridlines on a tint: --grid-max 0.5).
	icx829Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.933, catalog.OIII: 0.903, catalog.SII: 0.897, catalog.Hb: 0.865, catalog.NII6548: 0.937, catalog.NII6583: 0.927},
		Broad: 0.872,
	}
	// QHY's QHY4040 Pro FSI page, its absolute chart's GSENSE4040-CMT curve
	// (the in-plot legend painted out).
	gsense4040Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.941, catalog.OIII: 0.961, catalog.SII: 0.944, catalog.Hb: 0.962, catalog.NII6548: 0.945, catalog.NII6583: 0.936},
		Broad: 0.944,
	}
	// QHY's QHY4040 Pro BSI page, its absolute chart.
	gsense4040BSIMono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.941, catalog.OIII: 0.957, catalog.SII: 0.913, catalog.Hb: 0.883, catalog.NII6548: 0.939, catalog.NII6583: 0.941},
		Broad: 0.912,
	}
	// The Imaging Source's DMK 37BUX287 manual, §4.1 (Sony's IMX273/287, per
	// watt; tick marks only, so read with the curve's own axes, ÷ λ).
	imx287Mono = MonoResponse{
		Lines: [catalog.NumLines]float64{catalog.Ha: 0.849, catalog.OIII: 0.988, catalog.SII: 0.800, catalog.Hb: 0.990, catalog.NII6548: 0.853, catalog.NII6583: 0.843},
		Broad: 0.919,
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
// ToupTek's, else QHY's, October 2026; ToupTek's gain is its HCG mode's
// gain value, 100 at 0 dB, QHY's its own 0-350 scale): the pixels of the camera's output, its peak QE (the vendor's, often
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
	// predates the 0.86 e⁻ revision). The mono takes ZWO's curve for the
	// IMX571's 3.76 µm pixels (zwo376Mono), the ASI6200 manual charting none.
	"imx455": {
		Pixel: 3.76, PxLong: 9576, PxShort: 6388,
		Colour: &Variant{Response: &imx455, Gain: 100, QE: 0.80, ReadNoise: 1.35},
		Mono:   &Variant{MonoResponse: &zwo376Mono, Gain: 100, QE: 0.91, ReadNoise: 1.35},
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
	// gain 100. The mono takes the IMX250's curve (front-illuminated Pregius
	// too, whose 9 µm IMX432 reads the same at Hα per watt), ToupTek charting
	// only the colour.
	"imx428": {
		Pixel: 4.5, PxLong: 3200, PxShort: 2200,
		Colour: &Variant{Response: &imx428, Gain: 100, QE: 0.79, ReadNoise: 2.4},
		Mono:   &Variant{MonoResponse: &imx250Mono, Gain: 100, QE: 0.79, ReadNoise: 2.5},
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
	// QHY's QHY5III568C/M (Pregius S, global shutter). No high conversion
	// gain mode: its chart's read noise at gain 250, where it levels off.
	// QHY publishes only relative charts, so the peak QE is FLIR's EMVA 1288
	// report for its Dragonfly S DR-U3-50S4M/C, 523 nm: the mono's 67% and
	// the colour's green 53% (its red and blue, 63% and 86% of the green,
	// against the chart's 88% and 78%).
	"imx568": {
		Pixel: 2.74, PxLong: 2472, PxShort: 2064,
		Colour: &Variant{Response: &imx568, Gain: 250, QE: 0.53, ReadNoise: 1.54},
		Mono:   &Variant{MonoResponse: &imx568Mono, Gain: 250, QE: 0.67, ReadNoise: 1.54},
	},
	// QHY's QHY530M Pro II (Pregius S, global shutter, Camera Link): its
	// chart's 88% peak, no high conversion gain mode, the read noise at gain
	// 200, where it levels off. No Dark: its chart stops at 20 °C (0.085).
	"imx530": {
		Pixel: 2.74, PxLong: 5328, PxShort: 4608,
		Mono: &Variant{MonoResponse: &imx530Mono, Gain: 200, QE: 0.88, ReadNoise: 1.3},
	},
	// QHY's QHY550M (Pregius, global shutter; its QHY550P the polarized
	// version): its chart's 70% peak, the read noise at gain 200, past which
	// it hardly falls. No Dark: its chart stops at 10 °C, flattening below
	// 0 °C to 0.043 at -20 °C.
	"imx250": {
		Pixel: 3.45, PxLong: 2460, PxShort: 2070,
		Mono: &Variant{MonoResponse: &imx250Mono, Gain: 200, QE: 0.70, ReadNoise: 2.1},
	},
	// ZWO's ASI1600MM/MC (also QHY's QHY163M, Meade's DSI IV and Explore
	// Scientific's Deep Sky 16MP): ZWO's "around 60%" peak for both, an
	// estimate. No high conversion gain mode: unity gain. No Dark: its
	// chart's 0.40 e⁻/s at 25 °C would move typicalDark from 0.16 to 0.25.
	"mn34230": {
		Pixel: 3.8, PxLong: 4656, PxShort: 3520,
		Colour: &Variant{Response: &mn34230, Gain: 139, QE: 0.60, ReadNoise: 1.8},
		Mono:   &Variant{MonoResponse: &mn34230Mono, Gain: 139, QE: 0.60, ReadNoise: 1.8},
	},
	// Moravian's G3-16200/G3-16200C and Starlight Xpress's Trius SX-46 (CCD):
	// SX's peak and typical read noise for the mono, onsemi's colour peak
	// (its green) and Moravian's read noise for the colour. Dark current:
	// Moravian's 0.08 e⁻/s at 0 °C doubling every 5.7 °C, extrapolated to
	// 25 °C (no chart reaches it).
	"kaf16200": {
		Pixel: 6.0, Dark: 1.7, PxLong: 4540, PxShort: 3640,
		Colour: &Variant{Response: &kaf16200, QE: 0.40, ReadNoise: 10},
		Mono:   &Variant{MonoResponse: &kaf16200Mono, QE: 0.58, ReadNoise: 9},
	},
	// Starlight Xpress's Trius SX-56 (CCD; also Moravian's G4-16000, 11 e⁻):
	// onsemi's peak, SX's read noise. Dark current: onsemi's table at 25 °C.
	"kaf16803": {
		Pixel: 9.0, Dark: 3, PxLong: 4096, PxShort: 4096,
		Mono: &Variant{MonoResponse: &kaf16803Mono, QE: 0.60, ReadNoise: 9},
	},
	// Starlight Xpress's Trius PRO-825, Ultrastar PRO and Oculus PRO (Sony
	// EXview II CCD): their pages' peak QE and typical read noise.
	"icx825": {
		Pixel: 6.45, PxLong: 1392, PxShort: 1040,
		Mono: &Variant{MonoResponse: &icx825Mono, QE: 0.75, ReadNoise: 3.5},
	},
	// QHY's QHY695A (Sony EXview II CCD): its manual's peak QE and the middle
	// of its "typical 4e to 5e". No chart: the ICX694's, its sibling's.
	"icx695": {
		Pixel: 4.54, PxLong: 2758, PxShort: 2208,
		Mono: &Variant{MonoResponse: &icx694Mono, QE: 0.79, ReadNoise: 4.5},
	},
	// Starlight Xpress's Lodestar PRO guide camera (Sony EXview II CCD): its
	// 8.6 x 8.3 µm pixels as their mean; the 77% peak only in SX's autoguider
	// specification sheet, the read noise its page's.
	"icx829": {
		Pixel: 8.45, PxLong: 752, PxShort: 580,
		Mono: &Variant{MonoResponse: &icx829Mono, QE: 0.77, ReadNoise: 5.5},
	},
	// Gpixel's front-illuminated GSENSE4040 in QHY's QHY4040 Pro FSI (also
	// SBIG's Aluma AC4040, Moravian's C4-16000): its chart's 74% peak and its
	// table's typical read noise at gain 31, the high-gain channel's, which
	// the 16-bit HDR mode takes. No Dark: no chart.
	"gsense4040": {
		Pixel: 9.0, PxLong: 4096, PxShort: 4096,
		Mono: &Variant{MonoResponse: &gsense4040Mono, Gain: 31, QE: 0.74, ReadNoise: 4.0},
	},
	// Its back-illuminated version in QHY's QHY4040 Pro BSI: QHY's 90% peak
	// (Gpixel's and Diffraction Limited's 95%) and typical read noise at gain
	// 50. No Dark: its chart stops at 20 °C (12.9 e⁻/s).
	"gsense4040bsi": {
		Pixel: 9.0, PxLong: 4096, PxShort: 4096,
		Mono: &Variant{MonoResponse: &gsense4040BSIMono, Gain: 50, QE: 0.90, ReadNoise: 2.3},
	},
	// Moravian's C1-3000 (Pregius, global shutter): its read noise at 24 dB
	// (gain in 0.1 dB, no conversion gain switch), where it levels off.
	// Moravian publishes no QE, so the peak is FLIR's EMVA 1288 report for
	// its Blackfly S BFS-U3-32S4M/C at 525 nm (the colour's green); Sony
	// charts the IMX252 and IMX250 as one curve, so the mono takes the
	// IMX250's.
	"imx252": {
		Pixel: 3.45, PxLong: 2064, PxShort: 1544,
		Colour: &Variant{Response: &imx252, Gain: 240, QE: 0.60, ReadNoise: 2.0},
		Mono:   &Variant{MonoResponse: &imx250Mono, Gain: 240, QE: 0.64, ReadNoise: 2.0},
	},
	// Omegon's veLOX 287 M (a ToupTek rebadge; Omegon and ToupTek publish
	// no figures): Altair's GPCAM3 287M's "approx. 71%" peak and the higher
	// of its two read noises, at no stated gain (Gain 0). FLIR's EMVA 1288
	// report for its BFS-U3-04S2M measures 62% at 525 nm.
	"imx287": {
		Pixel: 6.9, PxLong: 728, PxShort: 544,
		Mono: &Variant{MonoResponse: &imx287Mono, QE: 0.71, ReadNoise: 1.41},
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
	// 81%"); the read noise its chart's at gain 80, for both versions. The
	// mono takes the IMX290's curve (its STARVIS predecessor's), ZWO's
	// ASI462 manual charting only the colour.
	chipIMX462: {
		Pixel: 2.9, PxLong: 1936, PxShort: 1096,
		Colour: &Variant{Response: &imx462, Gain: 80, QE: 0.81, ReadNoise: 1.05},
		Mono:   &Variant{MonoResponse: &imx290Mono, Gain: 80, QE: 0.89, ReadNoise: 1.05},
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
