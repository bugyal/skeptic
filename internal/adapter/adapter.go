// Package adapter turns a benchmark's on-disk layout into task.Task values.
package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/skeptic-labs/skeptic/internal/task"
)

// Adapter recognises and loads one benchmark task format.
type Adapter interface {
	// Name is the identifier recorded as Task.Format.
	Name() string
	// Detect reports whether dir is a task directory this adapter owns.
	// It must be cheap: Discover calls it across a whole tree.
	Detect(dir string) bool
	// Load builds a Task. It is only called when Detect returned true.
	Load(dir string) (*task.Task, error)
}

// SetAdapter is implemented by formats that describe many tasks in one file
// or directory -- a dataset export -- rather than one task per directory.
type SetAdapter interface {
	Adapter
	// DetectSet reports whether path is a task set this adapter owns. path
	// may be a file.
	DetectSet(path string) bool
	// LoadSet loads every task in the set.
	LoadSet(path string) ([]*task.Task, error)
}

// Registry holds the adapters tried during discovery, in priority order.
type Registry struct{ adapters []Adapter }

// NewRegistry returns a registry over the given adapters.
func NewRegistry(a ...Adapter) *Registry { return &Registry{adapters: a} }

// Register appends an adapter.
func (r *Registry) Register(a Adapter) { r.adapters = append(r.adapters, a) }

// Match returns the first adapter claiming dir, or nil.
func (r *Registry) Match(dir string) Adapter {
	for _, a := range r.adapters {
		if a.Detect(dir) {
			return a
		}
	}
	return nil
}

// maxDepth bounds how far below the root Discover looks for task directories.
// Benchmarks nest task sets a level or two deep (tasks/<name>, or
// <suite>/<name>); walking further turns a mistyped path into a filesystem
// crawl.
const maxDepth = 3

// Discover finds every task at or below root. A root that is itself a task
// yields exactly that task, so the same command works on one task or a set.
// Directories claimed by an adapter are not descended into.
func (r *Registry) Discover(root string) ([]*task.Task, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	// A dataset export is one path describing many tasks, so set adapters get
	// first refusal -- including on directories, which may hold an export.
	for _, a := range r.adapters {
		sa, ok := a.(SetAdapter)
		if !ok || !sa.DetectSet(root) {
			continue
		}
		ts, err := sa.LoadSet(root)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", root, err)
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
		return ts, nil
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory or a recognised task set", root)
	}

	var out []*task.Task
	var errs []error
	r.walk(root, 0, &out, &errs)

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	if len(out) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("no loadable tasks under %s: %w", root, errs[0])
		}
		return nil, fmt.Errorf("no recognised tasks under %s", root)
	}
	return out, nil
}

func (r *Registry) walk(dir string, depth int, out *[]*task.Task, errs *[]error) {
	if a := r.Match(dir); a != nil {
		t, err := a.Load(dir)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", dir, err))
			return
		}
		*out = append(*out, t)
		return
	}
	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		*errs = append(*errs, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || isSkippable(e.Name()) {
			continue
		}
		r.walk(filepath.Join(dir, e.Name()), depth+1, out, errs)
	}
}

// isSkippable filters directories that never contain tasks but are expensive
// or misleading to descend into.
func isSkippable(name string) bool {
	switch name {
	case ".git", ".svn", "node_modules", "__pycache__", ".venv", "venv",
		".skeptic", "dist", "build", ".idea", ".vscode":
		return true
	}
	return len(name) > 1 && name[0] == '.'
}
