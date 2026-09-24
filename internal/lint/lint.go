// Package lint performs static checks on task structure. It never runs Docker
// and never calls a model, so it is free, deterministic, and safe in CI.
package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	copySrc     = regexp.MustCompile(`(?i)^\s*(COPY|ADD)\s+(.*)$`)
)

// Check runs every static check against one task.
func Check(t *task.Task) Result {
	r := Result{TaskID: t.ID, Dir: t.Dir, Worst: OK}
	add := func(check string, sev Severity, format string, args ...interface{}) {
		r.Findings = append(r.Findings, Finding{check, sev, fmt.Sprintf(format, args...)})
		r.Worst = worse(r.Worst, sev)
	}

	if t.Unsupported != "" {
		add("supported", WARN, "task cannot be checked by skeptic: %s", t.Unsupported)
	}

	checkInstruction(t, add)
	checkSolution(t, add)
	checkTests(t, add)
	checkBuildContext(t, add)

	return r
}

func checkInstruction(t *task.Task, add func(string, Severity, string, ...interface{})) {
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

func checkSolution(t *task.Task, add func(string, Severity, string, ...interface{})) {
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

func checkTests(t *task.Task, add func(string, Severity, string, ...interface{})) {
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
func checkBuildContext(t *task.Task, add func(string, Severity, string, ...interface{})) {
	ctxDir := t.Environment.ContextDir
	if ctxDir == "" || t.Environment.Dockerfile == "" {
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
		if srcs := copiedSources(t.Environment.Dockerfile); coversPath(srcs, rel) {
			sev := FAIL
			msg := "the %s directory is inside the Docker build context and is copied into the image; an agent could read it"
			if label == "tests" {
				msg = "the %s directory is baked into the image; an agent could read the exam it is being graded on"
			}
			add("leakage", sev, msg, label)
		}
	}
}

// copiedSources extracts the source arguments of every COPY and ADD.
func copiedSources(dockerfile string) []string {
	f, err := os.Open(dockerfile)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := copySrc.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		fields := strings.Fields(m[2])
		// Drop flags such as --from=builder, and the final destination arg.
		var args []string
		for _, f := range fields {
			if !strings.HasPrefix(f, "--") {
				args = append(args, strings.Trim(f, `"'`))
			}
		}
		if len(args) > 1 {
			out = append(out, args[:len(args)-1]...)
		}
	}
	return out
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
