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

// maxSensorPx bounds a frame's long side in pixels; the largest consumer
// sensors are ~12k px across, so beyond this -fov and -scale disagree.
const maxSensorPx = 20000

// Pixel scales of real setups: ~0.1"/px for long planetary focal lengths to
// ~100"/px for wide camera lenses, with margin either side.
const minScale, maxScale = 0.01, 1000.0

// minSub and maxSub bound -sub, in seconds: beyond them the counts overflow
// or the per-sub SNR does, and no camera takes such subs.
const minSub, maxSub = 0.001, 3600.0

// maxDark bounds -dark, e⁻/s per pixel at 25 °C: ten times the warmest
// preset's, past which it is a typo (e⁻/min, or a hot pixel's).
const maxDark = 10.0

// maxMosaic bounds -mosaic; mosaicOverlap is the fraction of a frame side
// shared with the next panel, for registration.
const (
	maxMosaic     = 25
	mosaicOverlap = 0.1
)

type Config struct {
	Day            time.Time
	Loc            *time.Location
	Skip           map[string]bool // its names as catalog.NameKey
	TargetsFile    string
	ListName       string   // built-in target list (catalog.Lists)
	From, To       string   // optional local "HH:MM" limits within the night
	Twilight       string   // -twilight: "astronomical" or "nautical", the dusk/dawn definition
	SQMSource      string   // where SQM was looked up; empty when given with -sqm
	SkipFile       string   // -skip: file of names to leave out, shown in the header
	Filter         string   // -filter: comma-separated Filters or Broadband keys, lowercased, without repeats; "" = none
	SolarBasis     string   // what SolarFlux is: solar.Monthly, Recent or Predicted
	Camera         string   // -camera's (or -scope's chip's) Cameras key, lower case; "" for a camera given by -sensor/-pixel
	Scope          string   // -scope's Scopes key; "" without one
	SubsNeeds      string   // the flags SUBS lacks, without which it would borrow another instrument's optics or sensor and is left out: without -scope "-aperture", "-camera" or both; with it "-aperture" after -focal, "-qe and -read-noise" after a -pixel unlike its own; "" when it has them
	Mount          string   // -mount: MountAltAz, MountDerotated or MountEq; -scope's by default, else MountEq (with framing)
	Types          []string // -type: catalog.Classes to keep, nil = all
	Horizon        horizon.Horizon
	Plan           time.Duration
	MinRun         time.Duration // -min-run: shortest continuous observable stretch a target needs; 0 = any
	Lat, Lon       float64
	AltMin, AltMax float64
	SizeMin        float64
	SizeMax        float64
	RA, Dec        float64 // -ra/-dec sky region centre in J2000 degrees; see RASet/DecSet
	Tol            float64 // -tol: half-width of the region in degrees (RA at 15°/h)
	FilterNM       float64 // -filter-nm: each band's width
	FRatio         float64 // -filter-std: the beam's focal ratio its line filters sit in; 0 = collimated (filters made for it)
	Obstructed     float64 // -filter-std: the beam's central obstruction, a fraction of the aperture
	Extinction     float64 // mag per airmass; used everywhere when set explicitly
	FOVLong        float64 // arc minutes
	FOVShort       float64 // arc minutes
	Scale          float64 // arc seconds per pixel
	Aperture       float64 // mm, for the sub-exposure estimate; with framing the Origin's unless -aperture
	Obstruction    float64 // mm, the central obstruction's diameter
	Sub            float64 // s, sub-exposure length
	QE             float64 // the camera's peak quantum efficiency, 0-1
	ReadNoise      float64 // e⁻ per pixel per sub
	Throughput     float64 // the optics' share of the light reaching the sensor, 0-1; 0 in a hand-built Config is 1
	Dark           float64 // e⁻/s per pixel at a sensor temperature of 25 °C
	SensorTemp     float64 // °C, a cooled camera's setpoint; see SensorTempSet
	Reject         float64 // the share of subs lost to tracking, satellites and wind, 0-0.9
	Residual       float64 // -residual: flats' and gradients' residual, a share of the sky the stack keeps; 0 = none
	Top            int
	Mosaic         int // -mosaic: most panels per object, 1 = single frame
	Bortle         int
	SQM            float64 // measured zenith sky brightness, mag/arcsec²; 0 = unset
	CometMag       float64
	SolarFlux      float64 // sfu, the night's monthly 10.7 cm solar flux main looked up; 0 = unknown, the sky as configured
	DateSet        bool    // -date given; otherwise Day is now
	Version        bool    // -version: print the version and exit; nothing else is set
	JSON           bool    // -json: one JSON document instead of the tables
	CSV            bool    // -csv: the targets as CSV that N.I.N.A. imports, instead of the tables
	ExtinctionSet  bool    // -extinction given: skip the aerosol-based estimate
	SkySet         bool    // -sqm or -bortle given, even as 0: skip the DarkSkySites lookup
	RASet, DecSet  bool    // -ra/-dec given: keep only targets within Tol of them
	Framing        bool    // -scope, -fov, -scale or -focal with -sensor/-pixel: fit and score objects against the frame
	Rotate         bool    // -rotate: the camera turns to put an object's major axis along the frame's long side
	Mono           bool    // -mono: a monochrome camera, every pixel sees every band
	ApertureSet    bool    // the aperture is the telescope's own: -scope's or -aperture, not the Origin's stand-in
	SensorTempSet  bool    // -sensor-temp given: the sensor is held there, else ambient + scoring's sensorWarm
	MeanStack      bool    // -stack mean: subs averaged with equal weight (smart telescopes' live stacks), else weighted by SNR²
	AltAz          bool    // Mount is MountAltAz: field rotation limits the observable minutes
	OtherOptics    bool    // -scope with -focal: its camera behind other optics, whose aperture SUBS needs
	OtherChip      bool    // -scope with a -pixel unlike its own: another camera too, whose QE and read noise SUBS needs
	NoWeather      bool
	NoGeocode      bool
	NoSQM          bool
	NoComets       bool
	NoSolar        bool
	NoTUI          bool // -no-tui: the tables even on a terminal, not the interactive view

	// BandNM is a -scope's built-in filter's band widths by line, without
	// -filter-nm; 0 = FilterNM.
	BandNM [catalog.NumLines]float64
}

func Parse() (Config, error) {
	var cfg Config
	var date, tz string
	var minPx float64
	var fov, sensor string
	var focal, pixel float64
	var camera, scopeName string
	var filterStd bool
	var stack, mount string
	var horizonFile string
	var ra, dec, types string
	flag.Float64Var(&cfg.Lat, "lat", 0, "latitude in degrees, north positive (required)")
	flag.Float64Var(&cfg.Lon, "lon", 0, "longitude in degrees, east positive (required)")
	flag.StringVar(&date, "date", "", "date of the evening, YYYY-MM-DD (default tonight; during the night, the rest of the night in progress)")
	flag.StringVar(&tz, "tz", "", "IANA time zone of the location (default: looked up from -lat/-lon)")
	flag.StringVar(&cfg.From, "from", "", "start of the imaging window, local HH:MM (default astronomical dusk)")
	flag.StringVar(&cfg.To, "to", "", "end of the imaging window, local HH:MM; before noon means next morning (default astronomical dawn)")
	flag.StringVar(&cfg.Twilight, "twilight", "astronomical", "dusk and dawn definition: astronomical (Sun below -18°) or nautical (-12°, for bright targets)")
	flag.Float64Var(&cfg.AltMin, "alt-min", 30, "minimum altitude in degrees, 0 or more")
	flag.Float64Var(&cfg.AltMax, "alt-max", 80, "maximum altitude in degrees")
	flag.Float64Var(&cfg.SizeMin, "size-min", 10, "minimum object size in arc minutes (0 = no minimum)")
	flag.Float64Var(&cfg.SizeMax, "size-max", 300, "maximum object size in arc minutes")
	flag.StringVar(&ra, "ra", "", "keep only targets near this J2000 right ascension, hours as 20.5 or \"20 30 00\"")
	flag.StringVar(&dec, "dec", "", "keep only targets near this J2000 declination, degrees as -12.5 or \"-12 30 00\"")
	flag.Float64Var(&cfg.Tol, "tol", 10, "with -ra/-dec, how near in degrees, above 0 and at most 180; RA counts 15° per hour")
	flag.IntVar(&cfg.Top, "n", 20, "number of objects to list")
	flag.BoolVar(&cfg.JSON, "json", false, "print the report as JSON instead of tables")
	flag.BoolVar(&cfg.CSV, "csv", false, "print the targets (with -plan, the planned ones in order) as CSV in Telescopius's format, which N.I.N.A. imports, instead of tables")
	// web/index.html builds its select from the ": name, name" part of this help.
	flag.StringVar(&cfg.ListName, "list", "GaryImm", "built-in target list: "+strings.Join(catalog.Lists, ", "))
	flag.StringVar(&cfg.TargetsFile, "targets", "", "custom uptonight-style targets YAML file (overrides -list)")
	flag.StringVar(&cfg.SkipFile, "skip", "", "file of target names to leave out, one per line (e.g. already imaged), # comments; case and spaces do not matter")
	flag.StringVar(&types, "type", "", "keep only these object classes, comma-separated: "+strings.Join(catalog.Classes, ", ")+" (emission: what the line filters help; default all)")
	flag.StringVar(&scopeName, "scope", "", "frame for a smart telescope, its optics and camera: "+scopeChoices())
	flag.StringVar(&fov, "fov", "", "frame for another telescope: field of view WxH in degrees up to 180, e.g. 2.1x1.4 (default Origin; -scale optional)")
	flag.Float64Var(&focal, "focal", 0, "frame for another telescope: focal length in mm; with -sensor sets the field of view (instead of -fov), with -pixel the pixel scale (instead of -scale)")
	flag.StringVar(&camera, "camera", "", "frame for another telescope (with -focal) by its camera's sensor, setting -sensor, -pixel, -qe and -read-noise (with -mono its mono version): "+strings.Join(CameraNames(), ", "))
	flag.StringVar(&sensor, "sensor", "", "frame for another telescope: sensor size WxH in mm, e.g. 23.5x15.6; with -focal, sets the field of view instead of -fov")
	flag.Float64Var(&cfg.Scale, "scale", 0, "frame for another telescope: pixel scale in arc seconds per pixel, 0.01-1000 (Origin default 1.23; -fov optional)")
	flag.Float64Var(&pixel, "pixel", 0, "frame for another telescope: camera pixel size in µm; with -focal, sets the pixel scale instead of -scale")
	flag.Float64Var(&minPx, "min-px", 200, "with framing, minimum object size in pixels; 0 or more")
	flag.IntVar(&cfg.Mosaic, "mosaic", 1, "with framing, most mosaic panels per object, 1-25; an object's score is divided among its panels")
	flag.BoolVar(&cfg.Rotate, "rotate", false, "with framing, the camera can be rotated to frame the major axis along the long side, so the minor axis counts (not the Origin)")
	flag.Float64Var(&cfg.Aperture, "aperture", 0, "with framing, the telescope's aperture in mm, for the sub-exposure estimate (SUBS); Origin 152, -scope's")
	flag.Float64Var(&cfg.Obstruction, "obstruction", 0, "with framing, the central obstruction's diameter in mm; Origin Mark II 77, -scope's, otherwise 0 when -aperture is given")
	flag.Float64Var(&cfg.Sub, "sub", 0, "with framing, the sub-exposure length in seconds, 0.001-3600; Origin 10, -scope's")
	flag.Float64Var(&cfg.QE, "qe", 0, "with framing, the camera's peak quantum efficiency, above 0 and at most 1; Origin (IMX678) 0.83")
	flag.Float64Var(&cfg.ReadNoise, "read-noise", 0, "with framing, the camera's read noise in electrons, 0 or more; Origin 0.97")
	flag.Float64Var(&cfg.Throughput, "throughput", 0, "with framing, the share of the light the telescope's optics pass, above 0 and at most 1; a refractor 0.88, a prime-focus mirror (Origin) 0.81, another telescope (-aperture) 0.85")
	flag.Float64Var(&cfg.Dark, "dark", 0, "with framing, the camera's dark current in e⁻/s per pixel at a sensor temperature of 25 °C, doubling every 5 °C; the -camera or -scope chip's from its maker's chart, else 0.16")
	flag.Float64Var(&cfg.SensorTemp, "sensor-temp", 0, "with framing, a cooled camera's sensor temperature in °C, -60 to 60; without it the sensor is uncooled, 10 °C above the forecast air")
	flag.Float64Var(&cfg.Reject, "reject", 0.15, "with framing, the share of subs discarded (tracking, satellites, wind), 0-0.9, which tonight's time must make up")
	flag.StringVar(&stack, "stack", "", "with framing, how the subs are stacked: mean (equal weights, as smart telescopes' live stacks; the default with -scope) or weighted (by each sub's SNR², as PixInsight or Siril can; the default otherwise)")
	flag.StringVar(&mount, "mount", "", "with framing, the mount: altaz (its field turns through each sub, so a minute whose rotation trails the object's edge by more than a star's width is not observable), derotated (alt-az with a field derotator, as Draco and the ORBIS 60EDX: none) or eq (equatorial, or on a wedge: none); -scope's by default, else eq")
	flag.Float64Var(&cfg.Residual, "residual", 0.002, "with framing, what flats and gradient removal leave of the sky in the stack, a share 0-0.05, which caps its SNR however many subs (0 = none)")
	flag.BoolVar(&cfg.Mono, "mono", false, "with framing, the camera is monochrome, so every pixel sees every filter band; not with -scope, whose cameras are colour")
	flag.StringVar(&cfg.Filter, "filter", "", "filters you own, comma-separated: for emission-line targets dual (Ha + OIII at once), tri (+ Hb), quad (+ Hb and SII), ha, oiii, sii; broadband light pollution filters cls (Astronomik CLS) and lpro (Optolong L-Pro) for any target; each target is scored without and with each worn all night, and FILTER shows the one that gains it most (with framing, the one SUBS needs fewest subs through) (default none; with -scope its built-in filter: dual on the Seestars, the DWARF 3 and mini and the Draco)")
	flag.Float64Var(&cfg.FilterNM, "filter-nm", 7, "with -filter, the width of each line filter band in nm, 1-30; without it a -scope's built-in filter has its own")
	flag.BoolVar(&filterStd, "filter-std", false, "with -filter, the line filters are standard ones, whose bands shift blue in a fast beam: -scope's (the Origin's f/2.2), or -focal over -aperture (off: made for the beam)")
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
	flag.BoolVar(&cfg.NoSolar, "no-solar", false, "skip the NOAA solar flux lookup that scales the airglow with the solar cycle")
	flag.Float64Var(&cfg.CometMag, "comet-mag", 12, "include comets brighter than this total visual magnitude")
	flag.BoolVar(&cfg.NoTUI, "no-tui", false, "print the tables on a terminal too, instead of the interactive view (which runs only when both input and output are a terminal)")
	version := flag.Bool("version", false, "print the version and exit")
	setUsage()
	flag.Parse()
	// flag stops at the first argument that is not a flag, so everything
	// after a stray word (-rotate true: a bool takes -rotate=false) would be
	// dropped without a word.
	if flag.NArg() > 0 {
		return Config{}, fmt.Errorf("%w: unexpected argument %q, the flags after it were not read (a true/false flag takes -flag=false)", errInvalidFlag, flag.Arg(0))
	}
	if *version {
		return Config{Version: true}, nil
	}

	var err error
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
	if err := cfg.setTypes(types); err != nil {
		return Config{}, err
	}

	base, err := cfg.framing(set, frameFlags{scope: scopeName, camera: camera, fov: fov, sensor: sensor, focal: focal, pixel: pixel, minPx: minPx})
	if err != nil {
		return Config{}, err
	}

	cfg.builtIn(set, base)
	if err := cfg.validateFlags(set); err != nil {
		return Config{}, err
	}
	cfg.MeanStack = stack == "mean" || stack == "" && cfg.Scope != ""
	if stack != "" && stack != "mean" && stack != "weighted" {
		return Config{}, fmt.Errorf("%w: -stack must be mean or weighted", errInvalidFlag)
	}
	if err := cfg.setMount(set, mount, base); err != nil {
		return Config{}, err
	}
	if err := cfg.beam(set, filterStd, focal, base); err != nil {
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

// Filters are the -filter choices and the lines each passes; a multi-band
// filter passes its lines at once, onto the same frame.
var Filters = map[string][]catalog.Line{
	filterDual: {catalog.Ha, catalog.OIII},
	"tri":      {catalog.Ha, catalog.Hb, catalog.OIII},
	"quad":     {catalog.Ha, catalog.Hb, catalog.OIII, catalog.SII},
	"ha":       {catalog.Ha},
	"oiii":     {catalog.OIII},
	"sii":      {catalog.SII},
}

// Broadband are the broadband light-pollution -filter choices and their
// passbands in nm, within atmos.CameraLo–CameraHi. cls is Astronomik's
// published CLS (Optolong's CLS and IDAS's LPS-D2 are close); lpro the
// Optolong L-Pro (IDAS LPS-D1/P2/D3 and Baader's Moon & Skyglow alike),
// passing all but notches at the Hg 435.8/546.1/577-578 and Na 589/598/616
// lines.
// ponytail: the L-Pro's edges are read off Optolong's reference-only curve,
// ±5 nm; replace them with measured edges if any are published.
var Broadband = map[string][][2]float64{
	"cls":  {{450, 520}, {640, 690}},
	"lpro": {{400, 430}, {440, 540}, {555, 575}, {625, 700}},
}

// LineFilter reports whether -filter lists a line filter (Filters), the
// kind -filter-nm sizes; broadband ones have passbands of their own.
func (cfg *Config) LineFilter() bool {
	return slices.ContainsFunc(cfg.FilterNames(), func(f string) bool { return Filters[f] != nil })
}

// BandWidths returns each line filter band's width by line: BandNM's, else
// FilterNM.
func (cfg *Config) BandWidths() [catalog.NumLines]float64 {
	w := cfg.BandNM
	for l := range w {
		if w[l] == 0 {
			w[l] = cfg.FilterNM
		}
	}

	return w
}

// BuiltInBands reports whether the line filter bands are a built-in
// filter's (BandNM), not -filter-nm's alone.
func (cfg *Config) BuiltInBands() bool { return cfg.BandNM != [catalog.NumLines]float64{} }

// FilterNames returns the -filter choices in their order; nil for none.
func (cfg *Config) FilterNames() []string {
	if cfg.Filter == "" {
		return nil
	}

	return strings.Split(cfg.Filter, ",")
}

// SunAlt returns the Sun altitude (degrees) below which -twilight counts as night.
func (cfg *Config) SunAlt() float64 {
	if cfg.Twilight == "nautical" {
		return astro.Nautical
	}

	return astro.Astronomical
}

// parseTypes reads -type: comma-separated catalog.Classes, any case, each
// once; empty means all (nil).
func parseTypes(s string) ([]string, error) {
	var out []string
	for c := range strings.SplitSeq(s, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		switch {
		case c == "":
		case !slices.Contains(catalog.Classes, c):
			return nil, fmt.Errorf("%w: -type %q, want %s", errInvalidFlag, c, strings.Join(catalog.Classes, ", "))
		case !slices.Contains(out, c):
			out = append(out, c)
		}
	}

	return out, nil
}

// Wanted reports whether a target is of a -type class; without -type every
// target is.
func (cfg *Config) Wanted(tg catalog.Target) bool {
	return cfg.Types == nil || slices.ContainsFunc(cfg.Types, tg.Is)
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
// panels first. Without Rotate the fit depends on a grid's short side alone,
// so of the grids sharing one only the fewest-panelled, the first here, can
// win, and the rest are left out.
func (cfg *Config) Mosaics() []Mosaic {
	var out []Mosaic
	n := max(1, cfg.Mosaic)
	for c := 1; c <= n; c++ {
		for r := 1; c*r <= n; r++ {
			out = append(out, Mosaic{
				Cols: c, Rows: r,
				W: cfg.FOVLong * (float64(c) - float64(c-1)*mosaicOverlap),
				H: cfg.FOVShort * (float64(r) - float64(r-1)*mosaicOverlap),
			})
		}
	}
	slices.SortStableFunc(out, func(a, b Mosaic) int { return cmp.Compare(a.Panels(), b.Panels()) })
	if cfg.Rotate {
		return out
	}
	seen := map[float64]bool{}

	return slices.DeleteFunc(out, func(m Mosaic) bool {
		short := min(m.W, m.H)
		dup := seen[short]
		seen[short] = true

		return dup
	})
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

// builtIn takes -scope's built-in filter (Scope.Filter) as -filter's
// default, and its bands' widths unless -filter-nm gives one or -filter
// lists another line filter: the widths are by line, and an added Hα or
// tri-band filter's are not the built-in one's (broadband ones have their
// own passbands).
func (cfg *Config) builtIn(set map[string]bool, base Scope) {
	if cfg.Scope == "" || base.Filter == "" {
		return
	}
	if !set["filter"] {
		cfg.Filter = base.Filter
	}
	for f := range strings.SplitSeq(strings.ToLower(cfg.Filter), ",") {
		if f = strings.TrimSpace(f); Filters[f] != nil && f != base.Filter {
			return
		}
	}
	if !set["filter-nm"] {
		cfg.BandNM = base.Bands
	}
}

// setTypes sets Types from -type and turns the comet fetch off when comets
// cannot be kept; -type comet alone with -no-comets keeps nothing.
func (cfg *Config) setTypes(types string) error {
	var err error
	if cfg.Types, err = parseTypes(types); err != nil {
		return err
	}
	if cfg.NoComets && slices.Equal(cfg.Types, []string{"comet"}) {
		return fmt.Errorf("%w: -type comet with -no-comets keeps nothing", errInvalidFlag)
	}
	if cfg.Types != nil && !slices.Contains(cfg.Types, "comet") {
		cfg.NoComets = true
	}

	return nil
}

// frameFlags are the frame flags framing reads.
type frameFlags struct {
	scope, camera, fov, sensor string
	focal, pixel, minPx        float64
}

// framing sets the frame and the sub-exposure camera from the frame flags and
// returns the base scope, whose values the flags not given take.
func (cfg *Config) framing(set map[string]bool, f frameFlags) (Scope, error) {
	// Framing (-scope, -fov or -focal/-sensor, -scale or
	// -focal/-pixel) replaces the size defaults; explicit -size-min/-size-max
	// still win. Unset frame values default to the scope's, else the
	// Origin's. frameFOV rejects -sensor and -pixel without -focal, and
	// -focal without either.
	base, err := cfg.applyScope(set, f.scope)
	if err != nil {
		return Scope{}, err
	}
	preset, err := cfg.applyCamera(set, f.camera, &f.sensor, &f.pixel)
	if err != nil {
		return Scope{}, err
	}
	fovSet, scaleSet := set["fov"] || set["sensor"], set["scale"] || set["pixel"]
	cfg.Framing = cfg.Scope != "" || fovSet || scaleSet
	if cfg.FOVLong, cfg.FOVShort, err = frameFOV(set, f.fov, f.sensor, f.focal, base); err != nil {
		return Scope{}, err
	}
	if cfg.Scale, err = frameScale(set, cfg.Scale, f.pixel, f.focal, base); err != nil {
		return Scope{}, err
	}
	if f.minPx < 0 {
		return Scope{}, fmt.Errorf("%w: -min-px must be 0 or more", errInvalidFlag)
	}
	// Only a frame the user fully specified (-fov or -focal/-sensor, and
	// -scale or -focal/-pixel) can be checked against a sensor.
	// Deliberate: checking a lone -fov against the Origin's default scale (or
	// a lone -scale against its field) rejected valid wide fields such as
	// -fov 10x7, so a lone -fov typed in arc minutes is not caught (parseFOV
	// only bounds it at 180°).
	if px, _ := cfg.FramePx(); fovSet && scaleSet && !(px <= maxSensorPx) {
		return Scope{}, fmt.Errorf("%w: frame of %.0f px across is not a sensor; -fov is in degrees, -scale in arcsec/px", errInvalidFlag, px)
	}
	if err := cfg.frameSizes(set, f.minPx); err != nil {
		return Scope{}, err
	}
	// -focal on a scope is other optics, and a -pixel unlike its own
	// another camera (-sensor alone is a crop of its own).
	cfg.OtherOptics = cfg.Scope != "" && set["focal"]
	cfg.OtherChip = cfg.Scope != "" && set["pixel"] && f.pixel != base.Pixel
	if cfg.OtherChip {
		// Not the scope's chip: its dark current, response and label would
		// describe the wrong sensor.
		cfg.Camera = ""
	}
	if err := cfg.camera(set, preset, base); err != nil {
		return Scope{}, err
	}
	cfg.ApertureSet = set["aperture"] || cfg.Scope != "" && !cfg.OtherOptics

	return base, nil
}

// frameSizes checks -mosaic and -rotate, and with framing replaces the size
// defaults by -min-px up to the largest object a mosaic holds; explicit
// -size-min/-size-max still win.
func (cfg *Config) frameSizes(set map[string]bool, minPx float64) error {
	if cfg.Mosaic < 1 || cfg.Mosaic > maxMosaic {
		return fmt.Errorf("%w: -mosaic must be 1 to %d panels", errInvalidFlag, maxMosaic)
	}
	// Judged on the values, so an explicit default (-mosaic 1, -rotate=false,
	// as the web form's shared links carry) asks for nothing.
	if !cfg.Framing && (cfg.Mosaic > 1 || cfg.Rotate) {
		return fmt.Errorf("%w: -mosaic and -rotate need framing (-scope, -fov, -scale or -focal)", errInvalidFlag)
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
		return fmt.Errorf("%w: -size-max %.1f' exceeds the largest frame side an object can use, %.1f'; larger objects cannot be framed, and with -rotate only an object with a known minor axis uses the long side",
			errInvalidFlag, cfg.SizeMax, maxSize)
	}

	return nil
}

// setMount sets Mount from -mount, else base's with -scope, else MountEq,
// and AltAz from it. -mount altaz without -scope needs -sub: the field
// rotation grows with the sub, and the Origin's 10 s, which it would
// otherwise take, says nothing of another telescope's.
func (cfg *Config) setMount(set map[string]bool, mount string, base Scope) error {
	switch {
	case mount == "" && cfg.Scope != "":
		mount = base.Mount
	case mount == "":
		mount = MountEq
	case mount != MountAltAz && mount != MountDerotated && mount != MountEq:
		return fmt.Errorf("%w: -mount must be %s, %s or %s", errInvalidFlag, MountAltAz, MountDerotated, MountEq)
	case mount == MountAltAz && cfg.Scope == "" && !set["sub"]:
		return fmt.Errorf("%w: -mount altaz without -scope needs -sub, the length its field rotation is judged over", errInvalidFlag)
	}
	cfg.Mount, cfg.AltAz = mount, mount == MountAltAz

	return nil
}

// cameraFlags are the sub-exposure flags, besides -mono, that need framing.
// Their order is -h's ("Camera and exposure").
var cameraFlags = []string{"aperture", "obstruction", "throughput", "qe", "read-noise", "dark", "sensor-temp", "sub", "reject", "stack", "mount", "residual"}

// camera checks the sub-exposure flags, which need framing like -mosaic,
// and fills the base scope's values for those not given (its camera's QE
// and read noise, 0 for none, which leaves SUBS out), -camera's preset's QE
// and read noise when given; -aperture alone describes another telescope,
// so its obstruction is then 0.
func (cfg *Config) camera(set map[string]bool, preset *Variant, base Scope) error {
	given := cfg.Mono || slices.ContainsFunc(cameraFlags, func(f string) bool { return set[f] })
	if !cfg.Framing {
		if given {
			return fmt.Errorf("%w: -aperture, -obstruction, -sub, -qe, -read-noise, -mono, -throughput, -dark, -sensor-temp, -reject, -stack, -mount and -residual need framing (-scope, -fov, -scale or -focal)", errInvalidFlag)
		}

		return nil
	}
	for _, d := range []struct {
		name   string
		v      *float64
		origin float64
	}{
		{"aperture", &cfg.Aperture, base.Aperture},
		{"obstruction", &cfg.Obstruction, base.Obstruction},
		{"sub", &cfg.Sub, base.Sub},
		{"qe", &cfg.QE, 0},
		{"read-noise", &cfg.ReadNoise, 0},
	} {
		if !set[d.name] {
			*d.v = d.origin
		}
	}
	if v := base.variant(); v != nil && preset == nil {
		preset = v
	}
	if err := modelless(set, preset, base); err != nil {
		return err
	}
	if preset != nil {
		if !set["qe"] {
			cfg.QE = preset.QE
		}
		if !set["read-noise"] {
			cfg.ReadNoise = preset.ReadNoise
		}
	}
	if set["aperture"] && !set["obstruction"] {
		cfg.Obstruction = 0
	}
	switch {
	case cfg.Obstruction < 0 || cfg.Aperture <= cfg.Obstruction:
		return fmt.Errorf("%w: -aperture must be above -obstruction, which must be 0 or more", errInvalidFlag)
	case cfg.Sub < minSub || cfg.Sub > maxSub:
		return fmt.Errorf("%w: -sub must be %g to %g seconds", errInvalidFlag, minSub, maxSub)
	case set["qe"] && (cfg.QE <= 0 || cfg.QE > 1):
		return fmt.Errorf("%w: -qe must be above 0 and at most 1", errInvalidFlag)
	case cfg.ReadNoise < 0:
		return fmt.Errorf("%w: -read-noise must be 0 or more", errInvalidFlag)
	}

	if err := cfg.losses(set, base); err != nil {
		return err
	}
	if need := cfg.subsNeeds(set); len(need) > 0 {
		cfg.SubsNeeds, cfg.QE = strings.Join(need, " and "), 0
	}

	return nil
}

// subsNeeds returns the flags SUBS lacks (Config.SubsNeeds). Framing
// without -scope describes another telescope: the base scope's aperture,
// obstruction and throughput, or its sensor's QE, read noise and colour
// response, would give its SUBS another instrument's counts, so without
// -aperture and -camera there are none (QE 0). A mono camera with -qe and
// -read-noise needs no -camera: it has no colour response to borrow. With
// -scope, OtherOptics lack an aperture and OtherChip a QE and read noise,
// which the scope's would stand for.
func (cfg *Config) subsNeeds(set map[string]bool) []string {
	var need []string
	if cfg.Scope == "" {
		if !set["aperture"] {
			need = append(need, "-aperture")
		}
		if !set["camera"] && (!cfg.Mono || !set["qe"] || !set["read-noise"]) {
			need = append(need, "-camera")
		}

		return need
	}
	if cfg.OtherOptics && !set["aperture"] {
		need = append(need, "-aperture")
	}
	if cfg.OtherChip && (!set["qe"] || !set["read-noise"]) {
		need = append(need, "-qe", "-read-noise")
	}

	return need
}

// losses fills and checks what the sub-exposure estimate loses on the way:
// the optics' throughput (the base scope's by design, another telescope's
// with -aperture), the dark current (-camera's or -scope's chip's, else
// typicalDark: without either the sensor is not the Origin's), -sensor-temp,
// -reject and -residual.
func (cfg *Config) losses(set map[string]bool, base Scope) error {
	if !set["throughput"] {
		cfg.Throughput = base.throughput()
		if set["aperture"] {
			cfg.Throughput = throughputOther
		}
	}
	if !set["dark"] {
		cfg.Dark = cmp.Or(Cameras[cfg.Camera].Dark, typicalDark)
	}
	cfg.SensorTempSet = set["sensor-temp"]
	switch {
	case cfg.Throughput <= 0 || cfg.Throughput > 1:
		return fmt.Errorf("%w: -throughput must be above 0 and at most 1", errInvalidFlag)
	case cfg.Dark < 0 || cfg.Dark > maxDark:
		return fmt.Errorf("%w: -dark must be 0 to %g e⁻/s per pixel", errInvalidFlag, maxDark)
	case cfg.SensorTemp < -60 || cfg.SensorTemp > 60:
		return fmt.Errorf("%w: -sensor-temp must be -60 to 60 °C", errInvalidFlag)
	case cfg.Reject < 0 || cfg.Reject > 0.9:
		return fmt.Errorf("%w: -reject must be 0 to 0.9", errInvalidFlag)
	case cfg.Residual < 0 || cfg.Residual > 0.05:
		return fmt.Errorf("%w: -residual must be 0 to 0.05", errInvalidFlag)
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

// beam sets FRatio and Obstructed for -filter-std: the base scope's (the
// Origin's f/2.2 without one), or another telescope's from -focal and
// -aperture (camera validated -aperture against -obstruction); without
// -filter-std, 0.
func (cfg *Config) beam(set map[string]bool, filterStd bool, focal float64, base Scope) error {
	switch {
	case !filterStd:
		return nil
	case !cfg.LineFilter():
		return fmt.Errorf("%w: -filter-std needs a line filter in -filter (dual, tri, quad, ha, oiii or sii)", errInvalidFlag)
	case set["focal"] != set["aperture"]:
		return fmt.Errorf("%w: -filter-std takes the beam from -focal and -aperture together, or with neither from the %s's f/%.1f", errInvalidFlag, base.Name, base.Focal/base.Aperture)
	case set["focal"]:
		cfg.FRatio, cfg.Obstructed = focal/cfg.Aperture, cfg.Obstruction/cfg.Aperture
	case base.NoSubs && !set["obstruction"]:
		// A ring of the beam's steepest rays is missing; none would be guessed.
		return fmt.Errorf("%w: the %s's central obstruction is unpublished; give -obstruction (mm) with -filter-std", errInvalidFlag, base.Name)
	default:
		obstruction := base.Obstruction
		if set["obstruction"] {
			obstruction = cfg.Obstruction
		}
		cfg.FRatio, cfg.Obstructed = base.Focal/base.Aperture, obstruction/base.Aperture
	}

	return nil
}

// modelless checks the camera flags of a scope without a camera model, whose
// SUBS takes both from the flags: a missing read noise would be 0 e⁻, and an
// unpublished obstruction none, both optimistic.
func modelless(set map[string]bool, preset *Variant, base Scope) error {
	if preset != nil || !set["qe"] && !set["read-noise"] {
		return nil
	}
	switch {
	case set["qe"] != set["read-noise"]:
		return fmt.Errorf("%w: %s has no camera model; give -qe and -read-noise together", errInvalidFlag, base.Name)
	case base.NoSubs && !set["obstruction"] && !set["aperture"]:
		return fmt.Errorf("%w: %s's central obstruction is unpublished; give -obstruction (mm) with -qe and -read-noise", errInvalidFlag, base.Name)
	}

	return nil
}

// frameFOV returns the frame's long and short sides in arc minutes from -fov,
// or -focal with -sensor, or the base scope's sensor at -focal (with -pixel
// alone), or the base scope's field when none is given. It also validates
// -focal for frameScale.
func frameFOV(set map[string]bool, fov, sensor string, focal float64, base Scope) (float64, float64, error) {
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
	case set["focal"]:
		// The scope's field at its own focal length would not match the
		// scale -pixel gives at this one.
		return fovSide(float64(base.PxLong)*base.Pixel/1000, focal), fovSide(float64(base.PxShort)*base.Pixel/1000, focal), nil
	}

	long, short := base.fov()

	return long, short, nil
}

// frameScale returns the pixel scale in arc seconds per pixel from -scale, or
// -pixel (µm, else the base scope's with -focal) at -focal (mm, validated by
// frameFOV), or the base scope's.
func frameScale(set map[string]bool, scale, pixel, focal float64, base Scope) (float64, error) {
	switch {
	case set["scale"] && set["pixel"]:
		return 0, fmt.Errorf("%w: give either -scale or -focal with -pixel, not both", errInvalidFlag)
	case set["pixel"] && pixel <= 0:
		return 0, fmt.Errorf("%w: -pixel must be above 0 µm", errInvalidFlag)
	case set["pixel"] || !set["scale"] && set["focal"]:
		// -focal with -sensor alone keeps the base scope's pixels, as -pixel
		// alone keeps its sensor: the scale must follow the focal length.
		if !set["pixel"] {
			pixel = base.Pixel
		}
		if scale = pixelScale(pixel, focal); scale < minScale || scale > maxScale {
			return 0, fmt.Errorf("%w: -pixel %g µm at -focal %g mm is %.3g\"/px, outside %g-%g", errInvalidFlag, pixel, focal, scale, minScale, maxScale)
		}
	case !set["scale"]:
		return base.scale(), nil
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
	ws, hs, ok := strings.Cut(strings.ToLower(s), "x") // 2X1 too
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
	case cfg.Filter != "" && slices.ContainsFunc(cfg.FilterNames(), func(f string) bool { return Filters[f] == nil && Broadband[f] == nil }):
		return fmt.Errorf("%w: -filter must be dual, tri, quad, ha, oiii, sii, cls or lpro, or a comma-separated list of them", errInvalidFlag)
	case cfg.Filter != "" && (cfg.FilterNM < 1 || cfg.FilterNM > 30):
		return fmt.Errorf("%w: -filter-nm must be 1 to 30 nm", errInvalidFlag)
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

// validateFlags normalises the text flags (-twilight, -filter), rejects
// -filter-nm without -filter, and validates the result.
func (cfg *Config) validateFlags(set map[string]bool) error {
	cfg.Twilight = strings.ToLower(strings.TrimSpace(cfg.Twilight))
	raw := cfg.Filter
	var filters []string
	for f := range strings.SplitSeq(strings.ToLower(cfg.Filter), ",") {
		if f = strings.TrimSpace(f); !slices.Contains(filters, f) {
			filters = append(filters, f)
		}
	}
	cfg.Filter = strings.Join(filters, ",")               // "" stays "", and an empty item is an unknown filter
	if cfg.Filter == "" && strings.TrimSpace(raw) != "" { // only commas: every item empty, joined back to ""
		return fmt.Errorf("%w: -filter %q names no filter", errInvalidFlag, raw)
	}
	if set["filter-nm"] && !cfg.LineFilter() {
		return fmt.Errorf("%w: -filter-nm needs a line filter in -filter (dual, tri, quad, ha, oiii or sii); cls and lpro have fixed passbands", errInvalidFlag)
	}

	return cfg.validate()
}

// validate rejects numeric flags outside their meaningful range.
func (cfg *Config) validate() error {
	if err := cfg.validateSky(); err != nil {
		return err
	}
	switch {
	// NaN is Parse's "not given" sentinel here, not user input (visited rejects that).
	case math.IsNaN(cfg.Lat) || math.IsNaN(cfg.Lon):
		return fmt.Errorf("%w: -lat and -lon are required", errInvalidFlag)
	case math.Abs(cfg.Lat) > 90:
		return fmt.Errorf("%w: -lat must be between -90 and 90", errInvalidFlag)
	case math.Abs(cfg.Lon) > 180:
		return fmt.Errorf("%w: -lon must be between -180 and 180", errInvalidFlag)
	case cfg.Plan != 0 && cfg.Plan < 10*time.Minute:
		return fmt.Errorf("%w: -plan must be at least 10m", errInvalidFlag)
	case cfg.Plan%time.Minute != 0:
		return fmt.Errorf("%w: -plan must be a whole number of minutes", errInvalidFlag)
	case cfg.MinRun < 0:
		return fmt.Errorf("%w: -min-run must be 0 or more", errInvalidFlag)
	case cfg.Top < 1:
		return fmt.Errorf("%w: -n must be at least 1", errInvalidFlag)
	case cfg.JSON && cfg.CSV:
		return fmt.Errorf("%w: -json and -csv cannot be combined", errInvalidFlag)
	// Scoring compares the limits as sines, which only orders altitudes up
	// to 90°; below the horizon nothing is observable (atmos.Airmass clamps
	// there, so a negative -alt-min would score it at airmass 38).
	case cfg.AltMin < 0 || cfg.AltMax > 90:
		return fmt.Errorf("%w: -alt-min and -alt-max must be between 0 and 90", errInvalidFlag)
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
