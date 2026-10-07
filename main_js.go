// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/dkorunic/astro-recommender/internal/geotz"
)

// init serves web/index.html's time zone lookup: run as argv ["tz", lat, lon]
// it prints the zone and exits before main, so -h and the flags stay those
// of the command line.
func init() { //nolint:gochecknoinits // must run before main, which parses the flags
	if len(os.Args) != 3 || os.Args[0] != "tz" {
		return
	}
	lat, errLat := strconv.ParseFloat(os.Args[1], 64)
	lon, errLon := strconv.ParseFloat(os.Args[2], 64)
	if errLat != nil || errLon != nil || !(lat >= -90 && lat <= 90) || !(lon >= -180 && lon <= 180) {
		os.Exit(2)
	}
	name, err := geotz.Lookup(lat, lon)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(name)
	os.Exit(0)
}
