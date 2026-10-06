// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package comets turns Minor Planet Center comet orbital elements into
// moving targets.
package comets

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
)

const deg = math.Pi / 180

const (
	cometURL      = "https://www.minorplanetcenter.net/iau/MPCORB/CometEls.txt"
	cometCacheAge = 24 * time.Hour   // MPC updates the file daily
	gaussK        = 0.01720209895    // Gaussian gravitational constant, rad/day (AU, solar masses)
	j2000Obliq    = 23.4392911 * deg // mean obliquity at J2000, the frame of MPC elements
)

var errComets = errors.New("mpc comets")

// comet holds MPC orbital elements (J2000 ecliptic) and magnitude parameters.
type comet struct {
	perihelion time.Time // T, TT treated as UT
	name       string
	q, e       float64 // perihelion distance (AU), eccentricity
	peri, node float64 // argument of perihelion, longitude of ascending node (rad)
	incl       float64 // inclination (rad)
	h, k       float64 // total magnitude m = H + 5 log Δ + 2.5 K log r
}

// Elements are the comet orbital elements from Fetch.
type Elements []comet

// Fetch returns the MPC comet elements, cached for a day under the user
// cache directory. It is the only network step, so it can run alongside
// the others before the grid exists.
func Fetch(ctx context.Context) (Elements, error) {
	return fetch.Cached(ctx, cometURL, "CometEls.txt", cometCacheAge, parseComets)
}

// Targets returns the comets brighter than maxMag at mid-window as
// targets with a position track: Track[i] is the position at grid[i].
func (e Elements) Targets(grid []time.Time, maxMag float64) []catalog.Target {
	mid := grid[len(grid)/2]
	var out []catalog.Target
	for _, c := range e {
		ra, dec, r, delta := c.position(mid)
		if math.IsNaN(ra) { // position's "orbit not solved" sentinel
			continue
		}
		mag := c.h + 5*math.Log10(delta) + 2.5*c.k*math.Log10(r)
		if mag > maxMag {
			continue
		}
		tg := catalog.Target{Type: "Comet", Constellation: constellation.Of(ra, dec), Track: make([][2]float64, len(grid))}
		tg.Name, tg.Description, _ = strings.Cut(c.name, " (")
		tg.Description = strings.TrimSuffix(tg.Description, ")")
		tg.Description = strings.TrimSpace(fmt.Sprintf("%s mag %.1f, r %.2f AU, Δ %.2f AU", tg.Description, mag, r, delta))
		if c.track(grid, tg.Track) {
			out = append(out, tg)
		}
	}

	return out
}

// track fills track[i] with the position of date at grid[i]. It reports false
// if any position is NaN: scoring's altitude limits (a < lo || a > hi) would
// pass a NaN minute as observable and turn the score into NaN.
func (c *comet) track(grid []time.Time, track [][2]float64) bool {
	for i, t := range grid {
		ra, dec, _, _ := c.position(t)
		if math.IsNaN(ra) || math.IsNaN(dec) {
			return false
		}
		track[i][0], track[i][1] = astro.Precess(ra, dec, t) // J2000 -> of date, as for catalogs
	}

	return true
}

// parseComets reads the MPC one-line comet element format (fixed columns).
// Lines without magnitude parameters cannot be filtered by brightness and are skipped.
func parseComets(data []byte) (Elements, error) {
	var out Elements
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		if len(l) < 103 {
			continue
		}
		// Any field error skips the line, so the error itself is never reported.
		f := func(a, b int) (float64, error) {
			v, err := strconv.ParseFloat(strings.TrimSpace(l[a:b]), 64)
			if err == nil && !num.Finite(v) {
				return 0, errComets
			}

			return v, err
		}
		year, err1 := strconv.Atoi(l[14:18])
		month, err2 := strconv.Atoi(l[19:21])
		day, err3 := f(22, 29)
		q, err4 := f(30, 39)
		e, err5 := f(41, 49)
		peri, err6 := f(51, 59)
		node, err7 := f(61, 69)
		incl, err8 := f(71, 79)
		h, err9 := f(91, 95)
		k, err10 := f(96, 100)
		// A day out of range would overflow the Duration below (silently, as
		// float-to-int conversion does), and time.Date would shift a bad month.
		if errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10) != nil || q <= 0 || e < 0 ||
			month < 1 || month > 12 || day < 1 || day >= 32 {
			continue
		}
		t := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration((day - 1) * 24 * float64(time.Hour)))
		name := sanitize.Text(strings.TrimSpace(l[102:min(len(l), 158)]))
		out = append(out, comet{
			name: name, perihelion: t, q: q, e: e,
			peri: peri * deg, node: node * deg, incl: incl * deg, h: h, k: k,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no elements parsed", errComets)
	}

	return out, nil
}

// position returns the comet's geocentric J2000 right ascension and
// declination in degrees, its distance from the Sun r and from the Earth
// delta in AU. RA is NaN if the orbit cannot be solved.
// ponytail: two-body orbit, no light time or planetary perturbations;
// arc-minute level near the element epoch, fine for picking targets.
func (c *comet) position(t time.Time) (float64, float64, float64, float64) {
	nu, r := c.anomaly(t.Sub(c.perihelion).Hours() / 24)
	if math.IsNaN(r) {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}
	u := c.peri + nu
	sinU, cosU := math.Sincos(u)
	sinO, cosO := math.Sincos(c.node)
	sinI, cosI := math.Sincos(c.incl)
	x := r * (cosO*cosU - sinO*sinU*cosI)
	y := r * (sinO*cosU + cosO*sinU*cosI)
	z := r * sinU * sinI

	ex, ey := astro.EarthHelio(t)
	x, y = x-ex, y-ey
	delta := math.Sqrt(x*x + y*y + z*z)
	sinE, cosE := math.Sincos(j2000Obliq)
	ye, ze := y*cosE-z*sinE, y*sinE+z*cosE

	return math.Mod(math.Atan2(ye, x)/deg+360, 360), math.Asin(ze/delta) / deg, r, delta
}

// anomaly returns true anomaly (rad) and heliocentric distance (AU) dt days
// after perihelion, for elliptic, parabolic and hyperbolic orbits.
func (c *comet) anomaly(dt float64) (float64, float64) {
	const eps = 1e-12
	switch {
	case math.Abs(c.e-1) < 1e-9: // parabolic: Barker's equation, closed form
		w := 3 * gaussK / math.Sqrt(2*c.q*c.q*c.q) * dt
		y := math.Cbrt(w/2 + math.Sqrt(w*w/4+1))
		s := y - 1/y

		return 2 * math.Atan(s), c.q * (1 + s*s)
	case c.e < 1: // elliptic: Kepler's equation by Newton
		a := c.q / (1 - c.e)
		m := math.Remainder(gaussK/math.Pow(a, 1.5)*dt, 2*math.Pi)
		E := m
		if c.e > 0.8 {
			E = math.Copysign(math.Pi, m)
		}
		for range 100 {
			d := (E - c.e*math.Sin(E) - m) / (1 - c.e*math.Cos(E))
			if E -= d; math.Abs(d) < eps {
				break
			}
		}
		nu := 2 * math.Atan2(math.Sqrt(1+c.e)*math.Sin(E/2), math.Sqrt(1-c.e)*math.Cos(E/2))

		return nu, a * (1 - c.e*math.Cos(E))
	default: // hyperbolic: e sinh H - H = M by Newton
		a := c.q / (c.e - 1)
		m := gaussK / math.Pow(a, 1.5) * dt
		H := math.Copysign(math.Log(2*math.Abs(m)/c.e+1.8), m)
		for range 100 {
			d := (c.e*math.Sinh(H) - H - m) / (c.e*math.Cosh(H) - 1)
			if H -= d; math.Abs(d) < eps {
				break
			}
		}
		nu := 2 * math.Atan(math.Sqrt((c.e+1)/(c.e-1))*math.Tanh(H/2))

		return nu, a * (c.e*math.Cosh(H) - 1)
	}
}
