// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package output prints the report: header, forecast table, night plan and
// ranked targets, with optional ANSI colors.
package output

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
	"github.com/fatih/color"
)

// Header prints the site, window, Moon and sky; place is the reverse
// geocoded name, empty if unknown.
func Header(cfg *config.Config, s *scoring.Sky, place string) {
	label := func(s string) string { return paint("01", s) }
	if place != "" {
		place = " (" + place + ")"
	}
	fmt.Printf("%s %.4f, %.4f%s, %s\n", label("Location:"), cfg.Lat, cfg.Lon, place, cfg.Loc)
	layout := clockLayout(s)
	night := ""
	switch {
	case !s.Start.Equal(s.Night[0]) || !s.End.Equal(s.Night[1]):
		night = fmt.Sprintf(", %s night %s - %s", cfg.Twilight, clock(s, s.Night[0]), clock(s, s.Night[1]))
	case cfg.Twilight != "astronomical":
		night = ", " + cfg.Twilight + " night"
	}
	fmt.Printf("%s %s - %s (%s%s)\n", label("Window:  "), s.Start.Format("2006-01-02 "+layout), s.End.Format("2006-01-02 "+layout),
		s.End.Sub(s.Start).Round(time.Minute), night)
	fmt.Printf("%s %s illuminated, min separation %.0f°, %s\n", label("Moon:    "), paint(scale(s.Illum, 0.3, 0.7), fmt.Sprintf("%.0f%%", s.Illum*100)),
		s.MoonSep, moonText(s))
	fmt.Printf("%s %.1f' - %.1f'\n", label("Size:    "), cfg.SizeMin, cfg.SizeMax)
	if cfg.MinRun > 0 {
		fmt.Printf("%s at least %s observable without a break\n", label("Min run: "), cfg.MinRun)
	}
	if cfg.RASet || cfg.DecSet {
		ra, dec := position(cfg.RA, cfg.Dec)
		var region []string
		if cfg.RASet {
			region = append(region, "RA "+ra)
		}
		if cfg.DecSet {
			region = append(region, "Dec "+dec)
		}
		fmt.Printf("%s within %g° of %s\n", label("Region:  "), cfg.Tol, strings.Join(region, ", "))
	}
	targetsDesc := cfg.ListName
	if cfg.TargetsFile != "" {
		targetsDesc = cfg.TargetsFile
	}
	switch {
	case cfg.NoComets:
		targetsDesc += ", comets off"
	case s.CometsLost:
		targetsDesc += ", comets unavailable"
	default:
		targetsDesc += fmt.Sprintf(", %d comets brighter than mag %.1f", s.Comets, cfg.CometMag)
	}
	if cfg.SkipFile != "" {
		targetsDesc += fmt.Sprintf(", skipping %d names from %s", len(cfg.Skip), cfg.SkipFile)
	}
	fmt.Printf("%s %s\n", label("Targets: "), targetsDesc)
	if cfg.Framing {
		pxLong, pxShort := cfg.FramePx()
		fmt.Printf("%s %.2f° x %.2f°, %.2f\"/px (%.0f x %.0f px)\n", label("Frame:   "), cfg.FOVLong/60, cfg.FOVShort/60,
			cfg.Scale, pxLong, pxShort)
	}
	if len(cfg.Horizon) > 0 {
		fmt.Printf("%s %d points, %.0f° - %.0f°\n", label("Horizon: "), len(cfg.Horizon),
			slices.MinFunc(cfg.Horizon, func(a, b [2]float64) int { return cmp.Compare(a[1], b[1]) })[1],
			slices.MaxFunc(cfg.Horizon, func(a, b [2]float64) int { return cmp.Compare(a[1], b[1]) })[1])
	}
	skyDesc := fmt.Sprintf("Bortle not set (dark sky, %.1f mag/arcsec²)", atmos.RefZenithMag)
	if cfg.SkySet { // -bortle 0 or -sqm 0: asked for, not defaulted
		skyDesc = fmt.Sprintf("Bortle/SQM off (dark sky, %.1f mag/arcsec²)", atmos.RefZenithMag)
	}
	switch {
	case cfg.SQM != 0:
		b := atmos.BortleClass(cfg.SQM)
		src := ""
		if cfg.SQMSource != "" {
			src = cfg.SQMSource + ", "
		}
		skyDesc = paint(scale(float64(b), 4, 7), fmt.Sprintf("SQM %.2f mag/arcsec²", cfg.SQM)) + fmt.Sprintf(" (%s≈ Bortle %d)", src, b)
	case cfg.Bortle > 0:
		skyDesc = paint(scale(float64(cfg.Bortle), 4, 7), fmt.Sprintf("Bortle %d", cfg.Bortle)) +
			fmt.Sprintf(" (zenith %.2f mag/arcsec²)", atmos.BortleMag[cfg.Bortle])
	}
	lo, hi := slices.Min(s.Ext), slices.Max(s.Ext)
	extDesc := fmt.Sprintf("%.2f", lo)
	if hi-lo >= 0.005 {
		extDesc += fmt.Sprintf("-%.2f", hi)
	}
	switch {
	case cfg.ExtinctionSet:
		extDesc += " (set)"
	case math.IsNaN(s.Elevation):
		extDesc += " (default)"
	case len(s.AOD) == 0:
		extDesc += fmt.Sprintf(" (elevation %.0f m, typical aerosols)", s.Elevation)
	default:
		extDesc += fmt.Sprintf(" (elevation %.0f m, aerosols)", s.Elevation)
	}
	skyDesc += ", extinction " + paint(scale(hi, 0.25, 0.4), extDesc) + " mag/airmass"
	if cfg.Filter {
		skyDesc += fmt.Sprintf(", filter k=%.2f", cfg.FilterK)
	}
	fmt.Printf("%s %s\n\n", label("Sky:     "), skyDesc)
}

// forecastFetched reports whether any forecast came back, so there is a
// forecast table to print.
func forecastFetched(s *scoring.Sky) bool { return s.Weather != nil || s.Astro != nil || s.AOD != nil }

func Weather(cfg *config.Config, s *scoring.Sky) {
	if !forecastFetched(s) {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row(w, "01", names(weatherColumns)...)
	na := paint("39", "-")
	for t := s.Start.Truncate(time.Hour); t.Before(s.End); t = t.Add(time.Hour) {
		cloud, layers, dew, wind, transp, seeing := na, na, na, na, na, na
		// The value scoring used: hourly from the aerosols or the elevation alone, else the fixed one.
		k := s.ExtinctionAt(t, cfg.Extinction)
		ext := paint(scale(k, 0.25, 0.4), fmt.Sprintf("%.2f", k))
		if h, ok := s.Weather[t.Unix()]; ok {
			cloud = paint(scale(h.Cloud, 30, 70), fmt.Sprintf("%.0f%%", h.Cloud))
			if h.Precip >= weather.RainGate/2 { // the cell is painted once either way
				cloud = paint("31", fmt.Sprintf("%.0f%% %.1fmm", h.Cloud, h.Precip))
			}
			layers = paint("39", fmt.Sprintf("%.0f/%.0f/%.0f%%", h.Low, h.Mid, h.High))
			spread := h.Temp - h.DewPoint // < 2-3 °C: dew on the optics
			dew = paint(scale(spread, 4, 2), fmt.Sprintf("%.1f°C", spread))
			wind = paint(scale(h.Gust, 20, 35), fmt.Sprintf("%.0f/%.0f km/h", h.Wind, h.Gust))
			// The upper-air estimate stands in for hours 7Timer does not cover.
			if fwhm := weather.Seeing(h); fwhm > 0 {
				seeing = paint(scale(fwhm, 2, 2.5), fmt.Sprintf("~%.1f\"", fwhm)) // 7Timer's classes 6 and 8
			}
		}
		if a, ok := s.Astro[weather.AstroKey(t)]; ok {
			transp = paint(scale(float64(a.Transparency), 3, 6), fmt.Sprintf("%d/8", a.Transparency))
			seeing = paint(scale(float64(a.Seeing), 6, 8), weather.SeeingLabel(a.Seeing))
		}
		row(w, "", paint("39", clock(s, t.In(cfg.Loc))), cloud, layers, transp, ext, seeing, dew, wind)
	}
	w.Flush()
	fmt.Println()
}

func Plan(s *scoring.Sky, slots []plan.Slot) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row(w, "01", names(planColumns)...)
	for _, p := range slots {
		when := paint("39", clock(s, p.Start)+"-"+clock(s, p.End))
		if p.Result == nil {
			na := paint("39", "-")
			row(w, "", when, paint("39", "no target"), na, na, na, na)

			continue
		}
		row(w, "", when, paint("36", p.Result.Name), paint("39", p.Result.Description), paint(typeColor(p.Result.Target), p.Result.Type),
			paint(scale(p.Score, 0.66, 0.33), fmt.Sprintf("%.2f", p.Score)),
			paint("39", fmt.Sprintf("%.0f° @ %s", p.PeakAlt, clock(s, p.PeakAt))))
	}
	w.Flush()
	fmt.Println()
}

// Results prints the target table and returns its width in columns (0 when
// empty), which the legend wraps to.
func Results(cfg *config.Config, s *scoring.Sky, results []scoring.Result) int {
	if len(results) == 0 {
		fmt.Println("No objects within constraints.")

		return 0
	}
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	row(w, "01", names(resultColumns)...)
	for i, r := range results[:min(cfg.Top, len(results))] {
		px := "-" // without framing there is no pixel scale to measure against
		if cfg.Framing {
			px = sizeText(r.Target, "%.0f", r.Size*60/cfg.Scale)
		}
		ra, dec := position(r.Position())
		row(w, "", paint("39", strconv.Itoa(i+1)), paint("36", r.Name), paint("39", r.Description), paint(typeColor(r.Target), r.Type),
			paint("39", r.Constellation), paint("39", ra), paint("39", dec), paint("39", sizeText(r.Target, "%.0f'", r.Size)),
			paint(scale(r.Foto, 0.66, 0.33), fmt.Sprintf("%.2f", r.Foto)),
			paint(scale(r.Score, 0.66, 0.33), fmt.Sprintf("%.2f", r.Score)),
			paint("39", runText(s, r)),
			paint("39", fmt.Sprintf("%.0f° @ %s", r.MaxAlt, clock(s, r.MaxAt))),
			paint(scale(r.SkyMag, 20.5, 19), fmt.Sprintf("%.1f", r.SkyMag)),
			paint("39", sbText(r)),
			paint("39", px))
	}
	w.Flush()
	width := 0
	for line := range strings.Lines(buf.String()) {
		width = max(width, utf8.RuneCountInString(strings.TrimRight(sgr.ReplaceAllString(line, ""), " \n")))
	}
	fmt.Print(buf.String())

	return width
}

// sgr matches the escapes paint writes.
var sgr = regexp.MustCompile("\x1b\\[[0-9]*m")

var UseColor bool

// runText is the OBSERVABLE column: the longest continuous observable
// stretch, with a + when there are others.
func runText(s *scoring.Sky, r scoring.Result) string {
	text := clock(s, r.RunFrom) + "-" + clock(s, r.RunTo)
	if r.Runs > 1 {
		text += "+"
	}

	return text
}

// moonText says when the Moon is up within the window: "up throughout",
// "never up", or its rises and sets in order (geometric altitude, no
// refraction: a few minutes early for a rise, late for a set).
func moonText(s *scoring.Sky) string {
	var events []string
	for i := 1; i < len(s.Grid); i++ {
		switch {
		case s.MoonAlt[i-1] <= 0 && s.MoonAlt[i] > 0:
			events = append(events, "rises "+clock(s, s.Grid[i]))
		case s.MoonAlt[i-1] > 0 && s.MoonAlt[i] <= 0:
			events = append(events, "sets "+clock(s, s.Grid[i]))
		}
	}
	switch {
	case len(events) > 0:
		return strings.Join(events, ", ")
	case s.MoonAlt[0] > 0:
		return "up throughout"
	}

	return "never up"
}

// sbText is the SB column: the object's surface brightness, ~ when estimated
// (from its magnitude and size, or a B value moved to V by a typical colour),
// - when unknown.
func sbText(r scoring.Result) string {
	sb, estimated := r.SurfaceBrightness()
	switch {
	case sb <= 0:
		return "-"
	case estimated:
		return fmt.Sprintf("~%.1f", sb)
	}

	return fmt.Sprintf("%.1f", sb)
}

// clockLayout formats clock times, with the zone abbreviation when the night
// crosses a UTC offset change (DST): a bare "02:35" is then ambiguous, and a
// block printed as 01:35-02:35 lasts two hours.
func clockLayout(s *scoring.Sky) string {
	_, duskOff := s.Night[0].Zone()
	_, dawnOff := s.Night[1].Zone()
	if duskOff != dawnOff {
		return "15:04 MST"
	}

	return "15:04"
}

func clock(s *scoring.Sky, t time.Time) string { return t.Format(clockLayout(s)) }

// typeColor is the TYPE cell's color, as the legend explains it: magenta for
// emission-line targets, yellow for comets.
func typeColor(tg catalog.Target) string {
	switch {
	case catalog.EmissionLine(tg):
		return "35"
	case tg.Track != nil:
		return "33"
	}

	return "39"
}

// position formats RA and Dec in degrees as "hh mm.m" and "+dd mm". Rounding
// happens on the whole value in tenths of a minute (RA) or arcminutes (Dec)
// so 59.96 carries into the next hour or degree instead of printing as 60.0.
func position(ra, dec float64) (string, string) {
	t := int(math.Round(math.Mod(ra+360, 360)/15*600)) % (24 * 600)
	a := int(math.Round(math.Abs(dec) * 60))
	sign := "+"
	if dec < 0 && a > 0 {
		sign = "-"
	}

	return fmt.Sprintf("%02d %04.1f", t/600, float64(t%600)/10), fmt.Sprintf("%s%02d %02d", sign, a/60, a%60)
}

// sizeText formats a size column, "-" for comets and unknown sizes.
func sizeText(tg catalog.Target, format string, v float64) string {
	if !tg.HasSize() {
		return "-"
	}

	return fmt.Sprintf(format, v)
}

// ColorTerminal reports whether stdout is a terminal that wants colors.
// Importing fatih/color also enables ANSI escape processing on the Windows
// console; its NoColor adds the TTY (and Cygwin/MSYS pty) check.
func ColorTerminal() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" && os.Getenv("CLICOLOR_FORCE") != "0" {
		return true
	}

	return !color.NoColor
}

// paint wraps s in an SGR color code. code must be exactly 2 characters so
// every painted cell grows by the same byte count and tabwriter (which counts
// escape bytes as width) keeps columns aligned.
func paint(code, s string) string {
	if !UseColor {
		return s
	}

	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// row writes tab-separated cells; a non-empty code paints every cell with it.
// Cells of one column must be painted the same number of times on every row.
func row(w io.Writer, code string, cells ...string) {
	if code != "" {
		for i := range cells {
			cells[i] = paint(code, cells[i])
		}
	}
	fmt.Fprintln(w, strings.Join(cells, "\t"))
}

// scale picks green/yellow/red for v; okAt and badAt may be in either order,
// so it works for both higher-is-better and lower-is-better values.
func scale(v, okAt, badAt float64) string {
	sign := 1.0
	if okAt < badAt {
		sign = -1
	}
	switch {
	case sign*v >= sign*okAt:
		return "32"
	case sign*v <= sign*badAt:
		return "31"
	}

	return "33"
}

// column is a table column and its legend entry; tables build their headers
// from these so the legend cannot drift from the output.
type column struct{ name, desc string }

var weatherColumns = []column{
	{"HOUR", "local hour"},
	{"CLOUD", "effective cloud cover (thin high cloud counts half), with the rain in the hour when any"},
	{"LOW/MID/HIGH", "cloud cover per layer"},
	{"TRANSP", "7Timer transparency, 1 best to 8 worst"},
	{"EXT", "atmospheric extinction, mag per airmass"},
	{"SEEING", "7Timer seeing (star FWHM); ~n\" without it: the FWHM estimated from the upper-air profile"},
	{"DEW SPREAD", "temperature minus dew point; dew forms below ~2-3 °C"},
	{"WIND/GUST", "mean wind / gusts, km/h"},
}

const sameAsTargets = "as in the target list"

var planColumns = []column{
	{"TIME", "imaging block"},
	{"NAME", sameAsTargets},
	{"DESCRIPTION", sameAsTargets},
	{"TYPE", sameAsTargets},
	{"SCORE", "the target's score within this block"},
	{"PEAK ALT", "highest altitude within the block, and when"},
}

var resultColumns = []column{
	{"#", "rank by SCORE"},
	{"NAME", "catalog designation"},
	{"DESCRIPTION", "common name; comets: magnitude, Sun distance r, Earth distance Δ"},
	{"TYPE", "object type"},
	{"CONSTELLATION", "IAU constellation, from the official boundaries"},
	{"RA", "J2000 right ascension, hh mm.m (comets: mid-window position)"},
	{"DEC", "J2000 declination, ±dd mm (comets: mid-window position)"},
	{"SIZE", "major axis in arcminutes (- unknown or comet)"},
	{"FOTO", "fraction of the window within the altitude, horizon and Moon-distance limits"},
	{"SCORE", "0-1 imaging quality: 1 = every minute observable under a perfect, pristine dark sky"},
	{"OBSERVABLE", "longest stretch within the limits without a break (+ there are others)"},
	{"MAX ALT", "highest altitude in the window, and when"},
	{"SKY", "mean sky brightness at the object, mag/arcsec² (higher is darker; no filter)"},
	{"SB", "the object's own surface brightness, mag/arcsec², set against the sky it sees: SKY, cut to k by -filter for emission-line objects or halved for star clusters without nebulosity, whose own brightness is then not used (~ estimated from the magnitude and size or from a B value and a typical colour; - unknown, scored as sky-limited)"},
	{"PX", "size in pixels at the frame's pixel scale (- without framing)"},
}

func names(cols []column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.name
	}

	return out
}

// Legend explains the columns of the tables that were printed and the colors,
// wrapped to tableWidth (legendWidth when 0).
func Legend(s *scoring.Sky, withPlan bool, tableWidth int) {
	fmt.Println()
	fmt.Println(paint("01", "Legend"))
	// One tabwriter for all sections keeps them aligned; the buffer lets the
	// padding after section titles be trimmed.
	type section struct {
		title string
		cols  []column
	}
	var sections []section
	if forecastFetched(s) {
		sections = append(sections, section{"Forecast table:", weatherColumns})
	}
	if withPlan {
		sections = append(sections, section{"Night plan:", planColumns})
	}
	sections = append(sections, section{"Targets:", resultColumns})
	// Descriptions start where tabwriter puts the second column: after the
	// widest first cell (titles included) and the padding.
	indent := 0
	for _, sec := range sections {
		indent = max(indent, len("  "+sec.title))
		for _, c := range sec.cols {
			indent = max(indent, len("    "+c.name))
		}
	}
	width := cmp.Or(tableWidth, legendWidth) - indent - legendPad
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, legendPad, ' ', 0)
	for _, sec := range sections {
		fmt.Fprintln(w, "  "+sec.title+"\t")
		for _, c := range sec.cols {
			// Continuation lines go under the first, in the same column.
			name := c.name
			for _, line := range wrap(c.desc, width) {
				fmt.Fprintf(w, "    %s\t%s\n", name, line)
				name = ""
			}
		}
	}
	w.Flush()
	for line := range strings.Lines(buf.String()) {
		fmt.Println(strings.TrimRight(line, " \n"))
	}
	fmt.Printf("  Colors: %s good, %s fair, %s poor;\n    TYPE in %s is emission-line (helped by -filter), in %s a comet.\n",
		paint("32", "green"), paint("33", "yellow"), paint("31", "red"), paint("35", "magenta"), paint("33", "yellow"))
}

// legendWidth is the width the legend wraps to without a target table;
// legendPad is the
// gap between a column name and its description.
const (
	legendWidth = 80
	legendPad   = 2
)

// wrap splits text into lines of at most width runes, breaking between words.
// A word without letters or digits, such as the legend's "-" for unknown or
// "(~", stays with the word after it.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	words := strings.Fields(text)
	for i := 0; i < len(words); i++ {
		word := words[i]
		if !strings.ContainsFunc(word, isAlnum) && i+1 < len(words) {
			i++
			word += " " + words[i]
		}
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	return append(lines, line)
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
