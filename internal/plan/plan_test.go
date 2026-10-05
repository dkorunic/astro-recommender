// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/scoring"
)

func TestImprovePlan(t *testing.T) {
	// improvePlan fixes the greedy trap: target 0 is best in block 0, but only
	// target 0 can fill block 1 while target 1 nearly matches it in block 0.
	score := [][]float64{{5, 4}, {4.9, 0}}
	greedy := greedyPlan(score, [][]float64{{60, 60}, {60, 0}}, 2)
	if greedy[0] != 0 || greedy[1] != -1 {
		t.Errorf("greedyPlan = %v, want [0 -1]", greedy)
	}
	if got := improvePlan(greedy, score); got[0] != 1 || got[1] != 0 {
		t.Errorf("improvePlan = %v, want [1 0]", got)
	}
}

// A swap that would put a target into a block where it is not observable
// (score 0) must drop it rather than recommend it: X scores 5 in block 0 and
// 10 in block 1, Y scores 0 in block 0 and 3 in block 1.
func TestImprovePlanNoZeroScore(t *testing.T) {
	score := [][]float64{{5, 10}, {0, 3}}
	pick := improvePlan(greedyPlan(score, [][]float64{{60, 60}, {0, 60}}, 2), score)
	for b, j := range pick {
		if j >= 0 && score[j][b] == 0 {
			t.Errorf("block %d got target %d, which scores 0 there (plan %v)", b, j, pick)
		}
	}
	if pick[1] != 0 {
		t.Errorf("plan = %v, want X (0) in block 1 where it scores 10", pick)
	}
}

// A result without per-minute data (scored without perMinute) is a caller
// bug: Make says so instead of indexing out of range or planning nothing.
func TestMakeNoPerMinute(t *testing.T) {
	start := time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC)
	s := scoring.Sky{}
	for i := range 120 {
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
	}
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "perMinute") {
			t.Errorf("Make without per-minute data: recover() = %q, want a perMinute panic", msg)
		}
	}()
	Make(&s, []scoring.Result{{Score: 1, Frame: 1}}, time.Hour)
}

// No observable targets must give empty blocks, not a panic.
func TestMakeNoResults(t *testing.T) {
	start := time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC)
	s := scoring.Sky{}
	for i := range 300 {
		s.Grid = append(s.Grid, start.Add(time.Duration(i)*time.Minute))
	}
	slots := Make(&s, nil, 2*time.Hour)
	if len(slots) != 3 {
		t.Fatalf("got %d slots, want 3", len(slots))
	}
	for _, sl := range slots {
		if sl.Result != nil {
			t.Errorf("slot %v has a target", sl.Start)
		}
	}
}
