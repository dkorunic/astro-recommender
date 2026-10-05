// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package astro computes Sun and Moon positions, sidereal time, altitude and
// azimuth, precession and the astronomical night window from compact
// published formulas (validated against SOFA, see the README).
package astro

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/num"
)

var (
	errFields = errors.New("want 1 to 3 fields")
	errRange  = errors.New("fields must be positive, minutes and seconds below 60")
	errWindow = errors.New("requested window lies outside astronomical night")
)

const deg = math.Pi / 180

// Window returns [astronomical dusk, astronomical dawn) for the night starting
// on the evening of day's date in day's location. It searches from the site's
// solar noon (from its longitude, so a -tz that does not match the site cannot
// cut the night short) for 24 hours; polar night is capped there.
func Window(day time.Time, lat, lon float64) (time.Time, time.Time, bool) {
	y, m, d := day.Date()
	noon := time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Add(-time.Duration(lon / 15 * float64(time.Hour)))
	// Take the solar noon nearest local noon: zones across the date line from
	// their longitude (Samoa, Tonga, Kiritimati, Chatham at +13 to +14 but
	// west longitude) put it on the next local date, a night late.
	local := time.Date(y, m, d, 12, 0, 0, 0, day.Location())
	for noon.Sub(local) > 12*time.Hour {
		noon = noon.Add(-24 * time.Hour)
	}
	for local.Sub(noon) > 12*time.Hour {
		noon = noon.Add(24 * time.Hour)
	}
	noon = noon.Truncate(time.Minute)
	limit := noon.Add(24 * time.Hour)
	dark := func(t time.Time) bool {
		ra, dec := SunRADec(t)

		return Altitude(ra, dec, t, lat, lon) < -18
	}
	t := noon
	for t.Before(limit) && !dark(t) {
		t = t.Add(time.Minute)
	}
	if !t.Before(limit) {
		return time.Time{}, time.Time{}, false
	}
	start := t
	for t.Before(limit) && dark(t) {
		t = t.Add(time.Minute)
	}

	return start, t, true
}

// Tonight returns the date whose night "tonight" means at now: the night in
// progress (after today's dusk, or after midnight before dawn of the night
// that began yesterday), or else the coming one. inProgress reports the
// former, so that the elapsed part is not planned.
func Tonight(now time.Time, lat, lon float64) (time.Time, bool) {
	for _, day := range []time.Time{now.AddDate(0, 0, -1), now} {
		if dusk, dawn, ok := Window(day, lat, lon); ok && !now.Before(dusk) && now.Before(dawn) {
			return day, true
		}
	}

	return now, false
}

// ClipWindow narrows the night [start, end) to the local clock times from and
// to ("HH:MM", empty = no limit). Times before noon mean the morning after day,
// so -from 22:00 -to 02:00 spans midnight. The night is never extended.
func ClipWindow(day time.Time, loc *time.Location, start, end time.Time, from, to string) (time.Time, time.Time, error) {
	clock := func(s string) (time.Time, error) {
		c, err := time.Parse("15:04", s)
		if err != nil {
			return time.Time{}, fmt.Errorf("bad time %q, want HH:MM: %w", s, err)
		}
		y, m, d := day.Date()
		if c.Hour() < 12 {
			d++
		}

		return time.Date(y, m, d, c.Hour(), c.Minute(), 0, 0, loc), nil
	}
	if from != "" {
		t, err := clock(from)
		if err != nil {
			return start, end, err
		}
		if t.After(start) {
			start = t
		}
	}
	if to != "" {
		t, err := clock(to)
		if err != nil {
			return start, end, err
		}
		if t.Before(end) {
			end = t
		}
	}
	if !start.Before(end) {
		return start, end, fmt.Errorf("%w (%s - %s)", errWindow, start.Format("15:04"), end.Format("15:04"))
	}

	return start, end, nil
}

// MoonPhaseAngle returns the Sun-Moon-Earth angle in degrees (0 = full, 180 = new).
func MoonPhaseAngle(t time.Time) float64 {
	sra, sdec := SunRADec(t)
	mra, mdec := MoonRADec(t)

	return 180 - Separation(sra, sdec, mra, mdec)
}

// Precess moves J2000 equatorial coordinates (degrees) to the mean equator and
// equinox of t (IAU 1976 precession, Lieske et al. 1977). Without it, J2000
// catalog positions against sidereal time of date put altitudes up to ~40'
// off by 2026; with it they agree with SOFA to <0.3' (nutation ignored).
func Precess(ra, dec float64, t time.Time) (float64, float64) {
	return PrecessT(ra, dec, DaysJ2000(t)/36525)
}

// PrecessT is precess for t Julian centuries from J2000 (negative = earlier).
func PrecessT(ra, dec, t float64) (float64, float64) {
	zeta := (2306.2181*t + 0.30188*t*t + 0.017998*t*t*t) / 3600 * deg
	z := (2306.2181*t + 1.09468*t*t + 0.018203*t*t*t) / 3600 * deg
	theta := (2004.3109*t - 0.42665*t*t - 0.041833*t*t*t) / 3600 * deg
	sinA, cosA := math.Sincos(ra*deg + zeta)
	sinD, cosD := math.Sincos(dec * deg)
	sinT, cosT := math.Sincos(theta)
	a := cosD * sinA
	b := cosT*cosD*cosA - sinT*sinD
	c := sinT*cosD*cosA + cosT*sinD

	return math.Mod((math.Atan2(a, b)+z)/deg+360, 360), math.Asin(c) / deg
}

// DaysJ2000 returns days since J2000.0 (UT, ignoring ΔT).
func DaysJ2000(t time.Time) float64 {
	return float64(t.Unix())/86400 - 10957.5
}

// SunRADec returns the Sun's right ascension and declination in degrees
// (Astronomical Almanac low-precision formula, ~0.01°).
func SunRADec(t time.Time) (float64, float64) {
	n := DaysJ2000(t)
	L := 280.460 + 0.9856474*n
	g := (357.528 + 0.9856003*n) * deg
	lambda := (L + 1.915*math.Sin(g) + 0.020*math.Sin(2*g)) * deg

	return eclipticToEquatorial(lambda, 0, n)
}

// MoonRADec returns the Moon's geocentric right ascension and declination in
// degrees (Astronomical Almanac low-precision formula, ~0.4°).
func MoonRADec(t time.Time) (float64, float64) {
	n := DaysJ2000(t)
	T := n / 36525
	s := func(a, b float64) float64 { return math.Sin((a + b*T) * deg) }
	lambda := 218.32 + 481267.881*T +
		6.29*s(135.0, 477198.87) - 1.27*s(259.3, -413335.36) + 0.66*s(235.7, 890534.22) +
		0.21*s(269.9, 954397.74) - 0.19*s(357.5, 35999.05) - 0.11*s(186.5, 966404.03)
	beta := 5.13*s(93.3, 483202.02) + 0.28*s(228.2, 960400.89) -
		0.28*s(318.3, 6003.15) - 0.17*s(217.6, -407332.21)

	return eclipticToEquatorial(lambda*deg, beta*deg, n)
}

// eclipticToEquatorial converts ecliptic longitude/latitude (radians) to right
// ascension and declination in degrees.
func eclipticToEquatorial(lambda, beta, n float64) (float64, float64) {
	eps := (23.439 - 0.0000004*n) * deg
	x := math.Cos(beta) * math.Cos(lambda)
	y := math.Cos(eps)*math.Cos(beta)*math.Sin(lambda) - math.Sin(eps)*math.Sin(beta)
	z := math.Sin(eps)*math.Cos(beta)*math.Sin(lambda) + math.Cos(eps)*math.Sin(beta)

	return math.Mod(math.Atan2(y, x)/deg+360, 360), math.Asin(z) / deg
}

// MoonIllumination returns the illuminated fraction, from Sun-Moon elongation.
func MoonIllumination(t time.Time) float64 {
	sra, sdec := SunRADec(t)
	mra, mdec := MoonRADec(t)

	return (1 - math.Cos(Separation(sra, sdec, mra, mdec)*deg)) / 2
}

// Altitude in degrees of an object at ra/dec (degrees) for an observer.
func Altitude(ra, dec float64, t time.Time, lat, lon float64) float64 {
	alt, _ := AltAz(ra, dec, t, lat, lon)

	return alt
}

// MoonTopo returns the Moon's topocentric right ascension and declination in
// degrees: MoonRADec seen from the observer instead of the Earth's centre, at
// its horizontal parallax (Astronomical Almanac low precision, 54-61'). The
// shift is up to 1°, more than MoonRADec's own error; astroplan's Moon is
// topocentric too.
func MoonTopo(t time.Time, lat, lon float64) (float64, float64) {
	ra, dec := MoonRADec(t)
	dist := MoonDistance(t)
	sinRA, cosRA := math.Sincos(ra * deg)
	sinDec, cosDec := math.Sincos(dec * deg)
	sinLST, cosLST := math.Sincos(lst(t, lon) * deg)
	sinLat, cosLat := math.Sincos(lat * deg)
	x := dist*cosDec*cosRA - cosLat*cosLST
	y := dist*cosDec*sinRA - cosLat*sinLST
	z := dist*sinDec - sinLat

	return math.Mod(math.Atan2(y, x)/deg+360, 360), math.Atan2(z, math.Hypot(x, y)) / deg
}

// MoonDistance returns the Moon's geocentric distance in Earth radii (55.9-63.8,
// mean 60.27) from its horizontal parallax (Astronomical Almanac low precision).
func MoonDistance(t time.Time) float64 {
	T := DaysJ2000(t) / 36525
	c := func(a, b float64) float64 { return math.Cos((a + b*T) * deg) }
	parallax := 0.9508 + 0.0518*c(134.9, 477198.85) + 0.0095*c(259.2, -413335.38) +
		0.0078*c(235.7, 890534.23) + 0.0028*c(269.9, 954397.70)

	return 1 / math.Sin(parallax*deg)
}

// lst returns the local mean sidereal time in degrees (GMST + east longitude).
func lst(t time.Time, lon float64) float64 {
	return 280.46061837 + 360.98564736629*DaysJ2000(t) + lon
}

// AltAz returns altitude and azimuth (from north through east) in degrees of
// an object at ra/dec (degrees) for an observer.
func AltAz(ra, dec float64, t time.Time, lat, lon float64) (float64, float64) {
	h := (lst(t, lon) - ra) * deg
	sinLat, cosLat := math.Sincos(lat * deg)
	sinDec, cosDec := math.Sincos(dec * deg)
	sinAlt := sinLat*sinDec + cosLat*cosDec*math.Cos(h)
	az := math.Atan2(-math.Sin(h)*cosDec, sinDec*cosLat-cosDec*sinLat*math.Cos(h)) / deg

	return math.Asin(sinAlt) / deg, math.Mod(az+360, 360)
}

// Separation in degrees between two equatorial positions (degrees).
func Separation(ra1, dec1, ra2, dec2 float64) float64 {
	c := math.Sin(dec1*deg)*math.Sin(dec2*deg) + math.Cos(dec1*deg)*math.Cos(dec2*deg)*math.Cos((ra1-ra2)*deg)

	return math.Acos(max(-1, min(1, c))) / deg
}

// Sexagesimal parses "[+-]a b c" into a + b/60 + c/3600 with the sign applied.
func Sexagesimal(s string) (float64, error) {
	s = strings.TrimSpace(s)
	sign := 1.0
	if strings.HasPrefix(s, "-") {
		sign = -1
	}
	s = strings.TrimLeft(s, "+-")
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 3 {
		return 0, fmt.Errorf("%w: %q", errFields, s)
	}
	var v float64
	for i, f := range fields {
		x, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, err
		}
		if !num.Finite(x) || x < 0 || i > 0 && x >= 60 {
			return 0, fmt.Errorf("%w: %q", errRange, s)
		}
		v += x / math.Pow(60, float64(i))
	}

	return sign * v, nil
}

// EarthHelio returns the Earth's heliocentric ecliptic x, y in AU (z ~ 0) in
// the J2000 frame of MPC elements: the opposite of the Sun's geocentric
// position (Astronomical Almanac), whose longitude is of date and so loses
// the general precession (1.397°/century). Without that a comet 0.5 AU away
// lands ~0.7° off.
func EarthHelio(t time.Time) (float64, float64) {
	n := DaysJ2000(t)
	g := (357.528 + 0.9856003*n) * deg
	lambda := (280.460 + 0.9856474*n + 1.915*math.Sin(g) + 0.020*math.Sin(2*g) - 1.3969713*n/36525) * deg
	r := 1.00014 - 0.01671*math.Cos(g) - 0.00014*math.Cos(2*g)

	return -r * math.Cos(lambda), -r * math.Sin(lambda)
}
