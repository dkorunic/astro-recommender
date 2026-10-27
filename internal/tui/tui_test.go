// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/charmbracelet/x/ansi"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/view"
)

// testNight is a two-target night from 18:30 to 04:30 UTC, with a plan and
// forecast hours.
func testNight(day time.Time) Night {
	start := time.Date(day.Year(), day.Month(), day.Day(), 18, 30, 0, 0, time.UTC)
	n := 121 // samples, Step minutes apart: ten hours
	curve := func(peak float64, at int) []float64 {
		c := make([]float64, n)
		for k := range c {
			c[k] = max(0, peak-float64((k-at)*(k-at))/40)
		}

		return c
	}
	end := start.Add(10 * time.Hour).Unix()
	doc := view.Doc{
		Zone: "UTC", Start: start.Unix(), Step: view.Step * 60, Min: 30, Max: 80,
		Alt:  [][]float64{curve(74, 40), curve(55, 90)},
		Pos:  [][3]float64{{10.68, 41.27, 190}, {83.82, -5.39, 0}},
		Obs:  [][][2]int64{{{start.Unix(), end}}, {{start.Add(5 * time.Hour).Unix(), end}}},
		Subs: []*scoring.Estimate{{Modest: 105, Good: 420, Best: 1690, Max: 2874, Sub: 10, SNRPerSub: 0.5, Panels: 1}, nil},
		Star: make([]*output.StarInfo, 2),
		Cards: []view.Card{
			{Name: "M 31", Desc: "Andromeda Galaxy", Type: "Galaxy", Con: "Andromeda", Score: 0.71, Foto: 1, Size: 190, Minor: 60, Mag: 3.4, SB: 22.3, SkyMag: 21.2, MaxAlt: 74, MaxAz: 180, MaxAt: start.Add(200 * time.Minute).Unix(), RunFrom: start.Unix(), RunTo: end, Runs: 1},
			{Name: "M 42", Desc: "Orion Nebula", Type: "HII Region", Con: "Orion", Filter: "dual", Score: 0.22, Foto: 0.5, Size: 85, Mag: 4, SB: 20.1, SBGuess: true, SkyMag: 19.2, MaxAlt: 55, MaxAz: 175, MaxAt: start.Add(450 * time.Minute).Unix(), RunFrom: start.Add(5 * time.Hour).Unix(), RunTo: end, Runs: 2},
		},
		Plan: []view.Slot{{From: start.Unix(), To: start.Add(5 * time.Hour).Unix(), Name: "M 31", Rank: 1, Score: 0.7}, {From: start.Add(5 * time.Hour).Unix(), To: end}},
		Wx:   []view.WxHour{{T: start.Truncate(time.Hour).Unix(), Cloud: 10}, {T: start.Add(time.Hour).Truncate(time.Hour).Unix(), Cloud: 80, Rain: true}},
		Night: view.Night{
			Place: "Zagreb, Croatia", Twilight: "astronomical", Dusk: start.Unix(), Dawn: end, End: end,
			Illum: 0.42, MoonSep: 42, MoonUp: [][2]int64{{start.Unix(), start.Add(3 * time.Hour).Unix()}}, Targets: 40, Sky: 21.5,
		},
	}

	return Night{Day: day, Doc: doc}
}

// started returns a model sized w x h with its first night computed by a
// fake, which records the days it was asked for.
func started(t *testing.T, w, h int, asked *[]time.Time) *model {
	t.Helper()
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC, Setup: "GaryImm", Framing: true},
		func(_ context.Context, d time.Time, _ bool) Night {
			*asked = append(*asked, d)

			return testNight(d)
		})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(m.load(day, false)())

	return m
}

func screen(m *model) string { return ansi.Strip(m.render()) }

func press(m *model, k string) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Text: k, Code: []rune(k)[0]})

	return cmd
}

func TestView(t *testing.T) {
	var asked []time.Time
	m := started(t, 140, 44, &asked)
	s := screen(m)
	if os.Getenv("TUI_SHOW") != "" {
		t.Log("\n" + m.render())
	}
	for _, want := range []string{"ASTRO RECOMMENDER", "TONIGHT", "Moon 42% lit", "WEATHER", "PLAN", "2 of 40 targets", "#1  M 31", "0.71", "alt 74° · S · 21:50", "good 420 (1.2 h)", "Andromeda Galaxy"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w > 140 {
			t.Errorf("line %d is %d wide: %q", i, w, l)
		}
	}
	if n := len(strings.Split(s, "\n")); n > 44 {
		t.Errorf("%d lines for a 44-line window", n)
	}
	// Narrow: no left column, no picture, still within the width.
	m = started(t, 80, 30, &asked)
	s = screen(m)
	if strings.Contains(s, "TONIGHT") {
		t.Error("80 columns keep the left column")
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("narrow line %d is %d wide: %q", i, w, l)
		}
	}
}

func TestKeys(t *testing.T) {
	var asked []time.Time
	m := started(t, 140, 44, &asked)
	// s: sort by peak puts M 31 (peak 21:50) before M 42 still; twice
	// (altitude) too; three times (size): M 31 first; four (name) too.
	press(m, "s")
	if got := sorts[m.sortBy]; got != "peak" {
		t.Fatalf("sort %q, want peak", got)
	}
	press(m, "s")
	press(m, "s")
	press(m, "s") // name
	if it := m.list.Items()[0].(item); it.c.Name != "M 31" {
		t.Errorf("by name first %q", it.c.Name)
	}
	// → asks for the night after the shown one, ← the one before.
	cmd := press(m, "]")
	if cmd == nil {
		t.Fatal("] computes nothing")
	}
	m.Update(nightMsg{night: testNight(asked[0].AddDate(0, 0, 1)), seq: m.seq}) // what the command returns
	if got := m.night.Day.Format(time.DateOnly); got != "2026-10-28" {
		t.Errorf("after ]: %s", got)
	}
	// A stale night (an older seq) is dropped.
	old := m.seq
	press(m, "[")
	m.Update(nightMsg{night: testNight(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), seq: old})
	if got := m.night.Day.Format(time.DateOnly); got != "2026-10-28" {
		t.Errorf("a stale night replaced the shown one: %s", got)
	}
	// r: the red theme; w: the warnings pane.
	press(m, "r")
	if !m.red {
		t.Error("r keeps the dark theme")
	}
	m.Update(warnMsg("warning: no weather forecast: test"))
	press(m, "w")
	if s := screen(m); !strings.Contains(s, "no weather forecast: test") {
		t.Errorf("warnings pane lacks the warning:\n%s", s)
	}
}

func TestNothingToPlan(t *testing.T) {
	m := newModel(context.Background(), Options{Day: time.Now(), Loc: time.UTC}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(nightMsg{night: Night{Day: time.Now(), Msg: "No astronomical night at this location tonight."}, seq: m.seq})
	if s := screen(m); !strings.Contains(s, "No astronomical night") || !strings.Contains(s, "night before and after") {
		t.Errorf("screen:\n%s", s)
	}
}

// Once the terminal confirms Kitty graphics the picture switches to them,
// unless i already chose; i switches back.
func TestKittyAuto(t *testing.T) {
	var asked []time.Time
	picture.ForceKittyCapability(picture.KittyCapabilitySupported)
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnknown) })
	m := started(t, 140, 44, &asked)
	if m.pic.Mode() != picture.PictureKitty {
		t.Fatalf("mode %v, want Kitty once supported", m.pic.Mode())
	}
	press(m, "i")
	press(m, "j")
	if m.pic.Mode() != picture.PictureGlyph {
		t.Errorf("mode %v after i, want half-blocks kept", m.pic.Mode())
	}
}
