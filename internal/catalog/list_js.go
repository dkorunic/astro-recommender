// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package catalog

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"syscall/js"

	"github.com/dkorunic/astro-recommender/internal/fetch"
)

// The browser build embeds only the default list (config's -list default):
// the rest are 4.8 MB that every page load would download for one -list.
//
//go:embed targets/GaryImm.gob
var targetFS embed.FS

// openList opens the embedded gob of list name or else fetches it from
// lists/<name>.gob.gz beside the page (task web), gzipped by us since a
// static host need not compress an unknown type. A server that adds Content-Encoding has the
// browser unzip it already, so the gzip header is sniffed, not assumed.
func openList(name string) (io.ReadCloser, error) {
	f, err := targetFS.Open("targets/" + name + ".gob")
	if !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	// Go's client wants an absolute URL; resolve against the page's.
	base, err := url.Parse(js.Global().Get("location").Get("href").String())
	if err != nil {
		return nil, err
	}
	u := base.ResolveReference(&url.URL{Path: "lists/" + name + ".gob.gz"})
	data, err := fetch.GetText(context.Background(), u.String())
	if err != nil {
		return nil, err
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	return gzip.NewReader(bytes.NewReader(data))
}
