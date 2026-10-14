// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package atmos models the atmosphere for imaging: airmass, extinction and
// night sky brightness (Krisciunas & Schaefer 1991; moonlight after Winkler
// 2022), with Bortle/SQM darkness.
package atmos

import "math"

const deg = math.Pi / 180

// exp10 is 10^x. math.Pow's general path (Frexp, Log, Exp, Ldexp and the
// special cases) costs about three times a bare Exp, and scoring calls this
// several times per observable target-minute.
func exp10(x float64) float64 { return math.Exp(x * math.Ln10) }

// RefZenithMag is a pristine dark sky (V mag/arcsec²): full sky-glow credit.
const RefZenithMag = 22.0

// MinSQM and MaxSQM bound a plausible zenith sky brightness; both the -sqm
// flag and the DarkSkySites lookup reject values outside them.
const (
	MinSQM = 15.0
	MaxSQM = 23.0
)

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
// in mag per airmass: RayleighExtinction, OzoneExtinction and AerosolExtinction.
func ExtinctionCoeff(elevation, aod float64) float64 {
	return RayleighExtinction(elevation) + OzoneExtinction + AerosolExtinction(aod)
}

// RayleighExtinction is the Rayleigh scattering part of ExtinctionCoeff:
// Hayes & Latham (1975) at 0.55 µm (matching astrogo's Winkler 2022 tau_R =
// 0.098) scaled by elevation (m) over a scale height of ~8 km. It goes as
// λ⁻⁴ (BandSky), so a filter band's extinction scales it.
func RayleighExtinction(elevation float64) float64 { return 0.1066 * math.Exp(-elevation/7996) }

// OzoneExtinction is the Chappuis-band ozone part of ExtinctionCoeff for a
// 300 DU column (sigma ~3.3e-21 cm² at 550 nm). The band peaks at 600 nm
// and is within 0.02 mag/airmass of this at every nebula line, so a filter
// band keeps it as is.
const OzoneExtinction = 0.029

// AerosolExtinction is the aerosol part of ExtinctionCoeff for an optical
// depth aod at 550 nm (1.086 converts optical depth to magnitudes). It goes
// as λ^-angstrom (BandSky).
func AerosolExtinction(aod float64) float64 { return 1.086 * aod }

// SplitExtinction splits a fixed total coefficient k (-extinction, or its
// default without an elevation) into its Rayleigh and aerosol parts for a
// filter band's scaling: ozone first, then Rayleigh up to its sea-level
// value, the rest aerosol.
func SplitExtinction(k float64) (float64, float64) {
	k = max(0, k-OzoneExtinction)
	rayleigh := min(k, RayleighExtinction(0))

	return rayleigh, k - rayleigh
}

// TypicalAOD fills hours past the CAMS forecast range: clean continental air.
const TypicalAOD = 0.1

// Airmass returns the relative air mass at altitude alt in degrees
// (Pickering 2002, good down to the horizon).
func Airmass(alt float64) float64 {
	alt = max(alt, 0)

	return 1 / math.Sin((alt+244/(165+47*math.Exp(1.1*math.Log(alt))))*deg) // Exp(Log) is Pow without its overhead
}

// Extinction returns the fraction of a target's light that survives the
// atmosphere at altitude alt, relative to the zenith, for k mag per airmass.
func Extinction(alt, k float64) float64 {
	return Transmission(Airmass(alt), k)
}

// Transmission is Extinction at a given airmass, for callers that compare
// several coefficients at one altitude.
func Transmission(airmass, k float64) float64 {
	return exp10(-0.4 * k * (airmass - 1))
}

// meanMoonDist is the Moon's mean distance in Earth radii, the one the
// Moon's magnitude law assumes.
const meanMoonDist = 60.27

// NanoLamberts converts a surface brightness in V mag/arcsec² to NanoLamberts.
func NanoLamberts(mag float64) float64 { return 34.08 * math.Exp(20.7233-0.92104*mag) }

// MagFromNL converts nanoLamberts back to V mag/arcsec².
func MagFromNL(b float64) float64 { return (20.7233 - math.Log(b/34.08)) / 0.92104 }

// SkyBrightness returns the sky surface brightness in nanoLamberts at a target
// at altitude alt: the site's dark zenith brightness brightened towards the
// horizon (Krisciunas & Schaefer 1991), plus moonlight scattered from a Moon
// at moonAlt, rho degrees away, with phase angle alpha (0 = full) and
// moonDist Earth radii away (Winkler 2022), for extinction k split as
// SplitExtinction does.
func SkyBrightness(zenithNL, k, alt, moonAlt, rho, alpha, moonDist float64) float64 {
	ray, aer := SplitExtinction(k)

	return SkyBrightnessLit(zenithNL, k, alt, rho, MoonLight(k, ray, aer, moonAlt, alpha, moonDist))
}

// ks is K&S's airmass for scattering paths.
func ks(alt float64) float64 {
	z := math.Sin((90 - alt) * deg)

	return 1 / math.Sqrt(1-0.96*z*z)
}

// Scattered moonlight follows Winkler's (2022, MNRAS 514, 208) revised
// simplified scattering model: the Moon's beam scattered once in a uniform
// layer, at each height dimmed only by the air above it (K&S dim it by the
// whole column, which underestimates the moonlit sky, most for a low Moon),
// the scattered light dimmed on its way down, into a normalised phase
// function of molecules (Rayleigh) and aerosols (Henyey-Greenstein) weighted
// by their optical depths; a fitted offset carries the albedo and multiple
// scattering. In V at Sutherland it reproduces the moonlit sky within about
// 0.05-0.1 mag.
// ponytail: g and δ are Winkler's clean-site (Sutherland, τ = 0.14) fit;
// aerosols range over 0.5 < g < 0.8 (Andrews et al. 2006), so under heavy
// aerosol the forward peak is likely too narrow and the sky far from the
// Moon somewhat too dark. A fit at a hazy site would replace them.
const (
	// moonChi is the depolarisation term of the Rayleigh phase function
	// (Bucholtz 1995), K&S's 1.06 + cos²ρ shape.
	moonChi = 0.0148
	// moonG is the aerosol asymmetry parameter Winkler fitted.
	moonG = 0.8
	// moonDelta is Winkler's V-band offset log10(albedo × multiple
	// scattering), Table 4.
	moonDelta = 0.078
	// arcsec2PerSr converts a radiance per steradian to one per arcsec².
	arcsec2PerSr = (180 / math.Pi * 3600) * (180 / math.Pi * 3600)
	// tauPerMag converts an extinction coefficient in mag/airmass to an
	// optical depth: 0.4 ln 10.
	tauPerMag = 0.4 * math.Ln10
	// moonPR and moonPM are the phase functions' constant factors.
	moonPR = 3 * (1 - moonChi) / (4 * (1 + 2*moonChi)) / (4 * math.Pi)
	moonRC = (1 + 3*moonChi) / (1 - moonChi)
	moonPM = (1 - moonG*moonG) / (4 * math.Pi)
)

// moonFullNL is the full Moon's V flux (magnitude −12.73) as nanoLamberts
// over one arcsec².
var moonFullNL = NanoLamberts(-12.73)

// Moon is the target-independent part of a minute's scattered moonlight;
// the zero value is a Moon below the horizon.
type Moon struct {
	NL   float64 // the Moon's light above the atmosphere × 10^moonDelta, nanoLamberts per steradian of phase function
	X    float64 // its scattering airmass
	Fade float64 // its beam's transmission through the whole column, e^(−τX)
	Ray  float64 // the Rayleigh share of the scattering optical depth
}

// MoonLight returns the Moon's part of a minute's scattered moonlight for
// extinction k mag/airmass, of which kRayleigh is Rayleigh scattering and
// kAerosol aerosol; the zero Moon while it is down. Scoring computes it once
// per grid minute rather than per target-minute.
func MoonLight(k, kRayleigh, kAerosol, moonAlt, alpha, moonDist float64) Moon {
	if moonAlt <= 0 {
		return Moon{}
	}
	// The Moon's V magnitude (Allen, as K&S use it), −12.73 at full and the
	// mean distance; its light goes as distance⁻², up to 13% either way.
	r := meanMoonDist / moonDist
	nl := moonFullNL * exp10(-0.4*(0.026*alpha+4e-9*alpha*alpha*alpha*alpha)) * r * r
	// Opposition surge, which the magnitude law leaves out (K&S p. 1035):
	// 35% brighter at full, tapering linearly to none at 7°, as in
	// Thorstensen's skycalc.
	if alpha < 7 {
		nl *= 1.35 - 0.05*alpha
	}
	ray := 1.0
	if s := kRayleigh + kAerosol; s > 0 {
		ray = kRayleigh / s
	}
	x := ks(moonAlt)

	return Moon{NL: nl * exp10(moonDelta) / arcsec2PerSr, X: x, Fade: math.Exp(-tauPerMag * k * x), Ray: ray}
}

// scatter returns the moonlight m scatters to a target at scattering airmass
// x, rho degrees from the Moon, for extinction k: Rayleigh and aerosol parts.
func (m Moon) scatter(k, x, rho float64) (float64, float64) {
	// Winkler's gradation function: the single scattering integrated down
	// the line of sight, x (e^(−τx) − e^(−τX)) / (X − x), whose limit at
	// X = x is τ x e^(−τx).
	tau := tauPerMag * k
	e := math.Exp(-tau * x)
	var gamma float64
	if d := m.X - x; math.Abs(d) < 1e-6 {
		gamma = x * tau * e * (1 - tau*d/2)
	} else {
		gamma = x * (e - m.Fade) / d
	}
	c := math.Cos(rho * deg)
	t := 1 + moonG*moonG - 2*moonG*c
	base := m.NL * gamma

	return base * m.Ray * moonPR * (moonRC + c*c), base * (1 - m.Ray) * moonPM / (t * math.Sqrt(t))
}

// SkyBrightnessLit is SkyBrightness with the Moon's part m = MoonLight(...)
// precomputed.
func SkyBrightnessLit(zenithNL, k, alt, rho float64, m Moon) float64 {
	x := ks(alt)
	b := zenithNL * exp10(-0.4*k*(x-1)) * x
	if m.NL > 0 {
		rayleigh, mie := m.scatter(k, x, rho)
		b += rayleigh + mie
	}

	return b
}

// SkyParts is SkyBrightnessLit split by spectrum: the glow (airglow and light
// pollution, the dark-sky term) and the moonlight scattered by molecules
// (Rayleigh) and by aerosols (Mie). Their sum is SkyBrightnessLit; BandSky
// says how much of each a filter passes.
func SkyParts(zenithNL, k, alt, rho float64, m Moon) (float64, float64, float64) {
	x := ks(alt)
	glow := zenithNL * exp10(-0.4*k*(x-1)) * x
	if m.NL <= 0 {
		return glow, 0, 0
	}
	rayleigh, mie := m.scatter(k, x, rho)

	return glow, rayleigh, mie
}

// vCenter and VWidth are the V band's effective wavelength and FWHM in nm
// (Bessell 1990), the band every sky brightness here is measured in;
// VWidth is exported for the sub-exposure estimate's photon zero point.
const vCenter, VWidth = 551.0, 88.0

// angstrom is the aerosol scattering's Ångström exponent: aerosol optical
// depth goes as λ^-1.3 for typical continental aerosol.
const angstrom = 1.3

// BandSky returns the fractions of SkyParts' glow, Rayleigh and Mie
// brightness that a filter band width nm wide at center nm passes: its share
// of the V band's width, with the moonlight scattered as λ⁻⁴ (Rayleigh) and
// λ^-angstrom (Mie) relative to V, so a band at [O III] 500.7 nm passes 2.9
// times the Rayleigh moonlight per nm that one at Hα 656.3 nm does.
// ponytail: moonlight is taken as flat per nm before scattering: the Moon is
// 0.25 mag redder in B−V than the Sun, which roughly cancels the Sun's fall
// from 500 to 670 nm (within ~7%). The glow is flat too: airglow and the
// lamps' mix (sodium, white LEDs) vary by site, unknown here; a per-site
// spectrum would replace the first return.
func BandSky(center, width float64) (float64, float64, float64) {
	f := width / VWidth
	rayleigh, mie := BandScale(center)

	return f, f * rayleigh, f * mie
}

// CameraLo and CameraHi (nm) bound an unfiltered camera's band (UV/IR cut),
// against which BroadSky takes a broadband filter's passbands; the
// unfiltered weight's V sky stands for it.
const (
	CameraLo = 400.0
	CameraHi = 700.0
)

// BroadSky returns the fractions of SkyParts' glow, Rayleigh and Mie
// brightness a broadband filter's passband [lo, hi] nm passes against the
// unfiltered CameraLo–CameraHi: its share of the camera's nm, the moonlight
// weighted by each scattering's λ dependence (BandMean), so cutting the
// blue cuts more of the Rayleigh moonlight. The camera band itself is 1, 1, 1.
func BroadSky(lo, hi float64) (float64, float64, float64) {
	f := (hi - lo) / (CameraHi - CameraLo)
	r, m := BandMean(lo, hi)
	cr, cm := BandMean(CameraLo, CameraHi)

	return f, f * r / cr, f * m / cm
}

// BandMean returns the means of BandScale's Rayleigh and Mie factors over
// [lo, hi] nm.
func BandMean(lo, hi float64) (float64, float64) {
	return powMean(lo, hi, -4), powMean(lo, hi, -angstrom)
}

// powMean is the mean of (λ/vCenter)^n over [lo, hi] nm.
func powMean(lo, hi, n float64) float64 {
	a, b := lo/vCenter, hi/vCenter

	return vCenter * (math.Pow(b, n+1) - math.Pow(a, n+1)) / (n + 1) / (hi - lo)
}

// BandScale returns how Rayleigh and Mie scattering at center nm compare
// with the V band's: (λ/551)⁻⁴ and (λ/551)^-angstrom. BandSky's moonlight
// shares and a filter band's extinction (RayleighExtinction and
// AerosolExtinction scaled by these) both use them.
func BandScale(center float64) (float64, float64) {
	x := center / vCenter

	return math.Pow(x, -4), math.Pow(x, -angstrom)
}
