// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package main

import "testing"

// TestPlaceCache: the terminal view geocodes once, but not on a night that
// never asked (an early exit) or was cancelled without a name, and not again
// while a lookup is in flight.
func TestPlaceCache(t *testing.T) {
	var p placeCache

	// No night at the site: compute exits before any lookup.
	if !p.claim() {
		t.Fatal("first night does not look up")
	}
	if got := p.settle(true, "", false); got != "" {
		t.Errorf("early exit place %q", got)
	}

	// ← and → while a lookup is in flight: no second request.
	if !p.claim() {
		t.Fatal("night after an early exit does not look up")
	}
	if p.claim() {
		t.Error("lookup repeated while one is in flight")
	}

	// The claimant, cancelled after its lookup named the place, keeps it,
	// and the night computed meanwhile shows it.
	if got := p.settle(true, "Zagreb", false); got != "Zagreb" {
		t.Errorf("cancelled claimant place %q", got)
	}
	if got := p.settle(false, "", true); got != "Zagreb" {
		t.Errorf("concurrent night place %q", got)
	}
	if p.claim() {
		t.Error("lookup repeated once named")
	}

	// Asked without a name (at sea, offline): not asked again.
	var q placeCache
	q.claim()
	q.settle(true, "", true)
	if q.claim() {
		t.Error("lookup repeated after a nameless answer")
	}

	// Cancelled without a name: the next night asks.
	var r placeCache
	r.claim()
	r.settle(true, "", false)
	if !r.claim() {
		t.Error("cancelled nameless lookup not retried")
	}
}
