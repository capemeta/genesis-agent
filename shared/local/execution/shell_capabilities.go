package execution

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

var (
	shellCapabilitiesOnce sync.Once
	shellCapabilities     execmodel.ShellCapabilities
)

// detectedShellCapabilities 探测本地主机实际可执行的 Shell。
// Windows 优先 PowerShell，避免 cmd.exe 对引号、尾部反斜杠和正斜杠的特殊解析。
func detectedShellCapabilities() execmodel.ShellCapabilities {
	shellCapabilitiesOnce.Do(func() {
		shellCapabilities = detectShellCapabilities()
	})
	return execmodel.ShellCapabilities{
		Default:   shellCapabilities.Default,
		Supported: append([]execmodel.ShellInfo(nil), shellCapabilities.Supported...),
	}
}

func detectShellCapabilities() execmodel.ShellCapabilities {
	var supported []execmodel.ShellInfo
	add := func(kind execmodel.ShellKind, candidates ...string) {
		for _, candidate := range candidates {
			path, err := exec.LookPath(candidate)
			if err != nil || strings.TrimSpace(path) == "" {
				continue
			}
			supported = append(supported, execmodel.ShellInfo{Kind: kind, Path: path})
			return
		}
	}

	switch runtime.GOOS {
	case "windows":
		add(execmodel.ShellPowerShell, "pwsh.exe", "pwsh", "powershell.exe", "powershell")
		add(execmodel.ShellCmd, windowsShell())
	case "darwin":
		add(execmodel.ShellZsh, "zsh")
		add(execmodel.ShellBash, "bash")
		add(execmodel.ShellSh, "sh")
	default:
		add(execmodel.ShellBash, "bash")
		add(execmodel.ShellSh, "sh")
		add(execmodel.ShellZsh, "zsh")
	}

	if len(supported) == 0 {
		return execmodel.ShellCapabilities{}
	}
	return execmodel.ShellCapabilities{Default: supported[0], Supported: supported}
}

func findShell(kind execmodel.ShellKind) (execmodel.ShellInfo, bool) {
	for _, shell := range detectedShellCapabilities().Supported {
		if shell.Kind == kind {
			return shell, true
		}
	}
	return execmodel.ShellInfo{}, false
}
