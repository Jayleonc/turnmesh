package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLocalCommandExecutorCapturesStdoutAndStderr(t *testing.T) {
	exec := NewLocalCommandExecutor()

	result, err := exec.Execute(context.Background(), CommandRequest{
		Command: "sh",
		Args:    []string{"-c", `printf 'hello\n'; printf 'oops\n' >&2`},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := strings.TrimSpace(result.Stdout), "hello"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := strings.TrimSpace(result.Stderr), "oops"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestLocalCommandExecutorHonorsTimeout(t *testing.T) {
	exec := NewLocalCommandExecutor()

	_, err := exec.Execute(context.Background(), CommandRequest{
		Command: "sh",
		Args:    []string{"-c", "sleep 2"},
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false, err=%v", err)
	}
}
