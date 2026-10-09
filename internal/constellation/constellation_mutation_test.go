// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package constellation

import "testing"

// J2000 positions of bright stars and objects whose constellations are
// textbook facts, spread over both poles and both sides of 0h.
func TestMutOf(t *testing.T) {
	for _, c := range []struct {
		name    string
		ra, dec float64
		want    string
	}{
		{"Polaris", 37.95, 89.26, "Ursa Minor"},
		{"Kochab", 222.68, 74.16, "Ursa Minor"},
		{"Thuban", 211.10, 64.38, "Draco"},
		{"Dubhe", 165.93, 61.75, "Ursa Major"},
		{"Errai", 354.84, 77.63, "Cepheus"},
		{"sigma Oct", 317.20, -88.96, "Octans"},
		{"M 31", 10.68, 41.27, "Andromeda"},
		{"Alpheratz", 2.10, 29.09, "Andromeda"},
		{"Caph", 2.29, 59.15, "Cassiopeia"},
		{"omega Psc", 359.83, 6.86, "Pisces"},
		{"Algenib", 3.31, 15.18, "Pegasus"},
		{"Fomalhaut", 344.41, -29.62, "Piscis Austrinus"},
		{"M 42", 83.82, -5.39, "Orion"},
		{"Betelgeuse", 88.79, 7.41, "Orion"},
		{"Sirius", 101.29, -16.72, "Canis Major"},
		{"Vega", 279.23, 38.78, "Lyra"},
		{"M 45", 56.75, 24.12, "Taurus"},
		{"Rigil Kentaurus", 219.90, -60.83, "Centaurus"},
		{"Acrux", 186.65, -63.10, "Crux"},
		{"Antares", 247.35, -26.43, "Scorpius"},
		{"Deneb", 310.36, 45.28, "Cygnus"},
		{"Achernar", 24.43, -57.24, "Eridanus"},
	} {
		if got := Of(c.ra, c.dec); got != c.want {
			t.Errorf("Of(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMutFullName(t *testing.T) {
	for abbr, want := range map[string]string{"ori": "Orion", "UMa": "Ursa Major", "PSA": "Piscis Austrinus"} {
		if got, ok := FullName(abbr); !ok || got != want {
			t.Errorf("FullName(%q) = %q %v", abbr, got, ok)
		}
	}
	if _, ok := FullName("Xyz"); ok {
		t.Error("FullName(Xyz) found")
	}
}

func TestMutMeridianCrosses(t *testing.T) {
	for _, c := range []struct {
		a, b, ra float64
		want     bool
	}{
		{1, 3, 2, true},
		{3, 1, 2, true},
		{1, 3, 1, true},
		{1, 3, 3, false},
		{1, 3, 0.5, false},
		{23, 1, 0.5, true},
		{23, 1, 23.5, true},
		{23, 1, 12, false},
		{1, 23, 23, true},
		{1, 23, 1, false},
	} {
		if got := meridianCrosses(c.a, c.b, c.ra); got != c.want {
			t.Errorf("meridianCrosses(%v, %v, %v) = %v", c.a, c.b, c.ra, got)
		}
	}
}

func TestMutWindsAroundNorthPole(t *testing.T) {
	ring := func(dec float64) []boundaryPoint {
		return []boundaryPoint{{0, dec}, {6, dec}, {12, dec}, {18, dec}}
	}
	if !windsAroundNorthPole(ring(80)) {
		t.Error("north cap not polar")
	}
	if windsAroundNorthPole(ring(-80)) {
		t.Error("south cap polar")
	}
	if windsAroundNorthPole([]boundaryPoint{{1, 10}, {3, 10}, {3, 20}, {1, 20}}) {
		t.Error("box polar")
	}
}
