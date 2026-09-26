// Package check runs the control experiments and classifies the result.
package check

import (
	"fmt"
	"strings"

	"github.com/bugyal/skeptic/internal/task"
)

// Control names one experiment run against a task.
type Control string

const (
	// ControlNop changes nothing. Its score must be 0.0: a test that rewards
	// an untouched workspace is not grading the change.
	ControlNop Control = "nop"
	// ControlOracle applies the reference solution. Its score must be 1.0:
	// if the known-correct answer cannot pass, nothing can.
	ControlOracle Control = "oracle"
	// ControlPartial applies the reference solution with one hunk withheld.
	// Its score must drop below 1.0: a suite that still awards full marks was
	// never grading the withheld change. Unlike the other two this is a
	// heuristic, so it warns and never fails a run. See docs/decisions.md D5.
	ControlPartial Control = "partial"
)

// Verdict is the classification of one task.
type Verdict string

const (
	VerdictClean       Verdict = "CLEAN"
	VerdictNopPasses   Verdict = "NOP_PASSES"
	VerdictOracleFails Verdict = "ORACLE_FAILS"
	VerdictBoth        Verdict = "BOTH"
	// VerdictFlaky marks a task whose score changed between identical runs
	// under --repeat. Its grading is not a function of the answer alone, so
	// no single score from it can be trusted. See docs/decisions.md D18.
	VerdictFlaky Verdict = "FLAKY"
	// VerdictNoOracle marks a task shipping no reference solution. Upstream
	// documents solution/ as optional, so this is a reported fact, not a
	// defect, and it does not fail a run. See docs/decisions.md D3.
	VerdictNoOracle Verdict = "NO_ORACLE"
	// VerdictUnsupported marks a task Skeptic recognised but cannot run
	// faithfully. Excluded from totals rather than scored wrongly.
	VerdictUnsupported Verdict = "UNSUPPORTED"
	// VerdictError covers build failures, timeouts and unreadable scores.
	// It is never folded into pass or fail.
	VerdictError Verdict = "ERROR"
)

// Flagged reports whether a verdict means the task should block a CI run.
// NO_ORACLE and UNSUPPORTED are deliberately excluded: both describe a limit
// of what could be checked, not a defect found.
func (v Verdict) Flagged() bool {
	switch v {
	case VerdictNopPasses, VerdictOracleFails, VerdictBoth, VerdictFlaky, VerdictError:
		return true
	}
	return false
}

// Reason renders a short human explanation of a verdict.
func (v Verdict) Reason(r TaskResult) string {
	switch v {
	case VerdictClean:
		return "oracle 1.00, nop 0.00"
	case VerdictNopPasses:
		return "tests pass without any change (nop " + fmtScore(r.NopScore()) + ")"
	case VerdictOracleFails:
		return "reference solution does not pass (oracle " + fmtScore(r.OracleScore()) + ")"
	case VerdictBoth:
		return "nop " + fmtScore(r.NopScore()) + ", oracle " + fmtScore(r.OracleScore())
	case VerdictFlaky:
		return "scores differ between identical runs: " + runScores(r)
	case VerdictNoOracle:
		return "no reference solution; oracle not run"
	case VerdictUnsupported:
		return r.Unsupported
	case VerdictError:
		return r.Error
	}
	return ""
}

// classify applies the rule the whole tool exists to enforce:
// oracle must score 1.0 and nop must score 0.0.
func classify(t *task.Task, nop, oracle *ControlResult) (Verdict, string) {
	if t.Unsupported != "" {
		return VerdictUnsupported, t.Unsupported
	}
	// An error in any control makes the task's verdict unknowable. Reporting
	// it as anything else would be the silent miscount this tool exists to
	// prevent.
	for _, c := range []*ControlResult{nop, oracle} {
		if c != nil && c.Error != "" {
			return VerdictError, c.Error
		}
	}

	nopBad := nop != nil && nop.Score != nil && *nop.Score > 0
	oracleBad := oracle != nil && oracle.Score != nil && *oracle.Score < 1

	// A reference solution whose tests could not reach the network has not
	// been shown to fail; the host has. Reporting ORACLE_FAILS there would
	// flag a benchmark for this machine's firewall. A nop that passes is still
	// earned -- a missing network cannot make a test pass -- so it stands.
	if oracleBad {
		if line := networkFailure(oracle.combined); line != "" {
			if nopBad {
				return VerdictNopPasses, ""
			}
			return VerdictError, fmt.Sprintf(
				"oracle scored %s, but its tests could not reach the network, so the score says nothing about the solution: %s",
				fmtScore(oracle.Score), line)
		}
	}

	switch {
	case nopBad && oracleBad:
		return VerdictBoth, ""
	case nopBad:
		return VerdictNopPasses, ""
	case oracleBad:
		return VerdictOracleFails, ""
	}

	// No fault found. Say so only when the oracle actually ran; otherwise the
	// task is unproven, not clean.
	if oracle == nil && !t.Solution.Available() {
		return VerdictNoOracle, ""
	}
	return VerdictClean, ""
}

// classifyRuns classifies a task from every run of its controls. Each run is
// first classified on its own, so anything that makes one run unreadable --
// an error, a memory kill, a starved network -- makes the task ERROR rather
// than FLAKY: a score that disagrees because the host failed is the host's
// finding, not the benchmark's. Only then are the scores compared.
func classifyRuns(t *task.Task, nops, oracles []*ControlResult) (Verdict, string) {
	n := max(len(nops), len(oracles), 1)
	at := func(rs []*ControlResult, i int) *ControlResult {
		if i < len(rs) {
			return rs[i]
		}
		return nil
	}
	first, firstErr := classify(t, at(nops, 0), at(oracles, 0))
	if first == VerdictUnsupported || first == VerdictError {
		return first, firstErr
	}
	for i := 1; i < n; i++ {
		if v, err := classify(t, at(nops, i), at(oracles, i)); v == VerdictError {
			return v, err
		}
	}
	if !sameScores(nops) || !sameScores(oracles) {
		return VerdictFlaky, ""
	}
	return first, firstErr
}

// sameScores reports whether every run produced the same score. Exact
// comparison is deliberate: scores are ratios of test counts, and 0.98
// against 1.00 is one test that passed once and failed once.
func sameScores(rs []*ControlResult) bool {
	for _, r := range rs[min(len(rs), 1):] {
		a, b := r.Score, rs[0].Score
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			return false
		}
	}
	return true
}

func allScore(rs []*ControlResult, want float64) bool {
	for _, r := range rs {
		if r.Error != "" || r.Score == nil || *r.Score != want {
			return false
		}
	}
	return true
}

// runScores renders every run's score per control, e.g.
// "nop 0.00, 0.00, 0.00; oracle 1.00, 0.00, 1.00".
func runScores(r TaskResult) string {
	var parts []string
	for _, c := range []struct {
		name string
		runs []*ControlResult
	}{{"nop", r.NopRuns}, {"oracle", r.OracleRuns}} {
		if len(c.runs) == 0 {
			continue
		}
		scores := make([]string, len(c.runs))
		for i, run := range c.runs {
			scores[i] = fmtScore(run.Score)
		}
		parts = append(parts, c.name+" "+strings.Join(scores, ", "))
	}
	return strings.Join(parts, "; ")
}
