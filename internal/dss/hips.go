// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package dss

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // the tiles are JPEG
	"io"
	"maps"
	"math"
	"net/http"
	"slices"
	"sync"
)

// HiPS is ESA's mirror of the CDS DSS2 colour HiPS (CDS/P/DSS2/color, the
// survey hips2fits draws on): its tiles are what Cutout resamples when CDS
// does not answer. Equatorial, 512-pixel JPEG tiles, orders 0-9.
const HiPS = "https://skies.esac.esa.int/DSSColor/"

const (
	tileBits = 9 // a tile is 2^9 = 512 pixels a side
	tileSide = 1 << tileBits
	maxOrder = 9       // the survey's deepest tile order
	maxTiles = 64      // more tiles than any Cutout of a terminal or card needs
	maxTile  = 4 << 20 // bytes a tile may take
)

var (
	errCutout = errors.New("no cutout")
	errTile   = errors.New("bad tile")
)

// healpixDeg is a HEALPix pixel's side at nside 1, sqrt(4π/12) rad, in
// degrees: at nside N it is healpixDeg/N.
var healpixDeg = math.Sqrt(math.Pi/3) * 180 / math.Pi

// Pix is the nested HEALPix index at depth (nside 2^depth) of the J2000
// position ra, dec in degrees.
func Pix(depth uint, ra, dec float64) uint64 {
	r, d := ra*math.Pi/180, dec*math.Pi/180
	face, ix, iy := nest(depth, math.Sin(d), math.Cos(d), r)

	return face<<(2*depth) | spread(ix) | spread(iy)<<1
}

// nest is the base face and the x, y within it at depth of the direction
// with z = sin(dec), s = cos(dec) and longitude phi in radians: Górski et
// al. 2005's ang2pix_nest, with s for the polar caps' 1 - |z| = s²/(1 + |z|),
// which keeps its precision at the pole.
func nest(depth uint, z, s, phi float64) (uint64, uint64, uint64) {
	ns := int64(1) << depth
	nsf := float64(ns)
	za := math.Abs(z)
	tt := math.Mod(phi, 2*math.Pi)
	if tt < 0 {
		tt += 2 * math.Pi
	}
	tt *= 2 / math.Pi
	if tt >= 4 { // phi a rounding under 2π
		tt -= 4
	}
	var f, x, y int64
	if za <= 2.0/3 {
		t1, t2 := nsf*(0.5+tt), nsf*z*0.75
		jp, jm := int64(t1-t2), int64(t1+t2)
		ifp, ifm := jp>>depth, jm>>depth
		switch {
		case ifp == ifm:
			f = ifp | 4
		case ifp < ifm:
			f = ifp
		default:
			f = ifm + 8
		}
		x, y = jm&(ns-1), ns-(jp&(ns-1))-1
	} else {
		ntt := min(3, int64(tt))
		tp := tt - float64(ntt)
		tmp := nsf * s * math.Sqrt(3/(1+za))
		jp, jm := min(int64(tp*tmp), ns-1), min(int64((1-tp)*tmp), ns-1)
		if z >= 0 {
			f, x, y = ntt, ns-jm-1, ns-jp-1
		} else {
			f, x, y = ntt+8, jp, jm
		}
	}

	return uint64(f), uint64(x), uint64(y) //nolint:gosec // all within 0..12·nside
}

// spread puts v's low 32 bits in the even bits of the result.
func spread(v uint64) uint64 {
	v &= 0xFFFFFFFF
	v = (v | v<<16) & 0x0000FFFF0000FFFF
	v = (v | v<<8) & 0x00FF00FF00FF00FF
	v = (v | v<<4) & 0x0F0F0F0F0F0F0F0F
	v = (v | v<<2) & 0x3333333333333333

	return (v | v<<1) & 0x5555555555555555
}

// Order is the tile order whose pixels are nearest scale degrees, within the
// survey's 0-maxOrder.
func Order(scale float64) uint {
	p := math.Round(math.Log2(healpixDeg/scale)) - tileBits

	return uint(min(maxOrder, max(0, p)))
}

// Cutout is a w x h picture, scale degrees a pixel, of the gnomonic (TAN)
// field centred on the J2000 ra, dec in degrees, north up and east left:
// hips2fits' picture, resampled here (nearest pixel) from the tiles of the
// HiPS at base, fetched through client at once, at Order(scale), or coarser
// where that would take more than maxTiles. A tile the HiPS lacks (404)
// stays black, but none at all is an error (a moved survey, not a gap), as
// is any other failure.
func Cutout(ctx context.Context, client *http.Client, base, userAgent string, ra, dec, scale float64, w, h int) (*image.RGBA, error) {
	return cutout(ctx, client, base, userAgent, ra, dec, scale, w, h, maxTiles)
}

// cutout is Cutout with at most limit tiles.
func cutout(ctx context.Context, client *http.Client, base, userAgent string, ra, dec, scale float64, w, h, limit int) (*image.RGBA, error) {
	if w <= 0 || h <= 0 || !(scale > 0) {
		return nil, fmt.Errorf("%w: %dx%d at %v°/px", errCutout, w, h, scale)
	}
	order := Order(scale)
	tiles, where, off := layout(order, ra, dec, scale, w, h)
	for len(tiles) > limit && order > 0 {
		order--
		tiles, where, off = layout(order, ra, dec, scale, w, h)
	}
	if len(tiles) > limit {
		return nil, fmt.Errorf("%w: %d tiles, more than %d", errCutout, len(tiles), limit)
	}
	if err := fetchTiles(ctx, client, base, userAgent, order, tiles); err != nil {
		return nil, err
	}
	none := true
	for _, img := range tiles {
		none = none && img == nil
	}
	if none {
		return nil, fmt.Errorf("%w: no tile at %s", errCutout, base)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for p, t := range where {
		img := tiles[t]
		if img == nil {
			continue
		}
		b := img.Bounds()
		o := int(off[p])
		out.Set(p%w, p/w, img.At(b.Min.X+o%tileSide, b.Min.Y+o/tileSide))
	}

	return out, nil
}

// layout is the tiles at order Cutout's field needs, and each pixel's tile
// and offset within it (tileOffset).
func layout(order uint, ra, dec, scale float64, w, h int) (map[uint64]image.Image, []uint64, []int32) {
	depth := order + tileBits
	// The centre and its east and north unit vectors: pixel (i, j) is the
	// direction centre + ξ east + η north, ξ, η the tangent plane's.
	r, d := ra*math.Pi/180, dec*math.Pi/180
	sr, cr, sd, cd := math.Sin(r), math.Cos(r), math.Sin(d), math.Cos(d)
	c := [3]float64{cd * cr, cd * sr, sd}
	e := [3]float64{-sr, cr, 0}
	n := [3]float64{-sd * cr, -sd * sr, cd}
	step := scale * math.Pi / 180
	tiles := map[uint64]image.Image{}
	where := make([]uint64, w*h)
	off := make([]int32, w*h)
	for j := range h {
		eta := (float64(h)/2 - float64(j) - 0.5) * step
		for i := range w {
			xi := (float64(w)/2 - float64(i) - 0.5) * step // east left
			x, y, z := c[0]+xi*e[0]+eta*n[0], c[1]+xi*e[1]+eta*n[1], c[2]+xi*e[2]+eta*n[2]
			l := math.Sqrt(x*x + y*y + z*z)
			face, ix, iy := nest(depth, z/l, math.Hypot(x, y)/l, math.Atan2(y, x))
			t := face<<(2*order) | spread(ix>>tileBits) | spread(iy>>tileBits)<<1
			tiles[t] = nil
			where[j*w+i] = t
			off[j*w+i] = tileOffset(ix&(tileSide-1), iy&(tileSide-1))
		}
	}

	return tiles, where, off
}

// tileOffset is the row-major offset in a tile's image of the pixel at x, y
// within it in the HEALPix face's nested coordinates: HiPS tiles run y
// along the columns and x down the rows (checked against SkyView's DSS2 Red
// of four fields, the pole's among them: r 0.45-0.96 here, below 0.35 for
// every other of the eight orientations).
func tileOffset(x, y uint64) int32 {
	return int32(x*tileSide + y) //nolint:gosec // x, y < tileSide
}

// fetchTiles fills tiles' images at order from base, at once; a tile the
// HiPS lacks (404) stays nil. The first failure cancels the other fetches,
// whose tiles the picture can no longer use, and is the one returned.
func fetchTiles(ctx context.Context, client *http.Client, base, userAgent string, order uint, tiles map[uint64]image.Image) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	// The keys first: the goroutines write tiles while this loop would
	// still range over it.
	for _, t := range slices.Collect(maps.Keys(tiles)) {
		wg.Go(func() {
			img, err := fetchTile(ctx, client, base, userAgent, order, t)
			mu.Lock()
			defer mu.Unlock()
			tiles[t] = img
			if err != nil && first == nil {
				first = err
				cancel()
			}
		})
	}
	wg.Wait()

	return first
}

// fetchTile is tile t at order from base, nil when the HiPS lacks it.
func fetchTile(ctx context.Context, client *http.Client, base, userAgent string, order uint, t uint64) (image.Image, error) {
	u := fmt.Sprintf("%sNorder%d/Dir%d/Npix%d.jpg", base, order, t/10000*10000, t)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil //nolint:nilnil // no tile: that part of the picture stays black
	default:
		return nil, fmt.Errorf("%w %s: %s", errTile, u, resp.Status)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxTile))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	if b := img.Bounds(); b.Dx() != tileSide || b.Dy() != tileSide {
		return nil, fmt.Errorf("%w %s: %dx%d, want %d", errTile, u, b.Dx(), b.Dy(), tileSide)
	}

	return img, nil
}
