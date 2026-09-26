//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/custom"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestRepeatFindsFlakes runs the coin-flip fixture 16 times. Its oracle passes
// on half of runs, so all 16 agreeing -- the only way this test can fail on
// working code -- has probability 2^-15. A stable task under the same repeat
// must stay CLEAN, or --repeat would be inventing flakes.
func TestRepeatFindsFlakes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	run := func(root string, repeat int) check.TaskResult {
		tasks, err := adapter.NewRegistry(custom.New()).Discover(root)
		if err != nil {
			t.Fatal(err)
		}
		return check.NewRunner(dc, check.Options{
			RunDir:  t.TempDir(),
			Timeout: 5 * time.Minute,
			Repeat:  repeat,
		}).Run(ctx, tasks, 1, nil)[0]
	}

	r := run("../testdata/repeat/flaky", 16)
	if r.Verdict != check.VerdictFlaky {
		t.Fatalf("flaky fixture: verdict = %s (%s), want FLAKY", r.Verdict, r.Reason)
	}
	if len(r.OracleRuns) != 16 || len(r.NopRuns) != 16 {
		t.Errorf("recorded %d nop and %d oracle runs, want 16 each", len(r.NopRuns), len(r.OracleRuns))
	}

	r = run("../testdata/custom/clean", 3)
	if r.Verdict != check.VerdictClean {
		t.Fatalf("stable fixture under --repeat 3: verdict = %s (%s), want CLEAN", r.Verdict, r.Reason)
	}
}
