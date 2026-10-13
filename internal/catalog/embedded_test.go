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
	"strings"
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
			if err := gob.NewEncoder(&buf).Encode(pack(want)); err != nil {
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

// pack is the inverse of embedded's decoding, for task lists.
func pack(targets []Target) packed {
	var p packed
	var blob strings.Builder
	for _, t := range targets {
		pt := packedTarget{
			Name: uint32(len(t.Name)), Description: uint32(len(t.Description)), Type: uint32(len(t.Type)),
			Constellation: uint32(len(t.Constellation)), RA: uint32(len(t.RA)), Dec: uint32(len(t.Dec)),
			RADeg: t.RADeg, DecDeg: t.DecDeg, Size: t.Size, Minor: t.Minor,
			Mag: t.Mag, BMag: t.BMag, SurfBr: t.SurfBr, BSurfBr: t.BSurfBr,
		}
		for _, s := range []string{t.Name, t.Description, t.Type, t.Constellation, t.RA, t.Dec} {
			blob.WriteString(s)
		}
		p.Targets = append(p.Targets, pt)
	}
	p.Strings = blob.String()

	return p
}
