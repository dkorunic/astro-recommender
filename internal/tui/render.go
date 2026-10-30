// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/charmbracelet/x/ansi"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/view"
)

// Layout: the left column's width (dropped below wideAt columns), the
// detail pane's share of the height, and the meta lines under its curve.
const (
	leftW      = 32
	wideAt     = 110
	metaLines  = 3
	minDetailH = 12
	maxDetailH = 20
	picAt      = 90 // the detail pane's inner width from which the picture shows
)

// geometry is the panes' sizes for the window.
type geometry struct {
	left, right, body, list, detail, graph, pic, chart int
}

func (m *model) geometry() geometry {
	var g geometry
	if m.w >= wideAt {
		g.left = leftW
	}
	g.right = m.w - g.left
	g.body = max(0, m.h-1-lipgloss.Height(m.footer()))
	g.detail = min(maxDetailH, max(minDetailH, g.body*45/100))
	if g.body < 2*minDetailH {
		g.detail = g.body / 2
	}
	g.list = g.body - g.detail
	g.graph = max(3, g.detail-3-metaLines) // the border, the title, the meta
	inner := g.right - 4
	if inner >= picAt {
		g.pic = g.graph * 2 // half-blocks: two pixels a cell, about square
	}
	g.chart = inner - g.pic
	if g.pic > 0 {
		g.chart--
	}

	return g
}

func (m *model) picCols() int { return m.geometry().pic }
func (m *model) picRows() int { return m.geometry().graph }

// layout sizes the list and the picture to the window.
func (m *model) layout() tea.Cmd {
	g := m.geometry()
	m.list.SetSize(max(1, g.right-4), max(1, g.list-3))
	if g.pic == 0 {
		return nil
	}
	m.picURL = "" // its size is in the URL

	return tea.Batch(m.pic.SetSize(g.pic, g.graph), m.selectPicture())
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "Astro Recommender"

	return v
}

func (m *model) render() string {
	if m.w == 0 {
		return ""
	}
	g := m.geometry()
	var body string
	switch n := &m.night; {
	case m.loading && n.Day.IsZero():
		body = m.centre(g, m.spin.View()+" Computing the night: forecasts, Moon and every target…")
	case n.Err != nil:
		body = m.centre(g, m.theme.style(m.theme.poor).Render("error: "+n.Err.Error()))
	case n.Msg != "":
		body = m.centre(g, n.Msg+"\n\n"+m.theme.style(m.theme.muted).Render("← and → move to the night before and after."))
	default:
		right := lipgloss.JoinVertical(lipgloss.Left, m.listPane(g), m.detailPane(g))
		if g.left > 0 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, m.leftPane(g), right)
		} else {
			body = right
		}
	}

	// The panes keep to the body's height (the Tonight panel and the full
	// help do not shrink), and the frame to the window's.
	body = lipgloss.NewStyle().MaxHeight(g.body).Render(body)

	return lipgloss.NewStyle().MaxHeight(m.h).MaxWidth(m.w).Render(lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer()))
}

func (m *model) centre(g geometry, s string) string {
	return lipgloss.Place(m.w, g.body, lipgloss.Center, lipgloss.Center, lipgloss.NewStyle().Width(min(m.w, 80)).Render(s))
}

// header is the brand, the night and the setup, the spinner while a night
// computes, and the count of warnings.
func (m *model) header() string {
	t := m.theme
	brand := t.style(t.accent).Bold(true).Render("✦ ASTRO RECOMMENDER")
	parts := []string{}
	// The date first: a narrow terminal cuts the end. Also without a night
	// (none at the site, an error), so ← and → say where they are.
	if !m.night.Day.IsZero() {
		parts = append(parts, m.night.Day.Format("Mon 2 Jan 2006"))
	}
	if d := m.night.Doc; d.Start != 0 && d.Night.Place != "" {
		parts = append(parts, firstParts(d.Night.Place, 2))
	}
	if m.opt.Setup != "" {
		parts = append(parts, m.opt.Setup)
	}
	right := ""
	if m.loading {
		right = m.spin.View() + " computing"
	}
	if n := len(m.warnings); n > 0 {
		right += t.style(t.fair).Render(fmt.Sprintf("  ⚠ %d (w)", n))
	}
	left := brand + "  " + t.style(t.muted).Render(strings.Join(parts, " · "))
	left = ansi.Truncate(left, m.w-lipgloss.Width(right)-1, "…")

	return left + strings.Repeat(" ", max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

func (m *model) footer() string {
	m.help.SetWidth(m.w)

	return m.help.View(keys)
}

// firstParts keeps the first n comma-separated parts of a geocoded name.
func firstParts(s string, n int) string {
	p := strings.Split(s, ", ")

	return strings.Join(p[:min(n, len(p))], ", ")
}

// clock formats Unix seconds as the location's clock.
func (m *model) clock(sec int64) string { return time.Unix(sec, 0).In(m.opt.Loc).Format("15:04") }

// leftPane is the page's left column: Tonight (the Moon and the night),
// Weather (the forecast hours) and, with -plan, the Plan.
func (m *model) leftPane(g geometry) string {
	d := &m.night.Doc
	t := m.theme
	inner := leftW - 4
	muted, strong := t.style(t.muted), t.style(t.strong)
	var b strings.Builder
	moon := "down all night"
	if len(d.Night.MoonUp) > 0 {
		up := make([]string, 0, len(d.Night.MoonUp))
		for _, s := range d.Night.MoonUp {
			up = append(up, m.clock(s[0])+"–"+m.clock(s[1]))
		}
		moon = "up " + strings.Join(up, ", ")
	}
	fmt.Fprintf(&b, "%s %s\n%s\n", t.style(t.strong).Render("●"), t.style(t.accent).Bold(true).Render(fmt.Sprintf("Moon %.0f%% lit", d.Night.Illum*100)), muted.Render(moon))
	b.WriteString(m.nightBar(inner) + "\n")
	dusk, dawn := "dusk "+m.clock(d.Night.Dusk), "dawn "+m.clock(d.Night.Dawn)
	b.WriteString(muted.Render(dusk+strings.Repeat(" ", max(1, inner-len(dusk)-len(dawn)))+dawn) + "\n")
	fact := func(k, v string) { fmt.Fprintf(&b, "%s %s\n", muted.Render(fmt.Sprintf("%-8s", k)), strong.Render(v)) }
	fact("Window", m.clock(d.Start)+"–"+m.clock(d.Night.End))
	fact("Zenith", fmt.Sprintf("%.2f mag/″²", d.Night.Sky))
	fact("Moon ≥", fmt.Sprintf("%.0f° away%s", d.Night.MoonSep, d.Night.MoonLow))
	tonight := t.panel("TONIGHT", strings.TrimRight(b.String(), "\n"), leftW, min(11, g.body))
	rest := g.body - lipgloss.Height(tonight)
	panes := []string{tonight}
	planH := 0
	if len(d.Plan) > 0 {
		planH = min(len(d.Plan)+3, rest/2)
	}
	if len(d.Wx) > 0 && rest-planH >= 4 {
		panes = append(panes, t.panel("WEATHER", m.weather(inner, rest-planH-3), leftW, rest-planH))
	} else {
		planH = rest
	}
	if planH >= 4 {
		panes = append(panes, t.panel("PLAN", m.plan(inner, planH-3), leftW, planH))
	}

	return lipgloss.JoinVertical(lipgloss.Left, panes...)
}

// nightBar is the page's bar from dusk to dawn: the window in the accent,
// the Moon's hours as a light strip under it.
func (m *model) nightBar(w int) string {
	d := &m.night.Doc
	t := m.theme
	span := float64(d.Night.Dawn - d.Night.Dusk)
	at := func(i int) int64 { return d.Night.Dusk + int64(span*(float64(i)+0.5)/float64(w)) }
	var win, moon strings.Builder
	for i := range w {
		x := at(i)
		if x >= d.Start && x < d.Night.End {
			win.WriteString(t.style(t.accent).Render("━"))
		} else {
			win.WriteString(t.style(t.line).Render("━"))
		}
		up := false
		for _, s := range d.Night.MoonUp {
			up = up || x >= s[0] && x < s[1]
		}
		if up {
			moon.WriteString(t.style(t.fg).Render("▔"))
		} else {
			moon.WriteByte(' ')
		}
	}

	return win.String() + "\n" + moon.String()
}

// weather lists the forecast hours as bars of the clear share, by cloud
// cover's colour, rain in blue.
func (m *model) weather(w, rows int) string {
	t := m.theme
	var lines []string
	bw := w - 11
	for _, h := range m.night.Doc.Wx {
		if len(lines) == rows {
			break
		}
		clr := int(math.Round((100 - h.Cloud) / 100 * float64(bw)))
		c, label := t.grade(h.Cloud, 30, 70), fmt.Sprintf("%3.0f%%", h.Cloud)
		if h.Rain {
			c, label = t.rain, "rain"
		}
		lines = append(lines, t.style(t.muted).Render(m.clock(h.T))+" "+t.style(c).Render(strings.Repeat("█", clr))+
			t.style(t.line).Render(strings.Repeat("░", bw-clr))+" "+t.style(t.muted).Render(label))
	}

	return strings.Join(lines, "\n")
}

// plan lists the -plan blocks: their start, target and block score.
func (m *model) plan(w, rows int) string {
	t := m.theme
	var lines []string
	for _, s := range m.night.Doc.Plan {
		if len(lines) == rows {
			break
		}
		name, score := t.style(t.muted).Render("no target"), ""
		if s.Name != "" {
			name = t.style(t.strong).Render(s.Name)
			score = t.style(t.grade(s.Score, 0.66, 0.33)).Render(fmt.Sprintf("%.2f", s.Score))
		}
		line := t.style(t.muted).Render(m.clock(s.From)) + " " + name
		line = ansi.Truncate(line, w-5, "…")
		lines = append(lines, line+strings.Repeat(" ", max(1, w-lipgloss.Width(line)-lipgloss.Width(score)))+score)
	}

	return strings.Join(lines, "\n")
}

// listPane is the targets as cards, titled with the count and the sort.
func (m *model) listPane(g geometry) string {
	d := &m.night.Doc
	t := m.theme
	title := fmt.Sprintf("%d of %d targets · sort: %s", len(d.Cards), d.Night.Targets, sorts[m.sortBy])
	body := m.list.View()
	switch {
	case len(d.Cards) == 0:
		body = t.style(t.muted).Width(g.right - 4).Render(d.Night.Empty)
	case d.Night.Washout:
		body = t.style(t.fair).Render("Washed out: no usable hour tonight (cloud or seeing); listed for reference.") + "\n" + body
	}

	return t.panel(title, body, g.right, g.list)
}

// detailPane is the selected card opened up: its picture and curve, then
// what the page's card shows under the curve; or the warnings (w).
func (m *model) detailPane(g geometry) string {
	t := m.theme
	if m.showWarn {
		body := t.style(t.muted).Render("none")
		if len(m.warnings) > 0 {
			body = t.style(t.fair).Render("• " + strings.Join(m.warnings, "\n• "))
		}

		return t.panel("WARNINGS", body, g.right, g.detail)
	}
	i, ok := m.selected()
	if !ok {
		return t.panel("", "", g.right, g.detail)
	}
	d := &m.night.Doc
	c := d.Cards[i]
	label := fmt.Sprintf("alt %.0f° · %s · %s", c.MaxAlt, compass(c.MaxAz), m.clock(c.MaxAt))
	graph := t.style(t.strong).Render(label) + "\n" + m.chart(i, g.chart, g.graph-1)
	if g.pic > 0 {
		pic := lipgloss.NewStyle().Width(g.pic).Height(g.graph).MaxHeight(g.graph).Render(m.picture())
		graph = lipgloss.JoinHorizontal(lipgloss.Top, pic, " ", graph)
	}
	title := strings.Join(nonEmpty(c.Name, c.Desc), " · ")

	return t.panel(title, graph+"\n"+m.meta(i, g.right-4), g.right, g.detail)
}

// picture is the DSS2 picture, or what stands in its slot. Under the night
// theme half-blocks are reddened, and Kitty's pixels, which no escape here
// can tint, are left out (only their placeholders would draw them).
func (m *model) picture() string {
	switch {
	case m.probed && m.picURL == "":
		return m.theme.style(m.theme.muted).Render("No picture:\nCDS and SkyView\nunreachable")
	case !m.probed:
		return m.theme.style(m.theme.muted).Render("Checking CDS\nand SkyView…")
	case m.red && m.pic.Mode() == picture.PictureKitty:
		return m.theme.style(m.theme.muted).Render("Picture hidden\nin night vision;\ni: blocks")
	case m.red:
		return redden(m.pic.View().Content)
	}

	return m.pic.View().Content
}

// meta is the card's lines under its curve: UP, SKY, FILTER, the type and
// constellation, the sub-exposure estimate and any bright star.
func (m *model) meta(i, w int) string {
	d := &m.night.Doc
	c := d.Cards[i]
	t := m.theme
	k := func(s string) string { return t.style(t.muted).Render(s) + " " }
	line := k("Up") + fmt.Sprintf("%.0f%%", c.Foto*100) + "  " + k("Sky") + t.style(t.grade(c.SkyMag, 20.5, 19)).Render(fmt.Sprintf("%.1f", c.SkyMag))
	if c.Filter != "" {
		line += "  " + k("Filter") + c.Filter
	}
	line += "  " + k("Type") + c.Type
	if c.Con != "" {
		line += "  " + k("Con") + c.Con
	}
	lines := []string{line}
	if e := d.Subs[i]; e != nil {
		lines = append(lines, subsText(e))
	}
	if s := d.Star[i]; s != nil {
		kind := "star"
		if s.Planet {
			kind = "planet"
		}
		lines = append(lines, t.style(t.fair).Render(fmt.Sprintf("☀ bright %s in the frame: %s (V %.1f), %.0f′ from the centre", kind, s.Name, s.Mag, s.Separation)))
	}
	lines = append(lines, t.style(t.muted).Render("o: open in Aladin Lite"))
	for j, l := range lines {
		lines[j] = ansi.Truncate(l, w, "…")
	}

	return strings.Join(lines[:min(metaLines, len(lines))], "\n")
}

// subsText is the page's sub-exposure line (showSubs): the subs per level
// and their time, per panel of a mosaic, led by the nights a good image takes
// past tonight's limit, or never past the stack's ceiling.
func subsText(e *scoring.Estimate) string {
	// SNRPerSub 0: the forecast loses every sub, and the counts are a cap.
	if e.SNRPerSub == 0 {
		return fmt.Sprintf("Subs of %g s: none tonight, the forecast loses them all", e.Sub)
	}
	dur := func(n int) string {
		m := float64(n) * e.Sub / 60
		if m < 60 {
			return fmt.Sprintf("%.0f min", max(1, math.Round(m)))
		}

		return fmt.Sprintf("%.1f h", m/60)
	}
	lvl := func(name string, n int, snr float64) string {
		if e.Unreachable(snr) {
			return name + " never"
		}

		return fmt.Sprintf("%s %d (%s)", name, n, dur(n))
	}
	var lead string
	switch {
	case e.Unreachable(scoring.Levels[1]):
		lead = fmt.Sprintf("Good image: never, the stack stops at SNR %.1f. ", e.SNRLimit)
	case e.Good > e.Max && e.Max <= 0:
		lead = "Good image: no sub fits tonight. "
	case e.Good > e.Max:
		lead = "Good image: " + output.NightsText(e.Good, e.Max) + " like tonight. "
	}
	var qual string
	if e.Panels > 1 {
		qual += fmt.Sprintf(", per panel of %d", e.Panels)
	}
	if e.Assumed {
		qual += ", typical SB"
	}

	return fmt.Sprintf("%sSubs of %g s%s, %d fit tonight: %s · %s · %s", lead, e.Sub, qual, e.Max,
		lvl("modest", e.Modest, scoring.Levels[0]), lvl("good", e.Good, scoring.Levels[1]), lvl("best", e.Best, scoring.Levels[2]))
}

// chart is the altitude curve as the page draws it: 0-90°, the observable
// stretches shaded, the -alt-min/-alt-max limits as faint lines; drawn in
// box-drawing arcs, a solid line every monospaced font holds, where braille
// dots come from fallback fonts of other widths (and freeze's demo shifts).
func (m *model) chart(i, w, h int) string {
	alt := m.night.Doc.Alt[i]
	if len(alt) < 2 || w < 10 || h < 3 {
		return ""
	}
	// View runs on every message, the spinner's ticks and key repeats
	// included, and building and drawing the chart was 40% of a frame's
	// allocations: drawn again only for another curve, size or theme.
	key := chartKey{alt: &alt[0], w: w, h: h, red: m.red}
	if key == m.chartKey {
		return m.chartView
	}
	m.chartKey = key
	m.chartView = m.drawChart(i, w, h)

	return m.chartView
}

// chartKey is what chart's drawing depends on: the curve (its own array, a
// new night's being a new one), the size and the theme.
type chartKey struct {
	alt  *float64
	w, h int
	red  bool
}

// drawChart is chart's drawing.
func (m *model) drawChart(i, w, h int) string {
	d := &m.night.Doc
	t := m.theme
	alt := d.Alt[i]
	at := func(k int) time.Time { return time.Unix(d.Start+int64(k)*d.Step, 0) }
	first, last := at(0), at(len(alt)-1)
	ch := timeserieslinechart.New(w, h,
		timeserieslinechart.WithTimeRange(first, last),
		timeserieslinechart.WithYRange(0, 90),
		timeserieslinechart.WithXYSteps(3, 2),
		timeserieslinechart.WithAxesStyles(t.style(t.line), t.style(t.muted)),
		timeserieslinechart.WithStyle(t.style(t.accent)),
		timeserieslinechart.WithLineStyle(runes.ArcLineStyle),
		timeserieslinechart.WithXLabelFormatter(func(_ int, v float64) string { return m.clock(int64(v)) }),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string { return strconv.Itoa(int(math.Round(v))) + "°" }),
	)
	ch.SetViewYRange(0, 90)
	for k, a := range alt {
		// Clamped as the page's curve: below the horizon the chart would
		// rescale its axis to fit.
		ch.Push(timeserieslinechart.TimePoint{Time: at(k), Value: min(90, max(0, a))})
	}
	for _, lim := range []string{"min", "max"} {
		v := d.Min
		if lim == "max" {
			v = d.Max
		}
		ch.PushDataSet(lim, timeserieslinechart.TimePoint{Time: first, Value: v})
		ch.PushDataSet(lim, timeserieslinechart.TimePoint{Time: last, Value: v})
		ch.SetDataSetStyle(lim, t.style(t.good).Faint(true))
		ch.SetDataSetLineStyle(lim, runes.ThinLineStyle)
	}
	ch.DrawAll()
	shade := lipgloss.NewStyle().Background(t.soft)
	// A column spans (last-first)/plot width; the plot is narrower than w, so
	// stepping by the w-th part reaches every column, where d.Step skipped
	// columns once they outnumbered the samples.
	step := max(1, (last.Unix()-first.Unix())/int64(w))
	for _, s := range d.Obs[i] {
		for x := s[0]; x < s[1]; x += step {
			ch.SetColumnBackgroundStyle(time.Unix(x, 0), shade)
		}
	}

	return ch.View()
}

// compass names an azimuth's point, as the page's peak label.
func compass(az float64) string {
	return [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}[int(math.Round(az/45))%8]
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}

// aladin is the Aladin Lite link at a result's view.Doc position; thumb a
// hips2fits DSS2 picture of it for cols x rows cells at cellW x cellH pixels
// a cell (a high-DPI terminal's: Kitty graphics show them, scaled to the
// real cell; half-blocks take two a cell), the short side half again the
// object, within 0.1-5°, 0.5° for an unknown size: the page's skyURLs.
const survey = "CDS%2FP%2FDSS2%2Fcolor"

func fov(size float64) float64 {
	if size == 0 {
		return 0.5
	}

	return min(5, max(0.1, size*1.5/60))
}

func aladin(pos [3]float64) string {
	return fmt.Sprintf("https://aladin.cds.unistra.fr/AladinLite/?target=%.4f%%20%.4f&fov=%.2f&survey=%s", pos[0], pos[1], fov(pos[2]), survey)
}

const cellW, cellH = 16, 32

const (
	cdsURL    = "https://alaskybis.cds.unistra.fr/"
	userAgent = "astro-recommender (+https://github.com/dkorunic/astro-recommender)"
)

func thumb(pos [3]float64, cols, rows int) string {
	w, h := cols*cellW, rows*cellH
	f := fov(pos[2]) * float64(max(w, h)) / float64(min(w, h))

	return fmt.Sprintf(cdsURL+"hips-image-services/hips2fits?hips=%s&width=%d&height=%d&projection=TAN&fov=%.2f&ra=%.4f&dec=%.4f&format=jpg",
		survey, w, h, f, pos[0], pos[1])
}

// skyview is thumb's field from NASA SkyView's DSS2 Red, for when CDS fails:
// another operator, resampled to the pixels asked like hips2fits, but grey.
func skyview(pos [3]float64, cols, rows int) string {
	w, h := cols*cellW, rows*cellH
	px := fov(pos[2]) / float64(min(w, h)) // degrees a pixel

	return fmt.Sprintf("https://skyview.gsfc.nasa.gov/current/cgi/runquery.pl?Survey=DSS2+Red&Position=%.4f,%.4f&Size=%.3f,%.3f&Pixels=%d,%d&Projection=Tan&Scaling=Linear&Return=JPEG",
		pos[0], pos[1], px*float64(w), px*float64(h), w, h)
}

// item is one card in the list: its index into the view.Doc's slices.
type item struct {
	c *view.Card
	i int
}

func (it item) FilterValue() string {
	return it.c.Name + " " + it.c.Desc + " " + it.c.Type + " " + it.c.Con
}

// delegate draws an item as the page's card in two lines: the rank, name,
// description and type with SCORE at the right; then the size, magnitude,
// surface brightness, constellation and the observable stretch.
type delegate struct{ m *model }

func (delegate) Height() int                         { return 2 }
func (delegate) Spacing() int                        { return 1 }
func (delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, lm list.Model, index int, li list.Item) {
	it, ok := li.(item)
	if !ok {
		return
	}
	m, t, c := d.m, d.m.theme, it.c
	width := lm.Width()
	bar := "  "
	name := t.style(t.strong).Render(c.Name)
	if index == lm.Index() {
		bar = t.style(t.accent).Render("▌ ")
		name = t.style(t.accent).Bold(true).Render(c.Name)
	}
	score := t.style(t.grade(c.Score, 0.66, 0.33)).Bold(true).Render(fmt.Sprintf("%.2f", c.Score))
	line1 := bar + t.style(t.accent).Render(fmt.Sprintf("#%-3d", it.i+1)) + name +
		t.style(t.muted).Render(" · "+strings.Join(nonEmpty(c.Desc, c.Type), " · "))
	line1 = ansi.Truncate(line1, width-lipgloss.Width(score)-1, "…")
	line1 += strings.Repeat(" ", max(1, width-lipgloss.Width(line1)-lipgloss.Width(score))) + score
	var stats []string
	sym := func(s, v string) {
		if v != "" {
			stats = append(stats, t.style(t.muted).Render(s)+" "+v)
		}
	}
	if c.Size > 0 {
		size := strconv.FormatFloat(c.Size, 'f', -1, 64)
		if c.Minor > 0 {
			size += "×" + strconv.FormatFloat(c.Minor, 'f', -1, 64)
		}
		sym("⤢", size+"′")
	}
	switch {
	case c.Mag != 0:
		sym("✦", fmt.Sprintf("%.1f", c.Mag))
	case c.BMag != 0:
		sym("✦", fmt.Sprintf("%.1fB", c.BMag))
	}
	if c.SB > 0 {
		guess := ""
		if c.SBGuess {
			guess = "~"
		}
		sym("◐", fmt.Sprintf("%s%.1f", guess, c.SB))
	}
	sym("✧", c.Con)
	more := ""
	if c.Runs > 1 {
		more = "+"
	}
	sym("▲", m.clock(c.RunFrom))
	sym("◎", m.clock(c.MaxAt)+fmt.Sprintf(" %.0f°", c.MaxAlt))
	sym("▼", m.clock(c.RunTo)+more)
	sym("▦", c.Mosaic)
	if e := m.night.Doc.Subs[it.i]; m.opt.Framing && e != nil {
		_, good := output.SubsText(e)
		sym("SUBS", good)
	}
	line2 := ansi.Truncate("  "+strings.Repeat(" ", 4)+strings.Join(stats, "  "), width, "…")
	if index == lm.Index() {
		line2 = t.style(t.accent).Render("▌") + line2[1:]
	}
	_, _ = io.WriteString(w, line1+"\n"+line2)
}
