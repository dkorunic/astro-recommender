// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

import (
	"math"
	"slices"
)

// Galactic light: the Milky Way's unresolved stars and the starlight its dust
// scatters (the diffuse galactic light, 20-30% of it). After the zodiacal
// light it is the natural sky's largest variation across the sky: from about
// 30 S10 at the galactic poles to 900 in the Sagittarius star clouds, against
// 205 for a whole 22.0 mag/arcsec² sky. The tables are Pioneer 10's
// background starlight from beyond 2.8 AU, where the zodiacal light has gone
// (Toller 1981, as Leinert et al. 1998, A&AS 127, 1, Tables 35 and 36 give
// it): blue (440 nm) and red (640 nm) in S10⊙ units every 10° of galactic
// longitude (rows) by galactic latitude (columns), stars brighter than V 6.5
// excluded, as an imaging camera resolves them. Pioneer's ~2° beam smooths
// the star clouds; its spin axis kept it off the cells around the Sun, which
// are 0 here (l 180-240°, b 16-50°).

const galacticRows = 36 // galactic longitude 0, 10, ..., 350°

var galacticB = [...]float64{-80, -70, -60, -50, -40, -30, -24, -20, -16, -12, -8, -4, 0, 4, 8, 12, 16, 20, 24, 30, 40, 50, 60, 70, 80}

var galacticBlue = [galacticRows][len(galacticB)]float64{
	{34, 32, 39, 45, 56, 78, 99, 120, 190, 320, 484, 569, 290, 147, 164, 160, 97, 85, 92, 68, 47, 44, 37, 36, 29},     // l = 0
	{32, 28, 41, 48, 68, 87, 111, 133, 176, 204, 283, 371, 275, 210, 222, 142, 84, 68, 72, 63, 56, 43, 38, 40, 27},    // l = 10
	{30, 33, 41, 49, 53, 76, 96, 124, 176, 221, 224, 223, 182, 111, 77, 81, 75, 71, 68, 71, 50, 44, 36, 32, 29},       // l = 20
	{32, 35, 39, 43, 57, 77, 89, 105, 123, 143, 172, 172, 100, 87, 107, 167, 121, 102, 85, 66, 53, 32, 30, 31, 30},    // l = 30
	{29, 39, 37, 47, 63, 80, 90, 101, 111, 145, 180, 186, 122, 186, 209, 163, 125, 104, 86, 63, 50, 41, 32, 33, 28},   // l = 40
	{33, 32, 35, 49, 56, 88, 88, 124, 131, 186, 219, 256, 151, 169, 175, 155, 133, 102, 90, 67, 44, 38, 33, 27, 27},   // l = 50
	{35, 32, 41, 39, 57, 68, 91, 105, 142, 187, 237, 244, 187, 220, 223, 170, 130, 99, 84, 74, 52, 39, 33, 29, 27},    // l = 60
	{31, 34, 46, 46, 63, 71, 84, 94, 112, 162, 198, 210, 275, 342, 242, 174, 144, 114, 85, 72, 48, 39, 30, 30, 30},    // l = 70
	{28, 35, 36, 41, 46, 64, 81, 103, 113, 163, 230, 231, 219, 216, 220, 169, 139, 96, 80, 61, 41, 35, 36, 33, 31},    // l = 80
	{34, 31, 33, 43, 46, 60, 87, 105, 109, 148, 186, 262, 221, 156, 195, 160, 126, 94, 86, 66, 49, 27, 39, 31, 32},    // l = 90
	{31, 24, 35, 39, 48, 65, 80, 93, 120, 151, 235, 326, 247, 160, 140, 116, 98, 83, 73, 73, 47, 40, 40, 30, 35},      // l = 100
	{26, 34, 35, 37, 44, 73, 74, 95, 125, 157, 205, 224, 177, 153, 136, 124, 84, 74, 75, 61, 44, 39, 34, 28, 31},      // l = 110
	{26, 33, 32, 36, 48, 53, 83, 108, 114, 158, 191, 215, 199, 124, 111, 115, 104, 86, 79, 57, 44, 33, 34, 24, 28},    // l = 120
	{30, 30, 34, 41, 45, 67, 84, 97, 124, 144, 182, 223, 174, 136, 96, 95, 89, 90, 73, 61, 41, 37, 35, 27, 29},        // l = 130
	{27, 30, 35, 45, 41, 64, 78, 95, 116, 129, 150, 133, 132, 138, 96, 91, 101, 92, 70, 60, 51, 37, 33, 28, 30},       // l = 140
	{26, 36, 35, 42, 46, 57, 81, 78, 115, 135, 150, 119, 125, 108, 102, 84, 95, 79, 72, 58, 41, 45, 31, 30, 29},       // l = 150
	{27, 33, 43, 40, 47, 60, 69, 84, 73, 104, 99, 120, 148, 166, 133, 107, 93, 97, 75, 61, 44, 37, 32, 35, 28},        // l = 160
	{30, 32, 34, 44, 46, 61, 77, 79, 60, 63, 77, 108, 195, 171, 142, 129, 99, 87, 81, 56, 51, 44, 40, 35, 29},         // l = 170
	{33, 32, 36, 40, 51, 67, 67, 53, 73, 103, 84, 128, 180, 182, 147, 134, 105, 89, 77, 62, 51, 0, 45, 35, 31},        // l = 180
	{32, 32, 34, 45, 50, 65, 77, 90, 105, 117, 146, 190, 182, 191, 184, 146, 100, 92, 81, 0, 0, 0, 42, 36, 30},        // l = 190
	{28, 30, 31, 42, 56, 70, 97, 119, 130, 139, 156, 221, 209, 199, 186, 154, 112, 100, 0, 0, 0, 0, 45, 36, 29},       // l = 200
	{26, 30, 30, 39, 48, 74, 122, 122, 141, 130, 188, 182, 251, 222, 189, 149, 125, 0, 0, 0, 0, 0, 49, 36, 29},        // l = 210
	{26, 32, 33, 43, 52, 69, 91, 109, 121, 144, 185, 229, 278, 263, 186, 149, 0, 0, 0, 0, 0, 0, 57, 40, 31},           // l = 220
	{27, 30, 36, 38, 46, 57, 79, 109, 131, 208, 254, 255, 272, 261, 193, 143, 0, 0, 0, 0, 0, 72, 53, 42, 32},          // l = 230
	{27, 26, 37, 38, 42, 63, 91, 106, 139, 169, 262, 282, 312, 274, 197, 147, 0, 0, 0, 0, 65, 63, 52, 42, 31},         // l = 240
	{27, 26, 38, 40, 47, 66, 86, 101, 136, 163, 208, 219, 302, 294, 202, 126, 99, 86, 80, 61, 53, 53, 53, 45, 31},     // l = 250
	{28, 33, 33, 41, 52, 85, 113, 115, 143, 197, 288, 258, 239, 207, 169, 131, 111, 87, 75, 72, 56, 45, 55, 43, 30},   // l = 260
	{29, 29, 38, 40, 55, 81, 95, 119, 146, 169, 239, 246, 177, 181, 181, 142, 111, 93, 73, 61, 51, 46, 39, 35, 30},    // l = 270
	{28, 26, 42, 39, 58, 213, 87, 116, 128, 194, 276, 369, 338, 280, 204, 144, 132, 90, 82, 64, 49, 44, 42, 36, 30},   // l = 280
	{29, 25, 45, 44, 55, 68, 94, 107, 140, 197, 258, 351, 631, 441, 227, 159, 114, 95, 84, 64, 51, 41, 39, 40, 31},    // l = 290
	{29, 32, 42, 43, 68, 73, 94, 103, 112, 156, 246, 323, 354, 356, 238, 173, 145, 93, 80, 62, 47, 42, 32, 35, 33},    // l = 300
	{25, 29, 40, 50, 58, 78, 97, 116, 166, 196, 239, 376, 446, 337, 192, 208, 174, 117, 95, 67, 57, 50, 38, 35, 29},   // l = 310
	{25, 29, 38, 48, 62, 74, 103, 135, 176, 242, 292, 306, 248, 251, 197, 175, 141, 122, 95, 74, 52, 64, 35, 30, 25},  // l = 320
	{28, 31, 35, 38, 59, 85, 101, 130, 176, 234, 309, 467, 314, 279, 247, 209, 172, 125, 99, 80, 48, 46, 34, 32, 25},  // l = 330
	{28, 34, 36, 45, 61, 81, 108, 133, 180, 228, 320, 270, 289, 151, 127, 158, 121, 113, 87, 73, 47, 46, 38, 32, 29},  // l = 340
	{30, 35, 32, 51, 58, 82, 114, 156, 204, 281, 379, 318, 278, 251, 208, 162, 168, 146, 106, 76, 48, 42, 37, 33, 31}, // l = 350
}

var galacticRed = [galacticRows][len(galacticB)]float64{
	{42, 48, 52, 57, 67, 106, 143, 152, 245, 461, 801, 1306, 593, 442, 375, 310, 153, 125, 130, 96, 65, 52, 44, 56, 37}, // l = 0
	{38, 40, 62, 63, 78, 99, 153, 181, 233, 337, 563, 734, 537, 508, 444, 246, 148, 135, 110, 79, 81, 56, 44, 44, 34},   // l = 10
	{35, 56, 53, 57, 73, 110, 133, 181, 230, 328, 404, 556, 342, 264, 182, 159, 150, 121, 95, 91, 69, 51, 47, 49, 33},   // l = 20
	{40, 40, 42, 59, 65, 94, 128, 152, 191, 244, 289, 395, 258, 165, 191, 226, 190, 148, 116, 86, 64, 47, 40, 49, 35},   // l = 30
	{41, 51, 48, 52, 76, 88, 130, 148, 165, 224, 307, 359, 200, 333, 339, 242, 185, 139, 123, 85, 58, 48, 42, 44, 34},   // l = 40
	{43, 39, 49, 59, 70, 118, 126, 156, 185, 243, 307, 419, 287, 349, 278, 211, 176, 139, 106, 93, 60, 47, 44, 37, 33},  // l = 50
	{40, 38, 54, 51, 79, 109, 123, 138, 176, 249, 335, 411, 338, 361, 306, 221, 166, 128, 116, 84, 60, 47, 41, 32, 36},  // l = 60
	{37, 42, 50, 54, 70, 96, 116, 123, 166, 223, 290, 292, 403, 488, 319, 218, 183, 135, 107, 85, 69, 51, 35, 43, 36},   // l = 70
	{39, 42, 44, 63, 55, 86, 108, 133, 158, 215, 310, 323, 284, 309, 296, 216, 172, 118, 103, 76, 53, 42, 44, 38, 37},   // l = 80
	{47, 45, 45, 59, 56, 78, 90, 123, 147, 186, 259, 343, 330, 240, 265, 206, 151, 113, 98, 78, 56, 52, 44, 39, 38},     // l = 90
	{46, 45, 52, 42, 65, 83, 97, 118, 164, 188, 280, 394, 342, 270, 212, 165, 138, 98, 82, 79, 58, 47, 45, 42, 38},      // l = 100
	{41, 50, 45, 46, 57, 79, 88, 118, 146, 182, 244, 280, 248, 216, 189, 166, 122, 99, 94, 75, 58, 49, 51, 33, 37},      // l = 110
	{40, 41, 44, 50, 60, 73, 101, 134, 149, 192, 219, 248, 276, 202, 154, 157, 120, 113, 100, 77, 49, 43, 42, 36, 33},   // l = 120
	{38, 44, 35, 50, 53, 81, 92, 107, 134, 172, 217, 273, 250, 205, 146, 132, 123, 114, 94, 72, 55, 41, 40, 34, 30},     // l = 130
	{35, 45, 33, 51, 57, 81, 93, 104, 139, 160, 186, 172, 197, 198, 149, 119, 132, 104, 88, 62, 62, 60, 43, 36, 33},     // l = 140
	{35, 40, 42, 63, 58, 64, 97, 109, 142, 165, 189, 169, 188, 165, 154, 126, 128, 108, 84, 77, 58, 55, 40, 35, 33},     // l = 150
	{41, 42, 51, 47, 64, 73, 83, 99, 88, 142, 135, 162, 200, 214, 181, 135, 109, 126, 93, 81, 57, 58, 37, 39, 34},       // l = 160
	{40, 41, 38, 53, 79, 75, 92, 99, 90, 114, 122, 170, 238, 213, 176, 156, 127, 113, 109, 69, 54, 59, 51, 45, 36},      // l = 170
	{37, 45, 46, 53, 70, 88, 85, 105, 120, 138, 112, 143, 219, 230, 191, 157, 135, 103, 97, 73, 71, 0, 52, 50, 36},      // l = 180
	{31, 46, 43, 47, 70, 83, 90, 92, 123, 145, 187, 214, 225, 254, 216, 179, 127, 113, 106, 0, 0, 0, 56, 52, 36},        // l = 190
	{34, 40, 37, 50, 60, 76, 105, 124, 154, 176, 211, 254, 271, 238, 203, 179, 129, 127, 0, 0, 0, 0, 56, 50, 36},        // l = 200
	{36, 40, 48, 59, 61, 101, 129, 147, 169, 163, 225, 231, 293, 254, 202, 169, 148, 0, 0, 0, 0, 0, 57, 50, 35},         // l = 210
	{36, 36, 41, 49, 60, 81, 100, 130, 146, 158, 210, 250, 309, 283, 204, 176, 0, 0, 0, 0, 0, 0, 60, 51, 41},            // l = 220
	{35, 37, 39, 43, 58, 77, 96, 131, 156, 230, 276, 285, 296, 282, 220, 157, 0, 0, 0, 0, 0, 66, 58, 51, 41},            // l = 230
	{30, 38, 42, 46, 58, 72, 106, 104, 163, 210, 279, 329, 355, 310, 246, 160, 0, 0, 0, 0, 66, 70, 68, 54, 36},          // l = 240
	{33, 36, 39, 48, 57, 75, 94, 115, 160, 205, 267, 314, 344, 339, 252, 159, 139, 119, 114, 73, 63, 72, 65, 53, 34},    // l = 250
	{38, 40, 42, 58, 58, 101, 124, 123, 185, 239, 314, 302, 283, 253, 207, 194, 142, 105, 107, 71, 65, 67, 58, 43, 34},  // l = 260
	{37, 36, 44, 59, 69, 86, 115, 143, 171, 210, 268, 331, 256, 248, 227, 190, 146, 108, 95, 77, 64, 52, 50, 45, 32},    // l = 270
	{37, 32, 48, 49, 70, 230, 113, 124, 149, 212, 329, 445, 427, 383, 261, 191, 162, 119, 105, 90, 61, 53, 50, 43, 34},  // l = 280
	{38, 41, 47, 50, 68, 88, 109, 141, 161, 256, 319, 464, 760, 516, 301, 210, 152, 124, 108, 83, 60, 48, 46, 47, 39},   // l = 290
	{41, 42, 44, 48, 69, 90, 123, 132, 152, 223, 312, 481, 493, 436, 319, 236, 154, 127, 116, 86, 64, 55, 42, 46, 39},   // l = 300
	{44, 35, 44, 59, 67, 107, 123, 151, 208, 251, 348, 535, 584, 505, 324, 271, 204, 149, 117, 91, 70, 60, 46, 41, 35},  // l = 310
	{45, 41, 49, 58, 75, 95, 124, 164, 207, 309, 420, 517, 479, 401, 304, 226, 189, 156, 118, 94, 68, 63, 45, 38, 36},   // l = 320
	{43, 39, 47, 55, 78, 96, 127, 164, 231, 307, 446, 736, 530, 440, 359, 286, 231, 159, 126, 102, 64, 61, 45, 37, 39},  // l = 330
	{41, 43, 47, 55, 75, 106, 143, 178, 240, 329, 487, 478, 526, 331, 219, 241, 190, 131, 124, 102, 78, 54, 49, 43, 42}, // l = 340
	{38, 41, 53, 68, 88, 105, 151, 189, 260, 366, 575, 617, 460, 526, 382, 415, 283, 161, 145, 108, 68, 55, 43, 45, 41}, // l = 350
}

// galacticRedShare places V (550 nm) between the blue and red tables, which
// are in S10⊙ units (a source of solar colour reads the same in both), by
// wavelength in log brightness: 550 nm is 0.55 of the way from 440 to 640.
// The galactic light is redder than the Sun, red reading 1.3 times blue at
// high latitude, and more in the dusty plane.
const galacticRedShare = (550.0 - 440) / (640 - 440)

// galacticRefS10 is the galactic light the site's zenith brightness (-sqm,
// -bortle or the 22.0 default) is taken to include: the median of the V table
// over the sky by solid angle, what a zenith sees over the nights of a year.
// GalacticExcess is measured from it.
const galacticRefS10 = 81.0

var (
	galacticV     = galacticTable()
	galacticRefNL = s10NL(galacticRefS10)
	// galacticPole is the mean of the ±80° columns over longitude, the value
	// interpolation reaches at the galactic poles.
	galacticPole = [2]float64{galacticColumnMean(0), galacticColumnMean(len(galacticB) - 1)}
	// GalacticMinNL is the darkest galactic light in nanoLamberts, the least
	// any direction has: with ZodiacalPoleNL, a floor for the sky.
	GalacticMinNL = s10NL(galacticMin())
)

// galacticTable is the V table: blue and red combined by galacticRedShare,
// and Pioneer's gaps filled along each row's latitude, log-linearly between
// the nearest measured cells (the galactic light falls off smoothly away
// from the plane there, towards the anticentre).
func galacticTable() [galacticRows][len(galacticB)]float64 {
	var v [galacticRows][len(galacticB)]float64
	for i := range v {
		for j := range v[i] {
			if galacticBlue[i][j] > 0 {
				v[i][j] = math.Pow(galacticBlue[i][j], 1-galacticRedShare) * math.Pow(galacticRed[i][j], galacticRedShare)
			}
		}
		for j := range v[i] {
			if galacticBlue[i][j] > 0 {
				continue
			}
			lo, hi := j-1, j+1
			for galacticBlue[i][lo] == 0 {
				lo--
			}
			for galacticBlue[i][hi] == 0 {
				hi++
			}
			f := (galacticB[j] - galacticB[lo]) / (galacticB[hi] - galacticB[lo])
			v[i][j] = math.Exp(math.Log(v[i][lo])*(1-f) + math.Log(v[i][hi])*f)
		}
	}

	return v
}

func galacticColumnMean(j int) float64 {
	sum := 0.0
	for i := range galacticV {
		sum += galacticV[i][j]
	}

	return sum / galacticRows
}

func galacticMin() float64 {
	m := math.Inf(1)
	for i := range galacticV {
		m = min(m, slices.Min(galacticV[i][:]))
	}

	return min(m, galacticPole[0], galacticPole[1])
}

// s10NL converts S10 units to nanoLamberts (s10Mag mag/arcsec² for one).
func s10NL(v float64) float64 { return NanoLamberts(s10Mag - 2.5*math.Log10(v)) }

// Galactic returns the galactic light's V surface brightness in nanoLamberts
// at galactic longitude l and latitude b (degrees), by bilinear
// interpolation in the V table, towards the pole value past ±80°.
func Galactic(l, b float64) float64 {
	l = math.Mod(l, 360)
	if l < 0 {
		l += 360
	}
	b = max(-90, min(90, b))
	i := min(int(l/10), galacticRows-1)
	fi := l/10 - float64(i)

	return s10NL(galacticRow(i, b)*(1-fi) + galacticRow((i+1)%galacticRows, b)*fi)
}

// galacticRow interpolates row i of the V table at latitude b.
func galacticRow(i int, b float64) float64 {
	last := len(galacticB) - 1
	switch {
	case b > galacticB[last]:
		f := (b - galacticB[last]) / (90 - galacticB[last])

		return galacticV[i][last]*(1-f) + galacticPole[1]*f
	case b < galacticB[0]:
		f := (galacticB[0] - b) / (galacticB[0] + 90)

		return galacticV[i][0]*(1-f) + galacticPole[0]*f
	}
	j, fj := bracket(galacticB[:], b)

	return galacticV[i][j]*(1-fj) + galacticV[i][j+1]*fj
}

// GalacticExcess returns how much brighter, in nanoLamberts, the galactic
// light makes the sky at galactic (l, b) than the sky median the site's
// zenith brightness already includes; negative away from the Milky Way. On a
// pristine sky the galactic poles are 0.3 mag/arcsec² darker, the plane 0.5
// to 1.1 brighter, the Sagittarius star clouds 1.7.
func GalacticExcess(l, b float64) float64 {
	return Galactic(l, b) - galacticRefNL
}
