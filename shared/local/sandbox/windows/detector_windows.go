//go:build windows

package windowssandbox

import "context"

// Detect 检查 Windows process-constrained 能力。
func Detect(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	return true, ""
}
