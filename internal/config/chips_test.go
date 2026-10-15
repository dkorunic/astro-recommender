// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"testing"
)

// web/index.html shows -filter as one toggle per filter (CHIPS), so its
// values must be exactly the filters -filter takes, or the page offers one
// the program rejects or lacks one it scores.
func TestFilterChips(t *testing.T) {
	page, err := os.ReadFile("../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*filter: \{(.*)\},$`).FindSubmatch(page)
	if m == nil {
		t.Fatal("CHIPS' filter entry not found in web/index.html")
	}
	var got []string
	for _, v := range regexp.MustCompile(`(\w+): "`).FindAllSubmatch(m[1], -1) {
		got = append(got, string(v[1]))
	}
	want := slices.Sorted(maps.Keys(Filters))
	want = slices.Sorted(slices.Values(append(want, slices.Collect(maps.Keys(Broadband))...)))
	if slices.Sort(got); !slices.Equal(got, want) {
		t.Errorf("page's filters %q, -filter takes %q", got, want)
	}
}
