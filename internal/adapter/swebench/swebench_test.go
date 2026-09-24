package swebench

import (
	"strings"
	"testing"

	"github.com/skeptic-labs/skeptic/internal/task"
)

const fixture = "../../../testdata/swebench/instances.jsonl"

func loadAll(t *testing.T) map[string]*task.Task {
	t.Helper()
	ts, err := New().LoadSet(fixture)
	if err != nil {
		t.Fatalf("LoadSet: %v", err)
	}
	out := map[string]*task.Task{}
	for _, x := range ts {
		out[x.ID] = x
	}
	return out
}

func TestDetectSet(t *testing.T) {
	a := New()
	if !a.DetectSet(fixture) {
		t.Error("jsonl export should be detected")
	}
	// A directory holding the export works too, so `skeptic check ./data` does.
	if !a.DetectSet("../../../testdata/swebench") {
		t.Error("directory containing an export should be detected")
	}
	// Harbor task directories must not be claimed by this adapter.
	if a.DetectSet("../../../testdata/tasks/clean") {
		t.Error("harbor task dir should not be detected as a swebench export")
	}
}

func TestLoadInstance(t *testing.T) {
	got := loadAll(t)["demo__demo-1"]
	if got == nil {
		t.Fatal("demo__demo-1 not loaded")
	}
	if got.Unsupported != "" {
		t.Fatalf("Unsupported = %q, want empty", got.Unsupported)
	}
	if !got.Environment.Prebuilt() {
		t.Error("swebench instances must use the published image, not a build")
	}
	// The image name encodes its architecture; pinning it keeps an amd64 image
	// on an arm64 host from failing obscurely.
	if got.Environment.Platform != "linux/amd64" {
		t.Errorf("Platform = %q, want linux/amd64", got.Environment.Platform)
	}
	if got.Solution.Kind != task.SolutionPatch || got.Solution.PatchContent == "" {
		t.Errorf("Solution = %+v, want an inline patch", got.Solution)
	}
	if got.Tests.ScriptPath != EvalPath || got.Tests.ScriptContent == "" {
		t.Errorf("Tests script = %q/%q, want the eval script at %s", got.Tests.ScriptPath, got.Tests.ScriptContent, EvalPath)
	}
	if got.Tests.Score.Kind != task.ScoreFunc || got.Tests.Score.Scorer == nil {
		t.Error("swebench scoring must go through a scorer function")
	}
}

// The published naming convention encodes architecture; deriving it lets an
// amd64 image be pulled deliberately on an arm64 host.
func TestPlatformFor(t *testing.T) {
	for image, want := range map[string]string{
		"swebench/sweb.eval.x86_64.demo_1776_demo-1:latest": "linux/amd64",
		"swebench/sweb.eval.arm64.demo_1776_demo-2:latest":  "linux/arm64/v8",
		"some/other-image:latest":                           "",
	} {
		if got := platformFor(image); got != want {
			t.Errorf("platformFor(%q) = %q, want %q", image, got, want)
		}
	}
}

// An instance whose parser is not implemented must be refused, not scored with
// a parser written for a different test runner.
func TestUnknownParserIsUnsupported(t *testing.T) {
	got := loadAll(t)["demo__demo-2"]
	if !strings.Contains(got.Unsupported, "log parser") {
		t.Errorf("Unsupported = %q, want it to name the missing log parser", got.Unsupported)
	}
}

func TestMissingEvalScriptIsUnsupported(t *testing.T) {
	got := loadAll(t)["demo__demo-3"]
	if !strings.Contains(got.Unsupported, "eval_script") {
		t.Errorf("Unsupported = %q, want it to name the missing eval_script", got.Unsupported)
	}
}

// FAIL_TO_PASS arrives JSON-encoded from a Hugging Face export and as a real
// array from a local dump; both must load.
func TestStringListBothEncodings(t *testing.T) {
	var s stringList
	if err := s.UnmarshalJSON([]byte(`["a","b"]`)); err != nil || len(s) != 2 {
		t.Fatalf("array form: %v %v", s, err)
	}
	if err := s.UnmarshalJSON([]byte(`"[\"a\",\"b\"]"`)); err != nil || len(s) != 2 {
		t.Fatalf("encoded-string form: %v %v", s, err)
	}
}

func TestScorer(t *testing.T) {
	scorer := loadAll(t)["demo__demo-1"].Tests.Score.Scorer

	pass := ">>>>> Start Test Output\nPASSED t.py::test_f\nPASSED t.py::test_g\n>>>>> End Test Output"
	if score, detail, err := scorer(pass, "", 0); err != nil || score != 1 {
		t.Errorf("all expected tests passing: score %v detail %q err %v, want 1", score, detail, err)
	}

	// A regression in PASS_TO_PASS must cost the full score.
	regress := ">>>>> Start Test Output\nPASSED t.py::test_f\nFAILED t.py::test_g\n>>>>> End Test Output"
	if score, _, err := scorer(regress, "", 0); err != nil || score != 0 {
		t.Errorf("P2P regression: score %v err %v, want 0", score, err)
	}

	// Output the suite never produced must be an error, never a zero: a run
	// that did not happen is not a failing run.
	if _, _, err := scorer("container exited before tests ran", "", 1); err == nil {
		t.Error("missing markers should error, not score 0")
	}
}

// Scoring must honour the two asymmetries in upstream grading.
func TestScoreStatusSemantics(t *testing.T) {
	// XFAIL counts as a pass for FAIL_TO_PASS.
	if s, _ := Score(map[string]string{"a": StatusXfail}, []string{"a"}, nil, EvalPassAndFail); s != 1 {
		t.Error("XFAIL should count as passing for FAIL_TO_PASS")
	}
	// A skipped test is a regression for F2P...
	if s, _ := Score(map[string]string{"a": StatusSkipped}, []string{"a"}, nil, EvalPassAndFail); s != 0 {
		t.Error("SKIPPED must not satisfy FAIL_TO_PASS")
	}
	// ...but not for P2P.
	if s, _ := Score(map[string]string{"b": StatusSkipped}, nil, []string{"b"}, EvalPassAndFail); s != 1 {
		t.Error("SKIPPED should not count as a PASS_TO_PASS regression")
	}
	// fail_only instances ignore PASS_TO_PASS entirely.
	if s, _ := Score(map[string]string{"a": StatusPassed}, []string{"a"}, []string{"missing"}, EvalFailOnly); s != 1 {
		t.Error("fail_only should ignore PASS_TO_PASS")
	}
}

// Truncated parametrized ids resolve by prefix only when candidates agree.
func TestResolveTruncatedIds(t *testing.T) {
	sm := map[string]string{
		"test_x[log(photon/second)]": StatusPassed,
		"test_x[log(erg/s)]":         StatusPassed,
	}
	if !testPassed("test_x[log(photon", sm) {
		t.Error("agreeing candidates should resolve a truncated id")
	}
	// Two candidates that genuinely share the truncated prefix but disagree.
	disagree := map[string]string{
		"test_x[log(photon/second)]": StatusPassed,
		"test_x[log(photon/erg)]":    StatusFailed,
	}
	if testPassed("test_x[log(photon", disagree) {
		t.Error("disagreeing candidates must not resolve")
	}
	// A balanced, simply-absent id never prefix-matches.
	if testPassed("test_y", map[string]string{"test_y_extra": StatusPassed}) {
		t.Error("exact ids must keep exact-match semantics")
	}
}
