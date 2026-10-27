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

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/stars"
	"github.com/dkorunic/astro-recommender/internal/weather"
	"github.com/fatih/color"
	"golang.org/x/term"
)

// solarDesc is the Sky line's solar cycle part, in words: how much the
// airglow brightens or darkens the zenith, with the month's 10.7 cm flux;
// "" when the flux is unknown or the change rounds away (a bright sky).
func solarDesc(cfg *config.Config, s *scoring.Sky) string {
	d := airglowMag(s)
	if cfg.SolarFlux <= 0 || d == 0 {
		return ""
	}
	way := "brighter"
	if d > 0 {
		way = "darker"
	}

	return fmt.Sprintf(", airglow %.2f mag %s (solar flux %.0f sfu, %s)", math.Abs(d), way, cfg.SolarFlux, cfg.SolarBasis)
}

// airglowMag is the zenith's change in mag/arcsec² from s.Airglow: negative
// is brighter. Under a bright sky it rounds to 0, which %+.2f would print as
// -0.00.
func airglowMag(s *scoring.Sky) float64 {
	d := atmos.MagFromNL(s.ZenithNL) - atmos.MagFromNL(s.ZenithNL-s.Airglow)
	if math.Abs(d) < 0.005 {
		return 0
	}

	return d
}

// filterDesc is the Filter line: the -filter list and, with a line
// filter, its bands' width (-filter-nm sizes only those, not broadband ones)
// and, with -filter-std, the beam that shifts them; "" without -filter.
func filterDesc(cfg *config.Config) string {
	if cfg.Filter == "" {
		return ""
	}
	desc := strings.ReplaceAll(cfg.Filter, ",", ", ")
	if !cfg.LineFilter() {
		return desc
	}
	bands := fmt.Sprintf("%g nm line bands", cfg.FilterNM)
	if cfg.BuiltInBands() {
		widths := cfg.BandWidths()
		var each []string
		for _, l := range filterLines(cfg) {
			each = append(each, fmt.Sprintf("%s %g", lineNames[l], widths[l]))
		}
		bands = "built-in, " + strings.Join(each, ", ") + " nm"
	}
	if cfg.FRatio > 0 {
		bands += fmt.Sprintf("; standard, shifted in the f/%.1f beam", cfg.FRatio)
	}

	return desc + " (" + bands + ")"
}

// lineNames are the lines as the Filter line names them, lineKeys as JSON's
// sky.filter.bands does.
var (
	lineNames = [catalog.NumLines]string{catalog.Ha: "Hα", catalog.OIII: "[O III]", catalog.SII: "[S II]", catalog.Hb: "Hβ"}
	lineKeys  = [catalog.NumLines]string{catalog.Ha: "ha", catalog.OIII: "oiii", catalog.SII: "sii", catalog.Hb: "hb"}
)

// filterLines returns the lines the -filter choices' line filters are
// built around, each once, in line order.
func filterLines(cfg *config.Config) []catalog.Line {
	var used [catalog.NumLines]bool
	for _, f := range cfg.FilterNames() {
		for _, l := range config.Filters[f] {
			used[l] = true
		}
	}
	var ls []catalog.Line
	for l, u := range used {
		if u {
			ls = append(ls, catalog.Line(l))
		}
	}

	return ls
}

// frameDesc is the header's Frame line: the instrument first, as it is
// what the numbers describe, then the field, the scale and the options.
func frameDesc(cfg *config.Config) string {
	pxLong, pxShort := cfg.FramePx()
	lead, extra := "", ""
	if label := cmp.Or(cfg.ScopeLabel(), cfg.CameraLabel()); label != "" {
		lead = label + ", "
	}
	if cfg.Mosaic > 1 {
		extra += fmt.Sprintf(", mosaics of up to %d panels", cfg.Mosaic)
	}
	if cfg.Rotate {
		extra += ", rotatable"
	}
	switch cfg.Mount {
	case config.MountAltAz:
		extra += ", alt-az (field rotation limits large objects high up)"
	case config.MountDerotated:
		extra += ", alt-az with a field derotator"
	}
	if cfg.SubsNeeds != "" {
		extra += ", SUBS needs " + cfg.SubsNeeds
	}

	return fmt.Sprintf("%s%.2f° x %.2f°, %.2f\"/px (%.0f x %.0f px)%s", lead, cfg.FOVLong/60, cfg.FOVShort/60,
		cfg.Scale, pxLong, pxShort, extra)
}

// Header prints the site, window, Moon and sky; place is the reverse
// geocoded name, empty if unknown.
func Header(cfg *config.Config, s *scoring.Sky, place string) {
	if place != "" {
		place = " (" + place + ")"
	}
	headerLine("Location:", fmt.Sprintf("%.4f, %.4f%s, %s", cfg.Lat, cfg.Lon, place, cfg.Loc))
	layout := clockLayout(s)
	night := ""
	switch {
	case !s.Start.Equal(s.Night[0]) || !s.End.Equal(s.Night[1]):
		night = fmt.Sprintf(", %s night %s", cfg.Twilight, spanText(s, s.Night[0], s.Night[1]))
	case cfg.Twilight != "astronomical":
		night = ", " + cfg.Twilight + " night"
	}
	headerLine("Window:  ", fmt.Sprintf("%s - %s (%s%s)", s.Start.Format("2006-01-02 "+layout), s.End.Format("2006-01-02 "+layout),
		durText(s.End.Sub(s.Start)), night))
	headerLine("Moon:    ", fmt.Sprintf("%s illuminated, targets kept at least %.0f° away%s, %s", paint(scale(s.Illum, 0.3, 0.7), fmt.Sprintf("%.0f%%", s.Illum*100)),
		s.MoonSep, MoonFloor(cfg, s), moonText(s)+eclipseText(s)))
	headerLine("Size:    ", fmt.Sprintf("%.1f' - %.1f'", cfg.SizeMin, cfg.SizeMax))
	if cfg.MinRun > 0 {
		headerLine("Min run: ", fmt.Sprintf("at least %s observable without a break", durText(cfg.MinRun)))
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
		headerLine("Region:  ", fmt.Sprintf("within %g° of %s", cfg.Tol, strings.Join(region, ", ")))
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
		noun := "comets"
		if s.Comets == 1 {
			noun = "comet"
		}
		targetsDesc += fmt.Sprintf(", %d %s brighter than mag %.1f", s.Comets, noun, cfg.CometMag)
	}
	if cfg.Types != nil {
		targetsDesc += ", only " + strings.Join(cfg.Types, ", ")
	}
	if cfg.SkipFile != "" {
		targetsDesc += fmt.Sprintf(", skipping %d names from %s", len(cfg.Skip), cfg.SkipFile)
	}
	headerLine("Targets: ", targetsDesc)
	if cfg.Framing {
		headerLine("Frame:   ", frameDesc(cfg))
	}
	if len(cfg.Horizon) > 0 {
		headerLine("Horizon: ", fmt.Sprintf("%d points, %.0f° - %.0f°", len(cfg.Horizon),
			slices.MinFunc(cfg.Horizon, func(a, b [2]float64) int { return cmp.Compare(a[1], b[1]) })[1],
			slices.MaxFunc(cfg.Horizon, func(a, b [2]float64) int { return cmp.Compare(a[1], b[1]) })[1]))
	}
	skyDesc := fmt.Sprintf("dark, %.1f mag/arcsec² (default)", atmos.RefZenithMag)
	if cfg.SkySet { // -bortle 0 or -sqm 0: asked for, not defaulted
		skyDesc = fmt.Sprintf("dark, %.1f mag/arcsec² (-bortle/-sqm 0)", atmos.RefZenithMag)
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
	headerLine("Sky:     ", skyDesc+solarDesc(cfg, s))
	if f := filterDesc(cfg); f != "" {
		headerLine("Filter:  ", f)
	}
	fmt.Println()
}

// headerIndent is where header values start, after the padded label.
const headerIndent = len("Location: ")

// headerLine prints a header label and its value, the value wrapped at
// TermWidth under its own first column: a line run back under the labels
// reads as a new one.
func headerLine(name, value string) {
	lines := []string{value}
	if TermWidth > 0 {
		lines = wrap(value, max(20, TermWidth-headerIndent))
	}
	fmt.Println(paint("01", name), lines[0])
	for _, l := range lines[1:] {
		fmt.Println(strings.Repeat(" ", headerIndent) + l)
	}
}

// forecastFetched reports whether any forecast came back, so there is a
// forecast table to print.
func forecastFetched(s *scoring.Sky) bool {
	return len(s.Weather) > 0 || len(s.Astro) > 0 || len(s.AOD) > 0
}

func Weather(cfg *config.Config, s *scoring.Sky) {
	if !forecastFetched(s) {
		return
	}
	fmt.Println(paint("01", "Forecast"))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row(w, "01", names(weatherColumns)...)
	na := paint("39", "-")
	for t := s.Start.Truncate(time.Hour); !t.After(s.LastHour()); t = t.Add(time.Hour) {
		cloud, layers, dew, wind, transp, seeing, ext := na, na, na, na, na, na, na
		own := ownHour(s, t)
		if own { // the value scoring used: hourly from the aerosols or the elevation alone, else the fixed one
			k := s.ExtinctionAt(t, cfg.Extinction)
			ext = paint(scale(k, 0.25, 0.4), fmt.Sprintf("%.2f", k))
		}
		if h, ok := s.Weather[t.Unix()]; ok {
			cloud = paint(scale(h.Cloud, 30, 70), fmt.Sprintf("%.0f%%", h.Cloud))
			if h.Precip >= weather.RainGate/2 && own { // the cell is painted once either way
				cloud = paint("31", fmt.Sprintf("%.0f%% %.1fmm", h.Cloud, h.Precip))
			}
			layers = paint("39", fmt.Sprintf("%.0f/%.0f/%.0f%%", h.Low, h.Mid, h.High))
			spread := h.Temp - h.DewPoint // < 2-3 °C: dew on the optics
			dew = paint(scale(spread, 4, 2), fmt.Sprintf("%.1f°C", spread))
			// The gust scored: past the cap the forecast's is the model's, marked ~.
			g, mark := scoring.Gust(h.Gust, h.Wind), ""
			if g < h.Gust {
				mark = "~"
			}
			if own {
				wind = paint(scale(g, 20, 35), fmt.Sprintf("%.0f/%s%.0f km/h", h.Wind, mark, g))
			}
			// The upper-air estimate, as scoring's blur; 7Timer's class stands in for hours without one.
			if h.Seeing > 0 {
				seeing = paint(scale(h.Seeing, 2, 2.5), fmt.Sprintf("~%.1f\"", h.Seeing)) // 7Timer's classes 6 and 8
			}
		}
		if a, ok := s.Astro[weather.AstroKey(t)]; ok {
			transp = paint(scale(float64(a.Transparency), 3, 6), fmt.Sprintf("%d/8", a.Transparency))
			if s.Weather[t.Unix()].Seeing == 0 && own {
				seeing = paint(scale(float64(a.Seeing), 6, 8), weather.SeeingLabel(a.Seeing))
			}
		}
		row(w, "", paint("39", clock(s, t.In(cfg.Loc))), cloud, layers, transp, ext, seeing, dew, wind)
	}
	w.Flush()
	fmt.Println()
}

// ownHour reports that the minutes scored from forecast hour t's own values
// lie in the window: from End on (Sky.LastHour), an hour is shown only for
// the cloud, layers, dew and upper-air seeing the window's last minutes read
// from it (the nearest hour) and 7Timer's transparency, a 3-hour block they
// share; its extinction, wind, gust, rain and 7Timer seeing, which minutes
// take from their own hour, are left out. Weather and JSON both ask it.
func ownHour(s *scoring.Sky, t time.Time) bool { return t.Before(s.End) }

func Plan(cfg *config.Config, s *scoring.Sky, slots []plan.Slot) {
	if s.Washout() { // Results says why
		emptyPlan = true

		return
	}
	emptyPlan = !slices.ContainsFunc(slots, func(p plan.Slot) bool { return p.Result != nil })
	if emptyPlan {
		// A grid of "no target" rows says less than one line.
		fmt.Println(paint("01", "Night plan"))
		fmt.Println("No target fits any block.")
		fmt.Println()

		return
	}
	var table string
	table, _, planDropped = fit(planDropOrder, func(drop map[string]bool, desc int) (string, int, int) {
		return planTable(cfg, s, slots, drop, desc)
	})
	fmt.Println(paint("01", "Night plan"))
	fmt.Print(table)
	fmt.Println()
}

// planTable renders the plan without the columns in drop, descriptions cut
// to desc runes, and returns it, its width and the widest description.
func planTable(cfg *config.Config, s *scoring.Sky, slots []plan.Slot, drop map[string]bool, desc int) (string, int, int) {
	var buf bytes.Buffer
	widest := 0
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	cols := columns(cfg, planColumns, drop)
	row(w, "01", names(cols)...)
	types := varied(slots, func(p plan.Slot) string {
		if p.Result == nil {
			return "39"
		}

		return typeColor(p.Result.Target)
	})
	for i, p := range slots {
		// Every column's cell, in planColumns' order; cols picks those shown.
		all := []string{paint("39", spanText(s, p.Start, p.End)), paint("39", "no target")}
		for range len(planColumns) - len(all) {
			all = append(all, paint("39", "-"))
		}
		if p.Result != nil {
			d := shortDesc(p.Result.Description, desc)
			widest = max(widest, utf8.RuneCountInString(d))
			all = []string{
				all[0], paint("36", p.Result.Name), paint("39", d), paint(types[i], p.Result.Type),
				paint(scale(p.Score, 0.66, 0.33), fmt.Sprintf("%.2f", p.Score)),
				paint("39", altText(s, p.PeakAlt, p.PeakAt)), paint("39", filterText(*p.Result)),
			}
		}
		row(w, "", pick(all, planColumns, cols)...)
	}
	w.Flush()

	return buf.String(), tableWidth(buf.String()), widest
}

// pick returns the cells of row, one per column of all, for the columns
// shown.
func pick(row []string, all, shown []column) []string {
	cells := make([]string, len(shown))
	for j, c := range shown {
		cells[j] = row[slices.IndexFunc(all, func(a column) bool { return a.name == c.name })]
	}

	return cells
}

// tableWidth is the widest line of table in columns.
func tableWidth(table string) int {
	width := 0
	for line := range strings.Lines(table) {
		width = max(width, visible(strings.TrimRight(line, " \n")))
	}

	return width
}

// fit renders a table with render, which leaves out the columns in drop
// and cuts descriptions to desc runes. On a terminal narrower than the
// table it drops the column groups of order, least needed first, while the
// descriptions would otherwise go below descKeep, as a common name cut that
// far is lost; then it cuts the descriptions, down to minDesc. It returns
// the table, its width and the columns left out.
func fit(order [][]string, render func(drop map[string]bool, desc int) (string, int, int)) (string, int, map[string]bool) {
	drop := map[string]bool{}
	table, width, widest := render(drop, descWidth)
	for over := width - TermWidth; TermWidth > 0 && over > 0; over = width - TermWidth {
		if widest-over < descKeep && len(order) > 0 {
			for _, c := range order[0] {
				drop[c] = true
			}
			order = order[1:]
			table, width, widest = render(drop, descWidth)

			continue
		}
		if widest > minDesc {
			table, width, _ = render(drop, max(minDesc, widest-over))
		}

		break
	}

	return table, width, drop
}

// Results prints the target table and returns its width in columns (0 when
// empty): at most TermWidth when that is set. On a terminal narrower than
// the table, fit drops the columns of dropOrder, then cuts descriptions.
func Results(cfg *config.Config, s *scoring.Sky, results []scoring.Result) int {
	if reason := washoutReason(s); reason != "" {
		noTargets = true
		for _, l := range wrapped(washoutText(s, reason)) {
			fmt.Println(l)
		}

		return 0
	}
	noTargets = len(results) == 0
	if noTargets {
		for _, l := range wrapped(emptyText(cfg, s)) {
			fmt.Println(l)
		}

		return 0
	}
	order := dropOrder
	if cfg.Framing {
		order = framedOrder(order)
	}
	// PX carries a mosaic's grid, without which SUBS, per panel, reads short.
	if slices.ContainsFunc(results[:min(cfg.Top, len(results))], func(r scoring.Result) bool { return r.Mosaic.Panels() > 1 }) {
		order = slices.DeleteFunc(slices.Clone(order), func(g []string) bool { return slices.Contains(g, pxColumn) })
	}
	planets := midPlanets(s)
	var table string
	var width int
	table, width, dropped = fit(order, func(drop map[string]bool, desc int) (string, int, int) {
		return resultsTable(cfg, s, results, planets, drop, desc)
	})
	fmt.Println(paint("01", "Targets, ranked by SCORE"))
	fmt.Print(table)
	if TermWidth > 0 {
		width = min(width, TermWidth)
	}

	return width
}

// washoutText replaces the target table when washoutReason gives reason:
// the cloud, else the seeing, over the stretch scoring.Sky.UsableNeed asks
// for (the whole of a window shorter than scoring.MinUsable).
func washoutText(s *scoring.Sky, reason string) string {
	need := fmt.Sprintf("%d minutes in a row", s.UsableNeed())
	if s.UsableNeed() < int(scoring.MinUsable/time.Minute) {
		need += " (the whole window)"
	}
	if reason == "cloud" {
		return fmt.Sprintf("No target is observable tonight: the forecast leaves no %s at %.0f%% of a clear sky or better (cloud, rain, dew or gusts).", need, scoring.MinQuality*100)
	}

	return fmt.Sprintf(`No target is observable tonight: no %s are both clear enough and under %.0f" of forecast seeing.`, need, scoring.MaxSeeing)
}

// washoutReason is "" while some hour of the window is usable
// (scoring.Sky.Washout), else "cloud" when the weather alone rules every one
// out, "seeing" otherwise.
func washoutReason(s *scoring.Sky) string {
	switch {
	case !s.Washout():
		return ""
	case s.Clouded():
		return "cloud"
	default:
		return "seeing"
	}
}

// emptyText says that no target passed and which limits to loosen: the
// size and altitude always apply, the others only when set; the Moon's
// distance while it is up (scoring's geometric rule, which only a -filter
// loosens) is named, since under a bright Moon it is often what emptied
// the list.
func emptyText(cfg *config.Config, s *scoring.Sky) string {
	limits := []string{fmt.Sprintf("size %.1f'-%.1f'", cfg.SizeMin, cfg.SizeMax), fmt.Sprintf("altitude %.0f-%.0f°", cfg.AltMin, cfg.AltMax)}
	flags := []string{"-size-min/-size-max", "-alt-min/-alt-max"}
	if cfg.Types != nil {
		limits, flags = append(limits, "type "+strings.Join(cfg.Types, ", ")), append(flags, "-type")
	}
	if cfg.MinRun > 0 {
		limits, flags = append(limits, "runs of "+durText(cfg.MinRun)), append(flags, "-min-run")
	}
	if cfg.RASet || cfg.DecSet {
		limits, flags = append(limits, fmt.Sprintf("within %g° of the region", cfg.Tol)), append(flags, "-tol")
	}
	if len(cfg.Horizon) > 0 {
		limits, flags = append(limits, "the local horizon"), append(flags, "-horizon")
	}
	if len(cfg.Skip) > 0 {
		limits, flags = append(limits, fmt.Sprintf("%d skipped", len(cfg.Skip))), append(flags, "-skip")
	}
	if cfg.Framing && cfg.AltAz {
		limits, flags = append(limits, "an alt-az mount's field rotation"), append(flags, "-mount eq (with a wedge)")
	}
	if s.MoonSep >= 1 && slices.ContainsFunc(s.MoonAlt, func(a float64) bool { return a > 0 }) {
		limits = append(limits, fmt.Sprintf("at least %.0f° from the Moon while it is up%s", s.MoonSep, MoonFloor(cfg, s)))
	}

	return fmt.Sprintf("No objects within the limits (%s). Loosen %s, or try another -list.", strings.Join(limits, ", "), strings.Join(flags, ", "))
}

// MoonFloor qualifies the Moon distance s.MoonSep with what lowers it (the
// header's, the page's and the terminal view's Moon limit): a
// -filter, whose choices scale it (scoring's moonK) as far down as
// scoring.LoosestMoonK of it (nothing when that loosens it by no whole
// degree, or to none: a new Moon's 1° says nothing), and a lunar eclipse,
// which scales it by the least light the Earth's shadow leaves in the
// window (scoring.Sky.tooClose), shown down to 0°, and with a filter
// scales that too, as both apply at once; "" when neither does.
func MoonFloor(cfg *config.Config, s *scoring.Sky) string {
	var parts []string
	k := scoring.LoosestMoonK(cfg)
	filtered := math.Round(s.MoonSep * k)
	if filtered != math.Round(s.MoonSep) && filtered != 0 {
		parts = append(parts, fmt.Sprintf("%.0f° through a filter", filtered))
	}
	if len(s.Eclipse) > 0 {
		// Only below the filter's floor: a grazing eclipse's would repeat it.
		light := slices.MinFunc(s.Eclipse, func(a, b astro.Eclipse) int { return cmp.Compare(a.Light, b.Light) }).Light
		if lo := math.Round(s.MoonSep * light * k); lo < filtered {
			parts = append(parts, fmt.Sprintf("%.0f° in the eclipse", lo))
		}
	}
	if len(parts) == 0 {
		return ""
	}

	return " (down to " + strings.Join(parts, ", ") + ")"
}

// wrapped splits text at TermWidth when that is set, else returns it whole.
func wrapped(text string) []string {
	if TermWidth > 0 {
		return wrap(text, max(20, TermWidth))
	}

	return []string{text}
}

// resultsTable renders the target table without the columns in drop, with
// descriptions cut to desc runes, and returns it, its width in columns and
// the widest description in it.
func resultsTable(cfg *config.Config, s *scoring.Sky, results []scoring.Result, planets []stars.Star, drop map[string]bool, desc int) (string, int, int) {
	var buf bytes.Buffer
	widest := 0
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	cols := columns(cfg, resultColumns, drop)
	row(w, "01", names(cols)...)
	shown := results[:min(cfg.Top, len(results))]
	up := varied(shown, func(r scoring.Result) string { return scale(r.Foto, 0.66, 0.33) })
	types := varied(shown, func(r scoring.Result) string { return typeColor(r.Target) })
	sky := varied(shown, func(r scoring.Result) string { return scale(r.SkyMag, 20.5, 19) })
	for i, r := range shown {
		// What the frame holds: the object, or its core (catalog.Target.AtCore).
		major, minor := r.FrameAxes()
		var px string
		if cfg.Framing {
			px = sizeText(r.Target, "%.0f", major*60/cfg.Scale)
			if m := r.Mosaic; m.Panels() > 1 {
				px += fmt.Sprintf(" %dx%d", m.Cols, m.Rows)
			}
		}
		size := sizeText(r.Target, "%.0f'", major)
		// The minor axis only enters the frame fit with -rotate.
		if cfg.Rotate && minor < major {
			size = fmt.Sprintf("%.0fx%.0f'", major, minor)
		}
		ra, dec := position(r.Position())
		d := shortDesc(r.Description, desc)
		widest = max(widest, utf8.RuneCountInString(d))
		code, subs := SubsText(r.Exposure)
		starCode, star := "39", "-"
		if !drop[starColumn] { // a dropped column looks nothing up
			if st, _, ok := brightStar(cfg, s, r, planets); ok {
				starCode, star = "33", fmt.Sprintf("%s %.1f", st.Name, st.Mag)
			}
		}
		// Every column's cell, in resultColumns' order; cols picks those shown.
		all := []string{
			paint("39", strconv.Itoa(i+1)), paint("36", r.Name), paint("39", d), paint(types[i], r.Type),
			paint("39", constellation.Abbr(r.Constellation)), paint("39", ra), paint("39", dec), paint("39", size),
			paint(up[i], fmt.Sprintf("%.2f", r.Foto)), paint(scale(r.Score, 0.66, 0.33), fmt.Sprintf("%.2f", r.Score)),
			paint("39", runText(s, r)), paint("39", altText(s, r.MaxAlt, r.MaxAt)),
			paint(sky[i], fmt.Sprintf("%.1f", r.SkyMag)), paint("39", magText(r.Target)), paint("39", sbText(r)), paint(starCode, star),
			paint("39", filterText(r)), paint("39", px), paint(code, subs),
		}
		row(w, "", pick(all, resultColumns, cols)...)
	}
	w.Flush()

	return buf.String(), tableWidth(buf.String()), widest
}

// brightStar returns the brightest star or planet (stars.Brightest, with
// planets, midPlanets') that can fall in r's frame, centred on it at any
// angle: within the half-diagonal of its mosaic's grid, else of cfg's frame,
// the Origin Mark II's without framing. It also returns the separation in
// degrees.
func brightStar(cfg *config.Config, s *scoring.Sky, r scoring.Result, planets []stars.Star) (stars.Star, float64, bool) {
	w, h := cfg.FOVLong, cfg.FOVShort
	if r.Mosaic.W > 0 {
		w, h = r.Mosaic.W, r.Mosaic.H
	}
	ra, dec := J2000(s, r.Target)

	return stars.Brightest(ra, dec, math.Hypot(w, h)/2/60, planets)
}

// StarInfo is a result's STAR: the brightest star or planet that can fall
// in its frame, for JSON's brightStar and the web page's card.
type StarInfo struct { // betteralign:ignore
	Name       string  `json:"name"`
	Mag        float64 `json:"mag"`
	Separation float64 `json:"separation"`       // arcminutes from the target's centre
	Planet     bool    `json:"planet,omitempty"` // one of stars.Planets
}

// BrightStars returns each of results' StarInfo, nil where none can fall in
// its frame, the planets taken once (midPlanets).
func BrightStars(cfg *config.Config, s *scoring.Sky, results []scoring.Result) []*StarInfo {
	planets := midPlanets(s)
	out := make([]*StarInfo, len(results))
	for i, r := range results {
		if st, sep, ok := brightStar(cfg, s, r, planets); ok {
			planet := slices.ContainsFunc(planets, func(p stars.Star) bool { return p.Name == st.Name })
			out[i] = &StarInfo{Name: st.Name, Mag: st.Mag, Separation: math.Round(sep*600) / 10, Planet: planet}
		}
	}

	return out
}

// midPlanets is stars.Planets at mid-window, which STAR takes once per
// table rather than per row and render; none for an empty grid.
func midPlanets(s *scoring.Sky) []stars.Star {
	if len(s.Grid) == 0 {
		return nil
	}

	return stars.Planets(s.Grid[len(s.Grid)/2])
}

// altText is the MAX ALT and PEAK cell: the altitude and when it is reached.
func altText(s *scoring.Sky, alt float64, at time.Time) string {
	return fmt.Sprintf("%.0f° %s", alt, clock(s, at))
}

// durText formats a duration as "10h 05m": Go's own "10h5m0s" reads poorly.
func durText(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)

	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// sgr matches the escapes paint writes.
var sgr = regexp.MustCompile("\x1b\\[[0-9]*m")

var UseColor bool

// TermWidth is the terminal's width in columns, which the target table fits
// when it can; 0 (not a terminal) leaves the table as wide as it is.
var TermWidth int

// TerminalWidth returns stdout's width in columns, else $COLUMNS (which the
// web page sets from its output panel), 0 when neither is known (a pipe, a
// file).
func TerminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return w
	}
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		return w
	}

	return 0
}

// runText is the OBSERVABLE column: the longest continuous observable
// stretch, with a + when there are others.
func runText(s *scoring.Sky, r scoring.Result) string {
	text := spanText(s, r.RunFrom, r.RunTo)
	if r.Runs > 1 {
		text += "+"
	}

	return text
}

// moonText says when the Moon is up within the window: "up throughout",
// "never up", or its rises and sets in order, the upper limb on the
// refracted horizon as the almanacs give them (astro.MoonHorizon).
func moonText(s *scoring.Sky) string {
	var events []string
	for i := 1; i < len(s.Grid); i++ {
		switch {
		case s.MoonAlt[i-1] <= astro.MoonHorizon && s.MoonAlt[i] > astro.MoonHorizon:
			events = append(events, "rises "+clock(s, s.Grid[i]))
		case s.MoonAlt[i-1] > astro.MoonHorizon && s.MoonAlt[i] <= astro.MoonHorizon:
			events = append(events, "sets "+clock(s, s.Grid[i]))
		}
	}
	switch {
	case len(events) > 0:
		return strings.Join(events, ", ")
	case s.MoonAlt[0] > astro.MoonHorizon:
		return "up throughout"
	}

	return "never up"
}

// eclipsePhases are the phases of a lunar eclipse the header and JSON list,
// each the stretch the Moon is at least that deep in the Earth's shadow.
var eclipsePhases = []struct {
	name  string
	phase astro.EclipsePhase
}{{"penumbral", astro.Penumbral}, {"umbral", astro.Partial}, {"total", astro.Total}}

// eclipseSpan returns the stretch of the window in which the Moon is at
// least phase deep in the Earth's shadow, false when it never is; a night
// holds one eclipse at most, so the stretch is unbroken.
func eclipseSpan(s *scoring.Sky, phase astro.EclipsePhase) (span, bool) {
	first, last := -1, -1
	for i, e := range s.Eclipse {
		if e.Phase >= phase {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return span{}, false
	}

	to := s.Grid[last].Add(time.Minute)
	if to.After(s.End) {
		to = s.End
	}

	return span{From: s.Grid[first], To: to}, true
}

// eclipseText is the Moon line's lunar eclipse, its phases within the
// window, "" without one.
func eclipseText(s *scoring.Sky) string {
	var phases []string
	for _, p := range eclipsePhases {
		if sp, ok := eclipseSpan(s, p.phase); ok {
			phases = append(phases, p.name+" "+spanText(s, sp.From, sp.To))
		}
	}
	if len(phases) == 0 {
		return ""
	}

	return "; lunar eclipse, " + strings.Join(phases, ", ") + " (dimming its light)"
}

// descWidth caps the DESCRIPTION cell: tabwriter makes the column as wide as
// the longest description shown, and Caldwell's alias lists run past 100
// characters, 192-column rows. 40 holds every GaryImm description (37), so
// the default list prints unchanged; -json keeps the full text. minDesc is
// how far a narrow terminal may cut it, past which a common name is lost.
const descWidth, minDesc = 40, 15

// descKeep is the description width below which a narrow terminal drops the
// columns of dropOrder first.
const descKeep = 22

// dropOrder is the order a narrow terminal drops target columns in, least
// needed for choosing tonight's target first: STAR is only a warning, the
// position is for the mount (-csv carries it), UP repeats what OBSERVABLE
// shows, then detail the score already weighs (with framing SIZE goes
// before SKY, as PX shows it against the frame: framedOrder); most
// descriptions name the type; on a phone the peak's time and the filter go
// last, leaving what and when to shoot, how well and for how long.
var dropOrder = [][]string{{starColumn}, {raColumn, decColumn}, {"CON"}, {"UP"}, {"MAG"}, {"SB"}, {"SKY"}, {sizeColumn}, {pxColumn}, {typeColumn}, {"MAX ALT"}, {filterColumn}}

// framedOrder is order with SIZE moved to just after SB.
func framedOrder(order [][]string) [][]string {
	out := slices.DeleteFunc(slices.Clone(order), func(g []string) bool { return slices.Contains(g, sizeColumn) })
	at := slices.IndexFunc(out, func(g []string) bool { return slices.Contains(g, "SB") }) + 1

	return slices.Insert(out, at, []string{sizeColumn})
}

// planDropOrder is dropOrder for the night plan: what to shoot when, and
// how well, stay.
var planDropOrder = [][]string{{typeColumn}, {peakColumn}}

// raColumn, decColumn, sizeColumn, typeColumn and peakColumn are named by
// the drop orders.
const raColumn, decColumn, sizeColumn, typeColumn, peakColumn, starColumn = "RA", "DEC", "SIZE", "TYPE", "PEAK ALT", "STAR"

// dropped and planDropped hold the columns Results and Plan left out, and
// noTargets and emptyPlan that they printed no table, so the legend leaves
// those out too.
var (
	dropped, planDropped map[string]bool
	noTargets, emptyPlan bool
)

// shortDesc truncates a description to width runes, the ellipsis included,
// at the last comma-separated name that fits when there is one.
func shortDesc(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	prefix := string([]rune(s)[:width-1])
	// At the last name that fits, else the last word when that keeps at
	// least half: "Gamma Cassiope…" reads worse than "Gamma…" only when
	// the cut throws most of it away.
	if cut := strings.LastIndex(prefix, ", "); cut > 0 {
		prefix = prefix[:cut]
	} else if cut := strings.LastIndex(prefix, " "); cut >= len(prefix)/2 {
		prefix = prefix[:cut]
	}

	return prefix + "…"
}

// filterText is the FILTER cell: the -filter choice that scored best, "none"
// when no filter beat going without, a recommendation like any other.
func filterText(r scoring.Result) string { return cmp.Or(r.Filter, "none") }

// SubsText is the SUBS cell and its colour (the terminal view's card takes
// its text): the subs for a good image, ~ when the surface brightness is its
// class's typical one, past tonight's limit (yellow) the nights like tonight
// it takes, never past the stack's ceiling, - for none.
func SubsText(e *scoring.Estimate) (string, string) {
	if e == nil {
		return "39", "-"
	}
	// Past the stack's ceiling (-residual) no number of subs reaches it.
	if e.Unreachable(scoring.Levels[1]) {
		return "33", "never"
	}
	// Past what tonight allows, the nights like tonight it takes: what a
	// small scope stacks across, where every row's >N would read alike. The
	// count is tonight's limit, which no assumed surface brightness enters,
	// so it takes no ~.
	if e.Good > e.Max {
		return "33", nightsText(e.Good, e.Max)
	}
	if e.Assumed {
		return "39", "~" + strconv.Itoa(e.Good)
	}

	return "39", strconv.Itoa(e.Good)
}

// nightsText is good subs over the max that fit a night: one decimal under
// ten nights, whole ones to 99, past it (a night the forecast loses whole)
// >99; >N without a night's subs to divide by.
func nightsText(good, perNight int) string {
	if perNight <= 0 {
		return ">" + strconv.Itoa(perNight)
	}
	switch n := float64(good) / float64(perNight); {
	case n < 9.95:
		return fmt.Sprintf("%.1f nights", n)
	case n < 99.5:
		return fmt.Sprintf("%.0f nights", n)
	}

	return ">99 nights"
}

// magText is the MAG column: the integrated V magnitude, else the B one
// marked B, - when unknown.
func magText(tg catalog.Target) string {
	switch {
	case tg.HasMag():
		return fmt.Sprintf("%4.1f", tg.Mag) // decimals aligned
	case tg.BMag > 0:
		return fmt.Sprintf("%4.1fB", tg.BMag)
	}

	return "-"
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

// spanText formats [from, to) as clock times, the end marked +Nd when the
// span lasts N whole days or more: a polar night's 24 h window would
// otherwise read 12:00-12:00, as if it were empty.
func spanText(s *scoring.Sky, from, to time.Time) string {
	text := clock(s, from) + "-" + clock(s, to)
	if days := int(to.Sub(from) / (24 * time.Hour)); days > 0 {
		text += fmt.Sprintf("+%dd", days)
	}

	return text
}

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

// varied returns each row's colour code, or "39" for every row when they all
// share one: a colour no row differs in tells nothing apart (at a bright site
// SKY would be red on every row).
func varied[T any](rows []T, code func(T) string) []string {
	codes := make([]string, len(rows))
	for i, r := range rows {
		codes[i] = code(r)
	}
	if len(codes) > 1 && !slices.ContainsFunc(codes, func(c string) bool { return c != codes[0] }) {
		for i := range codes {
			codes[i] = "39"
		}
	}

	return codes
}

// column is a table column and its legend entry; tables build their headers
// from these so the legend cannot drift from the output.
type column struct{ name, desc string }

var weatherColumns = []column{
	{"HOUR", "local hour"},
	{"CLOUD", "effective cloud cover (high cloud counts half), and rain when any"},
	{"LOW/MID/HIGH", "cloud cover per layer"},
	{"TRANSP", "7Timer transparency, 1 best to 8 worst"},
	{"EXT", "atmospheric extinction, mag per airmass"},
	{"SEEING", "7Timer seeing (star FWHM); ~n\" estimated from the upper air"},
	{"DEW SPREAD", "temperature minus dew point; dew forms below ~2-3 °C"},
	{"WIND/GUST", "mean wind / gusts, km/h; ~ capped at 3x the wind (not below 20), as scored"},
}

// sameAsTargets marks a plan column the target legend already explains, so
// Legend leaves it out.
const sameAsTargets = ""

// filterColumn is shown only with -filter (columns).
const filterColumn = "FILTER"

// pxColumn is shown only with framing (columns).
const pxColumn = "PX"

// subsColumn is shown only with framing (columns), like PX.
const subsColumn = "SUBS"

var planColumns = []column{
	{"TIME", "imaging block"},
	{"NAME", sameAsTargets},
	{"DESCRIPTION", sameAsTargets},
	{typeColumn, sameAsTargets},
	{"BLOCK SCORE", "the target's score within its block"},
	{peakColumn, "highest altitude within the block, and when"},
	{filterColumn, sameAsTargets},
}

// resultColumns' descriptions stay one short line each: the reasoning behind
// a value belongs in README.md and MODELS.md, not in every run's output.
var resultColumns = []column{
	{"#", "rank by SCORE"},
	{"NAME", "catalog designation"},
	{"DESCRIPTION", "common name; comets: magnitude, Sun distance r, Earth Δ"},
	{typeColumn, "object type"},
	{"CON", "constellation"},
	{raColumn, "J2000 right ascension, hh mm.m"},
	{decColumn, "J2000 declination, ±dd mm"},
	{sizeColumn, "major axis in arcminutes (with -rotate: major x minor)"},
	{"UP", "fraction of the night observable (altitude, horizon, Moon, an alt-az mount's field rotation)"},
	{"SCORE", "imaging quality 0-1; 1 = all night under a perfect dark sky"},
	{"OBSERVABLE", "longest unbroken observable stretch (+ there are more)"},
	{"MAX ALT", "highest altitude, and when"},
	{"SKY", "sky brightness at the object, mag/arcsec² (higher is darker)"},
	{"MAG", "integrated magnitude, V (B where only blue is known)"},
	{"SB", "object surface brightness, mag/arcsec² (higher is fainter)"},
	{starColumn, "brightest star or planet to V 4 that can fall in the frame (without framing, the Origin's), and its magnitude: halos and reflections"},
	{filterColumn, "the filter of yours that helps most, worn all night, or none"},
	{pxColumn, "size in pixels (NxM: mosaic panels needed)"},
	{subsColumn, "-sub second subs for a good image, per panel; N nights: more than tonight fits; never: past what flats leave of the sky"},
}

// columns returns cols without FILTER when there is no -filter and without
// PX and SUBS when there is no framing.
func columns(cfg *config.Config, cols []column, drop map[string]bool) []column {
	return slices.DeleteFunc(slices.Clone(cols), func(c column) bool {
		return c.name == filterColumn && cfg.Filter == "" || (c.name == pxColumn || c.name == subsColumn) && !cfg.Framing ||
			drop[c.name]
	})
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
func Legend(cfg *config.Config, s *scoring.Sky, tableWidth int) {
	// One tabwriter for all sections keeps them aligned; the buffer lets the
	// padding after section titles be trimmed.
	type section struct {
		title string
		cols  []column
	}
	var sections []section
	if forecastFetched(s) {
		sections = append(sections, section{"Forecast:", weatherColumns})
	}
	if cfg.Plan > 0 && !emptyPlan {
		sections = append(sections, section{"Night plan:", columns(cfg, planColumns, planDropped)})
	}
	if !noTargets {
		sections = append(sections, section{"Targets:", columns(cfg, resultColumns, dropped)})
	}
	if len(sections) == 0 { // no table printed, nothing to explain
		return
	}
	fmt.Println()
	fmt.Println(paint("01", "Legend"))
	// Descriptions start where tabwriter puts the second column: after the
	// widest first cell (titles included) and the padding.
	indent := 0
	for _, sec := range sections {
		indent = max(indent, len("  "+sec.title))
		for _, c := range sec.cols {
			if c.desc != sameAsTargets {
				indent = max(indent, len("    "+c.name))
			}
		}
	}
	// Floored: on a terminal narrower than the indent the width would go
	// negative and wrap() would put one word per line.
	// The terminal's width when known: tables narrowed to fit it would
	// otherwise wrap the legend shorter than the screen.
	width := max(20, cmp.Or(TermWidth, tableWidth, legendWidth)-indent-legendPad)
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, legendPad, ' ', 0)
	for _, sec := range sections {
		fmt.Fprintln(w, "  "+sec.title+"\t")
		for _, c := range sec.cols {
			if c.desc == sameAsTargets {
				continue
			}
			// Continuation lines go under the first, in the same column.
			name := c.name
			for _, line := range wrap(c.desc, width) {
				fmt.Fprintf(w, "    %s\t%s\n", name, paint("90", line))
				name = ""
			}
		}
	}
	// The colours are the last cells, so their escapes do not shift the
	// description column.
	legendLine(w, "  Marks:", width, "90", "~ estimated, - unknown or not applicable")
	legendLine(w, "  Colors:", width, "32", "green", "90", "good,", "33", "yellow", "90", "fair,", "31", "red", "90", "poor")
	legendLine(w, "", width, "90", "TYPE:", "35", "magenta", "90", "emission-line (helped by -filter),", "33", "yellow", "90", "comet")
	legendLine(w, "", width, "90", "UP, SKY and TYPE are plain when every row is alike")
	w.Flush()
	for line := range strings.Lines(buf.String()) {
		fmt.Println(strings.TrimRight(line, " \n"))
	}
}

// legendLine writes a legend line of parts, colour code then text, words
// joined by spaces and wrapped at width under the description column, each
// word painted in its part's colour.
func legendLine(w io.Writer, title string, width int, parts ...string) {
	var words, codes []string
	for i := 0; i+1 < len(parts); i += 2 {
		for word := range strings.FieldsSeq(parts[i+1]) {
			words, codes = append(words, word), append(codes, parts[i])
		}
	}
	for i := 0; i < len(words); {
		var line []string
		n := 0
		for ; i < len(words); i++ {
			wn := utf8.RuneCountInString(words[i])
			if len(line) > 0 && n+1+wn > width {
				break
			}
			n += min(n, 1) + wn
			line = append(line, paint(codes[i], words[i]))
		}
		fmt.Fprintf(w, "%s\t%s\n", title, strings.Join(line, " "))
		title = ""
	}
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
		case visible(line)+1+visible(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	return append(lines, line)
}

// visible is s's width in columns, without paint's escapes.
func visible(s string) int { return utf8.RuneCountInString(sgr.ReplaceAllString(s, "")) }

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
