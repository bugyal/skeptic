// Package docker drives containers through the docker CLI.
//
// The CLI is used rather than the Docker Go SDK so that skeptic stays a small
// static binary with no container-runtime dependency tree, and so that any
// CLI-compatible runtime (docker, podman) works. Every call goes through
// Client, so swapping in the SDK later means replacing one file.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client runs docker commands.
type Client struct {
	Bin string
	Log *slog.Logger
}

// New returns a Client using the docker binary on PATH.
func New(log *slog.Logger) *Client {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &Client{Bin: "docker", Log: log}
}

// Result is the outcome of one command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// TimedOut distinguishes a killed command from one that exited non-zero.
	// Conflating them would let a timeout masquerade as a test failure.
	TimedOut bool
	Duration time.Duration
}

func (c *Client) run(ctx context.Context, timeout time.Duration, args ...string) (Result, error) {
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	cmd := exec.CommandContext(runCtx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	c.Log.Debug("docker", "args", strings.Join(args, " "))
	err := cmd.Run()

	res := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}
	// A deadline on the child context, with the parent still live, means the
	// timeout fired rather than the user cancelling.
	if runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		res.TimedOut = true
		res.ExitCode = -1
		return res, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil // non-zero exit is data, not a failure to run
		}
		return res, fmt.Errorf("running docker %s: %w", args[0], err)
	}
	return res, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// Available reports an error if the docker CLI is missing or the daemon is down.
func (c *Client) Available(ctx context.Context) error {
	if _, err := exec.LookPath(c.Bin); err != nil {
		return fmt.Errorf("%s not found on PATH", c.Bin)
	}
	res, err := c.run(ctx, 30*time.Second, "version", "--format", "{{.Server.APIVersion}}")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("docker daemon unreachable: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// APIVersion returns the server API version, or "" if it cannot be determined.
func (c *Client) APIVersion(ctx context.Context) string {
	res, err := c.run(ctx, 30*time.Second, "version", "--format", "{{.Server.APIVersion}}")
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// BuildOptions configures an image build.
type BuildOptions struct {
	Dockerfile string
	ContextDir string
	Tag        string
	BuildArgs  map[string]string
	Timeout    time.Duration
	NoCache    bool
	Platform   string
}

// Build builds an image and returns its ID. Build output is returned on the
// Result so a failed build can be captured as evidence rather than discarded.
func (c *Client) Build(ctx context.Context, o BuildOptions) (string, Result, error) {
	args := []string{"build", "-f", o.Dockerfile, "-t", o.Tag}
	if o.NoCache {
		args = append(args, "--no-cache")
	}
	if o.Platform != "" {
		args = append(args, "--platform", o.Platform)
	}
	for k, v := range o.BuildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}
	args = append(args, o.ContextDir)

	res, err := c.run(ctx, o.Timeout, args...)
	if err != nil {
		return "", res, err
	}
	if res.ExitCode != 0 {
		return "", res, fmt.Errorf("image build failed (exit %d)", res.ExitCode)
	}
	return c.ImageID(ctx, o.Tag), res, nil
}

// ImageID resolves an image reference to its content digest, used to record
// exactly which image produced a verdict.
func (c *Client) ImageID(ctx context.Context, ref string) string {
	res, err := c.run(ctx, 30*time.Second, "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// Pull fetches a prebuilt image.
func (c *Client) Pull(ctx context.Context, ref string, timeout time.Duration) (Result, error) {
	res, err := c.run(ctx, timeout, "pull", ref)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("pulling %s failed: %s", ref, strings.TrimSpace(res.Stderr))
	}
	return res, nil
}

// StartOptions configures a container.
type StartOptions struct {
	Image    string
	Name     string
	WorkDir  string
	Env      map[string]string
	Platform string
	Memory   string
	CPUs     string
}

// Start launches a detached container that idles until Remove is called, so
// the controls can exec into a live environment the way a real harness does.
func (c *Client) Start(ctx context.Context, o StartOptions) (string, error) {
	args := []string{"run", "-d", "--entrypoint", ""}
	if o.Name != "" {
		args = append(args, "--name", o.Name)
	}
	if o.WorkDir != "" {
		args = append(args, "-w", o.WorkDir)
	}
	if o.Platform != "" {
		args = append(args, "--platform", o.Platform)
	}
	if o.Memory != "" {
		args = append(args, "--memory", o.Memory)
	}
	if o.CPUs != "" {
		args = append(args, "--cpus", o.CPUs)
	}
	for k, v := range o.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, o.Image, "sh", "-c", "sleep infinity")

	res, err := c.run(ctx, 5*time.Minute, args...)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("starting container: %s", strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ExecOptions configures a command inside a container.
type ExecOptions struct {
	WorkDir string
	Env     map[string]string
	User    string
	Timeout time.Duration
}

// Exec runs a shell command inside a running container.
func (c *Client) Exec(ctx context.Context, container, command string, o ExecOptions) (Result, error) {
	args := []string{"exec"}
	if o.WorkDir != "" {
		args = append(args, "-w", o.WorkDir)
	}
	if o.User != "" {
		args = append(args, "-u", o.User)
	}
	for k, v := range o.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, container, "sh", "-c", command)
	return c.run(ctx, o.Timeout, args...)
}

// CopyIn copies a host file or directory into the container. A trailing
// "/." on a directory source copies its contents, matching docker cp semantics.
func (c *Client) CopyIn(ctx context.Context, container, hostPath, containerPath string) error {
	if _, err := c.Exec(ctx, container, "mkdir -p "+shellQuote(filepath.Dir(containerPath)), ExecOptions{Timeout: time.Minute}); err != nil {
		return err
	}
	res, err := c.run(ctx, 10*time.Minute, "cp", hostPath, container+":"+containerPath)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copying %s into container: %s", hostPath, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// CopyOut copies a path out of the container onto the host.
func (c *Client) CopyOut(ctx context.Context, container, containerPath, hostPath string) error {
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return err
	}
	res, err := c.run(ctx, 10*time.Minute, "cp", container+":"+containerPath, hostPath)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copying %s out of container: %s", containerPath, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// ReadFile returns the contents of a file inside the container. A missing file
// is reported as os.ErrNotExist so callers can tell absent from unreadable.
func (c *Client) ReadFile(ctx context.Context, container, containerPath string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "skeptic-read-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	dst := filepath.Join(dir, "f")
	if err := c.CopyOut(ctx, container, containerPath, dst); err != nil {
		if strings.Contains(err.Error(), "No such container:path") ||
			strings.Contains(err.Error(), "no such file or directory") ||
			strings.Contains(err.Error(), "Could not find the file") {
			return nil, fmt.Errorf("%s: %w", containerPath, os.ErrNotExist)
		}
		return nil, err
	}
	return os.ReadFile(dst)
}

// Remove force-removes a container. It is safe to call on an already-gone
// container so it can be used unconditionally in cleanup paths.
func (c *Client) Remove(ctx context.Context, container string) error {
	// Cleanup must still work when the caller's context is already cancelled,
	// which is exactly the Ctrl-C case.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	res, err := c.run(cleanupCtx, 2*time.Minute, "rm", "-f", container)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stderr, "No such container") {
		return fmt.Errorf("removing container: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// RemoveImage deletes an image by reference, ignoring absence.
func (c *Client) RemoveImage(ctx context.Context, ref string) error {
	_, err := c.run(ctx, 2*time.Minute, "image", "rm", "-f", ref)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
