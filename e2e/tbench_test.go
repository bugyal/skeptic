//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/tbench"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestTerminalBench1 runs both Terminal-Bench 1.x fixtures through the
// controls. Until this test existed, `check` had never scored one: the
// adapter ran ./run-tests.sh from /, the harness never copied the script in,
// and every reference solution scored 0. Unit tests on the adapter's fields
// passed throughout, because they asserted the wrong fields.
//
// The fixture images install pytest from PyPI, so this needs network at
// build time.
func TestTerminalBench1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	tasks, err := adapter.NewRegistry(tbench.New()).Discover("../testdata/tbench-tasks")
	if err != nil {
		t.Fatal(err)
	}
	results := check.NewRunner(dc, check.Options{RunDir: t.TempDir(), Timeout: 5 * time.Minute}).
		Run(ctx, tasks, 2, nil)

	want := map[string]check.Verdict{
		"clean":       check.VerdictClean,
		"no-solution": check.VerdictNoOracle,
	}
	for _, r := range results {
		if r.Verdict != want[r.ID] {
			t.Errorf("%s: verdict = %s (%s), want %s", r.ID, r.Verdict, r.Reason, want[r.ID])
		}
	}
	if len(results) != len(want) {
		t.Errorf("got %d results, want %d", len(results), len(want))
	}
}
