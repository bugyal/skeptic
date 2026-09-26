// Package tbench adapts Terminal-Bench 1.x task directories.
//
// The layout and its execution contract were read from the upstream sources
// (laude-institute/terminal-bench, original-tasks/hello-world) rather than
// inferred:
//
//	task.yaml               instruction, parser_name, timeouts
//	Dockerfile              at the task root (no environment/ nesting)
//	docker-compose.yaml     generated boilerplate: one `client` service that
//	                        builds the Dockerfile and runs `sleep infinity`
//	run-tests.sh            the grading entrypoint, run from the image root
//	tests/                  pytest files, copied to TEST_DIR at runtime
//	solution.sh             the reference solve script
//
// Skeptic builds the Dockerfile directly and starts the container detached
// (compose is boilerplate, per docs/decisions.md D4), so the adapter mirrors
// what Terminal-Bench's harness does at runtime: copy tests into $TEST_DIR,
// run run-tests.sh, and grade from pytest's exit status.
package tbench

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bugyal/skeptic/internal/task"
	"gopkg.in/yaml.v3"
)

// Container paths Terminal-Bench's harness guarantees.
const (
	// TestsMount is where tests/ is copied inside the container, matching the
	// TEST_DIR the boilerplate compose file sets.
	TestsMount = "/tests"
	// SolMount is where solution.sh is copied inside the container.
	SolMount = "/solution"
	// TestDirEnv is the variable run-tests.sh reads the test directory from.
	TestDirEnv = "TEST_DIR"
)

const (
	formatName     = "tbench"
	taskYAML       = "task.yaml"
	runTestsScript = "run-tests.sh"
	solutionScript = "solution.sh"
	// buildTimeout matches Terminal-Bench's own default for image builds.
	buildTimeout = 30 * time.Minute
	// defaultTestTimeout matches task.yaml's documented fallback when
	// max_test_timeout_sec is absent.
	defaultTestTimeout = 5 * time.Minute
)

// Adapter loads Terminal-Bench 1.x tasks.
type Adapter struct{}

// New returns a Terminal-Bench 1.x adapter.
func New() *Adapter { return &Adapter{} }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return formatName }

// Detect reports a Terminal-Bench 1.x task: a task.yaml beside run-tests.sh.
// The Dockerfile is implied but not required for detection — a task without
// one is still recognisable, and Load reports it as unsupported.
func (a *Adapter) Detect(dir string) bool {
	if !isFile(filepath.Join(dir, taskYAML)) {
		return false
	}
	return isFile(filepath.Join(dir, runTestsScript))
}

// cfg mirrors the task.yaml fields that affect how a task runs.
type cfg struct {
	Instruction        string   `yaml:"instruction"`
	ParserName         string   `yaml:"parser_name"`
	MaxAgentTimeoutSec float64  `yaml:"max_agent_timeout_sec"`
	MaxTestTimeoutSec  float64  `yaml:"max_test_timeout_sec"`
	RequiredEnvVars    []string `yaml:"required_env_vars"`
}

// Load reads a Terminal-Bench 1.x task directory.
func (a *Adapter) Load(dir string) (*task.Task, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(abs, taskYAML))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", taskYAML, err)
	}
	var c cfg
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", taskYAML, err)
	}

	t := &task.Task{
		ID:          filepath.Base(abs),
		Dir:         abs,
		Format:      formatName,
		Instruction: c.Instruction,
	}

	// Refuse rather than mis-run, mirroring the harbor adapter's posture.
	if reason := composeUnsupported(abs); reason != "" {
		t.Unsupported = reason
		return t, nil
	}

	dockerfile := filepath.Join(abs, "Dockerfile")
	if !isFile(dockerfile) {
		t.Unsupported = "no Dockerfile in the task directory"
		return t, nil
	}

	testsDir := filepath.Join(abs, "tests")
	entries, err := os.ReadDir(testsDir)
	if err != nil || len(entries) == 0 {
		t.Unsupported = "no tests directory (or it is empty)"
		return t, nil
	}

	cpus, memoryMB, reason := composeLimits(abs)
	if reason != "" {
		t.Unsupported = reason
		return t, nil
	}

	t.Environment = task.Environment{
		Dockerfile:   dockerfile,
		ContextDir:   abs,
		BuildTimeout: buildTimeout,
		CPUs:         cpus,
		MemoryMB:     memoryMB,
	}

	sol, err := stageSolution(abs)
	if err != nil {
		return nil, err
	}
	t.Solution = sol

	t.Tests = task.Tests{
		Dir:       testsDir,
		MountPath: TestsMount,
		// Terminal-Bench runs the grading entrypoint from the image root with
		// TEST_DIR pointing at the copied-in tests.
		Command: fmt.Sprintf("./%s", runTestsScript),
		Env: map[string]string{
			TestDirEnv: TestsMount,
		},
		WorkDir: "/",
		Timeout: seconds(c.MaxTestTimeoutSec, defaultTestTimeout),
		Score: task.ScoreSpec{
			// Terminal-Bench 1.x grades by pytest's exit status: run-tests.sh
			// propagates it, and the harness maps 0 -> passed. The mapping is
			// documented in README ("Terminal-Bench 1.x exit-status mapping")
			// per docs/decisions.md D2's fallback.
			Kind: task.ScoreExitCode,
		},
	}
	return t, nil
}

// stageSolution prepares the oracle's material.
//
// Terminal-Bench ships solution.sh at the task root, but the engine's script
// control uploads Solution.Dir wholesale into MountPath. Pointing Dir at the
// task root would therefore copy the entire task — tests/ and the Dockerfile
// included — into the container, which is exactly the leak Skeptic exists to
// flag. The script is staged into a scratch directory holding only the files
// the oracle is meant to carry in.
//
// The staging directory lives as long as the process. Per-task volumes of a
// few KB are irrelevant next to the image builds a sweep already performs, and
// a deterministic sweep cannot assume a writable temp dir is safe to reuse, so
// nothing is reclaimed early.
func stageSolution(abs string) (task.Solution, error) {
	p := filepath.Join(abs, solutionScript)
	if !isFile(p) {
		// Optional upstream: some tasks are graded on tests alone. The oracle
		// control is skipped rather than errored, and the task still gets its
		// nop control.
		return task.Solution{Kind: task.SolutionNone}, nil
	}

	stage, err := os.MkdirTemp("", "skeptic-tbench-solution-")
	if err != nil {
		return task.Solution{}, fmt.Errorf("staging solution: %w", err)
	}
	if err := copyFile(p, filepath.Join(stage, solutionScript)); err != nil {
		os.RemoveAll(stage)
		return task.Solution{}, fmt.Errorf("staging solution: %w", err)
	}
	return task.Solution{
		Kind:      task.SolutionScript,
		Dir:       stage,
		Script:    solutionScript,
		MountPath: SolMount,
		WorkDir:   "/",
	}, nil
}

// composeUnsupported refuses tasks whose compose file declares anything other
// than the generated single-service boilerplate. See docs/decisions.md D4.
func composeUnsupported(dir string) string {
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml"} {
		p := filepath.Join(dir, name)
		if !isFile(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Sprintf("unreadable %s", name)
		}
		var doc struct {
			Services map[string]struct {
				Build interface{} `yaml:"build"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return fmt.Sprintf("unparsable %s", name)
		}
		switch {
		case len(doc.Services) > 1:
			return fmt.Sprintf("multi-container task (%d compose services)", len(doc.Services))
		case len(doc.Services) == 1:
			for _, svc := range doc.Services {
				if svc.Build == nil {
					return "compose service has no build unit; nothing to build or run"
				}
			}
		}
	}
	return ""
}

// composeLimits reads the resource limits of the single compose service.
// Terminal-Bench starts tasks with `docker compose up`, and Compose applies
// deploy.resources.limits outside Swarm, so a task declaring
// `memory: 4.0G` runs capped at 4 GiB upstream and must here too. The
// service-level mem_limit and cpus are honoured when deploy does not set a
// value. Reservations are not limits and are ignored.
func composeLimits(dir string) (cpus float64, memoryMB int, reason string) {
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var doc struct {
			Services map[string]struct {
				MemLimit interface{} `yaml:"mem_limit"`
				CPUs     interface{} `yaml:"cpus"`
				Deploy   struct {
					Resources struct {
						Limits struct {
							Memory interface{} `yaml:"memory"`
							CPUs   interface{} `yaml:"cpus"`
						} `yaml:"limits"`
					} `yaml:"resources"`
				} `yaml:"deploy"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return 0, 0, fmt.Sprintf("unparsable %s", name)
		}
		// composeUnsupported has already required exactly one service.
		for _, svc := range doc.Services {
			lim := svc.Deploy.Resources.Limits
			mem, cpu := lim.Memory, lim.CPUs
			if mem == nil {
				mem = svc.MemLimit
			}
			if cpu == nil {
				cpu = svc.CPUs
			}
			if mem != nil {
				bytes, err := ramInBytes(mem)
				if err != nil {
					return 0, 0, fmt.Sprintf("%s: memory limit %v: %v", name, mem, err)
				}
				memoryMB = int(bytes / (1024 * 1024))
				if memoryMB == 0 {
					// Below a megabyte cannot be expressed in MB, and no
					// real task would start in it; refuse rather than drop.
					return 0, 0, fmt.Sprintf("%s: memory limit %v is below 1 MB", name, mem)
				}
			}
			if cpu != nil {
				v, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(cpu)), 64)
				if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
					return 0, 0, fmt.Sprintf("%s: cpu limit %v is not a number", name, cpu)
				}
				cpus = v
			}
		}
		return cpus, memoryMB, ""
	}
	return 0, 0, ""
}

// ramSize is the byte-size syntax Compose accepts for memory, from
// docker/go-units RAMInBytes: a number, then an optional unit k, m, g, t or
// p, optionally followed by b or ib, all binary multiples, case-insensitive.
var ramSize = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([kmgtp])?(?:i?b)?$`)

func ramInBytes(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	}
	m := ramSize.FindStringSubmatch(strings.TrimSpace(fmt.Sprint(v)))
	if m == nil {
		return 0, fmt.Errorf("not a size Compose accepts")
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	mult := map[string]float64{"": 1, "k": 1 << 10, "m": 1 << 20, "g": 1 << 30, "t": 1 << 40, "p": 1 << 50}[strings.ToLower(m[2])]
	return int64(f * mult), nil
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

// copyFile copies a regular file, preserving the permission bits. It refuses
// symlinks and anything else that is not a regular file: a symlinked
// solution.sh is not a solve script, and following it is how host state
// leaks into what the oracle control is meant to carry in.
func copyFile(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", src)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, fi.Mode().Perm())
}
