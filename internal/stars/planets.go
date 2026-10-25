// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package stars

import (
	"math"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
)

// planet holds JPL's Keplerian elements of one planet for 1800-2050
// (Standish, "Approximate Positions of the Planets", Table 1: the mean
// ecliptic and equinox of J2000) and their rates per Julian century: the
// semi-major axis (au), eccentricity, inclination, mean longitude, longitude
// of perihelion and of the ascending node (degrees), then mag, its V
// magnitude at r = Δ = 1 au and no phase. Its fields are in the table's
// order, which the literals follow, not alignment's.
type planet struct { // betteralign:ignore
	name                   string
	a, e, i, l, peri, node float64
	da, de, di, dl, dp, dn float64
	mag                    float64
	phase                  func(i float64) float64 // the phase angle's term, degrees
}

// The planets that reach V 4: Uranus' 5.3 at best does not. Magnitudes are
// Meeus' (Astronomical Algorithms, ch. 41, after the 1984 Astronomical
// Almanac); Saturn's rings come from its tilt (saturnRings).
var planets = [...]planet{
	{
		"Mercury", 0.38709927, 0.20563593, 7.00497902, 252.25032350, 77.45779628, 48.33076593,
		0.00000037, 0.00001906, -0.00594749, 149472.67411175, 0.16047689, -0.12534081,
		-0.42, func(i float64) float64 { return 0.0380*i - 0.000273*i*i + 0.000002*i*i*i },
	},
	{
		"Venus", 0.72333566, 0.00677672, 3.39467605, 181.97909950, 131.60246718, 76.67984255,
		0.00000390, -0.00004107, -0.00078890, 58517.81538729, 0.00268329, -0.27769418,
		-4.40, func(i float64) float64 { return 0.0009*i + 0.000239*i*i - 0.00000065*i*i*i },
	},
	{
		"Mars", 1.52371034, 0.09339410, 1.84969142, -4.55343205, -23.94362959, 49.55953891,
		0.00001847, 0.00007882, -0.00813131, 19140.30268499, 0.44441088, -0.29257343,
		-1.52, func(i float64) float64 { return 0.016 * i },
	},
	{
		"Jupiter", 5.20288700, 0.04838624, 1.30439695, 34.39644051, 14.72847983, 100.47390909,
		-0.00011607, -0.00013253, -0.00183714, 3034.74612775, 0.21252668, 0.20469106,
		-9.40, func(i float64) float64 { return 0.005 * i },
	},
	{
		"Saturn", 9.53667594, 0.05386179, 2.48599187, 49.95424423, 92.59887831, 113.66242448,
		-0.00125060, -0.00050991, 0.00193609, 1222.49362201, -0.41897216, -0.28867794,
		-8.88, func(float64) float64 { return 0 },
	},
}

// earthMoon is Table 1's Earth-Moon barycentre, from which the planets are
// seen (the Earth is within 4700 km of it: 13″ of Mars at its closest).
var earthMoon = planet{
	name: "Earth", a: 1.00000261, e: 0.01671123, i: -0.00001531, l: 100.46457166, peri: 102.93768193,
	da: 0.00000562, de: -0.00004392, di: -0.01294668, dl: 35999.37244981, dp: 0.32327364,
}

// Saturn's north pole, J2000 RA and Dec in degrees (IAU), across which its
// rings lie.
const saturnPoleRA, saturnPoleDec = 40.589, 83.537

// j2000Obliquity is the J2000 obliquity of the ecliptic, as Standish's
// elements are in that ecliptic, degrees.
const j2000Obliquity = 23.43928

// Planets returns the planets brighter than V 4 at t as Stars, with their
// geocentric J2000 positions (light time left out: under a minute of arc).
func Planets(t time.Time) []Star {
	cy := astro.DaysJ2000(t) / 36525
	earth := earthMoon.helio(cy)
	var out []Star
	for _, p := range planets {
		h := p.helio(cy)
		g := [3]float64{h[0] - earth[0], h[1] - earth[1], h[2] - earth[2]}
		r, delta, R := norm(h), norm(g), norm(earth)
		phase := math.Acos(max(-1, min(1, (r*r+delta*delta-R*R)/(2*r*delta)))) / deg
		ra, dec := equatorial(g)
		mag := p.mag + 5*math.Log10(r*delta) + p.phase(phase)
		if p.name == "Saturn" {
			mag += saturnRings(ra, dec)
		}
		if mag <= maxMag {
			out = append(out, Star{Name: p.name, Mag: math.Round(mag*100) / 100, RA: ra, Dec: dec}) // the catalogue's precision
		}
	}

	return out
}

// saturnRings is the rings' term of Saturn's magnitude, seen from ra, dec
// (J2000 degrees, the Earth to Saturn): −2.60 sin|B| + 1.25 sin²B for the
// tilt B of the rings to the line of sight, 0 edge on.
func saturnRings(ra, dec float64) float64 {
	pole, d := astro.Unit(saturnPoleRA, saturnPoleDec), astro.Unit(ra, dec)
	sinB := math.Abs(pole[0]*d[0] + pole[1]*d[1] + pole[2]*d[2])

	return -2.60*sinB + 1.25*sinB*sinB
}

// helio returns the planet's heliocentric position in au in the J2000
// equatorial frame, cy Julian centuries from J2000.
func (p planet) helio(cy float64) [3]float64 {
	a, e := p.a+p.da*cy, p.e+p.de*cy
	incl, l := (p.i+p.di*cy)*deg, (p.l+p.dl*cy)*deg
	peri, node := (p.peri+p.dp*cy)*deg, (p.node+p.dn*cy)*deg
	w := peri - node
	m := math.Remainder(l-peri, 2*math.Pi)
	E := m + e*math.Sin(m)
	for range 10 { // Newton on M = E − e sin E; e ≤ 0.21 converges in a few
		E -= (E - e*math.Sin(E) - m) / (1 - e*math.Cos(E))
	}
	x, y := a*(math.Cos(E)-e), a*math.Sqrt(1-e*e)*math.Sin(E)
	sinW, cosW := math.Sincos(w)
	sinN, cosN := math.Sincos(node)
	sinI, cosI := math.Sincos(incl)
	ex := (cosW*cosN-sinW*sinN*cosI)*x + (-sinW*cosN-cosW*sinN*cosI)*y
	ey := (cosW*sinN+sinW*cosN*cosI)*x + (-sinW*sinN+cosW*cosN*cosI)*y
	ez := sinW*sinI*x + cosW*sinI*y
	sinE, cosE := math.Sincos(j2000Obliquity * deg)

	return [3]float64{ex, cosE*ey - sinE*ez, sinE*ey + cosE*ez}
}

func norm(v [3]float64) float64 { return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]) }

// equatorial returns v's RA [0, 360) and Dec in degrees.
func equatorial(v [3]float64) (float64, float64) {
	return math.Mod(math.Atan2(v[1], v[0])/deg+360, 360), math.Asin(v[2]/norm(v)) / deg
}
