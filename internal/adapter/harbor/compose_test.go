package harbor

import (
	"os"
	"path/filepath"
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

// Harbor merges any task compose file on top of its base, which defines
// main. Every one of these therefore runs as a Compose project with the
// controls in main (docs/decisions.md D21).
func TestComposeTasksRunAsProjects(t *testing.T) {
	for _, tc := range []struct {
		name, compose string
	}{
		{"sidecar database", "services:\n  main:\n    depends_on: [database]\n  database:\n    image: postgres:16\n"},
		{"main overrides only", "services:\n  main:\n    environment:\n      - MODE=test\n"},
		// Refused before D21: no build unit. Harbor adds main from its base,
		// so this is main plus an alpine sidecar.
		{"image-only service", "services:\n  client:\n    image: alpine:3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTaskDir(t, map[string]string{"environment/docker-compose.yaml": tc.compose})
			tk, err := New().Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tk.Unsupported != "" {
				t.Fatalf("Unsupported = %q, want a compose task", tk.Unsupported)
			}
			c := tk.Environment.Compose
			if c == nil {
				t.Fatal("Environment.Compose = nil")
			}
			if c.Service != "main" || !c.Wait || len(c.Files) != 2 {
				t.Errorf("Compose = %+v, want service main, --wait, base + task file", c)
			}
			if filepath.Base(c.Files[1]) != "docker-compose.yaml" || c.ProjectDir != filepath.Join(dir, "environment") {
				t.Errorf("task file %q, project dir %q", c.Files[1], c.ProjectDir)
			}
			if c.Env["CONTEXT_DIR"] != c.ProjectDir {
				t.Errorf("CONTEXT_DIR = %q, want the environment directory", c.Env["CONTEXT_DIR"])
			}
		})
	}
}

// A task with no compose file is still a single container.
func TestNoComposeFileIsSingleContainer(t *testing.T) {
	tk, err := New().Load(writeTaskDir(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Environment.Compose != nil {
		t.Errorf("Compose = %+v, want nil", tk.Environment.Compose)
	}
}

// .yml is the other spelling docker compose accepts.
func TestComposeYmlExtensionHandled(t *testing.T) {
	dir := writeTaskDir(t, map[string]string{
		"environment/docker-compose.yml": "services:\n  client:\n    build: .\n",
	})
	tk, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tk.Environment.Compose == nil {
		t.Error("a .yml compose file was ignored")
	}
}

// An unparsable compose file must refuse the task, not silently treat it as
// absent: an unreadable description may hide a service that changes what is
// graded.
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
