// Package permission 提供文件系统风险上下文构建。
package permission

import (
	"fmt"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/filesystem/model"
)

// Operation 描述文件操作类型。
type Operation string

const (
	OperationRead   Operation = "read"
	OperationWrite  Operation = "write"
	OperationEdit   Operation = "edit"
	OperationDelete Operation = "delete"
	OperationList   Operation = "list"
	OperationWalk   Operation = "walk"
)

// BuildApprovalRequest 将文件系统上下文转换成通用审批请求。
func BuildApprovalRequest(toolName string, op Operation, path model.ResolvedPath) approvalmodel.Request {
	metadata := map[string]string{
		"scope":         string(path.Scope),
		"operation":     string(op),
		"workspace":     path.WorkspaceID,
		"raw_path":      path.RawPath,
		"backend":       path.BackendPath,
		"resource":      resourceType(op),
		"path_scope":    string(path.Scope),
		"workspace_rel": path.WorkspaceRel,
	}
	if path.Scope == model.PathScopeProtected || isProtected(path) {
		metadata["protected"] = "true"
		metadata["critical"] = "true"
		metadata["deny_reason"] = "system protected path"
	}
	if isWorkspaceMetadataWrite(op, path) {
		metadata["workspace_metadata_write"] = "true"
		metadata["critical"] = "true"
		metadata["deny_reason"] = "workspace metadata write protected"
	}
	if op == OperationDelete {
		metadata["destructive"] = "true"
	}

	risk := approvalmodel.RiskLow
	if path.Scope == model.PathScopeExternal {
		risk = approvalmodel.RiskHigh
	}
	if metadata["critical"] == "true" {
		risk = approvalmodel.RiskCritical
	}

	return approvalmodel.Request{
		ID:       fmt.Sprintf("%s:%s:%s", toolName, op, path.BackendPath),
		ToolName: toolName,
		Action:   actionOf(op),
		Resource: approvalmodel.Resource{
			Type:     resourceType(op),
			URI:      resourceURI(path),
			Display:  path.DisplayPath,
			Metadata: metadata,
		},
		Reason:          reasonOf(op, path),
		Risk:            risk,
		SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
		Metadata:        metadata,
	}
}

func actionOf(op Operation) approvalmodel.Action {
	switch op {
	case OperationRead:
		return approvalmodel.ActionFileRead
	case OperationWrite:
		return approvalmodel.ActionFileWrite
	case OperationEdit:
		return approvalmodel.ActionFileEdit
	case OperationDelete:
		return approvalmodel.Action("file.delete")
	case OperationList:
		return approvalmodel.ActionFileList
	case OperationWalk:
		return approvalmodel.ActionFileWalk
	default:
		return approvalmodel.Action("file." + op)
	}
}

func resourceType(op Operation) string {
	switch op {
	case OperationList, OperationWalk:
		return "directory"
	default:
		return "file"
	}
}

func resourceURI(path model.ResolvedPath) string {
	if path.Scope == model.PathScopeWorkspace {
		if path.WorkspaceRel == "" {
			return "workspace://."
		}
		return "workspace://" + path.WorkspaceRel
	}
	return "file://" + strings.ReplaceAll(path.BackendPath, "\\", "/")
}

func reasonOf(op Operation, path model.ResolvedPath) string {
	return fmt.Sprintf("%s %s", op, path.DisplayPath)
}

func isProtected(path model.ResolvedPath) bool {
	p := strings.ToLower(strings.ReplaceAll(path.BackendPath, "\\", "/"))
	protectedFragments := []string{
		"/windows/system32",
		"/windows/syswow64",
		"/program files",
		"/program files (x86)",
		"/etc/passwd",
		"/etc/shadow",
	}
	for _, fragment := range protectedFragments {
		if strings.Contains(p, fragment) {
			return true
		}
	}
	return p == ".ssh" || strings.HasSuffix(p, "/.ssh") || strings.Contains(p, "/.ssh/")
}

func isWorkspaceMetadataWrite(op Operation, path model.ResolvedPath) bool {
	if op != OperationWrite && op != OperationEdit && op != OperationDelete {
		return false
	}
	rel := strings.ToLower(strings.ReplaceAll(path.WorkspaceRel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "./")
	for _, protected := range []string{".git", ".codex", ".agents"} {
		if rel == protected || strings.HasPrefix(rel, protected+"/") {
			return true
		}
	}
	return false
}
