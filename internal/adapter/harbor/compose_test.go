package harbor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTaskDir lays down task.toml + Dockerfile, the minimum Detect requires,
// plus any extra files the caller wants (e.g. an environment/docker-compose.yaml).
func writeTaskDir(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "environment"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"task.toml":              "schema_version = \"1.4\"\n\n[task]\nname = \"compose/test\"\n",
		"environment/Dockerfile": "FROM alpine:3\nWORKDIR /app\n",
		"instruction.md":         "Do the task.\n",
		"tests/test.sh":          "#!/bin/sh\nexit 0\n",
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

// The Terminal-Bench 1.x boilerplate compose file: one `client` service whose
// command is `sleep infinity`. D4 rules it supported: Skeptic starts the
// container detached and drives the controls through exec, overriding the
// command entirely.
func TestComposeBoilerplateSupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"environment/docker-compose.yaml": strings.Join([]string{
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

// Two services mean the task grades a multi-container system that Skeptic
// would not reproduce; the task must be refused as unsupported, not mis-run.
func TestComposeMultiServiceUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"environment/docker-compose.yaml": strings.Join([]string{
			"services:",
			"  client:",
			"    build: .",
			"    command: [\"sh\", \"-c\", \"sleep infinity\"]",
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

// A compose file with one service but no build unit cannot be started.
func TestComposeSingleServiceNoBuildUnsupported(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"environment/docker-compose.yaml": strings.Join([]string{
			"services:",
			"  client:",
			"    image: alpine:3",
		}, "\n"),
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
		"environment/docker-compose.yml": "services:\n  client:\n    build: .\n",
	})
	if _, err := New().Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// An unparsable compose file must refuse the task, not silently treat it as
// absent — an unreadable description may hide a second service.
func TestComposeUnparsableRefused(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"environment/docker-compose.yaml": "services: [oops",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Unsupported == "" {
		t.Fatal("Unsupported = empty, want an unparsable-compose reason")
	}
}
