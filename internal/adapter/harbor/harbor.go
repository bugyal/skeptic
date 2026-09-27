// Package harbor adapts Harbor / Terminal-Bench 2.x task directories.
//
// The layout and the reward contract were read from the Harbor sources rather
// than inferred:
//
//	src/harbor/models/task/paths.py    task.toml, environment/, solution/, tests/
//	src/harbor/models/trial/paths.py   /logs/verifier, /tests, /solution
//	src/harbor/verifier/verifier.py    reward.json is preferred over reward.txt
//	src/harbor/agents/oracle.py        upload solution/, chmod, run solve.sh
//	src/harbor/models/task/config.py   cpus, memory_mb, the legacy memory field
//	src/harbor/environments/docker/    both applied as hard limits by default
package harbor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bugyal/skeptic/internal/task"
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
		// Memory is the deprecated spelling ("2G", "512M"), still accepted
		// upstream and migrated to memory_mb.
		Memory  interface{} `toml:"memory"`
		OS      string      `toml:"os"`
		WorkDir string      `toml:"workdir"`
		// Env is [environment.env]: set in main when it starts.
		Env map[string]string `toml:"env"`
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
	if b, err := os.ReadFile(filepath.Join(abs, "instruction.md")); err == nil {
		t.Instruction = string(b)
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
	composeFile, reason := taskCompose(envDir)
	if reason != "" {
		t.Unsupported = reason
		return t, nil
	}

	dockerfile, contextDir, err := locateDockerfile(abs, envDir)
	if err != nil {
		return nil, err
	}
	if composeFile != "" && contextDir != envDir {
		// Harbor's base compose file builds main from environment/, so a
		// compose task with only a root Dockerfile has nothing to build.
		t.Unsupported = "compose task without environment/Dockerfile: Harbor builds the main service from environment/"
		return t, nil
	}

	// Harbor passes -w only when task.toml sets workdir; otherwise every
	// command runs in the image's own WORKDIR (environments/docker/docker.py
	// _exec; agents/oracle.py and verifier/verifier.py pass no cwd). An empty
	// value here does the same. Defaulting to /app, as this adapter once did,
	// ran describe-image's tests in /app instead of its /workspace, and failed
	// outright on an image with no /app once tasks ran under Compose.
	workdir := cfg.Environment.WorkDir

	// Harbor resolves ${VAR} and ${VAR:-default} in all three env tables
	// from the host (utils/env.py resolve_env_vars, called by the
	// environment, the oracle agent and the verifier), and refuses the task
	// when a required variable is unset.
	for _, table := range []*map[string]string{&cfg.Environment.Env, &cfg.Solution.Env, &cfg.Verifier.Env} {
		resolved, err := resolveEnv(*table)
		if err != nil {
			t.Unsupported = err.Error()
			return t, nil
		}
		*table = resolved
	}

	memoryMB, err := memoryLimit(cfg.Environment.MemoryMB, cfg.Environment.Memory)
	if err != nil {
		// Harbor refuses to load such a task, so it has no defined behaviour
		// to reproduce.
		t.Unsupported = err.Error()
		return t, nil
	}

	t.Environment = task.Environment{
		Dockerfile:   dockerfile,
		ContextDir:   contextDir,
		BuildTimeout: seconds(cfg.Environment.BuildTimeoutSec, buildTimeout),
		WorkDir:      workdir,
		CPUs:         max(cfg.Environment.CPUs, 0),
		MemoryMB:     memoryMB,
		Env:          cfg.Environment.Env,
		// Harbor's base compose file sets main's command to sleep infinity
		// and leaves the image's ENTRYPOINT alone, so an entrypoint that
		// prepares the container runs.
		KeepEntrypoint: true,
	}
	if composeFile != "" {
		base, err := composeBase()
		if err != nil {
			return nil, err
		}
		// Harbor's order: its base file, then the task's, so the task can
		// override main and add the services main depends on.
		t.Environment.Compose = &task.Compose{
			Files:      []string{base, composeFile},
			ProjectDir: envDir,
			Service:    mainService,
			Env: map[string]string{
				"CONTEXT_DIR":     envDir,
				"MAIN_IMAGE_NAME": "hb__skeptic-${SKEPTIC_PROJECT}",
			},
			Wait: true,
		}
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

// mainService is the service Harbor runs the agent, the solution and the
// tests in.
const mainService = "main"

// composeBaseYAML is src/harbor/environments/docker/docker-compose-build.yaml,
// verbatim. Harbor layers the task's own compose file on top of it.
const composeBaseYAML = `services:
  main:
    build:
      context: ${CONTEXT_DIR}
    pull_policy: build
    command: [ "sh", "-c", "sleep infinity" ]
`

var (
	baseOnce sync.Once
	basePath string
	baseErr  error
)

// composeBase writes Harbor's base compose file once per process and returns
// its path.
func composeBase() (string, error) {
	baseOnce.Do(func() {
		dir, err := os.MkdirTemp("", "skeptic-harbor-compose-")
		if err != nil {
			baseErr = err
			return
		}
		basePath = filepath.Join(dir, "docker-compose-build.yaml")
		baseErr = os.WriteFile(basePath, []byte(composeBaseYAML), 0o644)
	})
	return basePath, baseErr
}

// taskCompose finds the task's own compose file, if any. Harbor merges any
// such file with its base, so every compose task runs as a Compose project
// with the controls in main (docs/decisions.md D21, superseding D4 for this
// format). A file that will not parse is refused: it may describe services
// that would change what is being graded.
func taskCompose(envDir string) (path, reason string) {
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml"} {
		p := filepath.Join(envDir, name)
		if !isFile(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Sprintf("unreadable %s", name)
		}
		var doc struct {
			Services map[string]interface{} `yaml:"services"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return "", fmt.Sprintf("unparsable %s", name)
		}
		return p, ""
	}
	return "", ""
}

// memoryLimit resolves memory_mb, migrating the deprecated memory field the
// way EnvironmentConfig._migrate_legacy_resource_fields does: a string is
// parsed as a size and must agree with memory_mb when both are set, and any
// other type is dropped. Zero or less means no limit.
func memoryLimit(memoryMB int, legacy interface{}) (int, error) {
	if str, ok := legacy.(string); ok {
		mb, err := parseSizeToMB(str)
		if err != nil {
			return 0, err
		}
		if memoryMB != 0 && memoryMB != mb {
			return 0, fmt.Errorf("conflicting memory (%q = %d MB) and memory_mb (%d)", str, mb, memoryMB)
		}
		memoryMB = mb
	}
	return max(memoryMB, 0), nil
}

// parseSizeToMB mirrors EnvironmentConfig._parse_size_to_mb, including its
// truncation towards zero.
func parseSizeToMB(size string) (int, error) {
	s := strings.ToUpper(strings.TrimSpace(size))
	var scale float64
	switch {
	case strings.HasSuffix(s, "G"):
		scale = 1024
	case strings.HasSuffix(s, "M"):
		scale = 1
	case strings.HasSuffix(s, "K"):
		scale = 1.0 / 1024
	default:
		return 0, fmt.Errorf("invalid memory size %q: expected a form like 1G or 512M", size)
	}
	v, err := strconv.ParseFloat(s[:len(s)-1], 64)
	// Python's int() raises on infinity and NaN; Go's conversion would not.
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("invalid memory size %q: expected a form like 1G or 512M", size)
	}
	return int(v * scale), nil
}

// envTemplate is utils/env.py's _TEMPLATE_PATTERN, matched against the whole
// value as fullmatch does.
var envTemplate = regexp.MustCompile(`^\$\{([^}:]+)(?::-(.*))?\}$`)

// resolveEnv mirrors resolve_env_vars: a value that is exactly ${VAR} or
// ${VAR:-default} is replaced from the host environment, anything else is a
// literal, and a template with no host value and no default is an error.
func resolveEnv(env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		m := envTemplate.FindStringSubmatch(v)
		if m == nil {
			out[k] = v
			continue
		}
		if host, ok := os.LookupEnv(m[1]); ok {
			out[k] = host
		} else if strings.Contains(v, ":-") {
			out[k] = m[2]
		} else {
			return nil, fmt.Errorf("task needs host environment variable %s (for %s), which is not set", m[1], k)
		}
	}
	return out, nil
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
