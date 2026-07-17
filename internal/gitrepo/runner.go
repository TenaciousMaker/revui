package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Runner is the Git subprocess seam. Its compact interface keeps cancellation,
// stderr handling, and test substitution out of repository logic.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout string, exitCode int, err error)
}

// ExecRunner executes the Git available on PATH.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		exitCode := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			exitCode = exit.ExitCode()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return stdout.String(), exitCode, errors.New(message)
	}
	return stdout.String(), 0, nil
}
