// Package permission 提供文件系统风险上下文构建。
package permission

import (
	"fmt"
	"path/filepath"
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

	isWriteOp := op == OperationWrite || op == OperationEdit || op == OperationDelete
	category, matched := classifyPathCategory(path)

	if matched {
		metadata["path_category"] = string(category)
		switch category {
		case CategorySystemCritical:
			metadata["protected"] = "true"
			if isWriteOp {
				metadata["critical"] = "true"
				metadata["deny_reason"] = "system critical path write prohibited"
			}
		case CategoryAuthCredential:
			metadata["protected"] = "true"
			if isWriteOp {
				metadata["critical"] = "true"
				metadata["deny_reason"] = "credential and secret key write prohibited"
			}
		case CategoryPersistence, CategoryNetworkConfig:
			metadata["protected"] = "true"
			if isWriteOp {
				metadata["dangerous"] = "true"
			}
		case CategoryAutoWorkflow:
			if isWriteOp {
				metadata["protected"] = "true"
				metadata["dangerous"] = "true"
			}
		}
	} else if path.Scope == model.PathScopeProtected {
		metadata["protected"] = "true"
		if isWriteOp {
			metadata["critical"] = "true"
			metadata["deny_reason"] = "system protected path"
		}
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
	if path.Scope == model.PathScopeExternal || (matched && category != CategoryAutoWorkflow) {
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
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
			approvalmodel.GrantScopeProject,
		},
		Metadata: metadata,
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

type PathRiskCategory string

const (
	CategorySystemCritical PathRiskCategory = "system_critical" // 系统致命级
	CategoryAuthCredential PathRiskCategory = "auth_credential" // 凭证与秘钥级
	CategoryPersistence    PathRiskCategory = "persistence"     // 启动项与挂钩持久化
	CategoryNetworkConfig  PathRiskCategory = "network_config"  // 网络与Hosts配置
	CategoryAutoWorkflow   PathRiskCategory = "auto_workflow"   // CI/Hook自动化配置
)

func classifyPathCategory(path model.ResolvedPath) (PathRiskCategory, bool) {
	p := strings.ToLower(strings.ReplaceAll(filepath.Clean(path.BackendPath), "\\", "/"))
	rel := strings.ToLower(strings.ReplaceAll(path.WorkspaceRel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "./")

	// 1. 系统致命级 (SystemCritical): SAM, System32, sudoers, pam.d, SIP, TCC, Kernel, Systemd
	sysCritical := []string{
		"/windows/system32", "/windows/syswow64", "/windows/winsxs", "/windows/servicing", "/windows/microsoft.net",
		"system32/config/sam", "system32/config/system", "system32/config/security",
		"bootmgr", "pagefile.sys", "hiberfil.sys", "swapfile.sys", "dumpstack.log",
		"/etc/passwd", "/etc/shadow", "/etc/group", "/etc/gshadow", "/etc/sudoers", "/etc/sudoers.d",
		"/etc/pam.d", "/etc/ld.so.preload", "/etc/ld.so.conf",
		"/proc", "/sys", "/dev", "/boot", "/system/library", "/private/var/db",
		"com.apple.tcc/tcc.db", "/library/keychains",
	}
	for _, target := range sysCritical {
		if isPathSegmentMatch(p, target) {
			return CategorySystemCritical, true
		}
	}

	// 2. 凭证私钥级 (AuthCredential): .ssh, .aws, .kube, .docker, Keychain, 明文密码
	authCreds := []string{
		".ssh", ".aws", ".kube", ".azure", ".docker", ".gnupg", ".netrc", ".pgpass", ".my.cnf", ".mylogin.cnf",
		".git-credentials", ".gitcredentials", ".npmrc", ".pypirc", ".cargo/credentials",
		".config/gcloud", ".config/doctl", ".config/oci", ".alibabacloud", ".tencentcloud", ".huaweicloud",
		"/etc/kubernetes", "/etc/ssl/private", "/etc/pki/private", "/etc/wireguard", "/etc/krb5.keytab",
		".openai", ".anthropic", ".cursor", ".claude", ".codex",
	}
	for _, target := range authCreds {
		if isPathSegmentMatch(p, target) || isPathSegmentMatch(rel, target) {
			return CategoryAuthCredential, true
		}
	}

	// 秘钥扩展名与文件匹配
	baseName := filepath.Base(p)
	if strings.HasPrefix(baseName, ".env") || baseName == "id_rsa" || baseName == "id_ed25519" || baseName == "id_dsa" || baseName == "id_ecdsa" || baseName == "authorized_keys" {
		return CategoryAuthCredential, true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".asc", ".kdbx", ".ppk", ".jks", ".keystore"} {
		if strings.HasSuffix(baseName, ext) {
			return CategoryAuthCredential, true
		}
	}

	// 3. 启动项与 Shell 持久化 (Persistence): Cron, Systemd, Shell Profile, Startup, LaunchAgents
	persistence := []string{
		".bashrc", ".bash_profile", ".zshrc", ".profile", "/etc/profile", "/etc/bash.bashrc", "/etc/profile.d",
		"/etc/crontab", "/etc/cron.", "/var/spool/cron", "/etc/anacrontab",
		"/etc/systemd/system", "/lib/systemd/system", "/usr/lib/systemd/system",
		"/etc/init.d", "/etc/rc.local", "/library/launchdaemons", "/library/launchagents",
		"start menu/programs/startup", "windowspowershell", "powershell",
		"/var/run/docker.sock", "/run/docker.sock", "/run/containerd/containerd.sock",
	}
	for _, target := range persistence {
		if isPathSegmentMatch(p, target) {
			return CategoryPersistence, true
		}
	}

	// 4. 网络与域名配置 (NetworkConfig): hosts, resolv.conf, netplan
	network := []string{
		"system32/drivers/etc/hosts", "/etc/hosts", "/private/etc/hosts",
		"/etc/resolv.conf", "/etc/hostname", "/etc/network", "/etc/netplan", "/etc/networkmanager",
	}
	for _, target := range network {
		if isPathSegmentMatch(p, target) {
			return CategoryNetworkConfig, true
		}
	}

	// 5. 自动工作流配置 (AutoWorkflow): CI/Hook
	autoWorkflow := []string{
		".github/workflows", ".gitlab-ci.yml", ".husky", "jenkinsfile", ".devcontainer", ".vscode/tasks.json",
	}
	for _, target := range autoWorkflow {
		if isPathSegmentMatch(rel, target) || isPathSegmentMatch(p, target) {
			return CategoryAutoWorkflow, true
		}
	}

	return "", false
}

// isPathSegmentMatch 实施严密的路径段边界匹配，消灭类似 system32-notes.txt 的子串误杀
func isPathSegmentMatch(targetPath, protectedPattern string) bool {
	target := strings.ToLower(strings.ReplaceAll(filepath.Clean(targetPath), "\\", "/"))
	pattern := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(protectedPattern), "\\", "/"))

	if target == pattern {
		return true
	}
	if strings.HasPrefix(pattern, "/") {
		if target == pattern || strings.HasPrefix(target, pattern+"/") {
			return true
		}
	} else {
		// 检查路径段包含
		if strings.Contains(target, "/"+pattern+"/") || strings.HasPrefix(target, pattern+"/") || strings.HasSuffix(target, "/"+pattern) {
			return true
		}
	}
	return false
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

