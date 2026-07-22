package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FilePatch 描述单文件变更补丁。
type FilePatch struct {
	RelativePath string `json:"relative_path"`
	NewContent   string `json:"new_content"`
}

// WorkspacePatch 描述来自远程子 Agent 容器的增量补丁。
type WorkspacePatch struct {
	PatchText string      `json:"patch_text"`
	Summary   string      `json:"summary"`
	Files     []FilePatch `json:"files,omitempty"`
}

// WorkspacePatchReconciler 负责校验并将远程子 Agent 的 Patch 对账 apply 到本地宿主机绝对路径工作区。
type WorkspacePatchReconciler struct{}

// NewWorkspacePatchReconciler 创建工作区补丁对账器。
func NewWorkspacePatchReconciler() *WorkspacePatchReconciler {
	return &WorkspacePatchReconciler{}
}

// ApplyPatch 校验物理绝对路径，防范目录越权，并安全 apply 补丁到宿主机工作区。
func (r *WorkspacePatchReconciler) ApplyPatch(baseDir string, patch WorkspacePatch) error {
	cleanBase := filepath.Clean(baseDir)
	if strings.TrimSpace(cleanBase) == "" || !filepath.IsAbs(cleanBase) {
		return fmt.Errorf("patch reconciler 要求绝对路径 baseDir: %s", baseDir)
	}

	// 1. 若有明确的 FilePatch 列表，直接应用文件更新
	if len(patch.Files) > 0 {
		for _, file := range patch.Files {
			rel := filepath.Clean(file.RelativePath)
			if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				return fmt.Errorf("越权目录穿透风险已被阻断: %s", file.RelativePath)
			}

			targetAbs := filepath.Join(cleanBase, rel)
			if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
				return fmt.Errorf("创建补丁目标目录失败: %w", err)
			}

			if err := os.WriteFile(targetAbs, []byte(file.NewContent), 0644); err != nil {
				return fmt.Errorf("写入补丁文件失败 %s: %w", targetAbs, err)
			}
		}
	}

	return nil
}
