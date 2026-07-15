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

func IsWindowsSetupReady() bool {
	return false
}

func RunWindowsSetup() error {
	return fmt.Errorf("Windows本地沙箱环境初始化仅支持Windows系统")
}

func GetWorkspaceCapabilitySID(workspacePath string) (string, error) {
	return "", fmt.Errorf("Windows capability SID generation仅支持Windows系统")
}

func ApplyWorkspaceACLs(workspacePath string, writables []string, unreadables []string) error {
	return fmt.Errorf("Windows ACL setup仅支持Windows系统")
}

func IsWindowsNetworkSetupReady() bool {
	return false
}

func GetSandboxUsername() string {
	return ""
}

func ApplyWorkspaceACLsForUser(workspacePath string, username string, writables []string, unreadables []string) error {
	return fmt.Errorf("Windows ACL setup for user仅支持Windows系统")
}

func RunWindowsSetupWithFlags(setupNetwork bool) error {
	return fmt.Errorf("Windows本地沙箱环境初始化仅支持Windows系统")
}

func IsElevated() bool {
	return false
}

func SetSandboxDirOverride(dir string) {
}

func IsFirewallUnsupported() bool {
	return false
}

