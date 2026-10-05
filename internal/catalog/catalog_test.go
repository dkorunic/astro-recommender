// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"go.yaml.in/yaml/v3"
)

func TestConstellationLists(t *testing.T) {
	// Against all ~18000 list entries the lookup disagrees only on boundary
	// cases and list errors once spelling variants are normalized.
	norm := strings.NewReplacer("ophiucus", "ophiuchus", "se1", "serpens", "se2", "serpens", "serpens caput", "serpens",
		"serpens claudia", "serpens", "monocerus", "monoceros", "pisces austrinus", "piscis austrinus", "boötes", "bootes")
	var mismatches int
	for _, name := range Lists {
		// GaryImmFull's constellation column is unreliable (72 labels more than
		// 1° away, e.g. vdB 40-49 listed in Taurus but in Orion); the lookup
		// replaces it anyway, so it says nothing about the lookup.
		if name == "GaryImmFull" {
			continue
		}
		data, err := targetFS.ReadFile("targets/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		var raw []Target
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		for _, tg := range raw {
			ra, _ := astro.Sexagesimal(tg.RA)
			dec, _ := astro.Sexagesimal(tg.Dec)
			want := norm.Replace(strings.ToLower(strings.TrimSpace(tg.Constellation)))
			if full, ok := constellation.FullName(want); ok {
				want = strings.ToLower(full)
			}
			if norm.Replace(strings.ToLower(constellation.Of(ra*15, dec))) != want {
				mismatches++
			}
		}
	}
	if mismatches > 5 {
		t.Errorf("constellationOf disagrees with %d list entries, want <= 5", mismatches)
	}
}

func TestTargetLists(t *testing.T) {
	// Every embedded list parses with valid coordinates.
	for _, name := range Lists {
		targets, _, err := Load(name, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			if _, err := astro.Sexagesimal(tg.RA); err != nil {
				t.Fatalf("%s: %s RA %q: %v", name, tg.Name, tg.RA, err)
			}
			if _, err := astro.Sexagesimal(tg.Dec); err != nil {
				t.Fatalf("%s: %s Dec %q: %v", name, tg.Name, tg.Dec, err)
			}
		}
	}
}

func TestLoadRejectsBadCoordinates(t *testing.T) {
	for name, yml := range map[string]string{
		"empty dec": `[{name: X, ra: "00 50 00", dec: ""}]`,
		"dec > 90":  `[{name: X, ra: "00 50 00", dec: "95 00 00"}]`,
		"ra >= 24":  `[{name: X, ra: "24 00 00", dec: "10 00 00"}]`,
		"ra < 0":    `[{name: X, ra: "-01 00 00", dec: "10 00 00"}]`,
		"nan":       `[{name: X, ra: "NaN", dec: "NaN"}]`,
		"size nan":  `[{name: X, ra: "00 50 00", dec: "10 00 00", size: .nan}]`,
		"size inf":  `[{name: X, ra: "00 50 00", dec: "10 00 00", size: .inf}]`,
	} {
		f := filepath.Join(t.TempDir(), "t.yaml")
		if err := os.WriteFile(f, []byte(yml), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load("", f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
