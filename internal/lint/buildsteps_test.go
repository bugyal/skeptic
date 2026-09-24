package lint

import (
	"strings"
	"testing"

	"github.com/skeptic-labs/skeptic/internal/dockerfile"
)

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
	sol := taskWithSolution("echo hello world > /app/greeting.txt")
	df := mustParse(t, "RUN echo hello world > /app/greeting.txt\nRUN echo 42 > /tmp/unrelated")
	add := func(_ string, sev Severity, _ string, _ ...interface{}) {
		t.Errorf("unexpected finding with severity %s", sev)
	}
	checkBuildSteps(sol, mustParseDockerfile(t, df), add)
}

func TestPlausibleAnswer(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"42", "42"},
		{`"the answer"`, "the answer"},
		{"1", ""},                     // too short: matches too much
		{"-n", ""},                    // a flag
		{"/etc/config", ""},           // a path
		{"$ANSWER", ""},               // unexpanded variable
		{"https://example.com", ""},   // a URL
		{"0", ""},                     // a score echo
		{"a > b", ""},                 // shell syntax leaked into the value
		{strings.Repeat("x", 300), ""} // absurdly long
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
	sol := taskWithSolution(strings.Join([]string{
		"#!/bin/sh",
		"echo 42 > /app/answer.txt",
		`printf '%s' "the quick brown fox" | tee /app/phrase.txt`,
		"echo 1 > /logs/verifier/reward.txt",
	}, "\n"))
	got := candidateAnswers(sol)
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
	sol := taskWithSolution("echo 42 > /app/answer.txt")
	df := mustParseDockerfile(t, mustParse(t, "COPY <<EOF /app/answer.txt\n42\nEOF"))
	add := func(check string, sev Severity, _ string, _ ...interface{}) {
		if check != "leakage" || sev != FAIL {
			t.Errorf("unexpected finding %s/%s", check, sev)
		}
	}
	checkBuildSteps(sol, df, add)
}

// A Dockerfile whose heredoc writes something unrelated must stay clean.
func TestHeredocUnrelatedStaysClean(t *testing.T) {
	sol := taskWithSolution("echo 42 > /app/answer.txt")
	df := mustParseDockerfile(t, mustParse(t, "COPY <<EOF /app/motd\nwelcome to the task\nEOF"))
	add := func(_ string, sev Severity, _ string, _ ...interface{}) {
		t.Errorf("unexpected finding with severity %s", sev)
	}
	checkBuildSteps(sol, df, add)
}

// A task with no solution has no known answer, so the check cannot fire —
// and must not guess one.
func TestNoSolutionSkipsBuildStepCheck(t *testing.T) {
	tk := taskNoSolution()
	df := mustParseDockerfile(t, mustParse(t, "RUN echo 42 > /app/answer.txt"))
	add := func(_ string, sev Severity, _ string, _ ...interface{}) {
		t.Errorf("unexpected finding with severity %s", sev)
	}
	checkBuildSteps(tk, df, add)
}