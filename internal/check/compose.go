package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bugyal/skeptic/internal/docker"
	"github.com/bugyal/skeptic/internal/task"
)

// projectName turns a container name into a valid Compose project name:
// lowercase letters, digits, '-' and '_', starting with a letter or digit.
func projectName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// project assembles the Compose project for one control. logDir receives the
// per-control directory the task's files may mount for logs, and the
// resource-limit override, so both are kept as evidence.
func (r *Runner) project(t *task.Task, name, logDir string) (docker.Project, error) {
	c := t.Environment.Compose
	p := docker.Project{
		Name:     projectName(name),
		Dir:      c.ProjectDir,
		Files:    append([]string(nil), c.Files...),
		Platform: r.platform(t),
		Env:      map[string]string{},
	}

	hostLogs, err := mountsDir(logDir)
	if err != nil {
		return p, err
	}
	if err := os.MkdirAll(hostLogs, 0o755); err != nil {
		return p, err
	}
	vars := map[string]string{"SKEPTIC_PROJECT": p.Name, "SKEPTIC_LOG_DIR": hostLogs}
	for k, v := range c.Env {
		p.Env[k] = os.Expand(v, func(key string) string {
			if val, ok := vars[key]; ok {
				return val
			}
			return "${" + key + "}"
		})
		// Docker creates a missing bind-mount source as root. Made here
		// first, the directories Skeptic names for the task's mounts belong
		// to whoever ran it, and can be cleaned up by them.
		if strings.HasPrefix(p.Env[k], hostLogs+string(filepath.Separator)) {
			if err := os.MkdirAll(p.Env[k], 0o755); err != nil {
				return p, err
			}
		}
	}

	// Limits and startup env go on the service the controls act on, as an
	// override file appended last, the way Harbor's resources and env
	// compose files work. Sidecars get neither.
	cpus, memMB := r.limits(t)
	if cpus > 0 || memMB > 0 || len(t.Environment.Env) > 0 {
		svc := map[string]interface{}{}
		if cpus > 0 {
			svc["cpus"] = strconv.FormatFloat(cpus, 'f', -1, 64)
		}
		if memMB > 0 {
			// Both, for the same reason docker.Start passes --memory-swap.
			svc["mem_limit"] = fmt.Sprintf("%dm", memMB)
			svc["memswap_limit"] = fmt.Sprintf("%dm", memMB)
		}
		if len(t.Environment.Env) > 0 {
			svc["environment"] = t.Environment.Env
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"services": map[string]interface{}{c.Service: svc},
		}, "", "  ")
		override, err := filepath.Abs(filepath.Join(logDir, "compose-override.json"))
		if err != nil {
			return p, err
		}
		if err := os.WriteFile(override, b, 0o644); err != nil {
			return p, err
		}
		p.Files = append(p.Files, override)
	}
	return p, nil
}

// buildCompose builds the task's services once, standing in for the image
// build of a single-container task. Each control then brings up its own
// project from the cached build.
func (r *Runner) buildCompose(ctx context.Context, t *task.Task, logDir string) error {
	p, err := r.project(t, "skeptic-build-"+sanitize(t.ID), logDir)
	if err != nil {
		return err
	}
	res, err := r.docker.ComposeBuild(ctx, p, t.Environment.BuildTimeout)
	writeFile(filepath.Join(logDir, "build.log"), res.Combined)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

// startCompose brings up a fresh project for one control and returns the
// container the controls act on, and how to tear the project down.
func (r *Runner) startCompose(ctx context.Context, t *task.Task, name, logDir string) (string, func(), error) {
	p, err := r.project(t, name, logDir)
	if err != nil {
		return "", func() {}, err
	}
	var image string
	down := func() {
		bg := context.WithoutCancel(ctx)
		writeFile(filepath.Join(logDir, "compose.log"), r.docker.ComposeLogs(bg, p))
		if r.opts.KeepContainers {
			r.log.Info("keeping compose project", "task", t.ID, "project", p.Name)
			return
		}
		if err := r.docker.ComposeDown(bg, p); err != nil {
			r.log.Warn("removing compose project", "task", t.ID, "err", err)
		}
		// The containers wrote to their mounts as root. Hand the evidence
		// back to the person who ran the check, so they can delete it.
		if uid := os.Getuid(); uid > 0 && image != "" {
			if dir, err := mountsDir(logDir); err == nil {
				if err := r.docker.Chown(bg, image, dir, uid, os.Getgid()); err != nil {
					r.log.Warn("restoring ownership of compose mounts", "task", t.ID, "dir", dir, "err", err)
				}
			}
		}
	}
	res, err := r.docker.ComposeUp(ctx, p, t.Environment.Compose.Wait, t.Environment.BuildTimeout)
	writeFile(filepath.Join(logDir, "compose-up.log"), res.Combined)
	if err != nil {
		// A sidecar that never became healthy, or a port already taken by
		// another task: the stack could not be run, so nothing is scored.
		return "", down, fmt.Errorf("starting compose project: %w", err)
	}
	id, err := r.docker.ComposeContainer(ctx, p, t.Environment.Compose.Service)
	if err != nil {
		return "", down, err
	}
	// Remembered for the ownership repair in down: the service's own image
	// is the one image certain to be present when the project is gone.
	if image, err = r.docker.ContainerImage(ctx, id); err != nil {
		r.log.Warn("finding the service image", "task", t.ID, "err", err)
	}
	return id, down, nil
}

// mountsDir is the host directory a control's project may bind-mount for
// its logs, SKEPTIC_LOG_DIR to the task's compose file.
func mountsDir(logDir string) (string, error) {
	return filepath.Abs(filepath.Join(logDir, "compose-mounts"))
}
