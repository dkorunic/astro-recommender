// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package horizon

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func mutLoad(t *testing.T, body string) (Horizon, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "h.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return Load(p)
}

func TestMutLoadBounds(t *testing.T) {
	for _, bad := range []string{"361 10", "-1 10", "10 91", "10 -91", "NaN 10", "10 NaN", "Inf 10", "10 20 30", "10"} {
		if _, err := mutLoad(t, bad+"\n"); err == nil {
			t.Errorf("Load(%q) accepted", bad)
		}
	}
	h, err := mutLoad(t, "\uFEFF# comment\n360 40 # north\n0 0\n\n180 20\n90 90\n270 -90\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Horizon{{0, 0}, {90, 90}, {180, 20}, {270, -90}, {360, 40}}
	if len(h) != len(want) {
		t.Fatalf("Load = %v, want %v", h, want)
	}
	for i := range want {
		if h[i] != want[i] {
			t.Fatalf("Load = %v, want %v", h, want)
		}
	}
}

func TestMutAt(t *testing.T) {
	if got := (Horizon{}).At(123); got != -90 {
		t.Errorf("empty At = %v, want -90", got)
	}
	// Two points straddling north: 350° at 0, 10° at 20, so north is 10.
	h := Horizon{{10, 20}, {350, 0}}
	for _, c := range [][2]float64{{0, 10}, {355, 5}, {5, 15}, {180, 10}, {10, 20}, {350, 0}} {
		if got := h.At(c[0]); math.Abs(got-c[1]) > 1e-9 {
			t.Errorf("At(%v) = %v, want %v", c[0], got, c[1])
		}
	}
	// 0° and 360° are a full turn apart: interpolate through the middle.
	h = Horizon{{0, 0}, {180, 20}, {360, 40}}
	if got := h.At(270); math.Abs(got-30) > 1e-9 {
		t.Errorf("At(270) = %v, want 30", got)
	}
	if got := (Horizon{{0, 0}, {360, 40}}).At(90); math.Abs(got-10) > 1e-9 {
		t.Errorf("At(90) between 0 and 360 = %v, want 10", got)
	}
	// A vertical step keeps file order; At on it returns the first.
	h = Horizon{{0, 0}, {90, 10}, {90, 30}, {180, 30}}
	if got := h.At(90); got != 10 {
		t.Errorf("At(step) = %v, want 10", got)
	}
	if got := h.At(45); math.Abs(got-5) > 1e-9 {
		t.Errorf("At(45) = %v, want 5", got)
	}
	if got := (Horizon{{100, 25}}).At(300); got != 25 {
		t.Errorf("single point At = %v, want 25", got)
	}
}

// Many points at one azimuth must keep their file order through the sort
// (an unstable sort reorders them once there are more than a dozen).
func TestMutLoadStable(t *testing.T) {
	body := "0 0\n"
	for i := range 40 {
		body += "90 " + string(rune('1'+i%9)) + "\n"
	}
	body += "180 5\n45 3\n"
	h, err := mutLoad(t, body)
	if err != nil {
		t.Fatal(err)
	}
	k := 0
	for _, p := range h {
		if p[0] != 90 {
			continue
		}
		if want := float64(1 + k%9); p[1] != want {
			t.Fatalf("point %d at 90 = %v, want %v (file order)", k, p[1], want)
		}
		k++
	}
}
