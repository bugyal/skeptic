package patch

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const twoHunks = `diff --git a/calc.py b/calc.py
--- a/calc.py
+++ b/calc.py
@@ -1,5 +1,6 @@
 def add(a, b):
-    return a - b
+    return a + b
 
 
 def sub(a, b):
@@ -8,3 +9,6 @@ def sub(a, b):
 
 def mul(a, b):
     return a * b
+
+def div(a, b):
+    return a / b
`

func TestParse(t *testing.T) {
	p, err := Parse(twoHunks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(p.Files))
	}
	if p.Files[0].Path != "calc.py" {
		t.Errorf("Path = %q, want calc.py", p.Files[0].Path)
	}
	if p.HunkCount() != 2 {
		t.Fatalf("HunkCount = %d, want 2", p.HunkCount())
	}
	h := p.Files[0].Hunks[0]
	if h.OldStart != 1 || h.OldCount != 5 || h.NewStart != 1 || h.NewCount != 6 {
		t.Errorf("hunk 0 = %+v, want -1,5 +1,6", h)
	}
	if h.Delta() != 1 {
		t.Errorf("Delta = %d, want 1", h.Delta())
	}
}

// Rendering a freshly parsed patch must reproduce it byte for byte, or every
// derived patch is suspect.
func TestRoundTrip(t *testing.T) {
	p, err := Parse(twoHunks)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.String(); got != twoHunks {
		t.Fatalf("round trip differs:\n--- got ---\n%s\n--- want ---\n%s", got, twoHunks)
	}
}

func TestWithoutRenumbersLaterHunks(t *testing.T) {
	p, err := Parse(twoHunks)
	if err != nil {
		t.Fatal(err)
	}
	reduced, dropped, err := Without0(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if reduced.HunkCount() != 1 {
		t.Fatalf("HunkCount = %d, want 1", reduced.HunkCount())
	}
	// Dropping a hunk that added one line must pull the next hunk's new-side
	// start back by one, or git apply will reject the result.
	if got := reduced.Files[0].Hunks[0].NewStart; got != 8 {
		t.Errorf("surviving hunk NewStart = %d, want 8 (9 minus the dropped hunk's delta)", got)
	}
	if dropped.Delta() != 1 {
		t.Errorf("dropped hunk delta = %d, want 1", dropped.Delta())
	}
	// The original must be untouched.
	if p.Files[0].Hunks[1].NewStart != 9 {
		t.Error("Without mutated the original patch")
	}
}

func TestWithoutDropsEmptyFile(t *testing.T) {
	single := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-old
+new
`
	p, err := Parse(single)
	if err != nil {
		t.Fatal(err)
	}
	reduced, _, err := Without0(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(reduced.Files) != 0 {
		t.Fatalf("got %d files, want 0 once its only hunk is gone", len(reduced.Files))
	}
}

func TestWithoutOutOfRange(t *testing.T) {
	p, _ := Parse(twoHunks)
	if _, _, err := Without0(p, 5); err == nil {
		t.Fatal("expected an error for an out-of-range hunk index")
	}
}

func TestDescribe(t *testing.T) {
	p, _ := Parse(twoHunks)
	if got := p.Describe(1); !strings.Contains(got, "calc.py") {
		t.Errorf("Describe = %q, want it to name the file", got)
	}
}

// The reduced patch has to be something git will actually apply. Anything less
// and the partial control would report a weak test when it had simply produced
// a broken diff.
func TestReducedPatchAppliesWithGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	write := func(name, content string) {
		t.Helper()
		if err := writeFile(filepath.Join(dir, name), content); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write("calc.py", "def add(a, b):\n    return a - b\n\n\ndef sub(a, b):\n    return a - b\n\n\ndef mul(a, b):\n    return a * b\n")
	run("add", "calc.py")
	run("commit", "-qm", "base")

	p, err := Parse(twoHunks)
	if err != nil {
		t.Fatal(err)
	}
	reduced, _, err := Without0(p, 0)
	if err != nil {
		t.Fatal(err)
	}

	patchPath := filepath.Join(dir, "reduced.diff")
	if err := writeFile(patchPath, reduced.String()); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(git, "apply", "--check", "-v", "reduced.diff")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply --check rejected the reduced patch: %v\n%s\npatch:\n%s", err, out, reduced.String())
	}
}
