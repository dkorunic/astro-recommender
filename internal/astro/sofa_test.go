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
		mjdUnix = 40587 // MJD of the Unix epoch
		arcsec  = 1.0 / 3600
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
	var worst struct{ sun, moon, illum, gmst, prec, alt, earth float64 }
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

		illum := (1 - math.Cos(Separation(sra, sdec, mra, mdec)*deg)) / 2
		worst.illum = max(worst.illum, math.Abs(MoonIllumination(ut)-illum)*100)

		gmst := gofa.Gmst06(2400000.5, mjd, 2400000.5, tt) / deg
		ours := math.Mod(280.46061837+360.98564736629*DaysJ2000(ut), 360)
		worst.gmst = max(worst.gmst, math.Abs(math.Remainder(ours-gmst, 360)))

		ra0, dec0 := rng.Float64()*360, rng.Float64()*170-85
		var s [3]float64
		gofa.S2c(ra0*deg, dec0*deg, &s)
		pra, pdec := radec(ofDate(s, tt))
		ra, dec = PrecessT(ra0, dec0, (tt-51544.5)/36525)
		worst.prec = max(worst.prec, Separation(ra, dec, pra, pdec))

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
		worst.alt = max(worst.alt, math.Abs(Altitude(ra, dec, ut, lat, lon)-math.Asin(sinAlt)/deg))

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
		{"Moon (degrees)", worst.moon, 0.4},
		{"Moon illumination (percentage points)", worst.illum, 0.3},
		{"GMST (degrees)", worst.gmst, 0.3 * arcsec},
		{"precession (degrees)", worst.prec, 0.1 * arcsec},
		{"target altitude (degrees)", worst.alt, 0.3 / 60},
		{"Earth position (AU)", worst.earth, 0.0003},
	} {
		if c.got > c.limit {
			t.Errorf("%s: worst difference from SOFA %g exceeds %g", c.name, c.got, c.limit)
		} else {
			t.Logf("%s: worst %.3g (limit %g)", c.name, c.got, c.limit)
		}
	}
}
