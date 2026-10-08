// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"os"
	"slices"
	"time"

	"github.com/dkorunic/astro-recommender/internal/atmos"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/plan"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// The -json report: what the tables show, in machine units (degrees,
// arcminutes, RFC 3339 times in -tz). Unknown values are left out. Field
// order is reading order, not alignment (betteralign:ignore).
type report struct { // betteralign:ignore
	Location location         `json:"location"`
	Window   window           `json:"window"`
	Moon     moon             `json:"moon"`
	Sky      sky              `json:"sky"`
	Frame    *frame           `json:"frame,omitempty"` // with framing only
	Forecast []map[string]any `json:"forecast,omitempty"`
	Plan     []slot           `json:"plan,omitempty"`
	Targets  []target         `json:"targets"`
}

type location struct { // betteralign:ignore
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Place    string  `json:"place,omitempty"`
	TimeZone string  `json:"timeZone"`
}

type window struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Dusk     time.Time `json:"dusk"`
	Dawn     time.Time `json:"dawn"`
	Twilight string    `json:"twilight"`
}

type span struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type moon struct { // betteralign:ignore
	Illumination  float64 `json:"illumination"`
	PhaseAngle    float64 `json:"phaseAngle"`
	MinSeparation float64 `json:"minSeparation"`
	Up            []span  `json:"up"` // when it is above the horizon within the window
}

type sky struct { // betteralign:ignore
	ZenithMag  float64    `json:"zenithMag"`
	Bortle     int        `json:"bortle,omitempty"`
	SQM        float64    `json:"sqm,omitempty"`
	SQMSource  string     `json:"sqmSource,omitempty"`
	Extinction [2]float64 `json:"extinction"` // min and max over the window, mag per airmass
	FilterK    float64    `json:"filterK,omitempty"`
}

type frame struct {
	FOVLong  float64 `json:"fovLong"`  // arcminutes
	FOVShort float64 `json:"fovShort"` // arcminutes
	Scale    float64 `json:"scale"`    // arcseconds per pixel
}

type slot struct { // betteralign:ignore
	span

	Name    string    `json:"name,omitempty"` // empty: no target
	Score   float64   `json:"score,omitempty"`
	PeakAlt float64   `json:"peakAlt,omitempty"`
	PeakAt  time.Time `json:"peakAt,omitzero"`
}

type target struct { // betteralign:ignore
	Rank          int       `json:"rank"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Type          string    `json:"type"`
	Constellation string    `json:"constellation"`
	Comet         bool      `json:"comet,omitempty"`
	RA            float64   `json:"ra"`
	Dec           float64   `json:"dec"`
	Size          *float64  `json:"size,omitempty"`
	Foto          float64   `json:"foto"`
	Score         float64   `json:"score"`
	Frame         float64   `json:"frame"`
	Observable    run       `json:"observable"`
	MaxAlt        float64   `json:"maxAlt"`
	MaxAt         time.Time `json:"maxAt"`
	SkyMag        float64   `json:"skyMag"`
	SB            *float64  `json:"surfaceBrightness,omitempty"`
	SBEstimated   bool      `json:"surfaceBrightnessEstimated,omitempty"`
	Px            *float64  `json:"px,omitempty"`
}

type run struct { // betteralign:ignore
	span

	Runs int `json:"runs"`
}

// JSON prints the whole report as one JSON document instead of the tables.
func JSON(cfg *config.Config, s *scoring.Sky, place string, slots []plan.Slot, results []scoring.Result) error {
	r := report{
		Location: location{Place: place, TimeZone: cfg.Loc.String(), Lat: cfg.Lat, Lon: cfg.Lon},
		Window:   window{Start: s.Start, End: s.End, Dusk: s.Night[0], Dawn: s.Night[1], Twilight: cfg.Twilight},
		Moon:     moon{Up: moonUp(s), Illumination: s.Illum, MinSeparation: s.MoonSep, PhaseAngle: s.MoonPhase},
		Sky: sky{
			ZenithMag: cfg.ZenithMag(), SQM: cfg.SQM, SQMSource: cfg.SQMSource,
			Extinction: [2]float64{slices.Min(s.Ext), slices.Max(s.Ext)},
		},
		Targets: []target{},
	}
	if cfg.SQM != 0 {
		r.Sky.Bortle = atmos.BortleClass(cfg.SQM)
	} else {
		r.Sky.Bortle = cfg.Bortle
	}
	if cfg.Filter {
		r.Sky.FilterK = cfg.FilterK
	}
	if cfg.Framing {
		r.Frame = &frame{FOVLong: cfg.FOVLong, FOVShort: cfg.FOVShort, Scale: cfg.Scale}
	}
	for t := s.Start.Truncate(time.Hour); forecastFetched(s) && t.Before(s.End); t = t.Add(time.Hour) {
		hour := map[string]any{"hour": t.In(cfg.Loc), "extinction": s.ExtinctionAt(t, cfg.Extinction)}
		if h, ok := s.Weather[t.Unix()]; ok {
			hour["cloud"], hour["low"], hour["mid"], hour["high"] = h.Cloud, h.Low, h.Mid, h.High
			hour["dewSpread"], hour["wind"], hour["gust"] = h.Temp-h.DewPoint, h.Wind, h.Gust
			if c := weather.Seeing(h); c > 0 {
				hour["seeingClass"] = c
			}
		}
		if a, ok := s.Astro[weather.AstroKey(t)]; ok {
			hour["transparency"], hour["seeing"] = a.Transparency, weather.SeeingLabel(a.Seeing)
		}
		r.Forecast = append(r.Forecast, hour)
	}
	for _, p := range slots {
		sl := slot{span: span{p.Start, p.End}}
		if p.Result != nil {
			sl.Name, sl.Score, sl.PeakAlt, sl.PeakAt = p.Result.Name, p.Score, p.PeakAlt, p.PeakAt
		}
		r.Plan = append(r.Plan, sl)
	}
	for i, res := range results[:min(cfg.Top, len(results))] {
		ra, dec := res.Position()
		tg := target{
			Rank: i + 1, Name: res.Name, Description: res.Description, Type: res.Type, Constellation: res.Constellation,
			RA: ra, Dec: dec, Foto: res.Foto, Score: res.Score, Frame: res.Frame, MaxAlt: res.MaxAlt, MaxAt: res.MaxAt,
			SkyMag: res.SkyMag, Comet: res.Track != nil,
			Observable: run{Runs: res.Runs, span: span{res.RunFrom, res.RunTo}},
		}
		if res.HasSize() {
			tg.Size = &res.Size
			if cfg.Framing {
				px := res.Size * 60 / cfg.Scale
				tg.Px = &px
			}
		}
		if sb, estimated := res.SurfaceBrightness(); sb > 0 {
			tg.SB, tg.SBEstimated = &sb, estimated
		}
		r.Targets = append(r.Targets, tg)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(r)
}

// moonUp lists the spans within the window during which the Moon is up.
func moonUp(s *scoring.Sky) []span {
	spans := []span{}
	for i, alt := range s.MoonAlt {
		switch up := alt > 0; {
		case up && (i == 0 || s.MoonAlt[i-1] <= 0):
			spans = append(spans, span{From: s.Grid[i], To: s.End})
		case !up && i > 0 && s.MoonAlt[i-1] > 0:
			spans[len(spans)-1].To = s.Grid[i]
		}
	}

	return spans
}
