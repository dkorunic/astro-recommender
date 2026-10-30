// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package fetch

import (
	"context"
	"time"
)

// Memo remembers a lookup's answer, its error too, for a session that asks
// it again: the terminal view's ← and → recompute every night, while some
// sources depend on the site alone (7Timer, which has no date, the
// DarkSkySites map, a grid cell's elevation). An answer older than the ttl
// given to Get (0: never) is asked again, and so is one a cancelled context
// cut short. A nil Memo asks every time. Callers share the value, so they
// must not modify it.
type Memo[T any] struct {
	at   time.Time
	v    T
	err  error
	lock chan struct{} // one lookup at a time; a channel, so waiting heeds ctx
	ok   bool
}

// NewMemo returns an empty Memo.
func NewMemo[T any]() *Memo[T] { return &Memo[T]{lock: make(chan struct{}, 1)} }

// Get returns the remembered answer while it is younger than ttl (0:
// always), else get's, remembered unless ctx ended first. A caller waiting
// for another's lookup gets its answer.
func (m *Memo[T]) Get(ctx context.Context, ttl time.Duration, get func(context.Context) (T, error)) (T, error) {
	if m == nil {
		return get(ctx)
	}
	select {
	case m.lock <- struct{}{}:
	case <-ctx.Done():
		var zero T

		return zero, ctx.Err()
	}
	defer func() { <-m.lock }()
	if m.ok && (ttl == 0 || time.Since(m.at) < ttl) {
		return m.v, m.err
	}
	v, err := get(ctx)
	if ctx.Err() == nil {
		m.v, m.err, m.at, m.ok = v, err, time.Now(), true
	}

	return v, err
}
