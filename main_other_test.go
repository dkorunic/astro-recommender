// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestPlaceCache: the terminal view geocodes once, and a night cancelled
// while the lookup runs (← or → pressed) neither stops it nor keeps the
// nights after it from its answer.
func TestPlaceCache(t *testing.T) {
	var p placeCache
	var asked atomic.Int32
	release := make(chan struct{})
	lookup := func() string {
		asked.Add(1)
		<-release

		return "Zagreb"
	}

	// The first night, cancelled while its lookup runs: no place, the
	// lookup carries on.
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan string)
	go func() { got <- p.get(ctx, lookup) }()
	for asked.Load() == 0 {
	}
	cancel()
	if place := <-got; place != "" {
		t.Errorf("cancelled night place %q", place)
	}

	// The night after waits for the same lookup's answer.
	go func() { got <- p.get(context.Background(), lookup) }()
	close(release)
	if place := <-got; place != "Zagreb" {
		t.Errorf("next night place %q", place)
	}
	if p.get(context.Background(), lookup); asked.Load() != 1 {
		t.Errorf("looked up %d times, want once", asked.Load())
	}
}
