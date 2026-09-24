package check

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/bugyal/skeptic/internal/task"
)

// Progress is called as each task finishes, for live output.
type Progress func(done, total int, r TaskResult)

// Run checks every task, at most parallel at a time, and returns results in
// the order the tasks were given.
//
// A cancelled context stops new tasks from starting but lets running ones
// finish their cleanup, so Ctrl-C leaves no orphaned containers and still
// produces a partial report.
func (r *Runner) Run(ctx context.Context, tasks []*task.Task, parallel int, onDone Progress) []TaskResult {
	if parallel <= 0 {
		parallel = min(4, runtime.NumCPU())
	}

	results := make([]TaskResult, len(tasks))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0

	for i, t := range tasks {
		if ctx.Err() != nil {
			// Record what was never attempted rather than leaving a zero value
			// that would read as a passing task.
			results[i] = TaskResult{
				ID: t.ID, Format: t.Format, Dir: t.Dir,
				Verdict: VerdictError, Error: "cancelled before this task ran",
				Reason: "cancelled before this task ran",
			}
			continue
		}

		wg.Add(1)
		go func(i int, t *task.Task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := r.CheckTask(ctx, t)
			_ = writeJSON(filepath.Join(res.LogDir, "result.json"), res)

			mu.Lock()
			results[i] = res
			done++
			if onDone != nil {
				onDone(done, len(tasks), res)
			}
			mu.Unlock()
		}(i, t)
	}
	wg.Wait()
	return results
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
