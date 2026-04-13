package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// LocalCommandExecutor executes commands on the local machine.
type LocalCommandExecutor struct{}

// NewLocalCommandExecutor returns a local command executor.
func NewLocalCommandExecutor() *LocalCommandExecutor {
	return &LocalCommandExecutor{}
}

// Execute runs the command with optional timeout, stdout/stderr capture and context cancellation.
func (e *LocalCommandExecutor) Execute(ctx context.Context, request CommandRequest) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	if request.Command == "" {
		return CommandResult{}, fmt.Errorf("executor: command is required")
	}

	runCtx := ctx
	cancel := func() {}
	if request.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, request.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, request.Command, request.Args...)
	if request.Dir != "" {
		cmd.Dir = request.Dir
	}
	if len(request.Env) > 0 {
		cmd.Env = request.Env
	}
	if len(request.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(request.Stdin)
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		return CommandResult{}, err
	}

	waitErr := cmd.Wait()
	finishedAt := time.Now()
	result := CommandResult{
		Command:    request.Command,
		Args:       append([]string(nil), request.Args...),
		Dir:        request.Dir,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Duration:   finishedAt.Sub(startedAt),
		Stdout:     normalizeOutput(stdoutBuf.Bytes()),
		Stderr:     normalizeOutput(stderrBuf.Bytes()),
	}

	if runCtx.Err() != nil {
		result.Canceled = true
		if runCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
		return result, runCtx.Err()
	}

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, waitErr
		}
		return result, waitErr
	}

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	return result, nil
}

func normalizeOutput(data []byte) string {
	output := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.ReplaceAll(output, "\r", "\n")
}
