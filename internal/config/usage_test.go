// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"flag"
	"slices"
	"strings"
	"testing"
)

// -h lists every flag once, under the headings of usageGroups in their
// order and each group's flags in its order, a flag in no group under
// Other, the help wrapped at the width (its indent shown as 8 columns),
// and each flag in PrintDefaults' "  -name type" format the page parses.
func TestUsage(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("/some/path/astro-recommender", flag.ContinueOnError)
	for _, g := range usageGroups {
		for _, n := range g.names {
			fs.String(n, "", strings.Repeat("word ", 30))
		}
	}
	fs.Bool("brand-new", false, "a flag no group lists")
	var buf bytes.Buffer
	usage(&buf, fs, 60)
	out := buf.String()
	if !strings.HasPrefix(out, "astro-recommender ranks") || !strings.Contains(out, "Example: astro-recommender -lat") {
		t.Errorf("no description or example:\n%s", out)
	}
	var headings, flags []string
	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")
		// After its 4 spaces a help line's tab reaches column 8.
		if n := len([]rune(strings.Replace(line, "    \t", "        ", 1))); n > 60 {
			t.Errorf("%d-column line at width 60: %q", n, line)
		}
		switch {
		case strings.HasPrefix(line, "  -"):
			flags = append(flags, strings.Fields(line)[0][1:])
		case strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " "):
			headings = append(headings, strings.TrimSuffix(line, ":"))
		}
	}
	var want, wantFlags []string
	for _, g := range usageGroups {
		want = append(want, g.title)
		wantFlags = append(wantFlags, g.names...)
	}
	if !slices.Equal(headings, append(want, "Other")) {
		t.Errorf("headings %q, want %q and Other", headings, want)
	}
	if !slices.Equal(flags, append(wantFlags, "brand-new")) {
		t.Errorf("flags %q, want %q and brand-new", flags, wantFlags)
	}
}

// Every flag Parse defines is in a group: none lands under Other.
func TestUsageGroupsCoverFlags(t *testing.T) {
	if _, err := mutParse(t, mutBase()...); err != nil {
		t.Fatal(err)
	}
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if !slices.ContainsFunc(usageGroups, func(g usageGroup) bool { return slices.Contains(g.names, f.Name) }) {
			t.Errorf("-%s is in no usage group", f.Name)
		}
	})
}
