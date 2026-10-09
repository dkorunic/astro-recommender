// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func mutNoDelay(t *testing.T) {
	t.Helper()
	old := RetryDelay
	RetryDelay = 0
	t.Cleanup(func() { RetryDelay = old })
}

func TestMutRetryWait(t *testing.T) {
	mutNoDelay(t)
	RetryDelay = 2 * time.Second
	for _, c := range []struct {
		after string
		want  time.Duration
		ok    bool
	}{
		{"", 2 * time.Second, true},
		{"1", 2 * time.Second, true},
		{"3", 3 * time.Second, true},
		{"5", 5 * time.Second, true},
		{"6", 0, false},
		{"99999999999999", 0, false},
		{time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), 2 * time.Second, true},
		{time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), 0, false},
	} {
		got, ok := retryWait(c.after)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("retryWait(%q) = %v %v, want %v %v", c.after, got, ok, c.want, c.ok)
		}
	}
}

func TestMutGetJSONRetry(t *testing.T) {
	mutNoDelay(t)
	for _, c := range []struct {
		first, final, calls int
	}{
		{http.StatusServiceUnavailable, http.StatusOK, 2},
		{http.StatusTooManyRequests, http.StatusOK, 2},
		{http.StatusInternalServerError, http.StatusOK, 2},
		{http.StatusNotFound, http.StatusNotFound, 1},
		{http.StatusBadRequest, http.StatusBadRequest, 1},
	} {
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := c.first
			if n.Add(1) > 1 {
				code = http.StatusOK
			}
			if r.Header.Get("X-Api-Key") != "k" || r.Header.Get("User-Agent") != userAgent {
				code = http.StatusUnauthorized
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"v":` + strconv.Itoa(code) + `}`))
		}))
		var body struct{ V int }
		err := GetJSONHeader(context.Background(), srv.URL, http.Header{"x-api-key": {"k"}}, &body)
		srv.Close()
		if int(n.Load()) != c.calls || body.V != c.final {
			t.Errorf("first %d: calls %d body %d, want %d %d", c.first, n.Load(), body.V, c.calls, c.final)
		}
		var se *StatusError
		if c.final == http.StatusOK {
			if err != nil {
				t.Errorf("first %d: %v", c.first, err)
			}
		} else if !errors.As(err, &se) || se.Code != c.final || !errors.Is(err, ErrStatus) {
			t.Errorf("first %d: err %v, want StatusError %d", c.first, err, c.final)
		}
	}
	// Retry-After past the cap: no retry, the error at once.
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var body struct{}
	if err := GetJSON(context.Background(), srv.URL, &body); !errors.Is(err, ErrStatus) || n.Load() != 1 {
		t.Errorf("long Retry-After: %v after %d calls", err, n.Load())
	}
}

func TestMutGetText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.Error(w, "nope", http.StatusNotFound)

			return
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	if b, err := GetText(context.Background(), srv.URL+"/ok"); err != nil || string(b) != "hello" {
		t.Errorf("GetText = %q %v", b, err)
	}
	if b, err := GetText(context.Background(), srv.URL+"/missing"); err == nil {
		t.Errorf("GetText 404 = %q, want error", b)
	}
}

func TestMutCheckRedirect(t *testing.T) {
	mk := func(s string) *http.Request {
		u, _ := url.Parse(s)

		return &http.Request{URL: u}
	}
	orig := mk("https://api.example.org/a")
	for _, c := range []struct {
		to string
		ok bool
	}{
		{"https://api.example.org/b", true},
		{"http://api.example.org/b", false},
		{"https://evil.example.net/b", false},
	} {
		if err := checkRedirect(mk(c.to), []*http.Request{orig}); (err == nil) != c.ok {
			t.Errorf("redirect to %s: %v", c.to, err)
		}
	}
	if err := checkRedirect(mk("http://h/b"), []*http.Request{mk("http://h/a")}); err != nil {
		t.Errorf("http to http same host: %v", err)
	}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = orig
	}
	if err := checkRedirect(mk("https://api.example.org/b"), via); err == nil {
		t.Error("11th redirect followed")
	}
	if err := checkRedirect(mk("https://api.example.org/b"), via[:9]); err != nil {
		t.Errorf("10th redirect: %v", err)
	}
}

func mutCacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}

	return filepath.Join(dir, "astro-recommender")
}

func mutParse(b []byte) (string, error) {
	if len(b) == 0 || b[0] != 'O' {
		return "", errors.New("bad")
	}

	return string(b), nil
}

func TestMutCached(t *testing.T) {
	dir := mutCacheHome(t)
	var body atomic.Value
	body.Store("OK1")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		b := body.Load().(string)
		if b == "500" {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}
		_, _ = w.Write([]byte(b))
	}))
	defer srv.Close()
	ctx := context.Background()
	get := func() (string, error) { return Cached(ctx, srv.URL, "f.txt", time.Hour, mutParse) }

	if v, err := get(); err != nil || v != "OK1" || calls.Load() != 1 {
		t.Fatalf("first: %q %v calls %d", v, err, calls.Load())
	}
	// Fresh: no download.
	body.Store("OK2")
	if v, err := get(); err != nil || v != "OK1" || calls.Load() != 1 {
		t.Fatalf("fresh: %q %v calls %d", v, err, calls.Load())
	}
	path := filepath.Join(dir, "f.txt")
	// A future mtime is stale: download.
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v, err := get(); err != nil || v != "OK2" || calls.Load() != 2 {
		t.Fatalf("future mtime: %q %v calls %d", v, err, calls.Load())
	}
	// Stale + bad 200: the stale good copy, and it is not replaced.
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(path, old, old)
	body.Store("garbage")
	if v, err := get(); err != nil || v != "OK2" {
		t.Fatalf("stale+garbage: %q %v", v, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "OK2" {
		t.Fatalf("cache replaced by %q", b)
	}
	// Stale + server down: stale copy.
	body.Store("500")
	if v, err := get(); err != nil || v != "OK2" {
		t.Fatalf("stale+500: %q %v", v, err)
	}
	// No cache + garbage: error, nothing written.
	_ = os.Remove(path)
	body.Store("garbage")
	if v, err := get(); err == nil {
		t.Fatalf("no cache+garbage: %q", v)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("garbage cached")
	}
	// A corrupt cached file is not served even when fresh.
	_ = os.WriteFile(path, []byte("corrupt"), 0o600)
	body.Store("OK3")
	if v, err := get(); err != nil || v != "OK3" {
		t.Fatalf("corrupt cache: %q %v", v, err)
	}
}
