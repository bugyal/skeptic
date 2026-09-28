//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/harbor"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestMultiStepTasks runs Harbor multi-step fixtures end to end
// (docs/decisions.md D24). Each broken fixture names the failure a combined
// score could hide.
func TestMultiStepTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	tasks, err := adapter.NewRegistry(harbor.New()).Discover("../testdata/harbor-steps")
	if err != nil {
		t.Fatal(err)
	}
	results := check.NewRunner(dc, check.Options{RunDir: t.TempDir(), Timeout: 5 * time.Minute}).
		Run(ctx, tasks, 4, nil)

	want := map[string]struct {
		verdict check.Verdict
		reason  string
	}{
		// Both steps in order, in one container, with the second step's
		// tests and env and nothing left from the first.
		"step-fixtures/clean": {check.VerdictClean, ""},
		// Half the mean for doing nothing.
		"step-fixtures/nop-passes": {check.VerdictNopPasses, ""},
		// The first step passing does not carry a wrong second one.
		"step-fixtures/oracle-fails": {check.VerdictOracleFails, ""},
		// The first step's reward file must not be read as the second's.
		"step-fixtures/no-reward": {check.VerdictError, `step "append"`},
	}
	if len(results) != len(want) {
		t.Fatalf("checked %d tasks, want %d", len(results), len(want))
	}
	for _, r := range results {
		w, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected task %s", r.ID)
			continue
		}
		if r.Verdict != w.verdict || !strings.Contains(r.Reason, w.reason) {
			t.Errorf("%s: verdict = %s (%s), want %s containing %q", r.ID, r.Verdict, r.Reason, w.verdict, w.reason)
		}
	}
}
