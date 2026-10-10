// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package config parses and validates the command-line flags.
package config

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/geotz"
	"github.com/dkorunic/astro-recommender/internal/horizon"
	"github.com/dkorunic/astro-recommender/internal/num"
)

// errInvalidFlag wraps every flag validation error.
var errInvalidFlag = errors.New("invalid flag")

// Celestron Origin: 335 mm focal length, IMX678 with 2.0 µm pixels at
// 3856 x 2180 px, giving 1.32° x 0.75° and 1.23"/px.
const (
	originFocal                 = 335.0 // mm
	originPixel                 = 2.0   // µm
	originPxLong, originPxShort = 3856, 2180
)

var (
	originFOVLong  = fovSide(originPxLong*originPixel/1000, originFocal)  // arc minutes
	originFOVShort = fovSide(originPxShort*originPixel/1000, originFocal) // arc minutes
	originScale    = pixelScale(originPixel, originFocal)                 // arc seconds per pixel
)

// maxSensorPx bounds a frame's long side in pixels; the largest consumer
// sensors are ~12k px across, so beyond this -fov and -scale disagree.
const maxSensorPx = 20000

// Pixel scales of real setups: ~0.1"/px for long planetary focal lengths to
// ~100"/px for wide camera lenses, with margin either side.
const minScale, maxScale = 0.01, 1000.0

// maxMosaic bounds -mosaic; mosaicOverlap is the fraction of a frame side
// shared with the next panel, for registration.
const (
	maxMosaic     = 25
	mosaicOverlap = 0.1
)

type Config struct {
	Day            time.Time
	Loc            *time.Location
	TargetsFile    string
	ListName       string          // built-in target list (catalog.Lists)
	From, To       string          // optional local "HH:MM" limits within the night
	Twilight       string          // -twilight: "astronomical" or "nautical", the dusk/dawn definition
	SQMSource      string          // where SQM was looked up; empty when given with -sqm
	SkipFile       string          // -skip: file of names to leave out, shown in the header
	Skip           map[string]bool // its names as catalog.NameKey
	Horizon        horizon.Horizon
	Plan           time.Duration
	MinRun         time.Duration // -min-run: shortest continuous observable stretch a target needs; 0 = any
	Lat, Lon       float64
	AltMin, AltMax float64
	SizeMin        float64
	SizeMax        float64
	RA, Dec        float64 // -ra/-dec sky region centre in J2000 degrees; see RASet/DecSet
	Tol            float64 // -tol: half-width of the region in degrees (RA at 15°/h)
	FilterK        float64
	Extinction     float64 // mag per airmass; used everywhere when set explicitly
	FOVLong        float64 // arc minutes
	FOVShort       float64 // arc minutes
	Scale          float64 // arc seconds per pixel
	Top            int
	Mosaic         int // -mosaic: most panels per object, 1 = single frame
	Bortle         int
	SQM            float64 // measured zenith sky brightness, mag/arcsec²; 0 = unset
	CometMag       float64
	DateSet        bool // -date given; otherwise Day is now
	Version        bool // -version: print the version and exit; nothing else is set
	JSON           bool // -json: one JSON document instead of the tables
	ExtinctionSet  bool // -extinction given: skip the aerosol-based estimate
	SkySet         bool // -sqm or -bortle given, even as 0: skip the DarkSkySites lookup
	RASet, DecSet  bool // -ra/-dec given: keep only targets within Tol of them
	Framing        bool // -origin, -fov, -scale or -focal with -sensor/-pixel: fit and score objects against the frame
	Rotate         bool // -rotate: the camera turns to put an object's major axis along the frame's long side
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
	var fov, sensor string
	var focal, pixel float64
	var horizonFile string
	var ra, dec string
	flag.Float64Var(&cfg.Lat, "lat", 0, "latitude in degrees, north positive (required)")
	flag.Float64Var(&cfg.Lon, "lon", 0, "longitude in degrees, east positive (required)")
	flag.StringVar(&date, "date", "", "date of the evening, YYYY-MM-DD (default tonight; during the night, the rest of the night in progress)")
	flag.StringVar(&tz, "tz", "", "IANA time zone of the location (default: looked up from -lat/-lon)")
	flag.StringVar(&cfg.From, "from", "", "start of the imaging window, local HH:MM (default astronomical dusk)")
	flag.StringVar(&cfg.To, "to", "", "end of the imaging window, local HH:MM; before noon means next morning (default astronomical dawn)")
	flag.StringVar(&cfg.Twilight, "twilight", "astronomical", "dusk and dawn definition: astronomical (Sun below -18°) or nautical (-12°, for bright targets)")
	flag.Float64Var(&cfg.AltMin, "alt-min", 30, "minimum altitude in degrees")
	flag.Float64Var(&cfg.AltMax, "alt-max", 80, "maximum altitude in degrees")
	flag.Float64Var(&cfg.SizeMin, "size-min", 10, "minimum object size in arc minutes (0 = no minimum)")
	flag.Float64Var(&cfg.SizeMax, "size-max", 300, "maximum object size in arc minutes")
	flag.StringVar(&ra, "ra", "", "keep only targets near this J2000 right ascension, hours as 20.5 or \"20 30 00\"")
	flag.StringVar(&dec, "dec", "", "keep only targets near this J2000 declination, degrees as -12.5 or \"-12 30 00\"")
	flag.Float64Var(&cfg.Tol, "tol", 10, "with -ra/-dec, how near in degrees, above 0 and at most 180; RA counts 15° per hour")
	flag.IntVar(&cfg.Top, "n", 20, "number of objects to list")
	flag.BoolVar(&cfg.JSON, "json", false, "print the report as JSON instead of tables")
	// web/index.html builds its select from the ": name, name" part of this help.
	flag.StringVar(&cfg.ListName, "list", "GaryImm", "built-in target list: "+strings.Join(catalog.Lists, ", "))
	flag.StringVar(&cfg.TargetsFile, "targets", "", "custom uptonight-style targets YAML file (overrides -list)")
	flag.StringVar(&cfg.SkipFile, "skip", "", "file of target names to leave out, one per line (e.g. already imaged), # comments; case and spaces do not matter")
	flag.BoolVar(&origin, "origin", false, "frame for the Celestron Origin (IMX678 at 335 mm: 1.32x0.75°, 1.23\"/px): fit the FOV, at least -min-px across")
	flag.StringVar(&fov, "fov", "", "frame for another telescope: field of view WxH in degrees up to 180, e.g. 2.1x1.4 (default Origin; -scale optional)")
	flag.Float64Var(&focal, "focal", 0, "frame for another telescope: focal length in mm; with -sensor sets the field of view (instead of -fov), with -pixel the pixel scale (instead of -scale)")
	flag.StringVar(&sensor, "sensor", "", "frame for another telescope: sensor size WxH in mm, e.g. 23.5x15.6; with -focal, sets the field of view instead of -fov")
	flag.Float64Var(&cfg.Scale, "scale", 0, "frame for another telescope: pixel scale in arc seconds per pixel, 0.01-1000 (Origin default 1.23; -fov optional)")
	flag.Float64Var(&pixel, "pixel", 0, "frame for another telescope: camera pixel size in µm; with -focal, sets the pixel scale instead of -scale")
	flag.Float64Var(&minPx, "min-px", 200, "with framing, minimum object size in pixels; 0 or more")
	flag.IntVar(&cfg.Mosaic, "mosaic", 1, "with framing, most mosaic panels per object, 1-25; an object's score is divided among its panels")
	flag.BoolVar(&cfg.Rotate, "rotate", false, "with framing, the camera can be rotated to frame the major axis along the long side, so the minor axis counts (not the Origin)")
	flag.BoolVar(&cfg.Filter, "filter", false, "dual-band nebula filter in use: emission nebulae tolerate moonlight")
	flag.Float64Var(&cfg.FilterK, "filter-k", 0.25, "with -filter, fraction of moonlight/light pollution passing the filter (~0.15 for <=4nm, ~0.4 for wide bands)")
	flag.IntVar(&cfg.Bortle, "bortle", 0, "Bortle class 1-9 of the site, sets the zenith sky brightness (0 = dark sky)")
	flag.Float64Var(&cfg.SQM, "sqm", 0, "measured zenith sky brightness in mag/arcsec² (SQM meter or light pollution map); overrides -bortle; default: looked up on DarkSkySites when a key is available (CLI: $DARKSKYSITES_API_KEY; browser: always) and -bortle is not")
	flag.Float64Var(&cfg.Extinction, "extinction", 0.2, "atmospheric extinction in mag per airmass; default: estimated per hour from elevation and CAMS aerosols, this value if unavailable")
	flag.StringVar(&horizonFile, "horizon", "", "local horizon file: \"azimuth altitude\" lines in degrees, # comments")
	flag.DurationVar(&cfg.Plan, "plan", 0, "print a night plan with one target per block of this length, e.g. 2h (0 = off)")
	flag.DurationVar(&cfg.MinRun, "min-run", 0, "keep only targets observable without a break for at least this long, e.g. 1h (0 = any)")
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
	if err := cfg.parseRegion(set, ra, dec); err != nil {
		return Config{}, err
	}

	// Framing (-origin, -fov or -focal/-sensor, -scale or -focal/-pixel)
	// replaces the size defaults; explicit -size-min/-size-max still win.
	// Unset frame values default to the Origin. frameFOV rejects -sensor and
	// -pixel without -focal, and -focal without either.
	fovSet, scaleSet := set["fov"] || set["sensor"], set["scale"] || set["pixel"]
	cfg.Framing = origin || fovSet || scaleSet
	var err error
	if cfg.FOVLong, cfg.FOVShort, err = frameFOV(set, fov, sensor, focal); err != nil {
		return Config{}, err
	}
	if cfg.Scale, err = frameScale(set, cfg.Scale, pixel, focal); err != nil {
		return Config{}, err
	}
	if minPx < 0 {
		return Config{}, fmt.Errorf("%w: -min-px must be 0 or more", errInvalidFlag)
	}
	// Only a frame the user fully specified (-fov or -focal/-sensor, and
	// -scale or -focal/-pixel) can be checked against a sensor.
	// Deliberate: checking a lone -fov against the Origin's default scale (or
	// a lone -scale against its field) rejected valid wide fields such as
	// -fov 10x7, so a lone -fov typed in arc minutes is not caught (parseFOV
	// only bounds it at 180°).
	if px, _ := cfg.FramePx(); fovSet && scaleSet && !(px <= maxSensorPx) {
		return Config{}, fmt.Errorf("%w: frame of %.0f px across is not a sensor; -fov is in degrees, -scale in arcsec/px", errInvalidFlag, px)
	}
	if err := cfg.frameSizes(set, minPx); err != nil {
		return Config{}, err
	}

	cfg.Twilight = strings.ToLower(strings.TrimSpace(cfg.Twilight))
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.loadFiles(horizonFile); err != nil {
		return Config{}, err
	}

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

// loadSkip reads a -skip file: one name per line, # comments, as catalog.NameKey.
func loadSkip(file string) (map[string]bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	// Editors that save "UTF-8 with BOM" would otherwise hide the first name.
	for line := range strings.Lines(strings.TrimPrefix(string(data), "\uFEFF")) {
		name, _, _ := strings.Cut(line, "#")
		if name = strings.TrimSpace(name); name != "" {
			skip[catalog.NameKey(name)] = true
		}
	}

	return skip, nil
}

// SunAlt returns the Sun altitude (degrees) below which -twilight counts as night.
func (cfg *Config) SunAlt() float64 {
	if cfg.Twilight == "nautical" {
		return astro.Nautical
	}

	return astro.Astronomical
}

// Near reports whether a position (J2000 degrees) is within Tol of the -ra
// and -dec given; an axis not given does not limit. RA wraps at 24h.
func (cfg *Config) Near(ra, dec float64) bool {
	if cfg.RASet && math.Abs(math.Mod(ra-cfg.RA+540, 360)-180) > cfg.Tol {
		return false
	}

	return !cfg.DecSet || math.Abs(dec-cfg.Dec) <= cfg.Tol
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

// Mosaic is a grid of frames, Cols along the frame's long side and Rows
// along its short one, overlapping by mosaicOverlap; W and H are its extent
// in arc minutes. A single frame is a 1x1 mosaic.
type Mosaic struct {
	Cols, Rows int
	W, H       float64
}

// Panels returns the frame count.
func (m Mosaic) Panels() int { return m.Cols * m.Rows }

// Mosaics returns every grid of at most -mosaic panels (at least the single
// frame, so a hand-built Config without -mosaic frames as before), fewest
// panels first.
func (cfg *Config) Mosaics() []Mosaic {
	var out []Mosaic
	for c := 1; c <= max(1, cfg.Mosaic); c++ {
		for r := 1; c*r <= max(1, cfg.Mosaic); r++ {
			out = append(out, Mosaic{
				Cols: c, Rows: r,
				W: cfg.FOVLong * (float64(c) - float64(c-1)*mosaicOverlap),
				H: cfg.FOVShort * (float64(r) - float64(r-1)*mosaicOverlap),
			})
		}
	}
	slices.SortStableFunc(out, func(a, b Mosaic) int { return cmp.Compare(a.Panels(), b.Panels()) })

	return out
}

// MaxFramed returns the largest major axis in arc minutes that some mosaic
// can hold: its longest side with -rotate, else its shortest, since an
// object's orientation in the frame is then unknown.
func (cfg *Config) MaxFramed() float64 {
	var size float64
	for _, m := range cfg.Mosaics() {
		if cfg.Rotate {
			size = max(size, m.W, m.H)
		} else {
			size = max(size, min(m.W, m.H))
		}
	}

	return size
}

// FramePx returns the frame's long and short sides in pixels. Parse
// guarantees Scale > 0; a zero-value Config returns NaN (0/0).
func (cfg *Config) FramePx() (float64, float64) {
	return cfg.FOVLong * 60 / cfg.Scale, cfg.FOVShort * 60 / cfg.Scale
}

// frameSizes checks -mosaic and -rotate, and with framing replaces the size
// defaults by -min-px up to the largest object a mosaic holds; explicit
// -size-min/-size-max still win.
func (cfg *Config) frameSizes(set map[string]bool, minPx float64) error {
	if !cfg.Framing && (set["mosaic"] || set["rotate"]) {
		return fmt.Errorf("%w: -mosaic and -rotate need framing (-origin, -fov, -scale or -focal)", errInvalidFlag)
	}
	if cfg.Mosaic < 1 || cfg.Mosaic > maxMosaic {
		return fmt.Errorf("%w: -mosaic must be 1 to %d panels", errInvalidFlag, maxMosaic)
	}
	if !cfg.Framing {
		return nil
	}
	if !set["size-min"] {
		cfg.SizeMin = minPx * cfg.Scale / 60
	}
	// A larger -size-max would only admit objects that are then never listed.
	maxSize := cfg.MaxFramed()
	if !set["size-max"] {
		cfg.SizeMax = maxSize
	}
	if cfg.SizeMax > maxSize {
		return fmt.Errorf("%w: -size-max %.1f' exceeds the largest frame side an object can use, %.1f'; larger objects cannot be framed",
			errInvalidFlag, cfg.SizeMax, maxSize)
	}

	return nil
}

// loadFiles reads the -horizon and -skip files that were given.
func (cfg *Config) loadFiles(horizonFile string) error {
	var err error
	if horizonFile != "" {
		if cfg.Horizon, err = horizon.Load(horizonFile); err != nil {
			return err
		}
	}
	if cfg.SkipFile != "" {
		cfg.Skip, err = loadSkip(cfg.SkipFile)
	}

	return err
}

// parseRegion sets RA/Dec (degrees) and RASet/DecSet from -ra (hours) and
// -dec (degrees), each decimal or sexagesimal, and checks -tol when either
// is given.
func (cfg *Config) parseRegion(set map[string]bool, ra, dec string) error {
	cfg.RASet, cfg.DecSet = set["ra"], set["dec"]
	if cfg.RASet {
		h, err := astro.Sexagesimal(ra)
		if err != nil {
			return fmt.Errorf("%w: -ra must be hours as 20.5 or \"20 30 00\": %w", errInvalidFlag, err)
		}
		if h < 0 || h > 24 {
			return fmt.Errorf("%w: -ra must be 0 to 24 hours as 20.5 or \"20 30 00\"", errInvalidFlag)
		}
		cfg.RA = math.Mod(h*15, 360) // 24h is 0h
	}
	if cfg.DecSet {
		d, err := astro.Sexagesimal(dec)
		if err != nil {
			return fmt.Errorf("%w: -dec must be degrees as -12.5 or \"-12 30 00\": %w", errInvalidFlag, err)
		}
		if math.Abs(d) > 90 {
			return fmt.Errorf("%w: -dec must be -90 to 90 degrees as -12.5 or \"-12 30 00\"", errInvalidFlag)
		}
		cfg.Dec = d
	}
	if (cfg.RASet || cfg.DecSet) && (cfg.Tol <= 0 || cfg.Tol > 180) {
		return fmt.Errorf("%w: -tol must be above 0 and at most 180 degrees", errInvalidFlag)
	}

	return nil
}

// frameFOV returns the frame's long and short sides in arc minutes from -fov,
// or -focal with -sensor, or the Origin's when neither is given. It also
// validates -focal for frameScale.
func frameFOV(set map[string]bool, fov, sensor string, focal float64) (float64, float64, error) {
	switch {
	case set["fov"] && set["sensor"]:
		return 0, 0, fmt.Errorf("%w: give either -fov or -focal with -sensor, not both", errInvalidFlag)
	case (set["sensor"] || set["pixel"]) && !set["focal"]:
		return 0, 0, fmt.Errorf("%w: -sensor and -pixel need -focal", errInvalidFlag)
	case set["focal"] && !set["sensor"] && !set["pixel"]:
		return 0, 0, fmt.Errorf("%w: -focal needs -sensor or -pixel", errInvalidFlag)
	case set["focal"] && focal <= 0:
		return 0, 0, fmt.Errorf("%w: -focal must be above 0 mm", errInvalidFlag)
	case set["fov"]:
		return parseFOV(fov)
	case set["sensor"]:
		return sensorFOV(sensor, focal)
	}

	return originFOVLong, originFOVShort, nil
}

// frameScale returns the pixel scale in arc seconds per pixel from -scale, or
// -pixel (µm) at -focal (mm, validated by frameFOV), or the Origin's.
func frameScale(set map[string]bool, scale, pixel, focal float64) (float64, error) {
	switch {
	case set["scale"] && set["pixel"]:
		return 0, fmt.Errorf("%w: give either -scale or -focal with -pixel, not both", errInvalidFlag)
	case set["pixel"] && pixel <= 0:
		return 0, fmt.Errorf("%w: -pixel must be above 0 µm", errInvalidFlag)
	case set["pixel"]:
		if scale = pixelScale(pixel, focal); scale < minScale || scale > maxScale {
			return 0, fmt.Errorf("%w: -pixel %g µm at -focal %g mm is %.3g\"/px, outside %g-%g", errInvalidFlag, pixel, focal, scale, minScale, maxScale)
		}
	case !set["scale"]:
		return originScale, nil
	case scale < minScale || scale > maxScale:
		return 0, fmt.Errorf("%w: -scale must be between %g and %g arcsec/px", errInvalidFlag, minScale, maxScale)
	}

	return scale, nil
}

// parseFOV parses -fov "WxH" in degrees into the long and short sides in arc minutes.
func parseFOV(s string) (float64, float64, error) {
	long, short, err := parseWxH("fov", "degrees", "1.32x0.75", s)
	if err != nil {
		return 0, 0, err
	}
	if long > 180 {
		return 0, 0, fmt.Errorf("%w: -fov sides must be at most 180 degrees, got %s", errInvalidFlag, s)
	}

	return long * 60, short * 60, nil
}

// sensorFOV returns the long and short sides in arc minutes of the field a
// sensor -sensor "WxH" (mm) covers at focal length focal (mm, above 0).
func sensorFOV(sensor string, focal float64) (float64, float64, error) {
	long, short, err := parseWxH("sensor", "mm", "23.5x15.6", sensor)
	if err != nil {
		return 0, 0, err
	}

	return fovSide(long, focal), fovSide(short, focal), nil
}

// fovSide returns the angle in arc minutes that mm of sensor spans at focal
// length focal (mm).
func fovSide(mm, focal float64) float64 {
	return 2 * math.Atan(mm/(2*focal)) / (math.Pi / 180) * 60
}

// pixelScale returns arc seconds per pixel for pixels of pixel µm at focal
// length focal (mm): 206265"/rad, with µm over mm leaving a factor of 1000.
func pixelScale(pixel, focal float64) float64 {
	return 206.264806 * pixel / focal
}

// parseWxH parses flag value s, "WxH" in unit, into its long and short sides.
func parseWxH(name, unit, example, s string) (float64, float64, error) {
	ws, hs, ok := strings.Cut(s, "x")
	w, err1 := strconv.ParseFloat(strings.TrimSpace(ws), 64)
	h, err2 := strconv.ParseFloat(strings.TrimSpace(hs), 64)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("%w: -%s must be WxH in %s, e.g. %s", errInvalidFlag, name, unit, example)
	}
	if !num.Finite(w) || !num.Finite(h) || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("%w: -%s sides must be above 0 %s, got %s", errInvalidFlag, name, unit, s)
	}

	return max(w, h), min(w, h), nil
}

// validateSky checks the sky flags: brightness, extinction, filter and twilight.
func (cfg *Config) validateSky() error {
	switch {
	case cfg.FilterK < 0 || cfg.FilterK > 1:
		return fmt.Errorf("%w: -filter-k must be between 0 and 1", errInvalidFlag)
	case cfg.Bortle < 0 || cfg.Bortle > 9:
		return fmt.Errorf("%w: -bortle must be between 1 and 9 (0 = off)", errInvalidFlag)
	case cfg.SQM != 0 && (cfg.SQM < atmos.MinSQM || cfg.SQM > atmos.MaxSQM):
		return fmt.Errorf("%w: -sqm must be between %g and %g mag/arcsec²", errInvalidFlag, atmos.MinSQM, atmos.MaxSQM)
	case cfg.Extinction < 0 || cfg.Extinction > 1:
		return fmt.Errorf("%w: -extinction must be between 0 and 1", errInvalidFlag)
	// Empty is a hand-built Config: astronomical, as SunAlt reads it.
	case cfg.Twilight != "" && cfg.Twilight != "astronomical" && cfg.Twilight != "nautical":
		return fmt.Errorf("%w: -twilight must be astronomical or nautical", errInvalidFlag)
	}

	return nil
}

// validate rejects numeric flags outside their meaningful range.
func (cfg *Config) validate() error {
	if err := cfg.validateSky(); err != nil {
		return err
	}
	switch {
	// NaN is Parse's "not given" sentinel here, not user input (visited rejects that).
	case math.IsNaN(cfg.Lat) || math.IsNaN(cfg.Lon) || math.Abs(cfg.Lat) > 90 || math.Abs(cfg.Lon) > 180:
		return fmt.Errorf("%w: valid -lat and -lon are required", errInvalidFlag)
	case cfg.Plan != 0 && cfg.Plan < 10*time.Minute:
		return fmt.Errorf("%w: -plan must be at least 10m", errInvalidFlag)
	case cfg.Plan%time.Minute != 0:
		return fmt.Errorf("%w: -plan must be a whole number of minutes", errInvalidFlag)
	case cfg.MinRun < 0:
		return fmt.Errorf("%w: -min-run must be 0 or more", errInvalidFlag)
	case cfg.Top < 1:
		return fmt.Errorf("%w: -n must be at least 1", errInvalidFlag)
	// Scoring compares the limits as sines, which only orders altitudes within ±90°.
	case math.Abs(cfg.AltMin) > 90 || math.Abs(cfg.AltMax) > 90:
		return fmt.Errorf("%w: -alt-min and -alt-max must be between -90 and 90", errInvalidFlag)
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
