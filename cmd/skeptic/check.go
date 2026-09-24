package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/skeptic-labs/skeptic/internal/check"
	"github.com/skeptic-labs/skeptic/internal/docker"
	"github.com/skeptic-labs/skeptic/internal/report"
	"github.com/skeptic-labs/skeptic/internal/task"
	"github.com/skeptic-labs/skeptic/internal/version"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var (
		only           string
		jsonPath       string
		runDir         string
		platform       string
		ids            []string
		limit          int
		parallel       int
		timeout        time.Duration
		failOnFlagged  bool
		noFailOnFlag   bool
		keepContainers bool
		noCache        bool
		partial        bool
		partialMax     int
	)

	cmd := &cobra.Command{
		Use:   "check <path>",
		Short: "Run the oracle and nop controls over a benchmark",
		Long: "Builds each task's environment, runs the requested controls in fresh\n" +
			"containers, and classifies every task. Full stdout, stderr and the raw\n" +
			"score artifact are captured per task and per control.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			log := logger()

			if only != "" && only != string(check.ControlNop) && only != string(check.ControlOracle) {
				return fmt.Errorf("--only must be nop or oracle, got %q", only)
			}

			dc := docker.New(log)
			if err := dc.Available(ctx); err != nil {
				return fmt.Errorf("docker is required for check: %w (use `skeptic lint` for checks that need no containers)", err)
			}

			tasks, err := registry().Discover(args[0])
			if err != nil {
				return err
			}
			tasks = selectTasks(tasks, ids, limit)
			if len(tasks) == 0 {
				return fmt.Errorf("no tasks matched")
			}

			if runDir == "" {
				runDir = filepath.Join(".skeptic", "runs", time.Now().UTC().Format("20060102-150405"))
			}
			if err := os.MkdirAll(runDir, 0o755); err != nil {
				return err
			}

			runner := check.NewRunner(dc, check.Options{
				RunDir:          runDir,
				Timeout:         timeout,
				KeepContainers:  keepContainers,
				NoCache:         noCache,
				Only:            check.Control(only),
				Platform:        platform,
				Partial:         partial,
				PartialMaxHunks: partialMax,
				Log:             log,
			})

			fmt.Fprintf(os.Stderr, "checking %d task(s), %d at a time\n", len(tasks), parallel)
			results := runner.Run(ctx, tasks, parallel, func(done, total int, r check.TaskResult) {
				fmt.Fprintf(os.Stderr, "  [%d/%d] %-12s %s\n", done, total, r.Verdict, r.ID)
			})

			rep := report.Build(results, version.Version, runDir, dc.APIVersion(ctx))

			// The report is written before any non-zero exit, so an
			// interrupted or failing run still leaves evidence behind.
			if err := rep.WriteJSON(filepath.Join(runDir, "report.json")); err != nil {
				return err
			}
			if jsonPath != "" {
				if err := rep.WriteJSON(jsonPath); err != nil {
					return err
				}
			}

			fmt.Fprintln(os.Stdout)
			rep.Table(os.Stdout)
			fmt.Fprintf(os.Stderr, "\nevidence: %s\n", runDir)

			if ctx.Err() != nil {
				return ctx.Err()
			}
			if failOnFlagged && !noFailOnFlag && rep.AnyFlagged() {
				os.Exit(1)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&ids, "task", nil, "only check these task IDs (repeatable)")
	f.IntVar(&limit, "limit", 0, "check only the first N tasks")
	f.IntVar(&parallel, "parallel", 0, "tasks to check concurrently (default min(4, NumCPU))")
	f.DurationVar(&timeout, "timeout", 30*time.Minute, "per test run")
	f.StringVar(&only, "only", "", "run one control only: nop or oracle")
	f.StringVar(&jsonPath, "json", "", "also write the full report here")
	f.StringVar(&runDir, "run-dir", "", "where to write evidence (default .skeptic/runs/<timestamp>)")
	f.StringVar(&platform, "platform", "", "docker platform, e.g. linux/amd64")
	f.BoolVar(&failOnFlagged, "fail-on-flagged", true, "exit 1 if any task is flagged")
	f.BoolVar(&noFailOnFlag, "no-fail-on-flagged", false, "never exit non-zero for flagged tasks")
	f.BoolVar(&keepContainers, "keep-containers", false, "leave containers running for debugging")
	f.BoolVar(&noCache, "no-cache", false, "force image rebuilds")
	f.BoolVar(&partial, "partial", false,
		"also probe for weak tests: withhold one hunk of the reference patch and expect the score to drop")
	f.IntVar(&partialMax, "partial-max-hunks", 3, "how many hunks to withhold, one at a time")
	return cmd
}

// selectTasks applies --task and --limit, in that order.
func selectTasks(tasks []*task.Task, ids []string, limit int) []*task.Task {
	if len(ids) > 0 {
		want := make(map[string]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		var out []*task.Task
		for _, t := range tasks {
			// Match the full ID or its trailing path element, so a user can
			// say "hello-world" instead of "harbor/hello-world".
			if want[t.ID] || want[filepath.Base(t.ID)] {
				out = append(out, t)
			}
		}
		tasks = out
	}
	if limit > 0 && limit < len(tasks) {
		tasks = tasks[:limit]
	}
	return tasks
}
