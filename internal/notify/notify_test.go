// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dkorunic/astro-recommender/internal/config"
	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/dkorunic/astro-recommender/internal/output"
	"github.com/dkorunic/astro-recommender/internal/scoring"
	"github.com/dkorunic/astro-recommender/internal/weather"
	"github.com/go-telegram/bot"
	"github.com/slack-go/slack"
)

const testToken = "123:secret"

var (
	testLimits = Limits{Cloud: 10, Seeing: 2, Clear: 2 * time.Hour}
	testStart  = time.Date(2026, 11, 8, 18, 15, 0, 0, time.UTC)
)

//go:fix inline
func ptr(v float64) *float64 { return new(v) }

// night is a window from testStart of one hour per cloud value.
func night(clouds ...float64) *Report {
	r := &Report{}
	r.Night.Start, r.Night.End = testStart, testStart.Add(time.Duration(len(clouds))*time.Hour)
	for i, c := range clouds {
		from := testStart.Add(time.Duration(i) * time.Hour)
		r.Forecast = append(r.Forecast, Hour{Hour: from.Truncate(time.Hour), Cloud: new(c)})
		r.Conditions = append(r.Conditions, Condition{Span: Span{from, from.Add(time.Hour)}, Cloud: new(c), Seeing: new(1.5)})
	}

	return r
}

func TestClearRuns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		edit func(*Report)
		name string
		want time.Duration
		runs int
	}{
		{name: "all clear", edit: func(*Report) {}, want: 5 * time.Hour, runs: 1},
		{name: "a cloudy stretch splits it", edit: func(r *Report) { r.Conditions[2].Cloud = new(float64(50)) }, want: 2 * time.Hour, runs: 2},
		{name: "bad seeing splits it", edit: func(r *Report) { r.Conditions[1].Seeing = new(2.5) }, want: 3 * time.Hour, runs: 2},
		{name: "unknown cloud is not clear", edit: func(r *Report) { r.Conditions[1].Cloud = nil }, want: 3 * time.Hour, runs: 2},
		{name: "unknown seeing is not clear", edit: func(r *Report) { r.Conditions[1].Seeing = nil }, want: 3 * time.Hour, runs: 2},
		{name: "unknown rain counts as none, as in the score", edit: func(r *Report) { r.Conditions[1].Precip = nil }, want: 5 * time.Hour, runs: 1},
		{name: "rain is not clear", edit: func(r *Report) { r.Conditions[1].Precip = new(0.2) }, want: 3 * time.Hour, runs: 2},
		{name: "a trace of rain is", edit: func(r *Report) { r.Conditions[1].Precip = new(0.01) }, want: 5 * time.Hour, runs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := night(0, 5, 10, 0, 0)
			tc.edit(r)
			runs := r.ClearRuns(testLimits)
			if got := longest(runs).length(); got != tc.want || len(runs) != tc.runs {
				t.Errorf("got %s in %d runs, want %s in %d", got, len(runs), tc.want, tc.runs)
			}
		})
	}
}

func seen(spans ...Span) Observed { return Observed{Span: spans[0], Spans: spans} }

func TestVerdict(t *testing.T) {
	t.Parallel()

	whole := func(r *Report) []Target {
		return []Target{{Name: "M 31", Observable: seen(Span{r.Night.Start, r.Night.End})}}
	}
	at := func(from, to float64) Span {
		return Span{testStart.Add(time.Duration(from * float64(time.Hour))), testStart.Add(time.Duration(to * float64(time.Hour)))}
	}
	check := func(name string, r *Report, qualifies bool) {
		t.Helper()
		if why := r.Verdict(testLimits, r.ClearRuns(testLimits)); (why == "") != qualifies {
			t.Errorf("%s: verdict %q, want it to qualify: %t", name, why, qualifies)
		}
	}

	r := night(0, 0, 0, 0)
	r.Targets = whole(r)
	check("clear night", r, true)

	short := night(0)
	short.Targets = whole(short)
	check("a window shorter than -min-clear, all clear", short, true)

	cloudy := night(0, 50, 0, 50, 0)
	cloudy.Targets = whole(cloudy)
	check("broken night", cloudy, false)

	check("no targets", night(0, 0, 0, 0), false)

	// Clear for three hours, then cloud.
	late := night(0, 0, 0, 50)
	late.Targets = []Target{{Name: "up as the cloud arrives", Observable: seen(at(2.5, 4))}}
	check("the target rises as the cloud arrives", late, false)
	late.Targets = append(late.Targets, Target{Name: "up early", Observable: seen(at(0, 2))})
	check("another target has the clear hours", late, true)

	// Clear only at the end: the target's longest run is in the cloud.
	second := night(50, 50, 50, 50, 0, 0, 0)
	second.Targets = []Target{{Name: "split", Observable: seen(at(0, 3), at(4, 6.5))}}
	check("a shorter run has the clear hours", second, true)

	// Two clear stretches: the target is up only in the shorter.
	two := night(0, 0, 50, 0, 0, 0)
	two.Targets = []Target{{Name: "early", Observable: seen(at(0, 2))}}
	check("the target has a clear stretch that is not the longest", two, true)
}

func TestLimitsValidate(t *testing.T) {
	t.Parallel()

	good := []Limits{testLimits, {Cloud: 0, Seeing: 0.5, Clear: time.Minute}, {Cloud: 100, Seeing: 30, Clear: 24 * time.Hour}}
	for _, l := range good {
		if err := l.Validate(); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, l := range []Limits{
		{Cloud: math.NaN(), Seeing: 2, Clear: time.Hour},
		{Cloud: 10, Seeing: math.NaN(), Clear: time.Hour},
		{Cloud: 10, Seeing: math.Inf(1), Clear: time.Hour},
		{Cloud: -1, Seeing: 2, Clear: time.Hour},
		{Cloud: 101, Seeing: 2, Clear: time.Hour},
		{Cloud: 10, Seeing: 0, Clear: time.Hour},
		{Cloud: 10, Seeing: 2, Clear: 0},
		{Cloud: 10, Seeing: 2, Clear: -time.Hour},
	} {
		if err := l.Validate(); !errors.Is(err, ErrLimits) {
			t.Errorf("%+v: error %v", l, err)
		}
	}
}

func TestForecastKnown(t *testing.T) {
	t.Parallel()

	if r := night(0, 0, 0); !r.ForecastKnown() {
		t.Error("a full forecast is unknown")
	}

	noCloud := night(0, 0, 0)
	for i := range noCloud.Conditions {
		noCloud.Conditions[i].Cloud = nil // Open-Meteo failed, CAMS answered
	}
	if noCloud.ForecastKnown() {
		t.Error("stretches without cloud count as a forecast")
	}

	noSeeing := night(0, 0, 0)
	for i := range noSeeing.Conditions {
		noSeeing.Conditions[i].Seeing = nil
	}
	if noSeeing.ForecastKnown() {
		t.Error("stretches without seeing count as a forecast")
	}

	if (&Report{}).ForecastKnown() {
		t.Error("no stretches count as a forecast")
	}
}

func TestZoneName(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0)
	r.Location.TimeZone = "Europe/Zagreb"
	r.Night.End = r.Night.End.In(time.FixedZone("", 3600)) // as decoded on a UTC host
	if msg := Message(r, testLimits, r.ClearRuns(testLimits), &Plain); !strings.Contains(msg, "CET") {
		t.Errorf("no zone name in\n%s", msg)
	}
}

// A night over the end of summer time: each stretch keeps its own offset.
func TestClearLineDST(t *testing.T) {
	t.Parallel()

	summer, winter := time.FixedZone("", 2*3600), time.FixedZone("", 3600)
	turn := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	r := &Report{Conditions: []Condition{
		{Span: Span{turn.Add(-2 * time.Hour).In(summer), turn.In(summer)}, Cloud: new(float64(0)), Seeing: new(float64(1))},
		{Span: Span{turn.In(winter), turn.Add(90 * time.Minute).In(winter)}, Cloud: new(float64(0)), Seeing: new(float64(1))},
	}}
	r.Night.Start, r.Night.End = r.Conditions[0].From, r.Conditions[1].To
	r.Location.TimeZone = "Europe/Zagreb"
	r.Forecast = []Hour{{Hour: turn.Add(-time.Hour).In(summer), Cloud: new(float64(0))}, {Hour: turn.In(winter), Cloud: new(float64(0))}}
	msg := Message(r, testLimits, r.ClearRuns(testLimits), &Plain)
	for _, want := range []string{"Clear 01:00 CEST–03:30 CET", "2 CEST", "2 CET"} {
		if !strings.Contains(msg, want) {
			t.Errorf("no %q in\n%s", want, msg)
		}
	}
}

func TestHalfHourZone(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0)
	india := time.FixedZone("IST", 5*3600+1800)
	for i := range r.Forecast {
		r.Forecast[i].Hour = r.Forecast[i].Hour.In(india)
	}
	if table := forecastTable(r, testLimits, r.times()); !strings.Contains(table, "23:30*") {
		t.Errorf("no minutes in\n%s", table)
	}
}

// The hour after the end is the nearest to the window's last minutes.
func TestTableLastHour(t *testing.T) {
	t.Parallel()

	r := night(0, 0)
	r.Night.End = r.Night.End.Add(25 * time.Minute)
	r.Conditions[1].To = r.Night.End
	r.Forecast = append(r.Forecast, Hour{Hour: r.Night.End.Truncate(time.Hour).Add(time.Hour), Cloud: new(float64(0))})
	if table := forecastTable(r, testLimits, r.times()); !strings.Contains(table, `21*    0%  1.5"`) {
		t.Errorf("no last hour in\n%s", table)
	}
}

// TestReportDecodes runs the real -json writer into Report, so a renamed key
// fails here rather than silently stopping the alerts.
func TestReportDecodes(t *testing.T) {
	start := time.Date(2026, 1, 15, 20, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	cfg := &config.Config{Lat: 45.8, Lon: 16, Loc: time.UTC, Twilight: "astronomical", Top: 5, Bortle: 4, Extinction: 0.2}
	s := &scoring.Sky{
		Start: start, End: end, Night: [2]time.Time{start, end}, Elevation: math.NaN(), Illum: 0.42, MoonSep: 42,
		Weather: map[int64]weather.HourWeather{
			start.Unix():                {Cloud: 5, Temp: 5, DewPoint: 1, Wind: 8, Gust: 12, Precip: 0, Seeing: 1.4},
			start.Add(time.Hour).Unix(): {Cloud: 80, Temp: 5, DewPoint: 1, Wind: 8, Gust: 12, Precip: 0, Seeing: 1.4},
		},
	}
	obs := make([]bool, 120)
	for i := range 120 {
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
		s.MoonAlt, s.Ext = append(s.MoonAlt, 10.0), append(s.Ext, 0.2)
		obs[i] = i < 30 || i >= 60
	}
	res := []scoring.Result{{
		Name: "M 31", Description: "Andromeda", Type: "Galaxy", Constellation: "Andromeda", RADeg: 10.7, DecDeg: 41.3, Size: 178, Mag: 3.4,
		Filter: "ha", Score: 0.8, MaxAlt: 70, MaxAt: start.Add(time.Hour), RunFrom: start.Add(time.Hour), RunTo: end, Runs: 2, Obs: obs,
	}}

	path := filepath.Join(t.TempDir(), "report.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = file
	err = output.JSON(cfg, s, "Somewhere", nil, res)
	os.Stdout = stdout
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Location.Place != "Somewhere" || r.Location.TimeZone != "UTC" || r.Location.Lat != 45.8 || r.Location.Lon != 16 ||
		!r.Night.Start.Equal(start) || !r.Night.End.Equal(end) || r.Moon.Illumination != 0.42 || len(r.Moon.Up) != 1 ||
		r.Sky.ZenithMag == 0 || r.Sky.Bortle != 4 {
		t.Errorf("header fields lost: %+v", r)
	}
	if len(r.Forecast) == 0 {
		t.Fatal("no forecast")
	}
	h := r.Forecast[0]
	if !h.Hour.Equal(start) || h.Cloud == nil || h.Wind == nil || h.Gust == nil || h.DewSpread == nil || h.Precip == nil {
		t.Errorf("forecast fields lost: %+v", h)
	}

	// Cloud changes at the half hour, to the next hour's value.
	if len(r.Conditions) != 2 {
		t.Fatalf("conditions %+v", r.Conditions)
	}
	c := r.Conditions[0]
	if !c.From.Equal(start) || !c.To.Equal(start.Add(30*time.Minute)) || c.Cloud == nil || *c.Cloud != 5 || c.Seeing == nil || *c.Seeing != 1.4 || c.Precip == nil {
		t.Errorf("first conditions: %+v", c)
	}
	if c := r.Conditions[1]; c.Cloud == nil || *c.Cloud != 80 || !c.To.Equal(end) {
		t.Errorf("last conditions: %+v", c)
	}

	if len(r.Targets) != 1 {
		t.Fatalf("%d targets", len(r.Targets))
	}
	tg := r.Targets[0]
	if tg.Name != "M 31" || tg.Description != "Andromeda" || tg.Type != "Galaxy" || tg.Constellation != "Andromeda" || tg.Filter != "ha" ||
		tg.RA != 10.7 || tg.Dec != 41.3 || tg.Size == nil || tg.Mag == nil || tg.Score != 0.8 || tg.MaxAlt != 70 || tg.MaxAt.IsZero() ||
		!tg.Observable.From.Equal(start.Add(time.Hour)) || !tg.Observable.To.Equal(end) {
		t.Errorf("target fields lost: %+v", tg)
	}
	if sp := tg.Observable.Spans; len(sp) != 2 || !sp[0].To.Equal(start.Add(30*time.Minute)) || !sp[1].From.Equal(start.Add(time.Hour)) || !sp[1].To.Equal(end) {
		t.Errorf("every run: %+v", sp)
	}
}

func TestMessageFits(t *testing.T) {
	t.Parallel()

	for _, st := range []*Style{&Telegram, &Discord, &Slack, &Plain} {
		t.Run(st.Name, func(t *testing.T) {
			t.Parallel()

			r := night(0, 0, 0, 0, 0)
			r.Location.Place = "A <b> & Co " + strings.Repeat("🔭", 200)
			for range 300 {
				r.Targets = append(r.Targets, Target{
					Name: "M<1>&", Description: strings.Repeat("🌌 long <description> ", 40), Type: "Galaxy", Constellation: "Lyra",
					Size: new(float64(90)), Mag: new(3.4), Filter: "ha",
				})
			}
			msg := Message(r, testLimits, r.ClearRuns(testLimits), st)
			if !st.fits(msg) {
				t.Errorf("%d bytes do not fit", len(msg))
			}
			if !strings.Contains(msg, " more") {
				t.Error("no sign of the targets left out")
			}

			// Small targets, where Telegram's entity limit is the one reached.
			r.Location.Place = "Here"
			r.Targets = r.Targets[:0]
			for range 300 {
				r.Targets = append(r.Targets, Target{Name: "M", Type: "G"})
			}
			if msg := Message(r, testLimits, r.ClearRuns(testLimits), st); !st.fits(msg) {
				t.Errorf("%d bytes with small targets do not fit", len(msg))
			}
		})
	}
}

func TestMessageEscapes(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0, 0, 0)
	r.Location.Place = "A<b>_"
	r.Targets = []Target{{Name: "M_<1>&*", Description: "[d](x)", Type: "G"}}
	for _, tc := range []struct {
		st   *Style
		want []string
		deny []string
	}{
		{&Telegram, []string{"A&lt;b&gt;_", "M_&lt;1&gt;&amp;*", `<a href="https://aladin`, "fov=0.50&amp;survey"}, []string{"<1>", "A<b>"}},
		{&Discord, []string{`A\<b\>\_`, `[M\_\<1\>&\*](https://aladin`, `\[d\](x)`, "```\nh "}, []string{"M_<1>", "<pre>"}},
		{&Slack, []string{"A&lt;b&gt;ˍ", "Mˍ&lt;1&gt;&amp;∗", "|Mˍ&lt;1&gt;&amp;∗>*", "&amp;fov=0.50&amp;survey", "```\nh "}, []string{"<1>", "A<b>", "<pre>", "**", "M_"}},
		{&Plain, []string{"A<b>_", "M_<1>&*", "[d](x)"}, []string{"https://aladin", "**", "<pre>"}},
	} {
		msg := Message(r, testLimits, r.ClearRuns(testLimits), tc.st)
		for _, s := range tc.want {
			if !strings.Contains(msg, s) {
				t.Errorf("%s: no %q in\n%s", tc.st.Name, s, msg)
			}
		}
		for _, s := range tc.deny {
			if strings.Contains(msg, s) {
				t.Errorf("%s: %q in\n%s", tc.st.Name, s, msg)
			}
		}
	}
}

func TestSendTelegram(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		chats []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": 123}})

			return
		}
		if req.FormValue("parse_mode") != "HTML" || req.FormValue("text") != "hi" {
			t.Errorf("sent %q in %q", req.FormValue("text"), req.FormValue("parse_mode"))
		}
		chat := req.FormValue("chat_id")
		mu.Lock()
		chats = append(chats, chat)
		mu.Unlock()
		if chat == "-1" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": "chat not found"})

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer srv.Close()

	err := SendTelegram(context.Background(), testToken, []string{"-1", "@two"}, "hi", bot.WithServerURL(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "chat -1") {
		t.Errorf("error %v, want the failing chat named", err)
	}
	if len(chats) != 2 {
		t.Errorf("reached %v, want both chats", chats)
	}

	srv.Close() // an http error quotes the URL holding the token
	err = SendTelegram(context.Background(), testToken, []string{"1"}, "hi", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("error %v, want one without the token", err)
	}
}

// toServer sends every request to a test server, whatever its host.
type toServer struct{ host string }

func (s toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme, req.URL.Host = "http", s.host

	return http.DefaultTransport.RoundTrip(req)
}

func TestSendDiscord(t *testing.T) {
	old := openGateway
	openGateway = func(*discordgo.Session) error { return nil }
	defer func() { openGateway = old }()

	var mu sync.Mutex
	sent := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Authorization"); got != "Bot "+testToken {
			t.Errorf("authorization %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		var in struct {
			RecipientID string `json:"recipient_id"`
			Embeds      []struct {
				Description string `json:"description"`
			} `json:"embeds"`
		}
		_ = json.NewDecoder(req.Body).Decode(&in)
		if strings.HasSuffix(req.URL.Path, "/users/@me/channels") {
			if in.RecipientID == "closed" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"code":50007,"message":"Cannot send messages to this user"}`))

				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "dm-" + in.RecipientID})

			return
		}
		parts := strings.Split(req.URL.Path, "/")
		if len(in.Embeds) == 1 {
			mu.Lock()
			sent[parts[len(parts)-2]] = in.Embeds[0].Description
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1"})
	}))
	defer srv.Close()

	err := SendDiscord(context.Background(), testToken, []string{"closed", "42", "43", "44"}, "hi", func(s *discordgo.Session) {
		s.Client = &http.Client{Transport: toServer{strings.TrimPrefix(srv.URL, "http://")}}
	})
	if err == nil || !strings.Contains(err.Error(), "user closed") {
		t.Errorf("error %v, want the failing user named", err)
	}
	// More than one send gets through, so -race sees them share the embed.
	for _, ch := range []string{"dm-42", "dm-43", "dm-44"} {
		if sent[ch] != "hi" {
			t.Errorf("sent %v, want the embed in %s", sent, ch)
		}
	}
}

func TestWebhook(t *testing.T) {
	t.Parallel()

	const key = "s3cret"
	var (
		got    *http.Request
		body   []byte
		status = http.StatusOK
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req
		body, _ = io.ReadAll(req.Body)
		if status == http.StatusFound {
			w.Header().Set("Location", "/elsewhere")
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()

	hook, err := NewWebhook(srv.URL+"/topic?token="+key, FormatText, []string{"X-Gotify-Key: abc: def", "Tags: telescope", "Tags: star"})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.Send(context.Background(), "title\nbody"); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || string(body) != "title\nbody" || got.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("%s %q as %q", got.Method, body, got.Header.Get("Content-Type"))
	}
	if got.Header.Get("X-Gotify-Key") != "abc: def" || len(got.Header.Values("Tags")) != 2 {
		t.Errorf("headers %v", got.Header)
	}

	hook.format = FormatJSON
	hook.header.Set("Content-Type", "application/vnd.custom+json")
	if err := hook.Send(context.Background(), "title\nline 1\nline 2"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]string
	if err := json.Unmarshal(body, &doc); err != nil || doc["title"] != "title" || doc["message"] != "line 1\nline 2" || len(doc) != 2 {
		t.Errorf("body %s (%v)", body, err)
	}
	if got.Header.Get("Content-Type") != "application/vnd.custom+json" {
		t.Errorf("the given Content-Type was replaced: %q", got.Header.Get("Content-Type"))
	}

	for _, status = range []int{http.StatusFound, http.StatusUnauthorized, http.StatusInternalServerError} {
		if err := hook.Send(context.Background(), "x"); !isStatus(err) {
			t.Errorf("status %d: error %v", status, err)
		}
	}

	srv.Close()
	if err := hook.Send(context.Background(), "x"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("error %v, want one without the URL's key", err)
	}
}

func TestNewWebhookRejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		want   error
		url    string
		format string
		lines  []string
	}{
		{url: "ntfy.sh/topic", format: FormatText, want: ErrWebhookURL},
		{url: "ftp://ntfy.sh/topic", format: FormatText, want: ErrWebhookURL},
		{url: "https://", format: FormatText, want: ErrWebhookURL},
		{url: "https://ntfy.sh/topic", format: "xml", want: errWebhookFormat},
		{url: "https://ntfy.sh/topic", format: FormatJSON, lines: []string{"no colon"}, want: errWebhookHeader},
		{url: "https://ntfy.sh/topic", format: FormatJSON, lines: []string{": no name"}, want: errWebhookHeader},
		{url: "https://ntfy.sh/topic", format: FormatJSON, lines: []string{"X Gotify Key: abc"}, want: errWebhookHeader},
		{url: "https://ntfy.sh/topic", format: FormatJSON, lines: []string{"Title: a\x01b"}, want: errWebhookHeader},
	} {
		if _, err := NewWebhook(tc.url, tc.format, tc.lines); !errors.Is(err, tc.want) {
			t.Errorf("%q %q %q: error %v, want %v", tc.url, tc.format, tc.lines, err, tc.want)
		}
	}
}

func TestMain(m *testing.M) {
	fetch.RetryDelay = 0
	os.Exit(m.Run())
}

func TestMessageTimes(t *testing.T) {
	t.Parallel()

	at := func(from, to float64) Span {
		return Span{testStart.Add(time.Duration(from * float64(time.Hour))), testStart.Add(time.Duration(to * float64(time.Hour)))}
	}

	// The longest run is in the cloud; the line shows the clear time.
	r := night(50, 50, 50, 50, 0, 0, 0)
	r.Targets = []Target{
		{Name: "cloudy", Type: "G", Observable: seen(at(0, 1))},
		{Name: "split", Type: "G", Observable: seen(at(0, 3), at(4, 6.5))},
	}
	msg := Message(r, testLimits, r.ClearRuns(testLimits), &Plain)
	for _, want := range []string{"🕒 22:15–00:45", "☁️ up 18:15–19:15"} {
		if !strings.Contains(msg, want) {
			t.Errorf("no %q in\n%s", want, msg)
		}
	}
	if strings.Index(msg, "2. split") > strings.Index(msg, "1. cloudy") {
		t.Errorf("the target that qualified is not first in\n%s", msg)
	}

	// Every stretch long enough is named.
	two := night(0, 0, 50, 0, 0, 0)
	if msg := Message(two, testLimits, two.ClearRuns(testLimits), &Plain); !strings.Contains(msg, "Clear 18:15–20:15, 21:15–00:15 (5.0 h)") {
		t.Errorf("stretches missing in\n%s", msg)
	}
}

// The target that qualified survives the space limit, whatever its rank.
func TestQualifiedTargetKept(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0)
	for range 300 {
		r.Targets = append(r.Targets, Target{Name: "M", Type: "G", Description: strings.Repeat("x", 70)})
	}
	r.Targets[250].Name, r.Targets[250].Observable = "last", seen(Span{r.Night.Start, r.Night.End})
	msg := Message(r, testLimits, r.ClearRuns(testLimits), &Telegram)
	if !strings.Contains(msg, ">last<") || !strings.Contains(msg, "251. ") {
		t.Errorf("the qualifying target or its rank is missing in\n%s", msg)
	}
}

func TestShortWindowFloor(t *testing.T) {
	t.Parallel()

	r := night(0)
	r.Night.End = r.Night.Start.Add(5 * time.Minute)
	r.Conditions[0].To = r.Night.End
	r.Targets = []Target{{Name: "M", Observable: seen(Span{r.Night.Start, r.Night.End})}}
	if why := r.Verdict(testLimits, r.ClearRuns(testLimits)); why == "" {
		t.Error("five minutes qualified against -min-clear 2h")
	}

	// A limit below the floor still means what it says.
	l := Limits{Cloud: 10, Seeing: 2, Clear: 5 * time.Minute}
	if why := r.Verdict(l, r.ClearRuns(l)); why != "" {
		t.Errorf("five minutes against -min-clear 5m: %q", why)
	}
}

func TestWebhookRetries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		after    string
		codes    []int
		attempts int
		ok       bool
	}{
		{name: "a 500 passes", codes: []int{500, 200}, attempts: 2, ok: true},
		{name: "a 429 passes", codes: []int{429, 200}, attempts: 2, ok: true},
		{name: "twice is the limit", codes: []int{503, 503, 200}, attempts: 2},
		{name: "a refusal is final", codes: []int{404, 200}, attempts: 1},
		{name: "a long Retry-After is final", codes: []int{429, 200}, after: "3600", attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.after != "" {
					w.Header().Set("Retry-After", tc.after)
				}
				w.WriteHeader(tc.codes[attempts.Add(1)-1])
			}))
			defer srv.Close()

			hook, err := NewWebhook(srv.URL, FormatText, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := hook.Send(context.Background(), "x"); (err == nil) != tc.ok {
				t.Errorf("error %v", err)
			}
			if got := int(attempts.Load()); got != tc.attempts {
				t.Errorf("%d attempts, want %d", got, tc.attempts)
			}
		})
	}
}

func TestWebhookHost(t *testing.T) {
	t.Parallel()

	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) { host = req.Host }))
	defer srv.Close()

	hook, err := NewWebhook(srv.URL, FormatText, []string{"Host: ntfy.internal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if host != "ntfy.internal" {
		t.Errorf("host %q", host)
	}
}

func TestTelegramRetries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		answer   map[string]any
		name     string
		status   int
		attempts int32
		ok       bool
	}{
		{name: "a 500 passes", status: 500, answer: map[string]any{"ok": false, "error_code": 500, "description": "oops"}, attempts: 2, ok: true},
		{name: "a flood limit passes", status: 429, answer: map[string]any{"ok": false, "error_code": 429, "description": "slow", "parameters": map[string]any{"retry_after": 0}}, attempts: 2, ok: true},
		{name: "a long flood limit is final", status: 429, answer: map[string]any{"ok": false, "error_code": 429, "description": "slow", "parameters": map[string]any{"retry_after": 3600}}, attempts: 1},
		{name: "a refusal is final", status: 403, answer: map[string]any{"ok": false, "error_code": 403, "description": "blocked"}, attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.WriteHeader(tc.status)
					_ = json.NewEncoder(w).Encode(tc.answer)

					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
			}))
			defer srv.Close()

			err := SendTelegram(context.Background(), testToken, []string{"1"}, "hi", bot.WithServerURL(srv.URL))
			if (err == nil) != tc.ok {
				t.Errorf("error %v", err)
			}
			if got := attempts.Load(); got != tc.attempts {
				t.Errorf("%d attempts, want %d", got, tc.attempts)
			}
		})
	}
}

// Serial, for openGateway: the parallel tests wait for it.
func TestDiscordGatewayAndRetry(t *testing.T) {
	var opened int
	stub := func(fail error) func() {
		old := openGateway
		openGateway = func(*discordgo.Session) error {
			opened++

			return fail
		}

		return func() { openGateway = old }
	}

	var creates atomic.Int32
	failFirst, failAll := true, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(req.URL.Path, "/users/@me/channels") {
			if creates.Add(1) == 1 && failFirst || failAll {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{}`))

				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1"})
	}))
	defer srv.Close()
	client := func(s *discordgo.Session) {
		s.Client = &http.Client{Transport: toServer{strings.TrimPrefix(srv.URL, "http://")}}
	}

	defer stub(nil)()
	if err := SendDiscord(context.Background(), testToken, []string{"42"}, "hi", client); err != nil {
		t.Errorf("a 500 did not pass: %v", err)
	}
	if opened != 1 || creates.Load() != 2 {
		t.Errorf("opened %d times, %d attempts", opened, creates.Load())
	}

	// A failed connection does not stop the send, and shows only with a failed one.
	broken := errors.New("no gateway")
	defer stub(broken)()
	failFirst = false
	if err := SendDiscord(context.Background(), testToken, []string{"42"}, "hi", client); err != nil {
		t.Errorf("gateway failure stopped the send: %v", err)
	}
	failAll = true
	if err := SendDiscord(context.Background(), testToken, []string{"42"}, "hi", client); !errors.Is(err, broken) {
		t.Errorf("error %v, want the gateway's too", err)
	}
}

func isStatus(err error) bool {
	_, ok := errors.AsType[*statusError](err)

	return ok
}

func TestPeakOnlyWithinTheTime(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0, 0)
	whole := Span{r.Night.Start, r.Night.End}
	r.Targets = []Target{
		{Name: "in", Type: "G", MaxAlt: 60, MaxAt: testStart.Add(time.Hour), Observable: seen(whole)},
		{Name: "out", Type: "G", MaxAlt: 89, MaxAt: testStart.Add(9 * time.Hour), Observable: seen(whole)},
	}
	msg := Message(r, testLimits, r.ClearRuns(testLimits), &Plain)
	if !strings.Contains(msg, "peak 60°") || strings.Contains(msg, "peak 89°") {
		t.Errorf("wrong peaks in\n%s", msg)
	}
}

func TestHalfHourDaylightZone(t *testing.T) {
	t.Parallel()

	// The first row on the hour, a later one at half past: Lord Howe's change.
	r := night(0, 0, 0)
	r.Forecast[1].Hour = r.Forecast[1].Hour.In(time.FixedZone("", 1800))
	if table := forecastTable(r, testLimits, r.times()); !strings.Contains(table, "19:30") {
		t.Errorf("no minutes in\n%s", table)
	}
}

func TestTelegramMigrated(t *testing.T) {
	t.Parallel()

	var chats []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		chat := req.FormValue("chat_id")
		chats = append(chats, chat)
		if chat == "-1" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "error_code": 400, "description": "upgraded", "parameters": map[string]any{"migrate_to_chat_id": -100123},
			})

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer srv.Close()

	if err := SendTelegram(context.Background(), testToken, []string{"-1"}, "hi", bot.WithServerURL(srv.URL)); err != nil {
		t.Errorf("error %v", err)
	}
	if len(chats) != 2 || chats[1] != "-100123" {
		t.Errorf("sent to %v", chats)
	}
}

func TestWebhookTrimsURL(t *testing.T) {
	t.Parallel()

	hook, err := NewWebhook(" https://ntfy.sh/mytopic \n", FormatText, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hook.url != "https://ntfy.sh/mytopic" {
		t.Errorf("url %q", hook.url)
	}
}

// Serial, for openGateway. A long retry_after must not be slept through.
func TestDiscordRateLimit(t *testing.T) {
	old := openGateway
	openGateway = func(*discordgo.Session) error { return nil }
	defer func() { openGateway = old }()

	for _, tc := range []struct {
		name     string
		after    float64
		attempts int32
		ok       bool
	}{
		{name: "a short limit is waited out", after: 0.001, attempts: 2, ok: true},
		{name: "a long limit is final", after: 3600, attempts: 1},
	} {
		var creates atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(req.URL.Path, "/users/@me/channels") && creates.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "slow", "retry_after": tc.after})

				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "1"})
		}))
		start := time.Now()
		err := SendDiscord(context.Background(), testToken, []string{"42"}, "hi", func(s *discordgo.Session) {
			s.Client = &http.Client{Transport: toServer{strings.TrimPrefix(srv.URL, "http://")}}
		})
		srv.Close()
		if (err == nil) != tc.ok || creates.Load() != tc.attempts {
			t.Errorf("%s: error %v after %d attempts", tc.name, err, creates.Load())
		}
		if took := time.Since(start); took > 30*time.Second {
			t.Errorf("%s: took %s", tc.name, took)
		}
	}
}

func TestClearTextTooShort(t *testing.T) {
	t.Parallel()

	r := night(50, 0, 50)
	msg := Message(r, testLimits, r.ClearRuns(testLimits), &Plain)
	if !strings.Contains(msg, "⚠️ Only clear 19:15–20:15") || strings.Contains(msg, "✅") {
		t.Errorf("a short stretch reads as clear in\n%s", msg)
	}
}

func TestMoonDownWording(t *testing.T) {
	t.Parallel()

	r := night(0, 0)
	if msg := Message(r, testLimits, nil, &Plain); !strings.Contains(msg, "down through 20:15") || strings.Contains(msg, "all night") {
		t.Errorf("wrong Moon line in\n%s", msg)
	}
}

// A last target that fits exactly needs no room for "and more".
func TestLastTargetFitsExactly(t *testing.T) {
	t.Parallel()

	r := night(0, 0, 0)
	runs := r.ClearRuns(testLimits)
	c := r.times()
	filler := Target{Name: "M", Type: "G", Description: strings.Repeat("x", 40)}
	last := Target{Name: "L", Type: "G", Description: "x"}
	size := len(header(r, testLimits, runs, &Plain, c))
	lastSize := len(last.block(0, runs, &Plain, c))
	for maxWebhook-size > lastSize+79 {
		r.Targets = append(r.Targets, filler)
		size += len(r.Targets[len(r.Targets)-1].block(len(r.Targets), runs, &Plain, c))
	}
	// Rank digits move the size a little: measure with the real rank.
	last.Description = ""
	r.Targets = append(r.Targets, last)
	base := len(last.block(len(r.Targets), runs, &Plain, c))
	last.Description = "y"
	withOne := len(last.block(len(r.Targets), runs, &Plain, c))
	last.Description = strings.Repeat("y", maxWebhook-size-withOne+1)
	if n := len([]rune(last.Description)); n < 1 || n > maxDesc {
		t.Fatalf("description of %d (base %d)", n, base)
	}
	r.Targets[len(r.Targets)-1] = last
	msg := Message(r, testLimits, runs, &Plain)
	if strings.Contains(msg, "more") {
		t.Errorf("last target dropped with room for it: %d bytes", len(msg))
	}
}

func TestSendSlack(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		sent = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		chat := req.FormValue("channel")
		if req.FormValue("text") != "hi" || req.FormValue("unfurl_links") != "false" || req.FormValue("unfurl_media") != "false" {
			t.Errorf("sent %v", req.Form)
		}
		if got := req.FormValue("token") + req.Header.Get("Authorization"); !strings.Contains(got, testToken) {
			t.Errorf("no token in %q", got)
		}
		mu.Lock()
		sent[chat] = req.FormValue("text")
		mu.Unlock()
		if chat == "gone" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "channel_not_found"})

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": chat, "ts": "1"})
	}))
	defer srv.Close()

	err := SendSlack(context.Background(), testToken, []string{"gone", "C42"}, "hi", slack.OptionAPIURL(srv.URL+"/"))
	if err == nil || !strings.Contains(err.Error(), "slack chat gone") || !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error %v, want the failing chat named", err)
	}
	if sent["C42"] != "hi" || len(sent) != 2 {
		t.Errorf("reached %v, want both", sent)
	}
}

func TestSlackRetries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		first    func(http.ResponseWriter)
		name     string
		attempts int32
		ok       bool
	}{
		{name: "a 500 passes", first: func(w http.ResponseWriter) { w.WriteHeader(500) }, attempts: 2, ok: true},
		{name: "a short limit passes", first: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
		}, attempts: 2, ok: true},
		{name: "a long limit is final", first: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
		}, attempts: 1},
		{name: "a refusal is final", first: func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
		}, attempts: 1},
		{name: "a 404 is final", first: func(w http.ResponseWriter) { w.WriteHeader(404) }, attempts: 1},
		{name: "a transient answer passes", first: func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "internal_error"})
		}, attempts: 2, ok: true},
		{name: "a bare 200 is not delivered", first: func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) }, attempts: 2, ok: true},
		{name: "an overflowing Retry-After is final", first: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "9300000000")
			w.WriteHeader(429)
		}, attempts: 1},
		{name: "a redirect is not followed", first: func(w http.ResponseWriter) {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(302)
		}, attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					tc.first(w)

					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C", "ts": "1"})
			}))
			defer srv.Close()

			err := SendSlack(context.Background(), testToken, []string{"C"}, "hi", slack.OptionAPIURL(srv.URL+"/"))
			if (err == nil) != tc.ok || attempts.Load() != tc.attempts {
				t.Errorf("error %v after %d attempts, want %d", err, attempts.Load(), tc.attempts)
			}
		})
	}
}

func TestSlackNeverPosted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()

	if err := SendSlack(context.Background(), testToken, []string{"C"}, "hi", slack.OptionAPIURL(srv.URL+"/")); !errors.Is(err, errSlackNotPosted) {
		t.Errorf("error %v", err)
	}
}
