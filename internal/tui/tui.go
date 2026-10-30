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
	"cmp"
	"context"
	"encoding/json"
	_ "image/jpeg" // hips2fits answers JPEG
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
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
	// Quitting cancels the computes still running.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, opt, compute)
	popts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opt.Snapshot != "" {
		m.picked = true // half-blocks under the pasted picture; no Kitty probe to wait for
		popts = append(popts, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(opt.Width, opt.Height))
	}
	p := tea.NewProgram(m, popts...)
	done := make(chan []string)
	go func() {
		// A Reader, not a Scanner: a Scanner stops at a line past 64 KiB, and
		// the pipe, undrained, would then block every later warning's writer.
		var all []string
		br := bufio.NewReader(r)
		for {
			l, err := br.ReadString('\n')
			// A superseded or quit compute's fetches fail as it is
			// cancelled (fetch's stale-cache fallback warns without a
			// context): no news, in the view or on exit.
			if l = strings.TrimSuffix(l, "\n"); (l != "" || err == nil) && !strings.Contains(l, context.Canceled.Error()) {
				all = append(all, l)
				p.Send(warnMsg(l))
			}
			if err != nil {
				break
			}
		}
		done <- all
	}()
	_, err = p.Run()
	cancel()
	if err == nil {
		err = m.snapErr
	}
	// A cancelled compute may still be warning: wait for it before the
	// pipe goes, or its lines are lost and os.Stderr races.
	m.gate.Lock()
	m.closed = true
	m.gate.Unlock()
	m.computing.Wait()
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
	Prev, Next, Tonight, Sort, Open, Picture, Night, Warn, Help, Quit, Select, Up, Down, Filter key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Prev, k.Next, k.Sort, k.Filter, k.Open, k.Help, k.Quit}
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
	Select:  key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select")), // the short help's Up and Down
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous target")),
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
	list      list.Model
	theme     theme
	want      time.Time       // the date of the night computing, zero when Tonight picks it
	probedAt  time.Time       // when up was last probed
	ctx       context.Context //nolint:containedctx // the computes' context for the program's life
	snapErr   error
	compute   Compute
	now       func() time.Time
	cancel    context.CancelFunc // the running compute's
	client    *http.Client       // the probes'
	help      help.Model
	pic       pictureurl.Model
	picURL    string // the selected target's picture, "" with no service up
	reselect  string // the target to select once a filter's refilter lands
	order     []int  // card indices in the sort's order
	warnings  []string
	spin      spinner.Model
	chartView string // chart's last drawing, of chartKey
	chartKey  chartKey
	opt       Options
	night     Night
	computing sync.WaitGroup // the running computes, which Run waits for once closed
	seq       int
	sortBy    int
	w, h      int
	gate      sync.Mutex  // orders computing's Add before Run's Wait
	up        servicesMsg // the picture services that answer
	loading   bool
	red       bool
	picked    bool // the picture's mode was chosen (i, or Kitty once supported)
	showWarn  bool
	closed    bool // Run is past its program: no compute starts
	probed    bool // up is known: no picture is asked for before
	probing   bool // a probe is out
}

func newModel(ctx context.Context, opt Options, compute Compute) *model {
	m := &model{ctx: ctx, compute: compute, opt: opt, theme: darkTheme, help: help.New(), now: time.Now, client: http.DefaultClient}
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
	m.applyTheme()
	m.pic = pictureurl.NewWithConfig(pictureurl.Config{UserAgent: userAgent, Fit: picture.FitContain})

	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.pic.Init(), m.load(m.opt.Day, m.opt.DateSet), m.reprobe())
}

// snapshotWait is how long a snapshot waits for the picture once asked: from
// the first night shown, which asks for it, so a slow compute does not eat
// into it.
const snapshotWait = 90 * time.Second

// snapshotMsg is the snapshot's deadline.
type snapshotMsg struct{}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:ireturn // tea.Model's signature
	var cmds []tea.Cmd
	if pictureurl.IsPictureMsg(msg) {
		cmds = append(cmds, m.pictureMsg(msg))
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
		first := m.night.Day.IsZero()
		m.night, m.loading, m.want = msg.night, false, time.Time{}
		cmds = append(cmds, m.resort(), m.layout())
		if first && m.opt.Snapshot != "" {
			// A picture CDS never sends must not hold the demo up for good.
			cmds = append(cmds, tea.Tick(snapshotWait, func(time.Time) tea.Msg { return snapshotMsg{} }))
		}
	case servicesMsg:
		m.up, m.probed, m.probing, m.probedAt = msg, true, false, m.now()
	case warnMsg:
		m.warnings = append(m.warnings, strings.TrimPrefix(string(msg), "warning: "))
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		cmds = append(cmds, cmd)
	case tea.KeyPressMsg:
		if cmd, done := m.key(msg); done {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if _, ok := msg.(list.FilterMatchesMsg); ok {
		if m.staleMatches() {
			// An older night's items: refilter this night's.
			cmds = append(cmds, m.resort())
		} else {
			m.reselectMatch()
		}
	}
	cmds = append(cmds, cmd, m.selectPicture())

	return m, tea.Batch(cmds...)
}

// key handles the view's own keys; false leaves the key to the list.
func (m *model) key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.list.SettingFilter() {
		return nil, false // the list's filter takes the keys
	}
	switch {
	case key.Matches(msg, keys.Quit):
		return tea.Quit, true
	case key.Matches(msg, keys.Prev), key.Matches(msg, keys.Next):
		// While tonight computes its date is unknown (Tonight picks it): a
		// step from the night shown would land elsewhere. A -date night's
		// is known (load sets want).
		if m.loading && m.want.IsZero() {
			return nil, true
		}
		// From the night last asked for while it computes, so presses
		// add up.
		if base := cmp.Or(m.want, m.night.Day); !base.IsZero() {
			d := 1
			if key.Matches(msg, keys.Prev) {
				d = -1
			}

			return m.load(base.AddDate(0, 0, d), true), true
		}
	case key.Matches(msg, keys.Tonight):
		// Tonight is now's, not the launch's: the view may have been open
		// for hours.
		day := m.opt.Day
		if !m.opt.DateSet {
			// In -tz's zone, as the launch's: Window takes the date and
			// local noon from day's zone, which time.Now's is not.
			day = m.now().In(m.opt.Loc)
		}

		return m.load(day, m.opt.DateSet), true
	case key.Matches(msg, keys.Sort):
		m.sortBy = (m.sortBy + 1) % len(sorts)
		cmd := m.resort()

		return tea.Batch(cmd, m.selectPicture()), true
	case key.Matches(msg, keys.Open):
		if i, ok := m.selected(); ok {
			openURL(m.ctx, aladin(m.night.Doc.Pos[i]))
		}

		return nil, true
	case key.Matches(msg, keys.Picture):
		// Before the Kitty probe answers, Toggle does nothing: no choice
		// made, so the switch on its answer still comes.
		before := m.pic.Mode()
		cmd := m.pic.Toggle()
		m.picked = m.picked || m.pic.Mode() != before

		return cmd, true
	case key.Matches(msg, keys.Night):
		m.red = !m.red
		m.theme = darkTheme
		if m.red {
			m.theme = nightTheme
		}
		m.applyTheme()

		return nil, true
	case key.Matches(msg, keys.Warn):
		m.showWarn = !m.showWarn

		return nil, true
	case key.Matches(msg, keys.Help):
		m.help.ShowAll = !m.help.ShowAll

		return m.layout(), true
	}

	return nil, false
}

// load computes the night of day in the background; only the latest one
// asked for is shown, and the one it supersedes is cancelled. A night of a
// date given is the one ← and → step from while it computes; tonight's
// date is Tonight's to pick, so they wait for it.
func (m *model) load(day time.Time, dateSet bool) tea.Cmd {
	m.seq++
	seq := m.seq
	m.loading, m.warnings, m.want = true, nil, time.Time{}
	if dateSet {
		m.want = day
	}
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	return func() tea.Msg {
		// Counted only while Run is not past its program, which then
		// waits for every compute counted.
		m.gate.Lock()
		if m.closed {
			m.gate.Unlock()

			return nil
		}
		m.computing.Add(1)
		m.gate.Unlock()
		defer m.computing.Done()

		return nightMsg{night: m.compute(ctx, day, dateSet), seq: seq}
	}
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
	if !m.probed {
		return false
	}
	st := m.pic.State()

	return m.picURL == "" || st == pictureurl.StateLoaded || st == pictureurl.StateError
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

// resort fills the list in the sort's order, keeping the selected target by
// name (a new night's cards are others). The command is SetItems': with a
// filter set, the list refilters through it and stays empty until it runs,
// so the target is selected then, among the matches (Select takes an index
// into them, which the unfiltered one overruns).
func (m *model) resort() tea.Cmd {
	cards := m.night.Doc.Cards
	var keep string
	if it, ok := m.list.SelectedItem().(item); ok {
		keep = it.c.Name
	}
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
		if cards[i].Name == keep {
			sel = j
		}
	}
	cmd := m.list.SetItems(items)
	if m.list.FilterState() == list.Unfiltered {
		m.list.Select(sel)
	} else {
		m.list.ResetSelected()
		m.reselect = keep
	}

	return cmd
}

// staleMatches reports whether the filter's matches are an older night's
// items, whose indices would read this night's slices.
func (m *model) staleMatches() bool {
	cards := m.night.Doc.Cards

	return slices.ContainsFunc(m.list.VisibleItems(), func(v list.Item) bool {
		it, ok := v.(item)

		return !ok || it.i >= len(cards) || it.c != &cards[it.i]
	})
}

// reselectMatch selects resort's kept target among a filter's matches once
// they land.
func (m *model) reselectMatch() {
	if m.reselect == "" {
		return
	}
	if j := slices.IndexFunc(m.list.VisibleItems(), func(v list.Item) bool { return v.(item).c.Name == m.reselect }); j >= 0 { //nolint:forcetypeassert // the list holds items only
		m.list.Select(j)
	}
	m.reselect = ""
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

// selectPicture points the picture at the selected target's DSS2 image,
// from CDS while it answers, else SkyView, else none.
func (m *model) selectPicture() tea.Cmd {
	i, ok := m.selected()
	if !ok || m.picCols() == 0 || !m.probed {
		return nil
	}
	// Both down: probed again before the next picture, at most once a
	// minute.
	if !m.up.cds && !m.up.sky && !m.probing && m.now().Sub(m.probedAt) >= reprobeAfter {
		return m.reprobe()
	}
	var u string
	switch pos := m.night.Doc.Pos[i]; {
	case m.up.cds:
		u = thumb(pos, m.picCols(), m.picRows())
	case m.up.sky:
		u = skyview(pos, m.picCols(), m.picRows())
	}
	if u == m.picURL {
		return nil
	}
	m.picURL = u

	return m.pic.SetURL(u)
}

// pictureMsg hands msg to the picture. A failed picture marks its service
// down for the session, and the selected target is asked of the other.
func (m *model) pictureMsg(msg tea.Msg) tea.Cmd {
	cmd := m.pic.Update(msg)
	if m.picURL == "" || m.pic.CurrentURL() != m.picURL || m.pic.State() != pictureurl.StateError {
		return cmd
	}
	if strings.HasPrefix(m.picURL, cdsURL) {
		m.up.cds = false
	} else {
		m.up.sky = false
	}
	m.picURL = ""

	return tea.Batch(cmd, m.selectPicture())
}

// reprobe sends probe, marking it out.
func (m *model) reprobe() tea.Cmd {
	m.probing = true

	return probe(m.ctx, m.client)
}

// reprobeAfter is how long both services marked down stay so.
const reprobeAfter = time.Minute

// servicesMsg is which picture services answered.
type servicesMsg struct{ cds, sky bool }

// probeTimeout is how long probe waits for each service.
const probeTimeout = 8 * time.Second

// probe asks CDS and SkyView for a one-cell picture each, at once: a service
// that is down then costs one wait at start, not one a target.
func probe(ctx context.Context, client *http.Client) tea.Cmd {
	return func() tea.Msg {
		var up servicesMsg
		var wg sync.WaitGroup
		wg.Go(func() { up.cds = answers(ctx, client, thumb([3]float64{}, 1, 1)) })
		wg.Go(func() { up.sky = answers(ctx, client, skyview([3]float64{}, 1, 1)) })
		wg.Wait()

		return up
	}
}

// answers reports whether u answers with a picture within probeTimeout.
func answers(ctx context.Context, client *http.Client, u string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusOK && strings.HasPrefix(resp.Header.Get("Content-Type"), "image/")
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
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }() // reaped, not left a zombie until exit
	}
}
