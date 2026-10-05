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
	"math"
	"net/http"
	"os"
	"path/filepath"
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

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Cached returns url's body parsed by parse, from the user cache directory
// when the cached copy (file) is younger than maxAge; a failed download
// falls back to a stale cached copy. Only data that parses is cached, so a
// 200 OK error page never replaces a good copy.
func Cached[T any](ctx context.Context, url, file string, maxAge time.Duration, parse func([]byte) (T, error)) (T, error) {
	var cache string
	if dir, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(dir, "astro-recommender", file)
	}
	if v, err := readCache(cache, maxAge, parse); err == nil {
		return v, nil
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
	v, serr := readCache(cache, time.Duration(math.MaxInt64), parse)
	if serr != nil {
		return v, err
	}
	fmt.Fprintf(os.Stderr, "warning: using cached %s: %v\n", file, err)

	return v, nil
}

var errNoCache = errors.New("no usable cached copy")

// readCache parses the cached copy at cache if it is younger than maxAge.
func readCache[T any](cache string, maxAge time.Duration, parse func([]byte) (T, error)) (T, error) {
	var zero T
	if cache == "" {
		return zero, errNoCache
	}
	if fi, err := os.Stat(cache); err != nil || time.Since(fi.ModTime()) >= maxAge {
		return zero, errNoCache
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		return zero, err
	}

	return parse(data)
}

// maxDownload caps text downloads (MPC comet elements are ~160 kB).
const maxDownload = 32 << 20

// GetText GETs url and returns its body, which must be 200 OK and at most
// maxDownload bytes: a larger one is an error, not silently cut short.
func GetText(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
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

// GetJSONHeader is GetJSON with extra request headers (e.g. an API key).
func GetJSONHeader(ctx context.Context, url string, hdr http.Header, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range hdr {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}
	req.Header.Set("User-Agent", userAgent) // required by Nominatim's usage policy
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Decode even error bodies (they carry reasons) before judging the status.
	err = json.NewDecoder(io.LimitReader(resp.Body, maxDownload)).Decode(v)
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
