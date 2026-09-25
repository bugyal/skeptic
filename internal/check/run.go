package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bugyal/skeptic/internal/docker"
	"github.com/bugyal/skeptic/internal/patch"
	"github.com/bugyal/skeptic/internal/task"
)

// ControlResult is the outcome of running one control against one task.
type ControlResult struct {
	Control  Control       `json:"control"`
	Score    *float64      `json:"score"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration_ns"`
	TimedOut bool          `json:"timed_out"`
	Error    string        `json:"error,omitempty"`
	LogDir   string        `json:"log_dir,omitempty"`
	// Detail is a short human summary from a custom scorer, e.g. how many
	// expected tests passed.
	Detail string `json:"detail,omitempty"`
	// Hunk names the withheld change, for partial control results.
	Hunk string `json:"hunk,omitempty"`

	// Captured output, kept off the report but available to a scorer.
	stdout, stderr, combined string
}

// TaskResult is everything Skeptic concluded about one task.
type TaskResult struct {
	ID      string         `json:"id"`
	Format  string         `json:"format"`
	Dir     string         `json:"dir"`
	Verdict Verdict        `json:"verdict"`
	Reason  string         `json:"reason"`
	Nop     *ControlResult `json:"nop"`
	Oracle  *ControlResult `json:"oracle"`
	// Partials holds one result per hunk withheld by the partial control.
	Partials []*ControlResult `json:"partials,omitempty"`
	// WeakTests names hunks whose absence the test suite failed to notice and
	// which add or change behaviour, so the gap is more likely to be real.
	WeakTests []string `json:"weak_tests,omitempty"`
	// UngradedCleanup names ungraded hunks that only delete lines. Usually
	// dead-code removal, which no test can observe; kept apart so it does not
	// inflate the weak-test count.
	UngradedCleanup []string      `json:"ungraded_cleanup,omitempty"`
	ImageDigest     string        `json:"image_digest,omitempty"`
	Duration        time.Duration `json:"duration_ns"`
	Error           string        `json:"error,omitempty"`
	Unsupported     string        `json:"unsupported,omitempty"`
	LogDir          string        `json:"log_dir,omitempty"`
}

// NopScore returns the nop control's score, or nil when it did not produce one.
func (r TaskResult) NopScore() *float64 {
	if r.Nop == nil {
		return nil
	}
	return r.Nop.Score
}

// OracleScore returns the oracle control's score, or nil.
func (r TaskResult) OracleScore() *float64 {
	if r.Oracle == nil {
		return nil
	}
	return r.Oracle.Score
}

func fmtScore(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *p)
}

// Options configures a run.
type Options struct {
	RunDir         string
	Timeout        time.Duration
	KeepContainers bool
	NoCache        bool
	Only           Control // empty runs both
	Platform       string
	// Partial enables the weak-test probe. Off by default: it costs an extra
	// container run per hunk sampled.
	Partial bool
	// PartialMaxHunks caps how many hunks are withheld, one at a time.
	PartialMaxHunks int
	Log             *slog.Logger
}

// Runner executes controls against tasks.
type Runner struct {
	docker *docker.Client
	opts   Options
	log    *slog.Logger
}

// NewRunner returns a Runner.
func NewRunner(d *docker.Client, o Options) *Runner {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{docker: d, opts: o, log: o.Log}
}

// CheckTask builds the environment, runs the requested controls in fresh
// containers, and classifies the result. It returns a result rather than an
// error: a failure to check is itself a reportable verdict.
func (r *Runner) CheckTask(ctx context.Context, t *task.Task) TaskResult {
	start := time.Now()
	res := TaskResult{ID: t.ID, Format: t.Format, Dir: t.Dir, Unsupported: t.Unsupported}

	taskLogDir := filepath.Join(r.opts.RunDir, sanitize(t.ID))
	res.LogDir = taskLogDir

	if t.Unsupported != "" {
		res.Verdict, res.Reason = VerdictUnsupported, t.Unsupported
		res.Duration = time.Since(start)
		return res
	}

	image, digest, err := r.image(ctx, t, taskLogDir)
	if err != nil {
		res.Verdict, res.Error = VerdictError, err.Error()
		res.Reason = err.Error()
		res.Duration = time.Since(start)
		return res
	}
	res.ImageDigest = digest

	if r.opts.Only == "" || r.opts.Only == ControlNop {
		res.Nop = r.runControl(ctx, t, image, ControlNop, taskLogDir, "")
	}
	if (r.opts.Only == "" || r.opts.Only == ControlOracle) && t.Solution.Available() {
		res.Oracle = r.runControl(ctx, t, image, ControlOracle, taskLogDir, "")
	}

	// The partial control only means anything once the full solution is known
	// to score 1.0; otherwise a lower score says nothing about the tests.
	if r.opts.Partial && res.Oracle != nil && res.Oracle.Score != nil && *res.Oracle.Score == 1 {
		r.runPartials(ctx, t, image, taskLogDir, &res)
	}

	res.Verdict, res.Error = classify(t, res.Nop, res.Oracle)
	res.Reason = res.Verdict.Reason(res)
	res.Duration = time.Since(start)
	return res
}

// runPartials withholds hunks from the reference patch, one at a time, and
// records any whose absence the test suite does not notice.
func (r *Runner) runPartials(ctx context.Context, t *task.Task, image, taskLogDir string, res *TaskResult) {
	if t.Solution.Kind != task.SolutionPatch {
		// A shell-script solution has no hunks to withhold; saying nothing is
		// correct, and inventing a mutation would need a model.
		return
	}
	content := t.Solution.PatchContent
	if content == "" && t.Solution.PatchFile != "" {
		b, err := os.ReadFile(t.Solution.PatchFile)
		if err != nil {
			return
		}
		content = string(b)
	}

	p, err := patch.Parse(content)
	if err != nil {
		r.log.Warn("partial control: unparsable solution patch", "task", t.ID, "err", err)
		return
	}
	total := p.HunkCount()
	if total < 2 {
		// A single-hunk patch cannot be reduced: withholding its only hunk is
		// just the nop control, which already ran.
		return
	}
	if p.SemanticHunks() < 2 {
		// Nothing left to learn: at most one hunk changes observable
		// behaviour, so any "weak test" result would be about comments.
		return
	}

	max := r.opts.PartialMaxHunks
	if max <= 0 {
		max = 3
	}
	if max > total {
		max = total
	}

	for i := 0; i < max; i++ {
		if ctx.Err() != nil {
			return
		}
		// A hunk that only edits comments or blank lines cannot be graded by
		// any test, so withholding it says nothing about the suite. Probing it
		// would report a weak test that is not there.
		if fi, hi, ok := p.Locate(i); ok && !p.Files[fi].Hunks[hi].Semantic() {
			r.log.Debug("partial control: skipping non-semantic hunk",
				"task", t.ID, "hunk", p.Describe(i))
			continue
		}
		reduced, _, err := p.Without(i)
		if err != nil || reduced.HunkCount() == 0 {
			continue
		}
		desc := p.Describe(i)
		out := r.runControl(ctx, t, image, ControlPartial,
			filepath.Join(taskLogDir, fmt.Sprintf("partial-%d", i)), reduced.String())
		out.Hunk = desc
		res.Partials = append(res.Partials, out)

		// Full marks without the hunk means the suite never graded it.
		if out.Error == "" && out.Score != nil && *out.Score == 1 {
			// A deletion-only hunk is usually cleanup: code nothing calls any
			// more once another hunk landed. Leaving it in place is
			// unobservable by construction, so the suite passing says nothing
			// about its quality. Recorded, but marked, so it is not counted
			// as evidence of a weak test.
			if fi, hi, ok := p.Locate(i); ok && p.Files[fi].Hunks[hi].DeletionOnly() {
				out.Hunk = desc + " (deletion-only; likely cleanup)"
				res.UngradedCleanup = append(res.UngradedCleanup, desc)
				continue
			}
			res.WeakTests = append(res.WeakTests, desc)
		}
	}
}

// image builds or pulls the task environment. Builds are cached by a content
// hash of the build context, so re-runs of an unchanged task set are cheap.
func (r *Runner) image(ctx context.Context, t *task.Task, logDir string) (ref, digest string, err error) {
	if t.Environment.Prebuilt() {
		// Published benchmark images run to several gigabytes each, so an
		// image already present is used as-is unless a rebuild was demanded.
		if !r.opts.NoCache {
			if id := r.docker.ImageID(ctx, t.Environment.Image); id != "" {
				r.log.Debug("image present locally", "task", t.ID, "image", t.Environment.Image)
				return t.Environment.Image, id, nil
			}
		}
		if _, err := r.docker.Pull(ctx, t.Environment.Image, r.platform(t), t.Environment.BuildTimeout); err != nil {
			return "", "", err
		}
		return t.Environment.Image, r.docker.ImageID(ctx, t.Environment.Image), nil
	}

	h, err := contextHash(t.Environment.Dockerfile, t.Environment.ContextDir)
	if err != nil {
		return "", "", fmt.Errorf("hashing build context: %w", err)
	}
	ref = "skeptic-env:" + h

	if !r.opts.NoCache {
		if id := r.docker.ImageID(ctx, ref); id != "" {
			r.log.Debug("image cache hit", "task", t.ID, "image", ref)
			return ref, id, nil
		}
	}

	r.log.Info("building image", "task", t.ID, "image", ref)
	id, out, err := r.docker.Build(ctx, docker.BuildOptions{
		Dockerfile: t.Environment.Dockerfile,
		ContextDir: t.Environment.ContextDir,
		Tag:        ref,
		BuildArgs:  t.Environment.BuildArgs,
		Timeout:    t.Environment.BuildTimeout,
		NoCache:    r.opts.NoCache,
		Platform:   r.opts.Platform,
	})
	// The build log is evidence whether or not the build succeeded.
	writeFile(filepath.Join(logDir, "build.log"), out.Stdout+out.Stderr)
	if err != nil {
		return "", "", fmt.Errorf("build failed: %w", err)
	}
	return ref, id, nil
}

// runControl runs one control in a container of its own. Each control gets a
// fresh container so neither can observe the other's side effects.
func (r *Runner) runControl(ctx context.Context, t *task.Task, image string, c Control, taskLogDir, reducedPatch string) *ControlResult {
	start := time.Now()
	out := &ControlResult{Control: c, LogDir: filepath.Join(taskLogDir, string(c))}
	if c == ControlPartial {
		// runPartials already gave each probe its own directory.
		out.LogDir = taskLogDir
	}

	name := fmt.Sprintf("skeptic-%s-%s-%d", sanitize(t.ID), c, time.Now().UnixNano())
	if len(name) > 100 {
		name = name[:100]
	}

	container, err := r.docker.Start(ctx, docker.StartOptions{
		Image:    image,
		Name:     name,
		WorkDir:  t.Environment.WorkDir,
		Platform: r.platform(t),
	})
	if err != nil {
		out.Error = fmt.Sprintf("starting container: %v", err)
		out.Duration = time.Since(start)
		return out
	}
	if !r.opts.KeepContainers {
		defer func() {
			if err := r.docker.Remove(ctx, container); err != nil {
				r.log.Warn("removing container", "task", t.ID, "err", err)
			}
		}()
	} else {
		r.log.Info("keeping container", "task", t.ID, "control", c, "container", name)
	}

	// Harbor mounts these directories into every trial; test scripts in the
	// wild write to them unconditionally.
	if _, err := r.docker.Exec(ctx, container,
		"mkdir -p /logs/verifier /logs/agent /logs/artifacts", docker.ExecOptions{Timeout: time.Minute}); err != nil {
		out.Error = fmt.Sprintf("preparing log directories: %v", err)
		out.Duration = time.Since(start)
		return out
	}

	if c == ControlOracle {
		if err := r.applySolution(ctx, container, t, t.Solution.PatchContent, out); err != nil {
			out.Error = err.Error()
			out.Duration = time.Since(start)
			return out
		}
	}
	if c == ControlPartial {
		if err := r.applySolution(ctx, container, t, reducedPatch, out); err != nil {
			out.Error = err.Error()
			out.Duration = time.Since(start)
			return out
		}
	}

	// Formats that carry their test script inline write it in now, after any
	// solution has run, so the solution cannot see or edit it.
	if t.Tests.ScriptContent != "" {
		if err := r.docker.WriteFile(ctx, container, t.Tests.ScriptPath, t.Tests.ScriptContent); err != nil {
			out.Error = fmt.Sprintf("writing test script: %v", err)
			out.Duration = time.Since(start)
			return out
		}
	}

	// Tests are copied in after the control has acted, exactly as a real
	// harness does, so the solution cannot see or edit them.
	if t.Tests.Dir != "" {
		if err := r.docker.CopyIn(ctx, container, t.Tests.Dir+"/.", t.Tests.MountPath); err != nil {
			out.Error = fmt.Sprintf("copying tests: %v", err)
			out.Duration = time.Since(start)
			return out
		}
	}

	testRes, err := r.docker.Exec(ctx, container, t.Tests.Command, docker.ExecOptions{
		WorkDir: t.Tests.WorkDir,
		Env:     t.Tests.Env,
		Timeout: r.testTimeout(t),
		// Scorers that locate results between markers need the true write
		// order, which only holds when the container merges the streams.
		CombineStreams: true,
	})
	out.ExitCode = testRes.ExitCode
	out.TimedOut = testRes.TimedOut
	out.stdout, out.stderr = testRes.Stdout, testRes.Stderr
	out.combined = testRes.Combined
	// Always written, even when empty: "the test printed nothing" is itself
	// evidence when someone disputes a flag.
	writeFileAlways(filepath.Join(out.LogDir, "test.stdout"), testRes.Stdout)
	writeFileAlways(filepath.Join(out.LogDir, "test.stderr"), testRes.Stderr)
	writeFileAlways(filepath.Join(out.LogDir, "test.combined"), testRes.Combined)
	writeFile(filepath.Join(out.LogDir, "exit-code.txt"), fmt.Sprintf("%d\n", testRes.ExitCode))

	if testRes.TimedOut {
		out.Error = fmt.Sprintf("test command timed out after %s", r.testTimeout(t))
		out.Duration = time.Since(start)
		return out
	}
	if err != nil {
		out.Error = fmt.Sprintf("running tests: %v", err)
		out.Duration = time.Since(start)
		return out
	}

	score, err := r.readScore(ctx, container, t, out)
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Score = &score
	}
	out.Duration = time.Since(start)
	return out
}

func (r *Runner) applySolution(ctx context.Context, container string, t *task.Task, patchOverride string, out *ControlResult) error {
	switch t.Solution.Kind {
	case task.SolutionScript:
		if err := r.docker.CopyIn(ctx, container, t.Solution.Dir+"/.", t.Solution.MountPath); err != nil {
			return fmt.Errorf("copying solution: %w", err)
		}
		script := t.Solution.MountPath + "/" + t.Solution.Script
		cmd := fmt.Sprintf("chmod +x %s && %s", script, script)
		res, err := r.docker.Exec(ctx, container, cmd, docker.ExecOptions{
			WorkDir: t.Solution.WorkDir,
			Env:     mergeEnv(t.Solution.Env, map[string]string{"DEBIAN_FRONTEND": "noninteractive"}),
			Timeout: r.solutionTimeout(t),
		})
		writeFile(filepath.Join(out.LogDir, "solution.stdout"), res.Stdout)
		writeFile(filepath.Join(out.LogDir, "solution.stderr"), res.Stderr)
		if res.TimedOut {
			return fmt.Errorf("solution script timed out after %s", r.solutionTimeout(t))
		}
		if err != nil {
			return fmt.Errorf("running solution: %w", err)
		}
		// A non-zero exit is recorded but not fatal: the tests, not the
		// script's exit status, decide whether the reference fix worked.
		if res.ExitCode != 0 {
			r.log.Warn("solution exited non-zero", "task", t.ID, "exit", res.ExitCode)
		}
		return nil

	case task.SolutionPatch:
		content := patchOverride
		if content == "" {
			content = t.Solution.PatchContent
		}
		if content == "" && t.Solution.PatchFile != "" {
			b, err := os.ReadFile(t.Solution.PatchFile)
			if err != nil {
				return fmt.Errorf("reading solution patch: %w", err)
			}
			content = string(b)
		}
		if content == "" {
			return errors.New("solution patch is empty")
		}
		return r.applyPatch(ctx, container, t, content, out)

	default:
		return fmt.Errorf("unknown solution kind %q", t.Solution.Kind)
	}
}

// gitApplyCmds mirrors swebench/harness/run_evaluation.py, which tries these
// in order. Real gold patches do not always apply cleanly with plain git apply.
var gitApplyCmds = []string{
	"git apply --verbose",
	"git apply --verbose --3way",
	"patch --batch --fuzz=5 -p1 -i",
}

// applyPatch writes the diff into the container and applies it, trying each
// strategy in turn. Failing to apply is an error, never a score of zero: an
// unapplied patch says nothing about whether the tests are any good.
func (r *Runner) applyPatch(ctx context.Context, container string, t *task.Task, content string, out *ControlResult) error {
	const patchPath = "/tmp/skeptic-solution.diff"
	if err := r.docker.WriteFile(ctx, container, patchPath, content); err != nil {
		return fmt.Errorf("writing patch: %w", err)
	}
	writeFile(filepath.Join(out.LogDir, "applied.diff"), content)

	var attempts []string
	for _, cmd := range gitApplyCmds {
		res, err := r.docker.Exec(ctx, container, cmd+" "+patchPath, docker.ExecOptions{
			WorkDir: t.Solution.WorkDir,
			Timeout: 5 * time.Minute,
		})
		if err == nil && res.ExitCode == 0 {
			writeFile(filepath.Join(out.LogDir, "patch-apply.log"),
				fmt.Sprintf("applied with: %s\n%s%s", cmd, res.Stdout, res.Stderr))
			return nil
		}
		attempts = append(attempts, fmt.Sprintf("%s -> exit %d: %s",
			cmd, res.ExitCode, strings.TrimSpace(firstLine(res.Stderr))))
	}
	writeFile(filepath.Join(out.LogDir, "patch-apply.log"), strings.Join(attempts, "\n"))
	return fmt.Errorf("could not apply patch (%s)", strings.Join(attempts, "; "))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// platform resolves the image architecture, preferring an explicit flag over
// the adapter's derivation.
func (r *Runner) platform(t *task.Task) string {
	if r.opts.Platform != "" {
		return r.opts.Platform
	}
	return t.Environment.Platform
}

// readScore recovers the numeric score. Candidate paths are tried in order and
// the first that exists wins; a path that exists but will not parse is an
// error rather than a reason to fall through to the next one, because silently
// preferring a stale reward.txt over a malformed reward.json would invent a
// verdict.
func (r *Runner) readScore(ctx context.Context, container string, t *task.Task, out *ControlResult) (float64, error) {
	switch t.Tests.Score.Kind {
	case task.ScoreFunc:
		if t.Tests.Score.Scorer == nil {
			return 0, fmt.Errorf("score kind is func but no scorer was set")
		}
		// The scorer sees the interleaved stream: a harness that brackets its
		// test run with xtrace markers writes those to stderr and the results
		// to stdout, and only the combined view keeps them in order.
		score, detail, err := t.Tests.Score.Scorer(out.combined, out.stderr, out.ExitCode)
		out.Detail = detail
		return score, err

	case task.ScoreExitCode:
		if out.ExitCode == 0 {
			return 1, nil
		}
		return 0, nil

	case task.ScoreRewardFile:
		var tried []string
		for _, p := range t.Tests.Score.Paths {
			b, err := r.docker.ReadFile(ctx, container, p)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					tried = append(tried, p)
					continue
				}
				return 0, fmt.Errorf("reading %s: %w", p, err)
			}
			writeFile(filepath.Join(out.LogDir, "reward"+filepath.Ext(p)), string(b))

			if strings.HasSuffix(p, ".json") {
				return task.ParseRewardJSON(b, t.Tests.Score.RewardKey)
			}
			return task.ParseRewardText(b)
		}
		return 0, fmt.Errorf("no reward file found (looked in %s)", strings.Join(tried, ", "))

	default:
		return 0, fmt.Errorf("unknown score kind %q", t.Tests.Score.Kind)
	}
}

func (r *Runner) testTimeout(t *task.Task) time.Duration {
	if t.Tests.Timeout > 0 && (r.opts.Timeout == 0 || t.Tests.Timeout < r.opts.Timeout) {
		return t.Tests.Timeout
	}
	return r.opts.Timeout
}

func (r *Runner) solutionTimeout(t *task.Task) time.Duration {
	if t.Solution.Timeout > 0 && (r.opts.Timeout == 0 || t.Solution.Timeout < r.opts.Timeout) {
		return t.Solution.Timeout
	}
	return r.opts.Timeout
}

func mergeEnv(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range extra {
		out[k] = v
	}
	// The task's own solution env wins, matching Harbor's ordering.
	for k, v := range base {
		out[k] = v
	}
	return out
}

// contextHash fingerprints the Dockerfile and every file in the build context,
// so an unchanged task reuses its image and a changed one does not.
func contextHash(dockerfile, contextDir string) (string, error) {
	h := sha256.New()

	df, err := os.ReadFile(dockerfile)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(h, "dockerfile\x00%d\x00", len(df))
	h.Write(df)

	var paths []string
	err = filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	for _, p := range paths {
		rel, _ := filepath.Rel(contextDir, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "file\x00%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// sanitize turns a task ID into a filesystem-safe directory name. Task IDs
// carry slashes (org/name), which would otherwise nest the evidence tree.
func sanitize(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeFile(path, content string) {
	if content == "" {
		return
	}
	writeFileAlways(path, content)
}

func writeFileAlways(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(content), 0o644)
}

// writeJSON is used by the report package to persist structured evidence.
func writeJSON(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
