// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package solar

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Both of NOAA's lists parse into month → sfu, skipping its -1 for none and
// anything implausible; a list without a usable month is an error.
func TestParse(t *testing.T) {
	observed := `[{"time-tag":"1749-01","ssn":96.7,"f10.7":-1.0},
		{"time-tag":"2026-08","ssn":70.1,"f10.7":116.2},
		{"time-tag":"2026-09","ssn":60.3,"f10.7":105.41},
		{"time-tag":"2026-13","f10.7":120},
		{"time-tag":"2026-07","f10.7":5000}]`
	got, err := parse([]byte(observed))
	if err != nil || len(got) != 2 || got["2026-09"] != 105.41 || got["2026-08"] != 116.2 {
		t.Errorf("observed: %v, %v", got, err)
	}
	predicted := `[{"time-tag":"2026-10","predicted_ssn":85.0,"predicted_f10.7":125.9}]`
	if got, err := parse([]byte(predicted)); err != nil || got["2026-10"] != 125.9 {
		t.Errorf("predicted: %v, %v", got, err)
	}
	for _, bad := range []string{`[]`, `[{"time-tag":"2026-10","f10.7":-1}]`, `<html>busy</html>`, `{"f10.7":100}`} {
		if got, err := parse([]byte(bad)); err == nil {
			t.Errorf("%s parsed: %v", bad, got)
		}
	}
}

// A night takes its UTC month's observed mean, else the last 30 days' mean
// until 30 days after them, else the month's prediction; none is an error
// carrying the fetch failures.
func TestPick(t *testing.T) {
	obs := map[string]float64{"2026-09": 105.4}
	pred := map[string]float64{"2026-09": 130, "2026-10": 125.9, "2026-11": 124}
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 20, 0, 0, 0, time.UTC) }
	latest := recent{first: day(9, 15), last: day(10, 14), mean: 107.3}
	oct1 := time.Date(2026, 9, 30, 23, 30, 0, 0, time.FixedZone("UTC-2", -2*3600)) // 1 October UTC
	for _, c := range []struct {
		t      time.Time
		latest recent
		want   float64
		basis  string
	}{
		{day(9, 20), latest, 105.4, Monthly},
		{oct1, latest, 107.3, Recent},
		{day(11, 12), latest, 107.3, Recent},
		{day(11, 14), latest, 124, Predicted},
		{oct1, recent{}, 125.9, Predicted},
	} {
		if v, basis, err := pick(obs, c.latest, pred, c.t, nil); v != c.want || basis != c.basis || err != nil {
			t.Errorf("%v: %v %q %v, want %v %q", c.t, v, basis, err, c.want, c.basis)
		}
	}
	down := errors.New("timeout")
	if _, _, err := pick(nil, recent{}, nil, oct1, down); !errors.Is(err, errNoFlux) || !errors.Is(err, down) {
		t.Errorf("no lists: %v", err)
	}
	if _, _, err := pick(obs, latest, pred, time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, errNoFlux) {
		t.Errorf("2040: %v", err)
	}
}

// The 30-day list's mean spans its first to last day; a bad or missing day
// rejects the list rather than averaging over a gap.
func TestParseRecent(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for d := 1; d <= 30; d++ {
		if d > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"time_tag":"2026-09-%02dT20:00:00","flux":%d}`, d, 100+d%2*10)
	}
	b.WriteString("]")
	r, err := parseRecent([]byte(b.String()))
	if err != nil || r.mean != 105 || r.first.Day() != 1 || r.last.Day() != 30 {
		t.Errorf("30 days: %+v, %v", r, err)
	}
	for _, bad := range []string{`[{"time_tag":"2026-09-01T20:00:00","flux":100}]`, `[{"time_tag":"x","flux":100}]`, `[{"time_tag":"2026-09-01T20:00:00","flux":-1}]`, `{}`} {
		if r, err := parseRecent([]byte(bad)); err == nil {
			t.Errorf("%s parsed: %+v", bad, r)
		}
	}
}
