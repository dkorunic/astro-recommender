// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set at build time by Taskfile.yml and .goreleaser.yml (-X main.Name=value).
var (
	GitTag    string
	GitCommit string
	GitDirty  string
	BuildTime string
)

// versionString describes the build. Without -X values (e.g. go install) it
// falls back to the module version and VCS data Go embeds in the binary.
func versionString() string {
	info, _ := debug.ReadBuildInfo()

	return describe(info)
}

// describe formats the version from the -X values and info (nil: none).
func describe(info *debug.BuildInfo) string {
	tag, commit, dirty, built := GitTag, GitCommit, strings.TrimSpace(GitDirty), BuildTime
	if info != nil {
		// Since Go 1.24 a modified tree's module version ends in "+dirty",
		// which vcs.modified below already reports after the commit.
		if v := strings.TrimSuffix(info.Main.Version, "+dirty"); tag == "" && v != "" && v != "(devel)" {
			tag = v
		}
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && commit == "":
				commit = s.Value[:min(7, len(s.Value))]
			case s.Key == "vcs.modified" && s.Value == "true" && dirty == "" && GitCommit == "":
				dirty = ".dirty"
			case s.Key == "vcs.time" && built == "":
				built = s.Value
			}
		}
	}

	return fmt.Sprintf("astro-recommender %s (commit %s%s, built %s, %s %s/%s)",
		cmp.Or(tag, "dev"), cmp.Or(commit, "unknown"), dirty, cmp.Or(built, "unknown"), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
