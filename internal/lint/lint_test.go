package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/skeptic-labs/skeptic/internal/adapter/harbor"
	"github.com/skeptic-labs/skeptic/internal/task"
)

func load(t *testing.T, dir string) Result {
	t.Helper()
	tk, err := harbor.New().Load(dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	return Check(tk)
}

func has(r Result, check string, sev Severity) bool {
	for _, f := range r.Findings {
		if f.Check == check && f.Severity == sev {
			return true
		}
	}
	return false
}

// A well-formed task must produce no findings at all: a linter that cries wolf
// on clean input trains people to ignore it.
func TestCleanTaskHasNoFindings(t *testing.T) {
	r := load(t, "../../testdata/tasks/clean")
	if r.Worst != OK {
		t.Fatalf("worst = %s, want OK; findings: %+v", r.Worst, r.Findings)
	}
}

// The most common documented benchmark defect: the answer is reachable from
// the task text.
func TestInstructionLeakage(t *testing.T) {
	r := load(t, "../../testdata/lint/leaky-instruction")
	if !has(r, "leakage", WARN) {
		t.Fatalf("expected a leakage warning, got %+v", r.Findings)
	}
	if r.Worst != WARN {
		t.Errorf("worst = %s, want WARN", r.Worst)
	}
}

// The answer must not be readable out of the agent's own filesystem.
func TestBuildContextLeakage(t *testing.T) {
	r := load(t, "../../testdata/lint/leaky-context")
	if !has(r, "leakage", FAIL) {
		t.Fatalf("expected a leakage failure for COPY . into the image, got %+v", r.Findings)
	}
	if r.Worst != FAIL {
		t.Errorf("worst = %s, want FAIL", r.Worst)
	}
}

func TestMissingSolutionWarns(t *testing.T) {
	r := load(t, "../../testdata/tasks/no-solution")
	if !has(r, "solution", WARN) {
		t.Fatalf("expected a solution warning, got %+v", r.Findings)
	}
}

func TestCoversPath(t *testing.T) {
	for _, tc := range []struct {
		srcs []string
		rel  string
		want bool
	}{
		{srcs: []string{"."}, rel: "solution", want: true},
		{srcs: []string{"./"}, rel: "tests", want: true},
		{srcs: []string{"solution"}, rel: "solution", want: true},
		{srcs: []string{"solution/"}, rel: "solution", want: true},
		{srcs: []string{"src"}, rel: "solution", want: false},
		{srcs: []string{"solutions"}, rel: "solution", want: false},
		{srcs: nil, rel: "solution", want: false},
	} {
		if got := coversPath(tc.srcs, tc.rel); got != tc.want {
			t.Errorf("coversPath(%v, %q) = %v, want %v", tc.srcs, tc.rel, got, tc.want)
		}
	}
}

// A .dockerignore entry makes an otherwise-dangerous COPY safe, and the
// linter must not report a leak that docker would never perform.
func TestDockerignoreSuppressesLeak(t *testing.T) {
	ctx := t.TempDir()
	if err := os.WriteFile(filepath.Join(ctx, ".dockerignore"), []byte("solution/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ignoredByDockerignore(ctx, "solution") {
		t.Error("solution/ in .dockerignore should suppress the leak")
	}
	if ignoredByDockerignore(ctx, "tests") {
		t.Error("tests is not in .dockerignore and must not be suppressed")
	}
}

// Leakage detection must work on formats that carry their instruction text in
// a manifest or dataset row rather than an instruction.md, because that is
// where the real corpus lives. Regression test for a gap where the check read
// only Harbor's on-disk file and so never ran on SWE-bench at all.
func TestLeakageUsesInstructionField(t *testing.T) {
	tk := &task.Task{
		ID:     "demo__demo-10097",
		Dir:    t.TempDir(), // no instruction.md here on purpose
		Format: "swebench",
		Instruction: "URLValidator accepts invalid characters.\n" +
			"Pull request: https://github.com/django/django/pull/10097\n",
		Tests:    task.Tests{Command: "true"},
		Solution: task.Solution{Kind: task.SolutionPatch, PatchContent: "diff"},
	}
	r := Check(tk)
	if !has(r, "leakage", WARN) {
		t.Fatalf("expected a leakage warning from Instruction, got %+v", r.Findings)
	}
}

// An empty instruction must fail rather than silently pass the leakage checks.
func TestMissingInstructionFails(t *testing.T) {
	tk := &task.Task{
		ID: "demo__empty", Dir: t.TempDir(), Format: "swebench",
		Tests:    task.Tests{Command: "true"},
		Solution: task.Solution{Kind: task.SolutionPatch, PatchContent: "diff"},
	}
	if r := Check(tk); !has(r, "instruction", FAIL) {
		t.Fatalf("expected an instruction failure, got %+v", r.Findings)
	}
}

// A task the adapter declined must not also collect structural failures for
// fields the adapter never populated. Reporting "no test command" for a layout
// skeptic does not read blames the benchmark for skeptic's own gap.
func TestUnsupportedTaskSkipsStructuralChecks(t *testing.T) {
	tk := &task.Task{
		ID: "demo__unsupported", Dir: t.TempDir(), Format: "tbench",
		Unsupported: "layout not supported",
		Instruction: "Do the thing.",
		// Tests and Solution deliberately empty, as an adapter leaves them.
	}
	r := Check(tk)
	if has(r, "tests", FAIL) {
		t.Errorf("unsupported task should not report a tests failure: %+v", r.Findings)
	}
	if has(r, "solution", WARN) {
		t.Errorf("unsupported task should not report a solution warning: %+v", r.Findings)
	}
	if !has(r, "supported", WARN) {
		t.Errorf("expected the unsupported warning, got %+v", r.Findings)
	}
	if r.Worst == FAIL {
		t.Errorf("worst = FAIL, want WARN for an unsupported task")
	}
}

// Leakage is still worth reporting on a task skeptic cannot run.
func TestUnsupportedTaskStillChecksLeakage(t *testing.T) {
	tk := &task.Task{
		ID: "demo__unsupported-leak", Dir: t.TempDir(), Format: "tbench",
		Unsupported: "layout not supported",
		Instruction: "See https://github.com/example/proj/pull/42 for the fix.",
	}
	if !has(Check(tk), "leakage", WARN) {
		t.Error("leakage should still be reported for an unsupported task")
	}
}
