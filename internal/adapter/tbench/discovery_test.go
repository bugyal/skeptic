package tbench

import (
	"testing"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/task"
)

// Discovery through the registry, against the committed fixture tree — the
// same path the CLI takes.
func TestRegistryDiscovery(t *testing.T) {
	reg := adapter.NewRegistry(New())
	tasks, err := reg.Discover("../../../testdata/tbench-tasks")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}

	byID := map[string]*task.Task{}
	for _, tk := range tasks {
		byID[tk.ID] = tk
	}
	for _, id := range []string{"clean", "no-solution"} {
		if byID[id] == nil {
			t.Errorf("task %q not discovered", id)
		}
	}

	c := byID["clean"]
	if c.Solution.Kind != task.SolutionScript {
		t.Errorf("clean: Solution.Kind = %q, want script", c.Solution.Kind)
	}
	if c.Tests.Score.Kind != task.ScoreExitCode {
		t.Errorf("clean: Score.Kind = %q, want exit_code", c.Tests.Score.Kind)
	}
	if c.Unsupported != "" {
		t.Errorf("clean: Unsupported = %q", c.Unsupported)
	}

	ns := byID["no-solution"]
	if ns.Solution.Kind != task.SolutionNone {
		t.Errorf("no-solution: Solution.Kind = %q, want none", ns.Solution.Kind)
	}
}
