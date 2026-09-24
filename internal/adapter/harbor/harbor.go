// Package harbor adapts Harbor / Terminal-Bench 2.x task directories.
//
// The layout and the reward contract were read from the Harbor sources rather
// than inferred:
//
//	src/harbor/models/task/paths.py    task.toml, environment/, solution/, tests/
//	src/harbor/models/trial/paths.py   /logs/verifier, /tests, /solution
//	src/harbor/verifier/verifier.py    reward.json is preferred over reward.txt
//	src/harbor/agents/oracle.py        upload solution/, chmod, run solve.sh
package harbor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/skeptic-labs/skeptic/internal/task"
	"gopkg.in/yaml.v3"
)

// Container paths Harbor's harness guarantees. Test scripts in the wild write
// to these literal paths, so they are fixed, not configurable.
const (
	LogsDir      = "/logs"
	VerifierDir  = "/logs/verifier"
	TestsDir     = "/tests"
	SolutionDir  = "/solution"
	RewardJSON   = "/logs/verifier/reward.json"
	RewardText   = "/logs/verifier/reward.txt"
	defaultWork  = "/app"
	formatName   = "harbor"
	configFile   = "task.toml"
	buildTimeout = 30 * time.Minute
)

// Adapter loads Harbor-format tasks.
type Adapter struct{}

// New returns a Harbor adapter.
func New() *Adapter { return &Adapter{} }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return formatName }

// Detect reports a Harbor task: a task.toml beside an environment/ directory
// or a Dockerfile. task.toml alone is not enough, since adapter templates and
// registry fragments also carry one.
func (a *Adapter) Detect(dir string) bool {
	if !isFile(filepath.Join(dir, configFile)) {
		return false
	}
	return isDir(filepath.Join(dir, "environment")) || isFile(filepath.Join(dir, "Dockerfile"))
}

// config mirrors the subset of task.toml that affects how a task runs.
type config struct {
	SchemaVersion string `toml:"schema_version"`
	Task          struct {
		Name string `toml:"name"`
	} `toml:"task"`
	Verifier struct {
		TimeoutSec float64           `toml:"timeout_sec"`
		Env        map[string]string `toml:"env"`
	} `toml:"verifier"`
	Agent struct {
		TimeoutSec float64 `toml:"timeout_sec"`
	} `toml:"agent"`
	Solution struct {
		Env map[string]string `toml:"env"`
	} `toml:"solution"`
	Environment struct {
		BuildTimeoutSec float64 `toml:"build_timeout_sec"`
		CPUs            float64 `toml:"cpus"`
		MemoryMB        int     `toml:"memory_mb"`
		OS              string  `toml:"os"`
		WorkDir         string  `toml:"workdir"`
	} `toml:"environment"`
	Steps []struct {
		Name string `toml:"name"`
	} `toml:"steps"`
}

// Load reads a Harbor task directory.
func (a *Adapter) Load(dir string) (*task.Task, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var cfg config
	if _, err := toml.DecodeFile(filepath.Join(abs, configFile), &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configFile, err)
	}

	t := &task.Task{
		ID:     taskID(cfg.Task.Name, abs),
		Dir:    abs,
		Format: formatName,
	}

	// Refuse rather than mis-run. Each of these needs execution machinery
	// Skeptic does not have, and guessing would produce a confident wrong
	// verdict -- the exact failure this tool exists to catch.
	if len(cfg.Steps) > 0 {
		t.Unsupported = fmt.Sprintf("multi-step task (%d steps)", len(cfg.Steps))
		return t, nil
	}
	if os := cfg.Environment.OS; os != "" && os != "linux" {
		t.Unsupported = fmt.Sprintf("non-linux environment (os = %q)", os)
		return t, nil
	}

	envDir := filepath.Join(abs, "environment")
	if reason := composeUnsupported(envDir); reason != "" {
		t.Unsupported = reason
		return t, nil
	}

	dockerfile, contextDir, err := locateDockerfile(abs, envDir)
	if err != nil {
		return nil, err
	}

	workdir := cfg.Environment.WorkDir
	if workdir == "" {
		workdir = defaultWork
	}

	t.Environment = task.Environment{
		Dockerfile:   dockerfile,
		ContextDir:   contextDir,
		BuildTimeout: seconds(cfg.Environment.BuildTimeoutSec, buildTimeout),
		WorkDir:      workdir,
	}
	t.Solution = loadSolution(abs, cfg, workdir)

	testsDir := filepath.Join(abs, "tests")
	testScript := discoverScript(testsDir, "test")
	if testScript == "" {
		return nil, fmt.Errorf("no test script in %s", testsDir)
	}
	t.Tests = task.Tests{
		Dir:       testsDir,
		MountPath: TestsDir,
		// Harbor chmods the script then executes it by absolute path.
		Command: fmt.Sprintf("chmod +x %s/%s && %s/%s",
			TestsDir, filepath.Base(testScript), TestsDir, filepath.Base(testScript)),
		Env:     cfg.Verifier.Env,
		WorkDir: workdir,
		Timeout: seconds(cfg.Verifier.TimeoutSec, 0),
		Score: task.ScoreSpec{
			Kind: task.ScoreRewardFile,
			// Order matters: Harbor's verifier checks reward.json first.
			Paths: []string{RewardJSON, RewardText},
		},
	}
	return t, nil
}

func loadSolution(abs string, cfg config, workdir string) task.Solution {
	solDir := filepath.Join(abs, "solution")
	script := discoverScript(solDir, "solve")
	if script == "" {
		// Documented as optional upstream: "Without solution/, the Oracle
		// agent cannot run." Not an error; the oracle control is skipped.
		return task.Solution{Kind: task.SolutionNone}
	}
	return task.Solution{
		Kind:      task.SolutionScript,
		Dir:       solDir,
		Script:    filepath.Base(script),
		MountPath: SolutionDir,
		Env:       cfg.Solution.Env,
		WorkDir:   workdir,
		Timeout:   seconds(cfg.Agent.TimeoutSec, 0),
	}
}

// locateDockerfile prefers environment/Dockerfile, falling back to a
// Dockerfile at the task root for older hand-written tasks.
func locateDockerfile(abs, envDir string) (dockerfile, contextDir string, err error) {
	if p := filepath.Join(envDir, "Dockerfile"); isFile(p) {
		return p, envDir, nil
	}
	if p := filepath.Join(abs, "Dockerfile"); isFile(p) {
		return p, abs, nil
	}
	return "", "", fmt.Errorf("no Dockerfile in %s or %s", envDir, abs)
}

// composeUnsupported returns a reason when a compose file describes something
// Skeptic cannot faithfully run. See docs/decisions.md D4:
//
//   - a single service with a build unit is always supported — Skeptic starts
//     it detached and drives both controls through docker exec, overriding
//     whatever command it declares (Terminal-Bench boilerplate runs
//     `sleep infinity`);
//   - a single service with no build unit offers nothing to build or run;
//   - multiple services need orchestrated networking (a database the task's
//     tests talk to, for instance), which would make Skeptic's controls test
//     a different system than the one the benchmark grades — refused.
func composeUnsupported(envDir string) string {
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml"} {
		p := filepath.Join(envDir, name)
		if !isFile(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Sprintf("unreadable %s", name)
		}
		var doc struct {
			Services map[string]struct {
				// Build is present whenever the service declares a build
				// unit; its shape (a string path or a mapping) does not
				// matter here.
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

// discoverScript mirrors Harbor's own priority: .sh before .bat.
func discoverScript(dir, stem string) string {
	for _, ext := range []string{".sh", ".bat"} {
		if p := filepath.Join(dir, stem+ext); isFile(p) {
			return p
		}
	}
	return ""
}

// taskID prefers the packaged name from task.toml so IDs stay stable across
// checkouts, and falls back to the directory name.
func taskID(name, dir string) string {
	if name != "" {
		return name
	}
	return filepath.Base(dir)
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
