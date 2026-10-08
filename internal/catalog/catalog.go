// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package catalog loads deep sky targets: the embedded uptonight lists or an
// uptonight-format YAML file.
package catalog

import (
	"embed"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
	"go.yaml.in/yaml/v4"
)

// Target lists from uptonight (MIT, (c) Markus Winkler); OpenNGC and OpenIC
// derive from the OpenNGC catalog (CC BY-SA 4.0, (c) Mattia Verga); MWSC is
// Kharchenko et al. (2013, CDS J/A+A/558/A53); Melotte cross-identifies Melotte
// (1915) and Collinder (1931) with NGC/IC and take positions from MWSC,
// OpenNGC, Dias et al. (2002, CDS B/ocl) and SIMBAD; PNnet is the
// Planetary Nebulae.net catalogue (planetarynebulae.net, with permission); HASH is the HASH PN database (Parker, Bojicic &
// Frew 2016, hashpn.space).
//
//go:embed targets/*.yaml
var targetFS embed.FS

type Target struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Type          string `yaml:"type"`
	Constellation string `yaml:"constellation"`
	RA            string `yaml:"ra"`  // "hh mm ss"
	Dec           string `yaml:"dec"` // "[+-]dd mm ss"

	// Track is set only by comets.Targets, one RA/Dec in degrees per grid
	// minute; never from YAML, where a short track would index past its end.
	Track [][2]float64 `yaml:"-"`
	// RADeg and DecDeg are RA and Dec in J2000 degrees, parsed and checked
	// by Load; never from YAML.
	RADeg   float64 `yaml:"-"`
	DecDeg  float64 `yaml:"-"`
	Size    float64 `yaml:"size"`    // major axis in arc minutes; 0 or negative (-9999) means unknown
	Mag     float64 `yaml:"mag"`     // integrated V magnitude; 0 or negative (-9999) means unknown
	BMag    float64 `yaml:"bmag"`    // integrated B magnitude (OpenNGC/OpenIC); 0 or negative means unknown
	SurfBr  float64 `yaml:"surfbr"`  // V surface brightness in mag/arcsec² (GaryImmFull galaxies); 0 or negative means unknown
	BSurfBr float64 `yaml:"bsurfbr"` // mean B surface brightness within the 25 mag isophote (OpenNGC/OpenIC), ~0.8 fainter than V; 0 or negative means unknown
}

// HasSize reports whether the size is known: comets and entries with a zero
// or negative size (-9999 in the lists, 0.0 in a few LDN entries) have none.
func (t Target) HasSize() bool { return t.Size > 0 }

// HasMag reports whether the magnitude is known. No deep sky object is
// brighter than 0, so like Size the sentinel is 0 or negative (-9999 in the
// lists; LBN.yaml's upstream value was Lynds' brightness class, blanked).
func (t Target) HasMag() bool { return t.Mag > 0 }

// minBV..maxBV bound a plausible deep sky B−V; OpenNGC/OpenIC pair
// inconsistent magnitudes for ~100 objects (IC 127: V 9.2, B 15.4).
const minBV, maxBV = -0.3, 1.5

// Bounds on a known (positive) magnitude or surface brightness in a list:
// nothing is fainter than maxNumber, and no extended object is brighter than
// minSB mag/arcsec², so a `surfbr: 2.3` typo for 22.3, which would make an
// object immune to sky glow, is rejected rather than ranked first.
const maxNumber, minSB = 35, 10

// Cluster reports whether the object is an open or globular cluster without
// nebulosity: light in point sources that sky glow hardly hurts.
func (t Target) Cluster() bool { return t.kind() == kindCluster }

// SurfaceBrightness returns the object's V surface brightness in mag/arcsec²,
// the visibility measure for extended objects (it compares directly with the
// sky's), or 0 when none is known, and whether it is an estimate. It is the
// measured V one, else the B one moved to V by colour() (an estimate when the
// colour is the type's typical one), else, for galaxies and nebulae only
// (not groups), one derived by spreading the V magnitude (or B − typicalBV)
// over a uniform disc of the major axis (an estimate; the disc overstates the
// area of an elongated object, so it leans faint). Clusters get none, measured
// or not: skyK (scoring) halves the sky for their point sources instead, and
// MWSC's sizes are the core only. Clusters with nebulosity derive none, since
// their integrated magnitude is the stars', not the glow's, so without a
// measurement they are scored sky-limited, like a nebula without data. Stars,
// asterisms and comets derive none either.
func (t Target) SurfaceBrightness() (float64, bool) {
	k := t.kind()
	if k == kindCluster {
		return 0, false
	}
	switch {
	case t.SurfBr > 0:
		return t.SurfBr, false
	case t.BSurfBr > 0:
		bv, own := t.colour(k)

		return t.BSurfBr - bv, !own
	}
	if t.Track != nil || !t.HasSize() || (k != kindGalaxy && k != kindNebula) {
		return 0, false
	}
	mag := t.Mag
	if !t.HasMag() && t.BMag > 0 {
		mag = t.BMag - t.typicalBV(k)
	}
	if mag <= 0 {
		return 0, false
	}

	return mag + 2.5*math.Log10(math.Pi/4*(60*t.Size)*(60*t.Size)), true
}

// Position returns the RA and Dec in degrees that stand for the target: the
// J2000 catalog position, or for a comet its mid-track one (of date, within
// 0.4° of J2000). The -ra/-dec filter and the RA/DEC columns both use it, so
// the table never shows a position outside the region that kept the object.
func (t Target) Position() (float64, float64) {
	if len(t.Track) > 0 {
		mid := t.Track[len(t.Track)/2]

		return mid[0], mid[1]
	}

	return t.RADeg, t.DecDeg
}

// kind is the class of object the scoring rules act on. The lists spell
// types many ways, so every rule asks kind() rather than the type text;
// TestKindLists fails on a spelling none of the rules know.
type kind int

const (
	kindOther         kind = iota // stars, asterisms, novae, duplicates
	kindGalaxy                    // a single galaxy
	kindGroup                     // galaxy group or pair: the magnitude is the brightest member's, the size the whole group's
	kindCluster                   // open or globular cluster: point sources that sky glow hardly hurts, which skyK (scoring) halves the sky for
	kindClusterNebula             // cluster with nebulosity: imaged for the glow (skyK keeps 1), but the magnitude is the stars'
	kindNebula                    // every other nebula, emission-line or not
)

// kindTypes are the lists' spellings (lower case) that the substring tests
// in kind() would misread: OpenNGC's galaxy group abbreviations, the
// clusters, the cluster+nebula abbreviations (OpenNGC, Pensack) and Pensack's
// "DN" (OpenNGC's "EmN" is in emissionTypes). A "Star Cloud" stays a nebula:
// M 24 is a bright patch, NGC 206 in M31 a faint one, and a derived surface
// brightness tells them apart.
var kindTypes = map[string]kind{
	"gtrpl": kindGroup, "ggroup": kindGroup,
	"open cluster": kindCluster, "globular cluster": kindCluster,
	"cl+n emission nebula": kindClusterNebula, "n+cl": kindClusterNebula, "bn+oc": kindClusterNebula, "cl+n": kindClusterNebula,
	"dn": kindNebula,
}

// kind classifies by the type text, except that a cluster in emissionNames is
// one with nebulosity (Messier types M 16 "Open Cluster"). For every other
// type the names say what is imaged, not what the magnitude measures (R
// Aquarii is a star), so they do not promote it.
func (t Target) kind() kind {
	typ := strings.ToLower(t.Type)
	k, ok := kindTypes[typ]
	if !ok {
		switch {
		case strings.Contains(typ, "galax") && slices.ContainsFunc(groupWords, func(w string) bool { return strings.Contains(typ, w) }):
			k = kindGroup
		case strings.Contains(typ, "galax"):
			k = kindGalaxy
		case emissionTypes[typ], strings.Contains(typ, "nebul"), strings.Contains(typ, "cloud"):
			k = kindNebula
			if strings.Contains(typ, "cluster") { // Herschel/MWSC "Cluster Nebulosity"
				k = kindClusterNebula
			}
		}
	}
	if k == kindCluster && emissionNames[t.Name] {
		return kindClusterNebula
	}

	return k
}

// groupWords mark a galaxy type as a group of galaxies.
var groupWords = []string{"group", "cluster", "pair", "duo", "trio", "triplet"}

// colour is the object's B−V and whether it is the object's own (both
// magnitudes known and plausible) rather than typicalBV.
func (t Target) colour(k kind) (float64, bool) {
	if bv := t.BMag - t.Mag; t.BMag > 0 && t.HasMag() && bv >= minBV && bv <= maxBV {
		return bv, true
	}

	return t.typicalBV(k), false
}

// typicalBV is the B−V assumed for the object's type when its own is not
// usable: 0.8 for galaxies and their groups (the median of the Compendium's
// V surface brightness against OpenNGC's B one over 723 shared galaxies), 0
// for emission-line objects (Hα/OIII dominated, often bluer than that), 0.5
// for the rest (reflection nebulae, clusters with nebulosity). Rough values;
// they move a B figure to V to within a few tenths.
func (t Target) typicalBV(k kind) float64 {
	switch {
	case k == kindGalaxy, k == kindGroup:
		return 0.8
	case EmissionLine(t):
		return 0
	}

	return 0.5
}

// Lists are the embedded uptonight target lists selectable with -list.
var Lists = []string{"GaryImm", "GaryImmFull", "Messier", "Herschel400", "Pensack500", "OpenNGC", "OpenIC", "LBN", "LDN", "MWSC", "Melotte", "Collinder", "PNnet", "HASH"}

var (
	errList        = errors.New("unknown target list")
	errCoordinates = errors.New("bad coordinates")
	errSky         = errors.New("RA must be 0 to 24h and Dec within ±90°")
	errNumber      = errors.New("bad number")
)

// Load reads -targets, or the embedded -list target list.
// It returns the targets and the canonical list name.
func Load(listName, file string) ([]Target, string, error) {
	var data []byte
	var err error
	if file != "" {
		data, err = os.ReadFile(file)
	} else {
		i := slices.IndexFunc(Lists, func(n string) bool { return strings.EqualFold(n, listName) })
		if i < 0 {
			return nil, "", fmt.Errorf("%w %q, want one of %s", errList, listName, strings.Join(Lists, ", "))
		}
		listName = Lists[i]
		data, err = targetFS.ReadFile("targets/" + listName + ".yaml")
	}
	if err != nil {
		return nil, "", err
	}
	var targets []Target
	if err := yaml.Unmarshal(data, &targets); err != nil {
		return nil, "", err
	}
	// -targets may come from anywhere; its text ends up on the terminal.
	for i := range targets {
		tg := &targets[i]
		tg.Name, tg.Description = strings.TrimSpace(sanitize.Text(tg.Name)), sanitize.Text(tg.Description)
		tg.Type = strings.TrimSpace(sanitize.Text(tg.Type)) // emissionNames and kindTypes match exactly
		// The IAU boundaries beat the lists' own field, which has wrong
		// entries (R Aquarii "Aquila") and spellings ("Ophiucus", "Se1").
		ra, err1 := astro.Sexagesimal(tg.RA)
		dec, err2 := astro.Sexagesimal(tg.Dec)
		err := errors.Join(err1, err2)
		if err == nil && (ra < 0 || ra >= 24 || math.Abs(dec) > 90) {
			err = fmt.Errorf("%w: %q %q", errSky, tg.RA, tg.Dec)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%w for %s: %w", errCoordinates, tg.Name, err)
		}
		// 0 or negative is the unknown sentinel; a known value must be plausible.
		for _, f := range []struct {
			name   string
			v      float64
			lo, hi float64
		}{
			{"size", tg.Size, 0, 180 * 60}, // arcminutes; nothing spans more than the sky
			{"mag", tg.Mag, 0, maxNumber},
			{"bmag", tg.BMag, 0, maxNumber},
			{"surfbr", tg.SurfBr, minSB, maxNumber},
			{"bsurfbr", tg.BSurfBr, minSB, maxNumber},
		} {
			if !num.Finite(f.v) || (f.v > 0 && (f.v < f.lo || f.v > f.hi)) {
				return nil, "", fmt.Errorf("%w for %s: %s %v", errNumber, tg.Name, f.name, f.v)
			}
		}
		tg.RADeg, tg.DecDeg = ra*15, dec // hours -> degrees
		tg.Constellation = constellation.Of(tg.RADeg, tg.DecDeg)
	}

	return targets, listName, nil
}

// emissionTypes are the lists' spellings (lower case) of emission-line types:
// uptonight's own, LBN's "HII region", OpenNGC/OpenIC's HII and cluster+nebula
// classes and their "EmN" abbreviation.
var emissionTypes = map[string]bool{
	"emission nebula": true, "planetary nebula": true, "supernova remnant": true, "wolf-rayet nebula": true,
	"hii region": true, "hii emission nebula": true, "cl+n emission nebula": true, "emn": true,
}

// emissionNames lists targets whose type does not say emission although what
// gets imaged is mostly Ha/OIII: dark nebulae, novae, and the types that mix
// emission with reflection or a cluster (Messier "Diffuse Nebula", Pensack
// "N+CL"/"BN+OC", Herschel "Cluster Nebulosity"), where only the
// emission-dominated objects are listed (not M 78, NGC 1977, 1980 or 2362).
var emissionNames = map[string]bool{
	"IC 1396":       true, // Elephant's Trunk, inside the IC 1396 HII region
	"Sh2-155":       true, // Cave Nebula
	"NGC 2024":      true, // Flame Nebula
	"IC 434":        true, // Horsehead, silhouetted against Ha emission
	"Cederblad 211": true, // R Aquarii symbiotic nebula
	"M 8":           true, // Lagoon
	"M 16":          true, // Eagle (an Open Cluster in Messier.yaml)
	"M 17":          true, // Omega
	"M 20":          true, // Trifid
	"M 42":          true, // Orion
	"M 43":          true, // De Mairan's
	"NGC 1931":      true, // Fly
	"NGC 281":       true, // PacMan
	"IC1805":        true, // Heart (Pensack's spelling)
	"NGC 2264":      true, // Cone / Christmas Tree
	"Mel 49":        true, // NGC 2264 (Melotte)
	"NGC 1976":      true, // M 42
	"NGC 2467":      true, // Skull and Crossbones
	"IC4703(BN)":    true, // M 16 Eagle (Pensack's spelling)
	"NGC 6611":      true, // M 16 (MWSC)
	"Mel 198":       true, // M 16 (Melotte)
	"NGC 6514":      true, // M 20
	"NGC 6618":      true, // M 17
	"NGC 6820":      true, // with NGC 6823
	"NGC 7380":      true, // Wizard
	"IC 1805":       true, // Heart (MWSC)
	"Mel 15":        true, // IC 1805 Heart (Melotte)
	// Collinder spellings of the above
	"Cr 26":  true, // IC 1805 Heart
	"Cr 68":  true, // NGC 1931 Fly
	"Cr 112": true, // NGC 2264 Cone
	"Cr 164": true, // NGC 2467
	"Cr 360": true, // NGC 6514 M 20
	"Cr 375": true, // NGC 6611 M 16
	"Cr 377": true, // NGC 6618 M 17
	"Cr 404": true, // NGC 6820
	"Cr 439": true, // IC 1396
	"Cr 452": true, // NGC 7380 Wizard
}

// EmissionLine reports whether a target shines mainly in Ha/OIII lines.
func EmissionLine(tg Target) bool {
	return emissionTypes[strings.ToLower(tg.Type)] || emissionNames[tg.Name]
}
