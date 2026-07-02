// Package model 定义文件系统能力的纯数据模型。
package model

// PathRef 是工具输入中的原始路径。
type PathRef struct {
	Raw string `json:"raw"`
}

// PathScope 描述解析后路径所在的安全范围。
type PathScope string

const (
	PathScopeWorkspace PathScope = "workspace"
	PathScopeExternal  PathScope = "external"
	PathScopeProtected PathScope = "protected"
)

// ResolvedPath 是经过 PathResolver 校验后的路径。
type ResolvedPath struct {
	DisplayPath  string    `json:"display_path"`
	BackendPath  string    `json:"backend_path"`
	WorkspaceRel string    `json:"workspace_rel"`
	WorkspaceID  string    `json:"workspace_id"`
	Scope        PathScope `json:"scope"`
	RawPath      string    `json:"raw_path,omitempty"`
}
