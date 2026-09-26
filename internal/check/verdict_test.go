package check

import (
	"strings"
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

// withOutput attaches captured test output to a control result.
func withOutput(c *ControlResult, out string) *ControlResult {
	c.combined = out
	return c
}

// Lines from the oracle run of psf__requests-1921 on a host whose egress went
// through a TLS-intercepting proxy. The instance is CLEAN with working network;
// see results/swe-bench-verified/2026-09-26-native-repro.
const requestsBehindProxy = `PASSED test_requests.py::RequestsTestCase::test_basic_building
E           requests.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1147)
FAILED test_requests.py::RequestsTestCase::test_DIGESTAUTH_WRONG_HTTP_401_GET
FAILED test_requests.py::RequestsTestCase::test_HTTP_200_OK_HEAD - assert 403...`

// A failure caused by the host's network is not a finding about the benchmark.
// It must be ERROR, never ORACLE_FAILS, and never CLEAN.
func TestOracleFailingOnNetworkIsAnError(t *testing.T) {
	task := &task.Task{Solution: task.Solution{Kind: task.SolutionPatch}}
	got, reason := classify(task, ctl(ControlNop, f(0), ""),
		withOutput(ctl(ControlOracle, f(0), ""), requestsBehindProxy))
	if got != VerdictError {
		t.Fatalf("classify = %s, want ERROR", got)
	}
	if !strings.Contains(reason, "CERTIFICATE_VERIFY_FAILED") || !strings.Contains(reason, "oracle scored 0.00") {
		t.Errorf("reason should carry the score and the evidence, got %q", reason)
	}
}

// The signal this rule must not swallow: a reference solution that genuinely
// fails, with nothing about the network in its output, is still ORACLE_FAILS.
func TestGenuineOracleFailureStaysFlagged(t *testing.T) {
	task := &task.Task{Solution: task.Solution{Kind: task.SolutionPatch}}
	out := `FAILED tests/test_parser.py::test_nested - AssertionError: assert 3 == 4
ConnectionRefusedError: [Errno 111] Connection refused
E   TimeoutError: timed out`
	got, _ := classify(task, ctl(ControlNop, f(0), ""), withOutput(ctl(ControlOracle, f(0), ""), out))
	if got != VerdictOracleFails {
		t.Fatalf("classify = %s, want ORACLE_FAILS: local servers and timeouts are not the host's network", got)
	}
}

// A passing nop is earned whatever the network did, so it is still reported.
func TestNopPassingSurvivesNetworkFailure(t *testing.T) {
	task := &task.Task{Solution: task.Solution{Kind: task.SolutionPatch}}
	got, _ := classify(task, ctl(ControlNop, f(1), ""), withOutput(ctl(ControlOracle, f(0), ""), requestsBehindProxy))
	if got != VerdictNopPasses {
		t.Fatalf("classify = %s, want NOP_PASSES", got)
	}
}

// Network noise in a passing run changes nothing: the rule only questions a
// bad oracle score, it never invents one.
func TestNetworkNoiseInPassingRunIsIgnored(t *testing.T) {
	task := &task.Task{Solution: task.Solution{Kind: task.SolutionPatch}}
	got, _ := classify(task, ctl(ControlNop, f(0), ""), withOutput(ctl(ControlOracle, f(1), ""), requestsBehindProxy))
	if got != VerdictClean {
		t.Fatalf("classify = %s, want CLEAN", got)
	}
}

func TestNetworkFailureSignatures(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		want      bool
	}{
		{"tls interception", requestsBehindProxy, true},
		{"dns", "socket.gaierror: [Errno -3] Temporary failure in name resolution", true},
		{"no route", "OSError: [Errno 113] No route to host", true},
		{"curl", "curl: (6) Could not resolve host: example.com", true},
		{"node", "Error: getaddrinfo ENOTFOUND registry.example.com", true},
		// Test names are lowercase and describe network behaviour on purpose.
		{"test names", "PASSED tests/test_ssl.py::test_certificate_verify_failed_is_raised\nPASSED test_proxy_error", false},
		{"local server", "ConnectionRefusedError: [Errno 111] Connection refused", false},
		{"timeout", "requests.exceptions.ReadTimeout: timed out", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := networkFailure(tc.out) != ""; got != tc.want {
				t.Fatalf("networkFailure(%q) matched = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

func runsOf(c Control, scores ...float64) []*ControlResult {
	out := make([]*ControlResult, len(scores))
	for i, v := range scores {
		out[i] = ctl(c, f(v), "")
	}
	return out
}

func TestClassifyRuns(t *testing.T) {
	tk := &task.Task{Solution: task.Solution{Kind: task.SolutionScript}}
	for _, tc := range []struct {
		name          string
		nops, oracles []*ControlResult
		want          Verdict
	}{
		{"one run behaves as before", runsOf(ControlNop, 0), runsOf(ControlOracle, 1), VerdictClean},
		{"consistent clean", runsOf(ControlNop, 0, 0, 0), runsOf(ControlOracle, 1, 1, 1), VerdictClean},
		{"consistent oracle failure", runsOf(ControlNop, 0, 0, 0), runsOf(ControlOracle, 0, 0, 0), VerdictOracleFails},
		{"consistent nop pass", runsOf(ControlNop, 1, 1), runsOf(ControlOracle, 1, 1), VerdictNopPasses},
		// The roadmap's example: neither CLEAN nor ORACLE_FAILS.
		{"oracle 1, 1, 0", runsOf(ControlNop, 0, 0, 0), runsOf(ControlOracle, 1, 1, 0), VerdictFlaky},
		{"flake on a later run is still seen", runsOf(ControlNop, 0, 0, 0.5), runsOf(ControlOracle, 1, 1, 1), VerdictFlaky},
		{"one test's worth of difference", runsOf(ControlNop, 0, 0), runsOf(ControlOracle, 1, 0.98), VerdictFlaky},
		// A flake says the grading is unreliable, so it outranks a
		// consistent-looking finding from the same unreliable grader.
		{"flaky beats a nop that passes", runsOf(ControlNop, 1, 1), runsOf(ControlOracle, 1, 0), VerdictFlaky},
		{"no solution, flaky nop", runsOf(ControlNop, 0, 1), nil, VerdictFlaky},
		{"no solution, steady nop", runsOf(ControlNop, 0, 0), nil, VerdictNoOracle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tk := tk
			if tc.oracles == nil {
				tk = &task.Task{Solution: task.Solution{Kind: task.SolutionNone}}
			}
			if got, _ := classifyRuns(tk, tc.nops, tc.oracles); got != tc.want {
				t.Fatalf("classifyRuns = %s, want %s", got, tc.want)
			}
		})
	}
}

// Disagreement caused by the host is the host's finding, not the benchmark's.
// Any run that could not be read makes the task ERROR, never FLAKY.
func TestHostFailuresAreNotFlakes(t *testing.T) {
	tk := &task.Task{Solution: task.Solution{Kind: task.SolutionScript}}

	errored := runsOf(ControlOracle, 1, 1)
	errored = append(errored, ctl(ControlOracle, nil, "out of memory: a process was killed at the 64 MB limit"))
	if got, reason := classifyRuns(tk, runsOf(ControlNop, 0, 0, 0), errored); got != VerdictError || !strings.Contains(reason, "out of memory") {
		t.Errorf("a run killed for memory: got %s (%s), want ERROR", got, reason)
	}

	starved := runsOf(ControlOracle, 1, 1)
	starved = append(starved, withOutput(ctl(ControlOracle, f(0), ""), requestsBehindProxy))
	if got, _ := classifyRuns(tk, runsOf(ControlNop, 0, 0, 0), starved); got != VerdictError {
		t.Errorf("a run that lost the network: got %s, want ERROR", got)
	}
}

func TestFlakyReasonListsEveryRun(t *testing.T) {
	r := TaskResult{NopRuns: runsOf(ControlNop, 0, 0, 0), OracleRuns: runsOf(ControlOracle, 1, 0, 1)}
	want := "scores differ between identical runs: nop 0.00, 0.00, 0.00; oracle 1.00, 0.00, 1.00"
	if got := VerdictFlaky.Reason(r); got != want {
		t.Errorf("Reason = %q, want %q", got, want)
	}
	if !VerdictFlaky.Flagged() {
		t.Error("FLAKY must fail CI: a task that cannot grade consistently is a defect")
	}
}
