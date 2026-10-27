// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"testing"

	"github.com/dkorunic/astro-recommender/internal/astro"
)

// Every cores key is some list's designation, and every entry a core moves
// keeps its centre within the object (half its listed size, from its listed
// position, and half the core's own, since the lists' centres differ), so a
// mistyped key or position fails here.
func TestCores(t *testing.T) {
	seen := map[string]bool{}
	for _, list := range Lists {
		targets, _, err := Load(list, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			c, ok := tg.AtCore()
			if !ok {
				continue
			}
			seen[NameKey(tg.Name)] = true
			if major, minor := c.FrameAxes(); major != c.Core.Major || minor != c.Core.Minor || c.Size != tg.Size {
				t.Errorf("%s %s: frame axes %v x %v, size %v", list, tg.Name, major, minor, c.Size)
			}
			if sep := astro.Separation(tg.RADeg, tg.DecDeg, c.RADeg, c.DecDeg) * 60; sep > (tg.Size+c.Core.Major)/2 {
				t.Errorf("%s %s: core %.0f' from the object's centre, size %v'", list, tg.Name, sep, tg.Size)
			}
		}
	}
	for key := range cores {
		if !seen[key] {
			t.Errorf("core %q: no list entry larger than it", key)
		}
	}
	if c, ok := (Target{Name: "NGC 2264", Size: 20}).AtCore(); ok {
		t.Errorf("a cluster entry smaller than the core moved to it: %+v", c)
	}
	if c, _ := (Target{Name: "NGC 7000", Size: 120, Description: "North America Nebula"}).AtCore(); c.Description != "Cygnus Wall, in North America Nebula" {
		t.Errorf("description %q", c.Description)
	}
}

// An entry that is the core already (GaryImm's IC 1396, the Elephant's
// Trunk itself) stays as it is: no "Elephant's Trunk region, in Elephant's
// Trunk".
func TestCoreIsEntry(t *testing.T) {
	trunk := Target{Name: "IC 1396", Description: "Elephant's Trunk", Size: 60, RADeg: 324.7, DecDeg: 57.5}
	if got, ok := trunk.AtCore(); ok || got.Description != trunk.Description {
		t.Errorf("the Trunk moved to its own core: %q", got.Description)
	}
	whole := Target{Name: "Sh2-131", Description: "IC 1396, Elephant's Trunk Nebula", Size: 170}
	if got, ok := whole.AtCore(); !ok || got.Description != "Elephant's Trunk region, in IC 1396, Elephant's Trunk Nebula" {
		t.Errorf("the whole IC 1396: %q, %v", got.Description, ok)
	}
}
