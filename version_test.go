// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

// Build-time -X values win over the embedded build info.
func TestVersionString(t *testing.T) {
	GitTag, GitCommit, GitDirty, BuildTime = "v1.2.3", "abc1234", ".dirty", "2026-10-05T07:00:00Z"
	t.Cleanup(func() { GitTag, GitCommit, GitDirty, BuildTime = "", "", "", "" })
	got := versionString()
	for _, want := range []string{"astro-recommender v1.2.3", "commit abc1234.dirty", "built 2026-10-05T07:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("versionString() = %q, missing %q", got, want)
		}
	}
}

// Without -X values the build info fills in, and a modified tree is marked
// once: Go's "+dirty" version suffix is not repeated as ".dirty".
func TestDescribeBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.8.1-0.20261009082124-94274bea1a5b+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "94274bea1a5b0000"}, {Key: "vcs.modified", Value: "true"}, {Key: "vcs.time", Value: "2026-10-09T08:21:24Z"},
		},
	}
	got := describe(info)
	want := "astro-recommender v0.8.1-0.20261009082124-94274bea1a5b (commit 94274be.dirty, built 2026-10-09T08:21:24Z"
	if !strings.HasPrefix(got, want) || strings.Count(got, "dirty") != 1 {
		t.Errorf("describe() = %q, want prefix %q and one dirty", got, want)
	}
	if got := describe(nil); !strings.HasPrefix(got, "astro-recommender dev (commit unknown, built unknown") {
		t.Errorf("describe(nil) = %q", got)
	}
}
