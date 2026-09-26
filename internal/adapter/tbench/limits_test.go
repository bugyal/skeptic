package tbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shape of hf-lora-adapter's compose file in terminal-bench-1.
const lora = `services:
  client:
    build:
      dockerfile: Dockerfile
    command: [ "sh", "-c", "sleep infinity" ]
    deploy:
      resources:
        limits:
          memory: 4.0G
        reservations:
          memory: 2.0G
`

func TestComposeLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		compose string
		cpus    float64
		mem     int
		reason  string
	}{
		{"deploy limit, reservation ignored", lora, 0, 4096, ""},
		{"deploy cpus", "services:\n  c:\n    build: .\n    deploy:\n      resources:\n        limits:\n          cpus: '0.5'\n", 0.5, 0, ""},
		{"service mem_limit", "services:\n  c:\n    build: .\n    mem_limit: 512m\n", 0, 512, ""},
		{"deploy wins", "services:\n  c:\n    build: .\n    mem_limit: 512m\n    deploy:\n      resources:\n        limits:\n          memory: 1gb\n", 0, 1024, ""},
		{"none", "services:\n  c:\n    build: .\n", 0, 0, ""},
		{"bad memory", "services:\n  c:\n    build: .\n    mem_limit: plenty\n", 0, 0, "memory limit"},
		{"tiny memory", "services:\n  c:\n    build: .\n    mem_limit: 100k\n", 0, 0, "below 1 MB"},
		{"bad cpus", "services:\n  c:\n    build: .\n    cpus: many\n", 0, 0, "cpu limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte(tc.compose), 0o644); err != nil {
				t.Fatal(err)
			}
			cpus, mem, reason := composeLimits(dir)
			if tc.reason != "" {
				if !strings.Contains(reason, tc.reason) {
					t.Fatalf("reason = %q, want it to contain %q", reason, tc.reason)
				}
				return
			}
			if reason != "" || cpus != tc.cpus || mem != tc.mem {
				t.Fatalf("composeLimits = %v, %d, %q; want %v, %d", cpus, mem, reason, tc.cpus, tc.mem)
			}
		})
	}
}
