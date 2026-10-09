// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package plan splits the observing window into blocks and assigns one target
// to each.
package plan

import (
	"cmp"
	"fmt"
	"time"

	"github.com/dkorunic/astro-recommender/internal/scoring"
)

// Slot is one block of the night plan; Result is nil when nothing is observable.
type Slot struct {
	Start, End time.Time
	PeakAt     time.Time
	Result     *scoring.Result
	Score      float64
	PeakAlt    float64
}

// Make splits the window into blocks of length block and assigns each
// block one target: greedily first, then improved by local search. Results
// must carry per-minute Alt and Weight over s.Grid (scoring.Score with
// perMinute); one without them is a caller bug and panics.
func Make(s *scoring.Sky, results []scoring.Result, block time.Duration) []Slot {
	// Below a minute the step would be 0 and the bounds loop endless.
	n := max(1, int(block/time.Minute))
	var bounds [][2]int
	for lo := 0; lo < len(s.Grid); lo += n {
		bounds = append(bounds, [2]int{lo, min(lo+n, len(s.Grid))})
	}
	// score[j][b]: target j's summed weight over block b (x framing);
	// meanAlt[j][b]: its mean altitude there while observable.
	score := make([][]float64, len(results))
	meanAlt := make([][]float64, len(results))
	for j, r := range results {
		score[j] = make([]float64, len(bounds))
		meanAlt[j] = make([]float64, len(bounds))
		if len(r.Weight) != len(s.Grid) || len(r.Alt) != len(s.Grid) {
			panic(fmt.Sprintf("plan.Make: %s has no per-minute data for the grid; score with perMinute", r.Name))
		}
		for b, bd := range bounds {
			var sum, altSum float64
			var good int
			for i, w := range r.Weight[bd[0]:bd[1]] {
				if w > 0 {
					sum += w
					altSum += r.Alt[bd[0]+i]
					good++
				}
			}
			if good > 0 {
				score[j][b], meanAlt[j][b] = sum*r.Frame, altSum/float64(good)
			}
		}
	}

	pick := improvePlan(greedyPlan(score, meanAlt, len(bounds)), score)

	plan := make([]Slot, len(bounds))
	for b, bd := range bounds {
		lo, hi := bd[0], bd[1]
		// A minute past the last sample, but never past the window's end,
		// which a -from/-to clip in a zone with an odd offset can put earlier.
		plan[b] = Slot{Start: s.Grid[lo], End: s.Grid[hi-1].Add(time.Minute)}
		if plan[b].End.After(s.End) {
			plan[b].End = s.End
		}
		if pick[b] < 0 {
			continue
		}
		p := &plan[b]
		p.Result = &results[pick[b]]
		p.Score = score[pick[b]][b] / float64(hi-lo)
		p.PeakAlt = -90
		for i := lo; i < hi; i++ {
			if a := p.Result.Alt[i]; a > p.PeakAlt {
				p.PeakAlt, p.PeakAt = a, s.Grid[i]
			}
		}
	}

	return plan
}

// greedyPlan gives each block, in time order, the unused target with the best
// score; mean altitude breaks the ties that equal weights produce. Targets are
// in ranking order, so remaining ties go to the better target overall.
// Returns the target index per block, -1 for none.
func greedyPlan(score, meanAlt [][]float64, blocks int) []int {
	used := make([]bool, len(score))
	pick := make([]int, blocks)
	for b := range pick {
		pick[b] = -1
		for j := range score {
			if used[j] || score[j][b] == 0 {
				continue
			}
			if pick[b] < 0 || cmp.Or(cmp.Compare(score[pick[b]][b], score[j][b]), cmp.Compare(meanAlt[pick[b]][b], meanAlt[j][b])) < 0 {
				pick[b] = j
			}
		}
		if pick[b] >= 0 {
			used[pick[b]] = true
		}
	}

	return pick
}

// improvePlan is a local search after the greedy pass, as in astrogo's
// SwapOptimizedStrategy: swap the targets of two blocks, or bring in an unused
// target, whenever the total score grows. Monotonic, so it terminates;
// a local optimum, not a global one.
func improvePlan(pick []int, score [][]float64) []int {
	if len(pick) == 0 {
		return pick
	}
	sc := func(j, b int) float64 {
		if j < 0 {
			return 0
		}

		return score[j][b]
	}
	used := make([]bool, len(score))
	for _, j := range pick {
		if j >= 0 {
			used[j] = true
		}
	}
	// bestUnused returns the best unused target for block b, -1 for none.
	bestUnused := func(b int) int {
		best := -1
		for j := range score {
			if !used[j] && score[j][b] > sc(best, b) {
				best = j
			}
		}

		return best
	}
	// Every accepted move raises the bounded total by more than eps, so the
	// passes end.
	const eps = 1e-9
	for {
		changed := false
		// Move block a's target to block b and b's target (if any) to a. A
		// target is never placed where it is not observable: if b's target
		// would score 0 in a it leaves the plan instead, and a block left
		// empty is refilled with the best unused target.
		for a := range pick {
			for b := range pick {
				nb := pick[a]
				if a == b || nb < 0 || sc(nb, b) == 0 {
					continue
				}
				na := pick[b]
				if na >= 0 && sc(na, a) == 0 {
					na = -1
				}
				if na < 0 {
					na = bestUnused(a)
				}
				if sc(na, a)+sc(nb, b) > sc(pick[a], a)+sc(pick[b], b)+eps {
					if pick[b] >= 0 && pick[b] != na {
						used[pick[b]] = false
					}
					if na >= 0 {
						used[na] = true
					}
					pick[a], pick[b] = na, nb
					changed = true
				}
			}
		}
		for b := range pick {
			for j := range score {
				if !used[j] && score[j][b] > sc(pick[b], b)+eps {
					if pick[b] >= 0 {
						used[pick[b]] = false
					}
					pick[b], used[j] = j, true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return pick
}
