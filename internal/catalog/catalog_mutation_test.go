// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A uniform disc 10′ across covers π/4·600² = 282 743 arcsec², 13.628 mag.
const mutDisc10 = 13.628

func mutSB(t *testing.T, name string, tg Target, want float64, est bool) {
	t.Helper()
	got, e := tg.SurfaceBrightness()
	if math.Abs(got-want) > 0.002 || e != est {
		t.Errorf("%s: SurfaceBrightness = %v %v, want %v %v", name, got, e, want, est)
	}
}

func TestMutSurfaceBrightness(t *testing.T) {
	gal := Target{Type: "Galaxy", Size: 10, Mag: 10}
	mutSB(t, "derived V", gal, 10+mutDisc10, true)
	mutSB(t, "derived 1'", Target{Type: "Galaxy", Size: 1, Mag: 12}, 12+2.5*math.Log10(math.Pi/4*3600), true)
	mutSB(t, "V beats B", Target{Type: "Galaxy", Size: 10, Mag: 10, BMag: 11}, 10+mutDisc10, true)
	mutSB(t, "unknown V, plausible-looking B-V", Target{Type: "Galaxy", BMag: 1, BSurfBr: 23}, 22.2, true)
	mutSB(t, "derived B", Target{Type: "Galaxy", Size: 10, BMag: 11}, 11-0.8+mutDisc10, true)
	mutSB(t, "derived B nebula", Target{Type: "Reflection Nebula", Size: 10, BMag: 11}, 11-0.5+mutDisc10, true)
	mutSB(t, "derived B emission", Target{Type: "HII region", Size: 10, BMag: 11}, 11+mutDisc10, true)
	mutSB(t, "measured V wins", Target{Type: "Galaxy", Size: 10, Mag: 10, SurfBr: 21.5, BSurfBr: 23}, 21.5, false)
	mutSB(t, "own colour", Target{Type: "Galaxy", Mag: 10.2, BMag: 11, BSurfBr: 23}, 22.2, false)
	mutSB(t, "colour -0.3 edge", Target{Type: "Galaxy", Mag: 10, BMag: 9.7, BSurfBr: 23}, 23.3, false)
	mutSB(t, "colour 1.5 edge", Target{Type: "Galaxy", Mag: 10, BMag: 11.5, BSurfBr: 23}, 21.5, false)
	mutSB(t, "implausible colour", Target{Type: "Galaxy", Mag: 9.2, BMag: 15.4, BSurfBr: 23}, 22.2, true)
	mutSB(t, "B only, no V", Target{Type: "Galaxy", BMag: 11, BSurfBr: 23}, 22.2, true)
	mutSB(t, "B emission typical", Target{Type: "Emission Nebula", BSurfBr: 23}, 23, true)
	mutSB(t, "B named emission", Target{Name: "NGC 2024", Type: "Bright Nebula", BSurfBr: 23}, 23, true)
	mutSB(t, "B reflection typical", Target{Type: "Reflection Nebula", BSurfBr: 23}, 22.5, true)
	mutSB(t, "B group typical", Target{Type: "GGroup", BSurfBr: 23}, 22.2, true)
	for name, tg := range map[string]Target{
		"cluster":        {Type: "Open Cluster", Size: 10, Mag: 6, SurfBr: 20},
		"globular":       {Type: "Globular Cluster", Size: 10, Mag: 6, BSurfBr: 20},
		"cluster nebula": {Type: "Cluster Nebulosity", Size: 10, Mag: 6},
		"group":          {Type: "Galaxy Triplet", Size: 10, Mag: 10},
		"star":           {Type: "Star", Size: 10, Mag: 6},
		"no size":        {Type: "Galaxy", Size: -9999, Mag: 10},
		"zero size":      {Type: "Galaxy", Size: 0, Mag: 10},
		"no mag":         {Type: "Galaxy", Size: 10, Mag: -9999},
		"comet":          {Type: "Comet", Size: 10, Mag: 10, Track: [][2]float64{{1, 2}}},
		"comet nebula":   {Type: "Nebula", Size: 10, Mag: 10, Track: [][2]float64{{1, 2}}},
		"bad V, bad B":   {Type: "Galaxy", Size: 10, Mag: -1, BMag: 0.5},
	} {
		if sb, e := tg.SurfaceBrightness(); sb != 0 || e {
			t.Errorf("%s: SurfaceBrightness = %v %v, want 0", name, sb, e)
		}
	}
}

func TestMutKind(t *testing.T) {
	for _, c := range []struct {
		name, typ string
		want      kind
	}{
		{"", "Galaxy", kindGalaxy},
		{"", "Spiral Galaxy", kindGalaxy},
		{"", "Galaxy Triplet", kindGroup},
		{"", "Galaxy Pair", kindGroup},
		{"", "Galaxy Cluster", kindGroup},
		{"", "GTrpl", kindGroup},
		{"", "Open Cluster", kindCluster},
		{"", "OPEN CLUSTER", kindCluster},
		{"", "Globular Cluster", kindCluster},
		{"M 16", "Open Cluster", kindClusterNebula},
		{"M 16", "Emission Nebula", kindNebula},
		{"Cederblad 211", "Star", kindOther},
		{"", "Cluster Nebulosity", kindClusterNebula},
		{"", "Cluster with Nebulosity", kindClusterNebula},
		{"", "Cl+N Emission Nebula", kindClusterNebula},
		{"", "N+CL", kindClusterNebula},
		{"", "BN+OC", kindClusterNebula},
		{"", "HII region", kindNebula},
		{"", "EmN", kindNebula},
		{"", "Dark Nebula", kindNebula},
		{"", "DN", kindNebula},
		{"", "Star Cloud", kindNebula},
		{"", "Molecular Cloud", kindNebula},
		{"", "Planetary Nebula", kindNebula},
		{"", "Asterism", kindOther},
		{"", "Double Star", kindOther},
	} {
		if got := (Target{Name: c.name, Type: c.typ}).kind(); got != c.want {
			t.Errorf("kind(%q, %q) = %v, want %v", c.name, c.typ, got, c.want)
		}
	}
	if (Target{Name: "M 16", Type: "Open Cluster"}).Cluster() || !(Target{Name: "M 11", Type: "Open Cluster"}).Cluster() {
		t.Error("Cluster() wrong for M 16 / M 11")
	}
}

func TestMutEmissionNameKeyPosition(t *testing.T) {
	for _, c := range []struct {
		tg   Target
		want bool
	}{
		{Target{Type: "HII Region"}, true},
		{Target{Type: "planetary nebula"}, true},
		{Target{Type: "Supernova Remnant"}, true},
		{Target{Type: "EMN"}, true},
		{Target{Name: "IC 434", Type: "Dark Nebula"}, true},
		{Target{Name: "ic 434", Type: "Dark Nebula"}, false},
		{Target{Name: "M 78", Type: "Diffuse Nebula"}, false},
		{Target{Type: "Reflection Nebula"}, false},
	} {
		if got := EmissionLine(c.tg); got != c.want {
			t.Errorf("EmissionLine(%+v) = %v", c.tg, got)
		}
	}
	if k := NameKey("NGC 7789 "); k != "ngc7789" {
		t.Errorf("NameKey = %q", k)
	}
	if NameKey("Sh2-155") != NameKey("sh2 - 155") {
		t.Error("NameKey spaces")
	}
	tg := Target{RADeg: 1, DecDeg: 2, Track: [][2]float64{{10, 1}, {20, 2}, {30, 3}, {40, 4}}}
	if ra, dec := tg.Position(); ra != 30 || dec != 3 {
		t.Errorf("Position(track) = %v %v", ra, dec)
	}
	if ra, dec := (Target{RADeg: 1, DecDeg: 2}).Position(); ra != 1 || dec != 2 {
		t.Errorf("Position = %v %v", ra, dec)
	}
	if (Target{Size: 0.01}).HasSize() == false || (Target{Size: 0}).HasSize() || (Target{Mag: 0}).HasMag() || !(Target{Mag: 0.1}).HasMag() {
		t.Error("HasSize/HasMag sentinels")
	}
}

func mutYAML(t *testing.T, body string) ([]Target, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tg, _, err := Load("", p)

	return tg, err
}

func mutEntry(ra, dec, extra string) string {
	return "- name: X\n  type: Galaxy\n  ra: \"" + ra + "\"\n  dec: \"" + dec + "\"\n" + extra
}

func TestMutLoadFile(t *testing.T) {
	tg, err := mutYAML(t, "---\n- name: \"  M 42\\u001b[1m \"\n  description: \"Orion\\u202e\"\n  type: \" Emission Nebula \"\n  constellation: Taurus\n  ra: \"05 35 17\"\n  dec: \"-05 23 28\"\n  size: 85\n  mag: 4\n")
	if err != nil || len(tg) != 1 {
		t.Fatalf("Load: %v %v", tg, err)
	}
	m := tg[0]
	if m.Name != "M 42[1m" || m.Type != "Emission Nebula" || m.Description != "Orion" {
		t.Errorf("sanitized %q %q %q", m.Name, m.Type, m.Description)
	}
	wantRA := (5 + 35.0/60 + 17.0/3600) * 15
	if math.Abs(m.RADeg-wantRA) > 1e-9 || math.Abs(m.DecDeg-(-5-23.0/60-28.0/3600)) > 1e-9 {
		t.Errorf("degrees %v %v", m.RADeg, m.DecDeg)
	}
	if m.Constellation != "Orion" && !strings.Contains(m.Constellation, "Ori") {
		t.Errorf("constellation %q, want Orion", m.Constellation)
	}
	for _, ok := range []string{
		mutEntry("23 59 59", "+90 00 00", ""),
		mutEntry("00 00 00", "-90 00 00", "  size: 10800\n  mag: 35\n  bmag: 35\n  surfbr: 10\n  bsurfbr: 35\n"),
		mutEntry("12 00 00", "+10", "  size: -9999\n  mag: -9999\n  surfbr: -9999\n"),
		"",
		"---\n",
		"# comment only\n",
		"---\n" + mutEntry("12 00 00", "+10", "") + "---\n",
	} {
		if _, err := mutYAML(t, ok); err != nil {
			t.Errorf("rejected %q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		mutEntry("24 00 00", "+10", ""),
		mutEntry("-01 00 00", "+10", ""),
		mutEntry("12 00 00", "+90 00 01", ""),
		mutEntry("12 00 00", "-91", ""),
		mutEntry("12 00 00", "", ""),
		mutEntry("12 00 00", "+10", "  size: 10801\n"),
		mutEntry("12 00 00", "+10", "  mag: 35.1\n"),
		mutEntry("12 00 00", "+10", "  bmag: 36\n"),
		mutEntry("12 00 00", "+10", "  surfbr: 9.9\n"),
		mutEntry("12 00 00", "+10", "  bsurfbr: 2.3\n"),
		mutEntry("12 00 00", "+10", "  surfbr: 35.5\n"),
		mutEntry("12 00 00", "+10", "  size: .nan\n"),
		mutEntry("12 00 00", "+10", "  mag: .inf\n"),
		mutEntry("12 00 00", "+10", "  bsurfbr: -.inf\n"),
		mutEntry("12 00 00", "+10", "") + "---\n" + mutEntry("12 00 00", "+10", ""),
		mutEntry("12 00 00", "+10", "") + mutEntry("13 00 00", "+10", "") + mutEntry("bad", "+10", ""),
	} {
		if tg, err := mutYAML(t, bad); err == nil {
			t.Errorf("accepted %q: %+v", bad, tg)
		}
	}
}

func TestMutLoadEmbedded(t *testing.T) {
	tg, name, err := Load("messier", "")
	if err != nil || name != "Messier" || len(tg) != 110 {
		t.Fatalf("Load(messier) = %d %q %v", len(tg), name, err)
	}
	for _, m := range tg {
		if m.Name == "M 31" && (math.Abs(m.RADeg-10.68) > 0.1 || math.Abs(m.DecDeg-41.27) > 0.1) {
			t.Errorf("M 31 at %v %v", m.RADeg, m.DecDeg)
		}
	}
	if _, _, err := Load("nope", ""); err == nil {
		t.Error("unknown list accepted")
	}
}
