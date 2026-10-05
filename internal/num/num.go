// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package num holds numeric checks shared by the parsers of untrusted input.
package num

import "math"

// Finite reports whether x is neither NaN nor infinite. strconv.ParseFloat
// and YAML accept both, and every range comparison written as x < lo || x > hi
// passes NaN, so check this before the bounds.
func Finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
