//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/custom"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestOutOfMemoryIsAnError is the roadmap's trap for resource limits: a test
// the kernel killed at the task's memory limit must be ERROR, never a score of
// zero. Raising the limit must make the same task CLEAN, which shows the limit
// really was applied and really was the cause.
func TestOutOfMemoryIsAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	tasks, err := adapter.NewRegistry(custom.New()).Discover("../testdata/limits/oom")
	if err != nil {
		t.Fatal(err)
	}

	run := func(override int) check.TaskResult {
		return check.NewRunner(dc, check.Options{
			RunDir:           t.TempDir(),
			Timeout:          5 * time.Minute,
			OverrideMemoryMB: override,
		}).Run(ctx, tasks, 1, nil)[0]
	}

	r := run(0)
	if r.Verdict != check.VerdictError || !strings.Contains(r.Reason, "out of memory") {
		t.Fatalf("at the declared 64 MB: verdict = %s (%s), want ERROR for out of memory", r.Verdict, r.Reason)
	}

	r = run(1024)
	if r.Verdict != check.VerdictClean {
		t.Fatalf("with --override-memory-mb 1024: verdict = %s (%s), want CLEAN", r.Verdict, r.Reason)
	}
}
