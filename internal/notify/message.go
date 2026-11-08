// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package notify

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // zone names for minimal containers, as in astro-recommender
	"unicode/utf16"

	"github.com/dkorunic/astro-recommender/internal/dss"
)

const (
	// One message's limits: a Telegram text's characters after the markup is
	// parsed and the entities it formats (past that, plain text), a Discord
	// embed description's characters, and the bytes of a ntfy message (past
	// that, an attachment), kept for every webhook.
	maxTelegram = 4096
	maxEntities = 100
	maxDiscord  = 4096
	maxWebhook  = 4096
	maxSlack    = 4000 // Slack's advice for a message's text; past 40000 it cuts

	maxPlace = 60 // runes; Nominatim's names run to a hundred
	maxDesc  = 80

	clock = "15:04"
	aside = " · "
)

// Style is how a destination marks a message up and how much of it takes:
// the formats hold one %s, esc makes catalogue text safe in the markup.
type Style struct {
	esc               func(string) string
	link              func(text, target string) string
	fits              func(string) bool
	Name              string
	bold, italic, pre string
}

var (
	telegramTags = regexp.MustCompile(`<[^>]*>`)
	// Slack's only escapes are these three entities. Its formatting marks
	// cannot be escaped, and the lists hold some (Pickering`s Triangle, "369-*N-8"),
	// so in catalogue text they become their lookalikes.
	slackEntities = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	slackText     = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "*", "∗", "_", "ˍ", "~", "∼", "`", "ʼ")
	discordEsc    = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `|`, `\|`, `[`, `\[`, `]`, `\]`, `<`, `\<`, `>`, `\>`)

	// Telegram is HTML, with target names linked.
	Telegram = Style{
		Name: "telegram", bold: "<b>%s</b>", italic: "<i>%s</i>", pre: "<pre>%s</pre>", esc: html.EscapeString,
		link: func(text, target string) string {
			return fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(target), text)
		},
		// Telegram counts the text after parsing; UTF-16 units are never fewer
		// than the characters it may count instead.
		fits: func(msg string) bool {
			return utf16Len(html.UnescapeString(telegramTags.ReplaceAllString(msg, ""))) <= maxTelegram &&
				strings.Count(msg, "</") <= maxEntities
		},
	}
	// Discord is Markdown for an embed's description.
	Discord = Style{
		Name: "discord", bold: "**%s**", italic: "*%s*", pre: "```\n%s\n```", esc: discordEsc.Replace,
		link: func(text, target string) string { return "[" + text + "](" + target + ")" },
		fits: func(msg string) bool { return utf16Len(msg) <= maxDiscord },
	}
	// Slack is mrkdwn.
	Slack = Style{
		Name: "slack", bold: "*%s*", italic: "_%s_", pre: "```\n%s\n```", esc: slackText.Replace,
		link: func(text, target string) string { return "<" + slackEntities.Replace(target) + "|" + text + ">" },
		fits: func(msg string) bool { return utf16Len(msg) <= maxSlack },
	}
	// Plain is unmarked text for a webhook.
	Plain = Style{
		Name: "webhook", bold: "%s", italic: "%s", pre: "%s", esc: func(s string) string { return s },
		link: func(text, _ string) string { return text },
		fits: func(msg string) bool { return len(msg) <= maxWebhook },
	}
)

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n-1])) + "…"
	}

	return s
}

// orDash formats a known value and leaves an unknown one a dash.
func orDash(format string, v *float64) string {
	if v == nil {
		return "-"
	}

	return fmt.Sprintf(format, *v)
}

// inWindow is whether any minute of the window reads the hour as its nearest.
func (r *Report) inWindow(h *Hour) bool {
	return h.Hour.Add(-time.Hour/2).Before(r.Night.End) && h.Hour.Add(time.Hour/2).After(r.Night.Start)
}

// conditionAt is the stretch holding t, moved into the window; nil without one.
func (r *Report) conditionAt(t time.Time) *Condition {
	if t.Before(r.Night.Start) {
		t = r.Night.Start
	}
	if !t.Before(r.Night.End) {
		t = r.Night.End.Add(-time.Minute)
	}
	for i := range r.Conditions {
		if c := &r.Conditions[i]; !t.Before(c.From) && t.Before(c.To) {
			return c
		}
	}

	return nil
}

// times writes the message's clock times in the site's zone: a decoded offset
// has a zone name only when it is the host's too. On a night over a change of
// offset each carries its zone, since a bare 02:30 happens twice or never.
type times struct {
	loc    *time.Location // nil: the report's own offsets
	layout string
}

func (r *Report) times() times {
	t := times{layout: clock}
	if loc, err := time.LoadLocation(r.Location.TimeZone); err == nil && r.Location.TimeZone != "" {
		t.loc = loc
	}
	_, from := r.Night.Start.Zone()
	if _, to := r.Night.End.Zone(); from != to {
		t.layout = clock + " MST"
	}

	return t
}

func (c times) in(t time.Time) time.Time {
	if c.loc == nil {
		return t
	}

	return t.In(c.loc)
}

func (c times) of(t time.Time) string { return c.in(t).Format(c.layout) }

// hourLabels is each forecast hour's first column: minutes only where a row
// has any (forecast hours are UTC's, which a half-hour zone reads at minutes
// past), the zone only on a night over a change of offset.
func hourLabels(r *Report, c times) []string {
	layout := "15"
	for i := range r.Forecast {
		if c.in(r.Forecast[i].Hour).Minute() != 0 {
			layout = clock
		}
	}
	if c.layout != clock {
		layout += " MST"
	}
	labels := make([]string, len(r.Forecast))
	for i := range r.Forecast {
		labels[i] = c.in(r.Forecast[i].Hour).Format(layout)
	}

	return labels
}

// forecastTable is monospaced text with nothing any markup treats specially.
func forecastTable(r *Report, l Limits, ct times) string {
	labels := hourLabels(r, ct)
	width := len("h")
	for _, label := range labels {
		width = max(width, len(label))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %5s %5s %5s %4s %3s\n", width+1, "h", "cloud", "see", "wind", "dew", "mm")
	for i := range r.Forecast {
		h := &r.Forecast[i]
		if !r.inWindow(h) {
			continue
		}
		mark, seeing, wind := " ", "-", orDash("%.0f", h.Wind)
		if c := r.conditionAt(h.Hour); c != nil {
			if c.clear(l) {
				mark = "*"
			}
			if c.Seeing != nil {
				seeing = fmt.Sprintf(`%.1f"`, *c.Seeing)
			}
		}
		if h.Gust != nil {
			wind += "/" + orDash("%.0f", h.Gust)
		}
		fmt.Fprintf(&b, "%-*s%s %5s %5s %5s %3s° %3s\n", width, labels[i], mark,
			orDash("%.0f%%", h.Cloud), seeing, wind, orDash("%.0f", h.DewSpread), orDash("%.1f", h.Precip))
	}

	return strings.TrimRight(b.String(), "\n")
}

func moonText(r *Report, c times) string {
	if len(r.Moon.Up) == 0 {
		// Not "all night": the report knows only its window, which -to can end early.
		return fmt.Sprintf("🌙 Moon %.0f%%, down through %s\n", r.Moon.Illumination*100, c.of(r.Night.End))
	}
	ups := make([]string, 0, len(r.Moon.Up))
	for _, s := range r.Moon.Up {
		ups = append(ups, c.of(s.From)+"–"+c.of(s.To))
	}

	return fmt.Sprintf("🌙 Moon %.0f%%, up %s\n", r.Moon.Illumination*100, strings.Join(ups, ", "))
}

// clearText is the header's line of clear stretches: those long enough to
// count, else the longest there is.
func clearText(r *Report, l Limits, runs []ClearRun, st *Style, ct times) string {
	need := l.need(r)
	shown := slices.DeleteFunc(slices.Clone(runs), func(c ClearRun) bool { return c.length() < need })
	lead, label := "✅ ", "Clear "
	if len(shown) == 0 && len(runs) > 0 { // none long enough: say so
		shown, lead, label = []ClearRun{longest(runs)}, "⚠️ ", "Only clear "
	}
	if len(shown) == 0 {
		return fmt.Sprintf("⚠️ "+st.bold+"\n", "No clear stretch")
	}
	var (
		spans         []string
		total         time.Duration
		cloud, seeing float64
	)
	for _, c := range shown {
		spans = append(spans, ct.of(c.From)+"–"+ct.of(c.To))
		total += c.length()
		cloud, seeing = max(cloud, c.Cloud), max(seeing, c.Seeing)
	}

	return fmt.Sprintf(lead+st.bold+" (%.1f h), cloud ≤%.0f%%, seeing ≤%.1f″\n",
		label+strings.Join(spans, ", "), total.Hours(), cloud, seeing)
}

// header is the message up to its targets, its first line the title.
func header(r *Report, l Limits, runs []ClearRun, st *Style, c times) string {
	place := r.Location.Place
	if place == "" {
		place = fmt.Sprintf("%.2f, %.2f", r.Location.Lat, r.Location.Lon)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔭 "+st.bold+"\n", st.esc(cut(place, maxPlace)))
	fmt.Fprintf(&b, "🗓 %s%s → %s\n", c.in(r.Night.Start).Format("Mon 2 Jan, "), c.of(r.Night.Start), c.in(r.Night.End).Format(clock+" MST"))
	b.WriteString(clearText(r, l, runs, st, c))
	b.WriteString(moonText(r, c))
	fmt.Fprintf(&b, "🌌 Zenith %.2f mag/arcsec²", r.Sky.ZenithMag)
	if r.Sky.Bortle > 0 {
		fmt.Fprintf(&b, ", Bortle %d", r.Sky.Bortle)
	}
	fmt.Fprintf(&b, "\n\n"+st.bold+"\n"+st.pre+"\n"+st.italic+"\n\n"+st.bold+"\n", "Forecast", forecastTable(r, l, c),
		st.esc("* clear; wind/gust km/h, dew spread, rain"), "Targets")

	return b.String()
}

func (t *Target) block(rank int, runs []ClearRun, st *Style, c times) string {
	size := 0.0
	if t.Size != nil {
		size = *t.Size
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n"+st.bold, fmt.Sprintf("%d. %s", rank, st.link(st.esc(t.Name), dss.Aladin(t.RA, t.Dec, size))))
	if t.Description != "" {
		b.WriteString(aside + st.esc(cut(t.Description, maxDesc)))
	}
	b.WriteString("\n" + st.esc(t.Type))
	if t.Constellation != "" {
		b.WriteString(aside + st.esc(t.Constellation))
	}
	if t.Size != nil {
		fmt.Fprintf(&b, "%s%.1f′", aside, *t.Size)
	}
	switch {
	case t.Mag != nil:
		fmt.Fprintf(&b, "%smag %.1f", aside, *t.Mag)
	case t.BMag != nil:
		fmt.Fprintf(&b, "%smag %.1fB", aside, *t.BMag)
	}
	// The time in clear air, else when it is up at all.
	lead, when := "🕒 ", t.clearSpan(runs)
	if when.length() <= 0 {
		lead, when = "☁️ up ", t.Observable.Span
	}
	fmt.Fprintf(&b, "\n%s%s–%s", lead, c.of(when.From), c.of(when.To))
	// The night's peak can fall outside that time, or above -alt-max.
	if !t.MaxAt.Before(when.From) && t.MaxAt.Before(when.To) {
		fmt.Fprintf(&b, "%speak %.0f° at %s", aside, t.MaxAlt, c.of(t.MaxAt))
	}
	fmt.Fprintf(&b, "%sscore %.2f", aside, t.Score)
	if t.Filter != "" {
		b.WriteString(aside + st.esc(t.Filter))
	}
	b.WriteString("\n")

	return b.String()
}

// imagingOrder is the listed targets' indexes, those that qualified the night
// first, so the space limits cannot cut them; else in rank order.
func imagingOrder(r *Report, l Limits, runs []ClearRun) []int {
	need := l.need(r)
	order := make([]int, 0, len(r.Targets))
	for _, qualified := range []bool{true, false} {
		for i := range r.Targets {
			if (r.Targets[i].clearSpan(runs).length() >= need) == qualified {
				order = append(order, i)
			}
		}
	}

	return order
}

// Message renders the report in a destination's style, with as many of the
// targets as fit in one message, those that qualified the night first; each
// keeps its rank.
func Message(r *Report, l Limits, runs []ClearRun, st *Style) string {
	more := func(n int) string { return "\n" + fmt.Sprintf(st.italic, fmt.Sprintf("…and %d more", n)) }

	c := r.times()
	msg := header(r, l, runs, st, c)
	order := imagingOrder(r, l, runs)
	for shown, i := range order {
		next := msg + r.Targets[i].block(i+1, runs, st, c)
		tail := "" // the last target needs no room for "and more"
		if shown+1 < len(order) {
			tail = more(len(order))
		}
		if !st.fits(next + tail) {
			return msg + more(len(order)-shown)
		}
		msg = next
	}

	return strings.TrimRight(msg, "\n")
}
