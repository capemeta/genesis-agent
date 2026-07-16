//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// PrepareConsoleOutput 为 Windows Console 启用 VT 输出及延迟换行。
// 返回的函数用于在 TUI 退出后恢复原始控制台模式。
func PrepareConsoleOutput() func() {
	handle := windows.Handle(os.Stdout.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		return func() {}
	}
	mode := original |
		windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING |
		windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(handle, mode); err != nil {
		return func() {}
	}
	return func() {
		_ = windows.SetConsoleMode(handle, original)
	}
}
