//go:build !windows

package windowssandbox

import "fmt"

// ElevationResult 描述提权子进程的执行结果。
type ElevationResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// WriteElevationResult 在非 Windows 平台为空实现。
func WriteElevationResult(cwd string, result ElevationResult) {}

// ReadElevationResult 在非 Windows 平台返回错误。
func ReadElevationResult(cwd string) (*ElevationResult, error) {
	return nil, fmt.Errorf("非 Windows 平台不支持")
}

// ClearElevationResult 在非 Windows 平台为空实现。
func ClearElevationResult(cwd string) {}

// RunElevatedWindowsSetup 在非 Windows 平台返回错误。
func RunElevatedWindowsSetup(exeOrGoRun string, args []string, workDir string) error {
	return fmt.Errorf("RunElevatedWindowsSetup 仅支持 Windows 平台")
}

