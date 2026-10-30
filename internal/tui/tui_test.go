// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/NimbleMarkets/ntcharts/v2/picture/pictureurl"
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

// A filter survives a re-sort and a new night: SetItems drops the filtered
// items and refilters through the command resort returns, which the list
// must receive, else it shows no target until the filter text changes.
func TestFilterSurvivesResort(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	m.list.SetFilterText("M 31")
	if n := len(m.list.VisibleItems()); n != 1 {
		t.Fatalf("%d targets match M 31, want 1", n)
	}
	for _, step := range []string{"sort", "night"} {
		var cmd tea.Cmd
		if step == "sort" {
			m.sortBy = (m.sortBy + 1) % len(sorts)
			cmd = m.resort()
		} else {
			m.seq++
			_, cmd = m.Update(nightMsg{night: testNight(m.night.Day.AddDate(0, 0, 1)), seq: m.seq})
		}
		feedFilter(m, cmd)
		if n := len(m.list.VisibleItems()); n != 1 {
			t.Errorf("after the %s: %d targets shown, want 1", step, n)
		}
	}
}

// feedFilter runs cmd and its batches and passes the list's filter results
// back, leaving the other commands (pictures, spinners) unrun.
func feedFilter(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			feedFilter(m, c)
		}
	case list.FilterMatchesMsg:
		m.Update(msg)
	}
}

// The observable stretch is shaded in every column of a wide chart, not in
// one column per sample.
func TestChartShadingContinuous(t *testing.T) {
	m := started(t, 200, 50, new([]time.Time))
	r, g, b, _ := m.theme.soft.RGBA()
	bg := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
	for _, w := range []int{60, 120, 200} {
		// M 31 is observable the whole night: every plot cell of a row
		// above its curve is shaded, the axis and the last instant aside.
		var row string
		for l := range strings.SplitSeq(m.chart(0, w, 8), "\n") {
			if strings.Contains(l, "│") {
				row = l

				break
			}
		}
		axis := []rune(ansi.Strip(row))
		plot := len(axis) - slices.Index(axis, '│') - 1
		if n := strings.Count(row, bg); n < plot-1 {
			t.Errorf("width %d: %d of %d cells shaded", w, n, plot)
		}
	}
}

// manyNight is testNight with n copies of M 31, "T 0" to "T n-1", peaking
// in reverse order, so the peak sort reverses the list.
func manyNight(day time.Time, n int) Night {
	night := testNight(day)
	d := &night.Doc
	one := *d
	d.Alt, d.Pos, d.Obs, d.Subs, d.Star, d.Cards = nil, nil, nil, nil, nil, nil
	for i := range n {
		d.Alt = append(d.Alt, one.Alt[0])
		d.Pos = append(d.Pos, one.Pos[0])
		d.Obs = append(d.Obs, one.Obs[0])
		d.Subs = append(d.Subs, nil)
		d.Star = append(d.Star, nil)
		c := one.Cards[0]
		c.Name, c.MaxAt = fmt.Sprintf("T %d", i), d.Start+int64(n-i)*60
		d.Cards = append(d.Cards, c)
	}

	return night
}

// A sort or a new night under a filter keeps the selected target among the
// matches: Select takes an index into them, and the one in the whole list
// put the cursor past their end, which panicked the next render.
func TestFilterResortKeepsTarget(t *testing.T) {
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC},
		func(_ context.Context, d time.Time, _ bool) Night { return manyNight(d, 40) })
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.Update(m.load(day, false)())
	m.list.SetFilterText("T 3")
	vis := m.list.VisibleItems()
	m.list.Select(len(vis) - 1)
	want := vis[len(vis)-1].(item).c.Name //nolint:forcetypeassert // the list holds items only
	for _, step := range []string{"sort", "night"} {
		var cmd tea.Cmd
		if step == "sort" {
			cmd = press(m, "s")
		} else {
			_, cmd = m.Update(press(m, "]")()) // load's nightMsg
		}
		feedFilter(m, cmd)
		_ = screen(m) // panicked
		got, ok := m.list.SelectedItem().(item)
		if !ok || got.c.Name != want {
			t.Errorf("after the %s: %v selected, want %s", step, got.c, want)
		}
	}
}

// The sub line words what the counts cannot say, as the page's showSubs: a
// night the forecast loses, a level past the stack's ceiling, no sub, many
// nights; per panel of a mosaic and with a typical SB.
func TestSubsText(t *testing.T) {
	ok := scoring.Estimate{Modest: 10, Good: 40, Best: 160, Max: 100, Sub: 10, SNRPerSub: 1, Panels: 1}
	for want, e := range map[string]scoring.Estimate{
		"none tonight, the forecast loses them all":           {Modest: 1e9, Good: 1e9, Best: 1e9, Max: 2874, Sub: 10, Panels: 1},
		"Good image: never, the stack stops at SNR 8.0":       {Modest: 50, Good: 1e9, Best: 1e9, Max: 2874, Sub: 10, SNRPerSub: 1, SNRLimit: 8, Panels: 1},
		"Good image: no sub fits tonight":                     {Modest: 50, Good: 200, Best: 800, Sub: 10, SNRPerSub: 1, Panels: 1},
		"Good image: >99 nights like tonight":                 {Modest: 50, Good: 1e6, Best: 4e6, Max: 100, Sub: 10, SNRPerSub: 1, Panels: 1},
		"Good image: 2.5 nights like tonight":                 {Modest: 50, Good: 250, Best: 1000, Max: 100, Sub: 10, SNRPerSub: 1, Panels: 1},
		"Subs of 10 s, per panel of 4, typical SB, 100 fit":   {Modest: 10, Good: 40, Best: 160, Max: 100, Sub: 10, SNRPerSub: 1, Panels: 4, Assumed: true},
		"Subs of 10 s, 100 fit tonight: modest 10 (2 min) · ": ok,
	} {
		if got := subsText(&e); !strings.Contains(got, want) || strings.Contains(got, "1000000000") {
			t.Errorf("%+v: %q, want %q in it", e, got, want)
		}
	}
}

// The card's SUBS is the table's cell, and a mosaic shows its grid.
func TestCardSubsMosaic(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	m.night.Doc.Subs[0] = &scoring.Estimate{Modest: 1e9, Good: 1e9, Best: 1e9, Max: 2874, Sub: 10, Panels: 4}
	m.night.Doc.Cards[0].Mosaic = "2x2"
	m.resort()
	s := screen(m)
	if !strings.Contains(s, "SUBS >99 nights") || strings.Contains(s, "347947") || !strings.Contains(s, "▦ 2x2") {
		t.Errorf("card without SUBS >99 nights or ▦ 2x2:\n%s", s)
	}
}

// Night vision paints everything red: the filter prompt and cursor, the
// help footer, the pagination dots and the spinner, not bubbles' yellow,
// pink and greys; every truecolor on the screen has red ahead of green and
// blue, and no 256-colour code is left.
func TestNightThemeAllRed(t *testing.T) {
	m := started(t, 160, 30, new([]time.Time))
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.night = manyNight(m.night.Day, 40) // paginated: the dots show
	m.resort()
	press(m, "r")
	press(m, "?")
	press(m, "/")
	m.loading = true // the spinner
	s := m.render()
	if strings.Contains(s, "\x1b[38;5;") || strings.Contains(s, "\x1b[48;5;") {
		t.Error("a 256-colour code under the night theme")
	}
	for _, c := range truecolor.FindAllStringSubmatch(s, -1) {
		r, _ := strconv.Atoi(c[2])
		g, _ := strconv.Atoi(c[3])
		b, _ := strconv.Atoi(c[4])
		if 2*g > r || 2*b > r {
			t.Errorf("colour %d,%d,%d under the night theme", r, g, b)
		}
	}
}

// redden keeps each half-block cell's brightness as a dim red.
func TestRedden(t *testing.T) {
	got := redden("\x1b[38;2;255;255;255;48;2;0;0;255m▀\x1b[48;2;0;0;0m")
	if want := "\x1b[38;2;140;16;0;48;2;10;1;0m▀\x1b[48;2;0;0;0m"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

// t asks for tonight as of now, not as of the launch: the view may have been
// open for hours, and a night in progress is clipped at now. Now is in -tz's
// zone, not the machine's, since Window takes the date from it.
func TestTonightIsNow(t *testing.T) {
	var asked []time.Time
	m := started(t, 160, 48, &asked)
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skip(err)
	}
	m.opt.Loc = auckland
	later := m.opt.Day.Add(9 * time.Hour) // 21:00 UTC: Auckland's next day
	m.now = func() time.Time { return later }
	m.Update(press(m, "t")())
	got := asked[len(asked)-1]
	if !got.Equal(later) {
		t.Errorf("t asked for %v, want now's %v", got, later)
	}
	if got.Location() != auckland {
		t.Errorf("t asked in %v, want -tz's %v", got.Location(), auckland)
	}
}

// → pressed while a night computes adds up from the night last asked for,
// and each new compute cancels the one it supersedes.
func TestStepsAddUp(t *testing.T) {
	var ctxs []context.Context
	var asked []time.Time
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC},
		func(ctx context.Context, d time.Time, _ bool) Night {
			ctxs, asked = append(ctxs, ctx), append(asked, d)

			return testNight(d)
		})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.Update(m.load(day, false)())
	var cmds []tea.Cmd
	for range 3 {
		cmds = append(cmds, press(m, "]"))
	}
	for _, c := range cmds {
		m.Update(c())
	}
	if want := day.AddDate(0, 0, 3); !m.night.Day.Equal(want) || !asked[len(asked)-1].Equal(want) {
		t.Errorf("three → shown %v, asked %v, want %v", m.night.Day, asked, want)
	}
	for i, ctx := range ctxs[1 : len(ctxs)-1] {
		if ctx.Err() == nil {
			t.Errorf("superseded compute %d not cancelled", i+1)
		}
	}
}

// With -date the night's date is known while it computes, at launch and
// after t: ← and → step from it rather than wait.
func TestStepWhileDateLoads(t *testing.T) {
	var asked []time.Time
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC, DateSet: true},
		func(_ context.Context, d time.Time, _ bool) Night {
			asked = append(asked, d)

			return testNight(d)
		})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.load(day, true) // the launch's, still computing
	cmd := press(m, "[")
	if cmd == nil {
		t.Fatal("← while the -date night computes did nothing")
	}
	m.Update(cmd())
	m.Update(press(m, "]")()) // back on the given date
	m.Update(press(m, "]")())
	press(m, "t") // the given date again, computing
	if cmd = press(m, "]"); cmd == nil {
		t.Fatal("→ while t's -date night computes did nothing")
	}
	cmd()
	want := []time.Time{day.AddDate(0, 0, -1), day, day.AddDate(0, 0, 1), day.AddDate(0, 0, 1)}
	if !slices.EqualFunc(asked, want, time.Time.Equal) {
		t.Errorf("asked %v, want %v", asked, want)
	}
}

// While t's night computes, ← and → wait: its date is Tonight's to pick, and
// a step from the night shown would land elsewhere.
func TestStepAfterTonight(t *testing.T) {
	var asked []time.Time
	m := started(t, 160, 48, &asked)
	m.Update(press(m, "]")())
	m.Update(press(m, "]")())
	tonight := press(m, "t")
	if cmd := press(m, "]"); cmd != nil {
		t.Fatal("→ while t computes stepped from the night shown")
	}
	m.Update(tonight())
	want := m.night.Day.AddDate(0, 0, 1)
	m.Update(press(m, "]")())
	if !asked[len(asked)-1].Equal(want) {
		t.Errorf("→ after t asked %v, want %v", asked[len(asked)-1], want)
	}
}

// A night with nothing to show still names its date in the header.
func TestHeaderDateWithoutNight(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	day := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	m.night = Night{Day: day, Msg: "No astronomical night at this location tonight."}
	if s := screen(m); !strings.Contains(s, "Sun 21 Jun 2026") {
		t.Errorf("no date in the header:\n%s", s)
	}
}

// The frame never runs past the window, help open or not.
func TestFrameFitsWindow(t *testing.T) {
	for _, wh := range [][2]int{{110, 14}, {110, 20}, {109, 12}, {120, 16}, {60, 10}, {200, 24}} {
		m := started(t, wh[0], wh[1], new([]time.Time))
		for _, k := range []string{"", "?"} {
			if k != "" {
				press(m, k)
			}
			lines := strings.Split(screen(m), "\n")
			wide := 0
			for _, l := range lines {
				wide = max(wide, ansi.StringWidth(l))
			}
			if len(lines) > wh[1] || wide > wh[0] {
				t.Errorf("%dx%d (%q): %d lines, %d columns", wh[0], wh[1], k, len(lines), wide)
			}
			// The panes give way, not the keys' help at the bottom.
			if !strings.Contains(strings.Join(lines[max(0, len(lines)-5):], "\n"), "↑/") { // both helps lead with ↑
				t.Errorf("%dx%d (%q): the help is cut off", wh[0], wh[1], k)
			}
		}
	}
}

// i before the Kitty probe answers toggles nothing, so it is no choice: the
// switch to Kitty on the probe's answer still comes.
func TestPictureKeyBeforeProbe(t *testing.T) {
	if picture.KittySupported() == picture.KittyCapabilitySupported {
		t.Skip("Kitty already confirmed")
	}
	m := started(t, 160, 48, new([]time.Time))
	press(m, "i")
	if m.picked {
		t.Error("i before the probe counted as a choice")
	}
}

// A filter's matches from an older night (its command run late) are not
// shown against this night's cards, whose slices their indices would overrun.
func TestStaleFilterMatches(t *testing.T) {
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC},
		func(_ context.Context, d time.Time, _ bool) Night { return manyNight(d, 40) })
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.Update(m.load(day, false)())
	m.list.SetFilterText("T 3")
	old := m.resort() // the 40-card night's refilter, run only later
	m.compute = func(_ context.Context, d time.Time, _ bool) Night { return testNight(d) }
	_, cmd := m.Update(press(m, "]")())
	feedFilter(m, cmd)
	feedFilter(m, old)
	_ = screen(m) // overran the two cards' slices
	cards := m.night.Doc.Cards
	for _, v := range m.list.VisibleItems() {
		if it := v.(item); it.i >= len(cards) || it.c != &cards[it.i] { //nolint:forcetypeassert // the list holds items only
			t.Errorf("an older night's item shown: %+v", it.c)
		}
	}
}

// A stderr line past bufio.Scanner's 64 KiB neither stops the warnings nor
// blocks the compute writing them.
func TestRunLongStderrLine(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })
	echoed := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); echoed <- b }() // Run echoes the lines on exit
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ran := make(chan error, 1)
	go func() {
		ran <- Run(context.Background(), Options{Day: day, Loc: time.UTC, Snapshot: t.TempDir() + "/frame", Width: 80, Height: 24},
			func(context.Context, time.Time, bool) Night {
				fmt.Fprintln(os.Stderr, strings.Repeat("x", 200<<10))
				fmt.Fprintln(os.Stderr, "warning: after")

				return Night{Day: day, Msg: "nothing"}
			})
		_ = w.Close()
	}()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run blocked")
	}
	if out := <-echoed; !strings.Contains(string(out), "warning: after") {
		t.Errorf("the line after the long one lost: %d bytes echoed", len(out))
	}
}

// flaky fails every request to a host in down and counts each host's.
type flaky struct {
	down  map[string]bool
	asked map[string]int
	mu    sync.Mutex
}

func (f *flaky) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked == nil {
		f.asked = map[string]int{}
	}
	f.asked[req.URL.Host]++
	if f.down[req.URL.Host] {
		return nil, errors.New(req.URL.Host + " down")
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 4)), nil)

	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(&b), Request: req}, nil
}

// runPicture runs cmd and the picture's messages it leads to.
func runPicture(m *model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 8 {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runPicture(m, c, depth+1)
		}
	default:
		if pictureurl.IsPictureMsg(msg) {
			runPicture(m, m.pictureMsg(msg), depth+1)
		}
	}
}

const cdsHost, skyHost = "alaskybis.cds.unistra.fr", "skyview.gsfc.nasa.gov"

// withServices gives m a picture fetched through tr and the probe's answer.
func withServices(m *model, tr *flaky, up servicesMsg) {
	m.pic = pictureurl.NewWithConfig(pictureurl.Config{HTTPClient: &http.Client{Transport: tr}, Fit: picture.FitContain})
	m.picURL = ""
	_, cmd := m.Update(up)
	runPicture(m, cmd, 0)
}

// No picture is asked for before the probe says which service answers.
func TestPictureWaitsForProbe(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	if m.selectPicture() != nil || m.picURL != "" {
		t.Errorf("picture asked for before the probe: %q", m.picURL)
	}
	if !strings.Contains(screen(m), "Checking CDS") {
		t.Error("the picture's slot does not say the services are being checked")
	}
}

// CDS failing mid-session: the target's picture comes from SkyView, and so
// do the next targets', without asking CDS again.
func TestPictureFallback(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	tr := &flaky{down: map[string]bool{cdsHost: true}}
	withServices(m, tr, servicesMsg{cds: true, sky: true})
	if m.pic.State() != pictureurl.StateLoaded || !strings.Contains(m.pic.CurrentURL(), skyHost) {
		t.Fatalf("state %v from %s, want loaded from SkyView", m.pic.State(), m.pic.CurrentURL())
	}
	m.list.Select(1)
	runPicture(m, m.selectPicture(), 0)
	if tr.asked[cdsHost] != 1 || tr.asked[skyHost] != 2 {
		t.Errorf("asked %v, want CDS once and SkyView for both targets", tr.asked)
	}
}

// With neither service answering the probe nothing is fetched, and the slot
// says why.
func TestPictureNoService(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	tr := &flaky{}
	withServices(m, tr, servicesMsg{})
	m.list.Select(1)
	runPicture(m, m.selectPicture(), 0)
	if len(tr.asked) != 0 {
		t.Errorf("asked %v with no service up", tr.asked)
	}
	if !strings.Contains(screen(m), "unreachable") {
		t.Error("the picture's slot does not say the services are unreachable")
	}
}

// The probe tells a service that answers with a picture from one that fails.
func TestProbe(t *testing.T) {
	tr := &flaky{down: map[string]bool{cdsHost: true}}
	if up := probe(context.Background(), &http.Client{Transport: tr})(); up != (servicesMsg{sky: true}) {
		t.Errorf("probe %+v, want SkyView alone", up)
	}
}

// A compute still warning after the view quits (cancelled, its packages
// unaware) is waited for: its lines reach the echo on exit, and os.Stderr is
// not swapped back under it.
func TestRunWaitsForCompute(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })
	echoed := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); echoed <- b }()
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	ran := make(chan error, 1)
	go func() {
		ran <- Run(ctx, Options{Day: day, Loc: time.UTC, Snapshot: t.TempDir() + "/frame", Width: 80, Height: 24},
			func(ctx context.Context, _ time.Time, _ bool) Night {
				close(started)
				<-ctx.Done()
				time.Sleep(100 * time.Millisecond)
				fmt.Fprintln(os.Stderr, "warning: using cached CometEls.txt: context canceled")
				fmt.Fprintln(os.Stderr, "warning: late")

				return Night{Day: day, Err: ctx.Err()}
			})
		_ = w.Close()
	}()
	<-started
	cancel() // the quit, while the compute runs
	select {
	case <-ran:
	case <-time.After(20 * time.Second):
		t.Fatal("Run blocked")
	}
	if out := <-echoed; !strings.Contains(string(out), "warning: late") {
		t.Errorf("the cancelled compute's warning lost: %q", out)
	} else if strings.Contains(string(out), "context canceled") {
		t.Errorf("a cancelled fetch's warning echoed: %q", out)
	}
}

// An empty list names the limits output.EmptyText found in force, not a
// fixed few.
func TestEmptyNamesLimits(t *testing.T) {
	day := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), Options{Day: day, Loc: time.UTC}, func(_ context.Context, d time.Time, _ bool) Night {
		n := manyNight(d, 0)
		n.Doc.Night.Empty = "No objects within the limits (runs of 2h). Loosen -min-run, or try another -list."

		return n
	})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m.Update(m.load(day, false)())
	if got := screen(m); !strings.Contains(got, "-min-run") {
		t.Errorf("empty list does not name -min-run:\n%s", got)
	}
}

// Both services down are probed again before the next picture once a minute
// has passed, not before, and a picture follows once one answers.
func TestPictureReprobe(t *testing.T) {
	m := started(t, 160, 48, new([]time.Time))
	tr := &flaky{down: map[string]bool{cdsHost: true, skyHost: true}}
	m.client = &http.Client{Transport: tr}
	withServices(m, tr, servicesMsg{})
	clock := m.probedAt
	m.now = func() time.Time { return clock }
	m.list.Select(1)
	if cmd := m.selectPicture(); cmd != nil {
		t.Fatal("probed again within the minute")
	}
	clock = clock.Add(reprobeAfter)
	delete(tr.down, skyHost)
	cmd := m.selectPicture()
	if cmd == nil || !m.probing {
		t.Fatal("not probed again after a minute")
	}
	if m.selectPicture() != nil {
		t.Error("a second probe while one is out")
	}
	_, cmd = m.Update(cmd())
	runPicture(m, cmd, 0)
	if m.pic.State() != pictureurl.StateLoaded || !strings.Contains(m.pic.CurrentURL(), skyHost) {
		t.Errorf("state %v from %s, want loaded from SkyView", m.pic.State(), m.pic.CurrentURL())
	}
}

// The chart is drawn once per curve, size and theme, and again when any of
// them changes: the next card, the night theme, a resize, a new night.
func TestChartCache(t *testing.T) {
	var asked []time.Time
	m := started(t, 180, 50, &asked)
	m.render()
	first := m.chartKey
	if first.alt == nil {
		t.Fatal("no chart drawn")
	}
	m.chartView = "cached"
	if !strings.Contains(m.render(), "cached") {
		t.Fatal("unchanged chart drawn again")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.render(); m.chartKey.alt == first.alt {
		t.Error("the next card kept the first's chart")
	}
	press(m, "r")
	if m.render(); !m.chartKey.red {
		t.Error("the night theme kept the dark chart")
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	if m.render(); m.chartKey.w == first.w {
		t.Error("a resize kept the chart's width")
	}
	before := m.chartKey.alt
	m.Update(m.load(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), true)())
	if m.render(); m.chartKey.alt == before {
		t.Error("a new night kept the old night's chart")
	}
}

// Whatever was pressed, a frame with the chart cached is the frame drawn
// afresh.
func TestChartCacheFrames(t *testing.T) {
	var asked []time.Time
	m := started(t, 180, 50, &asked)
	m.night = manyNight(m.night.Day, 12)
	steps := []func(){
		func() {},
		func() { m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) },
		func() { press(m, "s") },
		func() { press(m, "r") },
		func() { m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) },
		func() { m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) },
		func() { press(m, "r") },
		func() { m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) },
		func() { m.Update(tea.WindowSizeMsg{Width: 200, Height: 60}) },
	}
	for i, step := range steps {
		step()
		m.render() // fills the cache
		cached := m.render()
		m.chartKey = chartKey{}
		if fresh := m.render(); cached != fresh {
			t.Errorf("step %d: the cached frame differs from one drawn afresh", i)
		}
	}
}
