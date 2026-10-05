// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
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
