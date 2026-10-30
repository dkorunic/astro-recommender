// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/sqm"
)

// countingTransport answers the site-only services from canned bodies,
// counting the requests to each, and every other request with a 404.
type countingTransport struct {
	asked map[string]int
	mu    sync.Mutex
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var service, body string
	switch u := req.URL; {
	case u.Host == "www.7timer.info":
		service = "7timer"
		body = `{"init":"2026101112","dataseries":[{"timepoint":6,"seeing":3,"transparency":2},{"timepoint":9,"seeing":3,"transparency":2},{"timepoint":12,"seeing":4,"transparency":3},{"timepoint":15,"seeing":4,"transparency":3}]}`
	case u.Host == "www.darkskysites.com":
		service = "sqm"
		body = `{"sqm":20.5,"attribution":"test","dataset":{"id":"2026-09"}}`
	case u.Host == "api.open-meteo.com" && strings.Contains(u.RawQuery, "elevation=nan"):
		service = "grid"
		body = `{"elevation":312}`
	default:
		service = "other"
	}
	c.mu.Lock()
	c.asked[service]++
	c.mu.Unlock()
	status := http.StatusOK
	if service == "other" {
		status, body = http.StatusNotFound, `{}`
	}

	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
}

// The terminal view's nights share a session: 7Timer, the DarkSkySites map
// and the grid cell's elevation, which depend on the site alone, are asked
// once over two nights, and both nights still get their answers; a single
// run's zero session asks them every night.
func TestSessionAsksOnce(t *testing.T) {
	t.Setenv(sqm.KeyEnv, "test-key")
	ct := &countingTransport{asked: map[string]int{}}
	old := http.DefaultTransport
	http.DefaultTransport = ct
	t.Cleanup(func() { http.DefaultTransport = old })

	cfg := config.Config{
		Lat: 45.8, Lon: 16, AltMin: 30, AltMax: 80, SizeMax: 300, Top: 5, FilterNM: 7,
		Loc: time.UTC, DateSet: true, NoGeocode: true, NoComets: true, NoSolar: true,
	}
	targets := []catalog.Target{{Name: "A", Type: "Galaxy", RADeg: 300, DecDeg: 40, Size: 10, SurfBr: 21}}
	nights := func(ses session) {
		for _, day := range []int{11, 12} {
			c := cfg
			c.Day = time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC)
			var n night
			var err error
			capture(t, func() { n, err = compute(context.Background(), c, targets, ses) })
			if err != nil || n.msg != "" {
				t.Fatalf("night %d: %v %q", day, err, n.msg)
			}
			if n.cfg.SQM != 20.5 || !strings.Contains(n.cfg.SQMSource, "2026-09") {
				t.Errorf("night %d: SQM %v %q, want the lookup's 20.5", day, n.cfg.SQM, n.cfg.SQMSource)
			}
			if len(n.sky.Astro) == 0 || n.sky.AODElevation != 312 {
				t.Errorf("night %d: %d 7Timer points, grid elevation %v; want the answers", day, len(n.sky.Astro), n.sky.AODElevation)
			}
		}
	}

	nights(newSession())
	for _, service := range []string{"7timer", "sqm", "grid"} {
		if ct.asked[service] != 1 {
			t.Errorf("with a session %s asked %d times over two nights, want 1", service, ct.asked[service])
		}
	}
	clear(ct.asked)
	nights(session{})
	for _, service := range []string{"7timer", "sqm", "grid"} {
		if ct.asked[service] != 2 {
			t.Errorf("without a session %s asked %d times over two nights, want 2", service, ct.asked[service])
		}
	}
}
