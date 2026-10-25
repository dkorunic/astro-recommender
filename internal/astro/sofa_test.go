// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/hebl/gofa"
)

// TestSOFA checks the low-precision formulas against IAU SOFA (gofa, test-only
// dependency) over random dates in 2000-2050. The limits are the README's
// Accuracy table; tighten them there if you tighten them here.
func TestSOFA(t *testing.T) {
	const (
		mjdUnix     = 40587 // MJD of the Unix epoch
		arcsec      = 1.0 / 3600
		au          = 1.495978707e11 // m
		earthRadius = 6378137.0      // m, WGS84 equatorial, the unit of MoonDistance
	)
	// SOFA vector -> RA/Dec in degrees.
	radec := func(p [3]float64) (float64, float64) {
		var theta, phi float64
		gofa.C2s(p, &theta, &phi)

		return math.Mod(theta/deg+360, 360), phi / deg
	}
	// J2000 (GCRS/BCRS) vector -> mean equator and equinox of date (IAU 1976).
	ofDate := func(p [3]float64, tt float64) [3]float64 {
		var rm [3][3]float64
		var out [3]float64
		gofa.Pmat76(2400000.5, tt, &rm)
		gofa.Rxp(rm, p, &out)

		return out
	}

	rng := rand.New(rand.NewPCG(2026, 10))
	var worst struct{ sun, sunLon, moon, moonTopo, moonDist, phase, illum, gmst, prec, ecl, alt, az, earth float64 }
	for range 3000 {
		ut := time.Unix(rng.Int64N(50*365*86400)+946684800, 0).UTC()
		mjd := float64(ut.Unix())/86400 + mjdUnix
		// TT = UTC + (TAI-UTC) + 32.184 s; SOFA's leap second table covers to now.
		var dat float64
		gofa.Dat(ut.Year(), int(ut.Month()), ut.Day(), 0, &dat)
		tt := mjd + (dat+32.184)/86400

		var pvh, pvb, pvm [2][3]float64
		gofa.Epv00(2400000.5, tt, &pvh, &pvb)
		gofa.Moon98(2400000.5, tt, &pvm)
		sun := ofDate([3]float64{-pvh[0][0], -pvh[0][1], -pvh[0][2]}, tt)
		moon := ofDate(pvm[0], tt)

		sra, sdec := radec(sun)
		ra, dec := SunRADec(ut)
		worst.sun = max(worst.sun, Separation(ra, dec, sra, sdec))

		mra, mdec := radec(moon)
		ra, dec = MoonRADec(ut)
		worst.moon = max(worst.moon, Separation(ra, dec, mra, mdec))

		// The true phase angle, at the Moon between the Sun and the Earth,
		// which ours takes as 180° less the elongation, and the illuminated
		// fraction it gives, (1 + cos i)/2.
		sunGeo := [3]float64{-pvh[0][0], -pvh[0][1], -pvh[0][2]}
		toSun := [3]float64{sunGeo[0] - pvm[0][0], sunGeo[1] - pvm[0][1], sunGeo[2] - pvm[0][2]}
		toEarth := [3]float64{-pvm[0][0], -pvm[0][1], -pvm[0][2]}
		phase := gofa.Sepp(toSun, toEarth) / deg
		worst.phase = max(worst.phase, math.Abs(MoonPhaseAngle(ut)-phase))
		worst.illum = max(worst.illum, math.Abs(MoonIllumination(ut)-(1+math.Cos(phase*deg))/2)*100)
		// Geocentric distance in Earth (equatorial) radii.
		worst.moonDist = max(worst.moonDist, math.Abs(MoonDistance(ut)-gofa.Pm(pvm[0])*au/earthRadius))

		// The Sun's ecliptic longitude of date, against SOFA's mean ecliptic
		// and equinox of date (IAU 2006) from the GCRS direction.
		var gra, gdec, sl, sb float64
		gofa.C2s(sunGeo, &gra, &gdec)
		gofa.Eqec06(2400000.5, tt, gra, gdec, &sl, &sb)
		worst.sunLon = max(worst.sunLon, math.Abs(math.Remainder(SunLongitude(ut)-sl/deg, 360)))

		gmst := gofa.Gmst06(2400000.5, mjd, 2400000.5, tt) / deg
		ours := math.Mod(280.46061837+360.98564736629*DaysJ2000(ut), 360)
		worst.gmst = max(worst.gmst, math.Abs(math.Remainder(ours-gmst, 360)))

		// Uniform over the sphere, poles included (Precess clamps its sine there).
		ra0, dec0 := rng.Float64()*360, math.Asin(2*rng.Float64()-1)/deg
		var s [3]float64
		gofa.S2c(ra0*deg, dec0*deg, &s)
		pra, pdec := radec(ofDate(s, tt))
		ra, dec = PrecessT(ra0, dec0, (tt-51544.5)/36525)
		worst.prec = max(worst.prec, Separation(ra, dec, pra, pdec))

		// Ecliptic coordinates as scoring takes them for the zodiacal light
		// (J2000 precessed to date, then the mean obliquity), against SOFA's.
		var el, eb float64
		gofa.Eqec06(2400000.5, tt, ra0*deg, dec0*deg, &el, &eb)
		lon0, lat0 := Ecliptic(ra, dec, ut)
		worst.ecl = max(worst.ecl, Separation(lon0, lat0, el/deg, eb/deg))

		// Altitude as the pipeline computes it (precession only, GMST) against
		// SOFA's full bias-precession-nutation and apparent sidereal time.
		lat, lon := rng.Float64()*160-80, rng.Float64()*360-180
		var rnpb [3][3]float64
		var app [3]float64
		gofa.Pnm06a(2400000.5, tt, &rnpb)
		gofa.Rxp(rnpb, s, &app)
		ara, adec := radec(app)
		gast := gofa.Gst06a(2400000.5, mjd, 2400000.5, tt)/deg + lon
		sinAlt := math.Sin(lat*deg)*math.Sin(adec*deg) + math.Cos(lat*deg)*math.Cos(adec*deg)*math.Cos((gast-ara)*deg)
		alt, az := AltAz(ra, dec, ut, lat, lon)
		worst.alt = max(worst.alt, math.Abs(alt-math.Asin(sinAlt)/deg))
		// Azimuth (scoring's, through Horizontal, against a -horizon), as
		// an angle on the sky: × cos(altitude), since it is undefined at
		// the zenith.
		var saz, sel float64
		gofa.Hd2ae(gast*deg-ara*deg, adec*deg, lat*deg, &saz, &sel)
		worst.az = max(worst.az, math.Abs(math.Remainder(az-saz/deg, 360))*math.Cos(sel))

		// Topocentric Moon: SOFA's geocentric Moon minus the geodetic (WGS84)
		// observer, rotated from the Earth-fixed frame by apparent sidereal time.
		var site [3]float64
		gofa.Gd2gc(gofa.WGS84, lon*deg, lat*deg, 0, &site)
		sinG, cosG := math.Sincos(gofa.Gst06a(2400000.5, mjd, 2400000.5, tt))
		topo := [3]float64{
			moon[0] - (site[0]*cosG-site[1]*sinG)/au,
			moon[1] - (site[0]*sinG+site[1]*cosG)/au,
			moon[2] - site[2]/au,
		}
		tra, tdec := radec(topo)
		ra, dec = MoonTopo(ut, lat, lon)
		worst.moonTopo = max(worst.moonTopo, Separation(ra, dec, tra, tdec))

		// EarthHelio is in the J2000 ecliptic frame; SOFA's vector is equatorial.
		const eps = 23.4392911 * deg
		ex, ey := EarthHelio(ut)
		worst.earth = max(worst.earth, math.Hypot(ex-pvh[0][0], ey-(pvh[0][1]*math.Cos(eps)+pvh[0][2]*math.Sin(eps))))
	}

	for _, c := range []struct {
		name  string
		got   float64
		limit float64
	}{
		{"Sun (degrees)", worst.sun, 1.0 / 60},
		{"Moon (degrees)", worst.moon, 30 * arcsec},                 // 18″ seen, against Moon98 (itself ~10″)
		{"topocentric Moon (degrees)", worst.moonTopo, 45 * arcsec}, // 28″ seen: also a spherical Earth and mean sidereal time
		{"Moon illumination (percentage points)", worst.illum, 0.2}, // against the true fraction (1 + cos i)/2; 0.14 seen
		{"GMST (degrees)", worst.gmst, 0.3 * arcsec},
		{"precession (degrees)", worst.prec, 0.01 * arcsec}, // 0.004″ seen; a t² slip in zeta is 0.075″
		{"target altitude (degrees)", worst.alt, 0.3 / 60},
		{"Earth position (AU)", worst.earth, 0.00025},                // 0.000215 seen; dropping the r 2g term gives 0.00026
		{"Sun ecliptic longitude (degrees)", worst.sunLon, 1.0 / 60}, // 0.0123° seen, as the Sun's RA/Dec
		{"Moon distance (Earth radii)", worst.moonDist, 0.01},        // 0.0034 seen of 56-64
		{"Moon phase angle (degrees)", worst.phase, 0.2},             // 0.17° seen: 180° less the elongation, the Sun's parallax from the Moon
		{"ecliptic coordinates (degrees)", worst.ecl, 5 * arcsec},    // 3.9″ seen, the low-precision obliquity's at 2050
		{"target azimuth on the sky (degrees)", worst.az, 0.3 / 60},  // as the altitude
	} {
		if c.got > c.limit {
			t.Errorf("%s: worst difference from SOFA %g exceeds %g", c.name, c.got, c.limit)
		} else {
			t.Logf("%s: worst %.3g (limit %g)", c.name, c.got, c.limit)
		}
	}
}

// TestSOFAGalactic checks Galactic against SOFA's Icrs2g, whose ICRS frame
// sits a few milliarcseconds from the FK5 J2000 one the constants are of.
func TestSOFAGalactic(t *testing.T) {
	const arcsec = 1.0 / 3600
	r := rand.New(rand.NewPCG(3, 4))
	worst := 0.0
	for range 3000 {
		ra, dec := r.Float64()*360, math.Asin(2*r.Float64()-1)/deg
		var l, b float64
		gofa.Icrs2g(ra*deg, dec*deg, &l, &b)
		gl, gb := Galactic(ra, dec)
		if gl < 0 || gl >= 360 {
			t.Fatalf("Galactic(%v, %v) longitude %v outside [0, 360)", ra, dec, gl)
		}
		worst = max(worst, Separation(gl, gb, l/deg, b/deg))
	}
	if worst > 0.05*arcsec { // 0.004″ seen
		t.Errorf("galactic coordinates: worst difference from SOFA %g° exceeds 0.05″", worst)
	} else {
		t.Logf("galactic coordinates: worst %.3g″", worst/arcsec)
	}
	// The galactic centre and the north galactic pole.
	if l, b := Galactic(266.40499, -28.93617); math.Abs(math.Remainder(l, 360)) > 0.001 || math.Abs(b) > 0.001 {
		t.Errorf("galactic centre at (%v, %v)", l, b)
	}
	if _, b := Galactic(galPoleRA, galPoleDec); math.Abs(b-90) > 1e-9 {
		t.Errorf("north galactic pole at b = %v", b)
	}
}

// sofaSunAlt is the Sun's geometric altitude (degrees) at ut for the site
// from SOFA: the GCRS direction of Epv00's Sun, rotated to the true equator
// of date, against apparent sidereal time.
func sofaSunAlt(ut time.Time, lat, lon float64) float64 {
	const mjdUnix = 40587
	mjd := float64(ut.Unix())/86400 + mjdUnix
	var dat float64
	gofa.Dat(ut.Year(), int(ut.Month()), ut.Day(), 0, &dat)
	tt := mjd + (dat+32.184)/86400
	var pvh, pvb [2][3]float64
	gofa.Epv00(2400000.5, tt, &pvh, &pvb)
	var rnpb [3][3]float64
	var app [3]float64
	gofa.Pnm06a(2400000.5, tt, &rnpb)
	gofa.Rxp(rnpb, [3]float64{-pvh[0][0], -pvh[0][1], -pvh[0][2]}, &app)
	var ra, dec float64
	gofa.C2s(app, &ra, &dec)
	ha := gofa.Gst06a(2400000.5, mjd, 2400000.5, tt) + lon*deg - ra
	var az, el float64
	gofa.Hd2ae(ha, dec, lat*deg, &az, &el)

	return el / deg
}

// TestSOFAWindow checks that Window's dusk and dawn are the minutes SOFA's
// Sun crosses the twilight limit, at random sites (|latitude| ≤ 60°, where
// every night ends) and dates in 2000-2050: the Sun is past the limit at the
// returned minute and not yet a minute before, within its 0.0123° error
// (0.02°, about 5 s of the Sun's motion at the equator).
func TestSOFAWindow(t *testing.T) {
	const eps = 0.02
	rng := rand.New(rand.NewPCG(5, 6))
	for range 300 {
		lat, lon := rng.Float64()*120-60, rng.Float64()*360-180
		day := time.Unix(rng.Int64N(50*365*86400)+946684800, 0).UTC()
		for _, limit := range []float64{Astronomical, Nautical} {
			dusk, dawn, ok := Window(day, lat, lon, limit)
			if !ok || dawn.Sub(dusk) >= 24*time.Hour {
				continue // no night at that limit, or a polar one with no crossing
			}
			before, at := sofaSunAlt(dusk.Add(-time.Minute), lat, lon), sofaSunAlt(dusk, lat, lon)
			if before < limit-eps || at > limit+eps {
				t.Errorf("%v at %.2f, %.2f, limit %v: dusk %v, SOFA Sun %.3f° a minute before, %.3f° at it", day.Format(time.DateOnly), lat, lon, limit, dusk, before, at)
			}
			before, at = sofaSunAlt(dawn.Add(-time.Minute), lat, lon), sofaSunAlt(dawn, lat, lon)
			if before > limit+eps || at < limit-eps {
				t.Errorf("%v at %.2f, %.2f, limit %v: dawn %v, SOFA Sun %.3f° a minute before, %.3f° at it", day.Format(time.DateOnly), lat, lon, limit, dawn, before, at)
			}
		}
	}
}

// TestSOFAReference pins the reference results that SOFA's own validation
// program (t_sofa_c.c, release 2023-10-11, the current one) gives for the
// routines these tests compare against, so a gofa upgrade that strays from
// SOFA fails here rather than silently moving the comparisons.
func TestSOFAReference(t *testing.T) {
	near := func(name string, got, want, tol float64) {
		t.Helper()
		if math.Abs(got-want) > tol {
			t.Errorf("%s = %.19g, SOFA's reference %.19g", name, got, want)
		}
	}
	var pvh, pvb [2][3]float64
	gofa.Epv00(2400000.5, 53411.52501161, &pvh, &pvb)
	near("Epv00 ph(x)", pvh[0][0], -0.7757238809297706813, 1e-14)
	near("Epv00 ph(y)", pvh[0][1], 0.5598052241363340596, 1e-14)
	near("Epv00 ph(z)", pvh[0][2], 0.2426998466481686993, 1e-14)
	var pvm [2][3]float64
	gofa.Moon98(2400000.5, 43999.9, &pvm)
	near("Moon98 x", pvm[0][0], -0.2601295959971044180e-2, 1e-11)
	near("Moon98 y", pvm[0][1], 0.6139750944302742189e-3, 1e-11)
	near("Moon98 z", pvm[0][2], 0.2640794528229828909e-3, 1e-11)
	near("Gst06a", gofa.Gst06a(2400000.5, 53736.0, 2400000.5, 53736.0), 1.754166137675019159, 1e-12)
	near("Gmst06", gofa.Gmst06(2400000.5, 53736.0, 2400000.5, 53736.0), 1.754174971870091203, 1e-12)
	var r [3][3]float64
	gofa.Pnm06a(2400000.5, 50123.9999, &r)
	near("Pnm06a 11", r[0][0], 0.9999995832794205484, 1e-14)
	near("Pnm06a 12", r[0][1], 0.8372382772630962111e-3, 1e-14)
	near("Pnm06a 23", r[1][2], 0.4132905944611019498e-4, 1e-14)
	gofa.Pmat76(2400000.5, 50123.9999, &r)
	near("Pmat76 12", r[0][1], 0.8696632209480960785e-3, 1e-14)
	near("Pmat76 13", r[0][2], 0.3779153474959888345e-3, 1e-14)
	near("Pmat76 23", r[1][2], -0.1643284776111886407e-6, 1e-14)
	var a, b float64
	gofa.Eqec06(1234.5, 2440000.5, 1.234, 0.987, &a, &b)
	near("Eqec06 dl", a, 1.342509918994654619, 1e-14)
	near("Eqec06 db", b, 0.5926215259704608132, 1e-14)
	gofa.Icrs2g(5.9338074302227188048671087, -1.1784870613579944551540570, &a, &b)
	near("Icrs2g L", a, 5.5850536063818546461558, 1e-14)
	near("Icrs2g B", b, -0.7853981633974483096157, 1e-14)
	gofa.Hd2ae(1.1, 1.2, 0.3, &a, &b)
	near("Hd2ae a", a, 5.916889243730066194, 1e-13)
	near("Hd2ae e", b, 0.4472186304990486228, 1e-14)
	var xyz [3]float64
	gofa.Gd2gc(gofa.WGS84, 3.1, -0.5, 2500, &xyz)
	near("Gd2gc x", xyz[0], -5599000.5577049947, 1e-7)
	near("Gd2gc z", xyz[2], -3040909.4706983363, 1e-7)
	var dat float64
	gofa.Dat(2017, 9, 1, 0, &dat)
	near("Dat 2017", dat, 37, 1e-12)
}
