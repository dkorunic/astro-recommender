// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !js

package catalog

import (
	"embed"
	"io"
)

//go:embed targets/*.gob
var targetFS embed.FS

// openList opens the embedded gob of list name.
func openList(name string) (io.ReadCloser, error) {
	return targetFS.Open("targets/" + name + ".gob")
}
