package custom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bugyal/skeptic/internal/task"
)

const fixtures = "../../../testdata/custom"

func TestDetect(t *testing.T) {
	a := New()
	if !a.Detect(filepath.Join(fixtures, "clean")) {
		t.Error("clean fixture should be detected")
	}
	if a.Detect(fixtures) {
		t.Error("task-set root should not be detected as a task")
	}
	// A Harbor task is not ours.
	if a.Detect("../../../testdata/tasks/clean") {
		t.Error("harbor task detected as custom")
	}
}

func TestLoadClean(t *testing.T) {
	got, err := New().Load(filepath.Join(fixtures, "clean"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Unsupported != "" {
		t.Fatalf("Unsupported = %q, want empty", got.Unsupported)
	}
	if got.ID != "custom-fixtures/clean" || got.Format != "custom" {
		t.Errorf("ID, Format = %q, %q", got.ID, got.Format)
	}
	if !strings.Contains(got.Instruction, "ultimate question") {
		t.Errorf("Instruction not loaded: %q", got.Instruction)
	}
	e := got.Environment
	if filepath.Base(e.Dockerfile) != "Dockerfile" || filepath.Base(e.ContextDir) != "environment" || e.WorkDir != "/app" {
		t.Errorf("Environment = %+v", e)
	}
	s := got.Solution
	if s.Kind != task.SolutionScript || s.Script != "solve.sh" || filepath.Base(s.Dir) != "solution" ||
		s.MountPath != "/solution" || s.WorkDir != "/app" {
		t.Errorf("Solution = %+v", s)
	}
	ts := got.Tests
	if ts.Command != "sh /grader/run.sh" || ts.MountPath != "/grader" || filepath.Base(ts.Dir) != "tests" || ts.WorkDir != "/app" {
		t.Errorf("Tests = %+v", ts)
	}
	// Order is the author's and must survive: it decides which file wins.
	want := []string{"/logs/verifier/reward.json", "/logs/verifier/reward.txt"}
	if ts.Score.Kind != task.ScoreRewardFile || strings.Join(ts.Score.Paths, ",") != strings.Join(want, ",") {
		t.Errorf("Score = %+v, want reward_file %v", ts.Score, want)
	}
}

func TestLoadPatch(t *testing.T) {
	got, err := New().Load(filepath.Join(fixtures, "patch"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Unsupported != "" {
		t.Fatalf("Unsupported = %q, want empty", got.Unsupported)
	}
	// No [task] id: the directory name.
	if got.ID != "patch" {
		t.Errorf("ID = %q, want the directory name", got.ID)
	}
	if got.Solution.Kind != task.SolutionPatch || filepath.Base(got.Solution.PatchFile) != "fix.diff" {
		t.Errorf("Solution = %+v", got.Solution)
	}
	if got.Tests.Score.Kind != task.ScoreExitCode {
		t.Errorf("Score.Kind = %q, want exit_code", got.Tests.Score.Kind)
	}
}

// valid is a minimal manifest that loads. Each case below breaks one thing.
const valid = `
format_version = 1
[environment]
image   = "alpine:3"
workdir = "/app"
[solution]
kind   = "script"
script = "solve.sh"
[tests]
command = "true"
[tests.score]
kind = "exit_code"
`

func TestValidBaseline(t *testing.T) {
	got := load(t, valid)
	if got.Unsupported != "" {
		t.Fatalf("baseline manifest should load, got Unsupported = %q", got.Unsupported)
	}
	if got.Environment.Image != "alpine:3" || !got.Environment.Prebuilt() {
		t.Errorf("Environment = %+v", got.Environment)
	}
}

// Every one of these must refuse the task and say why. A manifest the adapter
// does not fully understand is never run on a guess.
func TestInvalidManifestsAreUnsupported(t *testing.T) {
	cases := []struct {
		name, manifest, reason string
	}{
		{"unparsable", "format_version = ", "unparsable"},
		{"no version", strings.Replace(valid, "format_version = 1", "", 1), "format_version is required"},
		{"future version", strings.Replace(valid, "format_version = 1", "format_version = 2", 1), "format_version 2 is not supported"},
		// A typo is the commonest way to lose a setting; it must not vanish.
		{"unknown key", strings.Replace(valid, `workdir = "/app"`, "workdir = \"/app\"\nworkdri = \"/x\"", 1), "environment.workdri"},
		{"unknown table", valid + "\n[verifier]\ntimeout = 3\n", "verifier"},
		{"no workdir", strings.Replace(valid, `workdir = "/app"`, "", 1), "environment.workdir is required"},
		{"relative workdir", strings.Replace(valid, `workdir = "/app"`, `workdir = "app"`, 1), "absolute container path"},
		{"image and dockerfile", strings.Replace(valid, `image   = "alpine:3"`, "image = \"alpine:3\"\ndockerfile = \"Dockerfile\"", 1), "not both"},
		{"no image source", strings.Replace(valid, `image   = "alpine:3"`, "", 1), "one of dockerfile or image"},
		{"dockerfile without context", strings.Replace(valid, `image   = "alpine:3"`, `dockerfile = "Dockerfile"`, 1), "context is required"},
		{"dockerfile missing", strings.Replace(valid, `image   = "alpine:3"`, "dockerfile = \"nope\"\ncontext = \".\"", 1), "not found"},
		{"escaping path", strings.Replace(valid, `script = "solve.sh"`, `script = "../solve.sh"`, 1), "inside the task directory"},
		{"absolute host path", strings.Replace(valid, `script = "solve.sh"`, `script = "/etc/passwd"`, 1), "inside the task directory"},
		{"no solution kind", strings.Replace(valid, `kind   = "script"`, "", 1), "solution.kind is required"},
		{"unknown solution kind", strings.Replace(valid, `kind   = "script"`, `kind = "docker"`, 1), `"docker" is not one of`},
		{"script missing", strings.Replace(valid, `script = "solve.sh"`, `script = "gone.sh"`, 1), "not found"},
		{"none with a script", strings.Replace(valid, `kind   = "script"`, `kind = "none"`, 1), "takes no other fields"},
		{"patch with a script", strings.Replace(valid, `kind   = "script"`, `kind = "patch"`, 1), "does not apply"},
		{"no command", strings.Replace(valid, `command = "true"`, "", 1), "tests.command is required"},
		{"dir without mount", strings.Replace(valid, `command = "true"`, "command = \"true\"\ndir = \"t\"", 1), "tests.mount is required"},
		{"mount without dir", strings.Replace(valid, `command = "true"`, "command = \"true\"\nmount = \"/t\"", 1), "without tests.dir"},
		{"no score kind", strings.Replace(valid, `kind = "exit_code"`, "", 1), "tests.score.kind is required"},
		{"reward file without paths", strings.Replace(valid, `kind = "exit_code"`, `kind = "reward_file"`, 1), "paths is required"},
		{"reward path relative", strings.Replace(valid, `kind = "exit_code"`, "kind = \"reward_file\"\npaths = [\"reward.txt\"]", 1), "absolute container path"},
		{"reward path extension", strings.Replace(valid, `kind = "exit_code"`, "kind = \"reward_file\"\npaths = [\"/logs/reward.yaml\"]", 1), ".json or .txt"},
		{"exit code with paths", strings.Replace(valid, `kind = "exit_code"`, "kind = \"exit_code\"\npaths = [\"/r.txt\"]", 1), "do not apply"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := load(t, c.manifest)
			if got.Unsupported == "" {
				t.Fatalf("loaded without complaint; want Unsupported containing %q", c.reason)
			}
			if !strings.Contains(got.Unsupported, c.reason) {
				t.Errorf("Unsupported = %q, want it to contain %q", got.Unsupported, c.reason)
			}
		})
	}
}

// kind = "none" is a legitimate choice (D3) and must load as NO_ORACLE.
func TestExplicitNoSolution(t *testing.T) {
	m := strings.Replace(valid, "kind   = \"script\"\nscript = \"solve.sh\"", `kind = "none"`, 1)
	got := load(t, m)
	if got.Unsupported != "" {
		t.Fatalf("Unsupported = %q", got.Unsupported)
	}
	if got.Solution.Available() {
		t.Error("Solution.Available() = true for kind = none")
	}
}

// load writes manifest into a fresh task directory beside a solve.sh and a
// Dockerfile, so cases fail on the field they name rather than a missing file.
func load(t *testing.T, manifest string) *task.Task {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		manifestFile: manifest,
		"solve.sh":   "#!/bin/sh\ntrue\n",
		"Dockerfile": "FROM alpine:3\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := New().Load(dir)
	if err != nil {
		t.Fatalf("Load returned an error; problems must be reported as Unsupported: %v", err)
	}
	return got
}
