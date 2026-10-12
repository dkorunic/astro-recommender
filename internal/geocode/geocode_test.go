// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package geocode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/fetch"
)

func TestReverse(t *testing.T) {
	var gotQuery string
	reply, status := "", http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
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

	// The name is sanitized: an escape sequence must not reach the terminal.
	reply = `{"display_name":"Gornji grad\u001b[31m, Zagreb‮"}`
	place, err := Reverse(ctx, 45.81234, 15.98766)
	if err != nil || place != "Gornji grad[31m, Zagreb" {
		t.Errorf("Reverse = %q, %v", place, err)
	}
	// Rounded to ~1 km: an exact position is not sent.
	if want := "format=jsonv2&lat=45.81&lon=15.99&zoom=14"; gotQuery != want {
		t.Errorf("query %q, want %q", gotQuery, want)
	}

	for name, c := range map[string]struct {
		reply  string
		want   string
		status int
	}{
		"error in 200": {`{"error":"Unable to geocode"}`, "Unable to geocode", http.StatusOK},
		"empty":        {`{}`, "no result", http.StatusOK},
		"sanitized":    {`{"error":"bad\u0007"}`, "nominatim: bad", http.StatusOK},
		"not json":     {`<html>`, "nominatim", http.StatusBadGateway},
	} {
		reply, status = c.reply, c.status
		if _, err := Reverse(ctx, 0, 0); err == nil || !strings.Contains(err.Error(), c.want) || strings.ContainsRune(err.Error(), '\a') {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	reply, status = `{}`, http.StatusBadGateway
	if _, err := Reverse(ctx, 0, 0); !errors.Is(err, fetch.ErrStatus) {
		t.Errorf("502: err = %v, want fetch.ErrStatus", err)
	}
}
