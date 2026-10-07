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

// A 503 or 429 is retried once, unless Retry-After asks for more than
// maxRetryWait; a 400 is not.
func TestGetJSONRetry(t *testing.T) {
	RetryDelay = 0
	t.Cleanup(func() { RetryDelay = time.Second })
	var calls int
	var code int
	var after string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			if after != "" {
				w.Header().Set("Retry-After", after)
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{}`))

			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		code  int
		after string
		calls int
		ok    bool
	}{
		{http.StatusServiceUnavailable, "", 2, true},
		{http.StatusTooManyRequests, "", 2, true},
		{http.StatusTooManyRequests, "0", 2, true},
		{http.StatusTooManyRequests, "120", 1, false},
		{http.StatusServiceUnavailable, time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), 1, false},
		{http.StatusBadRequest, "", 1, false},
	} {
		calls, code, after = 0, tc.code, tc.after
		var body struct {
			OK bool `json:"ok"`
		}
		err := GetJSON(context.Background(), srv.URL, &body)
		if calls != tc.calls || (err == nil) != tc.ok || body.OK != tc.ok {
			t.Errorf("%d %q: %d calls, err %v, ok %v; want %d calls, ok %v", tc.code, tc.after, calls, err, body.OK, tc.calls, tc.ok)
		}
	}
}

func TestRetryWait(t *testing.T) {
	old := RetryDelay
	RetryDelay = time.Second
	t.Cleanup(func() { RetryDelay = old })
	for _, tc := range []struct {
		after string
		want  time.Duration
		ok    bool
	}{
		{"", time.Second, true},
		{"junk", time.Second, true},
		{"0", time.Second, true},
		{"3", 3 * time.Second, true},
		{"5", 5 * time.Second, true},
		{"6", 0, false},
		{"9223372036854775807", 0, false}, // would overflow time.Duration
	} {
		if got, ok := retryWait(tc.after); got != tc.want || ok != tc.ok {
			t.Errorf("retryWait(%q) = %v, %v; want %v, %v", tc.after, got, ok, tc.want, tc.ok)
		}
	}
}

// A redirect within the host is followed; one to another host is refused
// before the caller's headers (an API key) reach it.
func TestRedirect(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Api-Key")
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/away":
			http.Redirect(w, r, other.URL, http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	hdr := http.Header{"X-Api-Key": {"secret"}}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := GetJSONHeader(context.Background(), srv.URL+"/moved", hdr, &body); err != nil || !body.OK {
		t.Errorf("same-host redirect = %v, ok %v", err, body.OK)
	}
	if err := GetJSONHeader(context.Background(), srv.URL+"/away", hdr, &body); !errors.Is(err, errRedirect) || leaked != "" {
		t.Errorf("cross-host redirect = %v, other host got key %q", err, leaked)
	}
}

// Cached downloads once, serves the cache afterwards and leaves no temp files.
func TestCached(t *testing.T) {
	tempCache(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("elements"))
	}))
	defer srv.Close()
	for range 2 {
		data, err := Cached(context.Background(), srv.URL, "test.txt", time.Hour, anyData)
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
	tempCache(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxDownload+1))
	}))
	defer srv.Close()
	if data, err := Cached(context.Background(), srv.URL, "big.txt", time.Hour, anyData); !errors.Is(err, errDownload) || data != nil {
		t.Errorf("Cached = %d bytes, %v; want errDownload", len(data), err)
	}
	dir, _ := os.UserCacheDir()
	if _, err := os.Stat(filepath.Join(dir, "astro-recommender", "big.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("oversized download was cached: %v", err)
	}
}

// A cached copy dated in the future (clock skew, a copied cache) is stale,
// not fresh forever.
func TestCachedFutureMtime(t *testing.T) {
	tempCache(t)
	body := "old"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	if _, err := Cached(context.Background(), srv.URL, "f.txt", time.Hour, anyData); err != nil {
		t.Fatal(err)
	}
	dir, _ := os.UserCacheDir()
	future := time.Now().Add(48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "astro-recommender", "f.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	body = "new"
	if data, err := Cached(context.Background(), srv.URL, "f.txt", time.Hour, anyData); err != nil || string(data) != "new" {
		t.Errorf("Cached = %q, %v; want a fresh download", data, err)
	}
}

// anyData accepts any body as is.
func anyData(data []byte) ([]byte, error) { return data, nil }

// tempCache points os.UserCacheDir at a temp dir on every platform:
// XDG_CACHE_HOME (Unix), HOME (macOS), LocalAppData (Windows).
func tempCache(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "appdata"))
}

// A 200 OK body that fails validation falls back to the stale copy and
// does not overwrite it.
func TestCachedInvalid(t *testing.T) {
	tempCache(t)
	body := "elements"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	errBad := errors.New("bad")
	valid := func(data []byte) (string, error) {
		if string(data) != "elements" {
			return "", errBad
		}

		return string(data), nil
	}
	if _, err := Cached(context.Background(), srv.URL, "v.txt", time.Hour, valid); err != nil {
		t.Fatal(err)
	}
	body = "<html>maintenance</html>"
	// maxAge 0: the cached copy is stale, so Cached downloads again.
	data, err := Cached(context.Background(), srv.URL, "v.txt", 0, valid)
	if err != nil || data != "elements" {
		t.Errorf("Cached = %q, %v; want the stale copy", data, err)
	}
	dir, _ := os.UserCacheDir()
	if got, _ := os.ReadFile(filepath.Join(dir, "astro-recommender", "v.txt")); string(got) != "elements" {
		t.Errorf("cache = %q, want it untouched", got)
	}
	if _, err := Cached(context.Background(), srv.URL, "w.txt", 0, valid); !errors.Is(err, errBad) {
		t.Errorf("Cached without a stale copy = %v, want errBad", err)
	}
}
