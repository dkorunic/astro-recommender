// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package sqm

import (
	"net/http"
	"runtime"
	"testing"
)

// serveAt points Lookup at url for the test: DarkSkySites' API at /api/sqm,
// the browser build's proxy at /sqm, their real paths, so that keyed can
// tell which one was asked.
func serveAt(t *testing.T, url string) {
	t.Helper()
	oldBase, oldProxy := baseURL, proxyURL
	baseURL, proxyURL = url+"/api/sqm", url+"/sqm"
	t.Cleanup(func() { baseURL, proxyURL = oldBase, oldProxy })
}

// keyed reports whether r went where its platform sends it, with the key
// it sends: DarkSkySites with key off the browser; in it the proxy, with no
// key, since the proxy adds its own and a key header would only force a
// CORS preflight.
func keyed(r *http.Request, key string) bool {
	if runtime.GOOS == "js" {
		return r.URL.Path == "/sqm" && r.Header.Get("X-Api-Key") == ""
	}

	return r.URL.Path == "/api/sqm" && r.Header.Get("X-Api-Key") == key
}
