//go:build linux

package landlock

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LandlockABI 描述 Landlock 内核 ABI 版本（1=v5.13, 2=v5.19, 3=v6.2, ...）。
type LandlockABI int

// Detect 通过 landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION)
// 探测内核 Landlock ABI 版本。返回 (可用, 原因说明)。
func Detect(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	abi, err := probeABI()
	if err != nil {
		return false, fmt.Sprintf("Landlock ABI探测失败: %v", err)
	}
	if abi <= 0 {
		return false, "Landlock ABI版本不满足最低要求（需要 ABI >= 1 / kernel >= 5.13）"
	}
	return true, fmt.Sprintf("Landlock ABI版本: %d", abi)
}

// probeABI 调用 landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION=1)
// 成功时返回内核支持的最高 ABI 版本号。
func probeABI() (LandlockABI, error) {
	// LANDLOCK_CREATE_RULESET_VERSION = 1 << 0
	const landlock_create_ruleset_version = 1
	r1, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0,                              // attr = NULL
		0,                              // size = 0
		landlock_create_ruleset_version, // flags
	)
	if errno != 0 {
		if errno == unix.ENOSYS || errno == unix.EOPNOTSUPP {
			return 0, nil // 内核不支持 Landlock，不视为错误
		}
		return 0, fmt.Errorf("syscall landlock_create_ruleset: %w", errno)
	}
	// 返回值即 ABI 版本
	_ = unsafe.Sizeof(r1) // 防止 r1 unused 警告
	return LandlockABI(r1), nil
}
