//go:build linux

package bubblewrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// Detect 检查 Linux bubblewrap 是否可用并可信。
//
// preferredPath 为产品配置的绝对 bwrap 路径（为空则走 PATH 探测）。
// 文档要求：产品配置的绝对路径优先；PATH 探测只是 fallback；
// 解析出的路径若位于不可信目录则视为 sandbox_unavailable。
func Detect(ctx context.Context, preferredPath string, trust HelperTrustOptions) (string, bool, string) {
	if err := ctx.Err(); err != nil {
		return "", false, err.Error()
	}

	path, err := resolveBwrapPath(preferredPath)
	if err != nil {
		return "", false, err.Error()
	}

	ok, reason := IsTrustedHelperPath(path, trust)
	if !ok {
		return path, false, reason
	}

	// 检查 user namespace 是否可用（WSL1 / 受限内核 / 容器环境）
	userNSOK, userNSReason := CheckUserNS()
	if !userNSOK {
		return path, false, userNSReason
	}

	return path, true, ""
}

// resolveBwrapPath 解析 bwrap 可执行文件路径。
// 优先使用 preferredPath（绝对路径），其次走 PATH 探测。
func resolveBwrapPath(preferredPath string) (string, error) {
	if preferredPath != "" {
		// 产品配置绝对路径：验证文件存在且为可执行文件
		abs, err := filepath.Abs(preferredPath)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", os.ErrNotExist
		}
		return abs, nil
	}

	// PATH 探测 fallback
	path, err := exec.LookPath("bwrap")
	if err != nil {
		path, err = exec.LookPath("bubblewrap")
	}
	return path, err
}
