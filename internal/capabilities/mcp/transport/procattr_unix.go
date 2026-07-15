//go:build unix

package transport

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	// Unix：独立进程组，借鉴 Codex process_group_cleanup。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
