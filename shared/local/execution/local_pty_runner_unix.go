//go:build !windows

package execution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// StartSession 在 Unix 物理系统上使用 creack/pty 启动 PTY 会话
func (r *LocalPTYRunner) StartSession(ctx context.Context, sessionID string, cmd execmodel.Command, opts execcontract.RunOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[sessionID]; exists {
		return fmt.Errorf("session [%s] already exists in runner", sessionID)
	}

	// 1. 设置 Shell 路径及命令
	var shellPath string
	var shellArgs []string

	if cmd.Shell == execmodel.ShellSystem {
		shellPath = cmd.Command
	} else if cmd.Shell == execmodel.ShellSh {
		shellPath = "/bin/sh"
		if cmd.Command != "" {
			shellArgs = []string{"-c", cmd.Command}
		}
	} else if cmd.Shell == execmodel.ShellZsh {
		shellPath = "/bin/zsh"
		if cmd.Command != "" {
			shellArgs = []string{"-c", cmd.Command}
		}
	} else {
		shellPath = "/bin/bash"
		if cmd.Command != "" {
			shellArgs = []string{"-c", cmd.Command}
		}
	}

	shellCmd := exec.Command(shellPath, shellArgs...)
	if cmd.Cwd != "" {
		shellCmd.Dir = cmd.Cwd
	}
	shellCmd.Env = os.Environ()
	for k, v := range cmd.Env {
		shellCmd.Env = append(shellCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// 开启进程组（Setpgid），以便后续可以对其组 PGID 进行整树级强杀（SIGKILL）
	shellCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 2. 启动物理 PTY
	f, err := pty.Start(shellCmd)
	if err != nil {
		return fmt.Errorf("unix pty.Start failed: %w", err)
	}

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	sess := &activeSession{
		cmd:      shellCmd,
		stdin:    f,
		outputCh: make(chan []byte, 1024),
		cancel:   sessionCancel,
		done:     make(chan struct{}),
	}
	r.sessions[sessionID] = sess

	// 3. 异步读取 PTY 输出，推入 outputCh 管道中
	go func() {
		defer close(sess.done)
		defer close(sess.outputCh)
		defer f.Close()

		buf := make([]byte, 8192)
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
				n, err := f.Read(buf)
				if n > 0 {
					data := make([]byte, n)
					copy(data, buf[:n])
					sess.outputCh <- data
				}
				if err != nil {
					return
				}
			}
		}
	}()

	return nil
}

// killProcessTree 在 Unix 下强杀整个子进程组
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 向进程组的负 PGID 发送信号以完成树级销毁
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	_ = cmd.Process.Kill()
	return nil
}
