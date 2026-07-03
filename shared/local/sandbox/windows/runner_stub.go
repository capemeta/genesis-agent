//go:build !windows

package windowssandbox

import (
	"fmt"
	"os/exec"
)

type PreparedCommandCleanup func()

func PrepareRestrictedCommand(cmd *exec.Cmd) (func(*exec.Cmd) error, PreparedCommandCleanup, error) {
	return nil, nil, fmt.Errorf("Windows restricted token sandbox仅支持Windows")
}
