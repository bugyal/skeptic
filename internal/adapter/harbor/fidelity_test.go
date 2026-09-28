package harbor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fidelityTask writes a Harbor task whose task.toml is toml plus files.
func fidelityTask(t *testing.T, toml string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{
		"task.toml":              toml,
		"environment/Dockerfile": "FROM alpine:3\n",
		"tests/test.sh":          "#!/bin/sh\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for name, body := range all {
		if body == "" && name != "task.toml" {
			continue
		}
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

// Each of these used to be run on a setup Harbor would not have used. They
// must now be refused, naming why (docs/decisions.md D23).
func TestUnreproducibleSetupsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name, toml, reason string
	}{
		{"explicit separate verifier", "[verifier]\nenvironment_mode = \"separate\"\n", "separate environment"},
		{"implicit separate verifier", "[verifier.environment]\ncpus = 1\n", "separate environment"},
		{"agent user", "[agent]\nuser = \"agent\"\n", "runs as user agent"},
		{"verifier uid", "[verifier]\nuser = 1000\n", "runs as user 1000"},
		{"allowlist", "[environment]\nnetwork_mode = \"allowlist\"\nallowed_hosts = [\"example.com\"]\n", "allowlist"},
		{"phase switch", "[environment]\nnetwork_mode = \"public\"\n[verifier]\nnetwork_mode = \"no-network\"\n", "switches network_mode"},
		{"unknown mode", "[environment]\nnetwork_mode = \"quantum\"\n", "not reproduced"},
		{"step separate verifier", "[[steps]]\nname = \"a\"\n[steps.verifier]\nenvironment_mode = \"separate\"\n", "step \"a\" verifier runs in a separate"},
		{"step inherits separate", "[verifier]\nenvironment_mode = \"separate\"\n[[steps]]\nname = \"a\"\n", "step \"a\" verifier runs in a separate"},
		{"step phase network", "[[steps]]\nname = \"a\"\n[steps.agent]\nnetwork_mode = \"no-network\"\n", "switches network_mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New().Load(fidelityTask(t, tc.toml, nil))
			if err != nil {
				t.Fatalf("Load returned an error; refusals must be reported as Unsupported: %v", err)
			}
			if !strings.Contains(got.Unsupported, tc.reason) {
				t.Errorf("Unsupported = %q, want it to contain %q", got.Unsupported, tc.reason)
			}
		})
	}
}

// The configurations Harbor runs the way Skeptic does must not be refused.
func TestReproducibleSetupsLoad(t *testing.T) {
	for _, tc := range []struct{ name, toml string }{
		{"defaults", ""},
		{"explicit shared verifier", "[verifier]\nenvironment_mode = \"shared\"\n"},
		{"explicit public", "[environment]\nnetwork_mode = \"public\"\n[agent]\nnetwork_mode = \"public\"\n"},
		{"offline throughout", "[environment]\nnetwork_mode = \"no-network\"\n[verifier]\nnetwork_mode = \"no-network\"\n"},
		{"step overrides a separate task back to shared", "[verifier]\nenvironment_mode = \"separate\"\n[[steps]]\nname = \"a\"\n[steps.verifier]\nenvironment_mode = \"shared\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New().Load(fidelityTask(t, tc.toml, nil))
			if err != nil {
				t.Fatal(err)
			}
			if got.Unsupported != "" {
				t.Errorf("Unsupported = %q, want it loaded", got.Unsupported)
			}
		})
	}
}

func TestNoNetworkRunsOffline(t *testing.T) {
	got, err := New().Load(fidelityTask(t, "[environment]\nnetwork_mode = \"no-network\"\n", nil))
	if err != nil || got.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, got.Unsupported)
	}
	if !got.Environment.NoNetwork {
		t.Error("NoNetwork = false for network_mode = no-network")
	}
	// A multi-container stack cannot simply be unplugged: main still needs
	// its sidecars. Harbor uses an egress sidecar for that; Skeptic refuses.
	got, err = New().Load(fidelityTask(t, "[environment]\nnetwork_mode = \"no-network\"\n",
		map[string]string{"environment/docker-compose.yaml": "services:\n  db:\n    image: postgres:16\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Unsupported, "multi-container") {
		t.Errorf("Unsupported = %q, want the compose refusal", got.Unsupported)
	}
}

// docker_image wins over a Dockerfile, as should_use_prebuilt_docker_image
// decides; environment/ is uploaded only when there is nothing to build.
func TestDockerImage(t *testing.T) {
	withDockerfile, err := New().Load(fidelityTask(t, "[environment]\ndocker_image = \"ubuntu:24.04\"\n", nil))
	if err != nil || withDockerfile.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, withDockerfile.Unsupported)
	}
	e := withDockerfile.Environment
	if e.Image != "ubuntu:24.04" || e.Dockerfile != "" || e.UploadDir != "" {
		t.Errorf("with a Dockerfile: Image %q, Dockerfile %q, UploadDir %q; want the image, no build, no upload",
			e.Image, e.Dockerfile, e.UploadDir)
	}

	dir := fidelityTask(t, "[environment]\ndocker_image = \"ubuntu:24.04\"\n",
		map[string]string{"environment/Dockerfile": "", "environment/data.txt": "x\n"})
	if err := os.Remove(filepath.Join(dir, "environment", "Dockerfile")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	imageOnly, err := New().Load(dir)
	if err != nil || imageOnly.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, imageOnly.Unsupported)
	}
	if imageOnly.Environment.UploadDir != filepath.Join(dir, "environment") {
		t.Errorf("UploadDir = %q, want environment/ uploaded", imageOnly.Environment.UploadDir)
	}

	compose, err := New().Load(fidelityTask(t, "[environment]\ndocker_image = \"ubuntu:24.04\"\n",
		map[string]string{"environment/docker-compose.yaml": "services:\n  main:\n    environment:\n      - A=b\n"}))
	if err != nil || compose.Unsupported != "" {
		t.Fatalf("Load: %v, %q", err, compose.Unsupported)
	}
	c := compose.Environment.Compose
	if c == nil || filepath.Base(c.Files[0]) != "docker-compose-prebuilt.yaml" || c.Env["PREBUILT_IMAGE_NAME"] != "ubuntu:24.04" {
		t.Errorf("Compose = %+v, want Harbor's prebuilt base with the image", c)
	}
	if compose.Environment.Image != "" {
		t.Errorf("Image = %q, want it left to compose", compose.Environment.Image)
	}
}

// Missing pieces are reported, not returned as load errors: a load error
// drops the task from a set without a word.
func TestMissingPiecesAreReported(t *testing.T) {
	dir := fidelityTask(t, "", nil)
	if err := os.Remove(filepath.Join(dir, "environment", "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	got, err := New().Load(dir)
	if err != nil || !strings.Contains(got.Unsupported, "no Dockerfile") {
		t.Errorf("no Dockerfile: err %v, Unsupported %q", err, got.Unsupported)
	}
	dir = fidelityTask(t, "", nil)
	if err := os.Remove(filepath.Join(dir, "tests", "test.sh")); err != nil {
		t.Fatal(err)
	}
	got, err = New().Load(dir)
	if err != nil || !strings.Contains(got.Unsupported, "no test script") {
		t.Errorf("no test script: err %v, Unsupported %q", err, got.Unsupported)
	}
}
