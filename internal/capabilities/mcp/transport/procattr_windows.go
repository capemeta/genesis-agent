//go:build windows

package transport

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	// Windows：创建新进程组，便于后续按树清理（对齐 Codex taskkill /T 语义的前置条件）。
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
