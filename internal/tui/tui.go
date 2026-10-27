// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package tui is the interactive terminal view, laid out like the browser
// page (after Telescopius' deep sky browser): the night, the Moon and the
// forecast hours on the left, the targets as a list of cards, and the
// selected one's DSS2 picture, altitude curve and sub-exposure estimate. It
// shows a view.Doc and asks its caller to compute other nights.
package tui

import (
	"bufio"
	"context"
	"encoding/json"
	_ "image/jpeg" // hips2fits answers JPEG
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/NimbleMarkets/ntcharts/v2/picture/pictureurl"
	"github.com/dkorunic/astro-recommender/internal/view"
)

// Night is one computed night: Doc, or Msg when there is nothing to plan
// (no night at the site, or tonight's part has passed), or Err. Day is the
// evening's date.
type Night struct {
	Day time.Time
	Err error
	Msg string
	Doc view.Doc
}

// Compute computes the night of day's evening; dateSet false means tonight,
// the night in progress included, as without -date.
type Compute func(ctx context.Context, day time.Time, dateSet bool) Night

// Options are what the view shows besides a night: the -date given (or
// now) and whether it was given, the zone, and a one-line description of
// the setup (list, telescope, filters) for the header.
type Options struct {
	Day   time.Time
	Loc   *time.Location
	Setup string
	// Snapshot, when set, renders one frame without a terminal instead:
	// Width x Height cells, written as ANSI to this file once the night and
	// the picture have loaded, with the picture's cell rectangle and URL in
	// Snapshot + ".json" (scripts/demo.sh pastes the picture's own pixels
	// over its half-blocks, as Kitty graphics show it).
	Snapshot      string
	Width, Height int
	DateSet       bool
	Framing       bool // SUBS is meaningful
}

// Run shows the interactive view until the user quits. The packages warn on
// stderr, which would scribble over the screen, so os.Stderr is a pipe while
// it runs and its lines are shown in the view (w), then printed again on exit.
func Run(ctx context.Context, opt Options, compute Compute) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	stderr := os.Stderr
	os.Stderr = w
	m := newModel(ctx, opt, compute)
	popts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opt.Snapshot != "" {
		m.picked = true // half-blocks under the pasted picture; no Kitty probe to wait for
		popts = append(popts, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(opt.Width, opt.Height))
	}
	p := tea.NewProgram(m, popts...)
	done := make(chan []string)
	go func() {
		var all []string
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			all = append(all, sc.Text())
			p.Send(warnMsg(sc.Text()))
		}
		done <- all
	}()
	_, err = p.Run()
	if err == nil {
		err = m.snapErr
	}
	os.Stderr = stderr
	_ = w.Close()
	for _, l := range <-done {
		_, _ = io.WriteString(stderr, l+"\n")
	}
	_ = r.Close()

	return err
}

// Messages: a computed night (seq tells a stale one from the latest) and a
// stderr line.
type (
	nightMsg struct {
		night Night
		seq   int
	}
	warnMsg string
)

// sorts are the orders s cycles through, as the page's Sort menu.
var sorts = []string{"score", "peak", "altitude", "size", "name"}

type keyMap struct {
	Prev, Next, Tonight, Sort, Open, Picture, Night, Warn, Help, Quit, Up, Down, Filter key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Prev, k.Next, k.Sort, k.Filter, k.Open, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Filter, k.Sort},
		{k.Prev, k.Next, k.Tonight},
		{k.Open, k.Picture, k.Night, k.Warn},
		{k.Help, k.Quit},
	}
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next target")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "find")),
	Sort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
	Prev:    key.NewBinding(key.WithKeys("left", "["), key.WithHelp("←", "night before")),
	Next:    key.NewBinding(key.WithKeys("right", "]"), key.WithHelp("→", "night after")),
	Tonight: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "back to the first night")),
	Open:    key.NewBinding(key.WithKeys("o", "enter"), key.WithHelp("o", "Aladin")),
	Picture: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "picture: blocks/Kitty")),
	Night:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "red night vision")),
	Warn:    key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "warnings")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type model struct {
	list     list.Model
	theme    theme
	ctx      context.Context //nolint:containedctx // the computes' context for the program's life
	snapErr  error
	compute  Compute
	help     help.Model
	pic      pictureurl.Model
	picURL   string
	order    []int // card indices in the sort's order
	warnings []string
	spin     spinner.Model
	opt      Options
	night    Night
	seq      int
	sortBy   int
	w, h     int
	loading  bool
	red      bool
	picked   bool // the picture's mode was chosen (i, or Kitty once supported)
	showWarn bool
}

func newModel(ctx context.Context, opt Options, compute Compute) *model {
	m := &model{ctx: ctx, compute: compute, opt: opt, theme: darkTheme, help: help.New()}
	m.spin = spinner.New(spinner.WithSpinner(spinner.Dot))
	m.list = list.New(nil, delegate{m}, 0, 0)
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	m.list.SetStatusBarItemName("target", "targets")
	// ← and → change the night; the list pages with PgUp/PgDn alone.
	m.list.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	m.list.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	m.list.KeyMap.Quit = key.NewBinding(key.WithKeys("q"))
	m.pic = pictureurl.NewWithConfig(pictureurl.Config{UserAgent: "astro-recommender (+https://github.com/dkorunic/astro-recommender)", Fit: picture.FitContain})

	return m
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.pic.Init(), m.load(m.opt.Day, m.opt.DateSet)}
	if m.opt.Snapshot != "" {
		// A picture CDS never sends must not hold the demo up for good.
		cmds = append(cmds, tea.Tick(snapshotWait, func(time.Time) tea.Msg { return snapshotMsg{} }))
	}

	return tea.Batch(cmds...)
}

// snapshotWait is how long a snapshot waits for the picture once asked.
const snapshotWait = 90 * time.Second

// snapshotMsg is the snapshot's deadline.
type snapshotMsg struct{}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:ireturn // tea.Model's signature
	var cmds []tea.Cmd
	if pictureurl.IsPictureMsg(msg) {
		cmds = append(cmds, m.pic.Update(msg))
	}
	// Kitty graphics show the picture's own pixels where half-blocks show two
	// a cell: switch once the terminal says it speaks them, unless i chose.
	if !m.picked && picture.KittySupported() == picture.KittyCapabilitySupported {
		m.picked = true
		if m.pic.Mode() == picture.PictureGlyph {
			cmds = append(cmds, m.pic.Toggle())
		}
	}
	if m.opt.Snapshot != "" {
		if _, deadline := msg.(snapshotMsg); deadline || m.snapshotReady() {
			m.snapshot()

			return m, tea.Quit
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		cmds = append(cmds, m.layout())
	case nightMsg:
		if msg.seq != m.seq {
			return m, tea.Batch(cmds...)
		}
		m.night, m.loading = msg.night, false
		cmds = append(cmds, m.resort(), m.layout())
	case warnMsg:
		m.warnings = append(m.warnings, strings.TrimPrefix(string(msg), "warning: "))
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		cmds = append(cmds, cmd)
	case tea.KeyPressMsg:
		if m.list.SettingFilter() {
			break // the list's filter takes the keys
		}
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Prev), key.Matches(msg, keys.Next):
			if !m.night.Day.IsZero() {
				d := 1
				if key.Matches(msg, keys.Prev) {
					d = -1
				}

				return m, m.load(m.night.Day.AddDate(0, 0, d), true)
			}
		case key.Matches(msg, keys.Tonight):
			return m, m.load(m.opt.Day, m.opt.DateSet)
		case key.Matches(msg, keys.Sort):
			m.sortBy = (m.sortBy + 1) % len(sorts)
			cmd := m.resort()

			return m, tea.Batch(cmd, m.selectPicture())
		case key.Matches(msg, keys.Open):
			if i, ok := m.selected(); ok {
				openURL(m.ctx, aladin(m.night.Doc.Pos[i]))
			}

			return m, nil
		case key.Matches(msg, keys.Picture):
			m.picked = true

			return m, m.pic.Toggle()
		case key.Matches(msg, keys.Night):
			m.red = !m.red
			m.theme = darkTheme
			if m.red {
				m.theme = nightTheme
			}

			return m, nil
		case key.Matches(msg, keys.Warn):
			m.showWarn = !m.showWarn

			return m, nil
		case key.Matches(msg, keys.Help):
			m.help.ShowAll = !m.help.ShowAll

			return m, m.layout()
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd, m.selectPicture())

	return m, tea.Batch(cmds...)
}

// load computes the night of day in the background; only the latest one
// asked for is shown.
func (m *model) load(day time.Time, dateSet bool) tea.Cmd {
	m.seq++
	seq := m.seq
	m.loading, m.warnings = true, nil

	return func() tea.Msg { return nightMsg{night: m.compute(m.ctx, day, dateSet), seq: seq} }
}

// snapshotReady reports whether the snapshot frame can be taken: the night
// shown, and its picture loaded or failed (or no slot for one).
func (m *model) snapshotReady() bool {
	if m.loading || m.night.Day.IsZero() {
		return false
	}
	if _, ok := m.selected(); !ok || m.picCols() == 0 {
		return true
	}
	st := m.pic.State()

	return m.picURL != "" && (st == pictureurl.StateLoaded || st == pictureurl.StateError)
}

// snapshot writes the frame and its picture's rectangle (snapshotRect).
func (m *model) snapshot() {
	rect := struct {
		URL  string `json:"url,omitempty"`
		Col  int    `json:"col"`
		Row  int    `json:"row"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}{}
	if _, ok := m.selected(); ok && m.picCols() > 0 && m.pic.State() == pictureurl.StateLoaded {
		g := m.geometry()
		// The detail pane's picture: past the left column, the border and
		// the padding; under the header, the list pane, the border and the
		// pane's title.
		rect.URL, rect.Col, rect.Row, rect.Cols, rect.Rows = m.picURL, g.left+2, 1+g.list+2, g.pic, g.graph
	}
	b, err := json.Marshal(rect)
	if err == nil {
		err = os.WriteFile(m.opt.Snapshot+".json", b, 0o600)
	}
	if err == nil {
		err = os.WriteFile(m.opt.Snapshot, []byte(m.render()+"\n"), 0o600)
	}
	m.snapErr = err
}

// resort fills the list in the sort's order, keeping the selected target.
// The command is SetItems': with a filter set, the list refilters through it
// and stays empty until it runs.
func (m *model) resort() tea.Cmd {
	cards := m.night.Doc.Cards
	keep, kept := m.selected()
	m.order = make([]int, len(cards))
	for i := range m.order {
		m.order[i] = i
	}
	by := map[string]func(a, b int) int{
		"peak":     func(a, b int) int { return int(cards[a].MaxAt - cards[b].MaxAt) },
		"altitude": func(a, b int) int { return cmpF(cards[b].MaxAlt, cards[a].MaxAlt) },
		"size":     func(a, b int) int { return cmpF(cards[b].Size, cards[a].Size) },
		"name":     func(a, b int) int { return strings.Compare(cards[a].Name, cards[b].Name) },
	}[sorts[m.sortBy]]
	if by != nil {
		slices.SortStableFunc(m.order, by)
	}
	items := make([]list.Item, len(m.order))
	sel := 0
	for j, i := range m.order {
		items[j] = item{i: i, c: &cards[i]}
		if kept && i == keep {
			sel = j
		}
	}
	cmd := m.list.SetItems(items)
	m.list.Select(sel)

	return cmd
}

func cmpF(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}

// selected returns the index into the Doc's per-result slices of the
// selected card.
func (m *model) selected() (int, bool) {
	if it, ok := m.list.SelectedItem().(item); ok {
		return it.i, true
	}

	return 0, false
}

// selectPicture points the picture at the selected target's DSS2 image.
func (m *model) selectPicture() tea.Cmd {
	i, ok := m.selected()
	if !ok || m.picCols() == 0 {
		return nil
	}
	u := thumb(m.night.Doc.Pos[i], m.picCols(), m.picRows())
	if u == m.picURL {
		return nil
	}
	m.picURL = u

	return m.pic.SetURL(u)
}

// openURL opens u in the system's browser; a failure is not worth a word.
func openURL(ctx context.Context, u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", u)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", u)
	}
	_ = cmd.Start()
}
