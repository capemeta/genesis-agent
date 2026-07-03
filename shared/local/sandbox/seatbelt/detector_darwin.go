//go:build darwin

package seatbelt

import (
	"context"
	"os"
)

// Detect 检查 macOS Seatbelt 是否可用。
func Detect(ctx context.Context) (string, bool, string) {
	if err := ctx.Err(); err != nil {
		return Path, false, err.Error()
	}
	info, err := os.Stat(Path)
	if err != nil {
		return Path, false, err.Error()
	}
	if info.IsDir() {
		return Path, false, "sandbox-exec路径是目录"
	}
	return Path, true, ""
}
