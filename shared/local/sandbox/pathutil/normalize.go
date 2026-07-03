// Package pathutil 提供 sandbox 使用的跨平台路径规范化工具。
package pathutil

import (
	"fmt"
	"path/filepath"
)

// NormalizeResult 描述单条路径规范化结果。
type NormalizeResult struct {
	Original   string
	Normalized string
	Err        error
}

// Normalize 对路径进行完整规范化：
//  1. filepath.Abs（解析相对路径 + 操作系统规范分隔符）
//  2. filepath.EvalSymlinks（解析软链接；路径不存在时跳过此步，原样使用 Abs 结果）
//  3. 平台特定处理（Windows：reparse point / junction / 8.3 short path 由 normalize_windows.go 处理）
func Normalize(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("filepath.Abs %q: %w", path, err)
	}
	// EvalSymlinks 要求路径存在；路径不存在时使用 Abs 结果（sandbox 规则可能指向将来创建的目录）
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// 路径不存在：用 Abs 结果（已去除多余 separator、已规范化大小写）
		resolved = platformClean(abs)
	}
	return resolved, nil
}

// NormalizeList 批量规范化路径列表，跳过空路径，收集所有错误。
// 返回的 results 与输入 paths 一一对应（空字符串条目 Err 不为 nil）。
func NormalizeList(paths []string) []NormalizeResult {
	results := make([]NormalizeResult, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			results = append(results, NormalizeResult{Original: p, Err: fmt.Errorf("空路径已跳过")})
			continue
		}
		normalized, err := Normalize(p)
		results = append(results, NormalizeResult{Original: p, Normalized: normalized, Err: err})
	}
	return results
}

// NormalizeListBestEffort 批量规范化，忽略错误，只返回成功规范化的路径。
// 适用于 sandbox builder：规范化失败的路径原样保留，避免策略丢失。
func NormalizeListBestEffort(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		normalized, err := Normalize(p)
		if err != nil {
			normalized = filepath.Clean(p) // 降级：至少做基础清理
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
