// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"bytes"
	"encoding/gob"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var update = flag.Bool("update", false, "regenerate targets/*.gob from targets/*.yaml")

// TestEmbeddedLists checks that every embedded gob is exactly what Load makes
// of the list's YAML, so a regenerated or edited YAML cannot ship with a stale
// gob; -update (task lists) rewrites them.
func TestEmbeddedLists(t *testing.T) {
	for _, name := range Lists {
		yamlPath := filepath.Join("targets", name+".yaml")
		want, _, err := Load("", yamlPath)
		if err != nil {
			t.Fatal(err)
		}
		if *update {
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(want); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join("targets", name+".gob"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}

			continue
		}
		got, canonical, err := Load(name, "")
		if err != nil {
			t.Fatalf("%v (run task lists)", err)
		}
		if canonical != name || !reflect.DeepEqual(got, want) {
			t.Errorf("targets/%s.gob does not match %s; run task lists", name, yamlPath)
		}
	}
}
