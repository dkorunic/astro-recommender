// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package sqm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dkorunic/astro-recommender/internal/fetch"
)

func mutServe(t *testing.T, code int, body string) *string {
	t.Helper()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.Header.Get("X-Api-Key") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"no key"}`))

			return
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old, oldDelay := baseURL, fetch.RetryDelay
	baseURL, fetch.RetryDelay = srv.URL, 0
	t.Cleanup(func() { baseURL, fetch.RetryDelay = old, oldDelay })

	return &query
}

func TestMutLookup(t *testing.T) {
	ctx := context.Background()
	q := mutServe(t, http.StatusOK, `{"sqm":21.3,"attribution":"DSS\u001b[2J","dataset":{"id":"2026-09"}}`)
	v, src, err := Lookup(ctx, "secret", 45.8149, -15.9819)
	if err != nil || v != 21.3 || src != "DSS[2J 2026-09" {
		t.Errorf("Lookup = %v %q %v", v, src, err)
	}
	if *q != "lat=45.81&lng=-15.98" {
		t.Errorf("query %q", *q)
	}
	mutServe(t, http.StatusOK, `{"sqm":23}`)
	if v, src, err := Lookup(ctx, "secret", 1, 2); err != nil || v != 23 || src != "darkskysites.com" {
		t.Errorf("Lookup(23) = %v %q %v", v, src, err)
	}
	mutServe(t, http.StatusOK, `{"sqm":15}`)
	if v, _, err := Lookup(ctx, "secret", 1, 2); err != nil || v != 15 {
		t.Errorf("Lookup(15) = %v %v", v, err)
	}
	for _, c := range []struct {
		code int
		body string
	}{
		{http.StatusOK, `{"sqm":23.01}`},
		{http.StatusOK, `{"sqm":14.99}`},
		{http.StatusOK, `{}`},
		{http.StatusOK, `{"sqm":21,"error":"quota"}`},
		{http.StatusForbidden, `{"sqm":21}`},
		{http.StatusTooManyRequests, `{"error":"quota"}`},
	} {
		mutServe(t, c.code, c.body)
		if v, _, err := Lookup(ctx, "secret", 1, 2); err == nil {
			t.Errorf("%d %s: %v, want error", c.code, c.body, v)
		}
	}
	if _, _, err := Lookup(ctx, "wrong", 1, 2); err == nil {
		t.Error("wrong key accepted")
	}
}
