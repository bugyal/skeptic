//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/harbor"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestOfflineTaskRunsOffline checks the environment a Harbor task declares is
// the one Skeptic gives it. The fixture's test passes only in a container
// with no network interface but loopback, as Harbor's no-network mode
// provides; run online, the oracle fails.
func TestOfflineTaskRunsOffline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	tasks, err := adapter.NewRegistry(harbor.New()).Discover("../testdata/harbor-fidelity/offline")
	if err != nil {
		t.Fatal(err)
	}
	r := check.NewRunner(dc, check.Options{RunDir: t.TempDir(), Timeout: 5 * time.Minute}).
		Run(ctx, tasks, 1, nil)[0]
	if r.Verdict != check.VerdictClean {
		t.Fatalf("verdict = %s (%s), want CLEAN", r.Verdict, r.Reason)
	}
}
