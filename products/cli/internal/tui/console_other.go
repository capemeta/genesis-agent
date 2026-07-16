//go:build !windows

package tui

// PrepareConsoleOutput 在非 Windows 平台无需额外设置。
func PrepareConsoleOutput() func() {
	return func() {}
}
