//go:build windows

package execution

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// StartSession 在 Windows 环境下使用 StdIO 管道进行交互模拟与优雅降级回退
func (r *LocalPTYRunner) StartSession(ctx context.Context, sessionID string, cmd execmodel.Command, opts execcontract.RunOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[sessionID]; exists {
		return fmt.Errorf("session [%s] already exists in runner", sessionID)
	}

	// 1. 组装 Windows 命令
	var shellPath string
	var shellArgs []string

	if cmd.Shell == execmodel.ShellSystem {
		shellPath = cmd.Command
	} else if cmd.Shell == execmodel.ShellCmd {
		shellPath = "cmd.exe"
		if cmd.Command != "" {
			shellArgs = []string{"/c", cmd.Command}
		}
	} else {
		// 默认选用 powershell 获得更好的脚本解析特性
		shellPath = "powershell.exe"
		if cmd.Command != "" {
			shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmd.Command}
		} else {
			shellArgs = []string{"-NoProfile"}
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

	// 2. 双向管道嫁接
	stdin, err := shellCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("windows stdin pipe failed: %w", err)
	}

	stdout, err := shellCmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("windows stdout pipe failed: %w", err)
	}

	shellCmd.Stderr = shellCmd.Stdout // 混合 stdout 与 stderr 字节

	// 3. 启动后台物理进程
	if err := shellCmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("windows process start failed: %w", err)
	}

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	sess := &activeSession{
		cmd:      shellCmd,
		stdin:    stdin,
		outputCh: make(chan []byte, 1024),
		cancel:   sessionCancel,
		done:     make(chan struct{}),
	}
	r.sessions[sessionID] = sess

	// 4. 异步推送 stdout 管道数据
	go func() {
		defer close(sess.done)
		defer close(sess.outputCh)
		defer stdin.Close()
		defer stdout.Close()

		buf := make([]byte, 8192)
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
				n, err := stdout.Read(buf)
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

// killProcessTree 在 Windows 下使用 taskkill 进行级联树清理，以防止子进程孤儿化泄漏
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	_ = killCmd.Run() // 尽力而为强杀

	_ = cmd.Process.Kill()
	return nil
}
