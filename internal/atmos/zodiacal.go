// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import "math"

// Zodiacal light: sunlight scattered by interplanetary dust, laid out around
// the Sun along the ecliptic. It is the one part of the natural sky that
// varies across the sky by more than a few tenths of a magnitude: Leinert et
// al. (1998, A&AS 127, 1) Table 17 spans a factor of 160 from the ecliptic
// pole to the region nearest the Sun, and at night (elongation above 90°)
// still a factor of 3 from the pole to the ecliptic. The table is theirs
// (transcribed as in astrogo's skybrightness package, MIT, (c) 2026 Rener
// Castro): the brightness at 500 nm in 1e-8 W m⁻² sr⁻¹ µm⁻¹ by the
// ecliptic longitude from the Sun (rows) and the ecliptic latitude
// (columns), 0 where it leaves the solar vicinity blank. The heliocentric
// (±7%) and seasonal (±10% at high latitude) factors are left out.

var (
	zodiacalLon = [...]float64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 60, 75, 90, 105, 120, 135, 150, 165, 180}
	zodiacalLat = [...]float64{0, 5, 10, 15, 20, 25, 30, 45, 60, 75}
)

var zodiacalTable = [len(zodiacalLon)][len(zodiacalLat)]float64{
	{0, 0, 0, 3140, 1610, 985, 640, 275, 150, 100},
	{0, 0, 0, 2940, 1540, 945, 625, 271, 150, 100},
	{0, 0, 4740, 2470, 1370, 865, 590, 264, 148, 100},
	{11500, 6780, 3440, 1860, 1110, 755, 525, 251, 146, 100},
	{6400, 4480, 2410, 1410, 910, 635, 454, 237, 141, 99},
	{3840, 2830, 1730, 1100, 749, 545, 410, 223, 136, 97},
	{2480, 1870, 1220, 845, 615, 467, 365, 207, 131, 95},
	{1650, 1270, 910, 680, 510, 397, 320, 193, 125, 93},
	{1180, 940, 700, 530, 416, 338, 282, 179, 120, 92},
	{910, 730, 555, 442, 356, 292, 250, 166, 116, 90},
	{505, 442, 352, 292, 243, 209, 183, 134, 104, 86},
	{338, 317, 269, 227, 196, 172, 151, 116, 93, 82},
	{259, 251, 225, 193, 166, 147, 132, 104, 86, 79},
	{212, 210, 197, 170, 150, 133, 119, 96, 82, 77},
	{188, 186, 177, 154, 138, 125, 113, 90, 77, 74},
	{179, 178, 166, 147, 134, 122, 110, 90, 77, 73},
	{179, 178, 165, 148, 137, 127, 116, 96, 79, 72},
	{196, 192, 179, 165, 151, 141, 131, 104, 82, 72},
	{230, 212, 195, 178, 163, 148, 134, 105, 83, 72},
}

// zodiacalPole is the table's value at the ecliptic pole (β = 90°), which
// Leinert et al. quote beside the grid.
const zodiacalPole = 77.0

// s10 converts the table's unit to S10 (tenth-magnitude solar-type stars
// per square degree): 1 S10 is 1.28e-8 W m⁻² sr⁻¹ µm⁻¹ at 500 nm and
// s10Mag = 27.78 mag/arcsec² in V (Leinert et al. 1998, Table 2).
const s10, s10Mag = 1.28, 27.78

// The reference direction, whose zodiacal light the site's zenith
// brightness (-sqm, -bortle or the 22.0 default) is taken to include: 135°
// from the Sun at ecliptic latitude 45°, a typical zenith for a mid-latitude
// site through the night (the zenith's latitude runs over 45° ± 23.4° and
// its elongation from 90° at dusk to 180° at midnight), where the table
// reads 90, near its median away from the Sun. Falchi et al.'s (2016)
// natural 22.0 mag/arcsec² and an SQM reading both include the zodiacal
// light of such a zenith. ZodiacalExcess is measured from here.
const (
	ZodiacalRefLon = 135.0 // degrees of ecliptic longitude from the Sun
	ZodiacalRefLat = 45.0  // degrees of ecliptic latitude
)

var (
	zodiacalRefNL = Zodiacal(ZodiacalRefLon, ZodiacalRefLat)
	// ZodiacalPoleNL is the zodiacal light at the ecliptic pole in
	// nanoLamberts, the least any direction has: a floor for the sky.
	ZodiacalPoleNL = Zodiacal(0, 90)
)

// Zodiacal returns the zodiacal light's V surface brightness in nanoLamberts
// dLon degrees of ecliptic longitude from the Sun (either way) at ecliptic
// latitude lat (either sign), by bilinear interpolation in Leinert's table.
// In the solar vicinity it leaves blank (below 15° elongation) the nearest
// tabulated latitude stands in: no night-time direction falls there.
func Zodiacal(dLon, lat float64) float64 {
	d := math.Mod(math.Abs(dLon), 360)
	if d > 180 {
		d = 360 - d
	}
	b := math.Mod(math.Abs(lat), 360)
	if b > 180 {
		b = 360 - b
	}
	if b > 90 {
		b = 180 - b
	}
	i, fi := bracket(zodiacalLon[:], d)
	v := zodiacalRow(i, b)*(1-fi) + zodiacalRow(i+1, b)*fi

	return NanoLamberts(s10Mag - 2.5*math.Log10(v/s10))
}

// zodiacalRow interpolates row i of the table at latitude b, the pole value
// standing past the last column; a blank cell takes the row's first filled one.
func zodiacalRow(i int, b float64) float64 {
	cell := func(j int) float64 {
		if j == len(zodiacalLat) {
			return zodiacalPole
		}
		for zodiacalTable[i][j] == 0 {
			j++
		}

		return zodiacalTable[i][j]
	}
	j, fj := bracket(zodiacalLat[:], b)
	if last := len(zodiacalLat) - 1; b > zodiacalLat[last] { // towards the pole
		j, fj = last, (b-zodiacalLat[last])/(90-zodiacalLat[last])
	}

	return cell(j)*(1-fj) + cell(j+1)*fj
}

// bracket returns the index of the last axis point at or below v (at most
// the second to last) and v's fraction of the way to the next point.
func bracket(axis []float64, v float64) (int, float64) {
	i := len(axis) - 2
	for i > 0 && axis[i] > v {
		i--
	}

	return i, max(0, min(1, (v-axis[i])/(axis[i+1]-axis[i])))
}

// ZodiacalExcess returns how much brighter, in nanoLamberts, the zodiacal
// light makes the sky in a direction than in the reference direction the
// site's zenith brightness already includes; negative towards the ecliptic
// pole. On the ecliptic 90° from the Sun it is +0.5 mag/arcsec² on a
// pristine sky, at the pole −0.06.
func ZodiacalExcess(dLon, lat float64) float64 {
	return Zodiacal(dLon, lat) - zodiacalRefNL
}
