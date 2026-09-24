// Package lint performs static checks on task structure. It never runs Docker
// and never calls a model, so it is free, deterministic, and safe in CI.
package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/skeptic-labs/skeptic/internal/dockerfile"
	"github.com/skeptic-labs/skeptic/internal/task"
)

// Severity is the outcome of one check.
type Severity string

const (
	OK   Severity = "OK"
	WARN Severity = "WARN"
	FAIL Severity = "FAIL"
)

func worse(a, b Severity) Severity {
	order := map[Severity]int{OK: 0, WARN: 1, FAIL: 2}
	if order[b] > order[a] {
		return b
	}
	return a
}

// Finding is one problem found in one task.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Result is every finding for one task.
type Result struct {
	TaskID   string    `json:"task_id"`
	Dir      string    `json:"dir"`
	Worst    Severity  `json:"worst"`
	Findings []Finding `json:"findings"`
}

// Leakage patterns. The SWE-bench+ audit found the fix present in the issue
// text for 32.67% of apparently-successful patches, making this the single
// most common way a benchmark task is unfair. See docs/decisions.md D5.
var (
	prIssueURL  = regexp.MustCompile(`https?://(?:www\.)?(?:github|gitlab)\.com/[^\s)"']+/(?:pull|pulls|issues|merge_requests|commit)/\S+`)
	diffMarkers = regexp.MustCompile(`(?m)^(?:diff --git |@@ -\d|\+\+\+ [ab]/|--- [ab]/)`)
	patchWord   = regexp.MustCompile(`(?i)\bpatch(?:es|ed|ing)?\b`)

	// Shell writes whose value is the interesting part:
	//   echo 42 > /app/answer.txt   printf '%s' 42 > /app/answer.txt
	//   echo 42 | tee /app/answer.txt
	writeRedirect = regexp.MustCompile(`\b(?:echo|printf)\s+(.+?)\s*>>?\s*(\S+)\s*$`)
	writeTee      = regexp.MustCompile(`\b(?:echo|printf)\s+(.+?)\s*\|\s*tee\s+(?:-a\s+)?(\S+)\s*$`)
	heredocWrite  = regexp.MustCompile(`\bcat\s+(?:>\s*|>>\s*)?(\S+)\s*<<\s*-?['"]?(\w+)['"]?\s*$`)
)

// reporter collects findings for one task.
type reporter func(check string, sev Severity, format string, args ...interface{})

// Check runs every static check against one task.
func Check(t *task.Task) Result {
	r := Result{TaskID: t.ID, Dir: t.Dir, Worst: OK}
	add := reporter(func(check string, sev Severity, format string, args ...interface{}) {
		r.Findings = append(r.Findings, Finding{check, sev, fmt.Sprintf(format, args...)})
		r.Worst = worse(r.Worst, sev)
	})

	if t.Unsupported != "" {
		add("supported", WARN, "task cannot be checked by skeptic: %s", t.Unsupported)
	}

	checkInstruction(t, add)
	checkSolution(t, add)
	checkTests(t, add)

	// Both Dockerfile checks share one parse.
	var df *dockerfile.File
	if t.Environment.Dockerfile != "" {
		var err error
		df, err = dockerfile.ParsePath(t.Environment.Dockerfile)
		if err != nil {
			add("dockerfile", FAIL, "cannot parse Dockerfile: %v", err)
		}
	}
	if df != nil {
		checkBuildContext(t, df.CopySources(), add)
		checkBuildSteps(t, df, add)
	}

	return r
}

func checkInstruction(t *task.Task, add reporter) {
	path := filepath.Join(t.Dir, "instruction.md")
	b, err := os.ReadFile(path)
	if err != nil {
		add("instruction", FAIL, "no readable instruction.md")
		return
	}
	text := string(b)
	if strings.TrimSpace(text) == "" {
		add("instruction", FAIL, "instruction.md is empty")
		return
	}

	if m := prIssueURL.FindString(text); m != "" {
		add("leakage", WARN,
			"instruction links to a pull request, issue or commit (%s); an agent that opens it may read the answer instead of solving the task", truncate(m, 80))
	}
	if diffMarkers.MatchString(text) {
		add("leakage", WARN, "instruction appears to contain a diff; the reference fix may be readable from the task text")
	}
	if patchWord.MatchString(text) {
		add("leakage", WARN, "instruction mentions a patch; check it does not describe the reference fix directly")
	}
}

func checkSolution(t *task.Task, add reporter) {
	if !t.Solution.Available() {
		// Upstream documents solution/ as optional, so this limits what can
		// be verified rather than breaking the task.
		add("solution", WARN, "no reference solution; the oracle control cannot run for this task")
		return
	}
	if t.Solution.Kind == task.SolutionScript {
		p := filepath.Join(t.Solution.Dir, t.Solution.Script)
		fi, err := os.Stat(p)
		if err != nil {
			add("solution", FAIL, "solution script %s is missing", t.Solution.Script)
			return
		}
		if fi.Size() == 0 {
			add("solution", FAIL, "solution script %s is empty", t.Solution.Script)
		}
	}
}

func checkTests(t *task.Task, add reporter) {
	if t.Tests.Command == "" {
		add("tests", FAIL, "no test command")
		return
	}
	if t.Tests.Dir != "" {
		entries, err := os.ReadDir(t.Tests.Dir)
		if err != nil || len(entries) == 0 {
			add("tests", FAIL, "tests directory is missing or empty")
			return
		}
	}
	if t.Tests.Score.Kind == task.ScoreRewardFile && len(t.Tests.Score.Paths) > 0 {
		if !testsWriteReward(t) {
			add("scoring", WARN,
				"no test file mentions any of the reward paths skeptic reads (%s); the score may be unreadable",
				strings.Join(t.Tests.Score.Paths, ", "))
		}
	}
}

// testsWriteReward looks for a mention of a reward path anywhere in tests/.
// It is a heuristic: a script may build the path indirectly, so a miss is a
// warning rather than a failure.
func testsWriteReward(t *task.Task) bool {
	found := false
	_ = filepath.WalkDir(t.Tests.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, rp := range t.Tests.Score.Paths {
			if strings.Contains(string(b), filepath.Base(rp)) {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

// checkBuildContext is the answer-leak check: if solution/ or tests/ sit inside
// the Docker build context and a COPY or ADD pulls them in, the agent can read
// the answer or the exam out of its own filesystem.
func checkBuildContext(t *task.Task, srcs []string, add reporter) {
	ctxDir := t.Environment.ContextDir
	if ctxDir == "" {
		return
	}

	for label, dir := range map[string]string{"solution": t.Solution.Dir, "tests": t.Tests.Dir} {
		if dir == "" {
			continue
		}
		rel, err := filepath.Rel(ctxDir, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // outside the context; docker cannot see it at all
		}
		if ignoredByDockerignore(ctxDir, rel) {
			continue
		}
		if coversPath(srcs, rel) {
			sev := FAIL
			msg := "the %s directory is inside the Docker build context and is copied into the image; an agent could read it"
			if label == "tests" {
				msg = "the %s directory is baked into the image; an agent could read the exam it is being graded on"
			}
			add("leakage", sev, msg, label)
		}
	}
}

// checkBuildSteps looks for the answer itself being baked into the image: a
// RUN that echoes the value the solution writes, an ENV that carries it, or a
// COPY heredoc that writes it. Any of these lets an agent score without
// solving, which is the defect the nop control catches the expensive way.
func checkBuildSteps(t *task.Task, df *dockerfile.File, add reporter) {
	if !t.Solution.Available() {
		return // without a solution there is no known answer to look for
	}
	answers := candidateAnswers(t.Solution)
	if len(answers) == 0 {
		return
	}

	for _, kv := range df.EnvPairs() {
		if v := mention(kv[1], answers); v != "" {
			add("leakage", FAIL,
				"ENV %s=%q carries the answer the solution writes; the agent can read it from its environment without solving", kv[0], v)
		}
	}

	for _, text := range df.RunTexts() {
		for _, line := range strings.Split(text, "\n") {
			if v := writtenValue(line); mention(v, answers) != "" {
				add("leakage", FAIL,
					"build step writes the answer into the image: %s; an agent could read the file instead of solving the task", truncate(strings.TrimSpace(line), 100))
			}
		}
	}

	for _, hc := range df.HeredocCopies() {
		for _, line := range strings.Split(hc.Body, "\n") {
			if mention(strings.TrimSpace(line), answers) != "" {
				add("leakage", FAIL,
					"COPY heredoc writes the answer into the image at %s; an agent could read the file instead of solving the task", hc.Dest)
			}
		}
	}
}

// candidateAnswers extracts the literal values the solution script writes to
// files — the known-correct answers the oracle plants. Only exact, plausible
// values are kept: the check must never cry wolf on incidental strings.
func candidateAnswers(sol task.Solution) []string {
	if sol.Kind != task.SolutionScript {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(sol.Dir, sol.Script))
	if err != nil {
		return nil
	}

	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		v = plausibleAnswer(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}

	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if m := writeRedirect.FindStringSubmatch(line); m != nil {
			add(stripPrintfFormat(shellStripQuotes(m[1])))
			continue
		}
		if m := writeTee.FindStringSubmatch(line); m != nil {
			add(stripPrintfFormat(shellStripQuotes(m[1])))
			continue
		}
		if m := heredocWrite.FindStringSubmatch(line); m != nil {
			for _, body := range lines[i+1:] {
				if strings.TrimSpace(body) == m[2] {
					break
				}
				add(strings.TrimSpace(body))
			}
		}
	}
	return out
}

// plausibleAnswer filters echo arguments down to values that could be a task
// answer. Scores (0, 1), paths, flags, URLs and unexpanded variables are
// either not answers or would match far too much.
func plausibleAnswer(v string) string {
	v = strings.TrimSpace(shellStripQuotes(v))
	switch {
	case len(v) < 2 || len(v) > 256,
		strings.HasPrefix(v, "-"),
		strings.HasPrefix(v, "/"),
		strings.ContainsAny(v, "$<>|;&`"),
		strings.Contains(v, "://"),
		isPunctuation(v):
		return ""
	}
	return v
}

func isPunctuation(v string) bool {
	for _, r := range v {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
			return false
		}
	}
	return true
}

// shellStripQuotes removes one layer of surrounding shell quotes.
func shellStripQuotes(v string) string {
	for _, q := range []string{`"`, `'`} {
		if len(v) >= 2 && strings.HasPrefix(v, q) && strings.HasSuffix(v, q) {
			return strings.TrimSuffix(strings.TrimPrefix(v, q), q)
		}
	}
	return v
}

// printfFormat matches a printf format token made only of directives, whose
// expansion is unknown: '%s', '%d', '%%' and flags/widths in between.
var printfFormat = regexp.MustCompile(`^%[-+ #0-9.]*[a-zA-Z%]$`)

// stripPrintfFormat drops a leading printf format argument made purely of
// directives (as in `printf '%s' "value"`), where the value follows it.
func stripPrintfFormat(v string) string {
	fields := strings.Fields(v)
	if len(fields) > 1 && printfFormat.MatchString(shellStripQuotes(fields[0])) {
		return strings.Join(fields[1:], " ")
	}
	return v
}

// writtenValue returns the value a shell line writes to a file, if any.
func writtenValue(line string) string {
	line = strings.TrimSpace(line)
	if m := writeRedirect.FindStringSubmatch(line); m != nil {
		return shellStripQuotes(m[1])
	}
	if m := writeTee.FindStringSubmatch(line); m != nil {
		return shellStripQuotes(m[1])
	}
	// echo -n 42 > f: the flag is not part of the value.
	return ""
}

// mention returns the candidate that appears in value, or "". A candidate
// matches when it is the whole value or a whole whitespace-separated token of
// it, so `ENV ANSWER=42` matches the answer 42 while a version string like
// go1.22 does not match the answer 22.
func mention(value string, candidates []string) string {
	for _, c := range candidates {
		if value == c {
			return c
		}
		for _, tok := range strings.Fields(value) {
			if tok == c {
				return c
			}
		}
	}
	return ""
}

// coversPath reports whether any COPY source would include rel. Sources are
// normalised first: "./", "solution/" and "solution" all name the same thing
// to docker, and treating them differently would miss real leaks.
func coversPath(srcs []string, rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	for _, s := range srcs {
		s = strings.Trim(strings.TrimPrefix(filepath.ToSlash(s), "./"), "/")
		// An empty source is "./" after normalising: the whole context.
		if s == "" || s == "." || s == "*" {
			return true
		}
		if s == rel || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

// ignoredByDockerignore reports whether .dockerignore excludes rel, which
// makes an otherwise-dangerous COPY safe.
func ignoredByDockerignore(ctxDir, rel string) bool {
	b, err := os.ReadFile(filepath.Join(ctxDir, ".dockerignore"))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		p := filepath.ToSlash(strings.TrimSuffix(strings.TrimPrefix(line, "./"), "/"))
		if p == rel || strings.HasPrefix(rel, p+"/") || p == rel+"/**" {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sprintf is fmt.Sprintf spelled differently so test callers can build
// finding messages the same way the checks do.
func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}
