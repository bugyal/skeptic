//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/swebench"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

const partialFixture = "../testdata/partial/demo"

// TestPartialControlFindsUngradedHunk is the regression test for the weak-test
// probe. The fixture passes both original controls; only the partial control
// can tell that half the reference fix is never graded.
func TestPartialControlFindsUngradedHunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	// The fixture image is built here rather than published.
	build := exec.CommandContext(ctx, "docker", "build", "-q",
		"-t", "skeptic-partial-demo:latest", partialFixture)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building fixture image: %v\n%s", err, out)
	}

	tasks, err := adapter.NewRegistry(swebench.New()).
		Discover(filepath.Join(partialFixture, "instances.jsonl"))
	if err != nil {
		t.Fatalf("discovering fixture: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}

	runner := check.NewRunner(dc, check.Options{
		RunDir:          t.TempDir(),
		Timeout:         5 * time.Minute,
		Partial:         true,
		PartialMaxHunks: 2,
	})
	res := runner.Run(ctx, tasks, 1, nil)[0]

	// Both original controls must be satisfied: this task looks fine to them.
	if res.Verdict != check.VerdictClean {
		t.Fatalf("verdict = %s (%s), want CLEAN", res.Verdict, res.Reason)
	}
	if s := res.NopScore(); s == nil || *s != 0 {
		t.Errorf("nop score = %v, want 0", s)
	}
	if s := res.OracleScore(); s == nil || *s != 1 {
		t.Errorf("oracle score = %v, want 1", s)
	}

	// And yet exactly one hunk turns out to be ungraded.
	if len(res.WeakTests) != 1 {
		t.Fatalf("WeakTests = %v, want exactly one ungraded hunk", res.WeakTests)
	}

	// Discrimination matters as much as detection: withholding the hunk the
	// suite *does* grade must score 0, or the probe is just noise.
	if len(res.Partials) != 2 {
		t.Fatalf("got %d partial probes, want 2", len(res.Partials))
	}
	var sawGraded, sawUngraded bool
	for _, p := range res.Partials {
		if p.Score == nil {
			t.Fatalf("partial probe %q produced no score: %s", p.Hunk, p.Error)
		}
		switch *p.Score {
		case 0:
			sawGraded = true
		case 1:
			sawUngraded = true
		}
	}
	if !sawGraded {
		t.Error("no probe scored 0; the graded hunk should have been caught when withheld")
	}
	if !sawUngraded {
		t.Error("no probe scored 1; the ungraded hunk should have gone unnoticed")
	}
}
