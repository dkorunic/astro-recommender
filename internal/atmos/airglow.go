// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package atmos

// Airglow: the upper atmosphere's own light, about two-thirds of a dark V
// zenith at solar minimum (Benn & Ellison 1998: 145 of 220 S10 at the
// ecliptic and galactic poles), which the Sun's ultraviolet drives, so it
// follows the 11-year cycle: 0.3-0.5 mag between minimum and maximum in V at
// Paranal, La Palma, Mauna Kea and CTIO. The ESO sky model (Noll et al. 2012,
// A&A 543, A92, Table 2) scales each airglow component as
// 1 + m·(S − 129), S the monthly mean 10.7 cm solar radio flux in sfu, m
// 0.0061 for the continuum and 0.0087 for [O I] 557.7, whose V share is
// about a fifth (Patat 2008).
const (
	airglowShare = 0.6   // of the natural 22.0 at the reference direction, where the zodiacal light is above the poles'
	airglowSlope = 0.006 // per sfu, V-weighted between Noll's continuum and [O I] 557.7
	airglowPivot = 129   // sfu, Noll's: the mean of solar cycles 19-23
	// AirglowRefSFU is the solar flux of the natural 22.0 sky: solar
	// minimum, as Falchi et al. (2016) take it, and Noll's model gives 22.0
	// at 90 sfu.
	AirglowRefSFU = 90
	// MinSFU bounds the flux the linear scaling is applied at from below: a
	// solar minimum runs near 65 sfu (Noll's fit spans 95-228).
	MinSFU = 65
	// MaxSFU bounds it from above: a strong maximum's monthly mean.
	MaxSFU = 250
)

// AirglowExcess returns how much brighter (negative: darker) the airglow is,
// in nanoLamberts at the zenith, at a monthly solar flux of sfu than in the
// natural 22.0 sky (AirglowRefSFU); 0 for an unknown flux (0 or less). It is
// an excess over the configured zenith brightness, whose natural part is
// 22.0's whatever the light pollution added to it.
func AirglowExcess(sfu float64) float64 {
	if !(sfu > 0) {
		return 0
	}
	s := min(max(sfu, MinSFU), MaxSFU)
	r := (1 + airglowSlope*(s-airglowPivot)) / (1 + airglowSlope*(AirglowRefSFU-airglowPivot))

	return airglowShare * NanoLamberts(RefZenithMag) * (r - 1)
}
