// Package task defines the format-agnostic model every adapter produces.
//
// Adapters translate a benchmark's on-disk layout into these types; nothing
// downstream of an adapter knows which benchmark a task came from.
package task

import "time"

// Task is one benchmark question: an environment to build, a reference
// solution that is supposed to pass, and tests that decide the score.
type Task struct {
	ID     string `json:"id"`
	Dir    string `json:"dir"`
	Format string `json:"format"`

	// Instruction is the task text shown to the agent. Adapters populate it
	// from wherever their format keeps it -- a file, a manifest field, or a
	// dataset column -- so the leakage checks work across every format
	// rather than only the one that happens to use instruction.md.
	Instruction string `json:"-"`

	Environment Environment `json:"environment"`
	Solution    Solution    `json:"solution"`
	Tests       Tests       `json:"tests"`

	// Unsupported is set by an adapter that recognised the task but cannot
	// run it faithfully. Such a task is reported, never scored: a confident
	// wrong verdict is worse than an admission of ignorance.
	Unsupported string `json:"unsupported,omitempty"`
}

// Environment describes how to obtain the base container image. Either a
// Dockerfile is built, or a prebuilt image is pulled; never both.
type Environment struct {
	Dockerfile   string            `json:"dockerfile,omitempty"`
	ContextDir   string            `json:"context_dir,omitempty"`
	Image        string            `json:"image,omitempty"`
	BuildArgs    map[string]string `json:"build_args,omitempty"`
	BuildTimeout time.Duration     `json:"build_timeout,omitempty"`
	WorkDir      string            `json:"workdir,omitempty"`
	// Platform pins the image architecture, e.g. "linux/amd64". Published
	// benchmark images are often amd64-only, and running one on arm64 without
	// saying so silently falls back to emulation or fails obscurely.
	Platform string `json:"platform,omitempty"`
}

// Prebuilt reports whether the environment is an image reference rather than
// something Skeptic has to build.
func (e Environment) Prebuilt() bool { return e.Image != "" }

// SolutionKind is how the reference fix is applied inside the container.
type SolutionKind string

const (
	// SolutionNone means the task ships no reference solution. This is a
	// supported configuration, not a defect, so the oracle control is
	// reported as not applicable rather than as an error.
	SolutionNone SolutionKind = "none"
	// SolutionScript uploads a directory and executes one script from it.
	SolutionScript SolutionKind = "script"
	// SolutionPatch applies a unified diff in the working directory.
	SolutionPatch SolutionKind = "patch"
)

// Solution is the reference fix: the answer sheet the oracle control hands in.
type Solution struct {
	Kind SolutionKind `json:"kind"`

	// Dir is uploaded to MountPath for SolutionScript.
	Dir       string `json:"dir,omitempty"`
	Script    string `json:"script,omitempty"`
	MountPath string `json:"mount_path,omitempty"`

	// PatchFile is a unified diff on the host, for SolutionPatch.
	PatchFile string `json:"patch_file,omitempty"`
	// PatchContent is an inline unified diff, used by dataset-backed formats
	// that carry the patch in a row rather than as a file on disk.
	PatchContent string `json:"-"`
	// PatchStrip is the -p level passed to patch/git apply.
	PatchStrip int `json:"patch_strip,omitempty"`

	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workdir,omitempty"`
	Timeout time.Duration     `json:"timeout,omitempty"`
}

// Available reports whether an oracle control can run at all.
func (s Solution) Available() bool { return s.Kind != SolutionNone && s.Kind != "" }

// Tests is the hidden grading: what to copy in, what to run, how to read a score.
type Tests struct {
	Dir       string `json:"dir,omitempty"`
	MountPath string `json:"mount_path,omitempty"`
	Command   string `json:"command"`
	// ScriptContent is written to ScriptPath inside the container before
	// Command runs, for formats that carry their test script inline.
	ScriptContent string            `json:"-"`
	ScriptPath    string            `json:"script_path,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	WorkDir       string            `json:"workdir,omitempty"`
	Timeout       time.Duration     `json:"timeout,omitempty"`
	Score         ScoreSpec         `json:"score"`
}

// ScoreKind is how a numeric score is recovered after the test command runs.
type ScoreKind string

const (
	// ScoreRewardFile reads the first readable path in ScoreSpec.Paths.
	// A .json path is parsed as a reward object, a .txt path as a bare float.
	ScoreRewardFile ScoreKind = "reward_file"
	// ScoreExitCode maps exit 0 to 1.0 and anything else to 0.0.
	ScoreExitCode ScoreKind = "exit_code"
	// ScoreFunc defers to ScoreSpec.Scorer, for formats whose score comes
	// from parsing test output rather than reading an artifact.
	ScoreFunc ScoreKind = "func"
)

// Scorer derives a score from captured test output. It returns the score, a
// short human-readable detail line for the report, and an error if the output
// could not be interpreted at all.
type Scorer func(stdout, stderr string, exitCode int) (float64, string, error)

// ScoreSpec says where the score comes from.
type ScoreSpec struct {
	Kind ScoreKind `json:"kind"`
	// Paths are container paths tried in order. Harbor's verifier prefers
	// reward.json over reward.txt, and Skeptic matches that precedence.
	Paths []string `json:"paths,omitempty"`
	// RewardKey names which entry of a multi-key reward object to use.
	// Empty means: a single-key object or a "reward" key is accepted, and
	// anything more ambiguous is an error rather than a guess.
	RewardKey string `json:"reward_key,omitempty"`

	// Scorer is used when Kind is ScoreFunc. It is runtime-only: the report
	// records the resulting score, not the function that produced it.
	Scorer Scorer `json:"-"`
}
