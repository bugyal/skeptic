package check

import (
	"testing"

	"github.com/bugyal/skeptic/internal/task"
)

// A task's declared limits must reach the container, and the command-line
// overrides must replace them.
func TestStartOptionsCarryLimits(t *testing.T) {
	tk := &task.Task{Environment: task.Environment{WorkDir: "/app", CPUs: 2, MemoryMB: 2048}}

	got := NewRunner(nil, Options{}).startOptions(tk, "img", "name")
	if got.Memory != "2048m" || got.CPUs != "2" {
		t.Errorf("Memory, CPUs = %q, %q; want 2048m, 2", got.Memory, got.CPUs)
	}

	got = NewRunner(nil, Options{OverrideCPUs: 0.5, OverrideMemoryMB: 512}).startOptions(tk, "img", "name")
	if got.Memory != "512m" || got.CPUs != "0.5" {
		t.Errorf("overridden Memory, CPUs = %q, %q; want 512m, 0.5", got.Memory, got.CPUs)
	}

	// No declared limit and no override: no flag at all, not a zero limit.
	got = NewRunner(nil, Options{}).startOptions(&task.Task{}, "img", "name")
	if got.Memory != "" || got.CPUs != "" {
		t.Errorf("undeclared limits gave Memory, CPUs = %q, %q; want both empty", got.Memory, got.CPUs)
	}
}
