//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/adapter"
	"github.com/bugyal/skeptic/internal/adapter/harbor"
	"github.com/bugyal/skeptic/internal/adapter/tbench"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

// TestComposeTasks runs a multi-container task in each format. Each fixture's
// test fetches a secret from a sidecar, so a pass needs main and the sidecar
// really networked together, not merely both started. A sidecar that never
// becomes healthy must make the task ERROR: the stack could not be run, so
// there is no verdict to report.
func TestComposeTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	reg := adapter.NewRegistry(harbor.New(), tbench.New())
	run := func(t *testing.T, root string) []check.TaskResult {
		t.Helper()
		tasks, err := reg.Discover(root)
		if err != nil {
			t.Fatal(err)
		}
		return check.NewRunner(dc, check.Options{RunDir: t.TempDir(), Timeout: 5 * time.Minute}).
			Run(ctx, tasks, 2, nil)
	}

	t.Run("fixtures are clean", func(t *testing.T) {
		results := run(t, "../testdata/compose")
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}
		for _, r := range results {
			if r.Verdict != check.VerdictClean {
				t.Errorf("%s: verdict = %s (%s), want CLEAN", r.ID, r.Verdict, r.Reason)
			}
		}
	})

	t.Run("unhealthy sidecar is an error", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "sick")
		if out, err := exec.Command("cp", "-r", "../testdata/compose/harbor-sidecar", dir).CombinedOutput(); err != nil {
			t.Fatalf("copying fixture: %v\n%s", err, out)
		}
		p := filepath.Join(dir, "environment", "docker-compose.yaml")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sick := strings.Replace(string(b),
			`test: ["CMD", "python", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:8000/secret.txt', timeout=2)"]`,
			`test: ["CMD", "false"]`, 1)
		sick = strings.Replace(sick, "retries: 20", "retries: 2", 1)
		if sick == string(b) {
			t.Fatal("fixture healthcheck changed; update this test")
		}
		if err := os.WriteFile(p, []byte(sick), 0o644); err != nil {
			t.Fatal(err)
		}
		r := run(t, dir)[0]
		if r.Verdict != check.VerdictError || !strings.Contains(r.Reason, "compose") {
			t.Fatalf("verdict = %s (%s), want ERROR from compose up", r.Verdict, r.Reason)
		}
	})
}
