// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import "strings"

// Core is the part of an object too large to frame whole that is imaged in
// its place: its axes in arc minutes, its J2000 centre in degrees and what
// it is.
type Core struct {
	Region       string
	Major, Minor float64
	RADeg        float64
	DecDeg       float64
}

// cores are the usual framings of large objects by NameKey, every list's
// designation of each: the regions and sizes NightAstro shows as their
// "core" framings (October 2026), centred on SIMBAD's position of the part
// (Melotte 15, IC 1396A, NGC 2244, NGC 2264), else the object's, and the
// Cygnus Wall and Pickering's Triangle, which SIMBAD has no position for,
// on the centres of plate-solved frames of them (Astrobin, Telescope Live).
var cores = func() map[string]*Core {
	m := map[string]*Core{}
	for _, c := range []struct {
		core  Core
		names []string
	}{
		{Core{"Bulge and inner spiral arms", 60, 35, 10.6846, 41.2692}, []string{"m31", "ngc224"}},
		{Core{"Bright main nebula", 60, 40, 83.82, -5.3875}, []string{"m42", "ngc1976"}},
		{Core{"Bright part of the Lagoon", 60, 30, 270.9042, -24.3867}, []string{"m8", "sh2-25"}},
		{Core{"Pillars of Creation region", 30, 25, 274.6879, -13.7919}, []string{"m16", "ngc6611", "sh2-49"}},
		{Core{"Central region around Melotte 15", 50, 40, 38.21, 61.4711}, []string{"ic1805", "sh2-190", "lbn654"}},
		{Core{"Bright central part of the Soul", 60, 35, 42.855, 60.4189}, []string{"ic1848", "sh2-199"}},
		{Core{"Elephant's Trunk region", 50, 35, 324, 57.4}, []string{"ic1396", "sh2-131"}},
		{Core{"Central cavity and inner Rosette", 50, 40, 98.045, 4.9139}, []string{"ngc2237", "c49", "sh2-275"}},
		{Core{"Cygnus Wall", 45, 30, 314.7917, 43.6639}, []string{"ngc7000", "c20", "sh2-117"}},
		{Core{"Pickering's Triangle and the inner filaments", 90, 70, 312.1167, 31.595}, []string{"sh2-103"}},
		{Core{"Cone Nebula and Christmas Tree Cluster", 60, 40, 100.2171, 9.8769}, []string{"ngc2264", "sh2-273"}},
	} {
		for _, n := range c.names {
			m[n] = &c.core
		}
	}

	return m
}()

// AtCore returns t moved to its core: centred on it, framed by its axes
// (FrameAxes) and described as it; false when t has none or its core is not
// smaller than the listed size (a cluster's entry, or one that is the part
// itself), or its description names the region (isCore). The size, magnitude and surface brightness stay the whole
// object's.
func (t Target) AtCore() (Target, bool) {
	c := byKey(cores, t.Name)
	if c == nil || !t.HasSize() || c.Major >= t.Size || t.isCore(c) {
		return t, false
	}
	t.Core, t.RADeg, t.DecDeg = c, c.RADeg, c.DecDeg
	desc := c.Region
	if t.Description != "" {
		desc += ", in " + t.Description
	}
	t.Description = desc

	return t, true
}

// FrameAxes returns the axes the frame must hold: the core's when t is at
// it, else Axes.
func (t Target) FrameAxes() (float64, float64) {
	if t.Core != nil {
		return t.Core.Major, t.Core.Minor
	}

	return t.Axes()
}

// isCore reports whether t is the core already: about its size (under twice
// its major axis) and described as its region (GaryImm's 60′ IC 1396 is the
// Elephant's Trunk), which "Elephant's Trunk region, in Elephant's Trunk"
// would only repeat. The whole complex may name it too (Sharpless' 170′
// Sh2-131: "IC 1396, Elephant's Trunk Nebula"), hence the size.
func (t Target) isCore(c *Core) bool {
	region := strings.ToLower(strings.TrimSuffix(c.Region, " region"))

	return t.Size < 2*c.Major && strings.Contains(strings.ToLower(t.Description), region)
}
