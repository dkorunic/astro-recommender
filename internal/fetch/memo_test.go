// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package fetch

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A failed refresh keeps the last answer beside its error, also while the
// failure is remembered, and the next success replaces it.
func TestMemoRefreshFailureKeepsAnswer(t *testing.T) {
	ctx := context.Background()
	errDown := errors.New("down")
	m := NewMemo[int]()
	answer := func(v int, err error) func(context.Context) (int, error) {
		return func(context.Context) (int, error) { return v, err }
	}
	if v, err := m.Get(ctx, time.Hour, answer(1, nil)); v != 1 || err != nil {
		t.Fatalf("first Get = %d, %v", v, err)
	}
	m.at = m.at.Add(-time.Hour)
	if v, err := m.Get(ctx, time.Hour, answer(0, errDown)); v != 1 || !errors.Is(err, errDown) {
		t.Errorf("failed refresh Get = %d, %v; want the last answer, 1, down", v, err)
	}
	if v, err := m.Get(ctx, time.Hour, answer(9, nil)); v != 1 || !errors.Is(err, errDown) {
		t.Errorf("within errTTL Get = %d, %v; want the remembered 1, down", v, err)
	}
	m.at = m.at.Add(-errTTL)
	if v, err := m.Get(ctx, time.Hour, answer(0, errDown)); v != 1 || !errors.Is(err, errDown) {
		t.Errorf("second failed refresh Get = %d, %v; want the last answer, 1, down", v, err)
	}
	m.at = m.at.Add(-errTTL)
	if v, err := m.Get(ctx, time.Hour, answer(2, nil)); v != 2 || err != nil {
		t.Errorf("after the failures Get = %d, %v; want 2, nil", v, err)
	}
}

// A failure stands for errTTL at most, whatever the ttl; an answer for the ttl.
func TestMemoErrorExpires(t *testing.T) {
	ctx := context.Background()
	for _, ttl := range []time.Duration{0, time.Hour} {
		calls := 0
		m := NewMemo[int]()
		get := func(context.Context) (int, error) {
			calls++
			if calls == 1 {
				return 0, errors.New("down")
			}

			return calls, nil
		}
		if _, err := m.Get(ctx, ttl, get); err == nil {
			t.Fatal("first lookup gave no error")
		}
		if _, err := m.Get(ctx, ttl, get); err == nil || calls != 1 {
			t.Errorf("ttl %v: within errTTL err %v after %d calls; want the failure remembered", ttl, err, calls)
		}
		m.at = m.at.Add(-errTTL)
		if v, err := m.Get(ctx, ttl, get); err != nil || v != 2 {
			t.Errorf("ttl %v: past errTTL Get = %d, %v; want asked again, 2", ttl, v, err)
		}
		m.at = m.at.Add(-errTTL)
		if ttl == 0 {
			if v, _ := m.Get(ctx, ttl, get); v != 2 {
				t.Errorf("an answer past errTTL Get = %d; want it kept, 2", v)
			}
		}
	}
}

// A Memo asks once, its error too, until the ttl; a cancelled lookup is not
// remembered; a nil Memo asks every time.
func TestMemo(t *testing.T) {
	calls := 0
	errDown := errors.New("down")
	get := func(context.Context) (int, error) {
		calls++

		return calls, errDown
	}
	ctx := context.Background()
	m := NewMemo[int]()
	for range 3 {
		if v, err := m.Get(ctx, 0, get); v != 1 || !errors.Is(err, errDown) {
			t.Fatalf("Get = %d, %v; want the first answer, 1, down", v, err)
		}
	}
	if v, _ := m.Get(ctx, time.Nanosecond, get); v != 2 || calls != 2 {
		t.Errorf("past its ttl Get = %d after %d calls; want asked again, 2", v, calls)
	}

	cancelled, cancel := context.WithCancel(ctx)
	fresh := NewMemo[int]()
	if _, err := fresh.Get(cancelled, 0, func(context.Context) (int, error) { cancel(); return 7, context.Canceled }); err == nil {
		t.Fatal("cancelled lookup gave no error")
	}
	if v, _ := fresh.Get(ctx, 0, func(context.Context) (int, error) { return 8, nil }); v != 8 {
		t.Errorf("after a cancelled lookup Get = %d; want asked again, 8", v)
	}

	var none *Memo[int]
	none.Get(ctx, 0, get)
	none.Get(ctx, 0, get)
	if calls != 4 {
		t.Errorf("nil Memo made %d calls in all; want every Get asked (4)", calls)
	}
}

// A caller waiting for another's lookup gives up when its own context ends.
func TestMemoWaitCancelled(t *testing.T) {
	m := NewMemo[int]()
	release := make(chan struct{})
	started := make(chan struct{})
	go m.Get(context.Background(), 0, func(context.Context) (int, error) {
		close(started)
		<-release

		return 1, nil
	})
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Get(ctx, 0, func(context.Context) (int, error) { return 2, nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("waiting Get = %v; want context.Canceled", err)
	}
	close(release)
	if v, _ := m.Get(context.Background(), 0, func(context.Context) (int, error) { return 3, nil }); v != 1 {
		t.Errorf("Get after the first lookup = %d; want its answer, 1", v)
	}
}
