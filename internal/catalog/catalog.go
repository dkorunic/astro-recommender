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
	"go.yaml.in/yaml/v3"
)

// Target lists from uptonight (MIT, (c) Markus Winkler); OpenNGC and OpenIC
// derive from the OpenNGC catalog (CC BY-SA 4.0, (c) Mattia Verga); MWSC is
// Kharchenko et al. (2013, CDS J/A+A/558/A53); Melotte cross-identifies Melotte
// (1915) and Collinder (1931) with NGC/IC and take positions from MWSC,
// OpenNGC, Dias et al. (2002, CDS B/ocl) and SIMBAD; PNnet is Le Du et al.
// (2022, CDS J/A+A/666/A152).
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
	RADeg  float64 `yaml:"-"`
	DecDeg float64 `yaml:"-"`
	Size   float64 `yaml:"size"` // major axis in arc minutes; 0 or negative (-9999) means unknown
}

// HasSize reports whether the size is known: comets and entries with a zero
// or negative size (-9999 in the lists, 0.0 in a few LDN entries) have none.
func (t Target) HasSize() bool { return t.Size > 0 }

// Lists are the embedded uptonight target lists selectable with -list.
var Lists = []string{"GaryImm", "GaryImmFull", "Messier", "Herschel400", "Pensack500", "OpenNGC", "OpenIC", "LBN", "LDN", "MWSC", "Melotte", "Collinder", "PNnet"}

var (
	errList        = errors.New("unknown target list")
	errCoordinates = errors.New("bad coordinates")
	errSky         = errors.New("RA must be 0 to 24h and Dec within ±90°")
	errSize        = errors.New("bad size")
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
		tg.Name, tg.Description = sanitize.Text(tg.Name), sanitize.Text(tg.Description)
		tg.Type = sanitize.Text(tg.Type)
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
		if !num.Finite(tg.Size) {
			return nil, "", fmt.Errorf("%w for %s: %v", errSize, tg.Name, tg.Size)
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
