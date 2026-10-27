// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// theme is the web page's palette (web/index.html): Telescopius' grey on
// near-black with one salmon accent, or the red night-vision one, dim red
// with nothing blue or white to spoil dark adaptation.
type theme struct {
	fg, strong, muted, line, accent, soft color.Color
	good, fair, poor, rain                color.Color
}

var (
	darkTheme = theme{
		fg: lipgloss.Color("#cccccc"), strong: lipgloss.Color("#eeeeee"), muted: lipgloss.Color("#8a8a8a"),
		line: lipgloss.Color("#3a3a3a"), accent: lipgloss.Color("#ee7777"), soft: lipgloss.Color("#3a2222"),
		good: lipgloss.Color("#6fd28a"), fair: lipgloss.Color("#e9b44c"), poor: lipgloss.Color("#ff7b72"),
		rain: lipgloss.Color("#6cb4f5"),
	}
	nightTheme = theme{
		fg: lipgloss.Color("#c8281d"), strong: lipgloss.Color("#e03a2c"), muted: lipgloss.Color("#7a1810"),
		line: lipgloss.Color("#2e0805"), accent: lipgloss.Color("#d93225"), soft: lipgloss.Color("#1a0302"),
		good: lipgloss.Color("#ff4d3d"), fair: lipgloss.Color("#d02e20"), poor: lipgloss.Color("#ab281d"),
		rain: lipgloss.Color("#a52418"),
	}
)

// grade is the report's colour for a value: good, fair or poor; okAt and
// badAt may be in either order (higher or lower is better).
func (t theme) grade(v, okAt, badAt float64) color.Color {
	sign := 1.0
	if okAt < badAt {
		sign = -1
	}
	switch {
	case sign*v >= sign*okAt:
		return t.good
	case sign*v <= sign*badAt:
		return t.poor
	}

	return t.fair
}

func (t theme) style(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// panel draws a bordered box w x h cells (border included) under its title,
// marked in the accent as the page's panel headers (the sections' upper
// case, a target's name as written).
func (t theme) panel(title, body string, w, h int) string {
	head := t.style(t.accent).Bold(true).Render("▍") + t.style(t.strong).Bold(true).Render(title)

	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.line).Padding(0, 1).
		Width(w).Height(h).MaxHeight(h).Render(head + "\n" + body)
}

// applyTheme styles what the palette does not reach by itself, bubbles'
// defaults otherwise: the list's filter prompt (yellow), cursor (pink),
// text, matches, pagination dots and empty line, the help footer's greys and
// the spinner. The list copies its filter input's and dots' styles when made,
// so they are set on those too.
func (m *model) applyTheme() {
	t := m.theme
	s := list.DefaultStyles(true)
	field := textinput.StyleState{Text: t.style(t.fg), Placeholder: t.style(t.muted), Suggestion: t.style(t.muted), Prompt: t.style(t.accent)}
	s.Filter.Focused, s.Filter.Blurred = field, field
	s.Filter.Cursor.Color = t.accent
	s.DefaultFilterCharacterMatch = t.style(t.strong).Underline(true)
	s.StatusBar, s.StatusEmpty, s.StatusBarActiveFilter = t.style(t.muted), t.style(t.muted), t.style(t.fg)
	s.StatusBarFilterCount, s.NoItems, s.ArabicPagination = t.style(t.muted), t.style(t.muted), t.style(t.muted)
	s.Spinner = t.style(t.muted)
	s.ActivePaginationDot = s.ActivePaginationDot.Foreground(t.fg)
	s.InactivePaginationDot = s.InactivePaginationDot.Foreground(t.line)
	s.DividerDot = s.DividerDot.Foreground(t.line)
	m.list.Styles = s
	m.list.FilterInput.SetStyles(s.Filter)
	m.list.Paginator.ActiveDot, m.list.Paginator.InactiveDot = s.ActivePaginationDot.String(), s.InactivePaginationDot.String()
	key, desc, sep := t.style(t.fg), t.style(t.muted), t.style(t.line)
	m.help.Styles = help.Styles{Ellipsis: sep, ShortKey: key, ShortDesc: desc, ShortSeparator: sep, FullKey: key, FullDesc: desc, FullSeparator: sep}
	m.spin.Style = t.style(t.accent)
}

// truecolor matches an SGR truecolor colour, foreground (38) or background
// (48), as the half-block picture paints each cell.
var truecolor = regexp.MustCompile(`([34])8;2;(\d+);(\d+);(\d+)`)

// redden turns a half-block picture's colours into dim reds by luminance, as
// the page's night filter does its picture (grayscale, sepia, red, darker).
func redden(s string) string {
	return truecolor.ReplaceAllStringFunc(s, func(c string) string {
		p := truecolor.FindStringSubmatch(c)
		r, _ := strconv.Atoi(p[2])
		g, _ := strconv.Atoi(p[3])
		b, _ := strconv.Atoi(p[4])
		l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) * 0.55

		return fmt.Sprintf("%s8;2;%d;%d;0", p[1], int(l), int(l*0.12))
	})
}
