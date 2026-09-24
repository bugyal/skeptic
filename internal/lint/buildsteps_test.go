package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skeptic-labs/skeptic/internal/dockerfile"
	"github.com/skeptic-labs/skeptic/internal/task"
)

// mustParseDockerfile parses Dockerfile text for the checkBuildSteps tests.
func mustParseDockerfile(t *testing.T, src string) *dockerfile.File {
	t.Helper()
	f, err := dockerfile.Parse(src)
	if err != nil {
		t.Fatalf("parsing Dockerfile: %v", err)
	}
	return f
}

// collect runs checkBuildSteps and returns the findings it produced.
func collect(t *testing.T, tk *task.Task, df *dockerfile.File) []Finding {
	t.Helper()
	var out []Finding
	checkBuildSteps(tk, df, func(check string, sev Severity, format string, args ...interface{}) {
		out = append(out, Finding{check, sev, sprintf(format, args...)})
	})
	return out
}

func hasFinding(fs []Finding, sev Severity) bool {
	for _, f := range fs {
		if f.Severity == sev {
			return true
		}
	}
	return false
}

// Fixture tasks exercising the build-step checks end to end.
func TestEchoAnswerInBuildStep(t *testing.T) {
	r := load(t, "../../testdata/lint/echo-answer")
	if !has(r, "leakage", FAIL) {
		t.Fatalf("expected a leakage FAIL for RUN echo 42 > /app/answer.txt, got %+v", r.Findings)
	}
}

func TestEnvCarryingAnswer(t *testing.T) {
	r := load(t, "../../testdata/lint/env-answer")
	if !has(r, "leakage", FAIL) {
		t.Fatalf("expected a leakage FAIL for ENV ANSWER=42, got %+v", r.Findings)
	}
}

// The answer-echo check must only fire when the value matches what the
// solution writes; otherwise every Dockerfile with a version echo would flag.
func TestUnrelatedEchoDoesNotFlag(t *testing.T) {
	tk := writeTask(t, "echo hello world > /app/greeting.txt")
	df := mustParseDockerfile(t, "RUN echo 42 > /tmp/unrelated")
	if fs := collect(t, tk, df); len(fs) != 0 {
		t.Fatalf("unexpected findings: %+v", fs)
	}
}

func TestMatchingEchoFlags(t *testing.T) {
	tk := writeTask(t, "echo 42 > /app/answer.txt")
	df := mustParseDockerfile(t, "RUN echo 42 > /app/answer.txt")
	if fs := collect(t, tk, df); !hasFinding(fs, FAIL) {
		t.Fatalf("expected a leakage FAIL, got %+v", fs)
	}
}

func TestPlausibleAnswer(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"42", "42"},
		{`"the answer"`, "the answer"},
		{"1", ""},                   // too short: matches too much
		{"-n", ""},                  // a flag
		{"/etc/config", ""},         // a path
		{"$ANSWER", ""},             // unexpanded variable
		{"https://example.com", ""}, // a URL
		{"0", ""},                   // a score echo
		{"a > b", ""},               // shell syntax leaked into the value
		{strings.Repeat("x", 300), ""},
	} {
		if got := plausibleAnswer(tc.in); got != tc.want {
			t.Errorf("plausibleAnswer(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMention(t *testing.T) {
	for _, tc := range []struct {
		value      string
		candidates []string
		want       string
	}{
		{"42", []string{"42"}, "42"},
		{"prefix 42 suffix", []string{"42"}, "42"},
		{"go1.22", []string{"22"}, ""}, // whole-token match only
		{"answer42", []string{"42"}, ""},
		{"", []string{"42"}, ""},
	} {
		if got := mention(tc.value, tc.candidates); got != tc.want {
			t.Errorf("mention(%q, %v) = %q, want %q", tc.value, tc.candidates, got, tc.want)
		}
	}
}

// The solution-side extraction: what the solve script writes is what the
// build must not already contain.
func TestCandidateAnswersFromSolveScript(t *testing.T) {
	tk := writeTask(t, strings.Join([]string{
		"#!/bin/sh",
		"echo 42 > /app/answer.txt",
		`printf '%s' "the quick brown fox" | tee /app/phrase.txt`,
		"echo 1 > /logs/verifier/reward.txt",
	}, "\n"))
	got := candidateAnswers(tk.Solution)
	found := map[string]bool{}
	for _, a := range got {
		found[a] = true
	}
	if !found["42"] {
		t.Errorf("candidateAnswers missing 42: %v", got)
	}
	if !found["the quick brown fox"] {
		t.Errorf("candidateAnswers missing tee'd phrase: %v", got)
	}
	if found["1"] {
		t.Errorf("candidateAnswers kept score echo 1: %v", got)
	}
}

func TestHeredocAnswerInDockerfile(t *testing.T) {
	tk := writeTask(t, "echo 42 > /app/answer.txt")
	df := mustParseDockerfile(t, "COPY <<EOF /app/answer.txt\n42\nEOF")
	if fs := collect(t, tk, df); !hasFinding(fs, FAIL) {
		t.Fatalf("expected a leakage FAIL for heredoc answer, got %+v", fs)
	}
}

// A Dockerfile whose heredoc writes something unrelated must stay clean.
func TestHeredocUnrelatedStaysClean(t *testing.T) {
	tk := writeTask(t, "echo 42 > /app/answer.txt")
	df := mustParseDockerfile(t, "COPY <<EOF /app/motd\nwelcome to the task\nEOF")
	if fs := collect(t, tk, df); len(fs) != 0 {
		t.Fatalf("unexpected findings: %+v", fs)
	}
}

// A task with no solution has no known answer, so the check cannot fire —
// and must not guess one.
func TestNoSolutionSkipsBuildStepCheck(t *testing.T) {
	tk := &task.Task{Solution: task.Solution{Kind: task.SolutionNone}}
	df := mustParseDockerfile(t, "RUN echo 42 > /app/answer.txt")
	if fs := collect(t, tk, df); len(fs) != 0 {
		t.Fatalf("unexpected findings: %+v", fs)
	}
}

// writeTask materialises a task directory with a solve script whose text is
// given, plus a grading test, so the solution-side extraction runs against
// real files.
func writeTask(t *testing.T, solveScript string) *task.Task {
	t.Helper()
	dir := t.TempDir()
	solDir := filepath.Join(dir, "solution")
	testDir := filepath.Join(dir, "tests")
	if err := os.MkdirAll(solDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	solve := filepath.Join(solDir, "solve.sh")
	if err := os.WriteFile(solve, []byte(solveScript), 0o755); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(testDir, "test.sh")
	if err := os.WriteFile(test, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &task.Task{
		ID:  "test/answer",
		Dir: dir,
		Solution: task.Solution{
			Kind:      task.SolutionScript,
			Dir:       solDir,
			Script:    "solve.sh",
			MountPath: "/solution",
		},
		Tests: task.Tests{
			Dir:       testDir,
			MountPath: "/tests",
			Command:   "chmod +x /tests/test.sh && /tests/test.sh",
		},
	}
}
