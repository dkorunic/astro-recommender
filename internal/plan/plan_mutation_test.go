// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package plan

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/dkorunic/astro-recommender/internal/scoring"
)

func mutGrid(n int) *scoring.Sky {
	s := &scoring.Sky{}
	t0 := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	for i := range n {
		s.Grid = append(s.Grid, t0.Add(time.Duration(i)*time.Minute))
	}
	s.Start, s.End = t0, s.Grid[n-1].Add(time.Minute)

	return s
}

func mutResult(name string, w, alt []float64) scoring.Result {
	return scoring.Result{Name: name, Weight: w, Alt: alt, Frame: 1}
}

func TestMutMakeBlocks(t *testing.T) {
	s := mutGrid(10)
	s.End = s.Grid[9].Add(30 * time.Second)
	a := mutResult("A",
		[]float64{1, 1, 0, 1, 0, 0, 0, 0, 0.5, 0.5},
		[]float64{40, 50, 70, 45, 10, 10, 10, 10, 60, 61})
	b := mutResult("B",
		[]float64{0, 0, 0, 0, 0.9, 0.9, 0.9, 0.9, 0, 0},
		[]float64{0, 0, 0, 0, 30, 35, 33, 20, 0, 0})
	b.Frame = 0.5
	p := Make(s, []scoring.Result{a, b}, 4*time.Minute)
	if len(p) != 3 {
		t.Fatalf("%d slots, want 3", len(p))
	}
	if !p[0].Start.Equal(s.Grid[0]) || !p[0].End.Equal(s.Grid[4]) || !p[1].Start.Equal(s.Grid[4]) || !p[2].Start.Equal(s.Grid[8]) {
		t.Errorf("bounds %v-%v %v %v", p[0].Start, p[0].End, p[1].Start, p[2].Start)
	}
	if !p[2].End.Equal(s.End) {
		t.Errorf("last slot ends %v, want the window's end %v", p[2].End, s.End)
	}
	if p[0].Result == nil || p[0].Result.Name != "A" || math.Abs(p[0].Score-3.0/4) > 1e-12 {
		t.Errorf("slot 0 %v %v", p[0].Result, p[0].Score)
	}
	// The peak is over the whole block, observable or not.
	if p[0].PeakAlt != 70 || !p[0].PeakAt.Equal(s.Grid[2]) {
		t.Errorf("slot 0 peak %v at %v", p[0].PeakAlt, p[0].PeakAt)
	}
	// B scores 3.6 × frame 0.5 over 4 minutes.
	if p[1].Result == nil || p[1].Result.Name != "B" || math.Abs(p[1].Score-0.45) > 1e-12 || p[1].PeakAlt != 35 || !p[1].PeakAt.Equal(s.Grid[5]) {
		t.Errorf("slot 1 %v %v %v", p[1].Result, p[1].Score, p[1].PeakAlt)
	}
	// A is used; nothing else is observable in the last block.
	if p[2].Result != nil || p[2].Score != 0 {
		t.Errorf("slot 2 %v", p[2].Result)
	}
	// The short last block (2 minutes) averages over its own length; the
	// first of two equal altitudes is the peak.
	c := mutResult("C",
		[]float64{0, 0, 0, 0, 0, 0, 0, 0, 0.5, 0.5},
		[]float64{0, 0, 0, 0, 0, 0, 0, 0, 42, 42})
	p = Make(s, []scoring.Result{a, b, c}, 4*time.Minute)
	if p[2].Result == nil || p[2].Result.Name != "C" || math.Abs(p[2].Score-0.5) > 1e-12 || !p[2].PeakAt.Equal(s.Grid[8]) {
		t.Errorf("short slot %v %v %v", p[2].Result, p[2].Score, p[2].PeakAt)
	}
	if &p[0].Result.Weight[0] != &a.Weight[0] {
		t.Log("result copied")
	}
	// Below the horizon (with a negative -alt-min) the peak is still found.
	d := mutResult("D", []float64{1, 1, 1, 1, 0, 0, 0, 0, 0, 0}, []float64{-8, -5, -6, -9, 0, 0, 0, 0, 0, 0})
	if p := Make(s, []scoring.Result{d}, 4*time.Minute); p[0].PeakAlt != -5 || !p[0].PeakAt.Equal(s.Grid[1]) {
		t.Errorf("negative peak %v at %v", p[0].PeakAlt, p[0].PeakAt)
	}
	// A block under a minute is one minute.
	if p := Make(s, []scoring.Result{a}, 30*time.Second); len(p) != 10 {
		t.Errorf("sub-minute blocks: %d", len(p))
	}
	defer func() {
		if recover() == nil {
			t.Error("no panic without per-minute data")
		}
	}()
	Make(s, []scoring.Result{{Name: "X"}}, time.Hour)
}

// Equal scores in a block: higher mean altitude over the observable
// minutes (not the whole block) wins; then list order.
func TestMutGreedyTies(t *testing.T) {
	s := mutGrid(4)
	a := mutResult("A", []float64{1, 1, 0, 0}, []float64{40, 40, 89, 89})
	b := mutResult("B", []float64{0, 0, 1, 1}, []float64{10, 10, 50, 50})
	p := Make(s, []scoring.Result{a, b}, 4*time.Minute)
	if p[0].Result.Name != "B" {
		t.Errorf("tie went to %s, want B (mean altitude 50 over 40)", p[0].Result.Name)
	}
	// Equal sums: A's 45° over four minutes beats B's 50° over two only if
	// the mean were over the block; it is over the observable minutes.
	a4 := mutResult("A4", []float64{0.5, 0.5, 0.5, 0.5}, []float64{45, 45, 45, 45})
	if p := Make(s, []scoring.Result{a4, b}, 4*time.Minute); p[0].Result.Name != "B" {
		t.Errorf("mean over observable minutes: got %s", p[0].Result.Name)
	}
	c := mutResult("C", []float64{0, 0, 1, 1}, []float64{10, 10, 50, 50})
	p = Make(s, []scoring.Result{b, c}, 4*time.Minute)
	if p[0].Result.Name != "B" {
		t.Errorf("full tie went to %s, want the first", p[0].Result.Name)
	}
	pick := greedyPlan([][]float64{{0, 5}, {3, 4}}, [][]float64{{0, 50}, {40, 40}}, 2)
	if pick[0] != 1 || pick[1] != 0 {
		t.Errorf("greedy %v, want [1 0]", pick)
	}
	if pick := greedyPlan([][]float64{{0}}, [][]float64{{0}}, 1); pick[0] != -1 {
		t.Errorf("zero score picked: %v", pick)
	}
}

// Greedy takes A for block 0 and leaves block 1 empty; swapping gives
// B (9.5) + A (9).
func TestMutImproveSwap(t *testing.T) {
	score := [][]float64{{10, 9}, {9.5, 0}}
	pick := improvePlan(greedyPlan(score, [][]float64{{1, 1}, {1, 1}}, 2), score)
	if pick[0] != 1 || pick[1] != 0 {
		t.Errorf("pick %v, want [1 0]", pick)
	}
	// An unused target better for a block replaces the incumbent.
	pick = improvePlan([]int{0, -1}, [][]float64{{1, 0}, {5, 0}, {0, 2}})
	if pick[0] != 1 || pick[1] != 2 {
		t.Errorf("pick %v, want [1 2]", pick)
	}
	// Never move a target into a block where it scores 0: B is better in
	// block 0, and A, worthless in block 1, leaves the plan.
	pick = improvePlan([]int{0, 1}, [][]float64{{1, 0}, {5, 1}})
	if pick[0] != 1 || pick[1] != -1 {
		t.Errorf("pick %v, want [1 -1]", pick)
	}
	if got := improvePlan(nil, nil); len(got) != 0 {
		t.Errorf("empty %v", got)
	}
}

func mutTotal(pick []int, score [][]float64) float64 {
	var t float64
	for b, j := range pick {
		if j >= 0 {
			t += score[j][b]
		}
	}

	return t
}

// mutBest is the global optimum by brute force (each target at most once).
func mutBest(score [][]float64, blocks int) float64 {
	used := make([]bool, len(score))
	var rec func(b int) float64
	rec = func(b int) float64 {
		if b == blocks {
			return 0
		}
		best := rec(b + 1)
		for j := range score {
			if !used[j] && score[j][b] > 0 {
				used[j] = true
				best = max(best, score[j][b]+rec(b+1))
				used[j] = false
			}
		}

		return best
	}

	return rec(0)
}

// Random plans: each target at most once, never where it scores 0, at least
// the greedy total, at most the optimum, and no single swap or replacement
// improves it.
func TestMutImproveInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	better := 0
	for range 400 {
		nt, nb := 1+rng.IntN(5), 1+rng.IntN(5)
		score := make([][]float64, nt)
		alt := make([][]float64, nt)
		for j := range score {
			score[j], alt[j] = make([]float64, nb), make([]float64, nb)
			for b := range score[j] {
				if rng.Float64() < 0.6 {
					score[j][b] = float64(1 + rng.IntN(20))
				}
				alt[j][b] = float64(rng.IntN(90))
			}
		}
		g := greedyPlan(score, alt, nb)
		gt := mutTotal(g, score)
		pick := improvePlan(append([]int(nil), g...), score)
		tot := mutTotal(pick, score)
		if tot < gt-1e-9 || tot > mutBest(score, nb)+1e-9 {
			t.Fatalf("score %v: total %v, greedy %v, best %v", score, tot, gt, mutBest(score, nb))
		}
		if tot > gt+1e-9 {
			better++
		}
		seen := map[int]bool{}
		for b, j := range pick {
			if j < 0 {
				continue
			}
			if seen[j] || score[j][b] == 0 {
				t.Fatalf("score %v: pick %v reuses or misplaces %d", score, pick, j)
			}
			seen[j] = true
		}
		for b := range pick {
			for j := range score {
				cur := 0.0
				if pick[b] >= 0 {
					cur = score[pick[b]][b]
				}
				if !seen[j] && score[j][b] > cur+1e-9 {
					t.Fatalf("score %v: pick %v, unused %d improves block %d", score, pick, j, b)
				}
			}
		}
		for a := range pick {
			for b := range pick {
				if a == b || pick[a] < 0 || pick[b] < 0 || score[pick[a]][b] == 0 || score[pick[b]][a] == 0 {
					continue
				}
				if score[pick[a]][b]+score[pick[b]][a] > score[pick[a]][a]+score[pick[b]][b]+1e-9 {
					t.Fatalf("score %v: pick %v, swapping %d and %d improves", score, pick, a, b)
				}
			}
		}
	}
	if better == 0 {
		t.Error("local search never beat greedy")
	}
}
