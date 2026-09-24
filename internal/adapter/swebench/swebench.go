package swebench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bugyal/skeptic/internal/task"
)

// Container conventions of the published SWE-bench evaluation images, from
// swebench/harness/run_evaluation.py: the eval script is written to /eval.sh
// and executed with bash, and the repository lives at /testbed.
const (
	formatName  = "swebench"
	EvalPath    = "/eval.sh"
	TestbedDir  = "/testbed"
	startMarker = ">>>>> Start Test Output"
	endMarker   = ">>>>> End Test Output"
	pullTimeout = 45 * time.Minute
)

// Instance is one row of a SWE-bench dataset export.
//
// The list fields arrive as JSON-encoded strings from a Hugging Face export and
// as real arrays from a local dump, so they are decoded leniently.
type Instance struct {
	InstanceID string `json:"instance_id"`
	Repo       string `json:"repo"`
	BaseCommit string `json:"base_commit"`
	Version    string `json:"version"`
	Image      string `json:"image"`
	EvalScript string `json:"eval_script"`
	LogParser  string `json:"log_parser"`
	EvalType   string `json:"eval_type"`
	Patch      string `json:"patch"`
	// ProblemStatement is the issue text handed to the agent. It is what the
	// leakage checks read: an audit of SWE-bench found the fix present here
	// in 32.67% of apparently-successful patches.
	ProblemStatement string     `json:"problem_statement"`
	TestPatch        string     `json:"test_patch"`
	FailToPass       stringList `json:"FAIL_TO_PASS"`
	PassToPass       stringList `json:"PASS_TO_PASS"`
}

// stringList decodes either ["a","b"] or "[\"a\",\"b\"]".
type stringList []string

func (s *stringList) UnmarshalJSON(b []byte) error {
	var direct []string
	if err := json.Unmarshal(b, &direct); err == nil {
		*s = direct
		return nil
	}
	var encoded string
	if err := json.Unmarshal(b, &encoded); err != nil {
		return err
	}
	if strings.TrimSpace(encoded) == "" {
		*s = nil
		return nil
	}
	var inner []string
	if err := json.Unmarshal([]byte(encoded), &inner); err != nil {
		return fmt.Errorf("decoding test list %q: %w", truncate(encoded, 60), err)
	}
	*s = inner
	return nil
}

// Adapter loads SWE-bench instances from a local JSONL or JSON export.
//
// Parquet is not read directly: converting an export to JSONL is one datasets
// call, and a parquet reader would be the single largest dependency in the
// project. See docs/decisions.md D7.
type Adapter struct{}

// New returns a SWE-bench adapter.
func New() *Adapter { return &Adapter{} }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return formatName }

// Detect always reports false: SWE-bench tasks are rows in a file, never
// directories, so discovery goes through DetectSet.
func (a *Adapter) Detect(string) bool { return false }

// Load is unreachable for this format; LoadSet is the entry point.
func (a *Adapter) Load(string) (*task.Task, error) {
	return nil, fmt.Errorf("swebench tasks load as a set, not a directory")
}

// DetectSet recognises a JSONL/JSON export, or a directory containing one.
func (a *Adapter) DetectSet(path string) bool {
	return a.resolve(path) != ""
}

// resolve returns the export file for path, or "" if there is none.
func (a *Adapter) resolve(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if !fi.IsDir() {
		if looksLikeExport(path) {
			return path
		}
		return ""
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(path, e.Name())
		if looksLikeExport(p) {
			return p
		}
	}
	return ""
}

// looksLikeExport checks the extension and then the first record, so an
// unrelated JSON file in a directory is not mistaken for a dataset.
func looksLikeExport(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".json":
	default:
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	head := string(buf[:n])
	return strings.Contains(head, `"instance_id"`)
}

// LoadSet reads every instance in the export.
func (a *Adapter) LoadSet(path string) ([]*task.Task, error) {
	file := a.resolve(path)
	if file == "" {
		return nil, fmt.Errorf("no SWE-bench export found at %s", path)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	instances, err := decode(b)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("%s contains no instances", file)
	}

	out := make([]*task.Task, 0, len(instances))
	for _, in := range instances {
		out = append(out, in.toTask(file))
	}
	return out, nil
}

// decode accepts a JSON array or newline-delimited JSON.
func decode(b []byte) ([]Instance, error) {
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "[") {
		var arr []Instance
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var out []Instance
	for i, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var in Instance
		if err := json.Unmarshal([]byte(line), &in); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, in)
	}
	return out, nil
}

func (in Instance) toTask(source string) *task.Task {
	t := &task.Task{
		ID:          in.InstanceID,
		Dir:         source,
		Format:      formatName,
		Instruction: in.ProblemStatement,
	}

	// Refuse loudly rather than score with the wrong tools.
	switch {
	case in.Image == "":
		t.Unsupported = "instance has no image; export lacks the fields the current harness needs"
		return t
	case in.EvalScript == "":
		t.Unsupported = "instance has no eval_script; export predates the current harness format"
		return t
	}
	parser := ParserFor(in.LogParser)
	if parser == nil {
		t.Unsupported = fmt.Sprintf("no log parser implemented for %q", in.LogParser)
		return t
	}

	t.Environment = task.Environment{
		Image:        in.Image,
		BuildTimeout: pullTimeout,
		WorkDir:      TestbedDir,
		Platform:     platformFor(in.Image),
	}

	if in.Patch == "" {
		t.Solution = task.Solution{Kind: task.SolutionNone}
	} else {
		t.Solution = task.Solution{
			Kind:         task.SolutionPatch,
			PatchContent: in.Patch,
			PatchStrip:   1,
			WorkDir:      TestbedDir,
		}
	}

	failToPass, passToPass, evalType := []string(in.FailToPass), []string(in.PassToPass), in.EvalType
	t.Tests = task.Tests{
		ScriptContent: in.EvalScript,
		ScriptPath:    EvalPath,
		// The published images run the eval script with bash explicitly.
		Command: "/bin/bash " + EvalPath,
		WorkDir: TestbedDir,
		Score: task.ScoreSpec{
			Kind: task.ScoreFunc,
			Scorer: func(stdout, stderr string, _ int) (float64, string, error) {
				// The harness brackets the test run with markers; anything
				// outside them is setup noise that must not be parsed.
				body, ok := between(stdout+"\n"+stderr, startMarker, endMarker)
				if !ok {
					return 0, "", fmt.Errorf("test output markers not found; the suite likely never ran")
				}
				statuses := parser(body)
				if len(statuses) == 0 {
					return 0, "", fmt.Errorf("no test results parsed from output")
				}
				score, o := Score(statuses, failToPass, passToPass, evalType)
				detail := fmt.Sprintf("F2P %d/%d, P2P %d/%d",
					o.FailToPassOK, o.FailToPass, o.PassToPassOK, o.PassToPass)
				if len(o.Missing) > 0 {
					detail += fmt.Sprintf(", %d expected test(s) absent", len(o.Missing))
				}
				return score, detail, nil
			},
		},
	}
	return t
}

// platformFor derives the image architecture from the published naming
// convention (sweb.eval.x86_64.* / sweb.eval.arm64.*), so an amd64 image on an
// arm64 host is pulled deliberately rather than failing obscurely.
func platformFor(image string) string {
	switch {
	case strings.Contains(image, ".x86_64."):
		return "linux/amd64"
	case strings.Contains(image, ".arm64."):
		return "linux/arm64/v8"
	}
	return ""
}

// between returns the text bracketed by start and end.
func between(s, start, end string) (string, bool) {
	i := strings.Index(s, start)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		// A crashed or killed run loses the closing marker; the output up to
		// that point is still the best evidence available.
		return rest, true
	}
	return rest[:j], true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
