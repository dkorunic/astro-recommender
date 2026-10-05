// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package horizon reads a local horizon profile and interpolates it by azimuth.
package horizon

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/dkorunic/astro-recommender/internal/num"
)

var errHorizon = errors.New("bad horizon line, want \"azimuth altitude\"")

// Horizon is a local Horizon profile: (azimuth, minimum altitude) pairs in
// degrees, sorted by azimuth.
type Horizon [][2]float64

// Load reads "azimuth altitude" lines (degrees, # starts a comment).
func Load(path string) (Horizon, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var h Horizon
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("%w: %s:%d", errHorizon, path, line)
		}
		az, err1 := strconv.ParseFloat(fields[0], 64)
		alt, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil || err2 != nil || !num.Finite(az) || !num.Finite(alt) || az < 0 || az > 360 || alt < -90 || alt > 90 {
			return nil, fmt.Errorf("%w: %s:%d", errHorizon, path, line)
		}
		h = append(h, [2]float64{math.Mod(az, 360), alt})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// Stable: two points at one azimuth are a vertical step, in file order.
	slices.SortStableFunc(h, func(a, b [2]float64) int { return cmp.Compare(a[0], b[0]) })

	return h, nil
}

// At returns the horizon altitude At azimuth az, interpolating linearly
// between points and wrapping around north. An empty horizon is -90 (no limit).
func (h Horizon) At(az float64) float64 {
	if len(h) == 0 {
		return -90
	}
	i, _ := slices.BinarySearchFunc(h, az, func(p [2]float64, a float64) int { return cmp.Compare(p[0], a) })
	lo, hi := h[(i-1+len(h))%len(h)], h[i%len(h)]
	span := math.Mod(hi[0]-lo[0]+360, 360)
	if span == 0 {
		return lo[1]
	}

	return lo[1] + (hi[1]-lo[1])*math.Mod(az-lo[0]+360, 360)/span
}
