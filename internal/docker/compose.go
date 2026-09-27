package docker

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Project is one Docker Compose project: a set of compose files brought up
// under a name of its own. Every control gets a fresh project, the way every
// single-container control gets a fresh container.
type Project struct {
	Name string
	// Dir is the project directory, which relative paths in the files
	// resolve against.
	Dir   string
	Files []string
	// Env holds the variables the files interpolate.
	Env map[string]string
	// Platform, when set, is passed as DOCKER_DEFAULT_PLATFORM, which is how
	// Compose is told to build and run for another architecture.
	Platform string
}

func (c *Client) compose(ctx context.Context, p Project, timeout time.Duration, args ...string) (Result, error) {
	full := []string{"compose", "-p", p.Name}
	if p.Dir != "" {
		full = append(full, "--project-directory", p.Dir)
	}
	for _, f := range p.Files {
		full = append(full, "-f", f)
	}
	env := map[string]string{}
	for k, v := range p.Env {
		env[k] = v
	}
	if p.Platform != "" {
		env["DOCKER_DEFAULT_PLATFORM"] = p.Platform
	}
	return c.runEnv(ctx, timeout, env, append(full, args...)...)
}

// ComposeBuild builds every service that has a build unit.
func (c *Client) ComposeBuild(ctx context.Context, p Project, timeout time.Duration) (Result, error) {
	res, err := c.compose(ctx, p, timeout, "build")
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("compose build failed (exit %d)", res.ExitCode)
	}
	return res, nil
}

// ComposeUp starts the project detached. With wait, it returns only once
// every service with a healthcheck reports healthy, which is what Harbor
// does; Terminal-Bench 1.x starts without waiting, and so does wait=false.
func (c *Client) ComposeUp(ctx context.Context, p Project, wait bool, timeout time.Duration) (Result, error) {
	args := []string{"up", "-d"}
	if wait {
		args = append(args, "--wait")
	}
	res, err := c.compose(ctx, p, timeout, args...)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("compose up failed (exit %d): %s", res.ExitCode, lastLine(res.Stderr))
	}
	return res, nil
}

// ComposeContainer returns the ID of the container running service.
func (c *Client) ComposeContainer(ctx context.Context, p Project, service string) (string, error) {
	res, err := c.compose(ctx, p, time.Minute, "ps", "-q", service)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(res.Stdout)
	if res.ExitCode != 0 || id == "" || strings.Contains(id, "\n") {
		return "", fmt.Errorf("no single running container for service %q: %s", service, lastLine(res.Stderr))
	}
	return id, nil
}

// ComposeLogs returns the logs of every service, for the evidence directory:
// when a sidecar misbehaves, its output is the only record of why.
func (c *Client) ComposeLogs(ctx context.Context, p Project) string {
	res, _ := c.compose(ctx, p, time.Minute, "logs", "--no-color")
	return res.Combined
}

// ComposeDown removes the project's containers, networks and volumes. Named
// volumes go too: a control must not inherit state from the one before it.
func (c *Client) ComposeDown(ctx context.Context, p Project) error {
	res, err := c.compose(ctx, p, 5*time.Minute, "down", "-v", "--remove-orphans", "--timeout", "10")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("compose down: %s", lastLine(res.Stderr))
	}
	return nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
