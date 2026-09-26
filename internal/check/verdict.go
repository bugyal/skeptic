// Package check runs the control experiments and classifies the result.
package check

import (
	"fmt"

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
	case VerdictNopPasses, VerdictOracleFails, VerdictBoth, VerdictError:
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
