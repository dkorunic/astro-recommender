// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package view gathers what the interactive front ends show of a night, the
// browser page (main_js.go writes it as JSON) and the terminal UI
// (internal/tui): each listed result's altitude curve, observable stretches
// and card, and the night's Moon, edges and forecast hours.
package view

import (
	"math"
	"strconv"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// Step is the spacing of the altitude curves, in grid minutes.
const Step = 5

// Doc is one night as the front ends show it; the JSON names are the web
// page's. Per-result slices are in rank order, one entry per listed result.
type Doc struct { // betteralign:ignore
	Zone  string              `json:"zone"` // IANA, "" for the viewer's own
	Alt   [][]float64         `json:"alt"`  // altitude to 0.1° every Step minutes from Start
	Pos   [][3]float64        `json:"pos"`  // J2000 RA and Dec (degrees), size (arcmin, 0 unknown)
	Obs   [][][2]int64        `json:"obs"`  // observable stretches, [from, to) Unix seconds
	Subs  []*scoring.Estimate `json:"subs"` // null without one
	Star  []*output.StarInfo  `json:"star"` // STAR's, null without one
	Wx    []WxHour            `json:"wx"`
	Cards []Card              `json:"cards"`
	Plan  []Slot              `json:"plan,omitempty"`
	Night Night               `json:"night"`
	Start int64               `json:"start"`
	Step  int64               `json:"step"`
	Min   float64             `json:"min"`
	Max   float64             `json:"max"`
}

// WxHour is one forecast hour within the window: cloud cover (%) and
// whether it rains (from the weather table's threshold).
type WxHour struct {
	T     int64   `json:"t"`
	Cloud float64 `json:"cloud"`
	Rain  bool    `json:"rain"`
}

// Card is what a result card shows beyond its curve: the target table's
// values as numbers (0 or "" unknown), times in Unix seconds, and the
// azimuth at the peak for the curve's label.
type Card struct { // betteralign:ignore
	Name     string  `json:"name"`
	Desc     string  `json:"desc,omitempty"`
	Type     string  `json:"type"`
	Con      string  `json:"con,omitempty"`
	Filter   string  `json:"filter,omitempty"`
	Mosaic   string  `json:"mosaic,omitempty"` // "2x1" above one panel
	Score    float64 `json:"score"`
	Foto     float64 `json:"foto"`
	Size     float64 `json:"size,omitempty"`  // arcmin, the frame's (a core's when framed at one)
	Minor    float64 `json:"minor,omitempty"` // arcmin, when below Size
	Mag      float64 `json:"mag,omitempty"`   // V
	BMag     float64 `json:"bmag,omitempty"`  // B, when no V
	SB       float64 `json:"sb,omitempty"`
	SkyMag   float64 `json:"sky"`
	MaxAlt   float64 `json:"maxAlt"`
	MaxAz    float64 `json:"maxAz"`
	MaxAt    int64   `json:"maxAt"`
	RunFrom  int64   `json:"runFrom"`
	RunTo    int64   `json:"runTo"`
	Runs     int     `json:"runs"`
	SBGuess  bool    `json:"sbGuess,omitempty"`
	Emission bool    `json:"emission,omitempty"`
	Comet    bool    `json:"comet,omitempty"`
}

// Night is the window's summary: the place, the Moon and the night's
// edges, in Unix seconds.
type Night struct { // betteralign:ignore
	Place    string     `json:"place,omitempty"`
	Lat      float64    `json:"lat"`
	Lon      float64    `json:"lon"`
	Twilight string     `json:"twilight"`
	Dusk     int64      `json:"dusk"`
	Dawn     int64      `json:"dawn"`
	End      int64      `json:"end"`
	Illum    float64    `json:"illum"`
	MoonSep  float64    `json:"moonSep"`
	MoonLow  string     `json:"moonFloor,omitempty"` // output.MoonFloor: " (down to 30° through a filter)"
	MoonUp   [][2]int64 `json:"moonUp"`
	Washout  bool       `json:"washout,omitempty"`
	Targets  int        `json:"targets"` // results scored, before -n
	Sky      float64    `json:"zenith"`  // zenith mag/arcsec² as configured
}

// Slot is one -plan block: its target's name and rank among the listed
// results (0 when not listed, "" without a target).
type Slot struct { // betteralign:ignore
	From   int64   `json:"from"`
	To     int64   `json:"to"`
	Name   string  `json:"name,omitempty"`
	Rank   int     `json:"rank,omitempty"`
	Score  float64 `json:"score,omitempty"`
	Filter string  `json:"filter,omitempty"`
}

// Build gathers the night s with its listed results (the first cfg.Top),
// the -plan slots and the geocoded place. A result without per-minute data
// is rescored once for its observable stretches.
func Build(cfg *config.Config, s *scoring.Sky, place string, results []scoring.Result, slots []plan.Slot) Doc {
	doc := Doc{Zone: zone(cfg.Loc), Step: Step * 60, Min: cfg.AltMin, Max: cfg.AltMax, Cards: []Card{}}
	if len(s.Grid) == 0 {
		return doc
	}
	doc.Start = s.Grid[0].Unix()
	for t := s.Start.Truncate(time.Hour); t.Before(s.End); t = t.Add(time.Hour) {
		if h, ok := s.Weather[t.Unix()]; ok {
			doc.Wx = append(doc.Wx, WxHour{T: t.Unix(), Cloud: math.Round(h.Cloud), Rain: h.Precip >= weather.RainGate/2})
		}
	}
	doc.Night = Night{
		Place: place, Lat: cfg.Lat, Lon: cfg.Lon, Twilight: cfg.Twilight, Dusk: s.Night[0].Unix(), Dawn: s.Night[1].Unix(),
		End: s.End.Unix(), Illum: s.Illum, MoonSep: s.MoonSep, MoonLow: output.MoonFloor(cfg, s),
		MoonUp: moonUp(cfg, s.Night[0], s.Night[1]), Washout: s.Washout(), Targets: len(results), Sky: cfg.ZenithMag(),
	}
	mid := s.Grid[len(s.Grid)/2]
	shown := results[:min(cfg.Top, len(results))]
	doc.Star = output.BrightStars(cfg, s, shown)
	for _, res := range shown {
		ra, dec := astro.Precess(res.RADeg, res.DecDeg, mid)
		curve := make([]float64, 0, len(s.Grid)/Step+1)
		for i := 0; i < len(s.Grid); i += Step {
			if res.Track != nil {
				ra, dec = res.Track[i][0], res.Track[i][1]
			}
			curve = append(curve, math.Round(astro.Altitude(ra, dec, s.Grid[i], cfg.Lat, cfg.Lon)*10)/10)
		}
		doc.Alt = append(doc.Alt, curve)
		// The page links each report row by its rank from these, so narrow
		// tables may drop the RA and DEC columns; J2000, as Aladin and
		// hips2fits expect (a comet's listed position is of date).
		pra, pdec := output.J2000(s, res.Target)
		size := 0.0
		if res.HasSize() {
			size, _ = res.FrameAxes()
		}
		doc.Pos = append(doc.Pos, [3]float64{math.Round(pra*1e4) / 1e4, math.Round(pdec*1e4) / 1e4, size})
		doc.Obs = append(doc.Obs, spans(s, observable(cfg, s, res)))
		doc.Subs = append(doc.Subs, res.Exposure)
		doc.Cards = append(doc.Cards, cardOf(cfg, s, res, ra, dec))
	}
	for _, sl := range slots {
		v := Slot{From: sl.Start.Unix(), To: sl.End.Unix()}
		if r := sl.Result; r != nil {
			v.Name, v.Score, v.Filter = r.Name, sl.Score, r.Filter
			for i := range shown {
				if &results[i] == r {
					v.Rank = i + 1
				}
			}
		}
		doc.Plan = append(doc.Plan, v)
	}

	return doc
}

// observable is res's per-minute observability: its own when scored with
// perMinute (-plan, or main's exposures()), else rescored.
// zone names loc for Intl, which rejects Go's "Local" and "Factory": the
// first is the browser's own zone under js ("" there), the second UTC.
func zone(loc *time.Location) string {
	switch name := loc.String(); name {
	case "Local":
		return ""
	case "Factory":
		return "UTC"
	default:
		return name
	}
}

// moonUp returns the Moon's spans above astro.MoonHorizon from dusk to dawn,
// minute by minute, which the bars span: the grid's s.MoonAlt holds only the
// window -from/-to clip.
func moonUp(cfg *config.Config, dusk, dawn time.Time) [][2]int64 {
	spans := [][2]int64{}
	was := false
	for t := dusk; t.Before(dawn); t = t.Add(time.Minute) {
		ra, dec := astro.MoonTopo(t, cfg.Lat, cfg.Lon)
		up := astro.Altitude(ra, dec, t, cfg.Lat, cfg.Lon) > astro.MoonHorizon
		switch {
		case up && !was:
			spans = append(spans, [2]int64{t.Unix(), dawn.Unix()})
		case !up && was:
			spans[len(spans)-1][1] = t.Unix()
		}
		was = up
	}

	return spans
}

func observable(cfg *config.Config, s *scoring.Sky, res scoring.Result) []bool {
	if res.Obs != nil {
		return res.Obs
	}
	if rs := scoring.Score(cfg, s, []catalog.Target{res.Target}, true); len(rs) == 1 {
		return rs[0].Obs
	}

	return nil
}

// spans turns per-minute observability into [from, to) Unix seconds.
func spans(s *scoring.Sky, obs []bool) [][2]int64 {
	var out [][2]int64
	for i, ok := range obs {
		switch {
		case ok && (i == 0 || !obs[i-1]):
			out = append(out, [2]int64{s.Grid[i].Unix(), s.End.Unix()})
		case !ok && i > 0 && obs[i-1]:
			out[len(out)-1][1] = s.Grid[i].Unix()
		}
	}

	return out
}

// cardOf fills res's card; ra and dec are its position of date, a comet's
// at the last curve sample, so the peak's azimuth is taken from its track.
func cardOf(cfg *config.Config, s *scoring.Sky, res scoring.Result, ra, dec float64) Card {
	c := Card{
		Name: res.Name, Desc: res.Description, Type: res.Type, Con: res.Constellation, Filter: res.Filter,
		Score: res.Score, Foto: res.Foto, SkyMag: res.SkyMag, MaxAlt: res.MaxAlt, MaxAt: res.MaxAt.Unix(),
		RunFrom: res.RunFrom.Unix(), RunTo: res.RunTo.Unix(), Runs: res.Runs,
		Emission: catalog.EmissionLine(res.Target), Comet: res.Track != nil,
	}
	if m := res.Mosaic; m.Panels() > 1 {
		c.Mosaic = strconv.Itoa(m.Cols) + "x" + strconv.Itoa(m.Rows)
	}
	if res.HasSize() {
		c.Size, c.Minor = res.FrameAxes()
		if c.Minor >= c.Size {
			c.Minor = 0
		}
	}
	switch {
	case res.HasMag():
		c.Mag = res.Mag
	case res.BMag > 0:
		c.BMag = res.BMag
	}
	c.SB, c.SBGuess = res.SurfaceBrightness()
	if i := int(res.MaxAt.Sub(s.Grid[0]) / time.Minute); res.Track != nil && i >= 0 && i < len(res.Track) {
		ra, dec = res.Track[i][0], res.Track[i][1]
	}
	_, c.MaxAz = astro.AltAz(ra, dec, res.MaxAt, cfg.Lat, cfg.Lon)
	c.MaxAz = math.Round(c.MaxAz)

	return c
}
