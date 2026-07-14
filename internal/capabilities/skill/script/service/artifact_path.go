package service

import (
	"os"
	"path/filepath"
	"strings"

	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

// 对外路径展示（LLM / 工具 JSON）——三端统一原则：
//
//	| Backend            | skill_dir / work_dir 回传              | artifacts[].path 回传        |
//	| ------------------ | -------------------------------------- | ---------------------------- |
//	| 无沙箱 (disabled)  | workspace-relative（.genesis/runs/...） | 同左（落在 output/<skill>/） |
//	| 本地平台沙箱       | 同无沙箱（隔离层不另开路径空间）       | 同无沙箱                     |
//	| 远程 genesis-sandbox | sandbox path（/workspace）           | workspace-relative（回收后） |
//
// 本地平台沙箱只改变进程隔离（seatbelt/bwrap/ACL），文件系统仍是宿主工作区；
// 因此不得另造一套 /workspace 展示，也不得回传 D:\ /Users 等宿主绝对路径。
// 远程执行态 cwd 仍是 /workspace；产物回收到宿主 .genesis/runs/.../output 后再相对化。

// projectArtifactsForModel 将产物 Path 投影为工作区相对路径再回传模型。
func projectArtifactsForModel(workspaceRoot string, arts []scriptcontract.Artifact) []scriptcontract.Artifact {
	if len(arts) == 0 {
		return arts
	}
	out := make([]scriptcontract.Artifact, len(arts))
	for i, a := range arts {
		out[i] = a
		out[i].Path = pathForModel(workspaceRoot, a.Path)
	}
	return out
}

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
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	cleaned := filepath.Clean(raw)
	if !filepath.IsAbs(cleaned) {
		return filepath.ToSlash(cleaned)
	}
	if root == "" {
		return filepath.ToSlash(cleaned)
	}
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, cleaned)
	if err != nil {
		return filepath.ToSlash(cleaned)
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(cleaned)
	}
	return filepath.ToSlash(rel)
}

// isSandboxNamespacePath 识别远程 session 内路径（非宿主工作区相对化对象）。
func isSandboxNamespacePath(p string) bool {
	slash := filepath.ToSlash(strings.TrimSpace(p))
	return slash == "/workspace" || strings.HasPrefix(slash, "/workspace/")
}
