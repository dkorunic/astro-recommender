// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mutParse runs Parse on a fresh flag set with args after the program name.
func mutParse(t *testing.T, args ...string) (Config, error) {
	t.Helper()
	oldArgs, oldCL := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldCL })
	os.Args = append([]string{"astro-recommender"}, args...)
	flag.CommandLine = flag.NewFlagSet("astro-recommender", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	return Parse()
}

func mutBase(extra ...string) []string {
	return append([]string{"-lat", "45.81", "-lon", "15.98", "-tz", "Europe/Zagreb"}, extra...)
}

func mutClose(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// The Origin: 3856×2180 px of 2 µm at 335 mm: 2·atan(7.712/670) = 1.31897°
// = 79.14′, 2·atan(4.36/670) = 44.74′; 206265·2/335000 = 1.2314″/px.
func TestMutOriginFrame(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-origin")...)
	if err != nil {
		t.Fatal(err)
	}
	mutClose(t, "FOVLong", cfg.FOVLong, 79.138, 0.01)
	mutClose(t, "FOVShort", cfg.FOVShort, 44.74, 0.01)
	mutClose(t, "Scale", cfg.Scale, 1.23143, 1e-4)
	mutClose(t, "SizeMin", cfg.SizeMin, 200*1.23143/60, 1e-3)
	mutClose(t, "SizeMax", cfg.SizeMax, 44.74, 0.01)
	if !cfg.Framing {
		t.Error("-origin not framing")
	}
	l, s := cfg.FramePx()
	mutClose(t, "FramePx long", l, 3856, 1)
	mutClose(t, "FramePx short", s, 2180, 1)
	// Explicit sizes win over the frame defaults.
	cfg, err = mutParse(t, mutBase("-origin", "-size-min", "0", "-size-max", "40", "-min-px", "500")...)
	if err != nil || cfg.SizeMin != 0 || cfg.SizeMax != 40 {
		t.Errorf("explicit sizes: %v %v %v", cfg.SizeMin, cfg.SizeMax, err)
	}
	// An explicit -size-min equal to the plain default still wins.
	cfg, err = mutParse(t, mutBase("-origin", "-size-min", "10")...)
	if err != nil || cfg.SizeMin != 10 {
		t.Errorf("explicit -size-min 10: %v %v", cfg.SizeMin, err)
	}
	// A lone -scale frames too, with the Origin's field.
	cfg, err = mutParse(t, mutBase("-scale", "2")...)
	if err != nil || !cfg.Framing || math.Abs(cfg.SizeMax-44.74) > 0.01 || math.Abs(cfg.SizeMin-200*2.0/60) > 1e-9 {
		t.Errorf("-scale alone: %v %v %v %v", cfg.Framing, cfg.SizeMin, cfg.SizeMax, err)
	}
	if _, err := mutParse(t, mutBase("-pixel", "3.76")...); err == nil || !strings.Contains(err.Error(), "need -focal") {
		t.Errorf("-pixel without -focal: %v", err)
	}
	// -min-px scales the default minimum.
	cfg, _ = mutParse(t, mutBase("-origin", "-min-px", "100")...)
	mutClose(t, "SizeMin 100px", cfg.SizeMin, 100*1.23143/60, 1e-3)
	if _, err := mutParse(t, mutBase("-origin", "-size-max", "45")...); err == nil {
		t.Error("-size-max above the short side accepted")
	}
	// No framing: plain defaults.
	cfg, err = mutParse(t, mutBase()...)
	if err != nil || cfg.Framing || cfg.SizeMin != 10 || cfg.SizeMax != 300 || cfg.AltMin != 30 || cfg.AltMax != 80 || cfg.Top != 20 {
		t.Errorf("defaults %+v %v", cfg, err)
	}
	if cfg.Day.Location().String() != "Europe/Zagreb" || cfg.DateSet {
		t.Errorf("day %v", cfg.Day)
	}
}

func TestMutOtherFrames(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-fov", "1.4x2.1", "-scale", "1.5")...)
	if err != nil || cfg.FOVLong != 126 || cfg.FOVShort != 84 || cfg.Scale != 1.5 || cfg.SizeMax != 84 {
		t.Errorf("-fov: %v %v %v %v %v", cfg.FOVLong, cfg.FOVShort, cfg.Scale, cfg.SizeMax, err)
	}
	// A wide lone -fov is fine whatever the Origin's scale says.
	if _, err := mutParse(t, mutBase("-fov", "10x7")...); err != nil {
		t.Errorf("-fov 10x7: %v", err)
	}
	// APS-C 23.5×15.6 mm at 400 mm: 2·atan(23.5/800) = 3.3652° = 201.9′;
	// 3.76 µm there is 206.265·3.76/400 = 1.9389″/px.
	cfg, err = mutParse(t, mutBase("-focal", "400", "-sensor", "15.6x23.5", "-pixel", "3.76")...)
	if err != nil {
		t.Fatal(err)
	}
	mutClose(t, "sensor long", cfg.FOVLong, 2*math.Atan(23.5/800)*180/math.Pi*60, 1e-9)
	mutClose(t, "sensor long approx", cfg.FOVLong, 201.9, 0.05)
	mutClose(t, "sensor short", cfg.FOVShort, 2*math.Atan(15.6/800)*180/math.Pi*60, 1e-9)
	mutClose(t, "pixel scale", cfg.Scale, 1.93889, 1e-4)
	// -pixel alone with -focal keeps the Origin field.
	cfg, err = mutParse(t, mutBase("-focal", "1000", "-pixel", "2")...)
	if err != nil || math.Abs(cfg.FOVShort-44.74) > 0.01 || math.Abs(cfg.Scale-0.41253) > 1e-4 {
		t.Errorf("-focal -pixel: %v %v %v", cfg.FOVShort, cfg.Scale, err)
	}
	// 20000 px across is a sensor; more is not.
	if _, err := mutParse(t, mutBase("-fov", "5x1", "-scale", "0.9")...); err != nil {
		t.Errorf("20000 px rejected: %v", err)
	}
	if _, err := mutParse(t, mutBase("-fov", "5x1", "-scale", "0.89")...); err == nil {
		t.Error("20225 px accepted")
	}
	for _, args := range [][]string{
		{"-sensor", "23.5x15.6"},
		{"-pixel", "3.76"},
		{"-focal", "400"},
		{"-focal", "0", "-sensor", "23.5x15.6"},
		{"-focal", "-5", "-pixel", "3"},
		{"-fov", "2x1", "-focal", "400", "-sensor", "23.5x15.6"},
		{"-scale", "1", "-focal", "400", "-pixel", "3.76"},
		{"-focal", "400", "-pixel", "0"},
		{"-focal", "1", "-pixel", "10", "-min-px", "0"},
		{"-focal", "100000", "-pixel", "1"},
		{"-scale", "0.009"},
		{"-scale", "1000.1"},
		{"-fov", "181x1"},
		{"-fov", "1x181"},
		{"-fov", "0x1"},
		{"-fov", "1x-1"},
		{"-fov", "2x"},
		{"-fov", "2"},
		{"-fov", "NaNx1"},
		{"-min-px", "-1"},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	for _, args := range [][]string{{"-scale", "0.01", "-fov", "0.05x0.05"}, {"-scale", "1000", "-min-px", "1"}, {"-fov", "180x180", "-size-max", "10"}, {"-min-px", "0", "-origin"}} {
		if _, err := mutParse(t, mutBase(args...)...); err != nil {
			t.Errorf("%v rejected: %v", args, err)
		}
	}
}

func TestMutValidate(t *testing.T) {
	for _, args := range [][]string{
		{"-tz", "UTC"},
		{"-lat", "45", "-tz", "UTC"},
		{"-lon", "15", "-tz", "UTC"},
		{"-lat", "91", "-lon", "0", "-tz", "UTC"},
		{"-lat", "-91", "-lon", "0", "-tz", "UTC"},
		{"-lat", "0", "-lon", "181", "-tz", "UTC"},
		{"-lat", "NaN", "-lon", "0", "-tz", "UTC"},
		{"-lat", "0", "-lon", "Inf", "-tz", "UTC"},
	} {
		if _, err := mutParse(t, args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := mutParse(t, "-lat", "-90", "-lon", "-180", "-tz", "UTC"); err != nil {
		t.Errorf("pole and antimeridian rejected: %v", err)
	}
	for _, args := range [][]string{
		{"-extinction", "Inf"},
		{"-plan", "5m"},
		{"-plan", "9m59s"},
		{"-plan", "90s"},
		{"-plan", "10m30s"},
		{"-comet-mag", "Inf"},
		{"-comet-mag", "NaN"},
		{"-min-run", "-1m"},
		{"-n", "0"},
		{"-alt-min", "30", "-alt-max", "30"},
		{"-alt-max", "91"},
		{"-alt-min", "-91"},
		{"-size-min", "20", "-size-max", "10"},
		{"-size-min", "-1"},
		{"-size-max", "-1", "-size-min", "-2"},
		{"-filter-k", "1.01"},
		{"-filter-k", "-0.01"},
		{"-bortle", "10"},
		{"-bortle", "-1"},
		{"-sqm", "14.9"},
		{"-sqm", "23.1"},
		{"-extinction", "1.01"},
		{"-extinction", "-0.1"},
		{"-twilight", "civil"},
		{"-date", "2026-13-01"},
		{"-tz", "Mars/Olympus"},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	for _, args := range [][]string{
		{"-plan", "10m"},
		{"-plan", "0"},
		{"-min-run", "0"},
		{"-n", "1"},
		{"-alt-min", "-90", "-alt-max", "90"},
		{"-size-min", "0", "-size-max", "0"},
		{"-filter-k", "0"},
		{"-filter-k", "1"},
		{"-bortle", "9"},
		{"-sqm", "15"},
		{"-sqm", "23"},
		{"-extinction", "1"},
		{"-extinction", "0"},
		{"-twilight", " Nautical "},
	} {
		if _, err := mutParse(t, mutBase(args...)...); err != nil {
			t.Errorf("%v rejected: %v", args, err)
		}
	}
}

func TestMutFlagsState(t *testing.T) {
	cfg, err := mutParse(t, mutBase("-twilight", "NAUTICAL", "-bortle", "0", "-date", "2026-10-12")...)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SunAlt() != -12 || !cfg.SkySet || cfg.ExtinctionSet || !cfg.DateSet {
		t.Errorf("state %v %v %v %v", cfg.SunAlt(), cfg.SkySet, cfg.ExtinctionSet, cfg.DateSet)
	}
	zg, _ := time.LoadLocation("Europe/Zagreb")
	if !cfg.Day.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, zg)) {
		t.Errorf("Day %v", cfg.Day)
	}
	cfg, _ = mutParse(t, mutBase("-sqm", "21", "-extinction", "0.2")...)
	if !cfg.SkySet || !cfg.ExtinctionSet || cfg.SunAlt() != -18 || cfg.ZenithMag() != 21 {
		t.Errorf("sqm/extinction set: %v %v %v", cfg.SkySet, cfg.ExtinctionSet, cfg.ZenithMag())
	}
	cfg, _ = mutParse(t, mutBase()...)
	if cfg.SkySet || cfg.ZenithMag() != 22 {
		t.Errorf("unset sky: %v %v", cfg.SkySet, cfg.ZenithMag())
	}
	if (&Config{Bortle: 4}).ZenithMag() != 20.85 || (&Config{Bortle: 4, SQM: 19}).ZenithMag() != 19 {
		t.Error("ZenithMag precedence")
	}
	if (&Config{}).SunAlt() != -18 {
		t.Error("empty twilight not astronomical")
	}
	cfg, err = mutParse(t, "-version")
	if err != nil || !cfg.Version || cfg.Loc != nil {
		t.Errorf("-version: %+v %v", cfg, err)
	}
}

func TestMutRegion(t *testing.T) {
	for _, c := range []struct {
		ra   string
		want float64
	}{{"24", 0}, {"12.5", 187.5}, {"20 30 00", 307.5}, {"0", 0}} {
		cfg, err := mutParse(t, mutBase("-ra", c.ra)...)
		if err != nil || !cfg.RASet || cfg.DecSet || math.Abs(cfg.RA-c.want) > 1e-9 {
			t.Errorf("-ra %s: %v %v %v", c.ra, cfg.RA, cfg.RASet, err)
		}
	}
	cfg, err := mutParse(t, mutBase("-dec", "-12 30 00", "-tol", "180")...)
	if err != nil || cfg.Dec != -12.5 || !cfg.DecSet || cfg.RASet || cfg.Tol != 180 {
		t.Errorf("-dec: %v %v", cfg.Dec, err)
	}
	if _, err := mutParse(t, mutBase("-tol", "0")...); err != nil {
		t.Errorf("-tol 0 without a region: %v", err)
	}
	for _, args := range [][]string{{"-ra", "24.01"}, {"-ra", "-1"}, {"-ra", "x"}, {"-dec", "90.5"}, {"-dec", "-91"}, {"-dec", ""}, {"-ra", "1", "-tol", "0"}, {"-dec", "1", "-tol", "180.1"}, {"-dec", "1", "-tol", "-1"}} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := mutParse(t, mutBase("-dec", "-90")...); err != nil {
		t.Errorf("-dec -90: %v", err)
	}
}

func TestMutNear(t *testing.T) {
	c := Config{RASet: true, RA: 0, Tol: 10}
	for ra, want := range map[float64]bool{355: true, 350: true, 349.9: false, 10: true, 10.1: false, 180: false, 5: true} {
		if got := c.Near(ra, 80); got != want {
			t.Errorf("Near(ra %v) = %v", ra, got)
		}
	}
	c = Config{RASet: true, RA: 350, Tol: 15}
	if !c.Near(4, 0) || c.Near(6, 0) {
		t.Error("RA wrap from 350")
	}
	c = Config{DecSet: true, Dec: -20, Tol: 5}
	for dec, want := range map[float64]bool{-25: true, -15: true, -14.9: false, -25.1: false} {
		if got := c.Near(123, dec); got != want {
			t.Errorf("Near(dec %v) = %v", dec, got)
		}
	}
	if !(&Config{}).Near(1, 2) {
		t.Error("no region limits")
	}
	c = Config{RASet: true, DecSet: true, RA: 100, Dec: 10, Tol: 5}
	if c.Near(100, 20) || c.Near(110, 10) || !c.Near(103, 13) {
		t.Error("both axes")
	}
}

func TestMutSkipFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "skip.txt")
	if err := os.WriteFile(p, []byte("\uFEFFNGC 7789\n# a comment\n  m 31  # Andromeda\n\nSh2-155\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := mutParse(t, mutBase("-skip", p)...)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ngc7789", "m31", "sh2-155"}
	if len(cfg.Skip) != len(want) {
		t.Errorf("Skip = %v", cfg.Skip)
	}
	for _, k := range want {
		if !cfg.Skip[k] {
			t.Errorf("Skip lacks %q: %v", k, cfg.Skip)
		}
	}
	h := filepath.Join(dir, "h.txt")
	_ = os.WriteFile(h, []byte("0 10\n180 30\n"), 0o600)
	cfg, err = mutParse(t, mutBase("-horizon", h)...)
	if err != nil || len(cfg.Horizon) != 2 {
		t.Errorf("horizon %v %v", cfg.Horizon, err)
	}
	for _, args := range [][]string{{"-skip", filepath.Join(dir, "none")}, {"-horizon", filepath.Join(dir, "none")}} {
		if _, err := mutParse(t, mutBase(args...)...); err == nil || !strings.Contains(err.Error(), "none") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
