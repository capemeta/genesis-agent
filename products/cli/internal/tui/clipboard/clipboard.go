// Package clipboard 封装 TUI 文本剪贴板能力，并按当前终端环境选择降级后端。
package clipboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	atottoclipboard "github.com/atotto/clipboard"
)

const (
	osc52MaxRawBytes = 100_000
	commandTimeout   = 3 * time.Second
)

type copyEnvironment struct {
	ssh  bool
	wsl  bool
	tmux bool
}

type copyBackend func(text string) error

// Write 将文本写入用户实际可访问的剪贴板。
//
// 本地会话优先使用系统剪贴板；WSL 中再尝试 Windows PowerShell；
// SSH 会话直接使用 OSC 52，避免把内容写到远程主机的剪贴板。
func Write(text string) error {
	environment := detectEnvironment()
	return writeWithBackends(
		text,
		environment,
		atottoclipboard.WriteAll,
		writeViaPowerShell,
		func(value string) error { return writeViaOSC52(value, environment.tmux) },
	)
}

// Read 从本机原生剪贴板读取文本。
func Read() (string, error) {
	text, err := atottoclipboard.ReadAll()
	if err != nil {
		return "", fmt.Errorf("读取系统剪贴板: %w", err)
	}
	return text, nil
}

func writeWithBackends(
	text string,
	environment copyEnvironment,
	native copyBackend,
	powerShell copyBackend,
	terminal copyBackend,
) error {
	if environment.ssh {
		if err := terminal(text); err != nil {
			return fmt.Errorf("SSH 会话 OSC 52 复制失败: %w", err)
		}
		return nil
	}

	nativeErr := native(text)
	if nativeErr == nil {
		return nil
	}

	if environment.wsl {
		powerShellErr := powerShell(text)
		if powerShellErr == nil {
			return nil
		}
		if terminalErr := terminal(text); terminalErr != nil {
			return fmt.Errorf(
				"原生剪贴板: %w; WSL PowerShell: %w; OSC 52: %w",
				nativeErr,
				powerShellErr,
				terminalErr,
			)
		}
		return nil
	}

	if terminalErr := terminal(text); terminalErr != nil {
		return fmt.Errorf("原生剪贴板: %w; OSC 52: %w", nativeErr, terminalErr)
	}
	return nil
}

func detectEnvironment() copyEnvironment {
	return copyEnvironment{
		ssh:  os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "",
		wsl:  os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "",
		tmux: os.Getenv("TMUX") != "" || os.Getenv("TMUX_PANE") != "",
	}
}

func writeViaPowerShell(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Set-Clipboard -Value ([Console]::In.ReadToEnd())",
	)
	cmd.Stdin = strings.NewReader(text)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("执行 PowerShell 复制超时: %w", ctx.Err())
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("执行 PowerShell 复制: %w", err)
	}
	return fmt.Errorf("执行 PowerShell 复制: %w: %s", err, detail)
}

func writeViaOSC52(text string, tmux bool) error {
	sequence, err := osc52Sequence(text, tmux)
	if err != nil {
		return err
	}

	terminal, openErr := openTerminalOutput()
	if openErr == nil {
		defer terminal.Close()
		if err := writeAndFlush(terminal, sequence); err == nil {
			return nil
		}
	}
	if err := writeAndFlush(os.Stdout, sequence); err != nil {
		if openErr != nil {
			return fmt.Errorf("打开终端输出: %w; 写入 stdout: %w", openErr, err)
		}
		return fmt.Errorf("写入 OSC 52: %w", err)
	}
	return nil
}

func openTerminalOutput() (*os.File, error) {
	paths := []string{"/dev/tty"}
	if os.PathSeparator == '\\' {
		paths = []string{"CONOUT$"}
	}
	var lastErr error
	for _, path := range paths {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			return file, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("无法打开控制终端: %w", lastErr)
}

func writeAndFlush(writer io.Writer, value string) error {
	if _, err := io.WriteString(writer, value); err != nil {
		return fmt.Errorf("写入终端: %w", err)
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("刷新终端: %w", err)
		}
	}
	return nil
}

func osc52Sequence(text string, tmux bool) (string, error) {
	if len(text) > osc52MaxRawBytes {
		return "", fmt.Errorf("OSC 52 内容过大（%d bytes，上限 %d bytes）", len(text), osc52MaxRawBytes)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if tmux {
		return "\x1bPtmux;\x1b\x1b]52;c;" + encoded + "\x07\x1b\\", nil
	}
	return "\x1b]52;c;" + encoded + "\x07", nil
}
