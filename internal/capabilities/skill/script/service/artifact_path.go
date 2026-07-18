package service

import (
	"path/filepath"
	"strings"
)

// 本地 backend 对模型仅显示项目相对工作路径；远程 backend 显示 sandbox 路径。
// produced 候选由独立函数转换为 run:/ 稳定引用，正式 Artifact 不经过本文件投影。

// projectHostWorkDirsForModel 对宿主侧 backend（无沙箱 / 本地平台沙箱）相对化 skill/work dir。
// 远程 backend 的 /workspace 原样保留，调用方勿对本函数传入远程 cwd。
func projectHostWorkDirsForModel(workspaceRoot, skillOrWorkDir string) string {
	return pathForModel(workspaceRoot, skillOrWorkDir)
}

func pathForModel(workspaceRoot, pathValue string) string {
	raw := strings.TrimSpace(pathValue)
	if raw == "" {
		return ""
	}
	// 远程 sandbox 路径空间：保持容器内绝对路径，不相对化到宿主工作区。
	if isSandboxNamespacePath(raw) {
		return filepath.ToSlash(raw)
	}
	root := strings.TrimSpace(workspaceRoot)
	cleaned := filepath.Clean(raw)
	if !filepath.IsAbs(cleaned) {
		return filepath.ToSlash(cleaned)
	}
	if root == "" {
		return ""
	}
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, cleaned)
	if err != nil {
		return filepath.ToSlash(cleaned)
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// isSandboxNamespacePath 识别远程 session 内路径（非宿主工作区相对化对象）。
func isSandboxNamespacePath(p string) bool {
	slash := filepath.ToSlash(strings.TrimSpace(p))
	return slash == "/workspace" || strings.HasPrefix(slash, "/workspace/")
}
