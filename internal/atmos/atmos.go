// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package atmos models the atmosphere for imaging: airmass, extinction and
// night sky brightness (Krisciunas & Schaefer 1991), with Bortle/SQM darkness.
package atmos

import "math"

const deg = math.Pi / 180

// RefZenithMag is a pristine dark sky (V mag/arcsec²): full sky-glow credit.
const RefZenithMag = 22.0

// BortleMag maps Bortle class to typical zenith sky brightness (V mag/arcsec²,
// mid-range of the usual SQM bands); index 0 (-bortle unset) assumes a dark sky.
var BortleMag = [...]float64{RefZenithMag, 21.9, 21.6, 21.4, 20.85, 19.75, 18.8, 18.25, 17.75, 17.3}

// BortleClass returns the Bortle class whose usual SQM band contains mag.
func BortleClass(mag float64) int {
	for i, lower := range [...]float64{21.75, 21.5, 21.3, 20.4, 19.1, 18.5, 18.0, 17.5} {
		if mag >= lower {
			return i + 1
		}
	}

	return 9
}

// ExtinctionCoeff estimates extinction at 550 nm (the CAMS AOD wavelength, ~V)
// in mag per airmass: Rayleigh scattering (Hayes & Latham 1975 at 0.55 µm,
// matching astrogo's Winkler 2022 tau_R = 0.098) scaled by elevation (scale
// height ~8 km), Chappuis-band ozone (300 DU, sigma ~3.3e-21 cm^2), and
// aerosols (1.086 converts optical depth to magnitudes).
func ExtinctionCoeff(elevation, aod float64) float64 {
	return 0.1066*math.Exp(-elevation/7996) + 0.029 + 1.086*aod
}

// TypicalAOD fills hours past the CAMS forecast range: clean continental air.
const TypicalAOD = 0.1

// Airmass returns the relative air mass at altitude alt in degrees
// (Pickering 2002, good down to the horizon).
func Airmass(alt float64) float64 {
	alt = max(alt, 0)

	return 1 / math.Sin((alt+244/(165+47*math.Pow(alt, 1.1)))*deg)
}

// Extinction returns the fraction of a target's light that survives the
// atmosphere at altitude alt, relative to the zenith, for k mag per airmass.
func Extinction(alt, k float64) float64 {
	return math.Pow(10, -0.4*k*(Airmass(alt)-1))
}

// NanoLamberts converts a surface brightness in V mag/arcsec² to NanoLamberts.
func NanoLamberts(mag float64) float64 { return 34.08 * math.Exp(20.7233-0.92104*mag) }

// MagFromNL converts nanoLamberts back to V mag/arcsec².
func MagFromNL(b float64) float64 { return (20.7233 - math.Log(b/34.08)) / 0.92104 }

// SkyBrightness returns the sky surface brightness in nanoLamberts at a target
// at altitude alt, after Krisciunas & Schaefer (1991): the site's dark zenith
// brightness brightened towards the horizon, plus moonlight scattered from a
// Moon at moonAlt, rho degrees away, with phase angle alpha (0 = full).
func SkyBrightness(zenithNL, k, alt, moonAlt, rho, alpha float64) float64 {
	ks := func(alt float64) float64 { // K&S airmass for scattering paths
		z := math.Sin((90 - alt) * deg)

		return 1 / math.Sqrt(1-0.96*z*z)
	}
	x := ks(alt)
	b := zenithNL * math.Pow(10, -0.4*k*(x-1)) * x
	if moonAlt > 0 {
		moon := math.Pow(10, -0.4*(3.84+0.026*alpha+4e-9*math.Pow(alpha, 4)))
		c := math.Cos(rho * deg)
		scatter := math.Pow(10, 5.36)*(1.06+c*c) + math.Pow(10, 6.15-rho/40)
		b += scatter * moon * math.Pow(10, -0.4*k*ks(moonAlt)) * (1 - math.Pow(10, -0.4*k*x))
	}

	return b
}
