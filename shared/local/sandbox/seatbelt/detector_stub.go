//go:build !darwin

package seatbelt

import "context"

// Detect 在非 macOS 平台返回不可用。
func Detect(ctx context.Context) (string, bool, string) {
	if err := ctx.Err(); err != nil {
		return Path, false, err.Error()
	}
	return Path, false, "seatbelt sandbox is only available on macOS"
}
