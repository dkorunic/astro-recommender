// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import (
	"cmp"
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/geocode"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/tui"
	"github.com/dkorunic/astro-recommender/internal/view"
	"golang.org/x/term"
)

// altitudes feeds the browser page's altitude curves; the CLI has none.
func altitudes(*config.Config, *scoring.Sky, string, []plan.Slot, []scoring.Result) {}

// snapshotEnv names a file: the interactive view then renders one frame
// there instead, without a terminal, at $COLUMNS x $LINES (scripts/demo.sh).
const snapshotEnv = "ASTRO_RECOMMENDER_SNAPSHOT"

// useTUI reports whether to show the interactive view rather than print the
// report: on a terminal, both ways (stdin for the keys), unless -no-tui,
// -json or -csv, or TERM=dumb asks for text.
func useTUI(cfg *config.Config) bool {
	if os.Getenv(snapshotEnv) != "" {
		return true
	}

	return !cfg.NoTUI && !cfg.JSON && !cfg.CSV && os.Getenv("TERM") != "dumb" &&
		term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// runTUI shows cfg's night in the interactive view, which computes the
// nights before and after it on request.
func runTUI(ctx context.Context, cfg config.Config, targets []catalog.Target) error {
	setup := []string{cfg.ListName}
	if cfg.TargetsFile != "" {
		setup[0] = cfg.TargetsFile
	}
	if label := cmp.Or(cfg.ScopeLabel(), cfg.CameraLabel()); label != "" {
		setup = append(setup, label)
	}
	if cfg.Filter != "" {
		setup = append(setup, "filters "+strings.ReplaceAll(cfg.Filter, ",", ", "))
	}
	opt := tui.Options{Day: cfg.Day, Loc: cfg.Loc, Setup: strings.Join(setup, " · "), DateSet: cfg.DateSet, Framing: cfg.Framing}
	if opt.Snapshot = os.Getenv(snapshotEnv); opt.Snapshot != "" {
		opt.Width, opt.Height = envInt("COLUMNS", 180), envInt("LINES", 50)
	}

	var pc placeCache
	geo := func() string {
		if cfg.NoGeocode {
			return ""
		}
		place, err := geocode.Reverse(ctx, cfg.Lat, cfg.Lon)
		if err != nil {
			warn(ctx, "warning: no reverse geocoding:", err)
		}

		return place
	}

	return tui.Run(ctx, opt, func(nctx context.Context, day time.Time, dateSet bool) tui.Night {
		c := cfg
		c.Day, c.DateSet, c.NoGeocode = day, dateSet, true
		n, err := compute(nctx, c, targets)
		out := tui.Night{Day: n.day, Msg: n.msg, Err: err}
		if err == nil && n.msg == "" {
			out.Doc = view.Build(&n.cfg, &n.sky, pc.get(nctx, geo), n.results, n.slots)
		}

		return out
	})
}

// placeCache geocodes the terminal view's site once: it never moves, and
// Nominatim allows one request a second, which holding ← or → would exceed.
// The lookup runs on the program's context, not a night's, since ← and →
// cancel the night computing, and every night waits for its one answer
// (named or not: at sea, offline or a 429 is not asked again). The first
// night past its early exits starts it, once the view holds stderr for its
// warning.
type placeCache struct {
	done  chan struct{}
	place string
	once  sync.Once
}

// get starts lookup on its first call and returns its place once known, or
// "" when ctx (the night's) ends first.
func (p *placeCache) get(ctx context.Context, lookup func() string) string {
	p.once.Do(func() {
		p.done = make(chan struct{})
		go func() {
			defer close(p.done)
			p.place = lookup()
		}()
	})
	select {
	case <-p.done:
		return p.place
	case <-ctx.Done():
		return ""
	}
}

// envInt is the positive integer in environment variable name, else def.
func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}

	return def
}
