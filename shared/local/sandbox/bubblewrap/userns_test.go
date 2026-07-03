//go:build linux

package bubblewrap

import "testing"

// parseWSLVersion 是包内函数，在 Linux 构建时直接测试。
// 不依赖 /proc/version 文件，纯字符串解析，可在任何 Linux 环境运行。

func TestParseWSLVersionNative(t *testing.T) {
	// 原生 Linux 内核，不含 "microsoft"
	content := "Linux version 6.1.0-21-amd64 (debian-kernel@lists.debian.org) " +
		"(gcc-12 (Debian 12.2.0-14) 12.2.0, GNU ld (GNU Binutils for Debian) 2.40)"
	if got := parseWSLVersion(content); got != WSLNone {
		t.Fatalf("原生Linux应为WSLNone，got=%d", got)
	}
}

func TestParseWSLVersionWSL1(t *testing.T) {
	// WSL1 内核：含 "Microsoft" 但不含 "WSL2"
	content := "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) " +
		"(gcc version 5.4.0 (GCC) ) #488-Microsoft Mon Sep 01 13:43:00 PST 2020"
	if got := parseWSLVersion(content); got != WSL1 {
		t.Fatalf("WSL1应为WSL1，got=%d", got)
	}
}

func TestParseWSLVersionWSL2(t *testing.T) {
	// WSL2 内核：含 "microsoft" 且含 "WSL2"
	content := "Linux version 5.15.153.1-microsoft-standard-WSL2 " +
		"(root@65c757a075e2) (gcc (GCC) 11.4.0) #1 SMP Wed Apr 30 23:43:10 UTC 2025"
	if got := parseWSLVersion(content); got != WSL2 {
		t.Fatalf("WSL2应为WSL2，got=%d", got)
	}
}

func TestParseWSLVersionWSL2NewerKernel(t *testing.T) {
	// WSL2 更新版本（6.x 内核）
	content := "Linux version 6.6.36.6-microsoft-standard-WSL2 " +
		"(root@...) (gcc (GCC) 13.3.0) #1 SMP Mon Aug 19 16:49:57 UTC 2024"
	if got := parseWSLVersion(content); got != WSL2 {
		t.Fatalf("WSL2新版应为WSL2，got=%d", got)
	}
}

func TestParseWSLVersionCaseInsensitive(t *testing.T) {
	// 大小写混用应同样被识别
	tests := []struct {
		name    string
		content string
		want    WSLVersion
	}{
		{"WSL1 全大写", "Linux version 4.4.0-MICROSOFT", WSL1},
		{"WSL2 全大写", "Linux version 5.15-MICROSOFT-STANDARD-WSL2", WSL2},
		{"WSL2 混合大小写", "Linux version 5.15-Microsoft-Standard-Wsl2", WSL2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseWSLVersion(tc.content); got != tc.want {
				t.Fatalf("parseWSLVersion(%q) = %d, want %d", tc.content, got, tc.want)
			}
		})
	}
}

func TestWSLVersionConstants(t *testing.T) {
	// 确认常量值语义正确，不会被意外改变
	if WSLNone != 0 {
		t.Fatalf("WSLNone should be 0, got %d", WSLNone)
	}
	if WSL1 != 1 {
		t.Fatalf("WSL1 should be 1, got %d", WSL1)
	}
	if WSL2 != 2 {
		t.Fatalf("WSL2 should be 2, got %d", WSL2)
	}
}
