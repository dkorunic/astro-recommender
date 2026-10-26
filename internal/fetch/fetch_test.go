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

// GetText retries a 503 once like GetJSON, and a 404 not at all.
func TestGetTextRetry(t *testing.T) {
	RetryDelay = 0
	t.Cleanup(func() { RetryDelay = time.Second })
	var calls, code int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls++; calls == 1 {
			w.WriteHeader(code)

			return
		}
		_, _ = w.Write([]byte("elements"))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		code, calls int
		ok          bool
	}{{http.StatusServiceUnavailable, 2, true}, {http.StatusNotFound, 1, false}} {
		calls, code = 0, tc.code
		data, err := GetText(context.Background(), srv.URL)
		if calls != tc.calls || (err == nil) != tc.ok || tc.ok && string(data) != "elements" {
			t.Errorf("%d: %d calls, %q, %v; want %d calls", tc.code, calls, data, err, tc.calls)
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
		{"9223372036854775807", 0, false},  // would overflow time.Duration
		{"99999999999999999999", 0, false}, // past int: Atoi's range error, still a long wait
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

// StatusError reads as the status line, so a warning names the code.
func TestStatusErrorText(t *testing.T) {
	if got := (&StatusError{http.StatusServiceUnavailable}).Error(); got != "HTTP status 503 Service Unavailable" {
		t.Errorf("Error() = %q", got)
	}
}

// A context cancelled during the retry pause ends the wait at once with
// the context's error, without the second request. The transport cancels
// as it answers 503, so the cancel lands after Do and before the wait,
// which a real server's timing cannot promise.
func TestGetJSONRetryCancel(t *testing.T) {
	old, oldClient := RetryDelay, httpClient
	RetryDelay = time.Hour
	t.Cleanup(func() { RetryDelay, httpClient = old, oldClient })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	httpClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()

		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})}
	if err := GetJSON(ctx, "http://example.invalid", new(any)); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("GetJSON = %v after %d calls, want context.Canceled after 1", err, calls)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A malformed URL or a failed request is an error from both getters.
func TestGetErrors(t *testing.T) {
	if _, err := GetText(context.Background(), "http://[::1"); err == nil {
		t.Error("GetText with a malformed URL succeeded")
	}
	if err := GetJSON(context.Background(), "http://[::1", new(any)); err == nil {
		t.Error("GetJSON with a malformed URL succeeded")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetText(ctx, srv.URL); !errors.Is(err, context.Canceled) {
		t.Errorf("GetText with a cancelled context = %v", err)
	}
}

// readCache reports a missing cache directory as errNoCache and an
// unreadable cache file as an error, never as data.
func TestReadCacheErrors(t *testing.T) {
	if _, _, err := readCache("", anyData); !errors.Is(err, errNoCache) {
		t.Errorf("readCache(\"\") = %v, want errNoCache", err)
	}
	// A directory passes Stat but not ReadFile.
	if data, _, err := readCache(t.TempDir(), anyData); err == nil {
		t.Errorf("readCache(dir) = %q, want an error", data)
	}
}

// A cache file that cannot be renamed into place (a directory there) leaves
// no temp file behind.
func TestWriteCacheRenameFails(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "c.txt")
	if err := os.MkdirAll(filepath.Join(cache, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeCache(cache, []byte("data"))
	if tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(tmps) != 0 {
		t.Errorf("leftover temp files: %v", tmps)
	}
}

// Prune removes only the matching files older than its age, and their
// leftover temp files, not a write in progress or another cache's.
func TestPrune(t *testing.T) {
	tempCache(t)
	base, _ := os.UserCacheDir()
	dir := filepath.Join(base, "astro-recommender")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for name, mtime := range map[string]time.Time{
		"cobs-1.json": old, "cobs-2.json": time.Now(), "other.json": old,
		"cobs-1.json.123.tmp": old, "cobs-2.json.456.tmp": time.Now(), "other.json.789.tmp": old,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	Prune("cobs-*.json", 24*time.Hour)
	for name, want := range map[string]bool{
		"cobs-1.json": false, "cobs-2.json": true, "other.json": true,
		"cobs-1.json.123.tmp": false, "cobs-2.json.456.tmp": true, "other.json.789.tmp": true,
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s kept %v, want %v", name, err == nil, want)
		}
	}
}
