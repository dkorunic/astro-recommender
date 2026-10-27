// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package tui

import (
	"image/color"

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
