// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package config parses and validates the command-line flags.
package config

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/geotz"
	"github.com/dkorunic/astro-recommender/internal/horizon"
	"github.com/dkorunic/astro-recommender/internal/num"
)

// errInvalidFlag wraps every flag validation error.
var errInvalidFlag = errors.New("invalid flag")

// Celestron Origin: 1.32° x 0.75° field of view, 1.23"/px.
const (
	originFOVLong  = 1.32 * 60 // arc minutes
	originFOVShort = 0.75 * 60 // arc minutes
	originScale    = 1.23      // arc seconds per pixel
)

// maxSensorPx bounds a frame's long side in pixels; the largest consumer
// sensors are ~12k px across, so beyond this -fov and -scale disagree.
const maxSensorPx = 20000

// Pixel scales of real setups: ~0.1"/px for long planetary focal lengths to
// ~100"/px for wide camera lenses, with margin either side.
const minScale, maxScale = 0.01, 1000.0

type Config struct {
	Day            time.Time
	Loc            *time.Location
	TargetsFile    string
	ListName       string // uptonight target list; GaryImm is embedded
	From, To       string // optional local "HH:MM" limits within the night
	SQMSource      string // where SQM was looked up; empty when given with -sqm
	Horizon        horizon.Horizon
	Plan           time.Duration
	Lat, Lon       float64
	AltMin, AltMax float64
	SizeMin        float64
	SizeMax        float64
	FilterK        float64
	Extinction     float64 // mag per airmass; used everywhere when set explicitly
	FOVLong        float64 // arc minutes
	FOVShort       float64 // arc minutes
	Scale          float64 // arc seconds per pixel
	Top            int
	Bortle         int
	SQM            float64 // measured zenith sky brightness, mag/arcsec²; 0 = unset
	CometMag       float64
	DateSet        bool // -date given; otherwise Day is now
	Version        bool // -version: print the version and exit; nothing else is set
	ExtinctionSet  bool // -extinction given: skip the aerosol-based estimate
	SkySet         bool // -sqm or -bortle given, even as 0: skip the DarkSkySites lookup
	Framing        bool // -origin, -fov or -scale: fit and score objects against the frame
	Filter         bool
	NoWeather      bool
	NoGeocode      bool
	NoSQM          bool
	NoComets       bool
}

func Parse() (Config, error) {
	var cfg Config
	var date, tz string
	var minPx float64
	var origin bool
	var fov string
	var horizonFile string
	flag.Float64Var(&cfg.Lat, "lat", 0, "latitude in degrees, north positive (required)")
	flag.Float64Var(&cfg.Lon, "lon", 0, "longitude in degrees, east positive (required)")
	flag.StringVar(&date, "date", "", "date of the evening, YYYY-MM-DD (default tonight; after midnight the rest of the night in progress)")
	flag.StringVar(&tz, "tz", "", "IANA time zone of the location (default: looked up from -lat/-lon)")
	flag.StringVar(&cfg.From, "from", "", "start of the imaging window, local HH:MM (default astronomical dusk)")
	flag.StringVar(&cfg.To, "to", "", "end of the imaging window, local HH:MM; before noon means next morning (default astronomical dawn)")
	flag.Float64Var(&cfg.AltMin, "alt-min", 30, "minimum altitude in degrees")
	flag.Float64Var(&cfg.AltMax, "alt-max", 80, "maximum altitude in degrees")
	flag.Float64Var(&cfg.SizeMin, "size-min", 10, "minimum object size in arc minutes (0 = no minimum)")
	flag.Float64Var(&cfg.SizeMax, "size-max", 300, "maximum object size in arc minutes")
	flag.IntVar(&cfg.Top, "n", 20, "number of objects to list")
	flag.StringVar(&cfg.ListName, "list", "GaryImm", "uptonight target list: "+strings.Join(catalog.Lists, ", "))
	flag.StringVar(&cfg.TargetsFile, "targets", "", "custom uptonight-style targets YAML file (overrides -list)")
	flag.BoolVar(&origin, "origin", false, "frame for the Celestron Origin (1.32x0.75°, 1.23\"/px): fit the FOV, at least -min-px across")
	flag.StringVar(&fov, "fov", "", "frame for another telescope: field of view WxH in degrees up to 180, e.g. 2.1x1.4 (default Origin; -scale optional)")
	flag.Float64Var(&cfg.Scale, "scale", 0, "frame for another telescope: pixel scale in arc seconds per pixel, 0.01-1000 (default Origin 1.23; -fov optional)")
	flag.Float64Var(&minPx, "min-px", 200, "with framing, minimum object size in pixels; 0 or more")
	flag.BoolVar(&cfg.Filter, "filter", false, "dual-band nebula filter in use: emission nebulae tolerate moonlight")
	flag.Float64Var(&cfg.FilterK, "filter-k", 0.25, "with -filter, fraction of moonlight/light pollution passing the filter (~0.15 for <=4nm, ~0.4 for wide bands)")
	flag.IntVar(&cfg.Bortle, "bortle", 0, "Bortle class 1-9 of the site, sets the zenith sky brightness (0 = dark sky)")
	flag.Float64Var(&cfg.SQM, "sqm", 0, "measured zenith sky brightness in mag/arcsec² (SQM meter or light pollution map); overrides -bortle; default: looked up on DarkSkySites when $DARKSKYSITES_API_KEY is set and -bortle is not")
	flag.Float64Var(&cfg.Extinction, "extinction", 0.2, "atmospheric extinction in mag per airmass; default: estimated per hour from elevation and CAMS aerosols, this value if unavailable")
	flag.StringVar(&horizonFile, "horizon", "", "local horizon file: \"azimuth altitude\" lines in degrees, # comments")
	flag.DurationVar(&cfg.Plan, "plan", 0, "print a night plan with one target per block of this length, e.g. 2h (0 = off)")
	flag.BoolVar(&cfg.NoWeather, "no-weather", false, "skip the Open-Meteo and 7Timer forecasts")
	flag.BoolVar(&cfg.NoGeocode, "no-geocode", false, "skip the OpenStreetMap reverse geocoding of the location")
	flag.BoolVar(&cfg.NoSQM, "no-sqm", false, "skip the DarkSkySites sky brightness lookup")
	flag.BoolVar(&cfg.NoComets, "no-comets", false, "skip comets (MPC orbital elements, cached for a day)")
	flag.Float64Var(&cfg.CometMag, "comet-mag", 12, "include comets brighter than this total visual magnitude")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		return Config{Version: true}, nil
	}

	set, nonFinite := visited()
	if len(nonFinite) > 0 {
		return Config{}, fmt.Errorf("%w: -%s must be a finite number", errInvalidFlag, strings.Join(nonFinite, ", -"))
	}
	// Required: zero defaults keep -h clean, NaN makes validate reject them.
	if !set["lat"] {
		cfg.Lat = math.NaN()
	}
	if !set["lon"] {
		cfg.Lon = math.NaN()
	}
	cfg.ExtinctionSet = set["extinction"]
	cfg.SkySet = set["sqm"] || set["bortle"]

	// Framing (-origin, -fov or -scale) replaces the size defaults; explicit
	// -size-min/-size-max still win. Unset frame values default to the Origin.
	cfg.Framing = origin || set["fov"] || set["scale"]
	cfg.FOVLong, cfg.FOVShort = originFOVLong, originFOVShort
	if set["fov"] {
		var err error
		if cfg.FOVLong, cfg.FOVShort, err = parseFOV(fov); err != nil {
			return Config{}, err
		}
	}
	if set["scale"] && (cfg.Scale < minScale || cfg.Scale > maxScale) {
		return Config{}, fmt.Errorf("%w: -scale must be between %g and %g arcsec/px", errInvalidFlag, minScale, maxScale)
	}
	if !set["scale"] {
		cfg.Scale = originScale
	}
	if minPx < 0 {
		return Config{}, fmt.Errorf("%w: -min-px must be 0 or more", errInvalidFlag)
	}
	// Only a frame the user fully specified can be checked against a sensor.
	// Deliberate: checking a lone -fov against the Origin's default scale (or
	// a lone -scale against its field) rejected valid wide fields such as
	// -fov 10x7, so a lone -fov typed in arc minutes is not caught (parseFOV
	// only bounds it at 180°).
	if px, _ := cfg.FramePx(); set["fov"] && set["scale"] && !(px <= maxSensorPx) {
		return Config{}, fmt.Errorf("%w: frame of %.0f px across is not a sensor; -fov is in degrees, -scale in arcsec/px", errInvalidFlag, px)
	}
	if cfg.Framing {
		if !set["size-min"] {
			cfg.SizeMin = minPx * cfg.Scale / 60
		}
		if !set["size-max"] {
			cfg.SizeMax = cfg.FOVShort
		}
		// The major axis must fit the short side whatever its orientation, so a
		// larger -size-max would only admit objects that are then never listed.
		if cfg.SizeMax > cfg.FOVShort {
			return Config{}, fmt.Errorf("%w: -size-max %.1f' exceeds the frame's short side %.1f'; larger objects cannot be framed",
				errInvalidFlag, cfg.SizeMax, cfg.FOVShort)
		}
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	if horizonFile != "" {
		var err error
		if cfg.Horizon, err = horizon.Load(horizonFile); err != nil {
			return Config{}, err
		}
	}

	var err error
	if tz == "" {
		// Look the zone up from the coordinates; the system zone is the
		// fallback, as before.
		if tz, err = geotz.Lookup(cfg.Lat, cfg.Lon); err != nil {
			fmt.Fprintln(os.Stderr, "warning: no time zone for the location, using the system zone:", err)
			tz = "Local"
		}
	}
	if cfg.Loc, err = time.LoadLocation(tz); err != nil {
		return Config{}, err
	}
	cfg.Day = time.Now().In(cfg.Loc)
	if date != "" {
		if cfg.Day, err = time.ParseInLocation(time.DateOnly, date, cfg.Loc); err != nil {
			return Config{}, err
		}
		cfg.DateSet = true
	}

	return cfg, nil
}

// ZenithMag returns the site's moonless zenith sky brightness: -sqm if given,
// else the typical value for -bortle (a dark sky when neither is set).
func (cfg *Config) ZenithMag() float64 {
	if cfg.SQM != 0 {
		return cfg.SQM
	}

	return atmos.BortleMag[cfg.Bortle]
}

// visited returns the flags given on the command line and the names of the
// float flags among them that are NaN or infinite: ParseFloat accepts those
// and every range check would pass them.
func visited() (map[string]bool, []string) {
	set := map[string]bool{}
	var nonFinite []string
	flag.Visit(func(f *flag.Flag) {
		set[f.Name] = true
		if g, ok := f.Value.(flag.Getter); ok {
			if v, ok := g.Get().(float64); ok && !num.Finite(v) {
				nonFinite = append(nonFinite, f.Name)
			}
		}
	})

	return set, nonFinite
}

// FramePx returns the frame's long and short sides in pixels. Parse
// guarantees Scale > 0; a zero-value Config returns NaN (0/0).
func (cfg *Config) FramePx() (float64, float64) {
	return cfg.FOVLong * 60 / cfg.Scale, cfg.FOVShort * 60 / cfg.Scale
}

// parseFOV parses -fov "WxH" in degrees into the long and short sides in arc minutes.
func parseFOV(s string) (float64, float64, error) {
	ws, hs, ok := strings.Cut(s, "x")
	w, err1 := strconv.ParseFloat(strings.TrimSpace(ws), 64)
	h, err2 := strconv.ParseFloat(strings.TrimSpace(hs), 64)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("%w: -fov must be WxH in degrees, e.g. 1.32x0.75", errInvalidFlag)
	}
	if !num.Finite(w) || !num.Finite(h) || w <= 0 || h <= 0 || w > 180 || h > 180 {
		return 0, 0, fmt.Errorf("%w: -fov sides must be above 0 and at most 180 degrees, got %s", errInvalidFlag, s)
	}

	return max(w, h) * 60, min(w, h) * 60, nil
}

// validate rejects numeric flags outside their meaningful range.
func (cfg *Config) validate() error {
	switch {
	// NaN is Parse's "not given" sentinel here, not user input (visited rejects that).
	case math.IsNaN(cfg.Lat) || math.IsNaN(cfg.Lon) || math.Abs(cfg.Lat) > 90 || math.Abs(cfg.Lon) > 180:
		return fmt.Errorf("%w: valid -lat and -lon are required", errInvalidFlag)
	case cfg.FilterK < 0 || cfg.FilterK > 1:
		return fmt.Errorf("%w: -filter-k must be between 0 and 1", errInvalidFlag)
	case cfg.Bortle < 0 || cfg.Bortle > 9:
		return fmt.Errorf("%w: -bortle must be between 1 and 9 (0 = off)", errInvalidFlag)
	case cfg.SQM != 0 && (cfg.SQM < atmos.MinSQM || cfg.SQM > atmos.MaxSQM):
		return fmt.Errorf("%w: -sqm must be between %g and %g mag/arcsec²", errInvalidFlag, atmos.MinSQM, atmos.MaxSQM)
	case cfg.Extinction < 0 || cfg.Extinction > 1:
		return fmt.Errorf("%w: -extinction must be between 0 and 1", errInvalidFlag)
	case cfg.Plan != 0 && cfg.Plan < 10*time.Minute:
		return fmt.Errorf("%w: -plan must be at least 10m", errInvalidFlag)
	case cfg.Top < 1:
		return fmt.Errorf("%w: -n must be at least 1", errInvalidFlag)
	case cfg.AltMin >= cfg.AltMax:
		return fmt.Errorf("%w: -alt-min (%g) must be below -alt-max (%g)", errInvalidFlag, cfg.AltMin, cfg.AltMax)
	case cfg.Framing && (cfg.Scale <= 0 || cfg.FOVShort <= 0 || cfg.FOVLong < cfg.FOVShort):
		return fmt.Errorf("%w: frame needs a positive scale and a field with long side >= short side", errInvalidFlag)
	case cfg.SizeMin < 0 || cfg.SizeMax < 0:
		return fmt.Errorf("%w: -size-min and -size-max must be 0 or more", errInvalidFlag)
	case cfg.SizeMin > cfg.SizeMax:
		return fmt.Errorf("%w: minimum size %.1f' exceeds maximum %.1f' (check -size-min/-size-max, or -min-px against the frame)",
			errInvalidFlag, cfg.SizeMin, cfg.SizeMax)
	}

	return nil
}
