// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestZenithMag(t *testing.T) {
	if got := (&Config{SQM: 19.3, Bortle: 9}).ZenithMag(); got != 19.3 {
		t.Errorf("-sqm should override -bortle, got %v", got)
	}
}

func TestParseFOV(t *testing.T) {
	for _, ok := range []string{"0.75x1.32", " 0.75x1.32", "0.75x 1.32 "} {
		if long, short, err := parseFOV(ok); err != nil || long != 1.32*60 || short != 0.75*60 {
			t.Errorf("parseFOV(%q) = %v %v %v", ok, long, short, err)
		}
	}
	for _, bad := range []string{"2.1x1.4abc", "2.1x1.4 9", "2.1", "x1", "0x1", "-1x1", "Infx1", "NaNx1"} {
		if _, _, err := parseFOV(bad); err == nil {
			t.Errorf("parseFOV(%q) accepted", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Config{Lat: 45.8, Lon: 16, FilterK: 0.25, Extinction: 0.2, Top: 20, AltMin: 30, AltMax: 80, SizeMin: 10, SizeMax: 300}
	if err := ok.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, mod := range map[string]func(*Config){
		"-n 0":              func(c *Config) { c.Top = 0 },
		"-n -1":             func(c *Config) { c.Top = -1 },
		"alt-min > max":     func(c *Config) { c.AltMin, c.AltMax = 60, 40 },
		"alt-min = max":     func(c *Config) { c.AltMin, c.AltMax = 40, 40 },
		"size-min > max":    func(c *Config) { c.SizeMin, c.SizeMax = 300, 10 },
		"-plan 10m45s":      func(c *Config) { c.Plan = 10*time.Minute + 45*time.Second },
		"size-min < 0":      func(c *Config) { c.SizeMin = -5 },
		"size-max < 0":      func(c *Config) { c.SizeMin, c.SizeMax = -5, -1 },
		"framing, no scale": func(c *Config) { c.Framing, c.FOVLong, c.FOVShort = true, 79.2, 45 },
		"framing, sides swapped": func(c *Config) {
			c.Framing, c.FOVLong, c.FOVShort, c.Scale = true, 45, 79.2, 1.23
		},
	} {
		c := ok
		mod(&c)
		if err := c.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// parse runs Parse on args (after -lat/-lon) with a fresh flag set, restoring
// the globals afterwards.
func parse(t *testing.T, args ...string) (Config, error) {
	t.Helper()
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = append([]string{"test", "-lat", "45.8", "-lon", "16"}, args...)

	return Parse()
}

func TestParseFrame(t *testing.T) {
	type frame struct{ long, short, scale, sizeMin, sizeMax float64 }
	origin := frame{originFOVLong, originFOVShort, originScale, 200 * originScale / 60, originFOVShort}
	// APS-C 23.5x15.6 mm at 400 mm: 3.364°x2.234°.
	side := func(mm float64) float64 { return 2 * math.Atan(mm/800) / (math.Pi / 180) * 60 }
	scale := 206.264806 * 3.76 / 400 // 3.76 µm pixels at 400 mm: 1.94"/px
	for name, c := range map[string]struct {
		args []string
		want string // substring of the error, "" to accept
		cfg  frame  // expected when accepted and Framing
	}{
		"origin":           {[]string{"-origin"}, "", origin},
		"fov alone":        {[]string{"-fov", "7x10"}, "", frame{600, 420, originScale, 200 * originScale / 60, 420}},
		"scale alone":      {[]string{"-scale", "0.2"}, "", frame{originFOVLong, originFOVShort, 0.2, 200 * 0.2 / 60, originFOVShort}},
		"fov and scale":    {[]string{"-fov", "2.5x1.7", "-scale", "3.8"}, "", frame{150, 102, 3.8, 200 * 3.8 / 60, 102}},
		"min-px 0":         {[]string{"-origin", "-min-px", "0"}, "", frame{originFOVLong, originFOVShort, originScale, 0, originFOVShort}},
		"explicit sizes":   {[]string{"-origin", "-size-min", "5", "-size-max", "40"}, "", frame{originFOVLong, originFOVShort, originScale, 5, 40}},
		"size-max > frame": {[]string{"-origin", "-size-max", "300"}, "cannot be framed", frame{}},
		"fov in arcmin":    {[]string{"-fov", "79x45", "-scale", "1.23"}, "not a sensor", frame{}},
		"scale off frame":  {[]string{"-fov", "10x7", "-scale", "0.5"}, "not a sensor", frame{}},
		"min-px negative":  {[]string{"-origin", "-min-px", "-100"}, "-min-px", frame{}},
		"min-px NaN":       {[]string{"-origin", "-min-px", "NaN"}, "-min-px must be a finite", frame{}},
		"min-px Inf":       {[]string{"-min-px", "Inf"}, "-min-px must be a finite", frame{}},
		"scale Inf":        {[]string{"-origin", "-scale", "Inf"}, "-scale must be a finite", frame{}},
		"extinction NaN":   {[]string{"-extinction", "NaN"}, "-extinction must be a finite", frame{}},
		"alt-min Inf":      {[]string{"-alt-min", "-Inf"}, "-alt-min must be a finite", frame{}},
		"size-min NaN":     {[]string{"-size-min", "NaN"}, "-size-min must be a finite", frame{}},
		"sqm NaN":          {[]string{"-sqm", "NaN"}, "-sqm must be a finite", frame{}},
		"comet-mag Inf":    {[]string{"-comet-mag", "Inf"}, "-comet-mag must be a finite", frame{}},
		"two non-finite":   {[]string{"-alt-min", "NaN", "-size-max", "Inf"}, "-alt-min, -size-max must be a finite", frame{}},
		"min-px unused":    {[]string{"-min-px", "-1"}, "-min-px", frame{}},
		"scale NaN":        {[]string{"-fov", "2.5x1.7", "-scale", "NaN"}, "-scale", frame{}},
		"scale negative":   {[]string{"-scale", "-1"}, "-scale", frame{}},
		"scale 0":          {[]string{"-scale", "0"}, "-scale", frame{}},
		"scale tiny":       {[]string{"-scale", "0.001"}, "-scale must be between", frame{}},
		"scale huge":       {[]string{"-scale", "5000"}, "-scale must be between", frame{}},
		"fov junk":         {[]string{"-fov", "2x1abc"}, "WxH", frame{}},
		"fov above 180":    {[]string{"-fov", "200x120"}, "180", frame{}},
		"focal and sensor": {[]string{"-focal", "400", "-sensor", "15.6x23.5", "-scale", "1.94"}, "", frame{side(23.5), side(15.6), 1.94, 200 * 1.94 / 60, side(15.6)}},
		"sensor off scale": {[]string{"-focal", "400", "-sensor", "23.5x15.6", "-scale", "0.1"}, "not a sensor", frame{}},
		"focal alone":      {[]string{"-focal", "400"}, "-focal needs -sensor or -pixel", frame{}},
		"sensor alone":     {[]string{"-sensor", "23.5x15.6"}, "need -focal", frame{}},
		"camera":           {[]string{"-focal", "400", "-sensor", "23.5x15.6", "-pixel", "3.76"}, "", frame{side(23.5), side(15.6), scale, 200 * scale / 60, side(15.6)}},
		"pixel and fov":    {[]string{"-fov", "2.5x1.7", "-focal", "400", "-pixel", "3.76"}, "", frame{150, 102, scale, 200 * scale / 60, 102}},
		"pixel, no field":  {[]string{"-focal", "400", "-pixel", "3.76"}, "", frame{originFOVLong, originFOVShort, scale, 200 * scale / 60, originFOVShort}},
		"pixel alone":      {[]string{"-pixel", "3.76"}, "need -focal", frame{}},
		"pixel and scale":  {[]string{"-focal", "400", "-pixel", "3.76", "-scale", "2"}, "not both", frame{}},
		"pixel 0":          {[]string{"-focal", "400", "-pixel", "0"}, "-pixel must be above 0", frame{}},
		"pixel in mm":      {[]string{"-focal", "400", "-pixel", "0.00376"}, "outside", frame{}},
		"pixel off sensor": {[]string{"-focal", "400", "-sensor", "23.5x15.6", "-pixel", "0.1"}, "not a sensor", frame{}},
		"fov and focal":    {[]string{"-fov", "2x1", "-focal", "400", "-sensor", "23.5x15.6"}, "not both", frame{}},
		"focal 0":          {[]string{"-focal", "0", "-sensor", "23.5x15.6"}, "-focal must be above 0", frame{}},
		"focal NaN":        {[]string{"-focal", "NaN", "-sensor", "23.5x15.6"}, "-focal must be a finite", frame{}},
		"sensor junk":      {[]string{"-focal", "400", "-sensor", "23.5"}, "-sensor must be WxH in mm", frame{}},
		"sensor negative":  {[]string{"-focal", "400", "-sensor", "-1x15"}, "-sensor sides must be above 0", frame{}},
	} {
		cfg, err := parse(t, c.args...)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: rejected: %v", name, err)
		case c.want != "" && err == nil:
			t.Errorf("%s: accepted", name)
		case c.want != "" && !strings.Contains(err.Error(), c.want):
			t.Errorf("%s: error %q does not mention %q", name, err, c.want)
		case c.want == "":
			got := frame{cfg.FOVLong, cfg.FOVShort, cfg.Scale, cfg.SizeMin, cfg.SizeMax}
			if !cfg.Framing || math.Abs(got.long-c.cfg.long) > 1e-9 || math.Abs(got.short-c.cfg.short) > 1e-9 ||
				math.Abs(got.scale-c.cfg.scale) > 1e-9 || math.Abs(got.sizeMin-c.cfg.sizeMin) > 1e-9 || got.sizeMax != c.cfg.sizeMax {
				t.Errorf("%s: frame %+v, want %+v (framing %v)", name, got, c.cfg, cfg.Framing)
			}
		}
	}
	if cfg, err := parse(t); err != nil || cfg.Framing || cfg.SizeMin != 10 || cfg.SizeMax != 300 {
		t.Errorf("no framing flags: %+v, %v", cfg, err)
	}
}
