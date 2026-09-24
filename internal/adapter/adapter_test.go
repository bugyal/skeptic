package adapter

import (
	"path/filepath"
	"testing"

	"github.com/bugyal/skeptic/internal/adapter/harbor"
)

const fixtures = "../../testdata/tasks"

func TestDiscoverTaskSet(t *testing.T) {
	r := NewRegistry(harbor.New())
	got, err := r.Discover(fixtures)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("found %d tasks, want 6", len(got))
	}
	// Discover sorts by ID so reports and tables are stable across runs.
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("results not sorted: %q before %q", got[i-1].ID, got[i].ID)
		}
	}
}

// The same command must work on a single task directory.
func TestDiscoverSingleTask(t *testing.T) {
	r := NewRegistry(harbor.New())
	got, err := r.Discover(filepath.Join(fixtures, "clean"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].ID != "skeptic-fixtures/clean" {
		t.Fatalf("got %d tasks, want just the clean fixture", len(got))
	}
}

func TestDiscoverNoTasks(t *testing.T) {
	r := NewRegistry(harbor.New())
	if _, err := r.Discover(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory with no tasks")
	}
}
