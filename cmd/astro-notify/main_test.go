// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/notify"
)

func TestDestinations(t *testing.T) {
	t.Parallel()

	hook := &notify.Webhook{}
	for _, tc := range []struct {
		want error
		o    options
	}{
		{o: options{}, want: errNoDestination},
		{o: options{telegramToken: "t"}, want: errTelegram},
		{o: options{chats: []string{"1"}, hook: hook}, want: errTelegram},
		{o: options{discordToken: "t"}, want: errDiscord},
		{o: options{users: []string{"1"}, hook: hook}, want: errDiscord},
		{o: options{hook: hook}},
		{o: options{telegramToken: "t", chats: []string{"1"}}},
		{o: options{discordToken: "t", users: []string{"1"}}},
	} {
		if err := tc.o.destinations(); !errors.Is(err, tc.want) {
			t.Errorf("%+v: error %v, want %v", tc.o, err, tc.want)
		}
	}
}

func TestSplitList(t *testing.T) {
	t.Parallel()

	if got := splitList(" 1, @two ,,"); len(got) != 2 || got[0] != "1" || got[1] != "@two" {
		t.Errorf("got %q", got)
	}
}

func TestChildEnv(t *testing.T) {
	t.Parallel()

	got := childEnv([]string{"PATH=/bin", telegramTokenEnv + "=a", discordTokenEnv + "=b", webhookURLEnv + "=c", webhookHeaderEnv + "=d", "DARKSKYSITES_API_KEY=e"})
	if len(got) != 2 || got[0] != "PATH=/bin" || got[1] != "DARKSKYSITES_API_KEY=e" {
		t.Errorf("got %q", got)
	}
}

func TestHeaderLines(t *testing.T) {
	t.Parallel()

	got := headerLines("A: b\n\n  \r\nC: d\r\n")
	if len(got) != 2 || got[0] != "A: b" || got[1] != "C: d" {
		t.Errorf("got %q", got)
	}
}

func TestRunAll(t *testing.T) {
	t.Parallel()

	errA := errors.New("a failed")
	stuck := make(chan struct{})
	defer close(stuck)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runAll(ctx, 10*time.Millisecond, []func() error{
		func() error { return errA },
		func() error { <-stuck; return nil },
	})
	if !errors.Is(err, errA) || !errors.Is(err, errUnfinished) {
		t.Errorf("error %v, want the finished one and the unfinished", err)
	}

	if err := runAll(context.Background(), time.Second, []func() error{func() error { return nil }}); err != nil {
		t.Errorf("error %v", err)
	}
}

func TestFetchReportNamesCancel(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "slow")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { //nolint:gosec // a script to run
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := fetchReport(ctx, bin, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error %v, want the deadline named", err)
	}
}
