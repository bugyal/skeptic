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
	"github.com/bugyal/skeptic/internal/adapter/custom"
	"github.com/bugyal/skeptic/internal/check"
	"github.com/bugyal/skeptic/internal/docker"
)

const customFixtures = "../testdata/custom"

// TestCustomFormat runs both skeptic.toml fixtures through every control,
// then weakens the patch fixture's suite and checks the partial control sees
// exactly the hunk that stopped being graded.
func TestCustomFormat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dc := docker.New(nil)
	if err := dc.Available(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	run := func(t *testing.T, root string) []check.TaskResult {
		t.Helper()
		tasks, err := adapter.NewRegistry(custom.New()).Discover(root)
		if err != nil {
			t.Fatalf("discovering %s: %v", root, err)
		}
		runner := check.NewRunner(dc, check.Options{
			RunDir:          t.TempDir(),
			Timeout:         5 * time.Minute,
			Partial:         true,
			PartialMaxHunks: 2,
		})
		return runner.Run(ctx, tasks, 2, nil)
	}

	t.Run("fixtures are clean", func(t *testing.T) {
		results := run(t, customFixtures)
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}
		for _, r := range results {
			if r.Verdict != check.VerdictClean {
				t.Errorf("%s: verdict = %s (%s), want CLEAN", r.ID, r.Verdict, r.Reason)
			}
			if len(r.WeakTests) != 0 {
				t.Errorf("%s: WeakTests = %v, want none: the suite grades every hunk", r.ID, r.WeakTests)
			}
		}
	})

	t.Run("weakened suite is caught", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "weak")
		if out, err := exec.Command("cp", "-r", filepath.Join(customFixtures, "patch"), dir).CombinedOutput(); err != nil {
			t.Fatalf("copying fixture: %v\n%s", err, out)
		}
		// Stop grading mul. Both controls still pass; only the partial
		// control can tell.
		p := filepath.Join(dir, "tests", "test.sh")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var kept []string
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "mul wrong") {
				kept = append(kept, line)
			}
		}
		if err := os.WriteFile(p, []byte(strings.Join(kept, "\n")), 0o755); err != nil {
			t.Fatal(err)
		}

		r := run(t, dir)[0]
		if r.Verdict != check.VerdictClean {
			t.Fatalf("verdict = %s (%s), want CLEAN", r.Verdict, r.Reason)
		}
		if len(r.WeakTests) != 1 {
			t.Fatalf("WeakTests = %v, want exactly the mul hunk", r.WeakTests)
		}
	})
}
