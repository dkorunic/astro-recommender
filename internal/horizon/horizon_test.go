// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package horizon

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestHorizonAt(t *testing.T) {
	// horizon.at interpolates and wraps across north.
	h := Horizon{{90, 40}, {180, 20}, {350, 30}}
	// 350° -> 90° spans 100° across north.
	for az, want := range map[float64]float64{135: 30, 90: 40, 0: 31, 355: 30.5, 60: 37} {
		if got := h.At(az); math.Abs(got-want) > 1e-9 {
			t.Errorf("horizon.at(%v) = %v, want %v", az, got, want)
		}
	}
	if (Horizon{}).At(10) != -90 {
		t.Error("empty horizon should not limit")
	}
}

func TestLoadRejects(t *testing.T) {
	for name, text := range map[string]string{
		"nan alt":     "0 NaN\n90 10",
		"inf az":      "Inf 10",
		"az > 360":    "361 10",
		"alt > 90":    "0 91",
		"three cols":  "0 10 20",
		"not numbers": "north 10",
	} {
		f := filepath.Join(t.TempDir(), "h.txt")
		if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	f := filepath.Join(t.TempDir(), "h.txt")
	if err := os.WriteFile(f, []byte("\uFEFF0 25 # BOM: UTF-8 with BOM\n180 20 # trees\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, err := Load(f); err != nil || len(h) != 2 {
		t.Errorf("valid file: %v, %v", h, err)
	}
}

// A point at 360° closes the profile from the west, not at 0°'s side.
func TestLoad360(t *testing.T) {
	f := filepath.Join(t.TempDir(), "h.txt")
	if err := os.WriteFile(f, []byte("0 10\n90 30\n360 20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	for az, want := range map[float64]float64{359: 20 + 10.0/270, 1: 10 + 20.0/90, 45: 20, 0: 10, 90: 30} {
		if got := h.At(az); math.Abs(got-want) > 1e-9 {
			t.Errorf("At(%v) = %v, want %v", az, got, want)
		}
	}
	// 0° and 360° alone are a full turn apart, a ramp from 10° to 20°.
	if got := (Horizon{{0, 10}, {360, 20}}).At(180); got != 15 {
		t.Errorf("0/360 only: At(180) = %v, want 15", got)
	}
	// A 360° point without a 0° one still closes the profile across north.
	if got := (Horizon{{90, 30}, {360, 20}}).At(10); math.Abs(got-(20+10.0/9)) > 1e-9 {
		t.Errorf("90/360: At(10) = %v, want %v", got, 20+10.0/9)
	}
}

// Two points at one azimuth are a vertical step and keep their file order.
func TestLoadSteps(t *testing.T) {
	var text string
	for i := range 36 {
		az := (180 + 10*i) % 360
		text += fmt.Sprintf("%d 10\n%d 40\n", az, az)
	}
	f := filepath.Join(t.TempDir(), "h.txt")
	if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(h); i += 2 {
		if h[i][1] != 10 || h[i+1][1] != 40 {
			t.Errorf("az %v: step %v, %v; want 10, 40", h[i][0], h[i][1], h[i+1][1])
		}
	}
}
