// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package catalog

import (
	"bytes"
	"encoding/gob"
	"errors"
	"flag"
	"io/fs"
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

// decodePacked rejects a gob whose per-target lengths do not cover the
// Strings blob exactly: version skew between a fetched list and the wasm's
// packedTarget (gob matches fields by name, a missing one stays zero) must
// stay a decode error, not a slice panic or silently misaligned strings.
func TestDecodePackedLengths(t *testing.T) {
	p := packed{Strings: "NnDdTtCcRrEe", Targets: []packedTarget{{Name: 2, Description: 2, Type: 2, Constellation: 2, RA: 2, Dec: 2}}}
	enc := func() *bytes.Buffer {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(&p); err != nil {
			t.Fatal(err)
		}

		return &buf
	}
	ts, err := decodePacked("x", enc())
	if err != nil || len(ts) != 1 || ts[0].Dec != "Ee" {
		t.Fatalf("consistent gob: %v, %v", ts, err)
	}
	// Bytes that are no gob at all (a truncated or HTML-error download in the
	// browser build) are a decode error naming the list.
	if _, err := decodePacked("x", strings.NewReader("<html>")); err == nil || !strings.Contains(err.Error(), "embedded list x") {
		t.Errorf("non-gob bytes: %v", err)
	}
	if _, err := embedded("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("embedded(nope) = %v, want ErrNotExist", err)
	}
	for _, n := range []uint32{1, 3} { // short lengths misalign, long ones would panic the slice
		p.Targets[0].Dec = n
		if _, err := decodePacked("x", enc()); err == nil {
			t.Errorf("Dec length %d accepted over a 12-byte blob", n)
		}
	}
}
