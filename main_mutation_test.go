// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func mutSetVersion(t *testing.T, tag, commit, dirty, built string) {
	t.Helper()
	old := [4]string{GitTag, GitCommit, GitDirty, BuildTime}
	GitTag, GitCommit, GitDirty, BuildTime = tag, commit, dirty, built
	t.Cleanup(func() { GitTag, GitCommit, GitDirty, BuildTime = old[0], old[1], old[2], old[3] })
}

func TestMutDescribe(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
		},
	}
	mutSetVersion(t, "", "", "", "")
	got := describe(info)
	if !strings.HasPrefix(got, "astro-recommender v1.2.3 (commit 0123456.dirty, built 2026-01-02T03:04:05Z, ") {
		t.Errorf("describe(buildinfo) = %q", got)
	}
	// -X values win, and a -X commit without a -X dirty is clean: the
	// build info's vcs.modified must not mark it.
	mutSetVersion(t, "v9", "abc1234", "", "today")
	got = describe(info)
	if !strings.HasPrefix(got, "astro-recommender v9 (commit abc1234, built today, ") {
		t.Errorf("describe(-X) = %q", got)
	}
	mutSetVersion(t, "", "", " .dirty\n", "")
	if got = describe(nil); !strings.HasPrefix(got, "astro-recommender dev (commit unknown.dirty, built unknown, ") {
		t.Errorf("describe(nil) = %q", got)
	}
	mutSetVersion(t, "", "", "", "")
	if got = describe(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}); !strings.HasPrefix(got, "astro-recommender dev (commit unknown, ") {
		t.Errorf("describe(devel) = %q", got)
	}
}
