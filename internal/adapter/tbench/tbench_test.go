package tbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/task"
)

// writeTaskDir lays down task.yaml + run-tests.sh, the minimum Detect
// requires, plus any extra files the caller wants.
func writeTaskDir(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		taskYAML: strings.Join([]string{
			"instruction: Do the task.",
			"parser_name: pytest",
			"max_agent_timeout_sec: 300",
			"max_test_timeout_sec: 60",
		}, "\n") + "\n",
		runTestsScript: "#!/bin/sh\nexit 0\n",
	}
	for name, content := range extra {
		files[name] = content
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetect(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"Dockerfile":      "FROM alpine:3\n",
		"solution.sh":     "#!/bin/sh\ntrue\n",
		"tests/test_x.py": "def test_x():\n    assert True\n",
	})
	a := New()
	if !a.Detect(dir) {
		t.Fatal("Detect = false for a full Terminal-Bench layout")
	}
}

func TestDetectRequiresTaskYAMLAndRunTests(t *testing.T) {
	dir := writeTaskDir(t, nil)
	// Drop run-tests.sh: task.yaml alone is not a Terminal-Bench task.
	if err := os.Remove(filepath.Join(dir, runTestsScript)); err != nil {
		t.Fatal(err)
	}
	if New().Detect(dir) {
		t.Fatal("Detect = true without run-tests.sh")
	}
}

func TestDetectDoesNotClaimHarborLayout(t *testing.T) {
	// harbor has task.toml, not task.yaml; the two adapters must not fight
	// over the same directory.
	dir := writeTaskDir(t, nil)
	if err := os.Rename(filepath.Join(dir, taskYAML), filepath.Join(dir, "task.toml")); err != nil {
		t.Fatal(err)
	}
	if New().Detect(dir) {
		t.Fatal("Detect claimed a task.toml directory")
	}
}

func TestLoad(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"Dockerfile":      "FROM python:3.11-slim\nWORKDIR /app\n",
		"solution.sh":     "#!/bin/sh\ntrue\n",
		"tests/test_x.py": "def test_x():\n    assert True\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported != "" {
		t.Fatalf("Unsupported = %q", tk.Unsupported)
	}
	if tk.ID != filepath.Base(dir) || tk.Format != formatName {
		t.Errorf("ID/Format = %q/%q", tk.ID, tk.Format)
	}
	if tk.Environment.Dockerfile == "" || tk.Environment.ContextDir == "" {
		t.Errorf("Environment not wired: %+v", tk.Environment)
	}

	if tk.Solution.Kind != task.SolutionScript {
		t.Fatalf("Solution.Kind = %q, want script", tk.Solution.Kind)
	}
	// The staged directory must hold only the solve script. Handing the engine
	// the task root would copy tests/ into /solution — a leak this adapter
	// exists to prevent.
	entries, err := os.ReadDir(tk.Solution.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != solutionScript {
		t.Errorf("staged solution dir contents = %v, want only %s", names(entries), solutionScript)
	}
	if tk.Solution.MountPath != SolMount || tk.Solution.WorkDir != "/" {
		t.Errorf("Solution mount/workdir = %q/%q", tk.Solution.MountPath, tk.Solution.WorkDir)
	}

	if tk.Tests.MountPath != TestsMount {
		t.Errorf("Tests.MountPath = %q", tk.Tests.MountPath)
	}
	if tk.Tests.Command != "./"+runTestsScript {
		t.Errorf("Tests.Command = %q", tk.Tests.Command)
	}
	if tk.Tests.Env[TestDirEnv] != TestsMount {
		t.Errorf("Tests.Env[%s] = %q", TestDirEnv, tk.Tests.Env[TestDirEnv])
	}
	if tk.Tests.WorkDir != "/" {
		t.Errorf("Tests.WorkDir = %q", tk.Tests.WorkDir)
	}
	if tk.Tests.Score.Kind != task.ScoreExitCode {
		t.Errorf("Tests.Score.Kind = %q, want exit_code (D2 fallback)", tk.Tests.Score.Kind)
	}
	if tk.Tests.Timeout != 60*time.Second {
		t.Errorf("Tests.Timeout = %v, want 60s from task.yaml", tk.Tests.Timeout)
	}
}

// Without max_test_timeout_sec the documented 5-minute default applies.
func TestLoadTestTimeoutFallback(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		taskYAML:          "instruction: Do the task.\n",
		"Dockerfile":      "FROM python:3.11-slim\n",
		"tests/test_x.py": "def test_x():\n    assert True\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Tests.Timeout != defaultTestTimeout {
		t.Errorf("Timeout = %v, want the %v default", tk.Tests.Timeout, defaultTestTimeout)
	}
}

// A task without solution.sh is valid upstream; the oracle control is skipped,
// not errored, and the nop control still runs.
func TestLoadNoSolution(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"Dockerfile":      "FROM python:3.11-slim\n",
		"tests/test_x.py": "def test_x():\n    assert True\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Solution.Kind != task.SolutionNone {
		t.Errorf("Solution.Kind = %q, want none", tk.Solution.Kind)
	}
	if tk.Solution.Available() {
		t.Error("Solution.Available() = true without solution.sh")
	}
}

func TestLoadNoDockerfileUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"tests/test_x.py": "def test_x():\n    assert True\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty without a Dockerfile")
	}
}

func TestLoadNoTestsUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"Dockerfile": "FROM python:3.11-slim\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty without a tests directory")
	}
}

// The Terminal-Bench 1.x boilerplate compose file: one `client` service whose
// command is `sleep infinity`. D4 rules it supported: Skeptic starts the
// container detached and drives the controls through exec, overriding the
// command entirely.
func TestComposeBoilerplateSupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"Dockerfile":      "FROM python:3.11-slim\n",
		"tests/test_x.py": "def test_x():\n    assert True\n",
		"docker-compose.yaml": strings.Join([]string{
			"services:",
			"  client:",
			"    build: .",
			"    command: [\"sh\", \"-c\", \"sleep infinity\"]",
			"    environment:",
			"      - TEST_DIR=/tests",
			"    volumes:",
			"      - ./logs:/logs",
		}, "\n"),
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported != "" {
		t.Fatalf("Unsupported = %q, want empty for the single-service boilerplate", tk.Unsupported)
	}
}

func TestComposeMultiServiceUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"docker-compose.yaml": strings.Join([]string{
			"services:",
			"  client:",
			"    build: .",
			"  database:",
			"    image: postgres:16",
		}, "\n"),
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty, want a multi-container reason")
	}
	if want := "2 compose services"; !strings.Contains(tk.Unsupported, want) {
		t.Errorf("Unsupported = %q, want it to mention %q", tk.Unsupported, want)
	}
}

func TestComposeSingleServiceNoBuildUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"docker-compose.yaml": "services:\n  client:\n    image: alpine:3\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty, want a no-buildable-unit reason")
	}
}

// .yml is the other spelling docker compose accepts.
func TestComposeYmlExtensionHandled(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"docker-compose.yml": "services:\n  client:\n    build: .\n",
	})
	if _, err := New().Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// An unparsable compose file must refuse the task, not silently treat it as
// absent — an unreadable description may hide a second service.
func TestComposeUnparsableRefused(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"docker-compose.yaml": "services: [oops",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty, want an unparsable-compose reason")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
