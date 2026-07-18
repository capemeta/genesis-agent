// Package model 定义解析后的 Agent App 有效运行配置。
package model

import (
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// WorkspaceSpec 只声明需求、默认偏好和允许上界，不授予资源权限。
type WorkspaceSpec struct {
	DefaultMode     execmodel.WorkspaceMode   `json:"default_mode"`
	AllowedModes    []execmodel.WorkspaceMode `json:"allowed_modes"`
	RequiresProject bool                      `json:"requires_project"`
	Persistent      bool                      `json:"persistent"`
	DefaultAccess   execmodel.WorkspaceAccess `json:"default_access"`
}

// EffectiveProfile 是产品在进入统一内核前完成身份与配置合并后的只读快照。
type EffectiveProfile struct {
	ID        string        `json:"id"`
	Version   string        `json:"version"`
	Workspace WorkspaceSpec `json:"workspace"`
}

// Validate 保证进入运行内核的是完整且自洽的有效配置快照。
func (p EffectiveProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Version) == "" || p.ID != strings.TrimSpace(p.ID) || p.Version != strings.TrimSpace(p.Version) {
		return fmt.Errorf("effective agent app 缺少 id/version")
	}
	if len(p.Workspace.AllowedModes) == 0 {
		return fmt.Errorf("agent app %s 未声明 allowed_modes", p.ID)
	}
	allowed := map[execmodel.WorkspaceMode]struct{}{}
	for _, mode := range p.Workspace.AllowedModes {
		switch mode {
		case execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession:
			allowed[mode] = struct{}{}
		default:
			return fmt.Errorf("agent app %s 包含非法 workspace mode %q", p.ID, mode)
		}
	}
	if _, ok := allowed[p.Workspace.DefaultMode]; !ok {
		return fmt.Errorf("agent app %s default_mode 不在 allowed_modes", p.ID)
	}
	if p.Workspace.DefaultAccess != execmodel.WorkspaceAccessReadOnly && p.Workspace.DefaultAccess != execmodel.WorkspaceAccessReadWrite {
		return fmt.Errorf("agent app %s default_access 非法", p.ID)
	}
	return nil
}
