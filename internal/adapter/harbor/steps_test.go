package harbor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/task"
)

// A multi-step task loads as its steps, each resolved the way Harbor
// resolves it (docs/decisions.md D24).
func TestStepsLoad(t *testing.T) {
	toml := `multi_step_reward_strategy = "final"
[verifier]
timeout_sec = 60
env = { SHARED = "1", OVERRIDE = "task" }
[[steps]]
name = "one"
min_reward = 0.5
[[steps]]
name = "two"
min_reward = { tests = 1.0 }
[steps.verifier]
timeout_sec = 90
env = { OVERRIDE = "step" }
[steps.healthcheck]
command = "true"
retries = 7
`
	dir := fidelityTask(t, toml, map[string]string{
		"solution/solve.sh":           "#!/bin/sh\n",
		"steps/one/instruction.md":    "first",
		"steps/two/instruction.md":    "second",
		"steps/two/solution/solve.sh": "#!/bin/sh\n",
		"steps/two/tests/test.sh":     "#!/bin/sh\n",
		"steps/two/workdir/setup.sh":  "#!/bin/sh\n",
		"steps/two/workdir/data.txt":  "x",
	})
	got, err := New().Load(dir)
	if err != nil || got.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, got.Unsupported)
	}
	if got.StepReward != "final" || len(got.Steps) != 2 {
		t.Fatalf("StepReward, steps = %q, %d", got.StepReward, len(got.Steps))
	}
	one, two := got.Steps[0], got.Steps[1]

	// Step one has no solution/ or tests/ of its own: the task's are used.
	if one.Solution.Dir != filepath.Join(dir, "solution") {
		t.Errorf("step one solution dir = %q, want the task's", one.Solution.Dir)
	}
	if one.Tests.Dir != filepath.Join(dir, "tests") || len(one.Tests.Overlay) != 0 {
		t.Errorf("step one tests = %q + %v, want the task's alone", one.Tests.Dir, one.Tests.Overlay)
	}
	if one.Tests.Timeout != 60*time.Second || one.Tests.Env["OVERRIDE"] != "task" {
		t.Errorf("step one verifier = %v, %v; want the task's", one.Tests.Timeout, one.Tests.Env)
	}
	if one.MinReward["reward"] != 0.5 || one.WorkdirDir != "" || one.Healthcheck != nil {
		t.Errorf("step one = %+v", one)
	}

	// Step two has its own: its solution replaces the task's, its tests are
	// laid over the task's, its verifier settings win.
	if two.Solution.Dir != filepath.Join(dir, "steps/two/solution") {
		t.Errorf("step two solution dir = %q", two.Solution.Dir)
	}
	if two.Tests.Dir != filepath.Join(dir, "tests") ||
		strings.Join(two.Tests.Overlay, ",") != filepath.Join(dir, "steps/two/tests") {
		t.Errorf("step two tests = %q + %v, want the task's then the step's", two.Tests.Dir, two.Tests.Overlay)
	}
	if two.Tests.Timeout != 90*time.Second || two.Tests.Env["OVERRIDE"] != "step" || two.Tests.Env["SHARED"] != "1" {
		t.Errorf("step two verifier = %v, %v; want the step's merged over the task's", two.Tests.Timeout, two.Tests.Env)
	}
	if two.WorkdirDir == "" || !two.Setup {
		t.Errorf("step two workdir = %q, setup %v", two.WorkdirDir, two.Setup)
	}
	hc := two.Healthcheck
	if hc == nil || hc.Retries != 7 || hc.Interval != 5*time.Second || hc.Timeout != 30*time.Second ||
		hc.StartPeriod != 0 || hc.StartInterval != 5*time.Second {
		t.Errorf("healthcheck = %+v, want Harbor's defaults with retries 7", hc)
	}
	if two.MinReward["tests"] != 1.0 || len(two.MinReward) != 1 {
		t.Errorf("step two min_reward = %v", two.MinReward)
	}

	if !got.Solution.Available() {
		t.Error("every step has a solution, but the oracle is not available")
	}
	if !strings.Contains(got.Instruction, "first") || !strings.Contains(got.Instruction, "second") {
		t.Errorf("Instruction = %q, want every step's", got.Instruction)
	}
}

// The oracle must be able to act on every step or it does not run: one
// step without a solution is NO_ORACLE, never ORACLE_FAILS.
func TestStepWithoutSolutionIsNoOracle(t *testing.T) {
	dir := fidelityTask(t, "[[steps]]\nname = \"a\"\n[[steps]]\nname = \"b\"\n", map[string]string{
		"steps/a/solution/solve.sh": "#!/bin/sh\n",
	})
	got, err := New().Load(dir)
	if err != nil || got.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, got.Unsupported)
	}
	if got.Solution.Available() {
		t.Error("step b has no solution, but the oracle would run")
	}
	if got.Steps[1].Solution.Kind != task.SolutionNone {
		t.Errorf("step b solution = %+v", got.Steps[1].Solution)
	}
}

func TestBadStepsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name, toml, reason string
		files              map[string]string
	}{
		{"unknown strategy", "multi_step_reward_strategy = \"max\"\n[[steps]]\nname = \"a\"\n", "multi_step_reward_strategy \"max\"", nil},
		{"path in name", "[[steps]]\nname = \"../a\"\n", "not a plain directory name", nil},
		{"empty name", "[[steps]]\nname = \"\"\n", "not a plain directory name", nil},
		{"duplicate name", "[[steps]]\nname = \"a\"\n[[steps]]\nname = \"A\"\n", "used twice", nil},
		{"no test script", "[[steps]]\nname = \"a\"\n", "no test script", map[string]string{"tests/test.sh": ""}},
		{"empty healthcheck", "[[steps]]\nname = \"a\"\n[steps.healthcheck]\ncommand = \" \"\n", "healthcheck has no command", nil},
		{"bad min_reward", "[[steps]]\nname = \"a\"\nmin_reward = \"high\"\n", "min_reward", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New().Load(fidelityTask(t, tc.toml, tc.files))
			if err != nil {
				t.Fatalf("Load returned an error; refusals must be reported as Unsupported: %v", err)
			}
			if !strings.Contains(got.Unsupported, tc.reason) {
				t.Errorf("Unsupported = %q, want it to contain %q", got.Unsupported, tc.reason)
			}
		})
	}
}
