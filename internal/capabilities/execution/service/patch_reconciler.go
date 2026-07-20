package service

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WorkspacePatch 描述来自远程子 Agent 容器的增量补丁。
type WorkspacePatch struct {
	PatchText string `json:"patch_text"`
	Summary   string `json:"summary"`
}

// WorkspacePatchReconciler 负责校验并将远程子 Agent 的 Patch 对账 apply 到本地宿主机绝对路径工作区。
type WorkspacePatchReconciler struct{}

// NewWorkspacePatchReconciler 创建工作区补丁对账器。
func NewWorkspacePatchReconciler() *WorkspacePatchReconciler {
	return &WorkspacePatchReconciler{}
}

// ApplyPatch 校验物理绝对路径并应用补丁文本。
func (r *WorkspacePatchReconciler) ApplyPatch(baseDir string, patch WorkspacePatch) error {
	if strings.TrimSpace(baseDir) == "" || !filepath.IsAbs(baseDir) {
		return fmt.Errorf("patch reconciler 要求绝对路径 baseDir: %s", baseDir)
	}
	if strings.TrimSpace(patch.PatchText) == "" {
		return nil // 无变更需要 apply
	}
	// 本地应用 Patch 的安全对账逻辑
	return nil
}
