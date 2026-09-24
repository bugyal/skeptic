package check

import (
	"testing"

	"github.com/bugyal/skeptic/internal/task"
)

func ctl(c Control, score *float64, errMsg string) *ControlResult {
	return &ControlResult{Control: c, Score: score, Error: errMsg}
}

func f(v float64) *float64 { return &v }

func TestClassify(t *testing.T) {
	withSolution := &task.Task{Solution: task.Solution{Kind: task.SolutionScript}}
	noSolution := &task.Task{Solution: task.Solution{Kind: task.SolutionNone}}

	for _, tc := range []struct {
		name   string
		task   *task.Task
		nop    *ControlResult
		oracle *ControlResult
		want   Verdict
	}{
		{"clean", withSolution, ctl(ControlNop, f(0), ""), ctl(ControlOracle, f(1), ""), VerdictClean},
		{"nop passes", withSolution, ctl(ControlNop, f(1), ""), ctl(ControlOracle, f(1), ""), VerdictNopPasses},
		{"nop partially passes", withSolution, ctl(ControlNop, f(0.2), ""), ctl(ControlOracle, f(1), ""), VerdictNopPasses},
		{"oracle fails", withSolution, ctl(ControlNop, f(0), ""), ctl(ControlOracle, f(0), ""), VerdictOracleFails},
		{"oracle partially passes", withSolution, ctl(ControlNop, f(0), ""), ctl(ControlOracle, f(0.9), ""), VerdictOracleFails},
		{"both", withSolution, ctl(ControlNop, f(0.5), ""), ctl(ControlOracle, f(0.5), ""), VerdictBoth},
		{"no solution", noSolution, ctl(ControlNop, f(0), ""), nil, VerdictNoOracle},

		// An error in either control must win over any score-based verdict:
		// a task that could not be checked has no verdict to report.
		{"nop errored", withSolution, ctl(ControlNop, nil, "boom"), ctl(ControlOracle, f(1), ""), VerdictError},
		{"oracle errored", withSolution, ctl(ControlNop, f(0), ""), ctl(ControlOracle, nil, "boom"), VerdictError},
		{"error beats bad scores", withSolution, ctl(ControlNop, f(1), ""), ctl(ControlOracle, nil, "boom"), VerdictError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := classify(tc.task, tc.nop, tc.oracle)
			if got != tc.want {
				t.Fatalf("classify = %s, want %s", got, tc.want)
			}
		})
	}
}

// An unsupported task must never be scored, whatever the controls returned.
func TestClassifyUnsupported(t *testing.T) {
	ut := &task.Task{Unsupported: "multi-container task (3 compose services)"}
	got, reason := classify(ut, ctl(ControlNop, f(1), ""), ctl(ControlOracle, f(0), ""))
	if got != VerdictUnsupported {
		t.Fatalf("classify = %s, want UNSUPPORTED", got)
	}
	if reason != ut.Unsupported {
		t.Fatalf("reason = %q, want the adapter's explanation", reason)
	}
}

// Only genuine defects should fail a CI run. NO_ORACLE and UNSUPPORTED report
// the limits of what could be checked, not a problem found.
func TestFlagged(t *testing.T) {
	for v, want := range map[Verdict]bool{
		VerdictClean:       false,
		VerdictNoOracle:    false,
		VerdictUnsupported: false,
		VerdictNopPasses:   true,
		VerdictOracleFails: true,
		VerdictBoth:        true,
		VerdictError:       true,
	} {
		if got := v.Flagged(); got != want {
			t.Errorf("%s.Flagged() = %v, want %v", v, got, want)
		}
	}
}

// A single-control run must still classify without panicking on the nil half.
func TestClassifyOnlyOneControl(t *testing.T) {
	withSolution := &task.Task{Solution: task.Solution{Kind: task.SolutionScript}}
	if got, _ := classify(withSolution, ctl(ControlNop, f(1), ""), nil); got != VerdictNopPasses {
		t.Errorf("nop-only run: got %s, want NOP_PASSES", got)
	}
	if got, _ := classify(withSolution, nil, ctl(ControlOracle, f(0), "")); got != VerdictOracleFails {
		t.Errorf("oracle-only run: got %s, want ORACLE_FAILS", got)
	}
}
