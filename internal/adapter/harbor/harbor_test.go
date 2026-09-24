package harbor

import (
	"path/filepath"
	"testing"

	"github.com/bugyal/skeptic/internal/task"
)

const fixtures = "../../../testdata/tasks"

func TestDetect(t *testing.T) {
	a := New()
	if !a.Detect(filepath.Join(fixtures, "clean")) {
		t.Error("clean fixture should be detected as a harbor task")
	}
	// A parent directory holding tasks is not itself a task.
	if a.Detect(fixtures) {
		t.Error("task-set root should not be detected as a task")
	}
	if a.Detect(filepath.Join(fixtures, "does-not-exist")) {
		t.Error("missing directory should not be detected")
	}
}

func TestLoadClean(t *testing.T) {
	got, err := New().Load(filepath.Join(fixtures, "clean"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "skeptic-fixtures/clean" {
		t.Errorf("ID = %q, want the packaged name from task.toml", got.ID)
	}
	if got.Format != "harbor" {
		t.Errorf("Format = %q, want harbor", got.Format)
	}
	if got.Unsupported != "" {
		t.Errorf("Unsupported = %q, want empty", got.Unsupported)
	}
	if !got.Solution.Available() || got.Solution.Kind != task.SolutionScript {
		t.Errorf("Solution = %+v, want an available script solution", got.Solution)
	}
	if got.Solution.Script != "solve.sh" {
		t.Errorf("Solution.Script = %q, want solve.sh", got.Solution.Script)
	}
	if got.Environment.WorkDir != "/app" {
		t.Errorf("WorkDir = %q, want /app", got.Environment.WorkDir)
	}
	// reward.json must be tried before reward.txt, matching Harbor's verifier.
	want := []string{RewardJSON, RewardText}
	if len(got.Tests.Score.Paths) != 2 ||
		got.Tests.Score.Paths[0] != want[0] || got.Tests.Score.Paths[1] != want[1] {
		t.Errorf("Score.Paths = %v, want %v", got.Tests.Score.Paths, want)
	}
}

// A task with no solution/ directory is a supported upstream configuration,
// so it must load cleanly with the oracle control marked unavailable.
func TestLoadNoSolution(t *testing.T) {
	got, err := New().Load(filepath.Join(fixtures, "no-solution"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Solution.Available() {
		t.Errorf("Solution.Available() = true, want false for a task with no solution/")
	}
	if got.Tests.Command == "" {
		t.Error("tests should still be loaded when no solution exists")
	}
}
