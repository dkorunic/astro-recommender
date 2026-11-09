// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// A cancelled night cannot warn (main's warn stays quiet for it), so a lookup
// already failed keeps its error for the first night that can, whichever
// case the select takes.
func TestPlaceCacheCancelledKeepsError(t *testing.T) {
	var p placeCache
	errDown := errors.New("down")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	lookup := func() (string, error) { return "", errDown }
	p.get(cancelled, lookup) //nolint:errcheck // only starts the lookup
	<-p.done
	for range 64 {
		if _, err := p.get(cancelled, lookup); err != nil {
			t.Fatalf("cancelled night got the error %v, which it cannot warn of", err)
		}
	}
	if _, err := p.get(context.Background(), lookup); !errors.Is(err, errDown) {
		t.Errorf("live night got %v; want down", err)
	}
}

// TestPlaceCache: the terminal view geocodes once, and a night cancelled
// while the lookup runs (← or → pressed) neither stops it nor keeps the
// nights after it from its answer.
func TestPlaceCache(t *testing.T) {
	var p placeCache
	var asked atomic.Int32
	release := make(chan struct{})
	errDown := errors.New("down")
	lookup := func() (string, error) {
		asked.Add(1)
		<-release

		return "Zagreb", errDown
	}

	// The first night, cancelled while its lookup runs: no place, the
	// lookup carries on.
	ctx, cancel := context.WithCancel(context.Background())
	type answer struct {
		err   error
		place string
	}
	got := make(chan answer)
	ask := func(ctx context.Context) {
		place, err := p.get(ctx, lookup)
		got <- answer{err, place}
	}
	go ask(ctx)
	for asked.Load() == 0 {
	}
	cancel()
	if a := <-got; a.place != "" || a.err != nil {
		t.Errorf("cancelled night got %q, %v", a.place, a.err)
	}

	// The night after waits for the same lookup's answer, and warns of its
	// failure; the nights after that have nothing to add.
	go ask(context.Background())
	close(release)
	if a := <-got; a.place != "Zagreb" || !errors.Is(a.err, errDown) {
		t.Errorf("next night got %q, %v", a.place, a.err)
	}
	place, err := p.get(context.Background(), lookup)
	if place != "Zagreb" || err != nil {
		t.Errorf("third night got %q, %v; want the place and no second warning", place, err)
	}
	if asked.Load() != 1 {
		t.Errorf("looked up %d times, want once", asked.Load())
	}
}
