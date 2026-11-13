// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package dss

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Pix against CDS's cdshealpix (lonlat_to_healpix): equator, both polar
// caps, the poles' neighbourhood and RA just under 360°.
func TestPix(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		ra, dec float64
		depth   uint
		want    uint64
	}{
		{0, 0, 0, 4},
		{83.8221, -5.3911, 18, 359697827929},
		{10.6847, 41.2687, 18, 45450571438},
		{279.2347, 38.7837, 12, 61056746},
		{101.2875, -16.7161, 17, 87828951597},
		{0.5, 89.9, 9, 262142},
		{200, -89.5, 15, 10737554001},
		{359.99, -30, 18, 277383304870},
		{45, 41.8, 10, 262143},
		{315, -41.9, 10, 11796479},
		{123.4, 66.6, 18, 130728725492},
	} {
		if got := Pix(c.depth, c.ra, c.dec); got != c.want {
			t.Errorf("Pix(%d, %v, %v) = %d, want %d", c.depth, c.ra, c.dec, got, c.want)
		}
	}
}

func TestOrder(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		scale float64
		want  uint
	}{
		{1, 0},           // coarser than order 0's 0.11°
		{0.5 / 240, 6},   // a 0.5° card picture
		{0.1 / 240, 8},   // the smallest field
		{1.0 / 3600, 9},  // finer than the survey
		{5.0 / 240, 2},   // the largest field
		{1e-9, maxOrder}, // clamped
	} {
		if got := Order(c.scale); got != c.want {
			t.Errorf("Order(%v) = %d, want %d", c.scale, got, c.want)
		}
	}
}

// Cutout asks for each tile it needs once, leaves a missing one black and
// fails on any other error.
func TestCutout(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	tile := image.NewGray(image.Rect(0, 0, tileSide, tileSide))
	for i := range tile.Pix {
		tile.Pix[i] = 200
	}
	if err := jpeg.Encode(&b, tile, nil); err != nil {
		t.Fatal(err)
	}
	var (
		asked   atomic.Int32
		missing atomic.Bool // order 0's face 0 missing
		code    atomic.Int32
	)
	code.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/Norder") || missing.Load() && r.URL.Path == "/Norder0/Dir0/Npix0.jpg" {
			http.NotFound(w, r)

			return
		}
		w.WriteHeader(int(code.Load()))
		_, _ = w.Write(b.Bytes())
	}))
	defer srv.Close()

	// Across the RA = 0 line on the equator: four base faces' corners meet
	// nowhere near, so two tiles at most.
	img, err := Cutout(context.Background(), srv.Client(), srv.URL+"/", "test", 0, 0, 0.5/64, 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{64, 32}) {
		t.Errorf("size %v", got)
	}
	if n := asked.Load(); n < 1 || n > 4 {
		t.Errorf("%d tiles asked", n)
	}
	if c := color.GrayModel.Convert(img.At(10, 10)).(color.Gray); c.Y < 190 {
		t.Errorf("pixel %v, want the tile's grey", c)
	}

	// Across RA 0 at +50°, faces 0 (east) and 3 at order 0, face 0 missing:
	// that half black, the other drawn. No tile at all is an error.
	missing.Store(true)
	img, err = Cutout(context.Background(), srv.Client(), srv.URL+"/", "test", 0, 50, 0.5, 64, 8)
	if err != nil {
		t.Fatalf("a missing tile is an error: %v", err)
	}
	var black, grey bool
	for x := range 64 {
		c := img.RGBAAt(x, 4)
		black = black || c == (color.RGBA{})
		grey = grey || c.R > 190
	}
	if !black || !grey {
		t.Errorf("black %v, grey %v: want the missing tile black and the other drawn", black, grey)
	}
	missing.Store(false)
	code.Store(http.StatusNotFound)
	if _, err := Cutout(context.Background(), srv.Client(), srv.URL+"/", "test", 0, 0, 0.5/64, 8, 8); err == nil {
		t.Error("no tile at all is no error")
	}

	code.Store(http.StatusServiceUnavailable)
	if _, err := Cutout(context.Background(), srv.Client(), srv.URL+"/", "test", 0, 0, 0.5/64, 8, 8); err == nil {
		t.Error("a failing mirror is no error")
	}
	if _, err := Cutout(context.Background(), srv.Client(), srv.URL+"/", "test", 0, 0, 0, 8, 8); err == nil {
		t.Error("a zero scale is no error")
	}

	// A field of more tiles than the limit at its own order takes a coarser
	// one rather than failing. Its own server and count: the failing
	// cutouts' cancelled fetches may still reach srv after they return.
	var tiles atomic.Int32
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tiles.Add(1)
		_, _ = w.Write(b.Bytes())
	}))
	defer big.Close()
	const limit = 2
	if _, err := cutout(context.Background(), big.Client(), big.URL+"/", "test", 10, 20, 0.5/400, 400, 400, limit); err != nil {
		t.Errorf("a large field: %v", err)
	}
	if n := tiles.Load(); n > limit {
		t.Errorf("%d tiles asked, more than %d", n, limit)
	}
	if n, _, _ := layout(Order(0.5/400), 10, 20, 0.5/400, 400, 400); len(n) <= limit {
		t.Errorf("the field takes %d tiles at its own order: not a test of the limit", len(n))
	}
}

// One tile failing cancels the others: a tile that would never come does
// not hold the picture up, and the error is the failing tile's.
func TestCutoutCancels(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Norder0/Dir0/Npix0.jpg" {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		<-r.Context().Done() // until the fetch is cancelled
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	// Across RA 0 at +50°: faces 0 (the failing tile) and 3 at order 0.
	_, err := Cutout(ctx, srv.Client(), srv.URL+"/", "test", 0, 50, 0.5, 64, 8)
	if !errors.Is(err, errTile) {
		t.Errorf("error %v, want the failing tile's", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v: the other tile was not cancelled", d)
	}
}
