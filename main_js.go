// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

//go:build js

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/dkorunic/astro-recommender/internal/astro"
	"github.com/dkorunic/astro-recommender/internal/catalog"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/geotz"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// init serves web/index.html's time zone lookup: run as argv ["tz", lat, lon]
// it prints the zone and exits before main, so -h and the flags stay those
// of the command line.
func init() { //nolint:gochecknoinits // must run before main, which parses the flags
	if len(os.Args) != 3 || os.Args[0] != "tz" {
		return
	}
	lat, errLat := strconv.ParseFloat(os.Args[1], 64)
	lon, errLon := strconv.ParseFloat(os.Args[2], 64)
	if errLat != nil || errLon != nil || !(lat >= -90 && lat <= 90) || !(lon >= -180 && lon <= 180) {
		os.Exit(2)
	}
	name, err := geotz.Lookup(lat, lon)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(name)
	os.Exit(0)
}

// wxHour is one forecast hour of the page's altitude curves.
type wxHour struct {
	T     int64   `json:"t"`
	Cloud float64 `json:"cloud"`
	Rain  bool    `json:"rain"`
}

// altStep is the spacing of the page's altitude curves, in grid minutes.
const altStep = 5

// card is what a result card shows beyond its curve: the target table's
// values as numbers (0 or "" unknown), times in Unix seconds, and the
// azimuth at the peak for the curve's label.
type card struct { // betteralign:ignore
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

// night is the sidebar's summary of the window: the place, the Moon and the
// night's edges, in Unix seconds.
type night struct { // betteralign:ignore
	Place    string     `json:"place,omitempty"`
	Lat      float64    `json:"lat"`
	Lon      float64    `json:"lon"`
	Twilight string     `json:"twilight"`
	Dusk     int64      `json:"dusk"`
	Dawn     int64      `json:"dawn"`
	End      int64      `json:"end"`
	Illum    float64    `json:"illum"`
	MoonSep  float64    `json:"moonSep"`
	MoonUp   [][2]int64 `json:"moonUp"`
	Washout  bool       `json:"washout,omitempty"`
	Targets  int        `json:"targets"` // results scored, before -n
	Sky      float64    `json:"zenith"`  // zenith mag/arcsec² as configured
}

// altitudes hands web/index.html the altitude curve of each listed result in
// /altitudes, a write-only file only the page's fs shim accepts (Go's js
// syscalls know no fd it did not open): one JSON document with the
// window start and step (Unix seconds), the zone, the altitude limits and,
// in rank order, altitudes to 0.1° every altStep minutes (whole degrees
// would put a flat top's peak at its first sample) over s.Grid,
// computed as scoring does (catalogue positions precessed to mid-window,
// comet tracks as they are), and the [from, to) Unix seconds of each
// observable stretch, rescored per minute unless -plan already did; and
// each result's bright star (STAR's, output.BrightStars); the forecast
// hours within the window, their cloud cover (%) and whether
// it rains (from the weather table's threshold).
func altitudes(cfg *config.Config, s *scoring.Sky, place string, results []scoring.Result) {
	if len(s.Grid) == 0 {
		return
	}
	doc := struct {
		Zone  string              `json:"zone"`
		Alt   [][]float64         `json:"alt"`
		Pos   [][3]float64        `json:"pos"` // per result: J2000 RA and Dec (degrees), size (arcmin, 0 unknown)
		Obs   [][][2]int64        `json:"obs"`
		Subs  []*scoring.Estimate `json:"subs"` // per result, null without one
		Star  []*output.StarInfo  `json:"star"` // per result, STAR's, null without one
		Wx    []wxHour            `json:"wx"`
		Cards []card              `json:"cards"`
		Night night               `json:"night"`
		Start int64               `json:"start"`
		Step  int64               `json:"step"`
		Min   float64             `json:"min"`
		Max   float64             `json:"max"`
	}{Zone: cfg.Loc.String(), Start: s.Grid[0].Unix(), Step: altStep * 60, Min: cfg.AltMin, Max: cfg.AltMax}
	for t := s.Start.Truncate(time.Hour); t.Before(s.End); t = t.Add(time.Hour) {
		if h, ok := s.Weather[t.Unix()]; ok {
			doc.Wx = append(doc.Wx, wxHour{T: t.Unix(), Cloud: math.Round(h.Cloud), Rain: h.Precip >= weather.RainGate/2})
		}
	}
	doc.Night = night{
		Place: place, Lat: cfg.Lat, Lon: cfg.Lon, Twilight: cfg.Twilight, Dusk: s.Night[0].Unix(), Dawn: s.Night[1].Unix(),
		End: s.End.Unix(), Illum: s.Illum, MoonSep: s.MoonSep, MoonUp: [][2]int64{}, Washout: s.Washout(),
		Targets: len(results), Sky: cfg.ZenithMag(),
	}
	for i, a := range s.MoonAlt {
		switch up := a > astro.MoonHorizon; {
		case up && (i == 0 || s.MoonAlt[i-1] <= astro.MoonHorizon):
			doc.Night.MoonUp = append(doc.Night.MoonUp, [2]int64{s.Grid[i].Unix(), s.End.Unix()})
		case !up && i > 0 && s.MoonAlt[i-1] > astro.MoonHorizon:
			doc.Night.MoonUp[len(doc.Night.MoonUp)-1][1] = s.Grid[i].Unix()
		}
	}
	mid := s.Grid[len(s.Grid)/2]
	shown := results[:min(cfg.Top, len(results))]
	doc.Star = output.BrightStars(cfg, s, shown)
	for _, res := range shown {
		ra, dec := astro.Precess(res.RADeg, res.DecDeg, mid)
		curve := make([]float64, 0, len(s.Grid)/altStep+1)
		for i := 0; i < len(s.Grid); i += altStep {
			if res.Track != nil {
				ra, dec = res.Track[i][0], res.Track[i][1]
			}
			curve = append(curve, math.Round(astro.Altitude(ra, dec, s.Grid[i], cfg.Lat, cfg.Lon)*10)/10)
		}
		doc.Alt = append(doc.Alt, curve)
		// sky() links each row by its rank from these, so narrow tables may
		// drop the RA and DEC columns; J2000, as Aladin and hips2fits expect
		// (a comet's listed position is of date).
		pra, pdec := output.J2000(s, res.Target)
		size := 0.0
		if res.HasSize() {
			size, _ = res.FrameAxes()
		}
		doc.Pos = append(doc.Pos, [3]float64{math.Round(pra*1e4) / 1e4, math.Round(pdec*1e4) / 1e4, size})
		obs := res.Obs
		if obs == nil {
			if rs := scoring.Score(cfg, s, []catalog.Target{res.Target}, true); len(rs) == 1 {
				obs = rs[0].Obs
			}
		}
		var spans [][2]int64
		for i, ok := range obs {
			switch {
			case ok && (i == 0 || !obs[i-1]):
				spans = append(spans, [2]int64{s.Grid[i].Unix(), s.End.Unix()})
			case !ok && i > 0 && obs[i-1]:
				spans[len(spans)-1][1] = s.Grid[i].Unix()
			}
		}
		doc.Obs = append(doc.Obs, spans)
		doc.Subs = append(doc.Subs, res.Exposure)
		doc.Cards = append(doc.Cards, cardOf(cfg, s, res, ra, dec))
	}
	if doc.Cards == nil {
		doc.Cards = []card{}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return
	}
	if f, err := os.OpenFile("/altitudes", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
		_, _ = f.Write(b)
		_ = f.Close()
	}
}

// cardOf fills res's card; ra and dec are its position of date, a comet's
// at the last curve sample, so the peak's azimuth is taken from its track.
func cardOf(cfg *config.Config, s *scoring.Sky, res scoring.Result, ra, dec float64) card {
	c := card{
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
