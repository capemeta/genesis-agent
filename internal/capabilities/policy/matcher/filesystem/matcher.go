// Package filesystem 提供文件系统策略 matcher。
package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/platform/config"
)

// Matcher 根据文件系统 request metadata 执行文件策略。
type Matcher struct {
	defaults config.PolicyDefaultsConfig
	files    config.PolicyFilesConfig
}

// New 创建文件系统 matcher。
func New(defaults config.PolicyDefaultsConfig, files config.PolicyFilesConfig) *Matcher {
	return &Matcher{defaults: defaults, files: files}
}

// Match 判断文件请求是否命中文件策略。
func (m *Matcher) Match(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.PolicyResult{}, false, err
	}
	if !strings.HasPrefix(string(req.Action), "file.") {
		return approvalmodel.PolicyResult{}, false, nil
	}
	metadata := mergeMetadata(req)
	op := operationOf(req, metadata)
	path := requestPath(req, metadata)

	if metadata["critical"] == "true" || metadata["protected"] == "true" || metadata["scope"] == "protected" || metadata["path_scope"] == "protected" {
		return resultOf(m.files.Protected.Default, "protected file resource", approvalmodel.RiskCritical, req.SuggestedScopes), true, nil
	}
	if metadata["workspace_metadata_write"] == "true" || (isWorkspaceMetadataOperation(op) && m.matchesWorkspaceMetadataPath(workspaceRel(req, metadata))) {
		return resultOf(m.files.WorkspaceMetadata.Write, "workspace metadata write policy", approvalmodel.RiskCritical, req.SuggestedScopes), true, nil
	}
	if rule, ok := m.matchPathRule(path, op, m.files.DenyPaths); ok {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyDeny, Reason: fmt.Sprintf("path denied by policy: %s", rule.Path), Risk: riskOf(op, metadata), SuggestedScopes: req.SuggestedScopes}, true, nil
	}
	if rule, ok := m.matchPathRule(path, op, m.files.AllowPaths); ok {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: fmt.Sprintf("path allowed by policy: %s", rule.Path), Risk: riskOf(op, metadata), SuggestedScopes: req.SuggestedScopes}, true, nil
	}

	scope := firstNonEmpty(metadata["scope"], metadata["path_scope"])
	switch scope {
	case "workspace":
		return resultOf(fileOperationDecision(m.files.Workspace, op, m.files.Default), "workspace file policy", riskOf(op, metadata), req.SuggestedScopes), true, nil
	case "external":
		return resultOf(fileOperationDecision(m.files.External, op, m.files.Default), "external file policy", riskOf(op, metadata), externalScopes(req.SuggestedScopes)), true, nil
	case "protected":
		return resultOf(m.files.Protected.Default, "protected file resource", approvalmodel.RiskCritical, req.SuggestedScopes), true, nil
	default:
		return resultOf(m.files.Default, "file policy default", riskOf(op, metadata), req.SuggestedScopes), true, nil
	}
}

func (m *Matcher) matchesWorkspaceMetadataPath(rel string) bool {
	rel = normalizeWorkspaceRel(rel)
	if rel == "" {
		return false
	}
	for _, candidate := range m.files.WorkspaceMetadata.Paths {
		root := normalizeWorkspaceRel(candidate)
		if root == "" {
			continue
		}
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func workspaceRel(req approvalmodel.Request, metadata map[string]string) string {
	if rel := strings.TrimSpace(metadata["workspace_rel"]); rel != "" {
		return rel
	}
	if strings.HasPrefix(req.Resource.URI, "workspace://") {
		return strings.TrimPrefix(req.Resource.URI, "workspace://")
	}
	return ""
}

func normalizeWorkspaceRel(rel string) string {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimSuffix(rel, "/")
	return strings.ToLower(rel)
}

func isWorkspaceMetadataOperation(op string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "write", "edit", "delete":
		return true
	default:
		return false
	}
}
func (m *Matcher) matchPathRule(path string, op string, rules []config.PolicyPathRuleConfig) (config.PolicyPathRuleConfig, bool) {
	if path == "" {
		return config.PolicyPathRuleConfig{}, false
	}
	for _, rule := range rules {
		if !operationMatches(op, rule.Operations) {
			continue
		}
		if isWithinPath(path, os.ExpandEnv(rule.Path)) {
			return rule, true
		}
	}
	return config.PolicyPathRuleConfig{}, false
}

func mergeMetadata(req approvalmodel.Request) map[string]string {
	metadata := make(map[string]string)
	for k, v := range req.Resource.Metadata {
		metadata[k] = v
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	return metadata
}

func operationOf(req approvalmodel.Request, metadata map[string]string) string {
	if op := strings.TrimSpace(metadata["operation"]); op != "" {
		return strings.ToLower(op)
	}
	value := strings.TrimPrefix(string(req.Action), "file.")
	if value == "" {
		return "read"
	}
	return strings.ToLower(value)
}

func requestPath(req approvalmodel.Request, metadata map[string]string) string {
	if backend := strings.TrimSpace(metadata["backend"]); backend != "" {
		return backend
	}
	uri := req.Resource.URI
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

func fileOperationDecision(ops config.PolicyFileOperations, op string, fallback string) string {
	switch op {
	case "read":
		return firstNonEmpty(ops.Read, fallback)
	case "list":
		return firstNonEmpty(ops.List, fallback)
	case "walk":
		return firstNonEmpty(ops.Walk, fallback)
	case "write":
		return firstNonEmpty(ops.Write, fallback)
	case "edit":
		return firstNonEmpty(ops.Edit, fallback)
	case "delete":
		return firstNonEmpty(ops.Delete, fallback)
	default:
		return fallback
	}
}

func resultOf(decision string, reason string, risk approvalmodel.RiskLevel, scopes []approvalmodel.GrantScope) approvalmodel.PolicyResult {
	result := approvalmodel.PolicyResult{Type: decisionOf(decision), Reason: reason, Risk: risk, SuggestedScopes: scopes}
	if result.Type == approvalmodel.PolicyAsk && len(result.SuggestedScopes) == 0 {
		result.SuggestedScopes = []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce}
	}
	return result
}

func decisionOf(value string) approvalmodel.PolicyType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return approvalmodel.PolicyAllow
	case "deny":
		return approvalmodel.PolicyDeny
	default:
		return approvalmodel.PolicyAsk
	}
}

func riskOf(op string, metadata map[string]string) approvalmodel.RiskLevel {
	if metadata["critical"] == "true" || metadata["protected"] == "true" || metadata["workspace_metadata_write"] == "true" {
		return approvalmodel.RiskCritical
	}
	if metadata["destructive"] == "true" || op == "delete" {
		return approvalmodel.RiskHigh
	}
	if metadata["scope"] == "external" || metadata["path_scope"] == "external" {
		return approvalmodel.RiskHigh
	}
	return approvalmodel.RiskLow
}

func operationMatches(op string, operations []string) bool {
	if len(operations) == 0 {
		return true
	}
	for _, candidate := range operations {
		if strings.EqualFold(strings.TrimSpace(candidate), op) {
			return true
		}
	}
	return false
}

func isWithinPath(path string, root string) bool {
	path = normalizePath(path)
	root = normalizePath(root)
	if path == "" || root == "" {
		return false
	}
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimRight(root, sep)+sep)
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func externalScopes(scopes []approvalmodel.GrantScope) []approvalmodel.GrantScope {
	if len(scopes) > 0 {
		return scopes
	}
	return []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
