// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package fetch provides HTTP helpers and a small download cache.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var errDownload = errors.New("download failed")

// ErrStatus reports a non-200 HTTP response.
var ErrStatus = errors.New("HTTP status")

// StatusError is the non-200 response GetJSON returns; it matches ErrStatus
// with errors.Is, and errors.As gives callers the code.
type StatusError struct{ Code int }

func (e *StatusError) Error() string {
	return fmt.Sprintf("%v %d %s", ErrStatus, e.Code, http.StatusText(e.Code))
}

func (e *StatusError) Is(target error) bool { return target == ErrStatus }

const userAgent = "astro-recommender (+https://github.com/dkorunic/astro-recommender)"

// identify sets the User-Agent Nominatim's usage policy requires. In the
// browser (GOOS=js) the browser sends its own with a Referer, which the policy
// accepts, and a custom one forces a CORS preflight that MPC's server fails.
func identify(req *http.Request) {
	if runtime.GOOS != "js" {
		req.Header.Set("User-Agent", userAgent)
	}
}

var httpClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: checkRedirect}

var errRedirect = errors.New("refused redirect")

// checkRedirect follows redirects only within the original host and without
// leaving HTTPS. Go re-sends every header but Authorization and cookies on a
// redirect, even to another host over plain HTTP, so a caller's API key
// (sqm's X-Api-Key) would otherwise follow it anywhere. None of the services
// redirect today.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("%w: more than 10", errRedirect)
	}
	if orig := via[0].URL; req.URL.Host != orig.Host || orig.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("%w from %s to %s://%s", errRedirect, orig.Host, req.URL.Scheme, req.URL.Host)
	}

	return nil
}

// Cached returns url's body parsed by parse, from the user cache directory
// when the cached copy (file) is younger than maxAge; a failed download
// falls back to a stale cached copy. Only data that parses is cached, so a
// 200 OK error page never replaces a good copy.
func Cached[T any](ctx context.Context, url, file string, maxAge time.Duration, parse func([]byte) (T, error)) (T, error) {
	var cache string
	if dir, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(dir, "astro-recommender", file)
	}
	// Read once: fresh, it is the answer; stale, the fallback below. A
	// negative age (a clock or a copied file ahead of now) is stale too.
	cached, age, cerr := readCache(cache, parse)
	if cerr == nil && age >= 0 && age < maxAge {
		return cached, nil
	}

	data, err := GetText(ctx, url)
	if err == nil {
		var v T
		if v, err = parse(data); err == nil {
			if cache != "" {
				// A unique temp file renamed into place: concurrent runs never
				// read or install half a file. A cache that cannot be written
				// only costs a download next time.
				writeCache(cache, data)
			}

			return v, nil
		}
	}
	if cerr != nil {
		var zero T

		return zero, err
	}
	fmt.Fprintf(os.Stderr, "warning: using cached %s: %v\n", file, err)

	return cached, nil
}

var errNoCache = errors.New("no cache directory")

// readCache parses the cached copy at cache and returns it with its age.
func readCache[T any](cache string, parse func([]byte) (T, error)) (T, time.Duration, error) {
	var zero T
	if cache == "" {
		return zero, 0, errNoCache
	}
	fi, err := os.Stat(cache)
	if err != nil {
		return zero, 0, err
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		return zero, 0, err
	}
	v, err := parse(data)

	return v, time.Since(fi.ModTime()), err
}

// maxDownload caps text downloads (MPC comet elements are ~160 kB); maxJSON
// caps API responses (~10 kB), which decode into slices of pointers and maps
// many times their size.
const (
	maxDownload = 32 << 20
	maxJSON     = 1 << 20
)

// GetText GETs url and returns its body, which must be 200 OK and at most
// maxDownload bytes: a larger one is an error, not silently cut short.
func GetText(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	identify(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s: %s", errDownload, url, http.StatusText(resp.StatusCode))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err == nil && len(data) > maxDownload {
		return nil, fmt.Errorf("%w: %s: larger than %d MB", errDownload, url, maxDownload>>20)
	}

	return data, err
}

// GetJSON GETs url and decodes the JSON body into v, whatever the status
// (error bodies carry reasons). A non-200 status is returned as a *StatusError.
func GetJSON(ctx context.Context, url string, v any) error {
	return GetJSONHeader(ctx, url, nil, v)
}

// RetryDelay is the pause before the one retry of a 429 or 5xx response:
// Open-Meteo answers 503 "overloaded" for moments at a time.
var RetryDelay = time.Second

// GetJSONHeader is GetJSON with extra request headers (e.g. an API key).
func GetJSONHeader(ctx context.Context, url string, hdr http.Header, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, vals := range hdr {
		req.Header[http.CanonicalHeaderKey(k)] = vals
	}
	identify(req)
	resp, err := httpClient.Do(req)
	if err == nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError) {
		resp.Body.Close()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(RetryDelay):
		}
		resp, err = httpClient.Do(req.Clone(ctx))
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Decode even error bodies (they carry reasons) before judging the status.
	err = json.NewDecoder(io.LimitReader(resp.Body, maxJSON)).Decode(v)
	if resp.StatusCode != http.StatusOK {
		return &StatusError{resp.StatusCode}
	}

	return err
}

func writeCache(cache string, data []byte) {
	dir := filepath.Dir(cache)
	if os.MkdirAll(dir, 0o750) != nil {
		return
	}
	f, err := os.CreateTemp(dir, filepath.Base(cache)+".*.tmp")
	if err != nil {
		return
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr != nil || cerr != nil || os.Rename(f.Name(), cache) != nil {
		_ = os.Remove(f.Name())
	}
}
