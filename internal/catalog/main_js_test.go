// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package catalog

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// listsGzipped counts the lists served gzipped; the others go out plain, as
// a server that set Content-Encoding (the browser unzipping them) would
// hand them over, so openList's sniffing reads both.
var listsGzipped, listsPlain atomic.Int32

// TestMain serves the browser build's lists as task web lays them out
// (lists/<name>.gob.gz beside the page) and points location, which Node
// lacks, at the page there, so that Load fetches every list but the
// embedded GaryImm as the browser does.
func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/lists/"), ".gob.gz")
		data, err := os.ReadFile(filepath.Join("targets", filepath.Base(name)+".gob"))
		if !ok || err != nil {
			http.NotFound(w, r)

			return
		}
		// Every other list plain, by its name's length.
		if len(name)%2 == 0 {
			listsPlain.Add(1)
			_, _ = w.Write(data)

			return
		}
		listsGzipped.Add(1)
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		_, _ = zw.Write(data)
		_ = zw.Close()
		_, _ = w.Write(b.Bytes())
	}))
	setLocation(srv.URL + "/index.html")
	code := m.Run()
	srv.Close()
	os.Exit(code)
}
