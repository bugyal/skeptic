// Package custom adapts task directories described by a skeptic.toml manifest.
//
// Unlike the other adapters, this format has no upstream to read: it is
// Skeptic's own, for benchmarks that do not use Harbor's layout. It is
// therefore strict by design. Every field that changes how a task runs must be
// stated, and a manifest carrying a key the adapter does not know, or missing
// one it needs, marks the task Unsupported with the reason. A guessed default
// would turn a misread manifest into a confident verdict, which is the one
// thing Skeptic must never produce.
//
// The format is documented in the README under "Custom skeptic.toml".
package custom

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bugyal/skeptic/internal/task"
)

const (
	formatName   = "custom"
	manifestFile = "skeptic.toml"
	// formatVersion is the only manifest version this build understands. A
	// manifest written for a later version may mean something this code
	// cannot see, so it is refused rather than half-read.
	formatVersion = 1
	// solutionMount is where the solution script's directory is uploaded.
	// Skeptic, not the manifest, builds the command that runs it, so the path
	// never appears in anything the author writes.
	solutionMount = "/solution"
	buildTimeout  = 30 * time.Minute
)

// Adapter loads skeptic.toml tasks.
type Adapter struct{}

// New returns a custom-format adapter.
func New() *Adapter { return &Adapter{} }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return formatName }

// Detect reports a directory carrying a skeptic.toml. The manifest is an
// explicit statement of intent, so nothing else is required beside it.
func (a *Adapter) Detect(dir string) bool {
	return isFile(filepath.Join(dir, manifestFile))
}

type manifest struct {
	FormatVersion int `toml:"format_version"`
	Task          struct {
		ID          string `toml:"id"`
		Instruction string `toml:"instruction"`
	} `toml:"task"`
	Environment struct {
		Dockerfile      string            `toml:"dockerfile"`
		Context         string            `toml:"context"`
		Image           string            `toml:"image"`
		Workdir         string            `toml:"workdir"`
		Platform        string            `toml:"platform"`
		BuildArgs       map[string]string `toml:"build_args"`
		BuildTimeoutSec float64           `toml:"build_timeout_sec"`
		// Pointers, so an explicit 0 can be refused rather than read as
		// "no limit": leaving the key out is how a task declares none.
		CPUs     *float64 `toml:"cpus"`
		MemoryMB *int     `toml:"memory_mb"`
	} `toml:"environment"`
	Solution struct {
		Kind       string            `toml:"kind"`
		Script     string            `toml:"script"`
		Patch      string            `toml:"patch"`
		Env        map[string]string `toml:"env"`
		TimeoutSec float64           `toml:"timeout_sec"`
	} `toml:"solution"`
	Tests struct {
		Command    string            `toml:"command"`
		Dir        string            `toml:"dir"`
		Mount      string            `toml:"mount"`
		Env        map[string]string `toml:"env"`
		TimeoutSec float64           `toml:"timeout_sec"`
		Score      struct {
			Kind      string   `toml:"kind"`
			Paths     []string `toml:"paths"`
			RewardKey string   `toml:"reward_key"`
		} `toml:"score"`
	} `toml:"tests"`
}

// Load reads a skeptic.toml task directory. It returns an error only when the
// directory itself cannot be resolved; every problem with the manifest is
// reported on the task as Unsupported, so a broken task stays visible in the
// report instead of vanishing from a set that otherwise loads.
func (a *Adapter) Load(dir string) (*task.Task, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	t := &task.Task{ID: filepath.Base(abs), Dir: abs, Format: formatName}

	var m manifest
	md, err := toml.DecodeFile(filepath.Join(abs, manifestFile), &m)
	if err != nil {
		t.Unsupported = fmt.Sprintf("unparsable %s: %v", manifestFile, err)
		return t, nil
	}
	if m.Task.ID != "" {
		t.ID = m.Task.ID
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = k.String()
		}
		sort.Strings(names)
		t.Unsupported = fmt.Sprintf("%s has keys this version does not understand: %s",
			manifestFile, strings.Join(names, ", "))
		return t, nil
	}
	if reason := build(t, abs, &m); reason != "" {
		t.Unsupported = reason
	}
	return t, nil
}

// build fills t from m, returning a reason when the manifest cannot be run as
// written. Checks run in manifest order so the first reason names the first
// problem an author would find reading top to bottom.
func build(t *task.Task, abs string, m *manifest) string {
	switch m.FormatVersion {
	case formatVersion:
	case 0:
		return fmt.Sprintf("%s: format_version is required (this build reads %d)", manifestFile, formatVersion)
	default:
		return fmt.Sprintf("%s: format_version %d is not supported (this build reads %d)",
			manifestFile, m.FormatVersion, formatVersion)
	}

	if p := m.Task.Instruction; p != "" {
		host, reason := local(abs, "task.instruction", p)
		if reason != "" {
			return reason
		}
		b, err := os.ReadFile(host)
		if err != nil {
			return fmt.Sprintf("task.instruction: cannot read %s", p)
		}
		t.Instruction = string(b)
	}

	if reason := buildEnvironment(t, abs, m); reason != "" {
		return reason
	}
	if reason := buildSolution(t, abs, m); reason != "" {
		return reason
	}
	return buildTests(t, abs, m)
}

func buildEnvironment(t *task.Task, abs string, m *manifest) string {
	e := m.Environment
	if e.Workdir == "" {
		return "environment.workdir is required"
	}
	if !strings.HasPrefix(e.Workdir, "/") {
		return fmt.Sprintf("environment.workdir %q must be an absolute container path", e.Workdir)
	}
	env := task.Environment{
		WorkDir:      e.Workdir,
		Platform:     e.Platform,
		BuildArgs:    e.BuildArgs,
		BuildTimeout: seconds(e.BuildTimeoutSec, buildTimeout),
	}

	if e.CPUs != nil {
		if *e.CPUs <= 0 {
			return fmt.Sprintf("environment.cpus must be positive, got %v; leave it out for no limit", *e.CPUs)
		}
		env.CPUs = *e.CPUs
	}
	if e.MemoryMB != nil {
		if *e.MemoryMB <= 0 {
			return fmt.Sprintf("environment.memory_mb must be positive, got %d; leave it out for no limit", *e.MemoryMB)
		}
		env.MemoryMB = *e.MemoryMB
	}

	switch {
	case e.Dockerfile != "" && e.Image != "":
		return "environment: set dockerfile or image, not both"
	case e.Image != "":
		if e.Context != "" || len(e.BuildArgs) > 0 {
			return "environment: context and build_args apply to dockerfile, not image"
		}
		env.Image = e.Image
	case e.Dockerfile != "":
		// The context is required even though "the task directory" would be
		// a natural default: whether solution/ and tests/ are inside it is
		// exactly what the leak check reads, so it must be what the author
		// meant rather than what Skeptic assumed.
		if e.Context == "" {
			return "environment.context is required with dockerfile"
		}
		df, reason := local(abs, "environment.dockerfile", e.Dockerfile)
		if reason != "" {
			return reason
		}
		if !isFile(df) {
			return fmt.Sprintf("environment.dockerfile: %s not found", e.Dockerfile)
		}
		ctx, reason := local(abs, "environment.context", e.Context)
		if reason != "" {
			return reason
		}
		if !isDir(ctx) {
			return fmt.Sprintf("environment.context: %s is not a directory", e.Context)
		}
		env.Dockerfile, env.ContextDir = df, ctx
	default:
		return "environment: one of dockerfile or image is required"
	}
	t.Environment = env
	return ""
}

func buildSolution(t *task.Task, abs string, m *manifest) string {
	s := m.Solution
	sol := task.Solution{
		Env:     s.Env,
		WorkDir: m.Environment.Workdir,
		Timeout: seconds(s.TimeoutSec, 0),
	}
	switch task.SolutionKind(s.Kind) {
	case task.SolutionNone:
		// A task with no reference solution is legitimate (D3), but it has to
		// be said: an omitted [solution] table is more likely a mistake than
		// a decision.
		if s.Script != "" || s.Patch != "" || len(s.Env) > 0 || s.TimeoutSec != 0 {
			return `solution: kind = "none" takes no other fields`
		}
		t.Solution = task.Solution{Kind: task.SolutionNone}
		return ""
	case task.SolutionScript:
		if s.Patch != "" {
			return `solution.patch does not apply to kind = "script"`
		}
		if s.Script == "" {
			return `solution.script is required for kind = "script"`
		}
		p, reason := local(abs, "solution.script", s.Script)
		if reason != "" {
			return reason
		}
		if !isFile(p) {
			return fmt.Sprintf("solution.script: %s not found", s.Script)
		}
		// The script's directory is uploaded whole, so a script can ship
		// helpers beside it -- the same contract as Harbor's solution/.
		sol.Kind = task.SolutionScript
		sol.Dir, sol.Script, sol.MountPath = filepath.Dir(p), filepath.Base(p), solutionMount
	case task.SolutionPatch:
		if s.Script != "" {
			return `solution.script does not apply to kind = "patch"`
		}
		if len(s.Env) > 0 {
			return `solution.env does not apply to kind = "patch"`
		}
		if s.Patch == "" {
			return `solution.patch is required for kind = "patch"`
		}
		p, reason := local(abs, "solution.patch", s.Patch)
		if reason != "" {
			return reason
		}
		if !isFile(p) {
			return fmt.Sprintf("solution.patch: %s not found", s.Patch)
		}
		sol.Kind, sol.PatchFile = task.SolutionPatch, p
		// Recorded so the leak check can ask whether the patch sits inside
		// the build context; patch application never reads it.
		sol.Dir = filepath.Dir(p)
	case "":
		return `solution.kind is required ("script", "patch" or "none")`
	default:
		return fmt.Sprintf(`solution.kind %q is not one of "script", "patch", "none"`, s.Kind)
	}
	t.Solution = sol
	return ""
}

func buildTests(t *task.Task, abs string, m *manifest) string {
	ts := m.Tests
	if ts.Command == "" {
		return "tests.command is required"
	}
	tests := task.Tests{
		Command: ts.Command,
		Env:     ts.Env,
		WorkDir: m.Environment.Workdir,
		Timeout: seconds(ts.TimeoutSec, 0),
	}

	// dir and mount travel together. The command is the author's own, so
	// where the tests land is something it depends on and Skeptic cannot
	// choose for it: "./tests/run.sh" run from /app means /app/tests, not
	// wherever a default would have put them.
	switch {
	case ts.Dir != "" && ts.Mount == "":
		return "tests.mount is required with tests.dir: the container path the directory is copied to"
	case ts.Dir == "" && ts.Mount != "":
		return "tests.mount given without tests.dir"
	case ts.Dir != "":
		p, reason := local(abs, "tests.dir", ts.Dir)
		if reason != "" {
			return reason
		}
		if !isDir(p) {
			return fmt.Sprintf("tests.dir: %s is not a directory", ts.Dir)
		}
		if !strings.HasPrefix(ts.Mount, "/") {
			return fmt.Sprintf("tests.mount %q must be an absolute container path", ts.Mount)
		}
		tests.Dir, tests.MountPath = p, ts.Mount
	}

	sc := ts.Score
	switch task.ScoreKind(sc.Kind) {
	case task.ScoreRewardFile:
		if len(sc.Paths) == 0 {
			return `tests.score.paths is required for kind = "reward_file"`
		}
		for _, p := range sc.Paths {
			if !strings.HasPrefix(p, "/") {
				return fmt.Sprintf("tests.score.paths: %q must be an absolute container path", p)
			}
			if ext := filepath.Ext(p); ext != ".json" && ext != ".txt" {
				// run.go decides how to parse a reward by its extension; any
				// other would be read one way while the author meant another.
				return fmt.Sprintf("tests.score.paths: %q must end in .json or .txt", p)
			}
		}
		tests.Score = task.ScoreSpec{Kind: task.ScoreRewardFile, Paths: sc.Paths, RewardKey: sc.RewardKey}
	case task.ScoreExitCode:
		if len(sc.Paths) > 0 || sc.RewardKey != "" {
			return `tests.score: paths and reward_key do not apply to kind = "exit_code"`
		}
		tests.Score = task.ScoreSpec{Kind: task.ScoreExitCode}
	case "":
		return `tests.score.kind is required ("reward_file" or "exit_code")`
	default:
		return fmt.Sprintf(`tests.score.kind %q is not one of "reward_file", "exit_code"`, sc.Kind)
	}
	t.Tests = tests
	return ""
}

// local resolves a manifest path against the task directory, refusing
// absolute paths and any that climb out of it. A task should be movable as
// one directory; one that reaches outside is not self-contained, and Skeptic
// would be building or uploading something the manifest's reader cannot see.
func local(abs, field, p string) (string, string) {
	if p == "." {
		return abs, ""
	}
	if !filepath.IsLocal(p) {
		return "", fmt.Sprintf("%s: %q must be a relative path inside the task directory", field, p)
	}
	return filepath.Join(abs, p), ""
}

func seconds(v float64, fallback time.Duration) time.Duration {
	if v <= 0 {
		return fallback
	}
	return time.Duration(v * float64(time.Second))
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
