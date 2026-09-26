package harbor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each case is checked against EnvironmentConfig._parse_size_to_mb in
// src/harbor/models/task/config.py.
func TestParseSizeToMB(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"2G", 2048, true},
		{"2g", 2048, true}, // upstream upper-cases first
		{" 512M ", 512, true},
		{"1.5G", 1536, true},
		{"1K", 0, true}, // int() truncates towards zero
		{"2GB", 0, false},
		{"2048", 0, false}, // no unit is an error upstream
		{"infG", 0, false},
		{"G", 0, false},
	} {
		got, err := parseSizeToMB(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseSizeToMB(%q) = %d, %v; want %d, ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

func TestMemoryLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		memoryMB int
		legacy   interface{}
		want     int
		wantErr  string
	}{
		{"memory_mb only", 2048, nil, 2048, ""},
		{"legacy only", 0, "4G", 4096, ""},
		{"agreeing", 2048, "2G", 2048, ""},
		{"conflicting", 1024, "2G", 0, "conflicting"},
		// Upstream pops the legacy key and only migrates strings, so a
		// number there is dropped, not read as megabytes.
		{"legacy number dropped", 0, int64(2048), 0, ""},
		{"negative is no limit", -1, nil, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := memoryLimit(tc.memoryMB, tc.legacy)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("memoryLimit = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// Limits declared in task.toml must reach the task, and a size upstream
// would refuse to load must make the task unsupported, not unlimited.
func TestLoadResourceLimits(t *testing.T) {
	write := func(t *testing.T, env string) string {
		dir := t.TempDir()
		files := map[string]string{
			"task.toml":              "[environment]\n" + env,
			"environment/Dockerfile": "FROM alpine:3\n",
			"tests/test.sh":          "#!/bin/sh\n",
		}
		for name, body := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	got, err := New().Load(write(t, "cpus = 2\nmemory_mb = 2048\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment.CPUs != 2 || got.Environment.MemoryMB != 2048 {
		t.Errorf("CPUs, MemoryMB = %v, %d; want 2, 2048", got.Environment.CPUs, got.Environment.MemoryMB)
	}

	got, err = New().Load(write(t, "memory = '8G'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment.MemoryMB != 8192 {
		t.Errorf("legacy memory = '8G' gave MemoryMB = %d, want 8192", got.Environment.MemoryMB)
	}

	got, err = New().Load(write(t, "memory = 'lots'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Unsupported, "invalid memory size") {
		t.Errorf("Unsupported = %q, want the size error", got.Unsupported)
	}
}
