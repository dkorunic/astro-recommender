// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import (
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/scoring"
)

// altitudes feeds the browser page's altitude curves; the CLI has none.
func altitudes(*config.Config, *scoring.Sky, []scoring.Result) {}
