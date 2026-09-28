package check

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bugyal/skeptic/internal/docker"
	"github.com/bugyal/skeptic/internal/task"
)

// controlSteps runs one control over a multi-step task: every step in order,
// in the one environment, as Harbor's trial/multi_step.py does. Before each
// step after the first, the previous step's graders and verifier output are
// removed; then the step's workdir files are uploaded and its setup.sh and
// healthcheck run; then the control acts (the oracle applies the step's
// solution, the nop nothing) and the step's tests grade it. A step whose
// rewards miss its min_reward stops the steps after it. The control's score
// combines the steps that ran, by the task's strategy.
//
// Where Harbor would carry on past a broken step and average what is left,
// this stops with ERROR: a step that could not be set up, graded or read is
// a step whose score is unknown, and a combined score that quietly omits it
// would be a verdict Skeptic did not earn (docs/decisions.md D24).
func (r *Runner) controlSteps(ctx context.Context, t *task.Task, container string, c Control, out *ControlResult, start time.Time) *ControlResult {
	fail := func(step, format string, args ...interface{}) *ControlResult {
		out.Error = fmt.Sprintf("step %q: ", step) + fmt.Sprintf(format, args...)
		out.Duration = time.Since(start)
		return out
	}

	var (
		ran      []map[string]float64
		lines    []string
		combined strings.Builder
		stopped  string
	)
	for i, st := range t.Steps {
		stepOut := &ControlResult{Control: c, LogDir: filepath.Join(out.LogDir, "step-"+sanitize(st.Name))}
		if err := os.MkdirAll(stepOut.LogDir, 0o755); err != nil {
			return fail(st.Name, "%v", err)
		}

		if i > 0 {
			// The previous step's shared verifier left its tests and
			// rewards in the container; the next step must not see them,
			// and must not be scored from them
			// (_reset_shared_step_verifier_dirs).
			if _, err := r.docker.Exec(ctx, container,
				"rm -rf /tests /logs/verifier && mkdir -p /tests /logs/verifier",
				docker.ExecOptions{Timeout: time.Minute}); err != nil {
				return fail(st.Name, "clearing the previous step's tests: %v", err)
			}
		}

		if msg := r.prepareStep(ctx, t, st, container, stepOut); msg != "" {
			return fail(st.Name, "%s", msg)
		}

		if c == ControlOracle && !st.Solution.Available() {
			// The adapter makes such a task NO_ORACLE; reaching here is a
			// loader bug, never a score.
			return fail(st.Name, "the oracle has no solution for this step")
		}
		view := *t
		view.Steps = nil
		view.Solution = st.Solution
		view.Tests = st.Tests
		ok := r.act(ctx, &view, container, c, "", stepOut)
		combined.WriteString(stepOut.combined)
		if !ok {
			return fail(st.Name, "%s", stepOut.Error)
		}
		rewards, err := r.readRewards(ctx, container, &view, stepOut)
		if err != nil {
			return fail(st.Name, "%v", err)
		}
		ran = append(ran, rewards)
		lines = append(lines, fmt.Sprintf("%s %s", st.Name, formatRewards(rewards)))

		if key, below := belowMinReward(rewards, st.MinReward); below {
			stopped = fmt.Sprintf("; stopped after %s (%s below min_reward)", st.Name, key)
			break
		}
	}
	out.combined = combined.String()

	strategy := t.StepReward
	if strategy == "" {
		strategy = "mean"
	}
	agg := combineSteps(ran, strategy)
	score, err := task.ReduceRewards(agg, t.Tests.Score.RewardKey)
	out.Detail = fmt.Sprintf("steps (%s): %s%s", strategy, strings.Join(lines, ", "), stopped)
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Score = &score
	}
	writeFile(filepath.Join(out.LogDir, "steps.txt"), out.Detail+"\n")
	out.Duration = time.Since(start)
	return out
}

// prepareStep uploads the step's workdir files, runs its setup.sh and its
// healthcheck (_prepare_step). It returns why the step could not be
// prepared, or "".
func (r *Runner) prepareStep(ctx context.Context, t *task.Task, st task.Step, container string, out *ControlResult) string {
	if st.WorkdirDir != "" {
		target, err := r.workdir(ctx, container, t)
		if err != nil {
			return fmt.Sprintf("finding the working directory: %v", err)
		}
		if err := r.docker.CopyIn(ctx, container, st.WorkdirDir+"/.", target); err != nil {
			return fmt.Sprintf("uploading workdir files: %v", err)
		}
		if st.Setup {
			script := strings.TrimRight(target, "/") + "/setup.sh"
			res, err := r.docker.Exec(ctx, container, "bash "+shellQuote(script), docker.ExecOptions{
				WorkDir: t.Environment.WorkDir,
				Env:     t.Environment.Env,
				Timeout: 10 * time.Minute,
			})
			// Always written: a setup.sh that printed nothing still ran,
			// and the evidence should say so.
			writeFileAlways(filepath.Join(out.LogDir, "setup.stdout"), res.Stdout)
			writeFileAlways(filepath.Join(out.LogDir, "setup.stderr"), res.Stderr)
			writeFile(filepath.Join(out.LogDir, "setup.exit-code.txt"), fmt.Sprintf("%d\n", res.ExitCode))
			if err != nil {
				return fmt.Sprintf("setup.sh: %v", err)
			}
			if res.ExitCode != 0 {
				return fmt.Sprintf("setup.sh exited %d", res.ExitCode)
			}
		}
	}
	if st.Healthcheck != nil {
		if err := r.healthcheck(ctx, container, t, st.Healthcheck); err != nil {
			return err.Error()
		}
	}
	return ""
}

// workdir is where a step's files go: the task's working directory, or the
// container's own when the task names none (_upload_step_workdir runs pwd).
func (r *Runner) workdir(ctx context.Context, container string, t *task.Task) (string, error) {
	if t.Environment.WorkDir != "" {
		return t.Environment.WorkDir, nil
	}
	res, err := r.docker.Exec(ctx, container, "pwd", docker.ExecOptions{Timeout: time.Minute})
	if err != nil {
		return "", err
	}
	if wd := strings.TrimSpace(res.Stdout); wd != "" {
		return wd, nil
	}
	return "/", nil
}

// healthcheck mirrors environments/base.py run_healthcheck: during the start
// period failures do not count and are retried at StartInterval; after it,
// Retries consecutive failures, spaced by Interval, are fatal.
func (r *Runner) healthcheck(ctx context.Context, container string, t *task.Task, hc *task.Healthcheck) error {
	startPeriodEnd := time.Now().Add(hc.StartPeriod)
	failures := 0
	for {
		inStartPeriod := time.Now().Before(startPeriodEnd)
		res, err := r.docker.Exec(ctx, container, hc.Command, docker.ExecOptions{
			WorkDir: t.Environment.WorkDir,
			Env:     t.Environment.Env,
			Timeout: hc.Timeout,
		})
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		wait := hc.StartInterval
		if !inStartPeriod {
			failures++
			if failures >= hc.Retries {
				return fmt.Errorf("healthcheck failed %d times in a row: %s", hc.Retries, hc.Command)
			}
			wait = hc.Interval
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// readRewards reads a step's reward file in per-key form. The first path
// that exists wins, as for a single control's score.
func (r *Runner) readRewards(ctx context.Context, container string, t *task.Task, out *ControlResult) (map[string]float64, error) {
	if t.Tests.Score.Kind != task.ScoreRewardFile {
		return nil, fmt.Errorf("a multi-step task must be scored from reward files, not %q", t.Tests.Score.Kind)
	}
	var tried []string
	for _, p := range t.Tests.Score.Paths {
		b, err := r.docker.ReadFile(ctx, container, p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				tried = append(tried, p)
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		writeFile(filepath.Join(out.LogDir, "reward"+filepath.Ext(p)), string(b))
		return task.ParseRewards(b, strings.HasSuffix(p, ".json"))
	}
	return nil, fmt.Errorf("no reward file found (looked in %s)", strings.Join(tried, ", "))
}

// combineSteps is trial.py _select_multi_step_reward: "final" takes the last
// step's rewards as they are; "mean" averages each key over the steps, a
// step without the key counting 0.
func combineSteps(steps []map[string]float64, strategy string) map[string]float64 {
	if len(steps) == 0 {
		return nil
	}
	if strategy == "final" {
		return steps[len(steps)-1]
	}
	keys := map[string]bool{}
	for _, s := range steps {
		for k := range s {
			keys[k] = true
		}
	}
	out := make(map[string]float64, len(keys))
	for k := range keys {
		var sum float64
		for _, s := range steps {
			sum += s[k]
		}
		out[k] = sum / float64(len(steps))
	}
	return out
}

// belowMinReward is multi_step.py _min_reward_failure: any gated key below
// its threshold, or missing, stops the steps after this one.
func belowMinReward(rewards, min map[string]float64) (string, bool) {
	keys := make([]string, 0, len(min))
	for k := range min {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := rewards[k]
		if !ok {
			v = math.Inf(-1)
		}
		if v < min[k] {
			return k, true
		}
	}
	return "", false
}

func formatRewards(m map[string]float64) string {
	if v, ok := m["reward"]; ok && len(m) == 1 {
		return fmt.Sprintf("%.2f", v)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%.2f", k, m[k])
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
