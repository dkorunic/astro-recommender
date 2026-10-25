// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package scoring

import (
	"math"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
)

// TestRotationTrail works the Origin's numbers at latitude 45° by hand for
// a 60' object, its edge 30' out (inside the frame's 45.5' corner) at
// 1.23"/px: k = ω cos φ × 10 s × 1463 px = 0.755 px. On the meridian the
// rate's factor is sin(δ − φ)/cos² h: at 60° (δ 75°, north of the zenith)
// 0.5/0.25, a 1.51 px (1.86") trail under the 2.95" star (2.5" seeing at
// X^0.6, 0.77" diffraction, 0.84" of pixel); at 80° (δ 55°) 5.76, 4.35 px
// (5.35"), past it; south of the zenith (δ 15°) the same 60° as north.
func TestRotationTrail(t *testing.T) {
	cfg := &config.Config{Lat: 45, Framing: true, AltAz: true, Sub: 10, Scale: 1.23, FOVLong: 79, FOVShort: 45, Aperture: 152}
	tg := catalog.Target{Name: "big", Size: 60}
	s := &Sky{}
	at := func(dec float64) bool {
		alt := 90 - math.Abs(cfg.Lat-dec)
		r := newRotation(cfg, s, tg, dec)

		return r.trailed(0, alt, math.Sin(alt*deg), 1)
	}
	if r := newRotation(cfg, s, tg, 75); math.Abs(r.k-0.7546) > 1e-3 {
		t.Errorf("k = %v, want 0.7546", r.k)
	}
	if at(75) || at(15) {
		t.Error("trailed at 60° on the meridian")
	}
	if !at(55) {
		t.Error("not trailed at 80° on the meridian")
	}
	// A 10' object's edge, 6x closer in, keeps 80°; so do an equatorial
	// mount and a comet, which the check leaves alone.
	small := catalog.Target{Name: "small", Size: 10}
	if r := newRotation(cfg, s, small, 55); r.trailed(0, 80, math.Sin(80*deg), 1) {
		t.Error("a 10' object trailed at 80°")
	}
	eq := *cfg
	eq.AltAz = false
	comet := catalog.Target{Name: "comet", Track: [][2]float64{{0, 55}}}
	if newRotation(&eq, s, tg, 55).k != 0 || newRotation(cfg, s, comet, 55).k != 0 {
		t.Error("checked on an equatorial mount or for a comet")
	}
	// Without a size the edge is the frame's corner, 45.5'.
	if r := newRotation(cfg, s, catalog.Target{Name: "unknown"}, 55); math.Abs(r.k/0.7546-45.46/30) > 1e-3 {
		t.Errorf("k without a size %v, want the corner's", r.k)
	}
}
