// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package notify decides from astro-recommender's -json report whether a
// night is worth an alert, renders the report for each destination and sends
// it to Telegram chats, Discord users, Slack chats and a webhook.
package notify

import (
	"errors"
	"fmt"
	"time"

	"github.com/dkorunic/astro-recommender/internal/num"
	"github.com/dkorunic/astro-recommender/internal/weather"
)

// minNeed is the shortest stretch a window shorter than -min-clear can
// qualify with.
const minNeed = 30 * time.Minute

// ErrLimits is a limit out of range.
var ErrLimits = errors.New("-max-cloud must be 0-100, -max-seeing and -min-clear above 0")

// Report holds the parts of the -json report the alert uses.
type Report struct { // betteralign:ignore
	Message  string `json:"message"` // instead of the rest: no night to report
	Location struct {
		Place    string  `json:"place"`
		TimeZone string  `json:"timeZone"`
		Lat      float64 `json:"lat"`
		Lon      float64 `json:"lon"`
	} `json:"location"`
	Night struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	} `json:"window"`
	Moon struct {
		Illumination float64 `json:"illumination"` // 0-1
		Up           []Span  `json:"up"`
	} `json:"moon"`
	Sky struct {
		ZenithMag float64 `json:"zenithMag"`
		Bortle    int     `json:"bortle"`
	} `json:"sky"`
	Forecast   []Hour      `json:"forecast"`
	Conditions []Condition `json:"conditions"`
	Targets    []Target    `json:"targets"`
}

// Span is a stretch of time, its own UTC offset on each end.
type Span struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Hour is one row of the forecast table; a value is nil when unknown, as an
// hour past the end of the window has no wind or rain.
type Hour struct { // betteralign:ignore
	Hour      time.Time `json:"hour"`
	Cloud     *float64  `json:"cloud"`
	Wind      *float64  `json:"wind"`
	Gust      *float64  `json:"gust"`
	DewSpread *float64  `json:"dewSpread"`
	Precip    *float64  `json:"precipitation"`
}

// Condition is the forecast over a stretch of the window, as the score read
// it; a value is nil when unknown.
type Condition struct { // betteralign:ignore
	Span

	Cloud  *float64 `json:"cloud"`
	Seeing *float64 `json:"seeingArcsec"`
	Precip *float64 `json:"precipitation"`
}

// Target is one listed object.
type Target struct { // betteralign:ignore
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Type          string    `json:"type"`
	Constellation string    `json:"constellation"`
	Filter        string    `json:"filter"`
	RA            float64   `json:"ra"`
	Dec           float64   `json:"dec"`
	Size          *float64  `json:"size"`
	Mag           *float64  `json:"mag"`
	BMag          *float64  `json:"bmag"`
	Score         float64   `json:"score"`
	MaxAlt        float64   `json:"maxAlt"`
	MaxAt         time.Time `json:"maxAt"`
	Observable    Observed  `json:"observable"`
}

// Observed is a target's observable time: the longest run, and every run.
type Observed struct {
	Span

	Spans []Span `json:"spans"`
}

// Limits is what a night has to meet.
type Limits struct {
	Cloud, Seeing float64 // the most cloud (%) and the worst seeing (arcseconds) in clear air
	Clear         time.Duration
}

// Validate rejects limits that could not mean anything: a NaN compares false
// with every forecast, which would stop every alert without a word, and a
// zero stretch would alert on an overcast night.
func (l Limits) Validate() error {
	if !num.Finite(l.Cloud) || !num.Finite(l.Seeing) || l.Cloud < 0 || l.Cloud > 100 || l.Seeing <= 0 || l.Clear <= 0 {
		return ErrLimits
	}

	return nil
}

// ClearRun is an unbroken clear stretch and its worst cloud and seeing.
type ClearRun struct {
	Span

	Cloud, Seeing float64
}

func (s Span) length() time.Duration { return s.To.Sub(s.From) }

// overlap is the time two spans share; of no positive length if none.
func (s Span) overlap(o Span) Span {
	if o.From.After(s.From) {
		s.From = o.From
	}
	if o.To.Before(s.To) {
		s.To = o.To
	}

	return s
}

// clear is whether the stretch meets the limits. An unknown cloud or seeing
// does not, since nothing then says the sky is clear.
func (c *Condition) clear(l Limits) bool {
	return c.Cloud != nil && *c.Cloud <= l.Cloud && c.Seeing != nil && *c.Seeing <= l.Seeing &&
		(c.Precip == nil || *c.Precip < weather.RainShown)
}

// ClearRuns is the window's unbroken clear stretches, in time order.
func (r *Report) ClearRuns(l Limits) []ClearRun {
	var runs []ClearRun
	open := false
	for i := range r.Conditions {
		c := &r.Conditions[i]
		if !c.clear(l) {
			open = false

			continue
		}
		if !open {
			runs = append(runs, ClearRun{Span: c.Span})
			open = true
		}
		run := &runs[len(runs)-1]
		run.To, run.Cloud, run.Seeing = c.To, max(run.Cloud, *c.Cloud), max(run.Seeing, *c.Seeing)
	}

	return runs
}

// longest is the longest of the runs, the earliest of equals; the zero
// ClearRun without any.
func longest(runs []ClearRun) ClearRun {
	var best ClearRun
	for _, run := range runs {
		if run.length() > best.length() {
			best = run
		}
	}

	return best
}

// ForecastKnown is whether the window has any cloud and any seeing forecast.
// Without one a failed fetch would pass for a cloudy night.
func (r *Report) ForecastKnown() bool {
	var cloud, seeing bool
	for i := range r.Conditions {
		cloud, seeing = cloud || r.Conditions[i].Cloud != nil, seeing || r.Conditions[i].Seeing != nil
	}

	return cloud && seeing
}

// Verdict says why the night is not worth a message; "" when it is.
func (r *Report) Verdict(l Limits, runs []ClearRun) string {
	need := l.need(r)
	if best := longest(runs); best.length() < need {
		return fmt.Sprintf("longest clear stretch %s, under %s", best.length(), need)
	}
	if !r.observableInClear(runs, need) {
		return fmt.Sprintf("no target observable for %s of a clear stretch", need)
	}

	return ""
}

// need is how long a stretch has to last: the limit, or a window shorter
// than it (summer, -from/-to), but never less than minNeed of a longer
// limit, or a window of minutes would pass for a clear night.
func (l Limits) need(r *Report) time.Duration {
	return max(min(l.Clear, r.Night.End.Sub(r.Night.Start)), min(l.Clear, minNeed))
}

// clearSpan is the target's longest time in clear air; of no length if none.
func (t *Target) clearSpan(runs []ClearRun) Span {
	var best Span
	for _, span := range t.Observable.Spans {
		for _, run := range runs {
			if o := run.overlap(span); o.length() > best.length() {
				best = o
			}
		}
	}

	return best
}

// observableInClear is whether some target can be imaged for at least need
// in one of the clear stretches.
func (r *Report) observableInClear(runs []ClearRun, need time.Duration) bool {
	for i := range r.Targets {
		if r.Targets[i].clearSpan(runs).length() >= need {
			return true
		}
	}

	return false
}
