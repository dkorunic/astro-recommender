// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package catalog loads deep sky targets: the embedded uptonight lists or an
// uptonight-format YAML file.
package catalog

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/constellation"
	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/sanitize"
	"go.yaml.in/yaml/v4"
)

// Target lists from uptonight (MIT, (c) Markus Winkler); OpenNGC and OpenIC
// derive from the OpenNGC catalog (CC BY-SA 4.0, (c) Mattia Verga); MWSC is
// Kharchenko et al. (2013, CDS J/A+A/558/A53); Melotte cross-identifies Melotte
// (1915) and Collinder (1931) with NGC/IC and take positions from MWSC,
// OpenNGC, Dias et al. (2002, CDS B/ocl) and SIMBAD; PNnet is the
// Planetary Nebulae.net catalogue (planetarynebulae.net, with permission); HASH is the HASH PN database (Parker, Bojicic &
// Frew 2016, hashpn.space); Sharpless is Sharpless (1959, CDS VII/20)
// cross-identified with Gary Imm's Deep Sky Compendium; Caldwell is Moore's
// Caldwell catalogue (1995) from the Compendium and OpenNGC; RCW is Rodgers,
// Campbell & Whiteoak (1960, CDS VII/216) and SNR the optically detected
// remnants of Green's catalogue (2024 October version, CDS VII/297), both
// cross-identified with the Compendium; Barnard (1927, VII/220A) dark
// nebulae, van den Bergh's (1966, VII/21) reflection nebulae, Arp's (1966,
// Webb's index VII/192) peculiar galaxies and Hickson's (1982, VII/213)
// compact groups likewise, and van den Bergh & Herbst's (1975, VII/218)
// southern reflection nebulae (vdBH) from VizieR alone.
//
// The binary embeds each list as the gob of what Load makes of its YAML
// (sanitized, checked, positions in degrees, constellations looked up; its
// strings packed into one blob, see packed):
// decoding OpenNGC that way takes 2 ms and 3 MB instead of YAML's 87 ms and
// 52 MB (631 ms in the browser build). The YAML stays the source; task lists
// regenerates the gob files, and TestEmbeddedLists fails when they are stale.
// The browser build embeds only the default list and fetches the others
// (list_js.go), so the page does not download every list up front.

type Target struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Type          string `yaml:"type"`
	Constellation string `yaml:"constellation"`
	RA            string `yaml:"ra"`  // "hh mm ss"
	Dec           string `yaml:"dec"` // "[+-]dd mm ss"

	// Track is set only by comets.Targets, one RA/Dec in degrees per grid
	// minute; never from YAML, where a short track would index past its end.
	Track [][2]float64 `yaml:"-"`
	// RADeg and DecDeg are RA and Dec in J2000 degrees, parsed and checked
	// by Load; never from YAML.
	RADeg   float64 `yaml:"-"`
	DecDeg  float64 `yaml:"-"`
	Size    float64 `yaml:"size"`    // major axis in arc minutes; 0 or negative (-9999) means unknown
	Minor   float64 `yaml:"minor"`   // minor axis in arc minutes (OpenNGC/OpenIC, and the lists fillmag.py fills); 0 or negative means unknown
	Mag     float64 `yaml:"mag"`     // integrated V magnitude; 0 or negative (-9999) means unknown
	BMag    float64 `yaml:"bmag"`    // integrated B magnitude (OpenNGC/OpenIC); 0 or negative means unknown
	SurfBr  float64 `yaml:"surfbr"`  // V surface brightness in mag/arcsec² (GaryImmFull galaxies); 0 or negative means unknown
	BSurfBr float64 `yaml:"bsurfbr"` // mean B surface brightness within the 25 mag isophote (OpenNGC/OpenIC), ~0.8 fainter than V; 0 or negative means unknown
}

// HasSize reports whether the size is known: comets and entries with a zero
// or negative size (-9999 in the lists, 0.0 in a few LDN entries) have none.
func (t Target) HasSize() bool { return t.Size > 0 }

// Axes returns the major and minor axes in arc minutes, both 0 without a
// size (never the -9999 sentinel). The minor axis is the major one when
// unknown: a round object, the frame fit's worst case.
func (t Target) Axes() (float64, float64) {
	switch {
	case !t.HasSize():
		return 0, 0
	case t.Minor > 0 && t.Minor < t.Size:
		return t.Size, t.Minor
	}

	return t.Size, t.Size
}

// HasMag reports whether the magnitude is known. No deep sky object is
// brighter than 0, so like Size the sentinel is 0 or negative (-9999 in the
// lists; LBN.yaml's upstream value was Lynds' brightness class, blanked).
func (t Target) HasMag() bool { return t.Mag > 0 }

// minBV..maxBV bound a plausible deep sky B−V; OpenNGC/OpenIC pair
// inconsistent magnitudes for ~100 objects (IC 127: V 9.2, B 15.4).
const minBV, maxBV = -0.3, 1.5

// Bounds on a known (positive) magnitude or surface brightness in a list:
// nothing is fainter than maxNumber, and no extended object is brighter than
// minSB mag/arcsec², so a `surfbr: 2.3` typo for 22.3, which would make an
// object immune to sky glow, is rejected rather than ranked first.
const maxNumber, minSB = 35, 10

// Cluster reports whether the object is an open or globular cluster without
// nebulosity: light in point sources that sky glow hardly hurts.
func (t Target) Cluster() bool { return t.kind() == kindCluster }

// Classes are the object classes -type selects, in help order.
var Classes = []string{classGalaxy, classCluster, classNebula, classEmission, classComet, classOther}

const (
	classGalaxy   = "galaxy"
	classCluster  = "cluster"
	classNebula   = "nebula"
	classEmission = "emission"
	classComet    = "comet"
	classOther    = "other"
)

// Is reports whether the target belongs to class c (one of Classes): a
// galaxy group is a galaxy, a cluster with nebulosity both a cluster and a
// nebula, emission is EmissionLine and other the stars, asterisms and
// unknown types.
func (t Target) Is(c string) bool {
	k := t.kind()
	switch c {
	case classGalaxy:
		return k == kindGalaxy || k == kindGroup
	case classCluster:
		return k == kindCluster || k == kindClusterNebula
	case classNebula:
		return k == kindNebula || k == kindClusterNebula
	case classEmission:
		return EmissionLine(t)
	case classComet:
		return t.Track != nil
	case classOther:
		return k == kindOther && t.Track == nil
	}

	return false
}

// SurfaceBrightness returns the object's V surface brightness in mag/arcsec²,
// the visibility measure for extended objects (it compares directly with the
// sky's), or 0 when none is known, and whether it is an estimate. It is the
// measured V one, else the B one moved to V by colour() (an estimate when the
// colour is the type's typical one), else, for galaxies and nebulae only
// (not groups), one derived by spreading the V magnitude (or B − typicalBV)
// over a uniform disc of the major axis (an estimate; the disc overstates the
// area of an elongated object, so it leans faint). Clusters get none, measured
// or not: skyK (scoring) halves the sky for their point sources instead, and
// MWSC's sizes are the core only. Clusters with nebulosity derive none, since
// their integrated magnitude is the stars', not the glow's, so without a
// measurement they are scored sky-limited, like a nebula without data. Stars,
// asterisms and comets derive none either.
func (t Target) SurfaceBrightness() (float64, bool) {
	k := t.kind()
	if k == kindCluster {
		return 0, false
	}
	switch {
	case t.SurfBr > 0:
		return t.SurfBr, false
	case t.BSurfBr > 0:
		bv, own := t.colour(k)

		return t.BSurfBr - bv, !own
	}
	if t.Track != nil || !t.HasSize() || (k != kindGalaxy && k != kindNebula) {
		return 0, false
	}
	mag := t.Mag
	if !t.HasMag() && t.BMag > 0 {
		mag = t.BMag - t.typicalBV(k)
	}
	if mag <= 0 {
		return 0, false
	}

	return mag + 2.5*math.Log10(math.Pi/4*(60*t.Size)*(60*t.Size)), true
}

// typicalSB is the median V surface brightness per sbClass over the embedded
// lists' objects that have one (TestTypicalSB recomputes it), for the
// sub-exposure estimate of an object without one; scoring never uses it.
// Brighter objects are the ones measured, so it likely flatters the object
// it stands in for.
// sbPlanetary is sbClass's planetary nebula class.
const sbPlanetary = "planetary"

var typicalSB = map[string]float64{
	"galaxy":     22.52,
	sbPlanetary:  21.75,
	"emission":   22.82,
	"reflection": 22.03,
	"nebula":     20.91,
}

// ExposureSB returns the surface brightness the sub-exposure estimate uses:
// the object's own (SurfaceBrightness), else its class's typical one with
// assumed true; 0 when no estimate applies (sbClass).
func (t Target) ExposureSB() (float64, bool) {
	class := t.sbClass()
	if class == "" {
		return 0, false
	}
	if sb, _ := t.SurfaceBrightness(); sb > 0 {
		return sb, false
	}

	return typicalSB[class], true
}

// Position returns the RA and Dec in degrees that stand for the target: the
// J2000 catalog position, or for a comet its mid-track one (of date, within
// 0.4° of J2000). The -ra/-dec filter and the RA/DEC columns both use it, so
// the table never shows a position outside the region that kept the object.
func (t Target) Position() (float64, float64) {
	if len(t.Track) > 0 {
		mid := t.Track[len(t.Track)/2]

		return mid[0], mid[1]
	}

	return t.RADeg, t.DecDeg
}

// Line is an emission line nebula filters isolate.
type Line int

const (
	Ha   Line = iota // Hα 656.3 nm
	OIII             // [O III] 500.7 nm
	SII              // [S II] 671.6/673.1 nm, one band
	Hb               // Hβ 486.1 nm
)

// LineNM are the lines' wavelengths in nm (the [S II] doublet's mean).
var LineNM = [...]float64{Ha: 656.3, OIII: 500.7, SII: 672.4, Hb: 486.1}

// balmer is the Hα/Hβ flux ratio of case-B recombination at 10⁴ K
// (Osterbrock & Ferland 2006), the same for every type; dust raises it.
const balmer = 2.86

// lineRatios are typical [O III] 500.7 and [S II] fluxes relative to Hα by
// object type. H II regions: [S II]/Hα 0.12 on average (0.1-0.3), [O III]
// weak outside the bright cores. Planetary nebulae: [O III]/Hβ 6-9 (Magellanic
// Cloud averages), so 2.8 Hα, [S II] faint. Supernova remnants: [S II]/Hα
// above 0.4 is their defining criterion (Mathewson & Clarke 1973), [O III]
// varies (strong in the Veil). Wolf-Rayet shells are [O III]-bright.
// ponytail: one ratio per type where single objects scatter by factors of a
// few, and [N II] 654.8/658.3, inside most Hα bands, is left out.
var lineRatios = []struct {
	word   string // in the type, lower case
	ratios [3]float64
}{
	{"planetary", [3]float64{Ha: 1, OIII: 2.8, SII: 0.05}},
	{"supernova", [3]float64{Ha: 1, OIII: 0.5, SII: 0.6}},
	{"wolf", [3]float64{Ha: 1, OIII: 1, SII: 0.2}},
}

// Lines returns the shares of an emission-line target's light in Hα, [O III]
// and [S II] (summing to 1), from its type's typical ratios (lineRatios; H II
// regions, and the dark nebulae and clusters emissionNames lists, otherwise),
// and Hβ on top at Hα/balmer, outside the sum so that adding it left the
// other filters' scores unchanged; all 0 when it is not EmissionLine.
func (t Target) Lines() [4]float64 {
	if !EmissionLine(t) {
		return [4]float64{}
	}
	r := classOf(t.Type).ratios
	sum := r[Ha] + r[OIII] + r[SII]

	return [4]float64{r[Ha] / sum, r[OIII] / sum, r[SII] / sum, r[Ha] / sum / balmer}
}

// sbClass is t's class for typicalSB; "" when an exposure estimate does not
// apply: clusters (point sources), galaxy groups (one member's magnitude over
// the group's size), comets, stars, and dark nebulae (no light of their own)
// unless imaged for an emission line behind them.
func (t Target) sbClass() string {
	typ := strings.ToLower(t.Type)
	switch k := t.kind(); {
	case k == kindGalaxy:
		return "galaxy"
	case k != kindNebula && k != kindClusterNebula:
		return ""
	case strings.Contains(typ, "planetary"):
		return sbPlanetary
	case EmissionLine(t):
		return "emission"
	case strings.Contains(typ, "dark"):
		return ""
	case strings.Contains(typ, "reflection"):
		return "reflection"
	}

	return "nebula"
}

// kind is the class of object the scoring rules act on. The lists spell
// types many ways, so every rule asks kind() rather than the type text;
// TestKindLists fails on a spelling none of the rules know.
type kind int

const (
	kindOther         kind = iota // stars, asterisms, novae, duplicates
	kindGalaxy                    // a single galaxy
	kindGroup                     // galaxy group or pair: the magnitude is the brightest member's, the size the whole group's
	kindCluster                   // open or globular cluster: point sources that sky glow hardly hurts, which skyK (scoring) halves the sky for
	kindClusterNebula             // cluster with nebulosity: imaged for the glow (skyK keeps 1), but the magnitude is the stars'
	kindNebula                    // every other nebula, emission-line or not
)

// kindTypes are the lists' spellings (lower case) that the substring tests
// in kind() would misread: OpenNGC's galaxy group abbreviations, the
// clusters, the cluster+nebula abbreviations (OpenNGC, Pensack) and Pensack's
// "DN" (OpenNGC's "EmN" is in emissionTypes). A "Star Cloud" stays a nebula:
// M 24 is a bright patch, NGC 206 in M31 a faint one, and a derived surface
// brightness tells them apart.
var kindTypes = map[string]kind{
	"gtrpl": kindGroup, "ggroup": kindGroup,
	"open cluster": kindCluster, "globular cluster": kindCluster,
	"cl+n emission nebula": kindClusterNebula, "n+cl": kindClusterNebula, "bn+oc": kindClusterNebula, "cl+n": kindClusterNebula,
	"dn": kindNebula,
}

// kind classifies by the type text, except that a cluster in emissionNames is
// one with nebulosity (Messier types M 16 "Open Cluster"). For every other
// type the names say what is imaged, not what the magnitude measures (R
// Aquarii is a star), so they do not promote it.
func (t Target) kind() kind {
	k := classOf(t.Type).kind
	if k == kindCluster && emissionName(t.Name) {
		return kindClusterNebula
	}

	return k
}

// typeClass is what the type text alone decides: kind() before the
// emissionNames promotion, whether it is in emissionTypes, and Lines' ratios.
type typeClass struct {
	ratios   [3]float64
	kind     kind
	emission bool
}

// typeClasses caches classify per Type spelling (a few hundred across the
// lists): lower-casing the type on every call was most of Score's
// allocations, several per target per -filter choice.
var typeClasses sync.Map // Type → typeClass

func classOf(typ string) typeClass {
	if c, ok := typeClasses.Load(typ); ok {
		return c.(typeClass) //nolint:forcetypeassert // classOf stores only typeClass
	}
	c := classify(typ)
	typeClasses.Store(typ, c)

	return c
}

func classify(typ string) typeClass {
	typ = strings.ToLower(typ)
	c := typeClass{emission: emissionTypes[typ], ratios: [3]float64{Ha: 1, OIII: 0.3, SII: 0.12}}
	for _, l := range lineRatios {
		if strings.Contains(typ, l.word) {
			c.ratios = l.ratios

			break
		}
	}
	k, ok := kindTypes[typ]
	if !ok {
		switch {
		case strings.Contains(typ, "galax") && slices.ContainsFunc(groupWords, func(w string) bool { return strings.Contains(typ, w) }):
			k = kindGroup
		case strings.Contains(typ, "galax"):
			k = kindGalaxy
		case c.emission, strings.Contains(typ, "nebul"), strings.Contains(typ, "cloud"):
			k = kindNebula
			if strings.Contains(typ, "cluster") { // Herschel/MWSC "Cluster Nebulosity"
				k = kindClusterNebula
			}
		}
	}
	c.kind = k

	return c
}

// groupWords mark a galaxy type as a group of galaxies.
var groupWords = []string{"group", "cluster", "pair", "duo", "trio", "triplet"}

// colour is the object's B−V and whether it is the object's own (both
// magnitudes known and plausible) rather than typicalBV.
func (t Target) colour(k kind) (float64, bool) {
	// 9.7 - 10 is -0.3000000000000007: the bounds are inclusive, so allow rounding.
	if bv := t.BMag - t.Mag; t.BMag > 0 && t.HasMag() && bv >= minBV-1e-9 && bv <= maxBV+1e-9 {
		return bv, true
	}

	return t.typicalBV(k), false
}

// typicalBV is the B−V assumed for the object's type when its own is not
// usable: 0.8 for galaxies and their groups (the median of the Compendium's
// V surface brightness against OpenNGC's B one over 723 shared galaxies), 0
// for emission-line objects (Hα/OIII dominated, often bluer than that), 0.5
// for the rest (reflection nebulae, clusters with nebulosity). Rough values;
// they move a B figure to V to within a few tenths.
func (t Target) typicalBV(k kind) float64 {
	switch {
	case k == kindGalaxy, k == kindGroup:
		return 0.8
	case EmissionLine(t):
		return 0
	}

	return 0.5
}

// Lists are the embedded uptonight target lists selectable with -list.
var Lists = []string{"GaryImm", "GaryImmFull", "Messier", "Herschel400", "Pensack500", "OpenNGC", "OpenIC", "LBN", "LDN", "MWSC", "Melotte", "Collinder", "PNnet", "HASH", "Sharpless", "Caldwell", "RCW", "SNR", "Barnard", "vdB", "vdBH", "Arp", "HCG", "Ced", "Gum", "CG", "ACO", "MCRN", "Henize", "DEM", "Harris", "Dias", "DCld", "AM"}

var (
	errList        = errors.New("unknown target list")
	errCoordinates = errors.New("bad coordinates")
	errSky         = errors.New("RA must be 0 to 24h and Dec within ±90°")
	errNumber      = errors.New("bad number")
	errDocuments   = errors.New("more than one YAML document")
)

// Load reads -targets, or the embedded -list target list.
// It returns the targets and the canonical list name.
func Load(listName, file string) ([]Target, string, error) {
	if file == "" {
		i := slices.IndexFunc(Lists, func(n string) bool { return strings.EqualFold(n, listName) })
		if i < 0 {
			return nil, "", fmt.Errorf("%w %q, want one of %s", errList, listName, strings.Join(Lists, ", "))
		}
		targets, err := embedded(Lists[i])

		return targets, Lists[i], err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	// Every document is read: Unmarshal would take the first and drop the
	// rest without a word, so a stray "---" lost every target after it. An
	// empty document (a leading or trailing "---", a comment-only file) holds
	// nothing and is skipped; a second list is an error, not merged.
	// Each document goes through a Node first, which keeps its keys: a
	// misspelt one (sise: 20) would otherwise vanish, and the object with it.
	var targets []Target
	var unknown []string
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for seen := false; ; {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		var doc []Target
		if err == nil {
			err = node.Decode(&doc)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
		if doc == nil {
			continue
		}
		if seen {
			return nil, "", fmt.Errorf("%w: %s", errDocuments, file)
		}
		targets, seen, unknown = doc, true, unknownKeys(&node)
	}
	if len(unknown) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s: unknown keys ignored, check their spelling: %s\n", file, strings.Join(unknown, ", "))
	}
	// -targets may come from anywhere; its text ends up on the terminal.
	for i := range targets {
		tg := &targets[i]
		tg.Name, tg.Description = strings.TrimSpace(sanitize.Text(tg.Name)), sanitize.Text(tg.Description)
		tg.Type = strings.TrimSpace(sanitize.Text(tg.Type)) // kindTypes and emissionTypes match exactly
		// The IAU boundaries beat the lists' own field, which has wrong
		// entries (R Aquarii "Aquila") and spellings ("Ophiucus", "Se1").
		ra, err1 := astro.Sexagesimal(tg.RA)
		dec, err2 := astro.Sexagesimal(tg.Dec)
		err := errors.Join(err1, err2)
		if err == nil && (ra < 0 || ra >= 24 || math.Abs(dec) > 90) {
			err = fmt.Errorf("%w: %q %q", errSky, tg.RA, tg.Dec)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%w for %s: %w", errCoordinates, tg.Name, err)
		}
		// 0 or negative is the unknown sentinel; a known value must be plausible.
		for _, f := range []struct {
			name   string
			v      float64
			lo, hi float64
		}{
			{"size", tg.Size, 0, 180 * 60}, // arcminutes; nothing spans more than the sky
			{"minor", tg.Minor, 0, 180 * 60},
			{"mag", tg.Mag, 0, maxNumber},
			{"bmag", tg.BMag, 0, maxNumber},
			{"surfbr", tg.SurfBr, minSB, maxNumber},
			{"bsurfbr", tg.BSurfBr, minSB, maxNumber},
		} {
			if !num.Finite(f.v) || (f.v > 0 && (f.v < f.lo || f.v > f.hi)) {
				return nil, "", fmt.Errorf("%w for %s: %s %v", errNumber, tg.Name, f.name, f.v)
			}
		}
		// A minor axis is a size too, so it needs a known major axis to sit under.
		if tg.Minor > 0 && tg.Minor > tg.Size {
			return nil, "", fmt.Errorf("%w for %s: minor %v exceeds size %v", errNumber, tg.Name, tg.Minor, tg.Size)
		}
		tg.RADeg, tg.DecDeg = ra*15, dec // hours -> degrees
		tg.Constellation = constellation.Of(tg.RADeg, tg.DecDeg)
	}

	return targets, listName, nil
}

// knownKeys are the target keys Load reads (Target's yaml tags) and the
// ones it tolerates unread: GaryImmFull's Compendium rating.
var knownKeys = func() map[string]bool {
	known := map[string]bool{"rating": true}
	t := reflect.TypeFor[Target]()
	for field := range t.Fields() {
		if name, _, _ := strings.Cut(field.Tag.Get("yaml"), ","); name != "" && name != "-" {
			known[name] = true
		}
	}

	return known
}()

// unknownKeys returns the keys of a target list document (a sequence of
// mappings) that knownKeys lacks, sanitized, sorted and each once. A merge
// key (<<: *anchor) is the decoder's, not a target's; the mapping it pulls
// in is checked where the anchor is defined.
func unknownKeys(doc *yaml.Node) []string {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	var out []string
	for _, item := range doc.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i].ShortTag() == "!!merge" {
				continue
			}
			if k := sanitize.Text(item.Content[i].Value); !knownKeys[k] && !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)

	return out
}

// embedded decodes the gob of list name, already in Load's form. Open
// rather than ReadFile: the decoder reads the embedded data in place
// instead of a copy.
func embedded(name string) ([]Target, error) {
	f, err := openList(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var p packed
	if err := gob.NewDecoder(f).Decode(&p); err != nil {
		return nil, fmt.Errorf("embedded list %s: %w", name, err)
	}
	strs := p.Strings
	next := func(n uint32) string {
		s := strs[:n]
		strs = strs[n:]

		return s
	}
	targets := make([]Target, len(p.Targets))
	for i, pt := range p.Targets {
		targets[i] = Target{
			Name: next(pt.Name), Description: next(pt.Description), Type: next(pt.Type),
			Constellation: next(pt.Constellation), RA: next(pt.RA), Dec: next(pt.Dec),
			RADeg: pt.RADeg, DecDeg: pt.DecDeg, Size: pt.Size, Minor: pt.Minor,
			Mag: pt.Mag, BMag: pt.BMag, SurfBr: pt.SurfBr, BSurfBr: pt.BSurfBr,
		}
	}

	return targets, nil
}

// packed is the embedded gob of a list: every target's strings in one
// Strings blob, in field order, each target holding their lengths. Gob
// decodes each string into its own allocation, so a plain []Target cost one
// per field, 91% of a whole run's allocations with OpenNGC; the targets'
// strings now share the blob. Track never comes from a list.
type packed struct {
	Strings string
	Targets []packedTarget
}

type packedTarget struct {
	// The strings' lengths; separate fields, as gob decodes an array
	// through a slice header allocated per target.
	Name, Description, Type, Constellation, RA, Dec        uint32
	RADeg, DecDeg, Size, Minor, Mag, BMag, SurfBr, BSurfBr float64
}

// emissionTypes are the lists' spellings (lower case) of emission-line types:
// uptonight's own, LBN's "HII region", OpenNGC/OpenIC's HII and cluster+nebula
// classes and their "EmN" abbreviation.
var emissionTypes = map[string]bool{
	"emission nebula": true, "planetary nebula": true, "supernova remnant": true, "wolf-rayet nebula": true,
	"hii region": true, "hii emission nebula": true, "cl+n emission nebula": true, "emn": true,
}

// emissionNames lists targets whose type does not say emission although what
// gets imaged is mostly Ha/OIII: dark nebulae, novae, and the types that mix
// emission with reflection or a cluster (Messier "Diffuse Nebula", Pensack
// "N+CL"/"BN+OC", Herschel "Cluster Nebulosity"), where only the
// emission-dominated objects are listed (not M 78, NGC 1977, 1980 or 2362).
// Names match as NameKey (emissionName), so Pensack's "IC1805" or a
// -targets "M16" needs no entry of its own; a different designation does.
var emissionNames = map[string]bool{
	"IC 1396":       true, // Elephant's Trunk, inside the IC 1396 HII region
	"Sh2-155":       true, // Cave Nebula
	"NGC 2024":      true, // Flame Nebula
	"IC 434":        true, // Horsehead, silhouetted against Ha emission
	"Barnard 33":    true, // the Horsehead itself (Barnard, GaryImmFull)
	"Cederblad 211": true, // R Aquarii symbiotic nebula
	"M 8":           true, // Lagoon
	"M 16":          true, // Eagle (an Open Cluster in Messier.yaml)
	"M 17":          true, // Omega
	"M 20":          true, // Trifid
	"M 42":          true, // Orion
	"M 43":          true, // De Mairan's
	"NGC 1931":      true, // Fly
	"NGC 281":       true, // PacMan
	"NGC 2264":      true, // Cone / Christmas Tree
	"Mel 49":        true, // NGC 2264 (Melotte)
	"NGC 1976":      true, // M 42
	"NGC 2467":      true, // Skull and Crossbones
	"IC4703(BN)":    true, // M 16 Eagle (Pensack's spelling)
	"NGC 6611":      true, // M 16 (MWSC)
	"Mel 198":       true, // M 16 (Melotte)
	"NGC 6514":      true, // M 20
	"NGC 6618":      true, // M 17
	"NGC 6820":      true, // with NGC 6823
	"NGC 7380":      true, // Wizard
	"IC 1805":       true, // Heart (MWSC)
	"Mel 15":        true, // IC 1805 Heart (Melotte)
	"NGC 2244":      true, // Rosette's cluster
	"Mel 47":        true, // NGC 2244 (Melotte)
	"NGC 6530":      true, // M 8 Lagoon's cluster
	"IC 1848":       true, // Soul (MWSC)
	"LBN 667":       true, // IC 1848 Soul (LBN's "Open cluster")
	"NGC 1893":      true, // in IC 410
	"Mel 33":        true, // NGC 1893 (Melotte)
	"NGC 6823":      true, // with NGC 6820
	"LBN 135":       true, // NGC 6823 (LBN's "Open cluster")
	// Collinder spellings of the above
	"Cr 26":  true, // IC 1805 Heart
	"Cr 32":  true, // in IC 1848 Soul
	"Cr 33":  true, // in IC 1848 Soul
	"Cr 34":  true, // IC 1848 Soul
	"Cr 63":  true, // NGC 1893
	"Cr 99":  true, // NGC 2244 Rosette
	"Cr 362": true, // NGC 6530 Lagoon
	"Cr 405": true, // NGC 6823
	"Cr 68":  true, // NGC 1931 Fly
	"Cr 112": true, // NGC 2264 Cone
	"Cr 164": true, // NGC 2467
	"Cr 360": true, // NGC 6514 M 20
	"Cr 375": true, // NGC 6611 M 16
	"Cr 377": true, // NGC 6618 M 17
	"Cr 404": true, // NGC 6820
	"Cr 439": true, // IC 1396
	"Cr 452": true, // NGC 7380 Wizard
	// Caldwell spellings
	"C 50":  true, // NGC 2244 Rosette
	"C 100": true, // Collinder 249 in IC 2944, Running Chicken
}

// EmissionLine reports whether a target shines mainly in Ha/OIII lines.
func EmissionLine(tg Target) bool {
	return classOf(tg.Type).emission || emissionName(tg.Name)
}

// emissionKeys is emissionNames keyed by NameKey.
var emissionKeys = func() map[string]bool {
	keys := make(map[string]bool, len(emissionNames))
	for name := range emissionNames {
		keys[NameKey(name)] = true
	}

	return keys
}()

// emissionName reports whether name is in emissionNames, case and spaces
// aside, as -skip matches names.
func emissionName(name string) bool { return HasKey(emissionKeys, name) }

// NameKey normalizes a designation for matching names typed by hand against
// the lists': case and spaces do not count, so "ngc7789" is "NGC 7789".
func NameKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", ""))
}

// HasKey reports m[NameKey(name)] without building the key on the heap for
// the short ASCII names the lists use (Score asks for every target); other
// names go through NameKey.
func HasKey(m map[string]bool, name string) bool {
	var buf [32]byte
	b := buf[:0]
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= utf8.RuneSelf || len(b) == len(buf):
			return m[NameKey(name)]
		case c == ' ':
			continue
		case 'A' <= c && c <= 'Z':
			c += 'a' - 'A'
		}
		b = append(b, c)
	}

	return m[string(b)] // the conversion in a map index does not allocate
}
