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
// derive from the OpenNGC catalog (CC BY-SA 4.0, (c) Mattia Verga).
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

	Track [][2]float64 // moving targets (comets): RA/Dec in degrees per grid minute
	Size  float64      `yaml:"size"` // major axis in arc minutes; 0 or negative (-9999) means unknown
}

// HasSize reports whether the size is known: comets and entries with a zero
// or negative size (-9999 in the lists, 0.0 in a few LDN entries) have none.
func (t Target) HasSize() bool { return t.Size > 0 }

// Lists are the embedded uptonight target lists selectable with -list.
var Lists = []string{"GaryImm", "GaryImmFull", "Messier", "Herschel400", "Pensack500", "OpenNGC", "OpenIC", "LBN", "LDN"}

var (
	errList        = errors.New("unknown target list")
	errCoordinates = errors.New("bad coordinates")
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
		if err1 != nil || err2 != nil || ra < 0 || ra >= 24 || math.Abs(dec) > 90 {
			return nil, "", fmt.Errorf("%w for %s: %q %q", errCoordinates, tg.Name, sanitize.Text(tg.RA), sanitize.Text(tg.Dec))
		}
		if !num.Finite(tg.Size) {
			return nil, "", fmt.Errorf("%w for %s: %v", errSize, tg.Name, tg.Size)
		}
		tg.Constellation = constellation.Of(ra*15, dec)
	}

	return targets, listName, nil
}

// emissionNames lists targets the catalog types as dark nebula, nova etc.
// although what gets imaged is mostly Ha/OIII emission.
var emissionNames = map[string]bool{
	"IC 1396":       true, // Elephant's Trunk, inside the IC 1396 HII region
	"Sh2-155":       true, // Cave Nebula
	"NGC 2024":      true, // Flame Nebula
	"IC 434":        true, // Horsehead, silhouetted against Ha emission
	"Cederblad 211": true, // R Aquarii symbiotic nebula
}

// EmissionLine reports whether a target shines mainly in Ha/OIII lines.
func EmissionLine(tg Target) bool {
	switch tg.Type {
	case "Emission Nebula", "Planetary Nebula", "Supernova Remnant", "Wolf-Rayet Nebula":
		return true
	}

	return emissionNames[tg.Name]
}
