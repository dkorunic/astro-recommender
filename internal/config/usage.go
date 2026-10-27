// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

// UsageWidth is the width -h wraps the flag help at, 0 for none; main sets
// it to the terminal's.
var UsageWidth int

// usageGroup is a heading of -h and the flags under it, in order.
type usageGroup struct {
	title string
	names []string
}

// usageGroups sorts the flags by purpose for -h. web/index.html builds its
// form from -h and reads these headings: its SITE_GROUP (Where and when)
// fills the Observatory panel, every other group is a tab of the Options
// panel. A flag in no group is listed
// under "Other", so a new flag is never lost.
var usageGroups = []usageGroup{
	{"Where and when", []string{"lat", "lon", "date", "tz"}},
	{"What to list", []string{"list", "n", "plan"}},
	{"Equipment", []string{"scope", "filter"}},
	{"Sky", []string{"bortle", "sqm"}},
	{"Night window", []string{"from", "to", "twilight", "min-run"}},
	{"Altitude and region", []string{"alt-min", "alt-max", "horizon", "ra", "dec", "tol"}},
	{"Selection", []string{"type", "size-min", "size-max", "targets", "skip", "comet-mag"}},
	{"Framing", []string{"focal", "camera", "fov", "sensor", "pixel", "scale", "min-px", "mosaic", "rotate"}},
	{"Camera and exposure", append(slices.Clone(cameraFlags), "mono")},
	{"Sky and filters", []string{"extinction", "filter-nm", "filter-std"}},
	{"Data sources", []string{"no-weather", "no-sqm", "no-geocode", "no-comets", "no-solar"}},
	{"Output", []string{"no-tui", "csv", "json", "version"}},
}

// usage prints -h: what the program does, an example, then the flags of fs
// under the headings of usageGroups, their help wrapped at width (0: not).
// Each flag keeps PrintDefaults' own format, which the web page parses.
func usage(w io.Writer, fs *flag.FlagSet, width int) {
	name := filepath.Base(fs.Name())
	intro := []string{name + " ranks deep sky objects for imaging tonight from your location."}
	if width > 0 {
		intro = wrapWords(intro[0], width)
	}
	fmt.Fprintf(w, "%s\n\n", strings.Join(intro, "\n"))
	fmt.Fprintf(w, "Usage:   %s -lat DEG -lon DEG [flags]\n", name)
	fmt.Fprintf(w, "Example: %s -lat 45.81 -lon 15.98 -plan 2h\n", name)
	groups := slices.Clone(usageGroups)
	var other []string
	fs.VisitAll(func(f *flag.Flag) {
		if !slices.ContainsFunc(groups, func(g usageGroup) bool { return slices.Contains(g.names, f.Name) }) {
			other = append(other, f.Name)
		}
	})
	if other != nil {
		groups = append(groups, usageGroup{"Other", other})
	}
	for _, g := range groups {
		// One FlagSet per flag prints it as fs would, in the group's order
		// (PrintDefaults sorts by name).
		var buf bytes.Buffer
		for _, n := range g.names {
			if f := fs.Lookup(n); f != nil {
				sub := flag.NewFlagSet(fs.Name(), flag.ContinueOnError)
				sub.Var(f.Value, f.Name, f.Usage)
				sub.SetOutput(&buf)
				sub.PrintDefaults()
			}
		}
		if buf.Len() == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s:\n", g.title)
		for line := range strings.Lines(buf.String()) {
			help, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "    \t")
			if !ok || width == 0 {
				fmt.Fprint(w, line)

				continue
			}
			// After the 4 spaces the tab reaches column 8.
			for _, l := range wrapWords(help, max(20, width-8)) {
				fmt.Fprintf(w, "    \t%s\n", l)
			}
		}
	}
}

// wrapWords splits text into lines of at most width runes, between words.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for word := range strings.FieldsSeq(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	return append(lines, line)
}

// setUsage makes -h print usage for the program's flags.
func setUsage() {
	flag.Usage = func() { usage(flag.CommandLine.Output(), flag.CommandLine, UsageWidth) }
}
