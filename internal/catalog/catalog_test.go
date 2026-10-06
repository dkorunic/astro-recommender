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
			if tg.Constellation == "" { // MWSC and Melotte have no constellation column
				continue
			}
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
	// Every embedded list parses with valid coordinates, which Load keeps
	// in degrees for scoring.
	for _, name := range Lists {
		targets, _, err := Load(name, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			ra, err := astro.Sexagesimal(tg.RA)
			if err != nil {
				t.Fatalf("%s: %s RA %q: %v", name, tg.Name, tg.RA, err)
			}
			dec, err := astro.Sexagesimal(tg.Dec)
			if err != nil {
				t.Fatalf("%s: %s Dec %q: %v", name, tg.Name, tg.Dec, err)
			}
			if tg.RADeg != ra*15 || tg.DecDeg != dec {
				t.Fatalf("%s: %s RADeg, DecDeg = %v, %v; want %v, %v", name, tg.Name, tg.RADeg, tg.DecDeg, ra*15, dec)
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
	// The reason reaches the user, with the text as written.
	f := filepath.Join(t.TempDir(), "t.yaml")
	if err := os.WriteFile(f, []byte(`[{name: X, ra: "00 50 00", dec: "+-10 00 00"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load("", f); err == nil || !strings.Contains(err.Error(), `more than one sign: "+-10 00 00"`) {
		t.Errorf("double sign: %v, want the reason and the text as written", err)
	}
}

// A track key in a -targets file is ignored: Track comes only from comets,
// and a YAML one shorter than the grid crashed scoring.
func TestLoadIgnoresTrack(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.yaml")
	yml := `[{name: X, ra: "00 42 44", dec: "+41 16 09", size: 9000, track: [[10.68, 41.27]]}]`
	if err := os.WriteFile(f, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, _, err := Load("", f)
	if err != nil || len(targets) != 1 || targets[0].Track != nil {
		t.Errorf("Load = %+v, %v; want one target without a track", targets, err)
	}
}

// Each list's spellings of emission nebulae reach EmissionLine: by type
// (case-insensitive) or, for mixed types, by name; reflection-dominated
// objects of those mixed types stay out.
func TestEmissionLineLists(t *testing.T) {
	for list, want := range map[string]map[string]bool{
		"Messier":     {"M 42": true, "M 8": true, "M 20": true, "M 78": false},
		"LBN":         {"LBN 1": true},
		"OpenNGC":     {"NGC 1715": true, "NGC 1736": true},
		"OpenIC":      {"IC 63": true, "IC 1310": true},
		"Pensack500":  {"IC1805": true, "NGC 281": true, "NGC 1977(CLUSTER)": false},
		"Herschel400": {"NGC 7380": true, "NGC 2362": false},
		"GaryImm":     {"Sh2-155": true},
		"MWSC":        {"NGC 2264": true, "NGC 6611": true},
		"Melotte":     {"Mel 49": true, "Mel 198": true, "Mel 22": false},
	} {
		targets, _, err := Load(list, "")
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, tg := range targets {
			if w, ok := want[tg.Name]; ok {
				found++
				if EmissionLine(tg) != w {
					t.Errorf("%s %s (%s): EmissionLine = %v, want %v", list, tg.Name, tg.Type, !w, w)
				}
			}
		}
		if found < len(want) {
			t.Errorf("%s: found %d of %d test targets", list, found, len(want))
		}
	}
}
