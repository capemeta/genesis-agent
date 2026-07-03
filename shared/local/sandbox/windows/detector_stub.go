//go:build !windows

package windowssandbox

import "context"

// Detect 在非 Windows 平台返回不可用。
func Detect(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	return false, "Windows process constrained sandbox is only available on Windows"
}
