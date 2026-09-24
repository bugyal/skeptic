//go:build e2e

// Package e2e exercises the whole pipeline against real Docker. It is gated
// behind the e2e build tag so `make test` stays fast and needs no daemon.
//
//	make e2e      # or: go test -tags e2e ./e2e/...
package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/harbor"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
	"github.com/bugyal/skeptic/internal/report"
)

const fixtures = "../testdata/tasks"

// TestVerdictClasses is the guarantee the whole tool rests on: each fixture is
// built to fail in one specific way, and skeptic must name that way exactly.
func TestVerdictClasses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	tasks, err := adapter.NewRegistry(harbor.New()).Discover(fixtures)
	if err != nil {
		t.Fatalf("discovering fixtures: %v", err)
	}

	runner := check.NewRunner(dc, check.Options{
		RunDir:  filepath.Join(t.TempDir(), "run"),
		Timeout: 5 * time.Minute,
	})
	results := runner.Run(ctx, tasks, 3, nil)

	want := map[string]check.Verdict{
		"skeptic-fixtures/clean":        check.VerdictClean,
		"skeptic-fixtures/nop-passes":   check.VerdictNopPasses,
		"skeptic-fixtures/oracle-fails": check.VerdictOracleFails,
		"skeptic-fixtures/both-wrong":   check.VerdictBoth,
		"skeptic-fixtures/build-error":  check.VerdictError,
		"skeptic-fixtures/no-solution":  check.VerdictNoOracle,
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}

	for _, r := range results {
		exp, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected task %q", r.ID)
			continue
		}
		if r.Verdict != exp {
			t.Errorf("%s: verdict = %s (%s), want %s", r.ID, r.Verdict, r.Reason, exp)
		}
	}

	// Scores must be exact, not merely on the right side of a threshold.
	byID := map[string]check.TaskResult{}
	for _, r := range results {
		byID[r.ID] = r
	}
	if s := byID["skeptic-fixtures/clean"].OracleScore(); s == nil || *s != 1 {
		t.Errorf("clean oracle score = %v, want 1", s)
	}
	if s := byID["skeptic-fixtures/clean"].NopScore(); s == nil || *s != 0 {
		t.Errorf("clean nop score = %v, want 0", s)
	}
	if s := byID["skeptic-fixtures/nop-passes"].NopScore(); s == nil || *s != 1 {
		t.Errorf("nop-passes nop score = %v, want 1", s)
	}
	// A failed build must leave no score at all rather than an implied zero.
	if s := byID["skeptic-fixtures/build-error"].NopScore(); s != nil {
		t.Errorf("build-error nop score = %v, want nil", *s)
	}

	rep := report.Build(results, "e2e", "run", dc.APIVersion(ctx))
	if rep.Totals.Clean != 1 || rep.Totals.Flagged != 3 || rep.Totals.Errors != 1 || rep.Totals.NoOracle != 1 {
		t.Errorf("totals = %+v; want 1 clean, 3 flagged, 1 error, 1 no-oracle", rep.Totals)
	}
	if !rep.AnyFlagged() {
		t.Error("AnyFlagged() = false, want true")
	}
}

// Image caching must not change any verdict: a second run over the same task
// set has to agree with the first, or the cache is lying.
func TestCachedRunAgrees(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	tasks, err := adapter.NewRegistry(harbor.New()).Discover(filepath.Join(fixtures, "clean"))
	if err != nil {
		t.Fatal(err)
	}
	runner := check.NewRunner(dc, check.Options{RunDir: t.TempDir(), Timeout: 5 * time.Minute})

	first := runner.Run(ctx, tasks, 1, nil)
	second := runner.Run(ctx, tasks, 1, nil)

	if first[0].Verdict != second[0].Verdict {
		t.Fatalf("cached run disagreed: %s then %s", first[0].Verdict, second[0].Verdict)
	}
	if first[0].ImageDigest != second[0].ImageDigest {
		t.Errorf("image digest changed between runs: %s vs %s", first[0].ImageDigest, second[0].ImageDigest)
	}
}
