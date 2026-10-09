// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package sqm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
)

func TestLookup(t *testing.T) {
	var gotKey, gotQuery string
	reply, status := "", http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotQuery = r.Header.Get("x-api-key"), r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })
	fetch.RetryDelay = 0 // the 502 case is retried once
	t.Cleanup(func() { fetch.RetryDelay = time.Second })
	ctx := context.Background()

	reply = `{"sqm":17.81,"bortleClass":9,"dataset":{"id":"08_2026"},"attribution":"darkskysites.com"}`
	mag, src, err := Lookup(ctx, "sqm_test", 45.81234, 15.98766)
	if err != nil || mag != 17.81 || src != "darkskysites.com 08_2026" {
		t.Errorf("Lookup = %v, %q, %v", mag, src, err)
	}
	if gotKey != "sqm_test" || gotQuery != "lat=45.81&lng=15.99" {
		t.Errorf("sent key %q, query %q", gotKey, gotQuery)
	}
	// An attribution that sanitizes to nothing still yields a source, since
	// main reads an empty one as no result.
	reply = `{"sqm":21.5,"attribution":"\u0007"}`
	if mag, src, err := Lookup(ctx, "k", 0, 0); err != nil || mag != 21.5 || src != "darkskysites.com" {
		t.Errorf("control-character attribution: Lookup = %v, %q, %v", mag, src, err)
	}

	for name, c := range map[string]struct {
		reply  string
		want   string
		status int
	}{
		"implausible":  {`{"sqm":0}`, "implausible", http.StatusOK},
		"bad key":      {`{"error":"Invalid API key"}`, "Invalid API key", http.StatusUnauthorized},
		"error in 200": {`{"error":"quota exceeded","sqm":0}`, "quota exceeded", http.StatusOK},
		"not json":     {`<html>`, "darkskysites", http.StatusBadGateway},
	} {
		reply, status = c.reply, c.status
		if _, _, err := Lookup(ctx, "k", 0, 0); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}
