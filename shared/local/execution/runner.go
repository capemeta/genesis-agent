// Package execution 提供本地主机命令执行适配。
package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

const defaultMaxOutputBytes = int64(128 * 1024)

// Runner 在本地主机执行平台 shell 命令。
type Runner struct{}

// NewRunner 创建本地命令 runner。
func NewRunner() *Runner { return &Runner{} }

// Run 执行命令并收集 stdout/stderr。
func (r *Runner) Run(ctx context.Context, command execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if command.Command == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("command不能为空"))
	}
	shell := normalizeShell(command.Shell)
	argv, shell, err := ShellArgv(shell, command.Command)
	if err != nil {
		return nil, err
	}
	return r.RunArgv(ctx, ArgvCommand{
		Argv:           argv,
		Cwd:            command.Cwd,
		Env:            command.Env,
		DisplayCommand: command.Command,
		Shell:          shell,
	}, opts)
}

type execResultMeta struct {
	Command string
	Cwd     string
	Shell   execmodel.ShellKind
	Env     map[string]string
}

type afterStartFunc func(*exec.Cmd) error

type commandFactory func(context.Context, []string) (*exec.Cmd, afterStartFunc, func(), error)

func (r *Runner) runArgv(ctx context.Context, argv []string, meta execResultMeta, opts execcontract.RunOptions) (*execmodel.Result, error) {
	return r.runArgvWithFactory(ctx, argv, meta, opts, defaultCommandFactory)
}

func (r *Runner) runArgvWithFactory(ctx context.Context, argv []string, meta execResultMeta, opts execcontract.RunOptions, factory commandFactory) (*execmodel.Result, error) {
	limit := opts.MaxOutputBytes
	if limit <= 0 {
		limit = defaultMaxOutputBytes
	}
	runCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()
	prepared, afterStart, cleanup, err := factory(runCtx, argv)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	prepared.Env = mergeEnv(os.Environ(), meta.Env)
	if meta.Cwd != "" {
		prepared.Dir = meta.Cwd
	}
	if prepared.Err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, prepared.Err)
	}

	stdout := newLimitBuffer(limit)
	stderr := newLimitBuffer(limit)
	prepared.Stdout = stdout
	prepared.Stderr = stderr

	started := time.Now()
	startErr := prepared.Start()
	if startErr == nil && afterStart != nil {
		startErr = afterStart(prepared)
	}
	var runErr error
	if startErr != nil {
		runErr = startErr
		if prepared.Process != nil {
			_ = prepared.Process.Kill()
			_ = prepared.Wait()
		}
	} else {
		runErr = prepared.Wait()
	}
	duration := time.Since(started)
	result := &execmodel.Result{
		Command:         meta.Command,
		Cwd:             meta.Cwd,
		Shell:           meta.Shell,
		Environment:     execmodel.EnvironmentLocal,
		ExitCode:        exitCode(runErr),
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		Duration:        duration,
		DurationMS:      duration.Milliseconds(),
		TimedOut:        errors.Is(runCtx.Err(), context.DeadlineExceeded),
		OutputTruncated: stdout.Truncated() || stderr.Truncated(),
	}
	if result.TimedOut {
		result.Error = "command timed out"
		return result, nil
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.Error = runErr.Error()
			return result, nil
		}
		return nil, execcontract.NewError(execcontract.ErrCodeRunnerFailed, runErr)
	}
	return result, nil
}
func normalizeShell(shell execmodel.ShellKind) execmodel.ShellKind {
	if shell == "" || shell == execmodel.ShellAuto || shell == execmodel.ShellSystem {
		if runtime.GOOS == "windows" {
			return execmodel.ShellCmd
		}
		if runtime.GOOS == "darwin" {
			return execmodel.ShellZsh
		}
		return execmodel.ShellBash
	}
	return shell
}

func windowsShell() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "cmd.exe")
	}
	return "cmd.exe"
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	out := append([]string{}, base...)
	for key, value := range extra {
		out = append(out, key+"="+value)
	}
	return out
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

type limitBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	written   int64
	truncated bool
}

func newLimitBuffer(limit int64) *limitBuffer {
	return &limitBuffer{limit: limit}
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.written
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	writeLen := int64(len(p))
	if writeLen > remaining {
		b.buf.Write(p[:remaining])
		b.written += remaining
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	b.written += writeLen
	return len(p), nil
}

func (b *limitBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
