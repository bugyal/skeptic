package patch

import (
	"fmt"
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

func TestHunkSemantic(t *testing.T) {
	commentOnly := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,3 +1,3 @@
 def f():
-    # siderial time
+    # sidereal time
     return 1
`
	p, err := Parse(commentOnly)
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Hunks[0].Semantic() {
		t.Error("a comment-only hunk must not count as semantic; no test can observe it")
	}
	if p.SemanticHunks() != 0 {
		t.Errorf("SemanticHunks = %d, want 0", p.SemanticHunks())
	}

	real, err := Parse(twoHunks)
	if err != nil {
		t.Fatal(err)
	}
	if !real.Files[0].Hunks[0].Semantic() {
		t.Error("a code change must count as semantic")
	}
	if real.SemanticHunks() != 2 {
		t.Errorf("SemanticHunks = %d, want 2", real.SemanticHunks())
	}
}

// Blank-line-only churn is not observable either.
func TestHunkSemanticBlankLines(t *testing.T) {
	blank := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,4 +1,3 @@
 def f():
     return 1
-
 
`
	p, err := Parse(blank)
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Hunks[0].Semantic() {
		t.Error("removing a blank line is not a semantic change")
	}
}

// A hunk mixing a comment edit with a code change is semantic.
func TestHunkSemanticMixed(t *testing.T) {
	mixed := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,3 +1,3 @@
 def f():
-    # old note
-    return 1
+    # new note
+    return 2
`
	p, err := Parse(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Files[0].Hunks[0].Semantic() {
		t.Error("a hunk containing a code change is semantic even if it also edits comments")
	}
}

func TestHunkDeletionOnly(t *testing.T) {
	deletion := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,6 +1,2 @@
 class A:
-    def dead(self):
-        return 1
-
     def live(self):
`
	p, err := Parse(deletion)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Files[0].Hunks[0].Semantic() {
		t.Error("removing code is a semantic change")
	}
	if !p.Files[0].Hunks[0].DeletionOnly() {
		t.Error("a hunk that only removes lines is deletion-only")
	}

	mixed, err := Parse(twoHunks)
	if err != nil {
		t.Fatal(err)
	}
	if mixed.Files[0].Hunks[0].DeletionOnly() {
		t.Error("a hunk that adds and removes is not deletion-only")
	}
}

// Regression for django__django-15368: the fix made an import unused, and a
// second hunk removed it. Withholding that hunk leaves an unused import, which
// no test can observe. DeletionOnly misses it because rewriting an import line
// both adds and removes.
func TestHunkImportOnly(t *testing.T) {
	importCleanup := `diff --git a/query.py b/query.py
--- a/query.py
+++ b/query.py
@@ -17,7 +17,7 @@
 from django.db.models import AutoField
-from django.db.models.expressions import Case, Expression, F
+from django.db.models.expressions import Case, F
 from django.db.models.functions import Cast
`
	p, err := Parse(importCleanup)
	if err != nil {
		t.Fatal(err)
	}
	h := p.Files[0].Hunks[0]
	if h.DeletionOnly() {
		t.Error("rewriting an import line is not deletion-only")
	}
	if !h.ImportOnly() {
		t.Error("a hunk changing only an import line is import-only")
	}
	if ok, why := h.Unobservable(); !ok || why != "import statements only" {
		t.Errorf("Unobservable = %v/%q, want true/import statements only", ok, why)
	}
}

// A hunk touching real code is observable even if it also moves an import.
func TestHunkImportOnlyMixed(t *testing.T) {
	mixed := `diff --git a/q.py b/q.py
--- a/q.py
+++ b/q.py
@@ -1,4 +1,4 @@
 import os
-from x import Expression
-    if not isinstance(attr, Expression):
+    if not hasattr(attr, 'resolve_expression'):
`
	p, err := Parse(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Hunks[0].ImportOnly() {
		t.Error("a hunk containing a code change is not import-only")
	}
	if ok, _ := p.Files[0].Hunks[0].Unobservable(); ok {
		t.Error("a hunk containing a code change is observable")
	}
}

func TestUnobservableClassifies(t *testing.T) {
	real, _ := Parse(twoHunks)
	if ok, _ := real.Files[0].Hunks[0].Unobservable(); ok {
		t.Error("a genuine code change must be observable")
	}
}

// Regression for django__django-16560: 18 hunks across two files, and taking
// the first three sampled only the file the tests never execute.
func TestSampleHunksCoversFiles(t *testing.T) {
	var b strings.Builder
	for _, f := range []struct {
		name  string
		hunks int
	}{{"a.py", 5}, {"b.py", 13}} {
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", f.name, f.name, f.name, f.name)
		for i := 0; i < f.hunks; i++ {
			fmt.Fprintf(&b, "@@ -%d,2 +%d,2 @@\n-old%d\n+new%d\n", i*10+1, i*10+1, i, i)
		}
	}
	p, err := Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if p.HunkCount() != 18 {
		t.Fatalf("HunkCount = %d, want 18", p.HunkCount())
	}

	got := p.SampleHunks(3)
	if len(got) != 3 {
		t.Fatalf("SampleHunks(3) = %v, want 3 indices", got)
	}
	files := map[string]bool{}
	for _, i := range got {
		fi, _, ok := p.Locate(i)
		if !ok {
			t.Fatalf("index %d does not locate", i)
		}
		files[p.Files[fi].Path] = true
	}
	if len(files) != 2 {
		t.Errorf("sampled files = %v, want both files represented", files)
	}

	// Deterministic across calls, or reruns would disagree.
	again := p.SampleHunks(3)
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("SampleHunks is not deterministic: %v then %v", got, again)
		}
	}
}

func TestSampleHunksSmallPatch(t *testing.T) {
	p, _ := Parse(twoHunks)
	if got := p.SampleHunks(5); len(got) != 2 {
		t.Errorf("SampleHunks(5) on a 2-hunk patch = %v, want both", got)
	}
	if got := p.SampleHunks(0); got != nil {
		t.Errorf("SampleHunks(0) = %v, want nil", got)
	}
}
