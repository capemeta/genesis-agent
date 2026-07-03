//go:build linux

package bubblewrap

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// CheckUserNS 检查当前 Linux 环境是否支持 rootless user namespace（bwrap 的前提）。
//
// 检查顺序：
//  1. WSL1 检测：WSL1 完全不支持 user namespace，直接拒绝。
//  2. WSL2 检测：WSL2 支持 user namespace，通过；记录 WSLVersion 供调用方 audit 使用。
//  3. unprivileged_userns_clone 策略检测（Debian/Ubuntu 内核补丁，WSL2 微软内核无此文件）。
//  4. 实际 unshare(CLONE_NEWUSER) 探测：以 syscall 结果为最终依据，覆盖所有边界情况。
func CheckUserNS() (bool, string) {

	wslVer := DetectWSLVersion()

	// WSL1 直接拒绝
	if wslVer == WSL1 {
		return false, "当前运行在 WSL1 环境，WSL1 不支持 user namespace，bwrap 不可用；" +
			"请升级到 WSL2 或在原生 Linux 上运行"
	}

	// unprivileged_userns_clone 策略检测（Debian/Ubuntu 系列内核补丁）
	// WSL2 微软内核不含此文件，文件不存在时跳过（默认允许）
	data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone")
	if err == nil {
		val := strings.TrimSpace(string(data))
		if val == "0" {
			return false, "内核限制 unprivileged_userns_clone=0，bwrap user namespace 不可用；" +
				"可通过 'sudo sysctl kernel.unprivileged_userns_clone=1' 开启"
		}
	}

	// 实际探测：尝试创建 user namespace，以 syscall 结果为最终依据
	// 这能覆盖所有 WSL2 内核版本差异、容器环境、AppArmor/seccomp 限制等边界情况
	if ok, reason := probeUserNamespace(); !ok {
		prefix := ""
		if wslVer == WSL2 {
			prefix = "WSL2 环境："
		}
		return false, prefix + reason
	}

	if wslVer == WSL2 {
		// WSL2 支持 user namespace，提供说明性信息（非错误）
		// 网络 namespace (--unshare-net) 在部分 WSL2 内核版本有限制，
		// 但无法统一检测，由 bwrap 运行时失败处理
		return true, "WSL2环境，user namespace可用"
	}

	return true, ""
}

// DetectWSLVersion 读取 /proc/version 判断 WSL 版本。
//
// /proc/version 样本：
//   - WSL1：Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) ...
//   - WSL2：Linux version 5.15.153.1-microsoft-standard-WSL2 (...)
//   - 原生：Linux version 6.1.0-21-amd64 (debian-kernel@...) ...
func DetectWSLVersion() WSLVersion {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return WSLNone
	}
	return parseWSLVersion(string(data))
}

// parseWSLVersion 从 /proc/version 内容字符串解析 WSL 版本，便于单测。
func parseWSLVersion(content string) WSLVersion {
	lc := strings.ToLower(content)
	if !strings.Contains(lc, "microsoft") {
		return WSLNone
	}
	// WSL2 内核版本字符串含 "wsl2"（如 "microsoft-standard-WSL2"）
	if strings.Contains(lc, "wsl2") {
		return WSL2
	}
	// 含 "microsoft" 但无 "wsl2" → WSL1（如 "4.4.0-19041-Microsoft"）
	return WSL1
}

// probeUserNamespace 通过 unshare(CLONE_NEWUSER) syscall 实际探测 user namespace 是否可创建。
// 使用 clone3/unshare 而非 fork，避免子进程开销。
//
// 原理：CLONE_NEWUSER 是最轻量的 namespace 类型，不需要 root 权限，
// 且不实际影响当前进程（unshare 后立即返回，namespace 跟随进程退出自动销毁）。
func probeUserNamespace() (bool, string) {
	err := unix.Unshare(unix.CLONE_NEWUSER)
	if err != nil {
		return false, fmt.Sprintf("user namespace 创建失败（unshare CLONE_NEWUSER: %v）；"+
			"可能原因：内核不支持、AppArmor/seccomp 限制、容器禁止嵌套 namespace", err)
	}
	return true, ""
}
