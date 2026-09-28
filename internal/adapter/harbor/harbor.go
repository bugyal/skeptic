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

// config mirrors the subset of task.toml that affects how a task runs, read
// from src/harbor/models/task/config.py. Fields Skeptic cannot reproduce are
// parsed too, so that a task using them is refused rather than run on a
// quietly different setup.
type config struct {
	SchemaVersion string `toml:"schema_version"`
	Task          struct {
		Name string `toml:"name"`
	} `toml:"task"`
	Verifier verifierConfig `toml:"verifier"`
	Agent    phaseConfig    `toml:"agent"`
	Solution struct {
		Env  map[string]string `toml:"env"`
		User interface{}       `toml:"user"`
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
		// DockerImage is a prebuilt image Harbor uses instead of building
		// environment/Dockerfile.
		DockerImage  string   `toml:"docker_image"`
		NetworkMode  string   `toml:"network_mode"`
		AllowedHosts []string `toml:"allowed_hosts"`
	} `toml:"environment"`
	MultiStepRewardStrategy string       `toml:"multi_step_reward_strategy"`
	Steps                   []stepConfig `toml:"steps"`
}

// phaseConfig is [agent], and a step's agent table.
type phaseConfig struct {
	TimeoutSec   float64     `toml:"timeout_sec"`
	User         interface{} `toml:"user"`
	NetworkMode  string      `toml:"network_mode"`
	AllowedHosts []string    `toml:"allowed_hosts"`
}

// verifierConfig is [verifier], and a step's verifier table.
type verifierConfig struct {
	phaseConfig
	Env             map[string]string `toml:"env"`
	EnvironmentMode string            `toml:"environment_mode"`
	// Environment is only checked for presence: declaring one implies a
	// separate verifier container.
	Environment map[string]interface{} `toml:"environment"`
}

type stepConfig struct {
	Name        string             `toml:"name"`
	Agent       phaseConfig        `toml:"agent"`
	Verifier    verifierConfig     `toml:"verifier"`
	MinReward   interface{}        `toml:"min_reward"`
	Healthcheck *healthcheckConfig `toml:"healthcheck"`
	Artifacts   []interface{}      `toml:"artifacts"`
}

type healthcheckConfig struct {
	Command          string  `toml:"command"`
	IntervalSec      float64 `toml:"interval_sec"`
	TimeoutSec       float64 `toml:"timeout_sec"`
	StartPeriodSec   float64 `toml:"start_period_sec"`
	StartIntervalSec float64 `toml:"start_interval_sec"`
	Retries          int     `toml:"retries"`
}

// verifierMode resolves a verifier table's own mode the way
// models/task/verifier_mode.py _resolve_mode does: an explicit
// environment_mode, else "separate" when it declares an environment, else
// "" for not specified here.
func verifierMode(v verifierConfig) string {
	if v.EnvironmentMode != "" {
		return v.EnvironmentMode
	}
	if v.Environment != nil {
		return "separate"
	}
	return ""
}

// unreproducible names the first thing in cfg that Skeptic cannot run the
// way Harbor would, or returns "". Every one of these was ignored before
// D23, which meant running the task on a quietly different setup: a
// verifier in the wrong container, a network the task said it would not
// have, commands as the wrong user. Refusing is the honest answer until
// each is supported.
func unreproducible(cfg config, compose bool) string {
	// Separate verifier environments: resolve_step_verifier_mode, falling
	// back to resolve_task_verifier_mode, falling back to shared.
	taskMode := verifierMode(cfg.Verifier)
	if taskMode == "" {
		taskMode = "shared"
	}
	if len(cfg.Steps) == 0 && taskMode != "shared" {
		return fmt.Sprintf("verifier runs in a %s environment; Skeptic runs tests in the agent's container", taskMode)
	}
	for _, st := range cfg.Steps {
		mode := verifierMode(st.Verifier)
		if mode == "" {
			mode = taskMode
		}
		if mode != "shared" {
			return fmt.Sprintf("step %q verifier runs in a %s environment; Skeptic runs tests in the agent's container", st.Name, mode)
		}
	}

	// Users: Harbor runs the agent phase and the verifier as these.
	users := []struct {
		where string
		user  interface{}
	}{{"[agent]", cfg.Agent.User}, {"[verifier]", cfg.Verifier.User}, {"[solution]", cfg.Solution.User}}
	for _, st := range cfg.Steps {
		users = append(users,
			struct {
				where string
				user  interface{}
			}{fmt.Sprintf("step %q agent", st.Name), st.Agent.User},
			struct {
				where string
				user  interface{}
			}{fmt.Sprintf("step %q verifier", st.Name), st.Verifier.User})
	}
	for _, u := range users {
		if u.user != nil {
			return fmt.Sprintf("%s runs as user %v; Skeptic runs every command as the image's default user", u.where, u.user)
		}
	}

	// Network policy. The baseline applies to the whole environment; the
	// agent and verifier phases, task-wide or per step, may override it,
	// which Harbor enforces by switching policy mid-task through an egress
	// sidecar. Skeptic can reproduce a fixed baseline of public, or of no
	// network for a single container, and nothing that changes.
	baseline := cfg.Environment.NetworkMode
	if baseline == "" {
		baseline = "public"
	}
	switch {
	case baseline != "public" && baseline != "no-network":
		return fmt.Sprintf("network_mode %q is not reproduced", baseline)
	case len(cfg.Environment.AllowedHosts) > 0:
		return "a network allowlist is not reproduced"
	case baseline == "no-network" && compose:
		return "network_mode \"no-network\" for a multi-container task is not reproduced"
	}
	phases := []struct {
		where string
		p     phaseConfig
	}{{"[agent]", cfg.Agent}, {"[verifier]", cfg.Verifier.phaseConfig}}
	for _, st := range cfg.Steps {
		phases = append(phases,
			struct {
				where string
				p     phaseConfig
			}{fmt.Sprintf("step %q agent", st.Name), st.Agent},
			struct {
				where string
				p     phaseConfig
			}{fmt.Sprintf("step %q verifier", st.Name), st.Verifier.phaseConfig})
	}
	for _, ph := range phases {
		if len(ph.p.AllowedHosts) > 0 {
			return fmt.Sprintf("%s network allowlist is not reproduced", ph.where)
		}
		if ph.p.NetworkMode != "" && ph.p.NetworkMode != baseline {
			return fmt.Sprintf("%s switches network_mode to %q mid-task; Skeptic keeps one network setting for the whole run",
				ph.where, ph.p.NetworkMode)
		}
	}
	return ""
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

	if reason := unreproducible(cfg, composeFile != ""); reason != "" {
		t.Unsupported = reason
		return t, nil
	}
	if len(cfg.Steps) > 0 {
		t.Unsupported = fmt.Sprintf("multi-step task (%d steps)", len(cfg.Steps))
		return t, nil
	}

	// A docker_image wins over any Dockerfile: Harbor uses the prebuilt
	// image unless forced to build, which is a command-line choice
	// (environments/definition.py should_use_prebuilt_docker_image).
	image := cfg.Environment.DockerImage
	var dockerfile, contextDir string
	if image == "" {
		dockerfile, contextDir, err = locateDockerfile(abs, envDir)
		if err != nil {
			// Reported, not returned: a load error drops the task from a
			// set without a word (docs/decisions.md D15).
			t.Unsupported = err.Error()
			return t, nil
		}
		if composeFile != "" && contextDir != envDir {
			// Harbor's base compose file builds main from environment/, so
			// a compose task with only a root Dockerfile has nothing to
			// build.
			t.Unsupported = "compose task without environment/Dockerfile: Harbor builds the main service from environment/"
			return t, nil
		}
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
		NoNetwork:    cfg.Environment.NetworkMode == "no-network",
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
	switch {
	case composeFile != "":
		name, content := "docker-compose-build.yaml", composeBuildYAML
		if image != "" {
			name, content = "docker-compose-prebuilt.yaml", composePrebuiltYAML
		}
		base, err := composeBase(name, content)
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
				"CONTEXT_DIR":         envDir,
				"MAIN_IMAGE_NAME":     "hb__skeptic-${SKEPTIC_PROJECT}",
				"PREBUILT_IMAGE_NAME": image,
			},
			Wait: true,
		}
	case image != "":
		t.Environment.Image = image
		if uploadsEnvironment(envDir) {
			t.Environment.UploadDir = envDir
		}
	}
	t.Solution = loadSolution(abs, cfg, workdir)

	testsDir := filepath.Join(abs, "tests")
	testScript := discoverScript(testsDir, "test")
	if testScript == "" {
		t.Unsupported = "no test script in tests/"
		return t, nil
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
	return "", "", fmt.Errorf("no Dockerfile in environment/ or the task directory")
}

// mainService is the service Harbor runs the agent, the solution and the
// tests in.
const mainService = "main"

// composeBuildYAML and composePrebuiltYAML are
// src/harbor/environments/docker/docker-compose-build.yaml and
// docker-compose-prebuilt.yaml, verbatim. Harbor layers the task's own
// compose file on top of one of them: the prebuilt one when the task names a
// docker_image.
const composeBuildYAML = `services:
  main:
    build:
      context: ${CONTEXT_DIR}
    pull_policy: build
    command: [ "sh", "-c", "sleep infinity" ]
`

const composePrebuiltYAML = `services:
  main:
    image: ${PREBUILT_IMAGE_NAME}
    command: [ "sh", "-c", "sleep infinity" ]
`

var (
	baseMu    sync.Mutex
	baseDir   string
	basePaths = map[string]string{}
)

// composeBase writes one of Harbor's base compose files once per process and
// returns its path.
func composeBase(name, content string) (string, error) {
	baseMu.Lock()
	defer baseMu.Unlock()
	if p, ok := basePaths[name]; ok {
		return p, nil
	}
	if baseDir == "" {
		dir, err := os.MkdirTemp("", "skeptic-harbor-compose-")
		if err != nil {
			return "", err
		}
		baseDir = dir
	}
	p := filepath.Join(baseDir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "", err
	}
	basePaths[name] = p
	return p, nil
}

// uploadsEnvironment is should_upload_environment_dir
// (environments/definition.py), given that the task names a docker_image:
// Harbor copies environment/ into the working directory when there is no
// Dockerfile or docker-compose.yaml to build from and the directory holds
// something.
func uploadsEnvironment(envDir string) bool {
	if isFile(filepath.Join(envDir, "Dockerfile")) || isFile(filepath.Join(envDir, "docker-compose.yaml")) {
		return false
	}
	entries, err := os.ReadDir(envDir)
	return err == nil && len(entries) > 0
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
