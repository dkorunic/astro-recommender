// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A non-200 response is an ErrStatus error while its JSON body (the reason)
// is still decoded.
func TestGetJSONStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"reason":"start_date out of range"}`))
	}))
	defer srv.Close()
	var body struct {
		Reason string `json:"reason"`
	}
	err := GetJSON(context.Background(), srv.URL, &body)
	if !errors.Is(err, ErrStatus) || body.Reason != "start_date out of range" {
		t.Errorf("GetJSON = %v, reason %q", err, body.Reason)
	}
}

// Cached downloads once, serves the cache afterwards and leaves no temp files.
func TestCached(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("elements"))
	}))
	defer srv.Close()
	for range 2 {
		data, err := Cached(context.Background(), srv.URL, "test.txt", time.Hour)
		if err != nil || string(data) != "elements" {
			t.Fatalf("Cached = %q, %v", data, err)
		}
	}
	if hits != 1 {
		t.Errorf("downloaded %d times, want 1", hits)
	}
	dir, _ := os.UserCacheDir()
	tmps, _ := filepath.Glob(filepath.Join(dir, "astro-recommender", "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("leftover temp files: %v", tmps)
	}
}

// A body over maxDownload is an error rather than a truncated copy, and
// Cached does not store it.
func TestGetTextTooLarge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxDownload+1))
	}))
	defer srv.Close()
	if data, err := Cached(context.Background(), srv.URL, "big.txt", time.Hour); !errors.Is(err, errDownload) || data != nil {
		t.Errorf("Cached = %d bytes, %v; want errDownload", len(data), err)
	}
	dir, _ := os.UserCacheDir()
	if _, err := os.Stat(filepath.Join(dir, "astro-recommender", "big.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("oversized download was cached: %v", err)
	}
}
