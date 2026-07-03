//go:build !linux

package landlock

import "context"

// Detect 在非 Linux 平台返回不可用。
func Detect(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	return false, "Landlock is only available on Linux"
}
