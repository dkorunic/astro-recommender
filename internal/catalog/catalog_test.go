// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"go.yaml.in/yaml/v4"
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
		data, err := os.ReadFile(filepath.Join("targets", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var raw []Target
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		for _, tg := range raw {
			if tg.Constellation == "" { // generated lists have no constellation column
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
		"empty dec":   `[{name: X, ra: "00 50 00", dec: ""}]`,
		"dec > 90":    `[{name: X, ra: "00 50 00", dec: "95 00 00"}]`,
		"ra >= 24":    `[{name: X, ra: "24 00 00", dec: "10 00 00"}]`,
		"ra < 0":      `[{name: X, ra: "-01 00 00", dec: "10 00 00"}]`,
		"nan":         `[{name: X, ra: "NaN", dec: "NaN"}]`,
		"size nan":    `[{name: X, ra: "00 50 00", dec: "10 00 00", size: .nan}]`,
		"size inf":    `[{name: X, ra: "00 50 00", dec: "10 00 00", size: .inf}]`,
		"mag nan":     `[{name: X, ra: "00 50 00", dec: "10 00 00", mag: .nan}]`,
		"bmag inf":    `[{name: X, ra: "00 50 00", dec: "10 00 00", bmag: .inf}]`,
		"surfbr nan":  `[{name: X, ra: "00 50 00", dec: "10 00 00", surfbr: .nan}]`,
		"bsurfbr nan": `[{name: X, ra: "00 50 00", dec: "10 00 00", bsurfbr: .nan}]`,
		"mag 40":      `[{name: X, ra: "00 50 00", dec: "10 00 00", mag: 40}]`, // nothing in a target list is that faint
		"surfbr 99":   `[{name: X, ra: "00 50 00", dec: "10 00 00", surfbr: 99}]`,
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

// Unknown (-9999) and ordinary values pass the range check.
func TestLoadAcceptsKnownAndUnknownNumbers(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.yaml")
	yml := `[{name: X, ra: "00 50 00", dec: "10 00 00", size: -9999, mag: -9999, bmag: 3.4, surfbr: 22.0, bsurfbr: 34.9}]`
	if err := os.WriteFile(f, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load("", f); err != nil {
		t.Fatal(err)
	}
}

// A second YAML document is an error, not silently dropped; an empty file
// or an empty trailing document loses nothing and loads.
func TestLoadDocuments(t *testing.T) {
	const list = `[{name: X, ra: "00 50 00", dec: "10 00 00"}]`
	for yml, want := range map[string]int{
		"":                                  0,
		"# only a comment\n":                0,
		list + "\n---\n":                    1,
		list + "\n---\n" + list + "\n":      -1,
		list + "\n---\n---\n" + list + "\n": -1, // an empty document does not end the file
		"---\n---\n" + list + "\n":          1,  // nor does a leading one count as the list
	} {
		f := filepath.Join(t.TempDir(), "t.yaml")
		if err := os.WriteFile(f, []byte(yml), 0o600); err != nil {
			t.Fatal(err)
		}
		targets, _, err := Load("", f)
		switch {
		case want < 0 && !errors.Is(err, errDocuments):
			t.Errorf("%q: Load = %v, want errDocuments", yml, err)
		case want >= 0 && (err != nil || len(targets) != want):
			t.Errorf("%q: Load = %d targets, %v; want %d", yml, len(targets), err, want)
		}
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
		"Messier":     {"M 42": true, "M 8": true, "M 16": true, "M 20": true, "M 78": false},
		"LBN":         {"LBN 1": true},
		"OpenNGC":     {"NGC 1715": true, "NGC 1736": true},
		"OpenIC":      {"IC 63": true, "IC 1310": true},
		"Pensack500":  {"IC1805": true, "NGC 281": true, "NGC 1977(CLUSTER)": false},
		"Herschel400": {"NGC 7380": true, "NGC 2362": false},
		"GaryImm":     {"Sh2-155": true},
		"MWSC":        {"NGC 2264": true, "NGC 6611": true, "IC 1805": true},
		"Melotte":     {"Mel 15": true, "Mel 49": true, "Mel 198": true, "Mel 22": false},
		"PNnet":       {"Ou 2": true, "StDr 56": true},
		"HASH":        {"NGC 7293": true, "Sh 2-216": true},
		"Sharpless":   {"Sh2-155": true, "Sh2-49": true, "Sh2-1": false},
		"Collinder":   {"Cr 26": true, "Cr 375": true, "Cr 429": false, "Cr 399": false},
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

func TestSurfaceBrightness(t *testing.T) {
	// A 10' uniform disc: 2.5 log10(pi/4 * 600"^2) = 13.63 mag fainter than the integrated magnitude.
	disc := 2.5 * math.Log10(math.Pi/4*600*600)
	for name, c := range map[string]struct {
		tg      Target
		want    float64 // 0: none
		derived bool
	}{
		"measured V wins":      {Target{SurfBr: 22.5, BSurfBr: 23.3, BMag: 10, Mag: 9, Size: 10}, 22.5, false},
		"B minus own colour":   {Target{BSurfBr: 23.3, BMag: 10, Mag: 9.1}, 22.4, false},
		"own colour on bound":  {Target{Type: "Galaxy", BSurfBr: 23.3, BMag: 9.7, Mag: 10}, 23.6, false}, // 9.7 - 10 rounds below -0.3
		"B minus galaxy 0.8":   {Target{Type: "Galaxy", BSurfBr: 23.3, Mag: 9.1}, 22.5, true},
		"B minus group 0.8":    {Target{Type: "GTrpl", BSurfBr: 23.3}, 22.5, true},
		"B minus emission 0":   {Target{Type: "Planetary Nebula", BSurfBr: 23.3}, 23.3, true},
		"B minus other 0.5":    {Target{Type: "Reflection Nebula", BSurfBr: 23.3}, 22.8, true},
		"implausible colour":   {Target{Type: "Galaxy", BSurfBr: 23.3, BMag: 15.35, Mag: 9.2}, 22.5, true}, // IC 127: typical instead
		"derived galaxy":       {Target{Type: "Galaxy", Mag: 10, Size: 10}, 10 + disc, true},
		"derived from B":       {Target{Type: "Galaxy", BMag: 10.8, Size: 10}, 10 + disc, true},
		"derived HII from B":   {Target{Type: "HII Emission Nebula", BMag: 10, Size: 10}, 10 + disc, true},
		"derived reflection":   {Target{Type: "Reflection Nebula", Mag: 10, Size: 10}, 10 + disc, true},
		"derived cloud":        {Target{Type: "Molecular Cloud", Mag: 10, Size: 10}, 10 + disc, true},
		"derived DN":           {Target{Type: "DN", Mag: 10, Size: 10}, 10 + disc, true},   // Pensack's dark nebula
		"cluster not derived":  {Target{Type: "Open Cluster", Mag: 6, Size: 13}, 0, false}, // stars, and MWSC sizes are the core only
		"globular not derived": {Target{Type: "Globular Cluster", Mag: 6, Size: 13}, 0, false},
		"cluster measured":     {Target{Type: "Open Cluster", SurfBr: 22.49}, 0, false},                                      // Toadstool: skyK's 0.5 already counts
		"cluster nebulosity":   {Target{Type: "Cluster Nebulosity", Mag: 3.8, Size: 8}, 0, false},                            // NGC 2362: the stars' magnitude
		"Cl+N not derived":     {Target{Type: "Cl+N Emission Nebula", Mag: 4.8, Size: 24}, 0, false},                         // NGC 2244
		"N+CL not derived":     {Target{Type: "N+CL", Mag: 4.8, Size: 24}, 0, false},                                         // Pensack's spelling
		"Cl+N measured":        {Target{Type: "Cluster Nebulosity", SurfBr: 21}, 21, false},                                  // a measurement of the glow wins
		"star cloud":           {Target{Type: "Star Cloud", Mag: 14, Size: 5}, 14 + 2.5*math.Log10(math.Pi/4*300*300), true}, // NGC 206 in M31: a faint patch
		"triplet not derived":  {Target{Type: "Galaxy Triplet", Mag: 12, Size: 3}, 0, false},                                 // a -targets spelling
		"M 16 measured":        {Target{Name: "M 16", Type: "Open Cluster", SurfBr: 21}, 21, false},                          // emission-named cluster: nebulosity, measurement wins
		"M 16 not derived":     {Target{Name: "M 16", Type: "Open Cluster", Mag: 6, Size: 7}, 0, false},
		"cluster with nebula":  {Target{Type: "Open Cluster with Nebulosity", Mag: 4, Size: 20}, 0, false}, // a -targets spelling
		"star not derived":     {Target{Type: "**", Mag: 9, Size: 0.5}, 0, false},
		"asterism not derived": {Target{Type: "Asterism", Mag: 5, Size: 60}, 0, false},
		"untyped not derived":  {Target{Mag: 10, Size: 10}, 0, false},
		"group not derived":    {Target{Type: "Galaxy Cluster", Mag: 11.6, Size: 150}, 0, false}, // one member's mag over the group's area
		"pair not derived":     {Target{Type: "Galaxy Pair", Mag: 12, Size: 3}, 0, false},
		"group measured ok":    {Target{Type: "Galaxy Group", SurfBr: 22.1}, 22.1, false},
		"mag only":             {Target{Type: "Galaxy", Mag: 10}, 0, false},
		"size only":            {Target{Type: "Galaxy", Size: 10}, 0, false},
		"comet":                {Target{Type: "Comet", Mag: 8, Size: 10, Track: [][2]float64{{0, 0}}}, 0, false},
		"nothing":              {Target{}, 0, false},
	} {
		got, derived := c.tg.SurfaceBrightness()
		if math.Abs(got-c.want) > 1e-9 || derived != c.derived {
			t.Errorf("%s: SurfaceBrightness() = %v, %v; want %v, %v", name, got, derived, c.want, c.derived)
		}
	}
}

// TestKindLists pins the kind of every type spelling in the embedded lists,
// so a regenerated list cannot smuggle in a spelling that lands in the wrong
// kind (a group derived as one galaxy, a cluster+nebula as a nebula) or in
// none.
func TestKindLists(t *testing.T) {
	kinds := map[string]kind{
		"galaxy": kindGalaxy, "spiral galaxy": kindGalaxy, "elliptical galaxy": kindGalaxy, "lenticular (s0) galaxy": kindGalaxy, "irregular galaxy": kindGalaxy,
		"galaxy group": kindGroup, "galaxy duo": kindGroup, "galaxy cluster": kindGroup, "galaxy pair": kindGroup, "gtrpl": kindGroup, "ggroup": kindGroup,
		"open cluster": kindCluster, "globular cluster": kindCluster,
		"cluster nebulosity": kindClusterNebula, "cl+n emission nebula": kindClusterNebula, "n+cl": kindClusterNebula, "bn+oc": kindClusterNebula, "cl+n": kindClusterNebula,
		"emission nebula": kindNebula, "planetary nebula": kindNebula, "reflection nebula": kindNebula, "supernova remnant": kindNebula,
		"dark nebula": kindNebula, "wolf-rayet nebula": kindNebula, "molecular cloud": kindNebula, "preplanetary nebula": kindNebula,
		"nebula": kindNebula, "hii region": kindNebula, "hii emission nebula": kindNebula, "emn": kindNebula, "dn": kindNebula,
		"diffuse nebula": kindNebula, "variable nebula": kindNebula, "star cloud": kindNebula,
		"*": kindOther, "**": kindOther, "*ass": kindOther, "*'s": kindOther, "star": kindOther, "double star": kindOther, "variable star": kindOther,
		"asterism": kindOther, "group/asterism": kindOther, "nova": kindOther, "young stellar object": kindOther, "herbig-haro object": kindOther,
		"other": kindOther, "duplicate": kindOther, "nonex": kindOther,
	}
	for _, list := range Lists {
		targets, _, err := Load(list, "")
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, tg := range targets {
			typ := strings.ToLower(tg.Type)
			if seen[typ] {
				continue
			}
			seen[typ] = true
			want, ok := kinds[typ]
			if !ok {
				t.Errorf("%s: type %q is new; add it to kindTypes if the substring tests misread it, and here", list, tg.Type)
			} else if got := (Target{Type: tg.Type}).kind(); got != want { // by type alone: a name in emissionNames may promote a cluster
				t.Errorf("%s: type %q: kind = %v, want %v", list, tg.Type, got, want)
			}
		}
	}
}
