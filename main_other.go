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

	return tui.Run(ctx, opt, func(ctx context.Context, day time.Time, dateSet bool) tui.Night {
		c := cfg
		c.Day, c.DateSet = day, dateSet
		lookup := pc.claim()
		c.NoGeocode = c.NoGeocode || !lookup
		n, err := compute(ctx, c, targets)
		// Only a compute past its early exits, uncancelled, has asked.
		n.place = pc.settle(lookup, n.place, err == nil && n.msg == "" && ctx.Err() == nil)
		out := tui.Night{Day: n.day, Msg: n.msg, Err: err}
		if err == nil && n.msg == "" {
			out.Doc = view.Build(&n.cfg, &n.sky, n.place, n.results, n.slots)
		}

		return out
	})
}

// placeCache geocodes the terminal view's site once: it never moves, and
// Nominatim allows one request a second, which holding ← or → would exceed.
// Once asked, not once named: a site at sea, offline or a 429 would
// otherwise ask again every night. A lookup in flight is not repeated by the
// computes started meanwhile (each ← or → cancels the one before), and one
// that never asked (an early exit: no night, the part passed) or was
// cancelled without a name leaves the next night to ask.
type placeCache struct {
	place    string
	mu       sync.Mutex
	inFlight bool
	done     bool
}

// claim reports whether this compute is to look the place up.
func (p *placeCache) claim() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done || p.inFlight {
		return false
	}
	p.inFlight = true

	return true
}

// settle records a claimed lookup's place (asked: the lookup ran to its
// end) and returns the place to show: its own, else the one known by now.
func (p *placeCache) settle(lookup bool, place string, asked bool) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if lookup {
		p.inFlight = false
		if place != "" || asked {
			p.place, p.done = place, true
		}
	}

	return p.place
}

// envInt is the positive integer in environment variable name, else def.
func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}

	return def
}
